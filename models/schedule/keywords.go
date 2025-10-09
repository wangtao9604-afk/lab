package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	"qywx/infrastructures/ipang"
	"qywx/infrastructures/keywords"
	"qywx/infrastructures/log"
	"qywx/infrastructures/tokenstore"
	"qywx/infrastructures/utils"
	"qywx/infrastructures/wxmsg/kefu"
	"qywx/infrastructures/wxmsg/upload"
	"qywx/models/thirdpart/minimax"
)

// KeywordDimension 关键字维度
type KeywordDimension string

const (
	DimensionLocation       KeywordDimension = "location"        // 地段
	DimensionDecoration     KeywordDimension = "decoration"      // 装修
	DimensionPropertyType   KeywordDimension = "property_type"   // 房产类型
	DimensionRoomLayout     KeywordDimension = "room_layout"     // 户型
	DimensionPrice          KeywordDimension = "price"           // 价格
	DimensionRentPrice      KeywordDimension = "rent_price"      // 租赁价格
	DimensionArea           KeywordDimension = "area"            // 房屋面积
	DimensionInterestPoints KeywordDimension = "interest_points" // 兴趣点
	DimensionCommercial     KeywordDimension = "commercial"      // 商业投资相关属性
	DimensionOrientation    KeywordDimension = "orientation"     // 房屋朝向
	DimensionSubway         KeywordDimension = "subway"          // 地铁信息
)

// 具体类型定义

// LocationInfo 地段信息
type LocationInfo struct {
	Province string `json:"province,omitempty"` // 省份
	District string `json:"district,omitempty"` // 区域
	Plate    string `json:"plate,omitempty"`    // 板块
	Landmark string `json:"landmark,omitempty"` // 地标
}

// RoomLayoutInfo 户型信息
type RoomLayoutInfo struct {
	Bedrooms    int    `json:"bedrooms,omitempty"`     // 卧室数
	LivingRooms int    `json:"living_rooms,omitempty"` // 客厅数
	Description string `json:"description,omitempty"`  // 描述，如"3室2厅"
}

// PriceInfo 价格信息
type PriceInfo struct {
	Min      float64 `json:"min,omitempty"`      // 最小价格
	Max      float64 `json:"max,omitempty"`      // 最大价格
	Operator string  `json:"operator,omitempty"` // 操作符，如"<="
	Unit     string  `json:"unit,omitempty"`     // 单位，如"万"
}

// AreaInfo 面积信息
type AreaInfo struct {
	Value    float64 `json:"value,omitempty"`    // 面积值
	Min      float64 `json:"min,omitempty"`      // 最小面积
	Max      float64 `json:"max,omitempty"`      // 最大面积
	Operator string  `json:"operator,omitempty"` // 操作符，如"about"、">="、"<="
	Unit     string  `json:"unit,omitempty"`     // 单位，如"平米"
}

// SubwayInfo 地铁信息
type SubwayInfo struct {
	Line    string `json:"line,omitempty"`    // 线路，如"13号线"
	Station string `json:"station,omitempty"` // 站点，如"华府大道站"
	Nearby  bool   `json:"nearby,omitempty"`  // 是否邻近地铁
}

// KeywordExtractionResult 关键字提取结果
type KeywordExtractionResult struct {
	// 每个维度的提取结果（使用具体类型）
	Location       []LocationInfo   `json:"location,omitempty"`
	Decoration     []string         `json:"decoration,omitempty"`
	PropertyType   []string         `json:"property_type,omitempty"`
	RoomLayout     []RoomLayoutInfo `json:"room_layout,omitempty"`
	Price          []PriceInfo      `json:"price,omitempty"`
	RentPrice      []PriceInfo      `json:"rent_price,omitempty"`
	Area           []AreaInfo       `json:"area,omitempty"`
	InterestPoints []string         `json:"interest_points,omitempty"`
	Commercial     []string         `json:"commercial,omitempty"`
	Orientation    []string         `json:"orientation,omitempty"`
	Subway         []SubwayInfo     `json:"subway,omitempty"`

	// 原始消息列表
	Messages []string `json:"messages"`

	// 每条消息的提取详情
	MessageResults []MessageExtractionResult `json:"message_results"`
}

// MessageExtractionResult 单条消息的提取结果
type MessageExtractionResult struct {
	Message    string                           `json:"message"`
	Dimensions map[KeywordDimension]interface{} `json:"dimensions"`
	Entities   []keywords.Entity                `json:"entities"`
}

// RecorderProducer Kafka Producer interface for recorder
type RecorderProducer interface {
	Produce(topic string, key, value []byte, headers []kafka.Header) error
}

// KeywordExtractorService 关键字提取服务
type KeywordExtractorService struct {
	extractor        *keywords.AdvancedFieldExtractor
	recorderTopic    string
	recorderProducer RecorderProducer
}

// NewKeywordExtractorService 创建关键字提取服务
func NewKeywordExtractorService(recorderProducer RecorderProducer, recorderTopic string) *KeywordExtractorService {
	return &KeywordExtractorService{
		extractor:        keywords.NewAdvancedFieldExtractor(),
		recorderProducer: recorderProducer,
		recorderTopic:    recorderTopic,
	}
}

// TestParseSubway 测试辅助方法，用于验证parseSubway的回退机制
func (s *KeywordExtractorService) TestParseSubway(value interface{}) *SubwayInfo {
	return parseSubway(value)
}

