package schedule

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"qywx/infrastructures/common"
	"qywx/infrastructures/ipang"
	"qywx/infrastructures/log"
	"qywx/models/thirdpart/minimax"
)

// 字段常量定义
const (
	FieldArea   = "area"
	FieldAmount = "amount"
	FieldCount  = "count"
	FieldPlate  = "plate"
	FieldSize   = "size"
	FieldPrice  = "price"
)

// 地理位置相关的关键词特征
var geoIndicators = []string{
	"路", "街", "巷", "弄", "号",
	"广场", "中心", "大厦", "大楼", "园区",
	"镇", "村", "湾", "港", "岛",
}

// 需要排除的非地理位置词汇
var excludeKeywords = []string{
	"学区", "学校", "地铁", "公交", "医院",
	"商场", "超市", "公园", "银行", "餐厅",
	"幼儿园", "小学", "中学", "大学",
}

// 已知的板块名称
var knownPlates = []string{
	"陆家嘴", "徐家汇", "五角场", "新天地", "人民广场",
	"虹桥", "张江", "金桥", "外高桥", "川沙",
	"莘庄", "七宝", "古美", "梅陇", "浦江",
}

// IpangAdapter 将Keywords提取结果转换为Ipang API参数
type IpangAdapter struct {
	// 冲突解决策略
	conflictStrategy map[string]ConflictResolution
}

// ConflictResolution 冲突解决策略
type ConflictResolution int

const (
	UseLatest ConflictResolution = iota // 使用最新值（默认）
	MergeAll                            // 合并所有值
	UseFirst                            // 使用首次值
	Intersect                           // 取交集
)

// PostFilter 后处理筛选条件
type PostFilter struct {
	Field    string  // 字段名
	Operator string  // 操作符：<=, >=, <, >
	Value    float64 // 比较值
	Unit     string  // 单位
}

// TransformResult 转换结果
type TransformResult struct {
	Params      *ipang.QueryParams // Ipang API参数
	PostFilters []PostFilter       // 需要后处理的筛选条件
	Warnings    []string           // 转换警告信息
}

// NewIpangAdapter 创建新的Ipang适配器
func NewIpangAdapter() *IpangAdapter {
	return &IpangAdapter{
		conflictStrategy: map[string]ConflictResolution{
			FieldArea:   UseLatest, // 用户可能改变区域意向
			FieldAmount: UseLatest, // 价格预算常调整
			FieldCount:  MergeAll,  // 可能想看多种户型
			FieldPlate:  MergeAll,  // 可能关注多个板块
			FieldSize:   UseLatest, // 面积需求相对固定
		},
	}
}

// Transform 将Keywords提取结果转换为Ipang API参数
func (a *IpangAdapter) Transform(extraction *KeywordExtractionResult, rawConverHistory []minimax.RawMsg) *TransformResult {
	result := &TransformResult{
		Params:      &ipang.QueryParams{},
		PostFilters: make([]PostFilter, 0),
		Warnings:    make([]string, 0),
	}

	// 1. Area转换：Location.District → Area
	result.Params.Area = a.extractArea(extraction.Location)

	// 2. Amount转换：Price → Amount（含操作符处理）
	amount, priceFilter := a.extractAmount(extraction.Price)
	result.Params.Amount = amount
	if priceFilter != nil {
		result.PostFilters = append(result.PostFilters, *priceFilter)
	}

	// 3. Count转换：RoomLayout.Bedrooms → Count
	result.Params.Count = a.extractCount(extraction.RoomLayout)

	// 4. Plate转换：直接使用Location.Plate字段
	result.Params.Plate = a.extractPlate(extraction.Location)

	// 5. Size转换：Area → Size（含操作符处理）
	size, areaFilter := a.extractSize(extraction.Area)
	result.Params.Size = size
	if areaFilter != nil {
		result.PostFilters = append(result.PostFilters, *areaFilter)
	}

	// 6. Qas转换：rawConverHistory → Qas（问答对）
	if len(rawConverHistory) > 0 {
		result.Params.Qas = a.buildQasFromHistory(rawConverHistory)
	}

	// 验证是否有至少一个搜索条件
	if a.isEmptyParams(result.Params) {
		result.Warnings = append(result.Warnings, "未能提取到有效的搜索条件，将返回默认推荐")
	}

	// 记录未映射的维度信息
	if len(extraction.Decoration) > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("装修要求（%s）将在结果中筛选", strings.Join(extraction.Decoration, "、")))
	}
	if len(extraction.Orientation) > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("朝向要求（%s）将在结果中筛选", strings.Join(extraction.Orientation, "、")))
	}

	log.GetInstance().Sugar.Infof("Ipang adapter transform completed: params=%+v, filters=%d, warnings=%d",
		result.Params, len(result.PostFilters), len(result.Warnings))

	return result
}

