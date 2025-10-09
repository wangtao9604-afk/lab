package kefu

import (
	"encoding/xml"
	"errors"
	"fmt"

	"qywx/infrastructures/httplib"
	"qywx/infrastructures/log"
)

// 客服消息类型常量
const (
	KFMsgTypeText        = "text"
	KFMsgTypeImage       = "image"
	KFMsgTypeVoice       = "voice"
	KFMsgTypeVideo       = "video"
	KFMsgTypeFile        = "file"
	KFMsgTypeLocation    = "location"
	KFMsgTypeLink        = "link"
	KFMsgTypeMiniprogram = "miniprogram"
	KFMsgTypeMenu        = "msgmenu"
	KFMsgTypeEvent       = "event"
)

// 客服事件类型常量
const (
	KFEventEnterSession            = "enter_session"              // 用户进入会话
	KFEventMsgSendFail             = "msg_send_fail"              // 消息发送失败
	KFEventServicerChange          = "servicer_change"            // 接待人员接待状态变更
	KFEventSessionStatusChange     = "session_status_change"      // 会话状态变更
	KFEventUserRecallMsg           = "user_recall_msg"            // 用户撤回消息
	KFEventServicerRecallMsg       = "servicer_recall_msg"        // 接待人员撤回消息
	KFEventRejectCustomerMsgSwitch = "reject_customer_msg_switch" // 拒收客户消息变更
)

// KFMessage 客服消息基础结构
type KFMessage struct {
	ToUser    string `json:"touser"`          // 接收消息的客户UserID
	OpenKFID  string `json:"open_kfid"`       // 客服账号ID
	MsgID     string `json:"msgid,omitempty"` // 消息ID（用于去重）
	MsgType   string `json:"msgtype"`         // 消息类型
	CorpToken string `json:"-"`               // 发送消息的token
}

// KFTextMessage 客服文本消息
type KFTextMessage struct {
	KFMessage
	Text struct {
		Content string `json:"content"`           // 文本内容
		MenuID  string `json:"menu_id,omitempty"` // 菜单ID（可选）
	} `json:"text"`
}

// KFImageMessage 客服图片消息
type KFImageMessage struct {
	KFMessage
	Image struct {
		MediaID string `json:"media_id"` // 图片媒体文件ID
	} `json:"image"`
}

// KFVoiceMessage 客服语音消息
type KFVoiceMessage struct {
	KFMessage
	Voice struct {
		MediaID string `json:"media_id"` // 语音文件ID
	} `json:"voice"`
}

// KFVideoMessage 客服视频消息
type KFVideoMessage struct {
	KFMessage
	Video struct {
		MediaID string `json:"media_id"` // 视频媒体文件ID
	} `json:"video"`
}

// KFFileMessage 客服文件消息
type KFFileMessage struct {
	KFMessage
	File struct {
		MediaID string `json:"media_id"` // 文件ID
	} `json:"file"`
}

// KFLinkMessage 客服链接消息
type KFLinkMessage struct {
	KFMessage
	Link struct {
		Title  string `json:"title"`             // 标题
		Desc   string `json:"desc"`              // 描述
		URL    string `json:"url"`               // 链接地址
		PicURL string `json:"pic_url,omitempty"` // 图片地址
	} `json:"link"`
}

// KFMiniprogramMessage 客服小程序消息
type KFMiniprogramMessage struct {
	KFMessage
	Miniprogram struct {
		AppID        string `json:"appid"`          // 小程序appid
		Title        string `json:"title"`          // 标题
		ThumbMediaID string `json:"thumb_media_id"` // 缩略图媒体ID
		PagePath     string `json:"pagepath"`       // 小程序页面路径
	} `json:"miniprogram"`
}