// ExtractFromConversationHistory 从对话历史中提取关键字
// 只提取用户消息（role="user"）
func (s *KeywordExtractorService) ExtractFromConversationHistory(history []minimax.Message) (*KeywordExtractionResult, error) {
	if s.extractor == nil {
		return nil, fmt.Errorf("keyword extractor not initialized")
	}

	// Jenny式修改：租售过滤已在extractor层通过谓语检查完成
	// 删除服务层的重复检查，避免冗余和潜在的逻辑冲突

	result := &KeywordExtractionResult{
		Location:       make([]LocationInfo, 0),
		Decoration:     make([]string, 0),
		PropertyType:   make([]string, 0),
		RoomLayout:     make([]RoomLayoutInfo, 0),
		Price:          make([]PriceInfo, 0),
		RentPrice:      make([]PriceInfo, 0),
		Area:           make([]AreaInfo, 0),
		InterestPoints: make([]string, 0),
		Commercial:     make([]string, 0),
		Orientation:    make([]string, 0),
		Subway:         make([]SubwayInfo, 0),
		Messages:       make([]string, 0),
		MessageResults: make([]MessageExtractionResult, 0),
	}

	// 初始化去重集合
	locationSeen := make(map[string]bool)
	roomLayoutSeen := make(map[string]bool)
	priceSeen := make(map[string]bool)
	areaSeen := make(map[string]bool)
	stringSeen := make(map[string]bool) // 用于字符串类型的去重

	// 遍历历史消息，只处理用户消息
	for _, msg := range history {
		if msg.Role != "user" {
			continue
		}

		// 获取消息内容
		content := extractMessageContent(msg)
		if content == "" {
			continue
		}

		result.Messages = append(result.Messages, content)

		// Jenny式改进：使用带超时控制的提取方法，防止单条消息处理时间过长
		// 设置5秒超时，足够处理复杂消息但避免无限等待
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		extractionResult, err := s.extractor.ExtractFromMessageWithContext(ctx, content)
		cancel() // 立即释放资源

		if err != nil {
			// 区分超时错误和其他错误，便于监控和问题定位
			if errors.Is(err, context.DeadlineExceeded) {
				// 截取消息前50个字符用于日志，避免日志过长
				logContent := content
				if len(content) > 50 {
					logContent = content[:50] + "..."
				}
				log.GetInstance().Sugar.Warnf("Keyword extraction timeout for message: %s", logContent)
			} else {
				log.GetInstance().Sugar.Warn("Failed to extract keywords from message: ", content, ", error: ", err)
			}
			continue
		}

		// 创建单条消息的结果
		msgResult := MessageExtractionResult{
			Message:    content,
			Dimensions: make(map[KeywordDimension]interface{}),
			Entities:   extractionResult.Entities,
		}

		// 处理提取的字段
		for fieldName, fieldValue := range extractionResult.Fields {
			s.processExtractedField(fieldName, fieldValue, result, msgResult,
				locationSeen, roomLayoutSeen, priceSeen, areaSeen, stringSeen)
		}

		result.MessageResults = append(result.MessageResults, msgResult)
	}

	return result, nil
}

// processExtractedField 处理提取的字段
func (s *KeywordExtractorService) processExtractedField(
	fieldName string,
	fieldValue interface{},
	result *KeywordExtractionResult,
	msgResult MessageExtractionResult,
	locationSeen, roomLayoutSeen, priceSeen, areaSeen, stringSeen map[string]bool,
) {
	switch fieldName {
	case "location":
		if loc := parseLocation(fieldValue); loc != nil {
			key := fmt.Sprintf("%s_%s_%s", loc.Province, loc.District, loc.Landmark)
			if !locationSeen[key] {
				locationSeen[key] = true
				result.Location = append(result.Location, *loc)
			}
			msgResult.Dimensions[DimensionLocation] = loc
		}

	case "decoration":
		decorations := parseStringArray(fieldValue)
		for _, dec := range decorations {
			key := fmt.Sprintf("decoration_%s", dec)
			if !stringSeen[key] {
				stringSeen[key] = true
				result.Decoration = append(result.Decoration, dec)
			}
		}
		if len(decorations) > 0 {
			msgResult.Dimensions[DimensionDecoration] = decorations
		}

	case "property_type":
		if propType := parseString(fieldValue); propType != "" {
			key := fmt.Sprintf("property_type_%s", propType)
			if !stringSeen[key] {
				stringSeen[key] = true
				result.PropertyType = append(result.PropertyType, propType)
			}
			msgResult.Dimensions[DimensionPropertyType] = propType
		}

	case "room_layout":
		if layout := parseRoomLayout(fieldValue); layout != nil {
			key := fmt.Sprintf("%d_%d_%s", layout.Bedrooms, layout.LivingRooms, layout.Description)
			if !roomLayoutSeen[key] {
				roomLayoutSeen[key] = true
				result.RoomLayout = append(result.RoomLayout, *layout)
			}
			msgResult.Dimensions[DimensionRoomLayout] = layout
		}

	case "price":
		if price := parsePrice(fieldValue); price != nil {
			key := fmt.Sprintf("%.2f_%.2f_%s_%s", price.Min, price.Max, price.Operator, price.Unit)
			if !priceSeen[key] {
				priceSeen[key] = true
				result.Price = append(result.Price, *price)
			}
			msgResult.Dimensions[DimensionPrice] = price
		}

	case "rent_price":
		if price := parsePrice(fieldValue); price != nil {
			key := fmt.Sprintf("rent_%.2f_%.2f_%s_%s", price.Min, price.Max, price.Operator, price.Unit)
			if !priceSeen[key] {
				priceSeen[key] = true
				result.RentPrice = append(result.RentPrice, *price)
			}
			msgResult.Dimensions[DimensionRentPrice] = price
		}

	case "area":
		if area := parseArea(fieldValue); area != nil {
			key := fmt.Sprintf("%.2f_%s", area.Value, area.Unit)
			if !areaSeen[key] {
				areaSeen[key] = true
				result.Area = append(result.Area, *area)
			}
			msgResult.Dimensions[DimensionArea] = area
		}

	case "interest_points":
		points := parseStringArray(fieldValue)
		for _, point := range points {
			key := fmt.Sprintf("interest_%s", point)
			if !stringSeen[key] {
				stringSeen[key] = true
				result.InterestPoints = append(result.InterestPoints, point)
			}
		}
		if len(points) > 0 {
			msgResult.Dimensions[DimensionInterestPoints] = points
		}

	case "commercial":
		if comm := parseString(fieldValue); comm != "" {
			key := fmt.Sprintf("commercial_%s", comm)
			if !stringSeen[key] {
				stringSeen[key] = true
				result.Commercial = append(result.Commercial, comm)
			}
			msgResult.Dimensions[DimensionCommercial] = comm
		}

	case "orientation":
		if orient := parseString(fieldValue); orient != "" {
			key := fmt.Sprintf("orientation_%s", orient)
			if !stringSeen[key] {
				stringSeen[key] = true
				result.Orientation = append(result.Orientation, orient)
			}
			msgResult.Dimensions[DimensionOrientation] = orient
		}

	case "subway":
		if subway := parseSubway(fieldValue); subway != nil {
			// 使用线路和站点作为唯一键
			key := fmt.Sprintf("subway_%s_%s", subway.Line, subway.Station)
			if !stringSeen[key] {
				stringSeen[key] = true
				result.Subway = append(result.Subway, *subway)
			}
			msgResult.Dimensions[DimensionSubway] = subway
		}
	}
}

