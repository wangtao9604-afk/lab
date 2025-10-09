package keywords

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"qywx/infrastructures/common"

	"github.com/mozillazg/go-pinyin"
)

// AdvancedNLPProcessor 高级NLP处理器
type AdvancedNLPProcessor struct {
	segmenter   *AdvancedSegmenter
	dictionary  *KeywordDictionary
	entityRules map[string]*regexp.Regexp
	numberRegex *regexp.Regexp
	initialized bool
}

// NewAdvancedNLPProcessor 创建高级NLP处理器
func NewAdvancedNLPProcessor(segmenter *AdvancedSegmenter, dict *KeywordDictionary) *AdvancedNLPProcessor {
	processor := &AdvancedNLPProcessor{
		segmenter:   segmenter,
		dictionary:  dict,
		entityRules: make(map[string]*regexp.Regexp),
	}

	processor.initializeEntityRules()
	processor.initialized = true

	return processor
}

// initializeEntityRules 初始化实体识别规则
func (nlp *AdvancedNLPProcessor) initializeEntityRules() {
	nlp.entityRules = map[string]*regexp.Regexp{
		// 价格相关正则
		"PRICE_RANGE":  regexp.MustCompile(`(\d+)\s*[-到至~]\s*(\d+)(万|亿)|(\d+)(万|亿)\s*[-到至~]\s*(\d+)(万|亿)|(\d+)(万|亿)以下|(\d+)(万|亿)以上|预算(\d+)\s*[-到至~]\s*(\d+)(万|亿)|预算(\d+)(万|亿)\s*[-到至~]\s*(\d+)(万|亿)|预算(\d+)(万|亿)以上|预算(\d+)(万|亿)以下|预算(\d+)(万|亿)?|(\d+)\s*[-到至~]\s*(\d+)元|(\d+)元以下|(\d+)元以上`),
		"SINGLE_PRICE": regexp.MustCompile(`(\d+(?:\.\d+)?)万左右|(\d+(?:\.\d+)?)万元$|(\d+(?:\.\d+)?)元$`),

		// 租金相关正则 - 改进版本
		"RENT_PRICE": regexp.MustCompile(`月租金(\d+)[-到至~](\d+)|月租(\d+)[-到至~](\d+)|租金(\d+)[-到至~](\d+)元?/?月?|月租金(\d+)到(\d+)元?|租金(\d+)到(\d+)元?|月租金(\d+)左右|月租(\d+)左右|租金(\d+)左右|月租金(\d+)|月租(\d+)|租金(\d+)元?/?月?`),

		// 面积相关正则 - 修复基础匹配
		"AREA_RANGE": regexp.MustCompile(`(\d+)[-到至~](\d+)平米?|(\d+)平米?以上|(\d+)平米?以下|面积(\d+)|(\d+)平米?左右|(\d+)平方米?|(\d+)平米?`),

		// 房型相关正则 - 支持中文数字（套房通过预处理转换）
		// 新增：单独的"N室"模式，支持阿拉伯数字、中文数字、中文大写数字
		"ROOM_LAYOUT": regexp.MustCompile(`(\d+)室(\d+)厅(\d+)卫|(\d+)房(\d+)厅|([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾1-9])居室|([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾1-9])室([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾1-9])厅|(\d+)房(\d+)厅(\d+)卫|(\d+)室(\d+)厅|(\d+)居室|(\d+)房间|(\d+)室|([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾])室`),

		// 楼层相关正则
		"FLOOR_INFO": regexp.MustCompile(`(\d+)楼|(\d+)层|(\d+)/(\d+)层|第(\d+)层|(\d+)-(\d+)层|高层|中层|低层`),

		// 年份相关正则
		"YEAR_INFO": regexp.MustCompile(`(\d{4})年|建成(\d+)年|房龄(\d+)年|(\d+)年代|(\d{2})年建`),

		// 地铁交通正则
		"SUBWAY_INFO": regexp.MustCompile(`(\w+)号线|地铁(\w+)站|(\w+)地铁站|距离(\w+)站|轨交(\d+)号线|(\d+)号线(\w+)站`),

		// 距离相关正则
		"DISTANCE_INFO": regexp.MustCompile(`(\d+)米|(\d+)公里|(\d+)km|步行(\d+)分钟|车程(\d+)分钟|距离(\d+)`),

		// 朝向相关正则 - 长词优先匹配
		"ORIENTATION": regexp.MustCompile(`南北通透|东西朝向|正南向|正北向|正东向|正西向|朝南|朝北|朝东|朝西|东南|东北|西南|西北`),

		// 装修相关正则
		"DECORATION": regexp.MustCompile(`毛坯|精装|简装|豪装|拎包入住|全装修|未装修|已装修|装修好|新装修`),

		// 房屋类型正则
		"PROPERTY_TYPE": regexp.MustCompile(`新房|二手房|期房|现房|别墅|公寓|商铺|写字楼|洋房|联排|独栋|叠拼|商业综合体|综合体`),
	}

	// 通用数字正则
	nlp.numberRegex = regexp.MustCompile(`\d+(?:\.\d+)?`)
}

// convertRoomLayout 居室转换预处理 - 解决56%成功率问题
func (nlp *AdvancedNLPProcessor) convertRoomLayout(text string) string {
	// 🏠 居室到室厅的转换规则 - 统一格式，避免分词冲突
	conversions := [][]string{
		// 🆕 套房映射：套[2/二,3/三,4/四]到[2/二,3/三,4/四]居室
		{"套二", "二居室"},
		{"套三", "三居室"},
		{"套四", "四居室"},
		{"套五", "五居室"},
		{"套2", "2居室"},
		{"套3", "3居室"},
		{"套4", "4居室"},
		{"套5", "5居室"},

		// 中文数字居室转换
		{"一居室", "一室一厅"},
		{"二居室", "二室一厅"},
		{"三居室", "三室一厅"},
		{"四居室", "四室一厅"},
		{"五居室", "五室一厅"},
		{"六居室", "六室一厅"},

		// 特殊处理"两"
		{"两居室", "二室一厅"},

		// 阿拉伯数字居室转换
		{"1居室", "一室一厅"},
		{"2居室", "二室一厅"},
		{"3居室", "三室一厅"},
		{"4居室", "四室一厅"},
		{"5居室", "五室一厅"},
		{"6居室", "六室一厅"},
	}

	result := text
	for _, conv := range conversions {
		result = strings.ReplaceAll(result, conv[0], conv[1])
	}

	return result
}

