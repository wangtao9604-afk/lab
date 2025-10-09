package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	ikeywords "qywx/infrastructures/keywords"
	"qywx/infrastructures/log"
	"qywx/infrastructures/tokenstore"
	"qywx/infrastructures/utils"
	"qywx/infrastructures/wxmsg/kefu"
	"qywx/models/message"
	"qywx/models/thirdpart/minimax"
	prom "qywx/observe/prometheus"
)

// 单一位置限制提示锚文本（与 Prompt 保持一致，便于去重）
const singleLocGuardLead = "为了保证推荐质量，小胖一次只能在单一区域/板块为您筛选房源。"

// 会话级“等待单一位置选择”标记：key=会话ID，value=true
var awaitingSingleLoc sync.Map

var (
	// 用户侧“无更多/催单”触发词（可按需要增删）
	stopWordsForEnd = []string{
		"没有", "没了", "无", "都可以", "不限", "随便", "差不多", "就这样",
		"可以开始推荐", "直接推荐", "生成报告", "帮忙推荐", "出清单", "你看着办",
		"不用了", "够了", "OK", "ok", "好的", "行了",
		"？", "?", "还在吗", "在么", "在不在", "可以了吗", "出吗", "好了没", "出报告",
	}
	// AI 侧“承诺语”触发词
	promisePhrases = []string{
		"我会为您推荐", "我会为你推荐", "我将为您", "我将为你",
		"为您生成", "为你生成", "我来生成", "我来准备", "马上为您", "稍后为您",
		"我现在去准备", "我会尽快生成", "我马上为您生成", "我马上为你生成",
	}
)

// LocCandidate 表示在用户单条消息里识别到的一个位置候选
type LocCandidate struct {
	District string
	Plate    string
	pos      int // 在原文本里的首现位置，用于排序
}

// detectMultiLocation 从用户本轮文本中识别多位置候选；若数量 >= 2 返回候选列表，否则返回 nil
func (p *Processor) detectMultiLocation(text string) []LocCandidate {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}

	// 1) 优先匹配板块（可反推区）
	plates := ikeywords.GetAllShanghaiPlates()
	seen := make(map[string]bool)
	var cands []LocCandidate
	for _, plate := range plates {
		if idx := strings.Index(t, plate); idx >= 0 {
			if ok, d := ikeywords.IsShanghaiPlate(plate); ok {
				key := d + "/" + plate
				if !seen[key] {
					seen[key] = true
					cands = append(cands, LocCandidate{District: d, Plate: plate, pos: idx})
				}
			}
		}
	}

	// 2) 再匹配区（若该区已有板块候选，则忽略“仅区”以避免重复）
	for d := range ikeywords.ShanghaiPlates {
		if idx := strings.Index(t, d); idx >= 0 {
			hasPlateUnderD := false
			for _, c := range cands {
				if c.District == d && c.Plate != "" {
					hasPlateUnderD = true
					break
				}
			}
			if !hasPlateUnderD {
				key := d + "/"
				if !seen[key] {
					seen[key] = true
					cands = append(cands, LocCandidate{District: d, Plate: "", pos: idx})
				}
			}
		}
	}

	if len(cands) < 2 {
		return nil
	}

	// 3) 按出现顺序排序，去重并限流（最多 5 个）
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].pos < cands[j].pos })
	uniq := make([]LocCandidate, 0, len(cands))
	seenPair := make(map[string]bool)
	for _, c := range cands {
		key := c.District + "/" + c.Plate
		if !seenPair[key] {
			seenPair[key] = true
			uniq = append(uniq, c)
			if len(uniq) >= 5 {
				break
			}
		}
	}
	if len(uniq) >= 2 {
		return uniq
	}
	return nil
}

// buildSingleLocationLimitReply 生成“单一位置限制”的礼貌提示 + 选项列表
// 约定：当 len(cands) < 2 时返回空串；由调用方决定是否发送（契约式写法）
func (p *Processor) buildSingleLocationLimitReply(cands []LocCandidate) string {
	if len(cands) < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString(singleLocGuardLead + "\n")
	b.WriteString("请先从下列选项中选择一个开始，我再按这个位置继续为您匹配：\n")
	for i, c := range cands {
		label := strings.TrimSpace(c.District)
		if c.Plate != "" {
			label = c.District + "/" + c.Plate
		}
		fmt.Fprintf(&b, "%d）%s\n", i+1, label)
	}
	b.WriteString("也可以直接回复一个区或板块的名称。")
	return b.String()
}

// convKeyFromMsg 返回当前消息所属的会话唯一键，用于跨轮去重标记。
func (p *Processor) convKeyFromMsg(msg *kefu.KFRecvMessage) string {
	if msg == nil {
		return ""
	}
	// 使用企业微信的 ExternalUserID 作为会话标识
	return msg.ExternalUserID
}

// PurchaseIntent 购房意向
type PurchaseIntent int

const (
	IntentAll PurchaseIntent = iota
	IntentNew
	IntentSecond
)

// SetPurchaseIntent 设置购房意向
func (p *Processor) setPurchaseIntent(intent PurchaseIntent) {
	p.purchaseIntentMu.Lock()
	p.purchaseIntent = intent
	p.purchaseIntentMu.Unlock()
}

// GetPurchaseIntent 获取购房意向
func (p *Processor) getPurchaseIntent() PurchaseIntent {
	p.purchaseIntentMu.RLock()
	defer p.purchaseIntentMu.RUnlock()
	return p.purchaseIntent
}

// run processor主循环
func (p *Processor) run() {
	defer p.scheduler.wg.Done()

	log.GetInstance().Sugar.Info("Processor started for user: ", p.userID)
	defer log.GetInstance().Sugar.Info("Processor stopped for user: ", p.userID)

	// 清理时移除自己
	defer p.scheduler.removeProcessor(p.userID)

	// 清理关键词提取资源
	defer p.cleanupKeywordsExtraction()

	// 生命周期计时器
	lifetimeTimer := time.NewTimer(p.lifetime)
	defer lifetimeTimer.Stop()

	for {
		select {
		case <-p.ctx.Done():
			// 调度器停止
			return

		case <-lifetimeTimer.C:
			// 生命周期到期
			log.GetInstance().Sugar.Info("Processor lifetime expired for user: ", p.userID)
			return

		case <-p.resetTimer:
			// 重置生命周期计时器
			// Stop()返回false表示计时器已触发或已停止
			// 此时需要非阻塞地清理channel中可能存在的旧触发信号
			// 避免Reset后立即收到"幽灵"超时信号
			if !lifetimeTimer.Stop() {
				select {
				case <-lifetimeTimer.C: // 尝试清理旧信号
				default: // channel为空则继续
				}
			}
			lifetimeTimer.Reset(p.lifetime)
			log.GetInstance().Sugar.Debug("Reset processor lifetime for user: ", p.userID)

		case in, ok := <-p.messageChan:
			if !ok {
				// channel被关闭
				return
			}

			if in == nil {
				continue
			}

			msg := in.Msg
			if msg == nil {
				if in.Ack != nil {
					in.Ack(true)
				}
				continue
			}

			// 检查是否处于压测模式
			cfg := config.GetInstance()
			if cfg.Stress {
				if err := p.processStressMessage(in); err != nil {
					log.GetInstance().Sugar.Error("Process stress message failed for user ", p.userID, ": ", err)
				}
			} else {
				if err := p.processMessage(msg); err != nil {
					log.GetInstance().Sugar.Error("Process message failed for user ", p.userID, ": ", err)
				}
			}

			if in.Ack != nil {
				in.Ack(true)
			}
		}
	}
}