// PrintExtractionResult 打印提取结果
func (s *KeywordExtractorService) PrintExtractionResult(result *KeywordExtractionResult) {
	log.GetInstance().Sugar.Info("=== 关键字提取结果 ===")
	log.GetInstance().Sugar.Info("处理消息数: ", len(result.Messages))

	// 打印每个维度的汇总结果
	log.GetInstance().Sugar.Info("\n维度汇总:")

	if len(result.Location) > 0 {
		log.GetInstance().Sugar.Info("  location: ", result.Location)
	}
	if len(result.Decoration) > 0 {
		log.GetInstance().Sugar.Info("  decoration: ", result.Decoration)
	}
	if len(result.PropertyType) > 0 {
		log.GetInstance().Sugar.Info("  property_type: ", result.PropertyType)
	}
	if len(result.RoomLayout) > 0 {
		log.GetInstance().Sugar.Info("  room_layout: ", result.RoomLayout)
	}
	if len(result.Price) > 0 {
		log.GetInstance().Sugar.Info("  price: ", result.Price)
	}
	if len(result.RentPrice) > 0 {
		log.GetInstance().Sugar.Info("  rent_price: ", result.RentPrice)
	}
	if len(result.Area) > 0 {
		log.GetInstance().Sugar.Info("  area: ", result.Area)
	}
	if len(result.InterestPoints) > 0 {
		log.GetInstance().Sugar.Info("  interest_points: ", result.InterestPoints)
	}
	if len(result.Commercial) > 0 {
		log.GetInstance().Sugar.Info("  commercial: ", result.Commercial)
	}
	if len(result.Orientation) > 0 {
		log.GetInstance().Sugar.Info("  orientation: ", result.Orientation)
	}
	if len(result.Subway) > 0 {
		log.GetInstance().Sugar.Info("  subway: ", result.Subway)
	}

	// 打印每条消息的详细结果
	log.GetInstance().Sugar.Info("\n消息详情:")
	for i, msgResult := range result.MessageResults {
		log.GetInstance().Sugar.Info("  消息", i+1, ": ", msgResult.Message)
		if len(msgResult.Dimensions) > 0 {
			for dim, value := range msgResult.Dimensions {
				log.GetInstance().Sugar.Info("    - ", string(dim), ": ", value)
			}
		}
		if len(msgResult.Entities) > 0 {
			log.GetInstance().Sugar.Info("    - 实体: ", len(msgResult.Entities), "个")
		}
	}
}

// 类型转换函数

// 地标清理相关的前缀配置
var (
	landmarkCleanPrefixes = []string{
		"找一个", "找个", "找",
		"我想了解一下", "我想了解", "了解一下", "了解",
		"我想在", "我想找", "我想要", "我想",
		"靠近", "在", "到", "去", "来",
		"请问", "请", "帮我找", "帮我",
		"有没有", "有无", "有",
		"想要", "要",
	}

	landmarkLocationPrefixes = []string{
		"陆家嘴", "浦东新区", "浦东", "徐汇", "静安", "黄浦", "虹口", "杨浦", "闵行", "宝山", "嘉定",
		"高新区", "成都高新区", "天府新区", "成都天府新区", "成都",
		"北京", "上海", "广州", "深圳", "杭州", "南京", "武汉", "重庆",
	}
)

// cleanLandmark 清理地标名称，去除无关前缀
func cleanLandmark(landmark string) string {
	if landmark == "" {
		return ""
	}

	cleaned := landmark
	for _, prefix := range landmarkCleanPrefixes {
		if len(cleaned) > len(prefix) && cleaned[:len(prefix)] == prefix {
			cleaned = cleaned[len(prefix):]
			break // 只移除第一个匹配的前缀
		}
	}

	// 去除地名前缀（如果这些地名已经在province或district中）
	for _, locPrefix := range landmarkLocationPrefixes {
		if len(cleaned) > len(locPrefix) && cleaned[:len(locPrefix)] == locPrefix {
			cleaned = cleaned[len(locPrefix):]
			break
		}
	}

	// 如果清理后为空或者只剩下省市区名称，返回空
	if cleaned == "" || cleaned == "成都" || cleaned == "上海" ||
		cleaned == "高新区" || cleaned == "浦东" || cleaned == "陆家嘴" ||
		cleaned == "天府新区" || cleaned == "成都高新区" || cleaned == "新区" ||
		cleaned == "浦东新区" {
		return ""
	}

	return cleaned
}

// parseLocation 解析地段信息
func parseLocation(value interface{}) *LocationInfo {
	if value == nil {
		return nil
	}

	// 处理map类型
	if m, ok := value.(map[string]interface{}); ok {
		loc := &LocationInfo{}
		if province, ok := m["province"].(string); ok {
			loc.Province = province
		}
		if district, ok := m["district"].(string); ok {
			loc.District = district
		}
		if plate, ok := m["plate"].(string); ok {
			loc.Plate = plate
		}
		if landmark, ok := m["landmark"].(string); ok {
			// 清理landmark字段
			loc.Landmark = cleanLandmark(landmark)
		}
		// 如果有任何一个字段有值，返回结果
		if loc.Province != "" || loc.District != "" || loc.Plate != "" || loc.Landmark != "" {
			return loc
		}
	}

	return nil
}

// parseRoomLayout 解析户型信息
func parseRoomLayout(value interface{}) *RoomLayoutInfo {
	if value == nil {
		return nil
	}

	// 处理map类型
	if m, ok := value.(map[string]interface{}); ok {
		layout := &RoomLayoutInfo{}

		// 处理bedrooms
		if bedrooms, ok := m["bedrooms"]; ok {
			switch v := bedrooms.(type) {
			case float64:
				layout.Bedrooms = int(v)
			case int:
				layout.Bedrooms = v
			}
		}

		// 处理living_rooms
		if livingRooms, ok := m["living_rooms"]; ok {
			switch v := livingRooms.(type) {
			case float64:
				layout.LivingRooms = int(v)
			case int:
				layout.LivingRooms = v
			}
		}

		// 处理description
		if desc, ok := m["description"].(string); ok {
			layout.Description = desc
		}

		// 如果有任何字段有值，返回结果
		if layout.Bedrooms > 0 || layout.LivingRooms > 0 || layout.Description != "" {
			return layout
		}
	}

	return nil
}

// parsePrice 解析价格信息
func parsePrice(value interface{}) *PriceInfo {
	if value == nil {
		return nil
	}

	// 处理map类型
	if m, ok := value.(map[string]interface{}); ok {
		price := &PriceInfo{}

		// 处理min
		if min, ok := m["min"]; ok {
			switch v := min.(type) {
			case float64:
				price.Min = v
			case int:
				price.Min = float64(v)
			}
		}

		// 处理max
		if max, ok := m["max"]; ok {
			switch v := max.(type) {
			case float64:
				price.Max = v
			case int:
				price.Max = float64(v)
			}
		}

		// 处理operator
		if op, ok := m["operator"].(string); ok {
			price.Operator = op
		}

		// 处理unit
		if unit, ok := m["unit"].(string); ok {
			price.Unit = unit
		} else {
			price.Unit = "万" // 默认单位
		}

		// 处理value字段（用于单个价格值，如"200万左右"）
		if val, ok := m["value"]; ok {
			switch v := val.(type) {
			case float64:
				// 根据operator决定如何设置Min/Max
				if price.Operator == "about" {
					// "左右"不设置具体的Min/Max，交给转换层处理
					// 但为了去重key的生成，使用value作为min
					price.Min = v
				} else if price.Operator == ">=" {
					price.Min = v
				} else if price.Operator == "<=" {
					price.Max = v
				} else {
					// 默认作为精确值
					price.Min = v
					price.Max = v
				}
			case int:
				floatValue := float64(v)
				if price.Operator == "about" {
					price.Min = floatValue
				} else if price.Operator == ">=" {
					price.Min = floatValue
				} else if price.Operator == "<=" {
					price.Max = floatValue
				} else {
					price.Min = floatValue
					price.Max = floatValue
				}
			}
		}

		// 如果有价格值，返回结果
		if price.Min > 0 || price.Max > 0 {
			return price
		}
	}

	return nil
}

