package keywords

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/go-ego/gse"
)

// AdvancedSegmenter 高级分词器
type AdvancedSegmenter struct {
	gse         gse.Segmenter
	userDict    map[string]bool
	mu          sync.RWMutex
	initialized bool
}

// ChineseNumberRegex 中文数字正则
var ChineseNumberRegex = regexp.MustCompile(`[一二三四五六七八九十两俩仨壹贰叁肆伍陆柒捌玖拾零〇百千万亿]{1,10}`)

// WordWithTag 词汇标记结构
type WordWithTag struct {
	Word string
	Tag  string
}

// NewAdvancedSegmenter 创建高级分词器
func NewAdvancedSegmenter() *AdvancedSegmenter {
	segmenter := &AdvancedSegmenter{
		userDict: make(map[string]bool),
	}

	if err := segmenter.init(); err != nil {
		return nil
	}

	return segmenter
}

// init 初始化分词器
func (seg *AdvancedSegmenter) init() error {
	seg.mu.Lock()
	defer seg.mu.Unlock()

	// 初始化gse分词器
	seg.gse.LoadDict()

	// 加载自定义词典
	seg.loadCustomDict()

	seg.initialized = true
	return nil
}

// loadCustomDict 加载自定义词典
func (seg *AdvancedSegmenter) loadCustomDict() {
	// 房产相关专业词汇
	customWords := []string{
		// 房型相关 - 确保这些词汇不被拆分
		"一居室", "二居室", "三居室", "四居室", "五居室",
		"1居室", "2居室", "3居室", "4居室", "5居室",
		"一室一厅", "二室一厅", "三室二厅", "四室二厅",
		"1室1厅", "2室1厅", "3室2厅", "4室2厅", "5室3厅",
		"单身公寓", "精装公寓", "loft公寓", "复式公寓",
		"开间", "单间", "大开间", "小开间",

		// 房产类型
		"地铁房", "刚需房", "改善房", "投资房",
		"南北通透", "东南向", "西南向", "朝南", "朝北",
		"毛坯房", "精装修", "简装修", "豪华装修", "拎包入住",
		"二手房", "新房", "期房", "现房", "准现房",
		"别墅", "洋房", "高层", "小高层", "多层",
		"地铁口", "地铁站", "公交站", "商圈", "配套",
		"户型图", "样板间", "售楼处", "中介费", "过户费",
	}

	for _, word := range customWords {
		seg.userDict[word] = true
		// gse 添加自定义词典
		seg.gse.AddToken(word, 1000, "n") // 设置为名词，权重1000
	}
}

// AddUserWord 添加用户自定义词汇
func (seg *AdvancedSegmenter) AddUserWord(word string) {
	seg.mu.Lock()
	defer seg.mu.Unlock()

	seg.userDict[word] = true
	seg.gse.AddToken(word, 500, "n")
}

// CutWithTag 分词并标注词性
func (seg *AdvancedSegmenter) CutWithTag(text string) []WordWithTag {
	if !seg.initialized {
		return []WordWithTag{}
	}

	// 使用gse进行分词
	segments := seg.gse.Cut(text, true)

	result := make([]WordWithTag, 0, len(segments))
	for _, word := range segments {
		if strings.TrimSpace(word) == "" {
			continue
		}

		// 简单的词性标注逻辑
		tag := seg.inferPOSTag(word)
		result = append(result, WordWithTag{
			Word: word,
			Tag:  tag,
		})
	}

	return result
}

// Cut 简单分词
func (seg *AdvancedSegmenter) Cut(text string) []string {
	if !seg.initialized {
		return []string{}
	}

	return seg.gse.Cut(text, true)
}

// CutForSearch 搜索分词
func (seg *AdvancedSegmenter) CutForSearch(text string) []string {
	if !seg.initialized {
		return []string{}
	}

	return seg.gse.CutSearch(text, true)
}

// inferPOSTag 推断词性标签
func (seg *AdvancedSegmenter) inferPOSTag(word string) string {
	// 基于规则的简单词性推断
	if len(word) == 0 {
		return "x"
	}

	// 数字
	if regexp.MustCompile(`^\d+$`).MatchString(word) {
		return "m"
	}

	// 中文数字
	if ChineseNumberRegex.MatchString(word) {
		return "m"
	}

	// 常见量词
	quantifiers := []string{"个", "套", "间", "栋", "层", "米", "平", "万", "千", "元"}
	for _, q := range quantifiers {
		if strings.Contains(word, q) {
			return "mq"
		}
	}

	// 地名
	locations := []string{"区", "县", "市", "省", "街", "路", "村", "镇", "里", "园"}
	for _, loc := range locations {
		if strings.HasSuffix(word, loc) {
			return "ns"
		}
	}

	// 时间词
	timeWords := []string{"年", "月", "日", "点", "时", "分", "今天", "明天", "昨天", "下周", "上月"}
	for _, t := range timeWords {
		if strings.Contains(word, t) {
			return "tg"
		}
	}

	// 默认为名词
	return "n"
}

// ExtractNumbers 提取数字
func (seg *AdvancedSegmenter) ExtractNumbers(text string) []string {
	// 阿拉伯数字
	arabicNumbers := regexp.MustCompile(`\d+(?:\.\d+)?`).FindAllString(text, -1)

	// 中文数字
	chineseNumbers := ChineseNumberRegex.FindAllString(text, -1)

	// 合并结果
	result := make([]string, 0, len(arabicNumbers)+len(chineseNumbers))
	result = append(result, arabicNumbers...)
	result = append(result, chineseNumbers...)

	return result
}