// processMessage 处理单条消息
func (p *Processor) processMessage(msg *kefu.KFRecvMessage) error {
	// 创建处理超时context（从配置读取，默认30秒）
	cfg := config.GetInstance()
	timeout := time.Duration(cfg.KFConfig.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// 新用户消息到来：取消旧的思考等待器，并推进轮次序号
	p.stopThinkingWaiter()
	p.incrementTurnSeq()

	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	// 构建deadline用于传递给下层
	deadline, _ := ctx.Deadline()

	// 根据消息类型处理
	switch msg.MsgType {
	case kefu.KFMsgTypeText:
		return p.handleTextMessage(ctx, deadline, msg)
	case kefu.KFMsgTypeImage:
		return p.handleImageMessage(ctx, deadline, msg)
	case kefu.KFMsgTypeEvent:
		return p.handleEventMessage(ctx, deadline, msg)
	default:
		log.GetInstance().Sugar.Info("Unsupported message type: ", msg.MsgType)
		return nil
	}
}

// handleTextMessage 处理文本消息
func (p *Processor) handleTextMessage(ctx context.Context, deadline time.Time, msg *kefu.KFRecvMessage) error {
	if msg.Text == nil {
		return nil
	}

	// 每条用户文本开头，重置服务端“自动结束”一次性开关
	atomic.StoreInt32(&p.autoEndOnce, 0)

	// 更新当前客服账号ID
	if msg.OpenKFID != "" {
		p.currentOpenKFID = msg.OpenKFID
	}

	// 在开始处理前检查context
	select {
	case <-ctx.Done():
		return fmt.Errorf("processing cancelled: %w", ctx.Err())
	default:
	}

	log.GetInstance().Sugar.Info("Processing text message from ", msg.ExternalUserID, ": ", msg.Text.Content)

	// 1. 添加用户消息到对话历史
	if err := p.addUserMessage(msg.Text.Content); err != nil {
		log.GetInstance().Sugar.Error("Failed to add user message to history: ", err)
		// 对话历史是核心功能，失败就应该报错，不要降级
		return fmt.Errorf("failed to add user message to history: %w", err)
	}

	// 若本轮文本中包含 >=2 个不同区/板块，则礼貌提示用户先选一个，直接返回；避免进入 LLM 造成重复提示
	key := p.convKeyFromMsg(msg)
	cands := p.detectMultiLocation(msg.Text.Content)

	if len(cands) >= 2 {
		// 若上一轮已经提示过“请先单选”，本轮简化提示，避免重复整段话术
		if key != "" {
			if v, ok := awaitingSingleLoc.Load(key); ok {
				if vb, ok2 := v.(bool); ok2 && vb {
					reply := "我们仍需先确定一个区域/板块，请先从上面的选项里选一个开始，或直接回复区/板块名称。"
					if err := p.addAssistantMessage(reply, false); err != nil {
						log.GetInstance().Sugar.Error("Failed to add single-location guard reply (short): ", err)
					}
					if err := p.sendAIReply(ctx, msg, reply); err != nil {
						log.GetInstance().Sugar.Warnf("Failed to send single-location guard reply (short): %v", err)
					}
					return nil
				}
			}
		}

		// 首次命中“同轮多位置”：给完整的候选列表提示，并置位“等待单选”
		if reply := p.buildSingleLocationLimitReply(cands); reply != "" {
			if err := p.addAssistantMessage(reply, false); err != nil {
				log.GetInstance().Sugar.Error("Failed to add single-location guard reply: ", err)
			}
			if err := p.sendAIReply(ctx, msg, reply); err != nil {
				log.GetInstance().Sugar.Warnf("Failed to send single-location guard reply: %v", err)
			}
			if key != "" {
				awaitingSingleLoc.Store(key, true)
				// 可选：自动过期，避免长会话遗留（例如 10 分钟）
				time.AfterFunc(10*time.Minute, func() {
					awaitingSingleLoc.Delete(key)
				})
			}
			return nil
		}
	}

	// 未检测到“同轮多位置”：如之前处于“等待单选”，则清标记
	if key != "" {
		if _, ok := awaitingSingleLoc.Load(key); ok {
			awaitingSingleLoc.Delete(key)
		}
	}

	// 1.5 异步提取关键词（非阻塞）
	select {
	case p.keywordsChan <- msg.Text.Content:
		// 成功入队
		log.GetInstance().Sugar.Debug("Enqueued message for keyword extraction")
	default:
		// 队列满，记录日志但不阻塞
		log.GetInstance().Sugar.Warn("Keywords channel full, skipping extraction")
	}

	// 1.6 若存在pending，尝试从用户文本中直接解析“是否切换/是否重置”的意图（肯定/否定短语门禁）
	p.maybeHandlePendingDecisionFromUser(msg.Text.Content)

	// 2. MiniMax客户端是必需的，没有它系统无法工作
	if p.minimaxClient == nil {
		// 记录严重错误并返回，不要假装系统还能工作
		log.GetInstance().Sugar.Error("FATAL: MiniMax client not initialized for user ", msg.ExternalUserID, " - system cannot function without AI brain")
		return fmt.Errorf("MiniMax client not available - critical system component missing")
	}

	// 打点：本轮开始（读取当前轮次与pending状态）
	p.thinkWaiterMu.Lock()
	curSeq := p.turnSeq
	p.thinkWaiterMu.Unlock()
	p.keywordsMu.RLock()
	hasPendingAtStart := (p.pendingLoc != nil)
	p.keywordsMu.RUnlock()
	log.GetInstance().Sugar.Infof("AI turn begin: seq=%d, pending=%v, user=%s", curSeq, hasPendingAtStart, msg.ExternalUserID)

	// 3. 调用MiniMax API获取智能回复
	aiResponse, err := p.callMiniMaxAPI(ctx)
	if err != nil {
		// 记录详细错误用于监控告警
		log.GetInstance().Sugar.Error("MiniMax API call failed for user ", msg.ExternalUserID, ": ", err)
		// API调用失败是运行时问题，暂时保留fallback给用户友好提示
		return p.sendFallbackReply(ctx, msg)
	}

	// 打点：AI返回概要
	var tcCount int
	if aiResponse.RawResponse != nil && len(aiResponse.RawResponse.Choices) > 0 && aiResponse.RawResponse.Choices[0].Message != nil {
		tcCount = len(aiResponse.RawResponse.Choices[0].Message.ToolCalls)
	}
	log.GetInstance().Sugar.Infof("AI turn resp: seq=%d, end=%v, tool_calls=%d, content_len=%d",
		curSeq, aiResponse.IsConversationEnd, tcCount, len(aiResponse.Content))

	// 3.5 处理AI提取的关键词（如果有）
	if aiResponse.ExtractedKeywords != nil && len(aiResponse.ExtractedKeywords) > 0 {
		log.GetInstance().Sugar.Info("AI extracted keywords for user ", msg.ExternalUserID, ": ", aiResponse.ExtractedKeywords)

		// Function Call返回使用统一合并策略
		p.deepMergeFromFunctionCall(aiResponse.ExtractedKeywords)
	}

	// 3.6 处理模型返回的工具调用（若存在）- 统一分发
	if aiResponse.RawResponse != nil && len(aiResponse.RawResponse.Choices) > 0 && aiResponse.RawResponse.Choices[0].Message != nil {
		toolCalls := aiResponse.RawResponse.Choices[0].Message.ToolCalls
		if len(toolCalls) > 0 {
			p.handleToolCalls(toolCalls, toolModeMainFlow)
		}
	}

	// 在处理文本前：若AI判定结束且存在未拦截的pending，优先发起位置确认（不下发本次结束文本）
	if aiResponse.IsConversationEnd {
		p.keywordsMu.Lock()
		hasPendingFirst := (p.pendingLoc != nil && !p.pendingEndIntercepted)
		p.keywordsMu.Unlock()
		if hasPendingFirst {
			if p.handleEndAttempt(ctx, msg, aiResponse.EndCallData, aiResponse.IsConversationEnd) {
				return nil
			}
		}
	}

	// 4. 处理AI回复内容
	responseContent := p.extractResponseContent(aiResponse.Content)

	// 若模型本轮没有显式 end，但已具备核心条件 + 命中触发词，则由服务端自动结束并进入后续流程
	if !aiResponse.IsConversationEnd {
		if p.tryServerAutoEnd(ctx, msg, msg.Text.Content, responseContent) {
			return nil // 已完成收尾与后续投递，本轮结束
		}
	}
	// 判定日志：空文本分支前打印长度信息
	log.GetInstance().Sugar.Infof("AI response content lens: raw=%d, trimmed=%d, user=%s",
		len(responseContent), len(strings.TrimSpace(responseContent)), msg.ExternalUserID)

	// 如果是空内容（仅tool_calls无文本），生成“确认+追问”的最小可读回复，避免用户感知为无应答
	if strings.TrimSpace(responseContent) == "" {
		log.GetInstance().Sugar.Info("AI response contains only tool calls for user, no message ", msg.ExternalUserID)
		// 启动“思考中”轮询机制（3s占位，最多10s兜底）
		log.GetInstance().Sugar.Infof("Start thinking waiter: seq=%d, user=%s", curSeq, msg.ExternalUserID)
		p.startThinkingWaiter(ctx, msg)
		return nil
	} else {
		// 4.1 若助手文本中包含“伪工具调用”标记（如：[调用confirm_location_change: …]），先解析并应用，随后再入历史与下发
		p.maybeApplyPseudoToolCallsFromText(responseContent)
		// 4.2 抑制连续重复文本：若与上一条assistant文本内容完全一致，则不再重复发送
		if p.isDuplicateAssistantText(responseContent) {
			log.GetInstance().Sugar.Info("Suppressed duplicate assistant text for user ", msg.ExternalUserID)
			// 不中断逻辑：若AI标记结束，后续结束处理仍会继续；否则本轮就不重复回帖
			responseContent = ""
		}
		if strings.TrimSpace(responseContent) != "" {
			// 5. 添加AI回复到对话历史
			if err := p.addAssistantMessage(responseContent, true); err != nil {
				log.GetInstance().Sugar.Error("Failed to add assistant message to history: ", err)
				// 对话历史完整性是核心功能，失败应该报错
				return fmt.Errorf("failed to add assistant message to history: %w", err)
			}

			// 6. 发送AI回复给用户
			if err := p.sendAIReply(ctx, msg, responseContent); err != nil {
				return fmt.Errorf("failed to send AI reply: %w", err)
			}
			log.GetInstance().Sugar.Infof("AI turn sent reply: seq=%d, len=%d, user=%s", curSeq, len(responseContent), msg.ExternalUserID)
		}
	}

	// 7. 检查对话是否结束（统一走拦截/收尾逻辑）
	if aiResponse.IsConversationEnd {
		// 若存在待确认的位置切换，拦截结束，但仅拦截一次
		p.keywordsMu.Lock()
		hasPending := (p.pendingLoc != nil)
		firstIntercept := false
		var pd *PendingLoc
		if hasPending {
			pd = p.pendingLoc
			if !p.pendingEndIntercepted {
				// 第一次拦截：标记后发确认语
				p.pendingEndIntercepted = true
				firstIntercept = true
			} else {
				// 第二次仍要结束：视为不切换，丢弃pending（合并暂存）
				log.GetInstance().Sugar.Infof("Second end while pending; assume no switch and discard pending [%s/%s]",
					p.pendingLoc.District, p.pendingLoc.Plate)
				p.discardPending()
				hasPending = false
			}
		}
		p.keywordsMu.Unlock()

		if hasPending && firstIntercept {
			log.GetInstance().Sugar.Infof("Intercepted end: pending location exists [%s/%s]; asking confirmation before report",
				pd.District, pd.Plate)
			confirmText := p.buildPendingConfirmReply()
			if err := p.addAssistantMessage(confirmText, false); err != nil {
				log.GetInstance().Sugar.Error("Failed to add pending confirm to history: ", err)
				return fmt.Errorf("failed to add pending confirm: %w", err)
			}
			if err := p.sendAIReply(ctx, msg, confirmText); err != nil {
				return fmt.Errorf("failed to send pending confirm reply: %w", err)
			}
			return nil
		}

		log.GetInstance().Sugar.Info("Conversation ended for user ", msg.ExternalUserID)
		p.logConversationEnd(aiResponse.EndCallData)

		// 在对话结束时发送任务到furtherProcess channel
		p.sendToFurtherProcess()

		// 不清理历史，因为需要保留历史以便后续的咨询
		// if aiResponse.IsEndFuncCalled {
		// p.clearConversationHistory()
		// log.GetInstance().Sugar.Info("Cleared conversation history for user ", msg.ExternalUserID, " after function call completion")
		// }

	}

	return nil
}

// handleImageMessage 处理图片消息
func (p *Processor) handleImageMessage(ctx context.Context, deadline time.Time, msg *kefu.KFRecvMessage) error {
	if msg.Image == nil {
		return nil
	}

	log.GetInstance().Sugar.Info("Received image from ", msg.ExternalUserID, ": ", msg.Image.MediaID)

	// TODO: 处理图片消息

	return nil
}

// handleEventMessage 处理事件消息
func (p *Processor) handleEventMessage(ctx context.Context, deadline time.Time, msg *kefu.KFRecvMessage) error {
	if msg.Event == nil {
		return nil
	}

	event := msg.Event
	log.GetInstance().Sugar.Info("Received event ", event.EventType, " from ", msg.ExternalUserID)

	// 根据EventType处理不同事件
	switch event.EventType {
	case kefu.KFEventEnterSession:
		log.GetInstance().Sugar.Info("User ", event.ExternalUserID, " entered session, scene: ", event.Scene)
		// TODO: 发送欢迎消息

	case kefu.KFEventSessionStatusChange:
		log.GetInstance().Sugar.Info("Session status changed for ", event.ExternalUserID,
			", change_type: ", event.ChangeType)
		// TODO: 处理会话状态变更

	default:
		log.GetInstance().Sugar.Debug("Event type ", event.EventType, " not handled in processor")
	}

	return nil
}

// addUserMessage 添加用户消息到对话历史
func (p *Processor) addUserMessage(content string) error {
	p.historyMu.Lock()
	defer p.historyMu.Unlock()

	userMessage := minimax.Message{
		Role:    "user",
		Content: content,
		Name:    "用户", // 可选字段，有助于对话历史的可读性
	}

	rawMsg := minimax.RawMsg{
		Source:  "用户",
		Content: content,
	}

	p.conversationHistory = append(p.conversationHistory, userMessage)
	p.rawConverHistory = append(p.rawConverHistory, rawMsg)
	log.GetInstance().Sugar.Debug("Added user message to history for user ", p.userID, ", total messages: ", len(p.conversationHistory))

	return nil
}

// addAssistantMessage 添加AI回复到对话历史
func (p *Processor) addAssistantMessage(content string, isRaw bool) error {
	p.historyMu.Lock()
	defer p.historyMu.Unlock()

	filtered := p.filterContentForUser(content)
	cfg := config.GetInstance()
	assistantMessage := minimax.Message{
		Role:    "assistant",
		Content: filtered,
		Name:    cfg.MiniMaxConfig.SystemRoleName, // 使用配置的角色名（如"小胖"），保持身份一致性
	}

	rawMsg := minimax.RawMsg{
		Source:  "AI",
		Content: filtered,
	}

	p.conversationHistory = append(p.conversationHistory, assistantMessage)
	if isRaw {
		p.rawConverHistory = append(p.rawConverHistory, rawMsg)
	}
	log.GetInstance().Sugar.Debug("Added assistant message to history for user ", p.userID, ", total messages: ", len(p.conversationHistory))

	return nil
}

// clearConversationHistory 清理对话历史
// 当用户完成一个完整的房源咨询后（通过Function Call结束），清理历史记录
// 这样下次用户咨询不同区域的房产时，不会被旧的上下文干扰
func (p *Processor) clearConversationHistory() {
	p.historyMu.Lock()
	defer p.historyMu.Unlock()

	oldCount := len(p.conversationHistory)
	p.conversationHistory = make([]minimax.Message, 0)

	log.GetInstance().Sugar.Info("Cleared conversation history for user ", p.userID,
		", removed ", oldCount, " messages")
}

// callMiniMaxAPI 调用MiniMax API获取智能回复
func (p *Processor) callMiniMaxAPI(ctx context.Context) (*minimax.RealEstateConsultResponse, error) {
	p.historyMu.RLock()
	// 复制一份历史记录，避免在API调用期间阻塞其他操作
	fullHistory := make([]minimax.Message, len(p.conversationHistory))
	copy(fullHistory, p.conversationHistory)
	p.historyMu.RUnlock()

	// 根据配置决定是否压缩历史
	cfg := config.GetInstance()
	var historySnapshot []minimax.Message

	if cfg.ScheduleConfig.CompactHistory && len(fullHistory) > cfg.ScheduleConfig.MaxHistoryLength {
		// 压缩模式：截断到配置的最大长度
		historySnapshot = fullHistory[len(fullHistory)-cfg.ScheduleConfig.MaxHistoryLength:]
		log.GetInstance().Sugar.Info("Compact mode enabled: truncated history for user ", p.userID,
			" from ", len(fullHistory), " to ", len(historySnapshot), " messages")
	} else {
		// 完整模式：使用全部历史
		historySnapshot = fullHistory
		if len(fullHistory) > cfg.ScheduleConfig.MaxHistoryLength {
			log.GetInstance().Sugar.Debug("Compact mode disabled: using full history of ",
				len(fullHistory), " messages for user ", p.userID)
		}
	}

	log.GetInstance().Sugar.Info("Calling MiniMax API for user ", p.userID, " with ", len(historySnapshot), " messages")

	// 调用房源咨询API
	result, err := p.minimaxClient.RealEstateConsultWithFunctionCall(ctx, historySnapshot)
	if err != nil {
		return nil, fmt.Errorf("RealEstateConsultWithFunctionCall failed: %w", err)
	}

	log.GetInstance().Sugar.Info("MiniMax API call successful for user ", p.userID)
	return result, nil
}

// extractResponseContent 提取Response部分，避免Thought/Action污染对话历史
func (p *Processor) extractResponseContent(fullContent string) string {
	// 第一步：移除JSON代码块（```json ... ```格式）
	// 这些是Function Calling的内部数据，不应该发送给用户
	cleanContent := p.removeJSONCodeBlocks(fullContent)

	// 第二步：查找Response:标记，使用LastIndex避免AI思考过程中出现Response:干扰
	responsePrefix := "Response:"
	responseIndex := strings.LastIndex(cleanContent, responsePrefix)

	if responseIndex == -1 {
		// 如果没找到Response:标记，可能是新格式或异常情况
		// 但仍需要清理JSON内容
		return cleanContent
	}

	// 提取Response:后面的内容
	responseContent := strings.TrimSpace(cleanContent[responseIndex+len(responsePrefix):])

	// 如果提取为空，返回清理后的内容
	if responseContent == "" {
		return cleanContent
	}

	return responseContent
}

// removeJSONCodeBlocks 移除内容中的JSON代码块
func (p *Processor) removeJSONCodeBlocks(content string) string {
	// 正则匹配 ```json ... ``` 格式的代码块
	// 使用(?s)让.匹配包括换行符在内的所有字符
	jsonBlockPattern := regexp.MustCompile("(?s)```json.*?```")

	// 移除所有JSON代码块
	cleaned := jsonBlockPattern.ReplaceAllString(content, "")

	// 清理多余的空行
	lines := strings.Split(cleaned, "\n")
	var result []string
	emptyLineCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			emptyLineCount++
			// 最多保留一个空行
			if emptyLineCount <= 1 {
				result = append(result, line)
			}
		} else {
			emptyLineCount = 0
			result = append(result, line)
		}
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}