// normalizeTextForStability 标准化文本以提高实体提取稳定性
func (nlp *AdvancedNLPProcessor) normalizeTextForStability(text string) string {
	// 🏠 居室转换预处理 - 优先处理，避免分词冲突
	normalizedText := nlp.convertRoomLayout(text)

	// 🔧 通用中文数字标准化系统

	// 1. 复合中文数字的特殊处理（避免错误拆分）
	complexNumbers := map[string]string{
		"一十": "10", "二十": "20", "三十": "30", "四十": "40", "五十": "50",
		"六十": "60", "七十": "70", "八十": "80", "九十": "90", "一百": "100",
		"二百": "200", "三百": "300", "四百": "400", "五百": "500",
	}

	// 🔧 修复稳定性问题：确保确定性的遍历顺序
	// 先处理复合数字
	var complexKeys []string
	for complex := range complexNumbers {
		complexKeys = append(complexKeys, complex)
	}
	sort.Strings(complexKeys)

	for _, complex := range complexKeys {
		number := complexNumbers[complex]
		normalizedText = strings.ReplaceAll(normalizedText, complex, number)
	}

	// 2. 基础中文数字到阿拉伯数字的映射
	chineseNumbers := map[string]string{
		"零": "0", "一": "1", "二": "2", "两": "2", "三": "3", "四": "4", "五": "5",
		"六": "6", "七": "7", "八": "8", "九": "9", "十": "10",
	}

	// 3. 房地产领域特定的标准化规则
	housingPatterns := []string{
		"居室", "室", "厅", "卫", "房", "层", "楼", "栋", "单元", "号",
		"平", "平米", "平方", "平方米", "万", "元", "块", "千", "百",
	}

	// 4. 通用数字相关后缀
	numberSuffixes := []string{
		"个", "位", "套", "间", "处", "家", "座", "台", "部", "辆",
		"年", "月", "日", "天", "小时", "分钟", "米", "千米", "公里",
		"公斤", "斤", "吨", "升", "毫升", "度", "摄氏度", "华氏度",
	}

	// 5. 组合所有需要标准化的后缀
	allSuffixes := append(housingPatterns, numberSuffixes...)

	// 6. 对剩余的单个中文数字进行标准化
	for chinese, arabic := range chineseNumbers {
		// 处理带后缀的中文数字
		for _, suffix := range allSuffixes {
			normalizedText = strings.ReplaceAll(normalizedText, chinese+suffix, arabic+suffix)
		}
	}

	// 7. 标准化常见的变体表达 - 处理顺序很重要
	// 先处理带"平"的复合单位，避免重复
	normalizedText = strings.ReplaceAll(normalizedText, "平㎡", "平米")
	normalizedText = strings.ReplaceAll(normalizedText, "平方米", "平米")
	normalizedText = strings.ReplaceAll(normalizedText, "平方", "平米")
	normalizedText = strings.ReplaceAll(normalizedText, "m²", "平米")

	variants := map[string]string{
		// 面积相关 - 只处理单独的单位符号
		"㎡": "平米",

		// 价格相关
		"块钱": "元", "圆": "元", "块": "元", "刀": "元",
		"万块": "万元", "万圆": "万元", "万刀": "万元",

		// 数量相关
		"几个": "多个", "若干": "多个", "一些": "多个",

		// 朝向相关 - 更全面的映射
		"南面": "朝南", "北面": "朝北", "东面": "朝东", "西面": "朝西",
		"向南": "朝南", "向北": "朝北", "向东": "朝东", "向西": "朝西",
		"向南面": "朝南", "向北面": "朝北", "向东面": "朝东", "向西面": "朝西",
		"南向": "朝南", "北向": "朝北", "东向": "朝东", "西向": "朝西",

		// 装修相关
		"装修过": "已装修", "装好": "已装修", "装修完": "已装修",
		"没装修": "未装修", "未装": "未装修", "毛胚": "毛坯",

		// 位置相关
		"附近": "周边", "旁边": "周边", "边上": "周边", "临近": "周边",

		// 时间相关
		"前年": "2年前", "去年": "1年前", "今年": "0年", "明年": "1年后",
	}

	for variant, standard := range variants {
		normalizedText = strings.ReplaceAll(normalizedText, variant, standard)
	}

	// 8. 标准化空白字符和标点符号
	normalizedText = regexp.MustCompile(`\s+`).ReplaceAllString(normalizedText, " ")
	normalizedText = strings.TrimSpace(normalizedText)

	// 9. 统一全角半角字符
	fullToHalfMap := map[string]string{
		"０": "0", "１": "1", "２": "2", "３": "3", "４": "4",
		"５": "5", "６": "6", "７": "7", "８": "8", "９": "9",
		"（": "(", "）": ")", "【": "[", "】": "]", "《": "<", "》": ">",
		"，": ",", "。": ".", "；": ";", "：": ":", "！": "!", "？": "?",
	}

	// 🔧 修复稳定性问题：确保确定性的遍历顺序
	var fullKeys []string
	for full := range fullToHalfMap {
		fullKeys = append(fullKeys, full)
	}
	sort.Strings(fullKeys)

	for _, full := range fullKeys {
		half := fullToHalfMap[full]
		normalizedText = strings.ReplaceAll(normalizedText, full, half)
	}

	return normalizedText
}

func (nlp *AdvancedNLPProcessor) ExtractEntities(text string) ([]Entity, error) {
	if !nlp.initialized {
		return nil, ErrNotInitialized
	}

	if text == "" {
		return nil, ErrEmptyInput
	}

	// 标准化文本以提高稳定性
	normalizedText := nlp.normalizeTextForStability(text)

	var entities []Entity

	// 1. 基于正则表达式的实体提取（使用标准化文本）
	regexEntities := nlp.extractRegexEntities(normalizedText)
	entities = append(entities, regexEntities...)

	// 2. 基于词典的实体提取（使用标准化文本）
	dictEntities := nlp.extractDictionaryEntities(normalizedText)
	entities = append(entities, dictEntities...)

	// 3. 基于分词的实体提取（使用标准化文本）
	segmentEntities := nlp.extractSegmentEntities(normalizedText)
	entities = append(entities, segmentEntities...)

	// 4. 地理位置实体提取（使用标准化文本）
	locationEntities := nlp.extractLocationEntities(normalizedText)
	entities = append(entities, locationEntities...)

	return nlp.deduplicateEntities(entities), nil
}

// extractRegexEntities 基于正则表达式提取实体
func (nlp *AdvancedNLPProcessor) extractRegexEntities(text string) []Entity {
	var entities []Entity

	for entityType, regex := range nlp.entityRules {
		matches := regex.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) > 0 {
				entity := Entity{
					Text:       match[0],
					Type:       entityType,
					Start:      strings.Index(text, match[0]),
					End:        strings.Index(text, match[0]) + len(match[0]),
					Confidence: 0.8,
				}

				// 解析实体值
				entity.Value = nlp.parseEntityValue(entityType, match)
				entities = append(entities, entity)
			}
		}
	}

	return entities
}