// KFMenuMessage 客服菜单消息
type KFMenuMessage struct {
	KFMessage
	MsgMenu struct {
		HeadContent string `json:"head_content,omitempty"` // 菜单头部内容
		List        []struct {
			ID      string `json:"id"`      // 菜单项ID
			Content string `json:"content"` // 菜单项内容
		} `json:"list"` // 菜单项列表
		TailContent string `json:"tail_content,omitempty"` // 菜单尾部内容
	} `json:"msgmenu"`
}

// KFSyncMsgRequest 同步消息请求
type KFSyncMsgRequest struct {
	Cursor      string `json:"cursor,omitempty"`       // 上一次调用的返回cursor
	Token       string `json:"token,omitempty"`        // 回调事件返回的token
	Limit       int    `json:"limit,omitempty"`        // 期望请求的数据量，默认100，最大1000
	VoiceFormat int    `json:"voice_format,omitempty"` // 语音格式，0-amr，1-silk
	OpenKFID    string `json:"open_kfid,omitempty"`    // 客服账号ID（可选）
}

// KFSyncMsgResponse 同步消息响应
type KFSyncMsgResponse struct {
	ErrCode    int             `json:"errcode"`
	ErrMsg     string          `json:"errmsg"`
	NextCursor string          `json:"next_cursor"` // 下次请求的cursor
	HasMore    int             `json:"has_more"`    // 是否还有更多数据
	MsgList    []KFRecvMessage `json:"msg_list"`    // 消息列表
}

// KFRecvMessage 接收的客服消息
type KFRecvMessage struct {
	MsgID          string             `json:"msgid"`                     // 消息ID
	OpenKFID       string             `json:"open_kfid"`                 // 客服账号ID
	ExternalUserID string             `json:"external_userid"`           // 客户UserID
	SendTime       int64              `json:"send_time"`                 // 发送时间
	Origin         int                `json:"origin"`                    // 消息来源：3-客户，4-系统，5-客服
	ServicerUserID string             `json:"servicer_userid,omitempty"` // 客服人员UserID
	MsgType        string             `json:"msgtype"`                   // 消息类型
	Text           *KFTextContent     `json:"text,omitempty"`
	Image          *KFImageContent    `json:"image,omitempty"`
	Voice          *KFVoiceContent    `json:"voice,omitempty"`
	Video          *KFVideoContent    `json:"video,omitempty"`
	File           *KFFileContent     `json:"file,omitempty"`
	Location       *KFLocationContent `json:"location,omitempty"`
	Link           *KFLinkContent     `json:"link,omitempty"`
	Event          *KFEventContent    `json:"event,omitempty"`
}

// 各种消息内容类型
type KFTextContent struct {
	Content string `json:"content"`
	MenuID  string `json:"menu_id,omitempty"`
}

type KFImageContent struct {
	MediaID string `json:"media_id"`
}

type KFVoiceContent struct {
	MediaID string `json:"media_id"`
}

type KFVideoContent struct {
	MediaID string `json:"media_id"`
}

type KFFileContent struct {
	MediaID string `json:"media_id"`
}

type KFLocationContent struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
}

type KFLinkContent struct {
	Title  string `json:"title"`
	Desc   string `json:"desc,omitempty"`
	URL    string `json:"url"`
	PicURL string `json:"pic_url,omitempty"`
}