// parseArea 解析面积信息
func parseArea(value interface{}) *AreaInfo {
	if value == nil {
		return nil
	}

	// 处理map类型
	if m, ok := value.(map[string]interface{}); ok {
		area := &AreaInfo{}

		// 处理value
		if val, ok := m["value"]; ok {
			switch v := val.(type) {
			case float64:
				area.Value = v
			case int:
				area.Value = float64(v)
			}
		}

		// 处理min
		if min, ok := m["min"]; ok {
			switch v := min.(type) {
			case float64:
				area.Min = v
			case int:
				area.Min = float64(v)
			}
		}

		// 处理max
		if max, ok := m["max"]; ok {
			switch v := max.(type) {
			case float64:
				area.Max = v
			case int:
				area.Max = float64(v)
			}
		}

		// 处理operator
		if op, ok := m["operator"].(string); ok {
			area.Operator = op
		}

		// 处理unit
		if unit, ok := m["unit"].(string); ok {
			area.Unit = unit
		} else {
			area.Unit = "平米" // 默认单位
		}

		// 如果有面积值或范围，返回结果
		if area.Value > 0 || area.Min > 0 || area.Max > 0 {
			return area
		}
	}

	return nil
}

// parseString 解析字符串类型
func parseString(value interface{}) string {
	if value == nil {
		return ""
	}

	var result string
	switch v := value.(type) {
	case string:
		result = v
	case []interface{}:
		// 如果是数组，取第一个元素
		if len(v) > 0 {
			if str, ok := v[0].(string); ok {
				result = str
			}
		}
	case []string:
		// 如果是字符串数组，取第一个元素
		if len(v) > 0 {
			result = v[0]
		}
	}

	// 过滤空字符串
	if result == "" {
		return ""
	}
	return result
}

// parseSubway 解析地铁信息
func parseSubway(value interface{}) *SubwayInfo {
	if value == nil {
		return nil
	}

	// 处理map类型（优先）
	if m, ok := value.(map[string]interface{}); ok {
		subway := &SubwayInfo{}

		// 处理line字段
		if line, ok := m["line"].(string); ok {
			subway.Line = line
		}

		// 处理station字段
		if station, ok := m["station"].(string); ok {
			subway.Station = station
		}

		// 处理nearby字段
		if nearby, ok := m["nearby"].(bool); ok {
			subway.Nearby = nearby
		}

		// 如果有任何字段有值，返回结果
		if subway.Line != "" || subway.Station != "" || subway.Nearby {
			return subway
		}
	}

	// 处理字符串类型（回退机制）
	if str, ok := value.(string); ok && str != "" {
		subway := &SubwayInfo{}

		// 尝试从字符串中提取线路信息
		if strings.Contains(str, "号线") {
			// 提取线路号，如 "13号线" -> "13号线"
			if idx := strings.Index(str, "号线"); idx > 0 {
				// 向前查找数字或中文数字
				start := idx - 1
				for start >= 0 && (unicode.IsDigit(rune(str[start])) ||
					strings.ContainsRune("一二三四五六七八九十", rune(str[start]))) {
					start--
				}
				if start < idx-1 {
					lineNum := str[start+1 : idx]
					// 中文数字转换
					lineNum = common.ConvertChineseToArabic(lineNum)
					subway.Line = lineNum + "号线"
				}
			}
		}

		// 检查是否包含站点信息
		if strings.Contains(str, "站") && !strings.Contains(str, "号线站") {
			// 简单提取站名（可能不准确，但聊胜于无）
			if idx := strings.LastIndex(str, "站"); idx > 0 {
				// 向前查找直到遇到非中文字符或数字
				start := idx - 1
				for start >= 0 && (unicode.Is(unicode.Han, rune(str[start])) ||
					unicode.IsDigit(rune(str[start]))) {
					start--
				}
				if start < idx-1 {
					station := str[start+1 : idx+1]
					if station != "站" && !strings.Contains(station, "地铁") {
						subway.Station = station
					}
				}
			}
		}

		// 检查是否是"近地铁"类的表述
		if strings.Contains(str, "近地铁") || strings.Contains(str, "靠近地铁") ||
			strings.Contains(str, "地铁附近") || strings.Contains(str, "地铁旁") {
			subway.Nearby = true
		}

		// 如果提取到任何信息，返回结果
		if subway.Line != "" || subway.Station != "" || subway.Nearby {
			return subway
		}
	}

	return nil
}

// parseStringArray 解析字符串数组
func parseStringArray(value interface{}) []string {
	if value == nil {
		return []string{}
	}

	result := make([]string, 0)

	switch v := value.(type) {
	case string:
		// 单个字符串转换为数组（过滤空字符串）
		if v != "" {
			result = append(result, v)
		}
	case []string:
		// 过滤空字符串
		for _, s := range v {
			if s != "" {
				result = append(result, s)
			}
		}
	case []interface{}:
		// interface数组转换为字符串数组（过滤空字符串）
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				result = append(result, str)
			}
		}
	default:
		// 尝试通过反射处理其他类型
		rv := reflect.ValueOf(value)
		if rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				if str := fmt.Sprintf("%v", rv.Index(i).Interface()); str != "" {
					result = append(result, str)
				}
			}
		}
	}

	return result
}

// extractMessageContent 从Message中提取内容字符串
func extractMessageContent(msg minimax.Message) string {
	switch v := msg.Content.(type) {
	case string:
		return v
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return text
		}
	}
	return ""
}

// getAllDimensions 获取所有维度
func getAllDimensions() []KeywordDimension {
	return []KeywordDimension{
		DimensionLocation,
		DimensionDecoration,
		DimensionPropertyType,
		DimensionRoomLayout,
		DimensionPrice,
		DimensionRentPrice,
		DimensionArea,
		DimensionInterestPoints,
		DimensionCommercial,
		DimensionOrientation,
		DimensionSubway,
	}
}