// extractDictionaryEntities 基于词典提取实体
func (nlp *AdvancedNLPProcessor) extractDictionaryEntities(text string) []Entity {
	var entities []Entity

	// 提取各类词典匹配 - commercial优先，避免被location误匹配
	categories := []string{"commercial", "property_type", "decoration", "orientation", "facility", "interest_point", "location"}

	for _, category := range categories {
		matches := nlp.dictionary.MatchKeywords(text, category)
		for _, match := range matches {
			// 查找匹配位置
			keywords := nlp.dictionary.GetCategoryKeywords(category)

			// 🔧 修复稳定性问题：确保确定性的遍历顺序
			var standardKeys []string
			for standardKey := range keywords {
				standardKeys = append(standardKeys, standardKey)
			}
			sort.Strings(standardKeys)

			for _, standardKey := range standardKeys {
				keywordList := keywords[standardKey]
				if standardKey == match {
					for _, keyword := range keywordList {
						if strings.Contains(strings.ToLower(text), strings.ToLower(keyword)) {
							start := strings.Index(strings.ToLower(text), strings.ToLower(keyword))
							entity := Entity{
								Text:       keyword,
								Type:       strings.ToUpper(category),
								Start:      start,
								End:        start + len(keyword),
								Confidence: 0.7,
								Value:      standardKey,
							}
							entities = append(entities, entity)
							break
						}
					}
					break
				}
			}
		}
	}

	return entities
}

// extractSegmentEntities 基于分词提取实体
func (nlp *AdvancedNLPProcessor) extractSegmentEntities(text string) []Entity {
	var entities []Entity

	if nlp.segmenter == nil {
		return entities
	}

	// 带词性标注的分词
	segments := nlp.segmenter.CutWithTag(text)

	// 基于词性提取实体
	for _, segment := range segments {
		entityType := nlp.mapPosToEntityType(segment.Tag)
		if entityType != "" {
			start := strings.Index(text, segment.Word)
			entity := Entity{
				Text:       segment.Word,
				Type:       entityType,
				Start:      start,
				End:        start + len(segment.Word),
				Confidence: 0.6,
				Value:      segment.Word,
			}
			entities = append(entities, entity)
		}
	}

	return entities
}

// extractLocationEntities 提取地理位置实体
func (nlp *AdvancedNLPProcessor) extractLocationEntities(text string) []Entity {
	var entities []Entity

	// 中国省市区匹配
	locationDict := nlp.dictionary.GetCategoryKeywords("location")

	// 先收集所有匹配的省份和区县
	var foundProvinces []string
	var foundDistricts []Entity

	for province, districts := range locationDict {
		// 匹配省份
		if strings.Contains(text, province) {
			start := strings.Index(text, province)
			entities = append(entities, Entity{
				Text:       province,
				Type:       "PROVINCE",
				Start:      start,
				End:        start + len(province),
				Confidence: 0.9,
				Value:      province,
			})
			foundProvinces = append(foundProvinces, province)
		}

		// 匹配区县 - 独立于省份匹配
		for _, district := range districts {
			if strings.Contains(text, district) {
				start := strings.Index(text, district)

				// 🆕 特殊处理上海板块
				if province == "上海" {
					// 先检查是否为板块（优先级最高）
					if isPlate, districtName := IsShanghaiPlate(district); isPlate {
						// 这是一个板块，创建PLATE实体
						districtEntity := Entity{
							Text:       district,
							Type:       "PLATE",
							Start:      start,
							End:        start + len(district),
							Confidence: 0.95,
							Value:      map[string]string{"province": province, "district": districtName, "plate": district},
						}
						foundDistricts = append(foundDistricts, districtEntity)
					} else if strings.Contains(district, "区") {
						// 这是一个区域（带"区"字的）
						districtEntity := Entity{
							Text:       district,
							Type:       "DISTRICT",
							Start:      start,
							End:        start + len(district),
							Confidence: 0.9,
							Value:      map[string]string{"province": province, "district": district},
						}
						foundDistricts = append(foundDistricts, districtEntity)
					} else {
						// 其他上海地名，可能是简写的区名或其他地点
						// 检查是否是上海的区（浦东、闵行等不带"区"字的）
						isDistrictName := false
						shanghaiDistricts := []string{
							"浦东", "黄浦", "徐汇", "长宁", "静安", "普陀", "虹口", "杨浦",
							"闵行", "宝山", "嘉定", "金山", "松江", "青浦", "奉贤", "崇明",
						}
						for _, d := range shanghaiDistricts {
							if district == d {
								isDistrictName = true
								break
							}
						}

						if isDistrictName {
							// 是区名
							districtEntity := Entity{
								Text:       district,
								Type:       "DISTRICT",
								Start:      start,
								End:        start + len(district),
								Confidence: 0.9,
								Value:      map[string]string{"province": province, "district": district},
							}
							foundDistricts = append(foundDistricts, districtEntity)
						} else {
							// 不是区名也不是板块，作为普通LOCATION处理
							districtEntity := Entity{
								Text:       district,
								Type:       "LOCATION",
								Start:      start,
								End:        start + len(district),
								Confidence: 0.8,
								Value:      province,
							}
							foundDistricts = append(foundDistricts, districtEntity)
						}
					}
				} else {
					// 其他省份正常处理
					districtEntity := Entity{
						Text:       district,
						Type:       "DISTRICT",
						Start:      start,
						End:        start + len(district),
						Confidence: 0.9,
						Value:      map[string]string{"province": province, "district": district},
					}
					foundDistricts = append(foundDistricts, districtEntity)
				}
			}
		}
	}

	// 添加所有找到的区县实体
	entities = append(entities, foundDistricts...)

	return entities
}

// mapPosToEntityType 将词性映射到实体类型
func (nlp *AdvancedNLPProcessor) mapPosToEntityType(pos string) string {
	posMapping := map[string]string{
		"ns": "LOCATION",     // 地名
		"nt": "ORGANIZATION", // 机构名
		"nz": "OTHER_PROPER", // 其他专名
		"m":  "NUMBER",       // 数词
		"mq": "QUANTITY",     // 数量词
		"tg": "TIME",         // 时间词
	}

	return posMapping[pos]
}

// parseEntityValue 解析实体值
func (nlp *AdvancedNLPProcessor) parseEntityValue(entityType string, match []string) interface{} {
	switch entityType {
	case "PRICE_RANGE":
		return nlp.parsePriceRange(match)
	case "SINGLE_PRICE":
		return nlp.parseSinglePrice(match)
	case "RENT_PRICE":
		return nlp.parseRentPrice(match)
	case "AREA_RANGE":
		return nlp.parseAreaRange(match)
	case "ROOM_LAYOUT":
		return nlp.parseRoomLayout(match)
	case "FLOOR_INFO":
		return nlp.parseFloorInfo(match)
	case "YEAR_INFO":
		return nlp.parseYearInfo(match)
	default:
		return match[0]
	}
}

// convertUnitToWan 将价格转换为万单位（健壮版本）
// 直接根据单位字符串转换，消除字符串查找的脆弱性
func (nlp *AdvancedNLPProcessor) convertUnitToWan(value float64, unit string) float64 {
	if unit == "亿" {
		return value * 10000 // 1亿 = 10000万
	}
	return value // 万或空字符串都按万处理
}