// KFEventContent 通用事件内容（用于KFRecvMessage.Event字段）
// 包含所有事件类型的字段，统一数据结构
type KFEventContent struct {
	EventType         string `json:"event_type"`
	OpenKFID          string `json:"open_kfid,omitempty"`
	ExternalUserID    string `json:"external_userid,omitempty"`
	Scene             string `json:"scene,omitempty"`               // 进入会话场景值（用户进入会话事件）
	SceneParam        string `json:"scene_param,omitempty"`         // 场景参数（用户进入会话事件）
	WelcomeCode       string `json:"welcome_code,omitempty"`        // 欢迎语code（用户进入会话事件）
	FailMsgID         string `json:"fail_msgid,omitempty"`          // 失败的消息ID（消息发送失败事件）
	FailType          int    `json:"fail_type,omitempty"`           // 失败类型（消息发送失败事件）
	ServicerUserID    string `json:"servicer_userid,omitempty"`     // 接待人员ID
	Status            int    `json:"status,omitempty"`              // 接待状态（接待状态变更事件）
	ChangeType        int    `json:"change_type,omitempty"`         // 变更类型（会话状态变更事件）
	OldServicerUserID string `json:"old_servicer_userid,omitempty"` // 老接待人员ID（会话状态变更事件）
	NewServicerUserID string `json:"new_servicer_userid,omitempty"` // 新接待人员ID（会话状态变更事件）
	RecallMsgID       string `json:"recall_msgid,omitempty"`        // 撤回的消息ID（撤回消息事件）
	RejectSwitch      int    `json:"reject_switch,omitempty"`       // 拒收开关：1-拒收 2-不拒收（拒收客户消息变更事件）
}

// KFCallbackMessage 客服回调消息（XML格式）
type KFCallbackMessage struct {
	ToUserName string `xml:"ToUserName"` // 企业微信CorpID
	CreateTime int64  `xml:"CreateTime"` // 消息创建时间
	MsgType    string `xml:"MsgType"`    // 消息类型，此时固定为：event
	Event      string `xml:"Event"`      // 事件类型：kf_msg_or_event
	Token      string `xml:"Token"`      // 调用企业的回调Token，用于后续调用sync_msg
	OpenKFID   string `xml:"OpenKfId"`   // 客服账号ID
}

// 客服API响应结果
type KFResult struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	MsgID   string `json:"msgid,omitempty"` // 消息ID（发送消息时返回）
}

// SendKFText 发送客服文本消息
func SendKFText(msg *KFTextMessage) (*KFResult, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	msg.MsgType = KFMsgTypeText
	return sendKFMessage(msg, msg.CorpToken)
}

// SendKFImage 发送客服图片消息
func SendKFImage(msg *KFImageMessage) (*KFResult, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	msg.MsgType = KFMsgTypeImage
	return sendKFMessage(msg, msg.CorpToken)
}

// SendKFVoice 发送客服语音消息
func SendKFVoice(msg *KFVoiceMessage) (*KFResult, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	msg.MsgType = KFMsgTypeVoice
	return sendKFMessage(msg, msg.CorpToken)
}

// SendKFVideo 发送客服视频消息
func SendKFVideo(msg *KFVideoMessage) (*KFResult, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	msg.MsgType = KFMsgTypeVideo
	return sendKFMessage(msg, msg.CorpToken)
}

// SendKFFile 发送客服文件消息
func SendKFFile(msg *KFFileMessage) (*KFResult, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	msg.MsgType = KFMsgTypeFile
	return sendKFMessage(msg, msg.CorpToken)
}

// SendKFLink 发送客服链接消息
func SendKFLink(msg *KFLinkMessage) (*KFResult, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	msg.MsgType = KFMsgTypeLink
	return sendKFMessage(msg, msg.CorpToken)
}

// SendKFMiniprogram 发送客服小程序消息
func SendKFMiniprogram(msg *KFMiniprogramMessage) (*KFResult, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	msg.MsgType = KFMsgTypeMiniprogram
	return sendKFMessage(msg, msg.CorpToken)
}

// SendKFMenu 发送客服菜单消息
func SendKFMenu(msg *KFMenuMessage) (*KFResult, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}
	if len(msg.MsgMenu.List) == 0 {
		return nil, errors.New("menu list cannot be empty")
	}
	msg.MsgType = KFMsgTypeMenu
	return sendKFMessage(msg, msg.CorpToken)
}