// extractArea 提取区域参数
func (a *IpangAdapter) extractArea(locations []LocationInfo) string {
	if len(locations) == 0 {
		return ""
	}

	// 根据策略选择：默认使用最新的
	strategy := a.conflictStrategy[FieldArea]

	switch strategy {
	case UseLatest:
		// 使用最新的区域意向
		for i := len(locations) - 1; i >= 0; i-- {
			if locations[i].District != "" {
				return a.normalizeDistrict(locations[i].District)
			}
		}
	case UseFirst:
		// 使用首次提及的区域
		for _, loc := range locations {
			if loc.District != "" {
				return a.normalizeDistrict(loc.District)
			}
		}
	case MergeAll:
		// 合并所有区域（通常不适用于area）
		districts := make([]string, 0)
		seen := make(map[string]bool)
		for _, loc := range locations {
			if loc.District != "" {
				normalized := a.normalizeDistrict(loc.District)
				if !seen[normalized] {
					seen[normalized] = true
					districts = append(districts, normalized)
				}
			}
		}
		if len(districts) > 0 {
			return districts[0] // Ipang API只支持单个区域
		}
	}

	return ""
}

// extractAmount 提取价格参数和筛选条件
func (a *IpangAdapter) extractAmount(prices []PriceInfo) (string, *PostFilter) {
	if len(prices) == 0 {
		return "", nil
	}

	// 使用最新的价格意向
	price := prices[len(prices)-1]

	// 处理单位转换（如果需要）
	if price.Unit != "万" && price.Unit != "" {
		log.GetInstance().Sugar.Warnf("Price unit '%s' may need conversion", price.Unit)
	}

	var amount string
	var postFilter *PostFilter

	switch price.Operator {
	case "<=", "<":
		// "不超过600万" → "0,600"
		if price.Max > 0 {
			amount = fmt.Sprintf("0,%d", int(price.Max))
			// 记录精确的筛选条件
			postFilter = &PostFilter{
				Field:    FieldPrice,
				Operator: price.Operator,
				Value:    price.Max,
				Unit:     price.Unit,
			}
		}

	case ">=", ">":
		// "至少500万" → "500,99999"
		if price.Min > 0 {
			amount = fmt.Sprintf("%d,99999", int(price.Min))
			postFilter = &PostFilter{
				Field:    FieldPrice,
				Operator: price.Operator,
				Value:    price.Min,
				Unit:     price.Unit,
			}
		}

	case "about":
		// "200万左右" → 添加±20%容差
		// 注意：价格使用±20%容差，面积使用±10%容差，这是基于业务需求的设计
		baseValue := 0.0
		if price.Min > 0 {
			// 正常情况：parsePrice已经将value设置为Min
			baseValue = price.Min
		} else if price.Max > 0 {
			// 防御性修复：某些情况下about操作符的值可能错误地设在Max字段
			baseValue = price.Max
		}

		if baseValue > 0 {
			// minVal := int(baseValue * 0.8)
			// maxVal := int(baseValue * 1.2)
			// amount = fmt.Sprintf("%d,%d", minVal, maxVal)
			// 用户要求about也返回单值
			amount = fmt.Sprintf("%d", int(baseValue))
		}

	case "==", "":
		// 精确值或范围
		if price.Min > 0 && price.Max > 0 {
			// 检查是否为相同值（无意义的区间）
			if int(price.Min) == int(price.Max) {
				// min == max，输出单个值
				amount = fmt.Sprintf("%d", int(price.Min))
			} else {
				// 真正的范围
				amount = fmt.Sprintf("%d,%d", int(price.Min), int(price.Max))
			}
		} else if price.Min > 0 {
			// 只有最小值
			amount = fmt.Sprintf("%d", int(price.Min))
		} else if price.Max > 0 {
			// 只有最大值
			amount = fmt.Sprintf("%d", int(price.Max))
		}

	default:
		// 未知操作符，尝试构造合理范围
		if price.Max > 0 {
			amount = fmt.Sprintf("0,%d", int(price.Max))
		} else if price.Min > 0 {
			amount = fmt.Sprintf("%d,99999", int(price.Min))
		}
		log.GetInstance().Sugar.Warnf("Unknown price operator: %s", price.Operator)
	}

	return amount, postFilter
}