// sendQAsToRecorder sends conversation QA pairs to Kafka recorder topic
func (s *KeywordExtractorService) sendQAsToRecorder(ctx context.Context, userID string, rawHistory []minimax.RawMsg) {
	if s.recorderProducer == nil {
		return
	}

	// 使用公共方法构建问答对（AI 消息作为 Q，用户消息作为 A）
	qas := BuildQAPairs(rawHistory, nil)

	if len(qas) == 0 {
		return
	}

	val, err := json.Marshal(qas)
	if err != nil {
		log.GetInstance().Sugar.Warnf("marshal QAs failed: %v", err)
		return
	}

	occurred := utils.Now().Unix()
	key := []byte(userID)
	headers := []kafka.Header{
		{Key: "schema", Value: []byte("ipang.qa.v1")},
		{Key: "user_id", Value: []byte(userID)},
		{Key: "occurred", Value: []byte(strconv.FormatInt(occurred, 10))},
		{Key: "qa_pairs", Value: []byte(strconv.Itoa(len(qas)))},
		{Key: "produced_at", Value: []byte(strconv.FormatInt(utils.Now().Unix(), 10))},
	}

	if err := s.recorderProducer.Produce(s.recorderTopic, key, val, headers); err != nil {
		log.GetInstance().Sugar.Errorf("produce recorder batch failed: %v", err)
		return
	}
}

// ExtractAndCallIpangAPI 提取关键字并调用Ipang API
func (s *KeywordExtractorService) ExtractAndCallIpangAPI(userID, openKFID string, history []minimax.Message, rawHistory []minimax.RawMsg) {
	log.GetInstance().Sugar.Info("Starting ExtractAndCallIpangAPI for user: ", userID)

	// 1. 提取关键字
	result, err := s.ExtractFromConversationHistory(history)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to extract keywords for user ", userID, ": ", err)
		return
	}

	// 2. 打印提取结果
	s.PrintExtractionResult(result)

	// 3. 转换为Ipang API参数
	adapter := NewIpangAdapter()
	transformResult := adapter.Transform(result, rawHistory)

	log.GetInstance().Sugar.Info("========== Ipang API 参数转换 ==========")
	log.GetInstance().Sugar.Infof("转换后的参数: %+v", transformResult.Params)
	if len(transformResult.PostFilters) > 0 {
		log.GetInstance().Sugar.Infof("后置过滤器: %+v", transformResult.PostFilters)
	}
	if len(transformResult.Warnings) > 0 {
		log.GetInstance().Sugar.Warnf("未映射维度: %v", transformResult.Warnings)
	}

	// 3.5. Send QAs to recorder before calling Ipang
	s.sendQAsToRecorder(context.Background(), userID, rawHistory)

	// 4. 调用Ipang POST API（支持诊断字段）
	ipangClient := ipang.NewClient()
	postResp, err := ipangClient.PostQuery(transformResult.Params)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to call Ipang API for user ", userID, ": ", err)
		return
	}

	// 5. 打印Ipang API返回结果
	log.GetInstance().Sugar.Info("========== Ipang API 返回结果 ==========")
	log.GetInstance().Sugar.Infof("新房数量: %d", len(postResp.Detail.NewHouse))
	log.GetInstance().Sugar.Infof("二手房数量: %d", len(postResp.Detail.SecHouse))

	// 打印新房信息
	if len(postResp.Detail.NewHouse) > 0 {
		log.GetInstance().Sugar.Info("【新房】")
		for i, house := range postResp.Detail.NewHouse {
			if i >= 3 { // 只打印前3个
				break
			}
			log.GetInstance().Sugar.Infof("  %s (%s %s)", house.Title, house.Area, house.Plate)
			for j, item := range house.List {
				if j >= 2 { // 每个楼盘只打印前2个户型
					break
				}
				log.GetInstance().Sugar.Infof("    - %s: %s㎡, %s万, %s",
					item.PanTitle, item.SizeRange, item.Amount, item.HouseType)
			}
		}
	}

	// 打印二手房信息
	if len(postResp.Detail.SecHouse) > 0 {
		log.GetInstance().Sugar.Info("【二手房】")
		for i, house := range postResp.Detail.SecHouse {
			if i >= 3 { // 只打印前3个
				break
			}
			log.GetInstance().Sugar.Infof("  %s (%s %s)", house.Title, house.Area, house.Plate)
			for j, item := range house.List {
				if j >= 2 { // 每个小区只打印前2个房源
					break
				}
				// 处理二手房的float64类型
				var sizeStr, amountStr string
				if size, ok := item.SizeRange.(float64); ok {
					sizeStr = fmt.Sprintf("%.0f", size)
				} else {
					sizeStr = fmt.Sprintf("%v", item.SizeRange)
				}
				if amount, ok := item.Amount.(float64); ok {
					amountStr = fmt.Sprintf("%.0f", amount)
				} else {
					amountStr = fmt.Sprintf("%v", item.Amount)
				}
				log.GetInstance().Sugar.Infof("    - %s㎡, %s万, %s",
					sizeStr, amountStr, item.HouseType)
			}
		}
	}

	// 6. 处理Ipang API返回结果并发送PDF给用户
	if err := s.handleIpangPostResultAndSendPDF(userID, openKFID, postResp); err != nil {
		log.GetInstance().Sugar.Error("Failed to handle Ipang result and send PDF for user ", userID, ": ", err)
	}

	log.GetInstance().Sugar.Info("====================================")
	log.GetInstance().Sugar.Info("ExtractAndCallIpangAPI completed for user: ", userID)
}

// CallIpangAPIFromKeywords 直接从聚合的关键词调用Ipang API（4.3 并发架构优化）
// 避免重复提取，直接使用processor聚合的关键词结果
func (s *KeywordExtractorService) CallIpangAPIFromKeywords(userID, openKFID string, aggregatedKeywords map[string]interface{}, rawHistory []minimax.RawMsg) {
	log.GetInstance().Sugar.Info("Starting CallIpangAPIFromKeywords for user: ", userID, ", dimensions: ", len(aggregatedKeywords))

	// 1. 转换聚合关键词为KeywordExtractionResult格式（简单实用原则）
	result := &KeywordExtractionResult{
		Location:       make([]LocationInfo, 0),
		Decoration:     make([]string, 0),
		PropertyType:   make([]string, 0),
		RoomLayout:     make([]RoomLayoutInfo, 0),
		Price:          make([]PriceInfo, 0),
		RentPrice:      make([]PriceInfo, 0),
		Area:           make([]AreaInfo, 0),
		InterestPoints: make([]string, 0),
		Commercial:     make([]string, 0),
		Orientation:    make([]string, 0),
		Subway:         make([]SubwayInfo, 0),
		Messages:       make([]string, 0), // 标记为聚合结果
	}

	// 转换聚合关键词到具体字段（简化版本，基本覆盖主要维度）
	s.convertAggregatedKeywords(aggregatedKeywords, result)

	// 2. 复用现有流程：打印结果
	s.PrintExtractionResult(result)

	// 3. 复用现有流程：转换为Ipang API参数
	adapter := NewIpangAdapter()
	transformResult := adapter.Transform(result, rawHistory)

	log.GetInstance().Sugar.Info("========== Ipang API 参数转换 ==========")
	log.GetInstance().Sugar.Infof("转换后的参数: %+v", transformResult.Params)
	if len(transformResult.PostFilters) > 0 {
		log.GetInstance().Sugar.Infof("后置过滤器: %+v", transformResult.PostFilters)
	}

	// 3.5. Send QAs to recorder before calling Ipang
	s.sendQAsToRecorder(context.Background(), userID, rawHistory)

	// 4. 复用现有流程：调用Ipang POST API
	ipangClient := ipang.NewClient()
	response, err := ipangClient.PostQuery(transformResult.Params)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to call Ipang API for user ", userID, ": ", err)
		return
	}

	log.GetInstance().Sugar.Info("Ipang API call successful for user: ", userID)

	// 5. 复用现有流程：处理结果并发送PDF
	if err := s.handleIpangPostResultAndSendPDF(userID, openKFID, response); err != nil {
		log.GetInstance().Sugar.Error("Failed to handle Ipang result and send PDF for user ", userID, ": ", err)
		return
	}

	log.GetInstance().Sugar.Info("CallIpangAPIFromKeywords completed for user: ", userID)
}