// ====== Pending 辅助：文本门禁与伪工具解析 ======

// wasLastAssistantAskingConfirm 粗判最近一条助手文本是否为位置切换确认语
func (p *Processor) wasLastAssistantAskingConfirm() bool {
	p.historyMu.RLock()
	defer p.historyMu.RUnlock()
	for i := len(p.conversationHistory) - 1; i >= 0; i-- {
		m := p.conversationHistory[i]
		if m.Role != "assistant" {
			continue
		}
		if c, ok := m.Content.(string); ok {
			if strings.Contains(c, "是否切换") {
				return true
			}
			// 英文/其他措辞兜底
			if strings.Contains(c, "切换到这里") || strings.Contains(c, "是否需要切换") {
				return true
			}
		}
		// 只看最近一条assistant
		break
	}
	return false
}

// maybeHandlePendingDecisionFromUser 在收到用户文本时，若存在pending，根据肯定/否定短语直接提交/丢弃
func (p *Processor) maybeHandlePendingDecisionFromUser(userText string) {
	txt := strings.TrimSpace(userText)
	if txt == "" {
		return
	}

	p.keywordsMu.Lock()
	hasPending := (p.pendingLoc != nil)
	p.keywordsMu.Unlock()
	if !hasPending {
		return
	}

	lower := strings.ToLower(txt)

	// 仅在最近一条助手消息为“确认切换”或用户明确出现关键动词时触发，避免误判
	trigger := p.wasLastAssistantAskingConfirm() ||
		strings.Contains(txt, "切换") || strings.Contains(txt, "换到") || strings.Contains(txt, "不沿用") ||
		strings.Contains(txt, "沿用") || strings.Contains(txt, "重置") || strings.Contains(txt, "清空")
	if !trigger {
		return
	}

	// 规则：
	// - 明确否定切换
	deny := strings.Contains(txt, "不用") || strings.Contains(txt, "不需要") || strings.Contains(txt, "不切换") || strings.Contains(txt, "不换") || strings.Contains(txt, "不要切换") || strings.Contains(txt, "保持原")
	if deny {
		p.keywordsMu.Lock()
		p.discardPending()
		p.keywordsMu.Unlock()
		log.GetInstance().Sugar.Info("User denied pending switch by text; discarded pending")
		return
	}

	// - 明确要求重置/不沿用
	if strings.Contains(txt, "清空") || strings.Contains(txt, "重置") || strings.Contains(txt, "重来") || strings.Contains(txt, "重新设置") || strings.Contains(txt, "不沿用") || strings.Contains(txt, "不保留") {
		p.keywordsMu.Lock()
		p.pendingResetNonLocation = true
		// 使用pending中的区/板块
		p.commitPending("", "")
		p.keywordsMu.Unlock()
		log.GetInstance().Sugar.Info("User accepted pending switch with reset=true by text gate")
		return
	}

	// - 明确沿用/保持不变
	if strings.Contains(txt, "沿用") || strings.Contains(txt, "不变") || strings.Contains(txt, "保持不变") || strings.Contains(txt, "按之前") || strings.Contains(txt, "按原") || strings.Contains(lower, "ok") {
		p.keywordsMu.Lock()
		p.pendingResetNonLocation = false
		p.commitPending("", "")
		p.keywordsMu.Unlock()
		log.GetInstance().Sugar.Info("User accepted pending switch with reset=false by text gate")
		return
	}
}

// maybeApplyPseudoToolCallsFromText 解析助手文本中的“[调用confirm_location_change: …]”伪标记并应用
func (p *Processor) maybeApplyPseudoToolCallsFromText(content string) {
	// 快速筛查
	if !strings.Contains(content, "confirm_location_change") {
		return
	}
	// 抽取方括号段
	bracketRe := regexp.MustCompile(`\[调用\s*confirm_location_change[^\]]*]`)
	loc := bracketRe.FindString(content)
	if loc == "" {
		return
	}
	// 提取 district / plate / reset_non_location
	var district, plate string
	var reset bool

	// 兼容 "xxx" 或 \"xxx\" 两种样式
	if m := regexp.MustCompile(`district\s*:\s*"([^"]+)"`).FindStringSubmatch(loc); len(m) == 2 {
		district = m[1]
	} else if m := regexp.MustCompile(`district\s*:\s*\\"([^\\"]+)\\"`).FindStringSubmatch(loc); len(m) == 2 {
		district = m[1]
	}
	if m := regexp.MustCompile(`plate\s*:\s*"([^"]+)"`).FindStringSubmatch(loc); len(m) == 2 {
		plate = m[1]
	} else if m := regexp.MustCompile(`plate\s*:\s*\\"([^\\"]+)\\"`).FindStringSubmatch(loc); len(m) == 2 {
		plate = m[1]
	}
	if m := regexp.MustCompile(`reset_non_location\s*:\s*(true|false)`).FindStringSubmatch(loc); len(m) == 2 {
		reset = (m[1] == "true")
	}

	// 应用
	p.keywordsMu.Lock()
	// 若没有pending，也补建一个pending，避免漏掉模型的“文本式确认”
	if p.pendingLoc == nil && (strings.TrimSpace(district) != "" || strings.TrimSpace(plate) != "") {
		p.beginPending(district, plate)
		log.GetInstance().Sugar.Infof("Pseudo confirm found without pending; created synthetic pending [%s/%s]", district, plate)
	}
	if p.pendingLoc != nil {
		p.pendingResetNonLocation = reset
		p.commitPending(district, plate)
		log.GetInstance().Sugar.Infof("Applied pseudo confirm_location_change from text: district=%s, plate=%s, reset=%v", district, plate, reset)
	}
	p.keywordsMu.Unlock()
}

// newReplyMessage 创建回复消息的通用辅助函数，消除代码重复
func (p *Processor) newReplyMessage(originalMsg *kefu.KFRecvMessage, msgID string) (*kefu.KFTextMessage, error) {
	cfg := config.GetInstance()
	platform := common.Platform(cfg.Platform)

	token, err := tokenstore.Instance().FetchCorpToken(
		platform,
		cfg.SuiteConfig.SuiteId,
		cfg.SuiteConfig.SuiteId,
		"",
		cfg.SuiteConfig.Secret,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch token failed: %w", err)
	}

	replyMsg := &kefu.KFTextMessage{
		KFMessage: kefu.KFMessage{
			ToUser:    originalMsg.ExternalUserID,
			OpenKFID:  originalMsg.OpenKFID,
			MsgID:     msgID,
			MsgType:   "text",
			CorpToken: token,
		},
	}
	return replyMsg, nil
}

// filterContentForUser 过滤所有不应该返回给用户的内容
// 包括：
// 1. Markdown语法（微信/企微不支持）
// 2. 其他未来可能的内部标记
func (p *Processor) filterContentForUser(content string) string {
	// 移除Markdown语法
	content = p.stripMarkdown(content)

	// 移除显式的工具调用注释块（如：[调用 extract_keywords: ...] / [调用 end_conversation: ...]）
	content = p.removeToolCallAnnotations(content)

	return content
}