// extractCount 提取房间数参数
func (a *IpangAdapter) extractCount(layouts []RoomLayoutInfo) string {
	if len(layouts) == 0 {
		return ""
	}

	strategy := a.conflictStrategy[FieldCount]
	counts := make([]string, 0)
	seen := make(map[int]bool)

	switch strategy {
	case MergeAll:
		// 合并所有户型需求
		intCounts := make([]int, 0)
		for _, layout := range layouts {
			roomCount := a.extractRoomCount(layout)
			if roomCount > 0 && !seen[roomCount] {
				seen[roomCount] = true
				intCounts = append(intCounts, roomCount)
			}
		}
		// 按升序排列房间数
		sort.Ints(intCounts)
		for _, count := range intCounts {
			counts = append(counts, strconv.Itoa(count))
		}

	case UseLatest:
		// 使用最新的户型需求
		for i := len(layouts) - 1; i >= 0; i-- {
			roomCount := a.extractRoomCount(layouts[i])
			if roomCount > 0 {
				return strconv.Itoa(roomCount)
			}
		}

	case UseFirst:
		// 使用首次提及的户型
		for _, layout := range layouts {
			roomCount := a.extractRoomCount(layout)
			if roomCount > 0 {
				return strconv.Itoa(roomCount)
			}
		}
	}

	return strings.Join(counts, ",")
}

// extractPlate 提取板块参数
func (a *IpangAdapter) extractPlate(locations []LocationInfo) string {
	plates := make([]string, 0)
	seen := make(map[string]bool)

	// 直接使用Location.Plate字段（keywords模块已经能够直接返回板块信息）
	for _, loc := range locations {
		if loc.Plate != "" {
			if !seen[loc.Plate] {
				seen[loc.Plate] = true
				plates = append(plates, loc.Plate)
			}
		}
	}

	// 根据策略处理
	if len(plates) == 0 {
		return ""
	}

	strategy := a.conflictStrategy[FieldPlate]
	switch strategy {
	case UseLatest:
		return plates[len(plates)-1]
	case UseFirst:
		return plates[0]
	case MergeAll:
		fallthrough
	default:
		return strings.Join(plates, ",")
	}
}