// convertAggregatedKeywords 转换聚合关键词到KeywordExtractionResult字段
// 简单实用原则：基本类型转换，复杂类型基础解析
func (s *KeywordExtractorService) convertAggregatedKeywords(aggregated map[string]interface{}, result *KeywordExtractionResult) {
	// 字符串数组类型的维度
	if decoration, ok := aggregated["decoration"].([]string); ok {
		result.Decoration = decoration
	}
	if propertyType, ok := aggregated["property_type"].([]string); ok {
		result.PropertyType = propertyType
	}
	if interestPoints, ok := aggregated["interest_points"].([]string); ok {
		result.InterestPoints = interestPoints
	}
	if commercial, ok := aggregated["commercial"].([]string); ok {
		result.Commercial = commercial
	}
	if orientation, ok := aggregated["orientation"].([]string); ok {
		result.Orientation = orientation
	}

	// 复杂类型基础转换（简单实用原则）
	if locationData, exists := aggregated["location"]; exists {
		if locations := s.parseLocationData(locationData); len(locations) > 0 {
			result.Location = locations
		}
	}

	if subwayData, exists := aggregated["subway"]; exists {
		if subways := s.parseSubwayData(subwayData); len(subways) > 0 {
			result.Subway = subways
		}
	}

	if priceData, exists := aggregated["price"]; exists {
		if prices := s.parsePriceData(priceData); len(prices) > 0 {
			result.Price = prices
		}
	}

	if rentPriceData, exists := aggregated["rent_price"]; exists {
		if rentPrices := s.parsePriceData(rentPriceData); len(rentPrices) > 0 {
			result.RentPrice = rentPrices
		}
	}

	if areaData, exists := aggregated["area"]; exists {
		if areas := s.parseAreaData(areaData); len(areas) > 0 {
			result.Area = areas
		}
	}

	if roomData, exists := aggregated["room_layout"]; exists {
		if rooms := s.parseRoomLayoutData(roomData); len(rooms) > 0 {
			result.RoomLayout = rooms
		}
	}

	log.GetInstance().Sugar.Debug("Converted aggregated keywords to KeywordExtractionResult for processing")
}

// parseSubwayData 解析地铁数据（简单实用版本）
func (s *KeywordExtractorService) parseSubwayData(data interface{}) []SubwayInfo {
	switch v := data.(type) {
	case map[string]interface{}:
		// 单个subway map
		subway := SubwayInfo{}
		if line, ok := v["line"].(string); ok {
			subway.Line = line
		}
		if station, ok := v["station"].(string); ok {
			subway.Station = station
		}
		if nearby, ok := v["nearby"].(bool); ok {
			subway.Nearby = nearby
		}
		// 如果有任何字段有值，返回结果
		if subway.Line != "" || subway.Station != "" || subway.Nearby {
			return []SubwayInfo{subway}
		}
	case []interface{}:
		// subway数组，取最后一个有效的（统一"最新为准"策略）
		for i := len(v) - 1; i >= 0; i-- {
			if itemMap, ok := v[i].(map[string]interface{}); ok {
				if subways := s.parseSubwayData(itemMap); len(subways) > 0 {
					return subways
				}
			}
		}
	}
	return []SubwayInfo{}
}

// parseLocationData 解析位置数据（简单实用版本）
func (s *KeywordExtractorService) parseLocationData(data interface{}) []LocationInfo {
	switch v := data.(type) {
	case map[string]interface{}:
		// 单个location map
		location := LocationInfo{}
		if province, ok := v["province"].(string); ok {
			location.Province = province
		}
		if district, ok := v["district"].(string); ok {
			location.District = district
		}
		if plate, ok := v["plate"].(string); ok {
			location.Plate = plate
		}
		if landmark, ok := v["landmark"].(string); ok {
			location.Landmark = landmark
		}
		return []LocationInfo{location}
	case []interface{}:
		// location数组，取最后一个有效的（统一"最新为准"策略）
		for i := len(v) - 1; i >= 0; i-- {
			if itemMap, ok := v[i].(map[string]interface{}); ok {
				if locations := s.parseLocationData(itemMap); len(locations) > 0 {
					return locations
				}
			}
		}
	}
	return []LocationInfo{}
}

// parsePriceData 解析价格数据（简单实用版本）
func (s *KeywordExtractorService) parsePriceData(data interface{}) []PriceInfo {
	switch v := data.(type) {
	case map[string]interface{}:
		// 单个price map
		price := PriceInfo{}
		if min, ok := v["min"].(float64); ok {
			price.Min = min
		}
		if max, ok := v["max"].(float64); ok {
			price.Max = max
		}
		if value, ok := v["value"].(float64); ok {
			// 处理单值价格，根据operator推导min/max
			if operator, ok := v["operator"].(string); ok {
				switch operator {
				case "about":
					// 约等于：仅设Min，transform时会加±20%容差
					price.Min = value
				case ">=":
					// 大于等于：设置最小值
					price.Min = value
				case "<=":
					// 小于等于：设置最大值
					price.Max = value
				default:
					// 其他情况：min=max=value
					price.Min = value
					price.Max = value
				}
				price.Operator = operator
			} else {
				// 无operator，默认等于
				price.Min = value
				price.Max = value
			}
		}
		if operator, ok := v["operator"].(string); ok {
			price.Operator = operator
		}
		if unit, ok := v["unit"].(string); ok {
			price.Unit = unit
		}
		return []PriceInfo{price}
	case []interface{}:
		// price数组，取最后一个有效的（统一"最新为准"策略）
		for i := len(v) - 1; i >= 0; i-- {
			if itemMap, ok := v[i].(map[string]interface{}); ok {
				if prices := s.parsePriceData(itemMap); len(prices) > 0 {
					return prices
				}
			}
		}
	}
	return []PriceInfo{}
}