// IsInitialized 检查是否已初始化
func (seg *AdvancedSegmenter) IsInitialized() bool {
	seg.mu.RLock()
	defer seg.mu.RUnlock()
	return seg.initialized
}

// GetStats 获取统计信息
func (seg *AdvancedSegmenter) GetStats() map[string]interface{} {
	seg.mu.RLock()
	defer seg.mu.RUnlock()

	return map[string]interface{}{
		"initialized":    seg.initialized,
		"user_dict_size": len(seg.userDict),
		"segmenter_type": "gse",
	}
}

// Close 关闭分词器
func (seg *AdvancedSegmenter) Close() {
	seg.mu.Lock()
	defer seg.mu.Unlock()

	// gse 不需要显式关闭
	seg.initialized = false
}

// PreprocessText 文本预处理
func (seg *AdvancedSegmenter) PreprocessText(text string) string {
	// 移除多余空白
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	// 标准化标点符号
	replacements := map[string]string{
		"，": ",", "。": ".", "！": "!", "？": "?",
		"；": ";", "：": ":", "（": "(", "）": ")",
		"【": "[", "】": "]", "《": "<", "》": ">",
	}

	for old, new := range replacements {
		text = strings.ReplaceAll(text, old, new)
	}

	return text
}

// ValidateText 验证文本
func (seg *AdvancedSegmenter) ValidateText(text string) error {
	if text == "" {
		return fmt.Errorf("文本不能为空")
	}

	if len(text) > 10000 {
		return fmt.Errorf("文本长度不能超过10000字符")
	}

	// 检查是否包含有效的中文字符
	chineseRegex := regexp.MustCompile(`[\p{Han}]`)
	if !chineseRegex.MatchString(text) {
		return fmt.Errorf("文本应包含中文字符")
	}

	return nil
}

// AnalyzeText 分析文本特征
func (seg *AdvancedSegmenter) AnalyzeText(text string) map[string]interface{} {
	words := seg.Cut(text)

	// 统计特征
	chineseCount := len(regexp.MustCompile(`[\p{Han}]`).FindAllString(text, -1))
	englishCount := len(regexp.MustCompile(`[a-zA-Z]`).FindAllString(text, -1))
	digitCount := len(regexp.MustCompile(`\d`).FindAllString(text, -1))
	punctCount := len(regexp.MustCompile(`[\p{P}]`).FindAllString(text, -1))

	// 计算平均词长，避免除零错误
	var avgWordLength float64
	if len(words) > 0 {
		avgWordLength = float64(len(text)) / float64(len(words))
	} else {
		avgWordLength = 0.0
	}

	return map[string]interface{}{
		"text_length":     len(text),
		"word_count":      len(words),
		"chinese_chars":   chineseCount,
		"english_chars":   englishCount,
		"digit_chars":     digitCount,
		"punct_chars":     punctCount,
		"avg_word_length": avgWordLength,
	}
}

// ConvertToSimplified 繁体转简体（简单实现）
func (seg *AdvancedSegmenter) ConvertToSimplified(text string) string {
	// 简单的繁体字映射
	traditionalToSimplified := map[string]string{
		"購": "购", "買": "买", "賣": "卖", "價": "价",
		"樓": "楼", "層": "层", "區": "区", "縣": "县",
		"環": "环", "點": "点", "時": "时", "間": "间",
		"動": "动", "産": "产", "業": "业", "務": "务",
		"門": "门", "車": "车", "學": "学", "園": "园",
	}

	result := text

	// 🔧 修复稳定性问题：确保确定性的遍历顺序
	var traditionalKeys []string
	for traditional := range traditionalToSimplified {
		traditionalKeys = append(traditionalKeys, traditional)
	}
	sort.Strings(traditionalKeys)

	for _, traditional := range traditionalKeys {
		simplified := traditionalToSimplified[traditional]
		result = strings.ReplaceAll(result, traditional, simplified)
	}

	return result
}

// SplitSentences 分句
func (seg *AdvancedSegmenter) SplitSentences(text string) []string {
	// 句子分割符
	sentenceEnders := regexp.MustCompile(`[。！？.!?]+`)

	sentences := sentenceEnders.Split(text, -1)

	// 清理空句子
	result := make([]string, 0, len(sentences))
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence != "" {
			result = append(result, sentence)
		}
	}

	return result
}

// TokenizeBasic 基础分词（纯空格分割）
func (seg *AdvancedSegmenter) TokenizeBasic(text string) []string {
	return strings.Fields(text)
}

// FilterStopWords 过滤停用词
func (seg *AdvancedSegmenter) FilterStopWords(words []string) []string {
	// 基础停用词列表
	stopWords := map[string]bool{
		"的": true, "了": true, "在": true, "是": true, "我": true,
		"有": true, "和": true, "就": true, "不": true, "人": true,
		"都": true, "一": true, "一个": true, "上": true, "也": true,
		"很": true, "到": true, "说": true, "要": true, "去": true,
		"你": true, "会": true, "着": true, "没有": true, "看": true,
		"好": true, "自己": true, "这": true, "那": true, "里": true,
		"就是": true, "还": true, "把": true, "比": true, "他": true,
	}

	result := make([]string, 0, len(words))
	for _, word := range words {
		if !stopWords[word] && len(word) > 1 {
			result = append(result, word)
		}
	}

	return result
}