// transformSingleArea 转换单个面积信息到字符串格式和筛选条件
func (a *IpangAdapter) transformSingleArea(area AreaInfo) (string, *PostFilter) {
	// 处理单位转换（如果需要）
	if area.Unit != "平米" && area.Unit != "" {
		log.GetInstance().Sugar.Warnf("Area unit '%s' may need conversion", area.Unit)
	}

	var size string
	var postFilter *PostFilter

	switch area.Operator {
	case "<=", "<":
		// "不超过120平" → "0,120"
		if area.Max > 0 {
			size = fmt.Sprintf("0,%d", int(area.Max))
			// 记录精确的筛选条件
			postFilter = &PostFilter{
				Field:    FieldSize,
				Operator: area.Operator,
				Value:    area.Max,
				Unit:     area.Unit,
			}
		}

	case ">=", ">":
		// "至少80平" → "80,999"
		if area.Min > 0 {
			size = fmt.Sprintf("%d,999", int(area.Min))
			postFilter = &PostFilter{
				Field:    FieldSize,
				Operator: area.Operator,
				Value:    area.Min,
				Unit:     area.Unit,
			}
		}

	case "about":
		// "100平左右" → 添加±10%容差
		// 注意：面积使用±10%容差，价格使用±20%容差，这是基于业务需求的设计
		if area.Value > 0 {
			// minVal := int(area.Value * 0.9)
			// maxVal := int(area.Value * 1.1)
			// size = fmt.Sprintf("%d,%d", minVal, maxVal)
			// 客户要求，取消about语义下的容差
			size = fmt.Sprintf("%d", int(area.Value))
		}

	case "==", "":
		// 精确值或范围
		if area.Min > 0 && area.Max > 0 {
			// 检查是否为相同值（无意义的区间）
			if int(area.Min) == int(area.Max) {
				// min == max，输出单个值
				size = fmt.Sprintf("%d", int(area.Min))
			} else {
				// 真正的范围
				size = fmt.Sprintf("%d,%d", int(area.Min), int(area.Max))
			}
		} else if area.Min > 0 {
			// 只有最小值
			size = fmt.Sprintf("%d", int(area.Min))
		} else if area.Max > 0 {
			// 只有最大值
			size = fmt.Sprintf("%d", int(area.Max))
		} else if area.Value > 0 {
			// 单个值
			size = fmt.Sprintf("%d", int(area.Value))
		}

	default:
		// 未知操作符，尝试构造合理范围
		if area.Max > 0 {
			size = fmt.Sprintf("0,%d", int(area.Max))
		} else if area.Min > 0 {
			size = fmt.Sprintf("%d,999", int(area.Min))
		} else if area.Value > 0 {
			size = fmt.Sprintf("%d", int(area.Value))
		}
		log.GetInstance().Sugar.Warnf("Unknown area operator: %s", area.Operator)
	}

	return size, postFilter
}

// extractSize 提取面积参数和筛选条件
func (a *IpangAdapter) extractSize(areas []AreaInfo) (string, *PostFilter) {
	if len(areas) == 0 {
		return "", nil
	}

	strategy := a.conflictStrategy[FieldSize]

	switch strategy {
	case UseLatest:
		// 使用最新的面积需求
		return a.transformSingleArea(areas[len(areas)-1])

	case UseFirst:
		// 使用首次提及的面积
		return a.transformSingleArea(areas[0])

	case MergeAll:
		// 构造面积范围（最小到最大）
		minArea := areas[0].Value
		maxArea := areas[0].Value
		for _, area := range areas {
			if area.Value < minArea {
				minArea = area.Value
			}
			if area.Value > maxArea {
				maxArea = area.Value
			}
		}
		if minArea > 0 && maxArea > 0 && minArea != maxArea {
			return fmt.Sprintf("%d,%d", int(minArea), int(maxArea)), nil
		} else if minArea > 0 {
			// 单一值，不做容差，直接返回
			// min := int(minArea * 0.9)
			// max := int(minArea * 1.1)
			// return fmt.Sprintf("%d,%d", min, max), nil
			return fmt.Sprintf("%d", int(minArea)), nil
		}
	}

	return "", nil
}