// stripMarkdown 移除文本中的Markdown语法标记
// 专门为微信/企微等不支持Markdown的聊天场景设计
func (p *Processor) stripMarkdown(content string) string {
	// 移除粗体标记 **text** 或 __text__
	content = regexp.MustCompile(`\*\*(.*?)\*\*`).ReplaceAllString(content, "$1")
	content = regexp.MustCompile(`__(.*?)__`).ReplaceAllString(content, "$1")

	// 移除斜体标记 *text* 或 _text_（注意避免影响乘号）
	content = regexp.MustCompile(`(?:^|\s)\*([^\*\s][^\*]*?)\*(?:$|\s)`).ReplaceAllString(content, " $1 ")
	content = regexp.MustCompile(`(?:^|\s)_([^_\s][^_]*?)_(?:$|\s)`).ReplaceAllString(content, " $1 ")

	// 移除行内代码标记 `code`
	content = regexp.MustCompile("`([^`]+)`").ReplaceAllString(content, "$1")

	// 移除列表标记 - 或 * 开头（保留内容）
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			lines[i] = strings.TrimPrefix(trimmed, "- ")
		} else if strings.HasPrefix(trimmed, "* ") {
			lines[i] = strings.TrimPrefix(trimmed, "* ")
		} else if matched, _ := regexp.MatchString(`^\d+\.\s`, trimmed); matched {
			// 移除数字列表标记 1. 2. 3. 等
			lines[i] = regexp.MustCompile(`^\d+\.\s`).ReplaceAllString(trimmed, "")
		}
	}
	content = strings.Join(lines, "\n")

	// 移除标题标记 # ## ### 等
	content = regexp.MustCompile(`(?m)^#{1,6}\s+(.*)$`).ReplaceAllString(content, "$1")

	// 移除链接语法 [text](url) -> text
	content = regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`).ReplaceAllString(content, "$1")

	// 移除图片语法 ![alt](url) -> alt
	content = regexp.MustCompile(`!\[([^\]]*)\]\([^\)]+\)`).ReplaceAllString(content, "$1")

	// 清理多余的空格和空行
	content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
	content = strings.TrimSpace(content)

	return content
}

// removeToolCallAnnotations 清理模型可能输出的“工具调用预览/注释”片段，防止暴露内部结构
// 规则：
// - 如果行以"[调用 "开头，则进入跳过模式；
// - 跳过后续看起来是"键: 值"结构的行（包含冒号的结构化行），直到遇到空行或不含":"的自然语言行；
// - 该逻辑只影响助手输出，不处理用户文本
func (p *Processor) removeToolCallAnnotations(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	// 兼容有/无空格写法与单行长内容，允许中间包含多个 ']'（数组/列表）
	// 起始多行形态：[调用 extract_keywords:
	reStart := regexp.MustCompile(`^\s*\[调用\s*[a-zA-Z_]+\s*:\s*$`)
	// 单行形态：[调用extract_keywords: ...]
	reSingle := regexp.MustCompile(`^\s*\[调用[^\n]*\]\s*$`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 单行预览，直接丢弃
		if reSingle.MatchString(trimmed) {
			continue
		}

		if !skipping && reStart.MatchString(trimmed) {
			skipping = true
			continue
		}

		if skipping {
			// 结束条件：空行 或 独立的"]" 行 或 非结构化（不含":"）行
			if trimmed == "" || trimmed == "]" || !strings.Contains(trimmed, ":") {
				skipping = false
				if trimmed == "" || trimmed == "]" {
					continue
				}
				// 非结构化行落回正常输出
			} else {
				// 结构化行继续跳过
				continue
			}
		}

		out = append(out, line)
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

// sendAIReply 发送AI回复给用户
func (p *Processor) sendAIReply(ctx context.Context, originalMsg *kefu.KFRecvMessage, content string) error {
	replyMsg, err := p.newReplyMessage(originalMsg, fmt.Sprintf("ai_reply_%d", utils.Now().Unix()))
	if err != nil {
		return err
	}

	// 过滤所有不能返回给用户的内容（Markdown等）
	replyMsg.Text.Content = p.filterContentForUser(content)

	if _, err := kefu.SendKFText(replyMsg); err != nil {
		return fmt.Errorf("send AI reply failed: %w", err)
	}

	log.GetInstance().Sugar.Info("AI reply sent to ", originalMsg.ExternalUserID)
	reportKefuMessage(replyMsg.MsgType)
	return nil
}

// sendFallbackReply 发送备用回复（仅在API调用失败时使用）
func (p *Processor) sendFallbackReply(ctx context.Context, msg *kefu.KFRecvMessage) error {
	replyMsg, err := p.newReplyMessage(msg, fmt.Sprintf("fallback_%d", utils.Now().Unix()))
	if err != nil {
		return err
	}

	replyMsg.Text.Content = "抱歉，智能助手暂时开小差了，请稍后再试。"

	if _, err := kefu.SendKFText(replyMsg); err != nil {
		return fmt.Errorf("send fallback reply failed: %w", err)
	}

	log.GetInstance().Sugar.Info("Fallback reply sent to ", msg.ExternalUserID)
	reportKefuMessage(replyMsg.MsgType)
	return nil
}

// logConversationEnd 记录对话结束信息
func (p *Processor) logConversationEnd(endCallData map[string]interface{}) {
	if endCallData == nil {
		log.GetInstance().Sugar.Info("Conversation ended for user ", p.userID, " without end call data")
		return
	}

	log.GetInstance().Sugar.Info("Conversation ended for user ", p.userID, " with collected data:")

	// 记录收集到的房源信息
	if summary, ok := endCallData["consultation_summary"].(string); ok {
		log.GetInstance().Sugar.Info("  - 咨询总结: ", summary)
	}
	if intent, ok := endCallData["user_intent"].(string); ok {
		log.GetInstance().Sugar.Info("  - 用户意图: ", intent)
	}
	if location, ok := endCallData["location"].(string); ok {
		log.GetInstance().Sugar.Info("  - 地点: ", location)
	}
	if budget, ok := endCallData["budget"].(string); ok {
		log.GetInstance().Sugar.Info("  - 预算: ", budget)
	}
	if houseType, ok := endCallData["house_type"].(string); ok {
		log.GetInstance().Sugar.Info("  - 户型: ", houseType)
	}

	log.GetInstance().Sugar.Info("对话结束，已为用户 ", p.userID, " 收集完整房源信息")
}

// sendToFurtherProcess 发送任务到后续处理channel
func (p *Processor) sendToFurtherProcess() {
	// 获取对话历史的副本
	p.historyMu.RLock()
	historyCopy := make([]minimax.Message, len(p.conversationHistory))
	copy(historyCopy, p.conversationHistory)
	rawHistoryCopy := make([]minimax.RawMsg, len(p.rawConverHistory))
	copy(rawHistoryCopy, p.rawConverHistory)
	p.historyMu.RUnlock()

	// 如果没有对话历史，直接返回
	if len(historyCopy) == 0 {
		log.GetInstance().Sugar.Info("No conversation history to send for further processing for user ", p.userID)
		return
	}

	// 生成有效快照（若存在pending，将暂存的非location维度合并入当前记录）
	p.keywordsMu.RLock()
	hasPending := (p.pendingLoc != nil)
	p.keywordsMu.RUnlock()
	keywordsCopy := p.buildEffectiveKeywordsSnapshot()
	log.GetInstance().Sugar.Infof("Built effective keywords snapshot (pending=%v), dimensions=%d", hasPending, len(keywordsCopy))

	// 创建任务
	task := &FurtherProcessTask{
		UserID:              p.userID,
		OpenKFID:            p.currentOpenKFID,
		ConversationHistory: historyCopy,
		RawConverHistory:    rawHistoryCopy,
		Keywords:            keywordsCopy, // 传递合并后的有效关键词
	}

	// 发送到channel（非阻塞）
	select {
	case p.scheduler.furtherProcess <- task:
		log.GetInstance().Sugar.Info("Sent conversation history and keywords to further process for user ", p.userID)
	default:
		log.GetInstance().Sugar.Warn("Further process channel is full for user ", p.userID)
	}
}

// ===== 并发关键词提取实现 =====

// initKeywordsExtraction 初始化关键词提取
func (p *Processor) initKeywordsExtraction() {
	// 创建worker context
	ctx, cancel := context.WithCancel(p.ctx)
	p.keywordsWorkerCtx = ctx
	p.keywordsWorkerCancel = cancel

	// 启动worker协程
	p.scheduler.wg.Add(1)
	go p.keywordsExtractionWorker()
}

// keywordsExtractionWorker 关键词提取worker
// 单个worker顺序处理，避免无限goroutine创建
func (p *Processor) keywordsExtractionWorker() {
	// 关键安全改进：添加panic recovery防止worker崩溃导致整个服务停止
	defer func() {
		if r := recover(); r != nil {
			log.GetInstance().Sugar.Errorf("Panic recovered in keywordsExtractionWorker for user %s: %v", p.userID, r)
		}
		p.scheduler.wg.Done()
		log.GetInstance().Sugar.Info("Keywords extraction worker stopped for user: ", p.userID)
	}()

	log.GetInstance().Sugar.Info("Keywords extraction worker started for user: ", p.userID)

	for {
		select {
		case <-p.keywordsWorkerCtx.Done():
			return
		case msg, ok := <-p.keywordsChan:
			if !ok {
				// Channel已关闭，worker退出
				return
			}
			// 顺序处理消息，避免并发问题
			p.extractAndMergeKeywords(msg)
		}
	}
}

// extractAndMergeKeywords 提取并合并关键词
func (p *Processor) extractAndMergeKeywords(message string) {
	// 调用keywords库提取
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := p.keywordExtractor.extractor.ExtractFromMessageWithContext(ctx, message)
	if err != nil {
		if strings.Contains(err.Error(), "deadline exceeded") {
			log.GetInstance().Sugar.Warnf("Keywords extraction timeout for message: %.50s...", message)
		} else {
			log.GetInstance().Sugar.Warnf("Keywords extraction failed: %v", err)
		}
		return
	}

	// 合并到map（加锁）
	p.keywordsMu.Lock()
	defer p.keywordsMu.Unlock()

	p.deepMergeKeywordsToMap(result.Fields)

	log.GetInstance().Sugar.Debugf("Extracted keywords for user %s: %+v", p.userID, result.Fields)
}

// deepMergeKeywordsToMap 深度合并关键词到map
// 实现维度特定的合并策略
func (p *Processor) deepMergeKeywordsToMap(fields map[string]interface{}) {
	if fields == nil {
		return
	}

	// 1) 先处理location，决定是否进入待确认阶段
	if rawLoc, ok := fields["location"]; ok && rawLoc != nil {
		province, district, plate, landmark := p.parseLocationValue(rawLoc)
		districtNorm := p.normalizeDistrict(district)
		plate = strings.TrimSpace(plate)

		// 若提供了plate，优先用板块反查区，修正不一致的district，避免误触发跨区pending
		if plate != "" {
			if ok, d := ikeywords.IsShanghaiPlate(plate); ok {
				if districtNorm == "" || districtNorm != d {
					log.GetInstance().Sugar.Infof("Correcting district by plate mapping: plate='%s' implies district='%s' (was '%s')", plate, d, districtNorm)
					districtNorm = d
				}
			}
		}

		// 读取当前上下文
		currDistrict := p.finalLoc.District
		currPlate := p.getCurrentPlate()

		// 判定：细化 vs 切换 vs 跨区
		sameDistrict := (strings.TrimSpace(currDistrict) != "" && districtNorm == currDistrict)
		isFirstLoc := (strings.TrimSpace(currDistrict) == "")
		onlyRefinePlate := (!isFirstLoc && sameDistrict && strings.TrimSpace(currPlate) == "" && plate != "")
		plateUnchanged := (plate != "" && plate == currPlate)
		districtChanged := (districtNorm != "" && districtNorm != currDistrict)
		plateChanged := (plate != "" && currPlate != "" && plate != currPlate)

		switch {
		case isFirstLoc:
			// 首次定位：若仅板块也允许，通过映射补区后直接采纳，不进入pending
			if strings.TrimSpace(districtNorm) == "" && strings.TrimSpace(plate) != "" {
				if ok, d := ikeywords.IsShanghaiPlate(plate); ok {
					districtNorm = d
				}
			}
			log.GetInstance().Sugar.Infof("Auto-commit first location: new[%s/%s] (no confirmation needed)", districtNorm, plate)
			p.beginPending(districtNorm, plate)
			p.commitPending(districtNorm, plate)

		case onlyRefinePlate:
			// 区→板块细化：直接合并到当前记录，不进入pending
			log.GetInstance().Sugar.Infof("Refine plate under same district without pending: current[%s/%s] -> refine[%s/%s]",
				currDistrict, currPlate, districtNorm, plate)
			p.mergeLocationIntoRecord(p.currentKeywords, province, districtNorm, plate, landmark)

		case plateUnchanged && !districtChanged:
			// 未变化：直接合并（可能只是补充landmark等）
			p.mergeLocationIntoRecord(p.currentKeywords, province, districtNorm, plate, landmark)

		case districtChanged:
			// 跨区：进入pending
			log.GetInstance().Sugar.Infof("Detected location change (district): current[%s/%s] -> new[%s/%s]; entering pending",
				currDistrict, currPlate, districtNorm, plate)
			p.beginPending(districtNorm, plate)

		case plateChanged:
			// 同区板块切换：进入pending
			log.GetInstance().Sugar.Infof("Detected plate switch within district: current[%s/%s] -> new[%s/%s]; entering pending",
				currDistrict, currPlate, districtNorm, plate)
			p.beginPending(districtNorm, plate)

		default:
			// 其他情况（例如只补区名与当前一致且当前已有plate为空等）
			p.mergeLocationIntoRecord(p.currentKeywords, province, districtNorm, plate, landmark)
		}

		// 移除location，剩余维度走常规路由
		delete(fields, "location")
	}

	// 2) 决定目标map：待确认阶段写入stagedNonLocation，否则写入当前记录
	var target map[string]interface{}
	if p.pendingLoc != nil {
		if p.stagedNonLocation == nil {
			p.stagedNonLocation = make(map[string]interface{})
		}
		target = p.stagedNonLocation
		log.GetInstance().Sugar.Debug("Merging fields into stagedNonLocation due to pendingLoc")
	} else {
		target = p.currentKeywords
		log.GetInstance().Sugar.Debug("Merging fields into currentKeywords")
	}

	// 3) 合并剩余维度到目标map
	p.mergeFieldsToTarget(target, fields)
}

// mergeFieldsToTarget 将字段集合合并到指定的目标map
func (p *Processor) mergeFieldsToTarget(target map[string]interface{}, fields map[string]interface{}) {
	for key, value := range fields {
		if value == nil || value == "" {
			continue
		}
		existing, exists := target[key]
		switch key {
		case "price", "rent_price":
			target[key] = p.mergePriceData(existing, value)
		case "area":
			target[key] = p.mergeAreaData(existing, value)
		case "room_layout":
			target[key] = p.mergeRoomLayoutData(existing, value)
		case "decoration", "property_type", "orientation", "interest_points", "commercial":
			target[key] = p.mergeStringArrayData(existing, value)
		default:
			if !exists {
				target[key] = value
			}
		}
	}
}

// ensureRecord 确保指定位置下的记录存在，并返回其引用
func (p *Processor) ensureRecord(loc LocationKey) map[string]interface{} {
	if p.keywordsByLoc == nil {
		p.keywordsByLoc = make(map[string]map[string]map[string]interface{})
	}
	if _, ok := p.keywordsByLoc[loc.City]; !ok {
		p.keywordsByLoc[loc.City] = make(map[string]map[string]interface{})
	}
	if _, ok := p.keywordsByLoc[loc.City][loc.District]; !ok {
		p.keywordsByLoc[loc.City][loc.District] = make(map[string]interface{})
	}
	return p.keywordsByLoc[loc.City][loc.District]
}

// getCurrentPlate 获取当前记录中的plate
func (p *Processor) getCurrentPlate() string {
	if p.currentKeywords == nil {
		return ""
	}
	if lm, ok := p.currentKeywords["location"].(map[string]interface{}); ok {
		if s, ok := lm["plate"].(string); ok {
			return s
		}
	}
	return ""
}

// mergeLocationIntoRecord 将location字段合并到指定记录（最新覆盖）
func (p *Processor) mergeLocationIntoRecord(record map[string]interface{}, province, district, plate, landmark string) {
	if record == nil {
		return
	}
	lm := map[string]interface{}{}
	if v, ok := record["location"].(map[string]interface{}); ok {
		// 复制以避免共享引用副作用
		for k, val := range v {
			lm[k] = val
		}
	}
	if strings.TrimSpace(province) != "" {
		lm["province"] = "上海" // 存储层统一用“上海”
	}
	if strings.TrimSpace(district) != "" {
		lm["district"] = district
	}
	if strings.TrimSpace(plate) != "" {
		lm["plate"] = plate
	}
	if strings.TrimSpace(landmark) != "" {
		lm["landmark"] = landmark
	}
	record["location"] = lm
}

// beginPending 进入待确认阶段
func (p *Processor) beginPending(district, plate string) {
	p.pendingLoc = &PendingLoc{City: "上海市", District: district, Plate: plate}
	// 重置暂存
	p.stagedNonLocation = make(map[string]interface{})
	// 默认保守：不重置非location维度，除非用户明确要求
	p.pendingResetNonLocation = false
	// 对于新一次pending，重置已拦截标记
	p.pendingEndIntercepted = false
	log.GetInstance().Sugar.Infof("Pending location change started: current[%s/%s] -> pending[%s/%s]",
		p.finalLoc.District, p.getCurrentPlate(), district, plate)
}

// commitPending 提交位置变更，并合并暂存的非位置维度
func (p *Processor) commitPending(confirmDistrict, confirmPlate string) {
	if p.pendingLoc == nil {
		return
	}
	// 优先使用函数返回，其次使用pending中的值
	newDistrict := p.normalizeDistrict(firstNonEmpty(confirmDistrict, p.pendingLoc.District))
	newPlate := strings.TrimSpace(firstNonEmpty(confirmPlate, p.pendingLoc.Plate))

	log.GetInstance().Sugar.Infof("Committing location change: confirm[district=%s, plate=%s], current[%s/%s]",
		newDistrict, newPlate, p.finalLoc.District, p.getCurrentPlate())

	// 目标位置与记录：如果district明确非空，则切换到新district；否则仍在当前finalLoc下更新plate
	var newLoc LocationKey
	if newDistrict != "" {
		newLoc = LocationKey{City: "上海市", District: newDistrict}
	} else {
		newLoc = p.finalLoc
	}

	newRec := p.ensureRecord(newLoc)

	// 板块变化根据策略决定是否清空非location维度（默认为保留，只有在用户明确要求reset时清空）
	p.applyPlateChangeClearIfNeeded(newRec, newPlate, p.pendingResetNonLocation)

	// 写入location：district 仅在非空时覆盖；plate 非空则覆盖
	p.mergeLocationIntoRecord(newRec, "上海", newDistrict, newPlate, "")

	// 迁移非location维度
	if p.currentKeywords != nil {
		// 1) 从未知区首次定位到具体区：迁移旧记录的非location维度
		if p.finalLoc.District == "" && newLoc.District != "" {
			p.mergeFieldsToTarget(newRec, cloneWithoutLocation(p.currentKeywords))
		}
		// 2) 跨区切换且未要求重置：从旧区迁移非location维度到新区
		if newDistrict != "" && newDistrict != p.finalLoc.District && !p.pendingResetNonLocation {
			p.mergeFieldsToTarget(newRec, cloneWithoutLocation(p.currentKeywords))
			log.GetInstance().Sugar.Infof("Cross-district switch with reset=false: migrated non-location fields from [%s] to [%s]", p.finalLoc.District, newDistrict)
		}
	}

	// 合并暂存的非位置维度
	if len(p.stagedNonLocation) > 0 {
		p.mergeFieldsToTarget(newRec, p.stagedNonLocation)
	}

	// 切换指针与状态
	if newDistrict != "" {
		p.finalLoc = newLoc
	}
	p.currentKeywords = newRec
	p.keywordsMap = p.currentKeywords // 兼容旧读取路径

	// 清理pending
	p.pendingLoc = nil
	p.stagedNonLocation = make(map[string]interface{})
	p.pendingResetNonLocation = false
	p.pendingEndIntercepted = false

	log.GetInstance().Sugar.Infof("Location change committed: finalLoc -> [%s], plate -> [%s]",
		p.finalLoc.District, p.getCurrentPlate())
}

// discardPending 放弃位置切换
func (p *Processor) discardPending() {
	// 将暂存的非location维度合并回当前记录，避免用户有效信息丢失
	if len(p.stagedNonLocation) > 0 {
		p.mergeFieldsToTarget(p.currentKeywords, p.stagedNonLocation)
		log.GetInstance().Sugar.Infof("Discarded pending location; merged %d staged non-location fields back to current record",
			len(p.stagedNonLocation))
	} else {
		log.GetInstance().Sugar.Infof("Pending location change discarded by user; keep current[%s/%s]",
			p.finalLoc.District, p.getCurrentPlate())
	}
	p.pendingLoc = nil
	p.stagedNonLocation = make(map[string]interface{})
	p.pendingResetNonLocation = false
	p.pendingEndIntercepted = false
}

// applyPlateChangeClearIfNeeded 如果plate变化，根据reset策略决定是否清空非location维度
func (p *Processor) applyPlateChangeClearIfNeeded(record map[string]interface{}, newPlate string, reset bool) {
	if record == nil || strings.TrimSpace(newPlate) == "" {
		return
	}
	var oldPlate string
	if lm, ok := record["location"].(map[string]interface{}); ok {
		if s, ok := lm["plate"].(string); ok {
			oldPlate = s
		}
	}
	if oldPlate != "" && oldPlate != newPlate {
		if reset {
			// 清空非location维度
			for _, k := range []string{"decoration", "property_type", "orientation", "interest_points", "commercial", "room_layout", "price", "rent_price", "area"} {
				delete(record, k)
			}
			log.GetInstance().Sugar.Infof("Plate changed: %s -> %s; reset=true -> cleared non-location dimensions", oldPlate, newPlate)
		} else {
			// 保守：不清空，沿用之前的维度
			log.GetInstance().Sugar.Infof("Plate changed: %s -> %s; reset=false -> keep non-location dimensions (conservative)", oldPlate, newPlate)
		}
	}
}

// handleConfirmLocationToolCalls 解析并处理位置确认工具调用
func (p *Processor) handleConfirmLocationToolCalls(calls []minimax.ToolCall) {
	if len(calls) == 0 {
		return
	}
	for _, tc := range calls {
		if tc.Function.Name != "confirm_location_change" {
			continue
		}
		// 解析参数
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			log.GetInstance().Sugar.Warnf("Failed to parse confirm_location_change args: %v", err)
			continue
		}
		district, _ := args["district"].(string)
		plate, _ := args["plate"].(string)
		// 读取是否要求清空非location维度（默认false）；不在schema也兼容
		reset, _ := args["reset_non_location"].(bool)
		log.GetInstance().Sugar.Infof("confirm_location_change received: district=%s, plate=%s, reset_non_location=%v", district, plate, reset)

		p.keywordsMu.Lock()
		// 记录策略
		p.pendingResetNonLocation = reset
		// 若当前没有pending（模型直接下发confirm），为确保不丢指令，这里补建pending再提交
		if p.pendingLoc == nil && (strings.TrimSpace(district) != "" || strings.TrimSpace(plate) != "") {
			p.beginPending(district, plate)
			log.GetInstance().Sugar.Infof("No pending when confirm received; created synthetic pending [%s/%s] before commit", district, plate)
		}
		if strings.TrimSpace(district) == "" && strings.TrimSpace(plate) == "" {
			// 否认切换
			p.discardPending()
			p.keywordsMu.Unlock()
			return
		}

		// 若只有plate，尝试反查district
		if strings.TrimSpace(district) == "" && strings.TrimSpace(plate) != "" {
			if ok, d := ikeywords.IsShanghaiPlate(plate); ok {
				district = d
				log.GetInstance().Sugar.Infof("Backfilled district by plate '%s' -> '%s'", plate, district)
			}
		}

		p.commitPending(district, plate)
		p.keywordsMu.Unlock()
		return
	}
}

// handleClassifyPurchaseIntentToolCalls 解析并处理购房意向分类工具调用
func (p *Processor) handleClassifyPurchaseIntentToolCalls(calls []minimax.ToolCall) {
	if len(calls) == 0 {
		return
	}
	for _, tc := range calls {
		if tc.Function.Name != "classify_purchase_intent" {
			continue
		}
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			log.GetInstance().Sugar.Warnf("Failed to parse classify_purchase_intent args: %v", err)
			continue
		}
		intentStr, _ := args["intent"].(string)
		toEnum := func(v string) PurchaseIntent {
			switch strings.TrimSpace(v) {
			case "新房":
				return IntentNew
			case "二手房":
				return IntentSecond
			default:
				return IntentAll
			}
		}
		intent := toEnum(intentStr)
		p.setPurchaseIntent(intent)
		log.GetInstance().Sugar.Infof("classify_purchase_intent received: intent=%s (enum=%d)", intentStr, intent)
		return
	}
}

// ===== 通用Tool调度器 =====

type toolMode int

const (
	toolModeMainFlow toolMode = iota
	toolModeRepollFlow
)

// handleToolCalls 统一处理各种工具调用；根据模式在轮询通道做保守处理
func (p *Processor) handleToolCalls(calls []minimax.ToolCall, mode toolMode) {
	if len(calls) == 0 {
		return
	}
	for _, tc := range calls {
		log.GetInstance().Sugar.Infof("Tool dispatch: name=%s, mode=%d", tc.Function.Name, mode)
		switch tc.Function.Name {
		case "extract_keywords":
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				log.GetInstance().Sugar.Warnf("Failed to parse extract_keywords args: %v", err)
				continue
			}
			// deepMergeFromFunctionCall 内部已加锁，这里不要再二次加锁以免自锁
			p.deepMergeFromFunctionCall(args)
			log.GetInstance().Sugar.Info("Handled extract_keywords: merged into current staging/record")

		case "confirm_location_change":
			p.handleConfirmLocationToolCalls([]minimax.ToolCall{tc})

		case "classify_purchase_intent":
			p.handleClassifyPurchaseIntentToolCalls([]minimax.ToolCall{tc})

		case "end_conversation":
			// 轮询通道不执行结束动作，主流程已有统一处理
			if mode == toolModeRepollFlow {
				log.GetInstance().Sugar.Debug("Repoll flow received end_conversation; ignored here")
			} else {
				// 主流程收到结束建议，实际结束由上层逻辑统一处理（含pending拦截）
				log.GetInstance().Sugar.Debug("Main flow received end_conversation; defer to end handling")
			}

		default:
			log.GetInstance().Sugar.Warnf("Unhandled tool call: %s", tc.Function.Name)
		}
	}
}

// parseLocationValue 从value解析出location字段
func (p *Processor) parseLocationValue(value interface{}) (province, district, plate, landmark string) {
	// 兼容数组/对象
	if value == nil {
		return
	}
	// 若为数组取最后一个
	if arr, ok := value.([]interface{}); ok {
		if len(arr) > 0 {
			return p.parseLocationValue(arr[len(arr)-1])
		}
		return
	}
	if m, ok := value.(map[string]interface{}); ok {
		if v, ok := m["province"].(string); ok {
			province = v
		}
		if v, ok := m["district"].(string); ok {
			district = v
		}
		if v, ok := m["plate"].(string); ok {
			plate = v
		}
		if v, ok := m["landmark"].(string); ok {
			landmark = v
		}
	}
	return
}

// normalizeDistrict 归一化区命名（浦东新区->浦东；去掉“区”后缀）
func (p *Processor) normalizeDistrict(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	if strings.HasSuffix(d, "新区") {
		d = strings.TrimSuffix(d, "新区")
	}
	if strings.HasSuffix(d, "区") {
		d = strings.TrimSuffix(d, "区")
	}
	return d
}

// cloneWithoutLocation 复制一个map并移除location键
func cloneWithoutLocation(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		if k == "location" {
			continue
		}
		dst[k] = v
	}
	return dst
}

// firstNonEmpty 返回第一个非空字符串
func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// ===== 快照与辅助 =====

// buildEffectiveKeywordsSnapshot 构建用于下游处理的有效关键词快照
// 若存在pending位置切换，保守不切换，将暂存的非location维度合并入当前记录视图
func (p *Processor) buildEffectiveKeywordsSnapshot() map[string]interface{} {
	p.keywordsMu.RLock()
	defer p.keywordsMu.RUnlock()

	base := cloneMapDeep(p.currentKeywords)
	if p.pendingLoc != nil && len(p.stagedNonLocation) > 0 {
		// 合并暂存的非location维度
		p.mergeFieldsToTarget(base, p.stagedNonLocation)
		log.GetInstance().Sugar.Infof("Effective snapshot merged %d staged fields due to pending location",
			len(p.stagedNonLocation))
	}
	return base
}

// cloneMapDeep 深拷贝 map[string]interface{}
func cloneMapDeep(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v interface{}) interface{} {
	switch vv := v.(type) {
	case map[string]interface{}:
		return cloneMapDeep(vv)
	case []interface{}:
		cp := make([]interface{}, len(vv))
		for i := range vv {
			cp[i] = deepCopyValue(vv[i])
		}
		return cp
	case []string:
		cp := make([]string, len(vv))
		copy(cp, vv)
		return cp
	default:
		return vv
	}
}

// buildPendingConfirmReply 构造一句确认切换的中文提示
func (p *Processor) buildPendingConfirmReply() string {
	p.keywordsMu.RLock()
	defer p.keywordsMu.RUnlock()
	if p.pendingLoc == nil {
		return "我注意到您提到了新的位置，是否需要切换并重新收集需求？"
	}
	// 优先展示板块；若同时有区与板块，展示“区/板块”以消除歧义
	var want string
	d := strings.TrimSpace(p.pendingLoc.District)
	pl := strings.TrimSpace(p.pendingLoc.Plate)
	switch {
	case pl != "" && d != "":
		want = d + "/" + pl
	case pl != "":
		want = pl
	case d != "":
		want = d
	}
	if want == "" {
		return "我注意到您提到了新的位置，是否需要切换并重新收集需求？"
	}
	return "我注意到您提到了“" + want + "”。是否切换到这里并重新收集购房需求？"
}

// ===== “思考中”占位与轮询 =====

// startThinkingWaiter 在AI仅返回tool_calls时启动轮询：
// - 0~3秒：最多2次快速重询；若仍无文本，发送一次占位消息（不入历史）
// - 3~10秒：后台再尝试1-2次；若仍无文本，发送buildProbingReply作为兜底（入历史）
func (p *Processor) startThinkingWaiter(ctx context.Context, msg *kefu.KFRecvMessage) {
	// 即使处于pending阶段也启动waiter，用于尽快获取一条自然语言确认/追问

	p.thinkWaiterMu.Lock()
	// 先取消上一个（如果还在跑）
	if p.thinkWaiterCancel != nil {
		p.thinkWaiterCancel()
		p.thinkWaiterCancel = nil
		p.thinkWaiterCtx = nil
		p.thinkWaiterRunning = false
	}
	// 为本轮创建可取消的上下文
	ctxWait, cancelWait := context.WithCancel(p.ctx)
	p.thinkWaiterCtx = ctxWait
	p.thinkWaiterCancel = cancelWait
	p.thinkWaiterRunning = true
	mySeq := p.turnSeq
	p.thinkWaiterMu.Unlock()

	go func() {
		defer func() {
			p.thinkWaiterMu.Lock()
			// 仅当仍指向当前上下文时重置状态（防并发覆盖）
			if p.thinkWaiterCtx == ctxWait {
				p.thinkWaiterRunning = false
				p.thinkWaiterCancel = nil
				p.thinkWaiterCtx = nil
			}
			p.thinkWaiterMu.Unlock()
		}()

		start := time.Now()
		log.GetInstance().Sugar.Infof("Thinking waiter started: seq=%d", mySeq)

		// 立即占位（不入历史，受冷却保护）
		if !p.isOutdatedSeq(mySeq) && time.Since(p.lastPlaceholderAt) > 30*time.Second {
			placeholder := "小胖正在思考中，很快给您回复哦…"
			if err := p.sendAIReply(p.ctx, msg, placeholder); err == nil {
				p.lastPlaceholderAt = time.Now()
				log.GetInstance().Sugar.Info("Immediate thinking placeholder sent to ", msg.ExternalUserID)
			} else {
				log.GetInstance().Sugar.Warnf("Failed to send immediate placeholder: %v", err)
			}
		}

		// 第一次重询：将单次AI请求超时提升到30秒
		if p.tryRepollForTextWithCtx(ctxWait, mySeq, msg, 1, 30*time.Second) {
			return
		}

		// 二次占位（若未发送或已过冷却）
		if !p.isOutdatedSeq(mySeq) && time.Since(p.lastPlaceholderAt) > 30*time.Second {
			placeholder := "我在梳理您的需求，马上回复您～"
			if err := p.sendAIReply(p.ctx, msg, placeholder); err == nil {
				p.lastPlaceholderAt = time.Now()
				log.GetInstance().Sugar.Info("Secondary thinking placeholder sent to ", msg.ExternalUserID)
			} else {
				log.GetInstance().Sugar.Warnf("Failed to send secondary placeholder: %v", err)
			}
		}

		// 继续后台重询最多2次，窗口不超过90秒
		deadline := start.Add(90 * time.Second)
		for i := 0; i < 2 && time.Now().Before(deadline); i++ {
			if p.tryRepollForTextWithCtx(ctxWait, mySeq, msg, 1, 30*time.Second) {
				return
			}
		}

		// 仍无文本：发送 probing 兜底（入历史）
		if !p.isOutdatedSeq(mySeq) {
			probing := p.buildProbingReply()
			if err := p.addAssistantMessage(probing, false); err == nil {
				_ = p.sendAIReply(p.ctx, msg, probing)
				log.GetInstance().Sugar.Info("Sent probing fallback after thinking window to ", msg.ExternalUserID)
			} else {
				log.GetInstance().Sugar.Warnf("Failed to add probing fallback to history: %v", err)
			}
		}
		log.GetInstance().Sugar.Infof("Thinking waiter finished: seq=%d", mySeq)
	}()
}

// tryRepollForTextWithCtx 进行若干次重询；interval 作为单次请求的超时，而非休眠时长
func (p *Processor) tryRepollForTextWithCtx(waitCtx context.Context, mySeq uint64, msg *kefu.KFRecvMessage, rounds int, interval time.Duration) bool {
	for i := 0; i < rounds; i++ {
		// 允许取消：若 waiter 被取消或轮次过期，直接退出
		select {
		case <-waitCtx.Done():
			return false
		default:
		}
		if p.isOutdatedSeq(mySeq) {
			return false
		}
		// 使用带超时的重询；第0轮：带nudge + tool_choice=auto；后续：tool_choice=none（强制文本）
		withNudge := (i == 0)
		toolChoice := "auto"
		if i > 0 {
			toolChoice = "none"
		}
		repollCtx, cancel := context.WithTimeout(waitCtx, interval)
		resp, err := p.repollCallMiniMax(repollCtx, withNudge, toolChoice)
		cancel()
		if err != nil {
			log.GetInstance().Sugar.Debugf("Repoll(%s/%v) failed: %v", toolChoice, withNudge, err)
		} else {
			// 先处理工具调用（若有）。若本轮已过期，避免用过期结果污染状态，直接跳过工具处理。
			if !p.isOutdatedSeq(mySeq) {
				if resp.RawResponse != nil && len(resp.RawResponse.Choices) > 0 && resp.RawResponse.Choices[0].Message != nil {
					tc := resp.RawResponse.Choices[0].Message.ToolCalls
					if len(tc) > 0 {
						p.handleToolCalls(tc, toolModeRepollFlow)
					}
				}
			} else {
				log.GetInstance().Sugar.Debug("Repoll result outdated; skip tool-call handling")
			}

			// 若AI判定结束且存在未拦截的pending，优先发起位置确认（不下发本次结束文本）
			if resp.IsConversationEnd {
				p.keywordsMu.Lock()
				hasPendingFirst := (p.pendingLoc != nil && !p.pendingEndIntercepted)
				p.keywordsMu.Unlock()
				if hasPendingFirst {
					if p.handleEndAttempt(p.ctx, msg, resp.EndCallData, resp.IsConversationEnd) {
						return true
					}
				}
			}
			// 再尝试抽取自然语言
			txt := strings.TrimSpace(p.extractResponseContent(resp.Content))
			log.GetInstance().Sugar.Debugf("Repoll(%s/%v) content_len=%d", toolChoice, withNudge, len(txt))
			if txt != "" {
				// 若本轮已过期，不把文本发给用户，但仍继续按一致的结束/收尾逻辑处理，避免错过及时结束
				if !p.isOutdatedSeq(mySeq) {
					if p.isDuplicateAssistantText(txt) {
						log.GetInstance().Sugar.Info("Suppressed duplicate assistant text in repoll for user ", msg.ExternalUserID)
					} else {
						// 先把文本发给用户，再按主流程同样的结束/拦截逻辑处理
						if err := p.addAssistantMessage(txt, true); err == nil {
							_ = p.sendAIReply(p.ctx, msg, txt)
							log.GetInstance().Sugar.Info("Repoll succeeded with text reply for user ", msg.ExternalUserID)
						} else {
							log.GetInstance().Sugar.Warnf("Repoll addAssistantMessage failed: %v", err)
						}
					}
				} else {
					log.GetInstance().Sugar.Info("Repoll text is outdated; skip sending but apply end-handling if any")
				}
				// 应用与主流程一致的结束拦截/收尾逻辑（即使过期也执行，以免错过结束）
				if p.handleEndAttempt(p.ctx, msg, resp.EndCallData, resp.IsConversationEnd) {
					return true
				}
				return true
			}
		}
		// 短暂让出调度，避免热循环
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// handleEndAttempt 统一处理“对话结束”的拦截/收尾逻辑；返回true表示本轮处理已完成
func (p *Processor) handleEndAttempt(ctx context.Context, msg *kefu.KFRecvMessage, endCallData map[string]interface{}, isEnd bool) bool {
	if !isEnd {
		return false
	}
	// 若存在待确认的位置切换，仅拦截一次
	p.keywordsMu.Lock()
	hasPending := (p.pendingLoc != nil)
	firstIntercept := false
	var pd *PendingLoc
	if hasPending {
		pd = p.pendingLoc
		if !p.pendingEndIntercepted {
			p.pendingEndIntercepted = true
			firstIntercept = true
		} else {
			log.GetInstance().Sugar.Infof("Second end while pending; assume no switch and discard pending [%s/%s]",
				p.pendingLoc.District, p.pendingLoc.Plate)
			p.discardPending()
			hasPending = false
		}
	}
	p.keywordsMu.Unlock()

	if hasPending && firstIntercept {
		log.GetInstance().Sugar.Infof("Intercepted end: pending location exists [%s/%s]; asking confirmation before report",
			pd.District, pd.Plate)
		confirmText := p.buildPendingConfirmReply()
		if err := p.addAssistantMessage(confirmText, false); err != nil {
			log.GetInstance().Sugar.Error("Failed to add pending confirm to history: ", err)
			return true
		}
		if err := p.sendAIReply(ctx, msg, confirmText); err != nil {
			log.GetInstance().Sugar.Warnf("Failed to send pending confirm reply: %v", err)
		}
		return true
	}

	// 正常收尾
	log.GetInstance().Sugar.Info("Conversation ended for user ", msg.ExternalUserID)
	p.logConversationEnd(endCallData)
	p.sendToFurtherProcess()
	return true
}

// repollCallMiniMax 构造消息并调用MiniMax，支持临时nudge与toolChoice
func (p *Processor) repollCallMiniMax(ctx context.Context, withNudge bool, toolChoice string) (*minimax.RealEstateConsultResponse, error) {
	// 构造历史快照（与主流程一致的裁剪）
	p.historyMu.RLock()
	fullHistory := make([]minimax.Message, len(p.conversationHistory))
	copy(fullHistory, p.conversationHistory)
	p.historyMu.RUnlock()

	cfg := config.GetInstance()
	var historySnapshot []minimax.Message
	if cfg.ScheduleConfig.CompactHistory && len(fullHistory) > cfg.ScheduleConfig.MaxHistoryLength {
		historySnapshot = fullHistory[len(fullHistory)-cfg.ScheduleConfig.MaxHistoryLength:]
	} else {
		historySnapshot = fullHistory
	}

	// 可选nudge（不写入真实历史，仅当前请求可见）
	if withNudge {
		n := minimax.CreateTextMessage("user", "请先用1-2句中文简短回复用户（确认/追问），不要仅返回函数调用；随后仍可按需继续调用函数。")
		historySnapshot = append(historySnapshot, n)
	}

	// 工具集
	tools := []minimax.Tool{
		minimax.CreateExtractKeywordsTool(),
		minimax.CreateEndConversationTool(),
		minimax.CreateConfirmLocationChangeTool(),
	}

	// 直接调用带工具的对话接口
	resp, err := p.minimaxClient.ChatCompletionWithTools(ctx, historySnapshot, tools, toolChoice)
	if err != nil {
		return nil, fmt.Errorf("repoll ChatCompletionWithTools failed: %w", err)
	}

	// 兼容主流程返回结构，包装成 RealEstateConsultResponse
	var extractedKeywords map[string]interface{}
	if len(resp.Choices) > 0 && resp.Choices[0].Message != nil && len(resp.Choices[0].Message.ToolCalls) > 0 {
		for _, tc := range resp.Choices[0].Message.ToolCalls {
			if tc.Function.Name == "extract_keywords" {
				var kw map[string]interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &kw); err == nil {
					extractedKeywords = kw
				}
			}
		}
	}

	// end 检测（服从主流程逻辑，但此处仅做标记，不触发结束）
	isEnd, endData, isEndFuncCalled, _ := minimax.IsConversationEndDetected(resp)
	log.GetInstance().Sugar.Infof("Repoll end-detection: isEnd=%v, isEndFuncCalled=%v", isEnd, isEndFuncCalled)

	result := &minimax.RealEstateConsultResponse{
		Content:           "",
		IsConversationEnd: isEnd,
		IsEndFuncCalled:   isEndFuncCalled,
		EndCallData:       endData,
		ExtractedKeywords: extractedKeywords,
		RawResponse:       resp,
	}

	// 若有文本内容，填充
	if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
		if c, ok := resp.Choices[0].Message.Content.(string); ok {
			result.Content = c
		}
	}
	return result, nil
}

// incrementTurnSeq 轮次推进（溢出无碍）
func (p *Processor) incrementTurnSeq() { p.thinkWaiterMu.Lock(); p.turnSeq++; p.thinkWaiterMu.Unlock() }

// isOutdatedSeq 判断轮次是否过期
func (p *Processor) isOutdatedSeq(my uint64) bool {
	p.thinkWaiterMu.Lock()
	cur := p.turnSeq
	p.thinkWaiterMu.Unlock()
	return my != cur
}

// stopThinkingWaiter 取消旧的等待器
func (p *Processor) stopThinkingWaiter() {
	p.thinkWaiterMu.Lock()
	if p.thinkWaiterCancel != nil {
		p.thinkWaiterCancel()
		p.thinkWaiterCancel = nil
		p.thinkWaiterCtx = nil
		p.thinkWaiterRunning = false
	}
	p.thinkWaiterMu.Unlock()
}

// mergeLocationData 合并位置信息（Jenny式：最新值覆盖）
func (p *Processor) mergeLocationData(existing, new interface{}) interface{} {
	// 如果没有existing，直接返回new
	if existing == nil {
		return new
	}

	// Jenny式简化：直接用最新值覆盖，避免复杂的细粒度判断逻辑
	// 用户最新的表达通常反映真实意图的变化
	return new
}

// mergePriceData 合并价格数据（Jenny式：最新值覆盖）
func (p *Processor) mergePriceData(existing, new interface{}) interface{} {
	// 如果没有existing，直接返回new
	if existing == nil {
		return new
	}

	// Jenny式简化：直接用最新值覆盖，避免min > max等复杂交集逻辑
	// 用户最新的表达通常反映真实意图的变化
	return new
}

// mergeAreaData 合并面积数据（Jenny式：最新值覆盖）
func (p *Processor) mergeAreaData(existing, new interface{}) interface{} {
	// 如果没有existing，直接返回new
	if existing == nil {
		return new
	}

	// Jenny式简化：直接用最新值覆盖，避免复杂的区间交集逻辑
	// 用户最新的表达通常反映真实意图的变化
	return new
}

// mergeRoomLayoutData 合并户型数据（Jenny式：最新值覆盖）
func (p *Processor) mergeRoomLayoutData(existing, new interface{}) interface{} {
	if existing == nil {
		return new
	}

	// 当两者都是map时，进行字段级合并：
	// - 数值字段（bedrooms, living_rooms）：优先采用new中非零值；否则保留existing
	// - 字符串字段（description）：优先采用new中非空值；否则保留existing
	eMap, eOK := existing.(map[string]interface{})
	nMap, nOK := new.(map[string]interface{})
	if eOK && nOK {
		out := make(map[string]interface{})
		for k, v := range eMap {
			out[k] = v
		}

		getInt := func(v interface{}) (int, bool) {
			switch t := v.(type) {
			case int:
				return t, true
			case int32:
				return int(t), true
			case int64:
				return int(t), true
			case float64:
				return int(t), true
			default:
				return 0, false
			}
		}

		if v, ok := nMap["bedrooms"]; ok {
			if iv, ok2 := getInt(v); ok2 && iv > 0 {
				out["bedrooms"] = iv
			}
		}
		if v, ok := nMap["living_rooms"]; ok {
			if iv, ok2 := getInt(v); ok2 && iv > 0 {
				out["living_rooms"] = iv
			}
		}
		if v, ok := nMap["description"].(string); ok && strings.TrimSpace(v) != "" {
			out["description"] = v
		}

		return out
	}

	return new
}

// mergeStringArrayData 合并字符串数组（取并集去重）
func (p *Processor) mergeStringArrayData(existing, new interface{}) interface{} {
	// 如果没有existing，直接返回new
	if existing == nil {
		return new
	}

	// 转换为字符串切片
	var existingSlice []string
	switch v := existing.(type) {
	case []string:
		existingSlice = v
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				existingSlice = append(existingSlice, str)
			}
		}
	default:
		// 无法转换，返回new
		return new
	}

	var newSlice []string
	switch v := new.(type) {
	case []string:
		newSlice = v
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				newSlice = append(newSlice, str)
			}
		}
	case string:
		// 单个字符串也处理
		newSlice = []string{v}
	default:
		// 无法转换，返回existing
		return existing
	}

	// 使用map去重并合并
	seen := make(map[string]bool)
	result := make([]string, 0)

	// 先添加existing的
	for _, s := range existingSlice {
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	// 再添加new的
	for _, s := range newSlice {
		if s != "" && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// deepMergeFromFunctionCall Function Call专用合并（统一合并策略）
func (p *Processor) deepMergeFromFunctionCall(extractedKeywords map[string]interface{}) {
	// 预处理：标准化数组格式为单值格式
	preprocessed := p.preprocessExtractedKeywords(extractedKeywords)

	// 使用统一的合并策略，保持双引擎合并的一致性
	// 字符串数组字段会进行并集去重，结构化字段会覆盖
	p.keywordsMu.Lock()
	defer p.keywordsMu.Unlock()

	p.deepMergeKeywordsToMap(preprocessed)
}

// preprocessExtractedKeywords 预处理AI提取的关键词（标准化数组格式）
func (p *Processor) preprocessExtractedKeywords(extracted map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range extracted {
		switch key {
		case "location", "price", "rent_price", "area", "room_layout":
			// 数组维度：取最新一项（最后一个）转为map
			if arr, ok := value.([]interface{}); ok && len(arr) > 0 {
				// 取最后一项作为"最新"值
				result[key] = arr[len(arr)-1]
			} else {
				result[key] = value // 已经是map或其他格式，直接使用
			}
		case "decoration", "property_type", "orientation", "interest_points", "commercial":
			// 字符串数组维度：保持为数组（或确保为[]string格式）
			if arr, ok := value.([]interface{}); ok {
				// 转换为[]string格式
				strArr := make([]string, 0, len(arr))
				for _, item := range arr {
					if str, ok := item.(string); ok && str != "" {
						strArr = append(strArr, str)
					}
				}
				if len(strArr) > 0 {
					result[key] = strArr
				}
			} else if strArr, ok := value.([]string); ok {
				// 已经是[]string，过滤空值
				nonEmpty := make([]string, 0, len(strArr))
				for _, str := range strArr {
					if str != "" {
						nonEmpty = append(nonEmpty, str)
					}
				}
				if len(nonEmpty) > 0 {
					result[key] = nonEmpty
				}
			} else {
				result[key] = value // 其他格式直接使用
			}
		default:
			// 其他维度直接使用
			result[key] = value
		}
	}

	return result
}

// cleanupKeywordsExtraction 清理关键词提取资源
func (p *Processor) cleanupKeywordsExtraction() {
	// 取消worker
	if p.keywordsWorkerCancel != nil {
		p.keywordsWorkerCancel()
	}

	// 关闭channel
	if p.keywordsChan != nil {
		close(p.keywordsChan)
	}

	log.GetInstance().Sugar.Info("Cleaned up keywords extraction for user: ", p.userID)
}

// ===== 智能体空文本兜底：生成“确认+追问”的最小可读回复 =====

// buildProbingReply 根据当前已聚合到的关键词，构造一句简短确认 + 一个缺失关键项的追问
func (p *Processor) buildProbingReply() string {
	// 使用“有效快照”：currentKeywords +（如有）stagedNonLocation 的只读合并
	snapshot := p.buildEffectiveKeywordsSnapshot()

	// 组装简短确认
	parts := make([]string, 0, 4)

	// location 概述
	if loc, ok := snapshot["location"].(map[string]interface{}); ok {
		var where string
		if plate, ok := loc["plate"].(string); ok && plate != "" {
			where = plate
		} else if dist, ok := loc["district"].(string); ok && dist != "" {
			where = dist
		}
		if where != "" {
			parts = append(parts, where)
		}
	}

	// 价格概述
	if price, ok := snapshot["price"].(map[string]interface{}); ok {
		var seg string
		if v, vok := price["operator"].(string); vok && v == "about" {
			if min, mok := price["min"].(float64); mok && min > 0 {
				seg = fmt.Sprintf("预算%.0f万左右", min)
			}
		} else {
			var minV, maxV float64
			if mv, mok := price["min"].(float64); mok && mv > 0 {
				minV = mv
			}
			if xv, xok := price["max"].(float64); xok && xv > 0 {
				maxV = xv
			}
			if minV > 0 && maxV > 0 {
				seg = fmt.Sprintf("预算%.0f-%.0f万", minV, maxV)
			} else if minV > 0 {
				seg = fmt.Sprintf("预算不低于%.0f万", minV)
			} else if maxV > 0 {
				seg = fmt.Sprintf("预算不超过%.0f万", maxV)
			}
		}
		if seg != "" {
			parts = append(parts, seg)
		}
	}

	// 户型概述
	if rl, ok := snapshot["room_layout"].(map[string]interface{}); ok {
		if desc, ok := rl["description"].(string); ok && desc != "" {
			parts = append(parts, desc)
		} else if b, bok := rl["bedrooms"].(float64); bok && b > 0 {
			parts = append(parts, fmt.Sprintf("%.0f室", b))
		}
	}

	// 面积概述
	if area, ok := snapshot["area"].(map[string]interface{}); ok {
		var seg string
		if v, vok := area["operator"].(string); vok && v == "about" {
			if val, ok := area["value"].(float64); ok && val > 0 {
				seg = fmt.Sprintf("面积约%.0f㎡", val)
			}
		} else {
			var minA, maxA float64
			if mv, ok := area["min"].(float64); ok && mv > 0 {
				minA = mv
			}
			if xv, ok := area["max"].(float64); ok && xv > 0 {
				maxA = xv
			}
			if minA > 0 && maxA > 0 {
				seg = fmt.Sprintf("面积%.0f-%.0f㎡", minA, maxA)
			} else if minA > 0 {
				seg = fmt.Sprintf("面积不低于%.0f㎡", minA)
			} else if maxA > 0 {
				seg = fmt.Sprintf("面积不超过%.0f㎡", maxA)
			}
		}
		if seg != "" {
			parts = append(parts, seg)
		}
	}

	// 选择一个最重要的缺失项进行追问（按优先级）
	missing := p.pickMostImportantMissing(snapshot)
	var question string
	switch missing {
	case "transaction_type":
		question = "请问您是想购买新房还是二手房？"
	case "location":
		question = "请问您更倾向于哪个区域或板块？"
	case "price":
		question = "请问您的购房预算大概是多少？"
	case "room_layout":
		question = "请问您期望的户型是几室几厅？"
	case "area":
		question = "请问您期望的面积大概多少平米？"
	default:
		question = "还有哪些偏好（如装修、朝向、楼层）想补充吗？"
	}

	if len(parts) > 0 {
		return fmt.Sprintf("我已记录：%s。%s", strings.Join(parts, "、"), question)
	}
	return fmt.Sprintf("我已记录您的需求。%s", question)
}

// pickMostImportantMissing 根据当前已知信息选择一个优先级最高的缺失项
func (p *Processor) pickMostImportantMissing(snapshot map[string]interface{}) string {
	// 交易类型通过 interest_points 中是否包含“新房/二手房”来判断
	hasTxn := false
	if pts, ok := snapshot["interest_points"].([]string); ok {
		for _, t := range pts {
			if t == "新房" || t == "二手房" {
				hasTxn = true
				break
			}
		}
	} else if anyPts, ok := snapshot["interest_points"].([]interface{}); ok {
		for _, iv := range anyPts {
			if s, ok := iv.(string); ok && (s == "新房" || s == "二手房") {
				hasTxn = true
				break
			}
		}
	}

	hasLocation := false
	if loc, ok := snapshot["location"].(map[string]interface{}); ok {
		if (loc["district"] != nil && loc["district"] != "") || (loc["plate"] != nil && loc["plate"] != "") {
			hasLocation = true
		}
	}

	hasPrice := false
	if price, ok := snapshot["price"].(map[string]interface{}); ok && len(price) > 0 {
		hasPrice = true
	}

	hasLayout := false
	if rl, ok := snapshot["room_layout"].(map[string]interface{}); ok && len(rl) > 0 {
		hasLayout = true
	}

	hasArea := false
	if area, ok := snapshot["area"].(map[string]interface{}); ok && len(area) > 0 {
		hasArea = true
	}

	if !hasTxn {
		return "transaction_type"
	}
	if !hasLocation {
		return "location"
	}
	if !hasPrice {
		return "price"
	}
	if !hasLayout {
		return "room_layout"
	}
	if !hasArea {
		return "area"
	}
	return "other"
}

// isDuplicateAssistantText 判断与上一条assistant文本是否重复（严格字符串相等，去首尾空白）
func (p *Processor) isDuplicateAssistantText(newText string) bool {
	t := strings.TrimSpace(newText)
	if t == "" {
		return false
	}
	// 取最后一条assistant内容对比
	p.historyMu.RLock()
	defer p.historyMu.RUnlock()
	for i := len(p.conversationHistory) - 1; i >= 0; i-- {
		m := p.conversationHistory[i]
		if m.Role != "assistant" {
			continue
		}
		if c, ok := m.Content.(string); ok {
			if strings.TrimSpace(c) == t {
				return true
			}
		}
		break // 仅检查最近一条assistant
	}
	return false
}

// hasCoreFourReadyNoPending：核心 4 维齐全 且 无位置切换 pending
func (p *Processor) hasCoreFourReadyNoPending() bool {
	p.keywordsMu.RLock()
	defer p.keywordsMu.RUnlock()

	if p.pendingLoc != nil {
		return false
	}
	kw := p.currentKeywords
	if kw == nil {
		return false
	}

	// 位置（至少区或板块其一存在）
	locOk := false
	if lm, ok := kw["location"].(map[string]interface{}); ok && lm != nil {
		if d, _ := lm["district"].(string); strings.TrimSpace(d) != "" {
			locOk = true
		} else if pl, _ := lm["plate"].(string); strings.TrimSpace(pl) != "" {
			locOk = true
		}
	}

	// 价格（区间任一端或 about/value 均可）
	priceOk := false
	if pm, ok := kw["price"].(map[string]interface{}); ok && pm != nil {
		if v, _ := pm["min"].(float64); v > 0 {
			priceOk = true
		}
		if v, _ := pm["max"].(float64); v > 0 {
			priceOk = true
		}
		if _, ok := pm["value"].(float64); ok {
			priceOk = true
		}
	}

	// 面积（区间任一端或 value）
	areaOk := false
	if am, ok := kw["area"].(map[string]interface{}); ok && am != nil {
		if v, _ := am["min"].(float64); v > 0 {
			areaOk = true
		}
		if v, _ := am["max"].(float64); v > 0 {
			areaOk = true
		}
		if _, ok := am["value"].(float64); ok {
			areaOk = true
		}
	}

	// 户型（bedrooms>=1 或有自然语言描述）
	layoutOk := false
	if rm, ok := kw["room_layout"].(map[string]interface{}); ok && rm != nil {
		if b, _ := rm["bedrooms"].(float64); b >= 1 {
			layoutOk = true
		}
		if desc, _ := rm["description"].(string); strings.TrimSpace(desc) != "" {
			layoutOk = true
		}
	}

	return locOk && priceOk && areaOk && layoutOk
}

// tryServerAutoEnd：命中条件后，服务端直接收尾（仅一次）
func (p *Processor) tryServerAutoEnd(ctx context.Context, msg *kefu.KFRecvMessage, userText, aiText string) bool {
	// 1) 核心 4 维齐 + 无 pending
	if !p.hasCoreFourReadyNoPending() {
		// 额外防守：同步提取本轮用户文本的关键词，避免异步抽取尚未完成导致判断偏差
		if p.keywordExtractor != nil && strings.TrimSpace(userText) != "" {
			p.extractAndMergeKeywords(userText)
		}
		if !p.hasCoreFourReadyNoPending() {
			return false
		}
	}

	// 2) 触发：用户“无更多/催单” 或 AI“承诺语”
	trigger := containsAny(userText, stopWordsForEnd) || containsAny(aiText, promisePhrases)
	if !trigger {
		return false
	}

	// 3) 每轮只触发一次
	if !atomic.CompareAndSwapInt32(&p.autoEndOnce, 0, 1) {
		return false
	}

	// 4) 组装结束文本：若 AI 文本不空，尾部补一行固定结束语；否则只发结束语
	endLine := "感谢您的咨询，小胖稍后会为您生成详细的房源推荐报告。"
	final := strings.TrimSpace(aiText)
	if final == "" {
		final = endLine
	} else if !strings.Contains(final, endLine) {
		final = final + "\n" + endLine
	}

	// 5) 入历史并发送
	if err := p.addAssistantMessage(final, true); err != nil {
		log.GetInstance().Sugar.Warn("auto-end: addAssistantMessage failed: ", err)
	}
	if err := p.sendAIReply(ctx, msg, final); err != nil {
		log.GetInstance().Sugar.Warn("auto-end: sendAIReply failed: ", err)
	}

	// 6) 推送到后续处理（生成报告链路）
	p.sendToFurtherProcess()
	p.logConversationEnd(nil)

	log.GetInstance().Sugar.Infof("Auto-ended by server fallback for user %s", msg.ExternalUserID)
	return true
}

// containsAny 简单包含判断（特殊处理 ? / ？）
func containsAny(text string, arr []string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, s := range arr {
		if s == "？" || s == "?" {
			if strings.Contains(t, "?") || strings.Contains(t, "？") {
				return true
			}
			continue
		}
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

// processStressMessage 压测模式下处理消息
// 只检查消息的 MsgID 是否连续，不执行任何业务逻辑
// Sleep 3 秒模拟业务处理耗时
func (p *Processor) processStressMessage(in *message.InboundMsg) error {
	if in == nil || in.Msg == nil {
		return nil
	}

	msg := in.Msg
	userID := msg.ExternalUserID
	msgID := msg.MsgID

	start := time.Now()
	defer func() {
		// 记录处理延迟（包括 3s sleep）
		prom.StressProcessingDuration.Observe(time.Since(start).Seconds())
	}()

	// 检查消息顺序
	if err := p.seqChecker.Check(userID, msgID); err != nil {
		// 顺序违规
		if sv, ok := IsSequenceViolation(err); ok {
			// 详细记录到日志（包含 user_id, expected, actual）
			log.GetInstance().Sugar.Errorf("Sequence violation - UserID: %s, Expected: %d, Got: %d",
				sv.UserID, sv.ExpectedSeq, sv.ActualSeq)

			// 上报 Prometheus 指标（不带高基数 labels）
			prom.StressSequenceViolations.Inc()
			prom.StressMessagesProcessed.WithLabelValues("sequence_error").Inc()
		} else {
			// 其他错误（如 MsgID 格式错误）
			log.GetInstance().Sugar.Errorf("Invalid message in stress test - UserID: %s, Error: %v", userID, err)
			prom.StressMessagesProcessed.WithLabelValues("invalid_format").Inc()
		}
		return err
	}

	// 顺序正常
	prom.StressMessagesProcessed.WithLabelValues("ok").Inc()

	// 模拟业务处理耗时 3 秒
	time.Sleep(10 * time.Millisecond)

	return nil
}