// parsePriceRange 解析价格区间（健壮版本，消除strings.Index风险）
func (nlp *AdvancedNLPProcessor) parsePriceRange(match []string) interface{} {
	result := make(map[string]interface{})

	// 遍历捕获组，寻找有效的价格模式
	for i := 1; i < len(match); i++ {
		if match[i] == "" {
			continue
		}

		// 检查价格区间模式: 数字-数字+单位
		if i+2 < len(match) && match[i+1] != "" && match[i+2] != "" {
			// 模式: "5000-1万" 或 "1-2亿"
			if min, err := strconv.ParseFloat(match[i], 64); err == nil {
				if max, err := strconv.ParseFloat(match[i+1], 64); err == nil {
					unit := match[i+2]
					if unit == "万" || unit == "亿" {
						result["min"] = nlp.convertUnitToWan(min, unit)
						result["max"] = nlp.convertUnitToWan(max, unit)
						result["unit"] = "万"
						return result
					}
				}
			}
		}

		// 检查混合单位模式: 数字+单位-数字+单位
		if i+3 < len(match) && match[i+1] != "" && match[i+2] != "" && match[i+3] != "" {
			// 模式: "5000万-1亿"
			if min, err := strconv.ParseFloat(match[i], 64); err == nil {
				if max, err := strconv.ParseFloat(match[i+2], 64); err == nil {
					minUnit := match[i+1]
					maxUnit := match[i+3]
					if (minUnit == "万" || minUnit == "亿") && (maxUnit == "万" || maxUnit == "亿") {
						result["min"] = nlp.convertUnitToWan(min, minUnit)
						result["max"] = nlp.convertUnitToWan(max, maxUnit)
						result["unit"] = "万"
						return result
					}
				}
			}
		}

		// 检查单值+操作符模式: 数字+单位+操作符
		if i+1 < len(match) && match[i+1] != "" {
			unit := match[i+1]
			if unit == "万" || unit == "亿" {
				if value, err := strconv.ParseFloat(match[i], 64); err == nil {
					// 查看完整匹配文本判断操作符
					fullText := match[0]
					if strings.Contains(fullText, "以下") || strings.Contains(fullText, "以内") {
						result["max"] = nlp.convertUnitToWan(value, unit)
						result["operator"] = "<="
					} else if strings.Contains(fullText, "以上") {
						result["min"] = nlp.convertUnitToWan(value, unit)
						result["operator"] = ">="
					} else {
						result["value"] = nlp.convertUnitToWan(value, unit)
					}
					result["unit"] = "万"
					return result
				}
			}
		}
	}

	// 处理元单位（保持向后兼容）
	for i := 1; i < len(match); i++ {
		if match[i] == "" {
			continue
		}
		if value, err := strconv.ParseFloat(match[i], 64); err == nil {
			fullText := match[0]
			if strings.Contains(fullText, "元") {
				if strings.Contains(fullText, "以下") {
					result["max"] = value
					result["operator"] = "<="
				} else if strings.Contains(fullText, "以上") {
					result["min"] = value
					result["operator"] = ">="
				} else {
					result["value"] = value
				}
				result["unit"] = "元"
				return result
			}
		}
	}

	return nil
}