// parseAreaData 解析面积数据（简单实用版本）
func (s *KeywordExtractorService) parseAreaData(data interface{}) []AreaInfo {
	switch v := data.(type) {
	case map[string]interface{}:
		// 单个area map
		area := AreaInfo{}
		if min, ok := v["min"].(float64); ok {
			area.Min = min
		}
		if max, ok := v["max"].(float64); ok {
			area.Max = max
		}
		if value, ok := v["value"].(float64); ok {
			area.Value = value
		}
		if operator, ok := v["operator"].(string); ok {
			area.Operator = operator
		}
		if unit, ok := v["unit"].(string); ok {
			area.Unit = unit
		}
		return []AreaInfo{area}
	case []interface{}:
		// area数组，取最后一个有效的（统一"最新为准"策略）
		for i := len(v) - 1; i >= 0; i-- {
			if itemMap, ok := v[i].(map[string]interface{}); ok {
				if areas := s.parseAreaData(itemMap); len(areas) > 0 {
					return areas
				}
			}
		}
	}
	return []AreaInfo{}
}

// parseRoomLayoutData 解析户型数据（简单实用版本）
func (s *KeywordExtractorService) parseRoomLayoutData(data interface{}) []RoomLayoutInfo {
	switch v := data.(type) {
	case map[string]interface{}:
		// 单个room_layout map
		room := RoomLayoutInfo{}
		// 兼容 float64 与 int 两种数值类型
		if b, ok := v["bedrooms"]; ok {
			switch t := b.(type) {
			case float64:
				room.Bedrooms = int(t)
			case int:
				room.Bedrooms = t
			case int64:
				room.Bedrooms = int(t)
			case int32:
				room.Bedrooms = int(t)
			}
		}
		if lr, ok := v["living_rooms"]; ok {
			switch t := lr.(type) {
			case float64:
				room.LivingRooms = int(t)
			case int:
				room.LivingRooms = t
			case int64:
				room.LivingRooms = int(t)
			case int32:
				room.LivingRooms = int(t)
			}
		}
		if description, ok := v["description"].(string); ok {
			room.Description = description
		}
		return []RoomLayoutInfo{room}
	case []interface{}:
		// room_layout数组，取最后一个有效的（统一"最新为准"策略）
		for i := len(v) - 1; i >= 0; i-- {
			if itemMap, ok := v[i].(map[string]interface{}); ok {
				if rooms := s.parseRoomLayoutData(itemMap); len(rooms) > 0 {
					return rooms
				}
			}
		}
	}
	return []RoomLayoutInfo{}
}

// handleIpangResultAndSendPDF 处理Ipang API结果并发送PDF给用户
func (s *KeywordExtractorService) handleIpangResultAndSendPDF(userID, openKFID string, response *ipang.QueryResponse) error {
	if openKFID == "" {
		return fmt.Errorf("openKFID is empty, cannot send message")
	}

	// 1. 检查Ipang API状态
	if response.Status != 0 {
		return s.sendErrorMessage(userID, openKFID, "抱歉，服务器开小差了，暂时无法为您生成报告")
	}

	// 2. 检查是否有房源数据
	hasData := len(response.Detail.NewHouse) > 0 || len(response.Detail.SecHouse) > 0
	if !hasData {
		return s.sendErrorMessage(userID, openKFID, "抱歉，基于您给出的条件，当前没有合适的房源，您调整下需求再试试呢？")
	}

	// 3. 检查是否有PDF链接
	if response.Detail.PdfUrl == "" {
		log.GetInstance().Sugar.Warn("Ipang API returned data but no PDF URL for user: ", userID)
		return s.sendErrorMessage(userID, openKFID, "抱歉，服务器开小差了，暂时无法为您生成报告")
	}

	log.GetInstance().Sugar.Infof("Found PDF URL for user %s: %s", userID, response.Detail.PdfUrl)

	// 4. 下载PDF文件
	pdfFilePath, err := s.downloadPDF(userID, response.Detail.PdfUrl)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to download PDF for user ", userID, ": ", err)
		return s.sendErrorMessage(userID, openKFID, "抱歉，服务器开小差了，暂时无法为您生成报告")
	}
	defer os.Remove(pdfFilePath) // 确保临时文件被清理

	// 5. 上传PDF为临时素材
	mediaID, err := s.uploadPDFAsMedia(pdfFilePath)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to upload PDF as media for user ", userID, ": ", err)
		return s.sendErrorMessage(userID, openKFID, "抱歉，服务器开小差了，暂时无法为您生成报告")
	}

	// 6. 发送文件消息
	return s.sendFileMessage(userID, openKFID, mediaID)
}

// handleIpangPostResultAndSendPDF 处理Ipang POST返回（含诊断字段）并发送PDF/建议
func (s *KeywordExtractorService) handleIpangPostResultAndSendPDF(userID, openKFID string, resp *ipang.PostQueryResponse) error {
	if openKFID == "" {
		return fmt.Errorf("openKFID is empty, cannot send message")
	}

	if resp.Status != 0 {
		return s.sendErrorMessage(userID, openKFID, "抱歉，服务器开小差了，暂时无法为您生成报告")
	}

	// 结合意向生成建议
	intent := IntentAll
	if p := GetInstance().getProcessor(userID); p != nil {
		intent = p.getPurchaseIntent()
	}

	// 按意向判断要不要给出“可调参数”建议
	// 新房建议
	if (intent == IntentNew) && len(resp.Detail.NewHouse) == 0 && len(resp.Detail.NewHouseNoExistParams) > 0 {
		msg := s.composeAdjustSuggestion("新房", []string(resp.Detail.NewHouseNoExistParams))
		if len(msg) > 0 {
			err := s.sendTextMessage(userID, openKFID, msg)
			if err != nil {
				log.GetInstance().Sugar.Warn("send suggestion failed: ", err)
			}
		}
	}
	// 二手房建议
	if (intent == IntentSecond) && len(resp.Detail.SecHouse) == 0 && len(resp.Detail.SecHouseNoExistParams) > 0 {
		msg := s.composeAdjustSuggestion("二手房", []string(resp.Detail.SecHouseNoExistParams))
		if len(msg) > 0 {
			err := s.sendTextMessage(userID, openKFID, msg)
			if err != nil {
				log.GetInstance().Sugar.Warn("send suggestion failed: ", err)
			}
		}
	}

	// 有pdf则发送
	if resp.Detail.PdfUrl == "" {
		// 无pdf则根据有无数据给出通用提示
		hasData := len(resp.Detail.NewHouse) > 0 || len(resp.Detail.SecHouse) > 0
		if !hasData {
			return s.sendTextMessage(userID, openKFID, "当前条件下暂无合适房源，您可以适当放宽预算、面积或户型等条件，我来为您重新检索。")
		}
		return nil
	}

	// 下载->上传->发送
	pdfFilePath, err := s.downloadPDF(userID, resp.Detail.PdfUrl)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to download PDF for user ", userID, ": ", err)
		return s.sendErrorMessage(userID, openKFID, "抱歉，服务器开小差了，暂时无法为您生成报告")
	}
	defer os.Remove(pdfFilePath)

	mediaID, err := s.uploadPDFAsMedia(pdfFilePath)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to upload PDF as media for user ", userID, ": ", err)
		return s.sendErrorMessage(userID, openKFID, "抱歉，服务器开小差了，暂时无法为您生成报告")
	}
	return s.sendFileMessage(userID, openKFID, mediaID)
}