// ApplyPostFilters 应用后处理筛选
// TODO: 需要根据新的QueryResponse结构重新实现
// 当前版本暂时注释，因为ipang.Property已经不存在
/*
func (a *IpangAdapter) ApplyPostFilters(properties []*ipang.Property, filters []PostFilter) []*ipang.Property {
	if len(filters) == 0 {
		return properties
	}

	result := make([]*ipang.Property, 0)

	for _, prop := range properties {
		if a.matchFilters(prop, filters) {
			result = append(result, prop)
		}
	}

	log.GetInstance().Sugar.Infof("Post-filter applied: %d/%d properties matched",
		len(result), len(properties))

	return result
}

// matchFilters 检查属性是否满足所有筛选条件
func (a *IpangAdapter) matchFilters(prop *ipang.Property, filters []PostFilter) bool {
	for _, filter := range filters {
		if !a.matchSingleFilter(prop, filter) {
			return false
		}
	}
	return true
}

// matchSingleFilter 检查单个筛选条件
func (a *IpangAdapter) matchSingleFilter(prop *ipang.Property, filter PostFilter) bool {
	switch filter.Field {
	case FieldPrice:
		// 价格比较（单位：万）
		propPrice := prop.Price
		filterValue := filter.Value

		switch filter.Operator {
		case "<=":
			return propPrice <= filterValue
		case "<":
			return propPrice < filterValue
		case ">=":
			return propPrice >= filterValue
		case ">":
			return propPrice > filterValue
		case "==":
			return propPrice == filterValue
		default:
			return true
		}

	default:
		// 未知字段，默认通过
		return true
	}
}
*/

// 辅助方法

// normalizeDistrict 标准化区域名称
func (a *IpangAdapter) normalizeDistrict(district string) string {
	district = strings.TrimSpace(district)

	// 规范名称映射：key为可能的输入，value为API期望的标准名称
	// 根据Ipang API的实际需求调整标准名称（不带"区"后缀）
	canonicalMap := map[string]string{
		"浦东":   "浦东新区",
		"浦东新区": "浦东新区",
		"徐汇":   "徐汇",
		"徐汇区":  "徐汇",
		"静安":   "静安",
		"静安区":  "静安",
		"黄浦":   "黄浦",
		"黄浦区":  "黄浦",
		"虹口":   "虹口",
		"虹口区":  "虹口",
		"杨浦":   "杨浦",
		"杨浦区":  "杨浦",
		"闵行":   "闵行",
		"闵行区":  "闵行",
		"宝山":   "宝山",
		"宝山区":  "宝山",
		"嘉定":   "嘉定",
		"嘉定区":  "嘉定",
		"普陀":   "普陀",
		"普陀区":  "普陀",
		"长宁":   "长宁",
		"长宁区":  "长宁",
		"松江":   "松江",
		"松江区":  "松江",
		"青浦":   "青浦",
		"青浦区":  "青浦",
		"奉贤":   "奉贤",
		"奉贤区":  "奉贤",
		"金山":   "金山",
		"金山区":  "金山",
		"崇明":   "崇明",
		"崇明区":  "崇明",
	}

	if canonical, ok := canonicalMap[district]; ok {
		return canonical
	}

	// 如果不在映射表中，返回原值
	return district
}