// parseSinglePrice 解析单个价格
func (nlp *AdvancedNLPProcessor) parseSinglePrice(match []string) interface{} {
	result := make(map[string]interface{})

	// 查找数字匹配组
	for i := 1; i < len(match); i++ {
		if match[i] != "" {
			if price, err := strconv.ParseFloat(match[i], 64); err == nil {
				result["value"] = price
				result["unit"] = "万"
				break
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// parseRentPrice 解析租金价格
func (nlp *AdvancedNLPProcessor) parseRentPrice(match []string) interface{} {
	result := make(map[string]interface{})

	// 新的正则分组：
	// 1,2: 月租金(\d+)[-到至~](\d+)
	// 3,4: 月租(\d+)[-到至~](\d+)
	// 5,6: 租金(\d+)[-到至~](\d+)元?/?月?
	// 7,8: 月租金(\d+)到(\d+)元?
	// 9,10: 租金(\d+)到(\d+)元?
	// 11: 月租金(\d+)左右
	// 12: 月租(\d+)左右
	// 13: 租金(\d+)左右
	// 14: 月租金(\d+)
	// 15: 月租(\d+)
	// 16: 租金(\d+)元?/?月?

	if len(match) > 2 && match[1] != "" && match[2] != "" {
		// 区间格式: "月租金3000-5000"
		if min, err := strconv.ParseFloat(match[1], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(match[2], 64); err == nil {
			result["max"] = max
		}
		result["unit"] = "元"
	} else if len(match) > 4 && match[3] != "" && match[4] != "" {
		// 区间格式: "月租3000-5000"
		if min, err := strconv.ParseFloat(match[3], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(match[4], 64); err == nil {
			result["max"] = max
		}
		result["unit"] = "元"
	} else if len(match) > 6 && match[5] != "" && match[6] != "" {
		// 区间格式: "租金3000-5000元/月"
		if min, err := strconv.ParseFloat(match[5], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(match[6], 64); err == nil {
			result["max"] = max
		}
		result["unit"] = "元"
	} else if len(match) > 8 && match[7] != "" && match[8] != "" {
		// 区间格式: "月租金3000到5000元"
		if min, err := strconv.ParseFloat(match[7], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(match[8], 64); err == nil {
			result["max"] = max
		}
		result["unit"] = "元"
	} else if len(match) > 10 && match[9] != "" && match[10] != "" {
		// 区间格式: "租金3000到5000元"
		if min, err := strconv.ParseFloat(match[9], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(match[10], 64); err == nil {
			result["max"] = max
		}
		result["unit"] = "元"
	} else {
		// 单个价格值
		for i := 11; i < len(match); i++ {
			if match[i] != "" {
				if price, err := strconv.ParseFloat(match[i], 64); err == nil {
					result["value"] = price
					result["unit"] = "元"
					break
				}
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// parseAreaRange 解析面积区间
func (nlp *AdvancedNLPProcessor) parseAreaRange(match []string) interface{} {
	result := make(map[string]interface{})

	if len(match) >= 3 && match[1] != "" && match[2] != "" {
		// 组1,2: 面积区间 "80-120平米"
		if min, err := strconv.ParseFloat(match[1], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(match[2], 64); err == nil {
			result["max"] = max
		}
	} else if len(match) >= 4 && match[3] != "" {
		// 组3: "120平米以上"
		if area, err := strconv.ParseFloat(match[3], 64); err == nil {
			result["min"] = area
			result["operator"] = ">="
		}
	} else if len(match) >= 5 && match[4] != "" {
		// 组4: "120平米以下"
		if area, err := strconv.ParseFloat(match[4], 64); err == nil {
			result["max"] = area
			result["operator"] = "<="
		}
	} else if len(match) >= 6 && match[5] != "" {
		// 组5: "面积120"
		if area, err := strconv.ParseFloat(match[5], 64); err == nil {
			result["value"] = area
		}
	} else if len(match) >= 7 && match[6] != "" {
		// 组6: "120平米左右"
		if area, err := strconv.ParseFloat(match[6], 64); err == nil {
			result["value"] = area
			result["operator"] = "about"
		}
	} else {
		// 组7,8: 其他单个面积值
		for i := 7; i < len(match); i++ {
			if match[i] != "" {
				if area, err := strconv.ParseFloat(match[i], 64); err == nil {
					result["value"] = area
					break
				}
			}
		}
	}

	result["unit"] = "平米"
	return result
}

// parseRoomLayout 解析房型布局
func (nlp *AdvancedNLPProcessor) parseRoomLayout(match []string) interface{} {
	result := make(map[string]interface{})

	// 正则表达式分组（套房通过预处理convertRoomLayout转换）：
	// 1,2,3: (\d+)室(\d+)厅(\d+)卫
	// 4,5: (\d+)房(\d+)厅
	// 6: ([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾1-9])居室
	// 7,8: ([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾1-9])室([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾1-9])厅
	// 9,10,11: (\d+)房(\d+)厅(\d+)卫
	// 12,13: (\d+)室(\d+)厅
	// 14: (\d+)居室
	// 15: (\d+)房间
	// 16: (\d+)室 - 单独的阿拉伯数字室
	// 17: ([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾])室 - 单独的中文数字室


	// 查找非空的匹配组
	for i := 1; i < len(match); i++ {
		if match[i] != "" {
			switch i {
			case 1, 4, 9, 12: // 室/房的数量
				if num, err := strconv.Atoi(match[i]); err == nil {
					result["bedrooms"] = num
				}
			case 2, 5, 10, 13: // 厅的数量
				if num, err := strconv.Atoi(match[i]); err == nil {
					result["living_rooms"] = num
				}
			case 3, 11: // 卫的数量
				if num, err := strconv.Atoi(match[i]); err == nil {
					result["bathrooms"] = num
				}
			case 6: // 居室的情况 - 特殊处理"一居室"等
				// "一居室"通常意思是1室1厅
				bedrooms := common.ConvertChineseToInt(match[i])
				if bedrooms > 0 {
					result["bedrooms"] = bedrooms
					result["living_rooms"] = 1
					result["description"] = fmt.Sprintf("%d室1厅", bedrooms)
				}
			case 7: // 中文数字室
				bedrooms := common.ConvertChineseToInt(match[i])
				if bedrooms > 0 {
					result["bedrooms"] = bedrooms
				}
			case 8: // 中文数字厅
				livingRooms := common.ConvertChineseToInt(match[i])
				if livingRooms > 0 {
					result["living_rooms"] = livingRooms
				}
			case 14: // 阿拉伯数字居室 - 处理"1居室"等
				if num, err := strconv.Atoi(match[i]); err == nil {
					result["bedrooms"] = num
					result["living_rooms"] = 1
					result["description"] = fmt.Sprintf("%d室1厅", num)
				}
			case 15: // 房间数量
				if num, err := strconv.Atoi(match[i]); err == nil {
					result["bedrooms"] = num
					result["living_rooms"] = 1
					result["description"] = fmt.Sprintf("%d室1厅", num)
				}
			case 16: // 单独的阿拉伯数字"N室"
				if num, err := strconv.Atoi(match[i]); err == nil {
					result["bedrooms"] = num
					result["living_rooms"] = 1
					result["description"] = fmt.Sprintf("%d室1厅", num)
				}
			case 17: // 单独的中文数字"N室"（包括大写）
				bedrooms := common.ConvertChineseToInt(match[i])
				if bedrooms > 0 {
					result["bedrooms"] = bedrooms
					result["living_rooms"] = 1
					result["description"] = fmt.Sprintf("%d室1厅", bedrooms)
				}
			}
		}
	}

	// 如果没有解析出任何内容，返回nil
	if len(result) == 0 {
		return nil
	}

	// 生成描述字符串
	if desc, exists := result["description"]; !exists || desc == "" {
		var descParts []string
		if bedrooms, ok := result["bedrooms"].(int); ok {
			descParts = append(descParts, fmt.Sprintf("%d室", bedrooms))
		}
		if livingRooms, ok := result["living_rooms"].(int); ok {
			descParts = append(descParts, fmt.Sprintf("%d厅", livingRooms))
		}
		if bathrooms, ok := result["bathrooms"].(int); ok {
			descParts = append(descParts, fmt.Sprintf("%d卫", bathrooms))
		}
		if len(descParts) > 0 {
			result["description"] = strings.Join(descParts, "")
		}
	}

	return result
}

// parseFloorInfo 解析楼层信息
func (nlp *AdvancedNLPProcessor) parseFloorInfo(match []string) interface{} {
	result := make(map[string]interface{})

	if len(match) >= 2 && match[1] != "" {
		if floor, err := strconv.Atoi(match[1]); err == nil {
			result["floor"] = floor
		}
	}

	if len(match) >= 4 && match[3] != "" && match[4] != "" {
		// "8/30层" 格式
		if current, err := strconv.Atoi(match[3]); err == nil {
			result["current"] = current
		}
		if total, err := strconv.Atoi(match[4]); err == nil {
			result["total"] = total
		}
	}

	return result
}

// parseYearInfo 解析年份信息
func (nlp *AdvancedNLPProcessor) parseYearInfo(match []string) interface{} {
	for i := 1; i < len(match); i++ {
		if match[i] != "" {
			if year, err := strconv.Atoi(match[i]); err == nil {
				return year
			}
		}
	}
	return nil
}

// ExtractNumbers 提取数字实体
func (nlp *AdvancedNLPProcessor) ExtractNumbers(text string) ([]NumberEntity, error) {
	if text == "" {
		return nil, ErrEmptyInput
	}

	var numbers []NumberEntity

	// 提取阿拉伯数字
	arabicMatches := nlp.numberRegex.FindAllString(text, -1)
	for _, match := range arabicMatches {
		if value, err := strconv.ParseFloat(match, 64); err == nil {
			start := strings.Index(text, match)
			number := NumberEntity{
				Text:       match,
				Value:      value,
				Start:      start,
				End:        start + len(match),
				Confidence: 0.9,
			}

			// 分析数字类型和单位
			number.Type, number.Unit = nlp.analyzeNumberContext(text, start)
			numbers = append(numbers, number)
		}
	}

	// 🔧 修复：不再在标准化后的文本中提取中文数字
	// 因为标准化过程已经将中文数字转换为阿拉伯数字
	// 如果再提取中文数字，会导致"八十"被识别为"8"+"10"=810的错误
	//
	// 💡 设计决策：extractChineseNumbers 函数已重构并保留，但当前未启用
	// 若未来需要在标准化前进行中文数字直接提取，可基于 common 包恢复使用
	//
	// 原代码：
	// chineseNumbers := nlp.extractChineseNumbers(text)
	// numbers = append(numbers, chineseNumbers...)

	return numbers, nil
}

// extractChineseNumbers 提取中文数字
// 🔄 当前状态：保留但未启用 (上层未合并)
// 📝 重构说明：已基于 common.ChineseToArabicExtended 重构，消除重复映射
// 🚀 未来规划：若需要中文数字直接抽取，可基于 common 包做更精细的上下文规则后恢复
func (nlp *AdvancedNLPProcessor) extractChineseNumbers(text string) []NumberEntity {
	var numbers []NumberEntity

	// 使用统一的中文数字映射表
	for chinese, value := range common.ChineseToArabicExtended {
		if strings.Contains(text, chinese) {
			start := strings.Index(text, chinese)
			number := NumberEntity{
				Text:       chinese,
				Value:      value,
				Start:      start,
				End:        start + len(chinese),
				Confidence: 0.8,
			}

			// 分析数字类型和单位
			number.Type, number.Unit = nlp.analyzeNumberContext(text, start)
			numbers = append(numbers, number)
		}
	}

	return numbers
}

// analyzeNumberContext 分析数字上下文
func (nlp *AdvancedNLPProcessor) analyzeNumberContext(text string, position int) (string, string) {
	// 分析数字前后的上下文来判断类型
	contextBefore := ""
	contextAfter := ""

	if position > 10 {
		contextBefore = text[position-10 : position]
	} else {
		contextBefore = text[:position]
	}

	if position+20 < len(text) {
		contextAfter = text[position : position+20]
	} else {
		contextAfter = text[position:]
	}

	context := contextBefore + contextAfter

	// 优先检查具体的单位词，避免被模糊词干扰

	// 面积相关 - 优先检查，因为"平"是专用单位
	if strings.Contains(context, "平") || strings.Contains(context, "㎡") ||
		strings.Contains(context, "面积") {
		return "area", "平米"
	}

	// 楼层相关 - 优先检查，因为"层"/"楼"是专用单位
	if strings.Contains(context, "层") || strings.Contains(context, "楼") {
		return "floor", "层"
	}

	// 房型相关 - 优先检查，因为"室"/"厅"/"卫"是专用单位
	if strings.Contains(context, "室") || strings.Contains(context, "厅") ||
		strings.Contains(context, "卫") {
		return "room", "间"
	}

	// 年份相关 - 优先检查
	if strings.Contains(context, "年") {
		return "year", "年"
	}

	// 价格相关 - 最后检查，避免"价"字的模糊匹配干扰其他专用单位
	if strings.Contains(context, "万") || strings.Contains(context, "元") ||
		strings.Contains(context, "价") || strings.Contains(context, "钱") {
		if strings.Contains(context, "万") {
			return "price", "万"
		}
		return "price", "元"
	}

	return "unknown", ""
}

// AnalyzeSentiment 分析情感
func (nlp *AdvancedNLPProcessor) AnalyzeSentiment(text string) (*SentimentResult, error) {
	// 简单的基于词典的情感分析
	positiveWords := []string{
		"好", "棒", "优秀", "满意", "喜欢", "赞", "不错", "完美", "理想",
		"方便", "便利", "舒适", "豪华", "精美", "漂亮", "干净", "新",
		"宽敞", "明亮", "安静", "交通便利", "配套齐全", "性价比高",
	}

	negativeWords := []string{
		"差", "坏", "糟糕", "不满意", "讨厌", "垃圾", "破", "旧", "脏",
		"吵", "暗", "小", "贵", "远", "不方便", "麻烦", "问题", "缺点",
		"噪音", "污染", "老旧", "狭窄", "昏暗", "偏僻",
	}

	positiveCount := 0
	negativeCount := 0

	text = strings.ToLower(text)

	for _, word := range positiveWords {
		if strings.Contains(text, word) {
			positiveCount++
		}
	}

	for _, word := range negativeWords {
		if strings.Contains(text, word) {
			negativeCount++
		}
	}

	// 计算情感得分
	totalWords := positiveCount + negativeCount
	var score float64
	var label string
	var confidence float64

	if totalWords == 0 {
		score = 0
		label = "neutral"
		confidence = 0.5
	} else {
		score = float64(positiveCount-negativeCount) / float64(totalWords)
		confidence = float64(totalWords) / 10.0 // 简单的置信度计算
		if confidence > 1.0 {
			confidence = 1.0
		}

		if score > 0.1 {
			label = "positive"
		} else if score < -0.1 {
			label = "negative"
		} else {
			label = "neutral"
		}
	}

	return &SentimentResult{
		Score:      score,
		Label:      label,
		Confidence: confidence,
	}, nil
}

// ExtractPredicate 提取句子的谓语动词
// Jenny式设计：专注于识别主要动作动词，用于过滤租售场景
func (nlp *AdvancedNLPProcessor) ExtractPredicate(text string) string {
	// 分词
	segments := nlp.segmenter.Cut(text)
	if len(segments) == 0 {
		return ""
	}

	// 租售相关的动词（只包含核心动词，不包含模态动词组合）
	rentalSellingVerbs := map[string]bool{
		"出租": true, "租": true, "求租": true,
		"出售": true, "售": true, "卖": true, "售卖": true,
		"转让": true, "出让": true, "转手": true,
	}

	// 模态动词和助动词（通常在主要动词前）
	modalVerbs := map[string]bool{
		"想": true, "要": true, "想要": true, "打算": true, "准备": true,
		"需要": true, "希望": true, "计划": true, "考虑": true,
		"能": true, "可以": true, "应该": true, "必须": true,
		"我要": true, "我想": true, "我准备": true, "我打算": true,
	}

	// 🆕 从句标记词 - 这些词后面的动词通常是从句动词，不是主句谓语
	subordinateMarkers := map[string]bool{
		"后": true, "以后": true, "之后": true, // 时间从句
		"然后": true, "接着": true, "再": true, // 顺序从句
		"用来": true, "用于": true, "用作": true, // 目的从句
		"为了": true, "以便": true, "以免": true, // 目的从句
		"如果": true, "要是": true, "假如": true, // 条件从句
		"虽然": true, "尽管": true, "即使": true, // 让步从句
		"因为": true, "由于": true, "既然": true, // 原因从句
	}

	// 🆕 检查是否进入从句区域
	inSubordinate := false

	// 🔧 Jenny式改进：处理复合分词段
	// 检查每个分词段内部是否包含租售动词
	for i, segment := range segments {
		// 检查是否遇到从句标记
		if subordinateMarkers[segment] {
			inSubordinate = true
			continue
		}

		// 遇到逗号，句号等标点可能结束从句
		if segment == "，" || segment == "。" || segment == "；" {
			// 从句可能继续，不要立即重置
			continue
		}

		// 🆕 如果在从句中，跳过租售动词检查
		// 除非这是句子的第一个动词（说明没有主句动词）
		if inSubordinate && i > 2 {
			// 从句中的动词不作为主要谓语
			continue
		}
		// 跳过明显的名词性复合词
		if strings.Contains(segment, "租金") || strings.Contains(segment, "售价") ||
			strings.Contains(segment, "租赁费") || strings.Contains(segment, "出售价") {
			continue
		}

		// 1️⃣ 首先检查完整匹配（优先级最高）
		if rentalSellingVerbs[segment] {
			// 额外检查：确保不是复合词的一部分
			if i > 0 {
				prevWord := segments[i-1]
				if prevWord+segment == "租金" || prevWord+segment == "售价" {
					continue
				}
			}
			return segment
		}

		// 2️⃣ 然后检查分词段内部是否包含租售动词
		// 处理"想租个"、"卖房子"这种复合分词
		// 按长度降序检查，优先匹配长的动词
		var foundVerb string
		maxLen := 0
		for verb := range rentalSellingVerbs {
			if strings.Contains(segment, verb) && len(verb) > maxLen {
				// 确保不是"租金"这样的名词
				if verb == "租" && strings.Contains(segment, "租金") {
					continue
				}
				if verb == "售" && (strings.Contains(segment, "售价") || strings.Contains(segment, "售楼")) {
					continue
				}
				if verb == "卖" && strings.Contains(segment, "买卖") {
					continue // "买卖"是名词，不是动词
				}
				// 找到了更长的租售动词
				foundVerb = verb
				maxLen = len(verb)
			}
		}
		if foundVerb != "" {
			return foundVerb
		}

		// 检查模态动词+动词组合
		if modalVerbs[segment] && i+1 < len(segments) {
			nextWord := segments[i+1]

			// 🆕 检查下一个词内部是否包含租售动词
			// 处理"卖房子"这种情况
			for verb := range rentalSellingVerbs {
				if strings.Contains(nextWord, verb) {
					// 排除名词情况
					if verb == "租" && strings.Contains(nextWord, "租金") {
						continue
					}
					if verb == "售" && (strings.Contains(nextWord, "售价") || strings.Contains(nextWord, "售楼")) {
						continue
					}
					if verb == "卖" && strings.Contains(nextWord, "买卖") {
						continue
					}
					return verb
				}
			}

			// 如果下一个词是完整的租售动词
			if rentalSellingVerbs[nextWord] {
				return nextWord
			}
		}
	}

	// 没有找到租售相关动词，尝试识别其他主要动词
	// 常见的购房相关非租售动词（这些不应该被过滤）
	commonVerbs := []string{
		"找", "买", "购买", "购置", "选", "选择", "看", "查看",
		"了解", "咨询", "投资", "置业", "入手", "考虑",
	}

	for i, word := range segments {
		// 跳过模态动词
		if modalVerbs[word] {
			continue
		}

		// 检查是否是常见动词
		for _, verb := range commonVerbs {
			if word == verb || strings.HasPrefix(word, verb) {
				// 特殊处理"买"，需要确认不是"买房"的买
				if verb == "买" {
					// 检查是否跟着租售相关的词
					if i+1 < len(segments) {
						nextWord := segments[i+1]
						if strings.Contains(nextWord, "房") || strings.Contains(nextWord, "套") {
							// 这是购房，不是出售，返回"买"
							return "买"
						}
					}
				}
				return word
			}
		}
	}

	// 如果还没找到，返回第一个可能的动词（基于词性判断）
	// 简单启发式：2-3个字的词，不是名词性词缀的，可能是动词
	nounSuffixes := []string{"房", "室", "厅", "区", "市", "路", "街", "园", "楼", "万", "平", "米"}
	for _, word := range segments {
		if len(word) >= 2 && len(word) <= 6 {
			isNoun := false
			for _, suffix := range nounSuffixes {
				if strings.HasSuffix(word, suffix) {
					isNoun = true
					break
				}
			}
			if !isNoun && !modalVerbs[word] {
				// 可能是动词
				return word
			}
		}
	}

	return ""
}

// NormalizeText 文本标准化
func (nlp *AdvancedNLPProcessor) NormalizeText(text string) string {
	// 1. 移除多余空白
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	// 2. 标准化标点符号
	punctuationMap := map[string]string{
		"，": ",",
		"。": ".",
		"！": "!",
		"？": "?",
		"；": ";",
		"：": ":",
		"（": "(",
		"）": ")",
		"【": "[",
		"】": "]",
		"《": "<",
		"》": ">",
	}

	// 🔧 修复稳定性问题：确保确定性的遍历顺序
	var punctKeys []string
	for chinese := range punctuationMap {
		punctKeys = append(punctKeys, chinese)
	}
	sort.Strings(punctKeys)

	for _, chinese := range punctKeys {
		english := punctuationMap[chinese]
		text = strings.ReplaceAll(text, chinese, english)
	}

	// 3. 🏠 居室转换预处理 - 优先处理，避免分词冲突
	text = nlp.convertRoomLayout(text)

	// 4. 繁简转换（简单版本）
	text = nlp.simplifyTraditionalChinese(text)

	// 5. 数字标准化
	text = nlp.normalizeNumbers(text)

	// 6. 大小写标准化
	text = nlp.normalizeCases(text)

	return text
}

// simplifyTraditionalChinese 繁体转简体（简单版本）
func (nlp *AdvancedNLPProcessor) simplifyTraditionalChinese(text string) string {
	traditionalToSimplified := map[string]string{
		"樓": "楼", "層": "层", "間": "间", "廳": "厅", "衛": "卫",
		"區": "区", "門": "门", "車": "车", "電": "电", "線": "线",
		"開": "开", "關": "关", "買": "买", "賣": "卖", "價": "价",
		"錢": "钱", "員": "员", "國": "国", "學": "学", "會": "会",
	}

	// 🔧 修复稳定性问题：确保确定性的遍历顺序
	var traditionalKeys []string
	for traditional := range traditionalToSimplified {
		traditionalKeys = append(traditionalKeys, traditional)
	}
	sort.Strings(traditionalKeys)

	for _, traditional := range traditionalKeys {
		simplified := traditionalToSimplified[traditional]
		text = strings.ReplaceAll(text, traditional, simplified)
	}

	return text
}

// normalizeNumbers 数字标准化
func (nlp *AdvancedNLPProcessor) normalizeNumbers(text string) string {
	// 使用字典中的同义词映射
	if nlp.dictionary != nil {
		text = nlp.dictionary.NormalizeText(text)
	}

	return text
}

// normalizeCases 大小写标准化
func (nlp *AdvancedNLPProcessor) normalizeCases(text string) string {
	// 英文单词首字母大写
	words := strings.Fields(text)
	for i, word := range words {
		if nlp.isEnglishWord(word) {
			words[i] = strings.Title(strings.ToLower(word))
		}
	}

	return strings.Join(words, " ")
}

// isEnglishWord 判断是否为英文单词
func (nlp *AdvancedNLPProcessor) isEnglishWord(word string) bool {
	for _, r := range word {
		if !unicode.IsLetter(r) || r > unicode.MaxASCII {
			return false
		}
	}
	return len(word) > 0
}

// deduplicateEntities 去重实体
func (nlp *AdvancedNLPProcessor) deduplicateEntities(entities []Entity) []Entity {
	if len(entities) <= 1 {
		return entities
	}

	// 🔧 修复稳定性问题：确保确定性的遍历顺序
	// 1. 按位置分组
	positionGroups := make(map[string][]Entity)

	for _, entity := range entities {
		key := fmt.Sprintf("%d_%d", entity.Start, entity.End)
		positionGroups[key] = append(positionGroups[key], entity)
	}

	// 2. 确保确定性顺序：按位置键排序
	var keys []string
	for key := range positionGroups {
		keys = append(keys, key)
	}
	sort.Strings(keys) // 按字符串排序确保稳定顺序

	var result []Entity

	// 3. 处理每个位置组
	for _, key := range keys {
		group := positionGroups[key]
		if len(group) == 1 {
			result = append(result, group[0])
		} else {
			// 4. 多个实体在同一位置，进行智能去重
			best := nlp.selectBestEntity(group)
			result = append(result, best)
		}
	}

	// 5. 处理重叠实体（不同位置但内容重叠）
	result = nlp.resolveOverlappingEntities(result)

	// 6. 最终排序：按位置排序确保输出稳定性
	sort.Slice(result, func(i, j int) bool {
		if result[i].Start != result[j].Start {
			return result[i].Start < result[j].Start
		}
		return result[i].End < result[j].End
	})

	return result
}

// selectBestEntity 从同位置的多个实体中选择最佳实体
func (nlp *AdvancedNLPProcessor) selectBestEntity(entities []Entity) Entity {
	if len(entities) == 1 {
		return entities[0]
	}

	// 1. 按优先级排序（确保稳定性）
	sort.Slice(entities, func(i, j int) bool {
		priorityI := getEntityPriority(entities[i].Type)
		priorityJ := getEntityPriority(entities[j].Type)
		if priorityI != priorityJ {
			return priorityI > priorityJ // 高优先级在前
		}
		// 优先级相同时，按类型名称排序确保稳定性
		return entities[i].Type < entities[j].Type
	})

	// 2. 选择最高优先级的实体
	best := entities[0]

	// 3. 如果多个实体优先级相同，选择置信度最高的
	for _, entity := range entities {
		if getEntityPriority(entity.Type) == getEntityPriority(best.Type) {
			if entity.Confidence > best.Confidence {
				best = entity
			} else if entity.Confidence == best.Confidence {
				// 置信度也相同，选择文本更具体的（更长的）
				if len(entity.Text) > len(best.Text) {
					best = entity
				} else if len(entity.Text) == len(best.Text) {
					// 长度也相同，按字典序选择确保稳定性
					if entity.Text < best.Text {
						best = entity
					}
				}
			}
		}
	}

	return best
}

// resolveOverlappingEntities 解决重叠实体问题
func (nlp *AdvancedNLPProcessor) resolveOverlappingEntities(entities []Entity) []Entity {
	if len(entities) <= 1 {
		return entities
	}

	// 按开始位置排序
	sort.Slice(entities, func(i, j int) bool {
		return entities[i].Start < entities[j].Start
	})

	var result []Entity
	result = append(result, entities[0])

	for i := 1; i < len(entities); i++ {
		current := entities[i]
		last := &result[len(result)-1]

		// 检查是否重叠
		if current.Start < last.End {
			// 重叠情况：选择更好的实体
			if nlp.isEntityBetter(current, *last) {
				// 用当前实体替换最后一个
				result[len(result)-1] = current
			}
			// 否则忽略当前实体
		} else {
			// 无重叠，直接添加
			result = append(result, current)
		}
	}

	return result
}

// isEntityBetter 判断实体A是否比实体B更好
func (nlp *AdvancedNLPProcessor) isEntityBetter(a, b Entity) bool {
	// 1. 优先级比较
	priorityA := getEntityPriority(a.Type)
	priorityB := getEntityPriority(b.Type)
	if priorityA != priorityB {
		return priorityA > priorityB
	}

	// 2. 置信度比较
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}

	// 3. 文本长度比较（更具体的更好）
	if len(a.Text) != len(b.Text) {
		return len(a.Text) > len(b.Text)
	}

	// 4. 位置比较（起始位置更早的更好）
	if a.Start != b.Start {
		return a.Start < b.Start
	}

	// 5. 字典序比较（确保稳定性）
	return a.Text < b.Text
}

// getEntityPriority 获取实体类型的优先级，数字越大优先级越高
func getEntityPriority(entityType string) int {
	priorities := map[string]int{
		"PLATE":          11, // 板块最具体（如陆家嘴、张江等）
		"DISTRICT":       10, // 区县最具体
		"PROVINCE":       8,  // 省份次之
		"COMMERCIAL":     7,  // 商业地产信息比通用地点更重要
		"INTEREST_POINT": 8,  // 兴趣点比通用地点更重要
		"SINGLE_PRICE":   9,  // 单个价格比价格区间更具体
		"PRICE_RANGE":    8,  // 价格区间
		"ORIENTATION":    7,  // 朝向
		"ROOM_LAYOUT":    9,  // 房型 - 提高优先级确保稳定性
		"DECORATION":     6,  // 装修
		"PROPERTY_TYPE":  6,  // 房屋类型
		"LOCATION":       5,  // 通用地点
		"RENT_PRICE":     9,  // 租金价格比价格区间更具体
	}

	if priority, exists := priorities[entityType]; exists {
		return priority
	}
	return 1 // 默认优先级
}

// GetProcessorStats 获取处理器统计信息
func (nlp *AdvancedNLPProcessor) GetProcessorStats() map[string]interface{} {
	return map[string]interface{}{
		"initialized":          nlp.initialized,
		"entity_rules":         len(nlp.entityRules),
		"segmenter_available":  nlp.segmenter != nil,
		"dictionary_available": nlp.dictionary != nil,
	}
}

// ConvertToPinyin 中文转拼音
func (nlp *AdvancedNLPProcessor) ConvertToPinyin(text string) ([]string, error) {
	args := pinyin.NewArgs()
	args.Style = pinyin.NORMAL
	args.Heteronym = false
	args.Separator = " "
	args.Fallback = func(r rune, a pinyin.Args) []string {
		return []string{string(r)}
	}

	return pinyin.LazyConvert(text, &args), nil
}