// sendKFMessage 统一的客服消息发送函数
func sendKFMessage(msg interface{}, token string) (*KFResult, error) {
	result := &KFResult{}
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/kf/send_msg?access_token=%s", token)

	log.GetInstance().Sugar.Debugf("sending kf message: %+v", msg)

	if err := httplib.PostJSON(url, msg, result); err != nil {
		return nil, fmt.Errorf("send kf message failed: %w", err)
	}

	if result.ErrCode != 0 {
		return result, fmt.Errorf("kf api error: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	return result, nil
}

// ParseKFCallbackMessage 解析客服回调消息
// 直接解析XML，因为解密工作已在controller层完成
func ParseKFCallbackMessage(data []byte) (*KFCallbackMessage, error) {
	msg := &KFCallbackMessage{}
	if err := xml.Unmarshal(data, msg); err != nil {
		return nil, fmt.Errorf("parse kf callback message failed: %w", err)
	}

	log.GetInstance().Sugar.Infof("parsed kf callback: Event=%s, Token=%s, OpenKFID=%s",
		msg.Event, msg.Token, msg.OpenKFID)

	return msg, nil
}

// SyncOneKFMessage 获取单条最新的客服消息（用于快速获取用户ID等场景）
func FetchExternalUserID(openKFID string, msgToken, accessToken string) (string, string, error) {
	req := &KFSyncMsgRequest{
		Cursor:   "", // 空游标从最老的消息开始
		Limit:    1,
		OpenKFID: openKFID,
		Token:    msgToken,
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/kf/sync_msg?access_token=%s", accessToken)

	result := &KFSyncMsgResponse{}
	if err := httplib.PostJSON(url, req, result); err != nil {
		return "", "", fmt.Errorf("sync one kf message failed: %w", err)
	}

	if result.ErrCode != 0 {
		return "", "", fmt.Errorf("kf sync api error: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	if len(result.MsgList) == 0 {
		return "", "", fmt.Errorf("no kf message found")
	}

	externalUserId := result.MsgList[0].ExternalUserID
	if result.MsgList[0].MsgType == "event" {
		externalUserId = result.MsgList[0].Event.ExternalUserID
	}

	return externalUserId, result.NextCursor, nil
}

// SyncKFMessages 同步客服消息（自动处理分页，获取所有消息）
func SyncKFMessages(req *KFSyncMsgRequest, token string) (*KFSyncMsgResponse, error) {
	// 参数初始化
	if req == nil {
		req = &KFSyncMsgRequest{}
	}
	if req.Limit <= 0 || req.Limit > 1000 {
		req.Limit = 100
	}

	// 准备返回的聚合结果
	aggregateResult := &KFSyncMsgResponse{
		MsgList: []KFRecvMessage{},
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/kf/sync_msg?access_token=%s", token)
	cursor := req.Cursor

	// 循环获取直到HasMore为0
	for {
		// 构造请求
		currentReq := &KFSyncMsgRequest{
			Cursor:      cursor,
			Token:       req.Token,
			Limit:       req.Limit,
			VoiceFormat: req.VoiceFormat,
			OpenKFID:    req.OpenKFID,
		}

		log.GetInstance().Sugar.Debugf("fetching kf messages batch: cursor=%s", cursor)

		// 发送请求
		batchResult := &KFSyncMsgResponse{}
		if err := httplib.PostJSON(url, currentReq, batchResult); err != nil {
			return nil, fmt.Errorf("sync kf messages failed: %w", err)
		}

		if batchResult.ErrCode != 0 {
			return nil, fmt.Errorf("kf sync api error: code=%d, msg=%s", batchResult.ErrCode, batchResult.ErrMsg)
		}

		// 聚合消息列表
		aggregateResult.MsgList = append(aggregateResult.MsgList, batchResult.MsgList...)

		// 更新游标和HasMore状态
		aggregateResult.NextCursor = batchResult.NextCursor
		aggregateResult.HasMore = batchResult.HasMore

		log.GetInstance().Sugar.Infof("fetched %d messages in this batch, total: %d, has_more: %d",
			len(batchResult.MsgList), len(aggregateResult.MsgList), batchResult.HasMore)

		// 检查是否还有更多数据
		if batchResult.HasMore == 0 {
			break
		}

		// 更新游标继续获取
		cursor = batchResult.NextCursor
	}

	log.GetInstance().Sugar.Infof("sync completed, total messages: %d", len(aggregateResult.MsgList))
	return aggregateResult, nil
}

// GetKFAccount 获取客服账号信息
type KFAccountInfo struct {
	ErrCode  int    `json:"errcode"`
	ErrMsg   string `json:"errmsg"`
	OpenKFID string `json:"open_kfid"` // 客服账号ID
	Name     string `json:"name"`      // 客服名称
	Avatar   string `json:"avatar"`    // 头像URL
}

// GetKFAccount 获取客服账号信息
func GetKFAccount(openKFID string, token string) (*KFAccountInfo, error) {
	if openKFID == "" {
		return nil, errors.New("open_kfid cannot be empty")
	}

	result := &KFAccountInfo{}
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/kf/account/get?access_token=%s&open_kfid=%s",
		token, openKFID)

	if err := httplib.GetJSON(url, result); err != nil {
		return nil, fmt.Errorf("get kf account failed: %w", err)
	}

	if result.ErrCode != 0 {
		return result, fmt.Errorf("get kf account error: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	return result, nil
}

// KFSessionState 客服会话状态
type KFSessionState struct {
	ErrCode       int    `json:"errcode"`
	ErrMsg        string `json:"errmsg"`
	ServiceState  int    `json:"service_state"`  // 会话状态：0-未处理，1-由智能助手接待，2-待接入池，3-由人工接待，4-已结束
	ServiceUserID string `json:"service_userid"` // 接待客服userid
}

// GetKFSessionState 获取会话状态
func GetKFSessionState(openKFID, externalUserID string, token string) (*KFSessionState, error) {
	if openKFID == "" || externalUserID == "" {
		return nil, errors.New("open_kfid and external_userid cannot be empty")
	}

	req := map[string]string{
		"open_kfid":       openKFID,
		"external_userid": externalUserID,
	}

	result := &KFSessionState{}
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/kf/service_state/get?access_token=%s", token)

	if err := httplib.PostJSON(url, req, result); err != nil {
		return nil, fmt.Errorf("get session state failed: %w", err)
	}

	if result.ErrCode != 0 {
		return result, fmt.Errorf("get session state error: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	return result, nil
}

// TransKFSession 转接会话
func TransKFSession(openKFID, externalUserID, servicerUserID string, token string) error {
	if openKFID == "" || externalUserID == "" || servicerUserID == "" {
		return errors.New("open_kfid, external_userid and servicer_userid cannot be empty")
	}

	req := map[string]string{
		"open_kfid":       openKFID,
		"external_userid": externalUserID,
		"servicer_userid": servicerUserID,
	}

	result := &struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}{}
	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/kf/service_state/trans?access_token=%s", token)

	if err := httplib.PostJSON(url, req, result); err != nil {
		return fmt.Errorf("trans session failed: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("trans session error: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	return nil
}

// ParseKFEvent 解析客服事件（从KFRecvMessage中的Event字段）
// ParseKFEvent 解析客服事件（简化版：直接返回KFEventContent）
func ParseKFEvent(msg *KFRecvMessage) (*KFEventContent, error) {
	if msg == nil || msg.MsgType != KFMsgTypeEvent || msg.Event == nil {
		return nil, errors.New("not a valid event message")
	}
	// 直接返回Event字段，它已经包含所有信息
	return msg.Event, nil
}

// GetKFEventType 获取事件类型（辅助函数）
func GetKFEventType(msg *KFRecvMessage) string {
	if msg == nil || msg.MsgType != KFMsgTypeEvent || msg.Event == nil {
		return ""
	}
	return msg.Event.EventType
}

// IsKFEvent 判断是否为事件消息（辅助函数）
func IsKFEvent(msg *KFRecvMessage) bool {
	return msg != nil && msg.MsgType == KFMsgTypeEvent && msg.Event != nil
}