// extractRoomCount 从户型信息提取房间总数
// Jenny式修改：计算所有int类型房间字段的总和（卧室+客厅+卫生间）
func (a *IpangAdapter) extractRoomCount(layout RoomLayoutInfo) int {
	totalRooms := 0

	// 直接相加卧室与客厅数量
	if layout.Bedrooms > 0 {
		totalRooms += layout.Bedrooms
	}
	if layout.LivingRooms > 0 {
		totalRooms += layout.LivingRooms
	}

	// 仅当两者都为0时，才从描述中回退解析（4室2厅 → 6）
	if totalRooms == 0 && strings.TrimSpace(layout.Description) != "" {
		desc := layout.Description

		// 支持阿拉伯/中文数字，兼容“室/房”与“厅”
		reNum := `(?:\d+|[一二两俩仨四五六七八九十壹贰叁肆伍陆柒捌玖拾〇零]{1,3})`
		reShi := regexp.MustCompile(reNum + `\s*(?:室|房)`)
		reTing := regexp.MustCompile(reNum + `\s*厅`)

		toInt := func(s string) int {
			if n, err := strconv.Atoi(s); err == nil {
				return n
			}
			s2 := common.ConvertChineseToArabic(s)
			if n, err := strconv.Atoi(s2); err == nil {
				return n
			}
			return 0
		}

		totalFromDesc := 0
		if m := reShi.FindString(desc); m != "" {
			// 提取数字部分（再次用正则取前导数字/中文数字）
			rePick := regexp.MustCompile(reNum)
			if n := rePick.FindString(m); n != "" {
				totalFromDesc += toInt(n)
			}
		}
		if m := reTing.FindString(desc); m != "" {
			rePick := regexp.MustCompile(reNum)
			if n := rePick.FindString(m); n != "" {
				totalFromDesc += toInt(n)
			}
		}

		return totalFromDesc
	}

	return totalRooms
}

// extractGeographicKeywords 从兴趣点中提取地理位置关键词
func (a *IpangAdapter) extractGeographicKeywords(interestPoints []string) []string {
	result := make([]string, 0)

	for _, point := range interestPoints {
		// 检查是否应该排除
		shouldExclude := false
		for _, exclude := range excludeKeywords {
			if strings.Contains(point, exclude) {
				shouldExclude = true
				break
			}
		}

		if shouldExclude {
			continue
		}

		// 检查是否包含地理位置特征
		for _, indicator := range geoIndicators {
			if strings.Contains(point, indicator) {
				result = append(result, point)
				break
			}
		}

		// 检查是否是已知的板块名称
		for _, plate := range knownPlates {
			if point == plate || strings.Contains(point, plate) {
				result = append(result, point)
				break
			}
		}
	}

	return result
}

// isEmptyParams 检查参数是否为空
func (a *IpangAdapter) isEmptyParams(params *ipang.QueryParams) bool {
	return params.Area == "" &&
		params.Amount == "" &&
		params.Count == "" &&
		params.Plate == "" &&
		params.Size == ""
}

// buildQasFromHistory 从原始对话历史构建问答对
func (a *IpangAdapter) buildQasFromHistory(rawHistory []minimax.RawMsg) string {
	if len(rawHistory) == 0 {
		return ""
	}

	// 使用公共方法构建问答对（AI 消息作为 Q，用户消息作为 A）
	// 传入 isValidQuestionContent 作为内容验证器
	qas := BuildQAPairs(rawHistory, a.isValidQuestionContent)

	// 如果没有有效的问答对，返回空字符串
	if len(qas) == 0 {
		return ""
	}

	// 序列化为JSON字符串
	qasBytes, err := json.Marshal(qas)
	if err != nil {
		log.GetInstance().Sugar.Warnf("Failed to marshal QAs to JSON: %v", err)
		return ""
	}

	qasStr := string(qasBytes)
	log.GetInstance().Sugar.Infof("Built Qas from conversation history: %d pairs, JSON length: %d", len(qas), len(qasStr))
	return qasStr
}

// isValidQuestionContent 判断AI消息内容是否适合作为问题
// 由于RawMsg构建时已经过滤了临时性和系统性消息，这里只需要基本检查
func (a *IpangAdapter) isValidQuestionContent(content string) bool {
	content = strings.TrimSpace(content)
	return content != ""
}

// OperatorToText 将操作符转换为中文描述
func OperatorToText(op string) string {
	switch op {
	case "<=":
		return "不超过"
	case ">=":
		return "不低于"
	case "<":
		return "低于"
	case ">":
		return "高于"
	case "==":
		return "等于"
	default:
		return "约"
	}
}