// composeAdjustSuggestion 根据缺失参数生成用户可调建议
func (s *KeywordExtractorService) composeAdjustSuggestion(category string, params []string) string {
	if len(params) == 0 {
		return ""
	}
	// 参数到建议的映射（简化版）
	explain := map[string]string{
		"amount": "适当调整总价预算（例如从50万提高到60万）",
		"size":   "扩大面积范围（例如从90-110㎡放宽到85-120㎡）",
		"count":  "放宽户型数量（例如从3房改为2-3房）",
		"area":   "更换区域（可考虑相邻区域）",
		"plate":  "尝试附近的板块",
	}

	items := make([]string, 0, len(params))
	for _, p := range params {
		if tip, ok := explain[p]; ok {
			items = append(items, fmt.Sprintf("%s", tip))
		}
	}
	if len(items) > 0 {
		return fmt.Sprintf("当前%s暂无合适房源。您可尝试调整以下条件：\n%s", category, strings.Join(items, "\n"))
	}

	return ""
}

// sendTextMessage 发送普通文本消息
func (s *KeywordExtractorService) sendTextMessage(userID, openKFID, message string) error {
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
		log.GetInstance().Sugar.Error("Failed to fetch corp token for user ", userID, ": ", err)
		return fmt.Errorf("failed to fetch corp token: %w", err)
	}

	textMsg := &kefu.KFTextMessage{
		KFMessage: kefu.KFMessage{
			ToUser:    userID,
			OpenKFID:  openKFID,
			CorpToken: token,
		},
	}
	textMsg.Text.Content = message
	_, err = kefu.SendKFText(textMsg)
	if err != nil {
		return fmt.Errorf("failed to send text message: %w", err)
	}
	reportKefuMessage(textMsg.MsgType)
	return nil
}

// sendErrorMessage 发送错误消息给用户
func (s *KeywordExtractorService) sendErrorMessage(userID, openKFID, message string) error {
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
		log.GetInstance().Sugar.Error("Failed to fetch corp token for user ", userID, ": ", err)
		return fmt.Errorf("failed to fetch corp token: %w", err)
	}

	textMsg := &kefu.KFTextMessage{
		KFMessage: kefu.KFMessage{
			ToUser:    userID,
			OpenKFID:  openKFID,
			CorpToken: token,
		},
	}
	textMsg.Text.Content = message

	_, err = kefu.SendKFText(textMsg)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to send error message to user ", userID, ": ", err)
		return fmt.Errorf("failed to send error message: %w", err)
	}

	log.GetInstance().Sugar.Info("Sent error message to user ", userID, ": ", message)
	reportKefuMessage(textMsg.MsgType)
	return nil
}

// downloadPDF 下载PDF文件到临时目录
func (s *KeywordExtractorService) downloadPDF(userID, pdfURL string) (string, error) {
	// 创建临时目录
	tempDir := "/tmp/qywx_pdf"
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// 创建包含用户ID的文件名
	fileName := fmt.Sprintf("ipang_report_%s_%d.pdf", userID, utils.Now().Unix())
	filePath := filepath.Join(tempDir, fileName)

	// 创建HTTP客户端（带超时）
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	log.GetInstance().Sugar.Infof("Downloading PDF from %s to %s", pdfURL, filePath)

	// 下载文件
	resp, err := client.Get(pdfURL)
	if err != nil {
		return "", fmt.Errorf("failed to download PDF: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download PDF: HTTP %d", resp.StatusCode)
	}

	// 创建本地文件
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create local file: %w", err)
	}
	defer file.Close()

	// 复制内容
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		os.Remove(filePath) // 清理失败的文件
		return "", fmt.Errorf("failed to save PDF file: %w", err)
	}

	log.GetInstance().Sugar.Infof("Successfully downloaded PDF to %s", filePath)
	return filePath, nil
}

// uploadPDFAsMedia 上传PDF文件为微信临时素材
func (s *KeywordExtractorService) uploadPDFAsMedia(filePath string) (string, error) {
	cfg := config.GetInstance()
	platform := common.Platform(cfg.Platform)

	// 获取访问令牌
	token, err := tokenstore.Instance().FetchCorpToken(
		platform,
		cfg.SuiteConfig.SuiteId,
		cfg.SuiteConfig.SuiteId,
		"",
		cfg.SuiteConfig.Secret,
	)
	if err != nil {
		log.GetInstance().Sugar.Error("Failed to fetch corp token for PDF upload: ", err)
		return "", fmt.Errorf("failed to fetch corp token: %w", err)
	}

	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF file: %w", err)
	}
	defer file.Close()

	log.GetInstance().Sugar.Infof("Uploading PDF file %s as temporary media", filePath)

	// 使用context进行超时控制
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 上传为临时素材
	mediaID, _, err := upload.UploadTemporaryMaterial(ctx, token, "file", file, "搜房报告.pdf")
	if err != nil {
		return "", fmt.Errorf("failed to upload PDF as temporary media: %w", err)
	}

	log.GetInstance().Sugar.Infof("Successfully uploaded PDF as media: %s", mediaID)
	return mediaID, nil
}

// sendFileMessage 发送文件消息给用户
func (s *KeywordExtractorService) sendFileMessage(userID, openKFID, mediaID string) error {
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
		log.GetInstance().Sugar.Error("Failed to fetch corp token for user ", userID, ": ", err)
		return fmt.Errorf("failed to fetch corp token: %w", err)
	}

	fileMsg := &kefu.KFFileMessage{
		KFMessage: kefu.KFMessage{
			ToUser:    userID,
			OpenKFID:  openKFID,
			CorpToken: token,
		},
	}
	fileMsg.File.MediaID = mediaID

	var result *kefu.KFResult
	result, err = kefu.SendKFFile(fileMsg)
	if err != nil {
		return fmt.Errorf("failed to send file message: %w", err)
	}
	reportKefuMessage(fileMsg.MsgType)

	log.GetInstance().Sugar.Infof("Successfully sent PDF file message to user %s, msgid: %s", userID, result.MsgID)
	return nil
}
