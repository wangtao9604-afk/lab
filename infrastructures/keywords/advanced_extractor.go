package keywords

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"qywx/infrastructures/common"
	"qywx/infrastructures/config"

	"github.com/sirupsen/logrus"
)

// FilterStrategy 租售过滤策略
type FilterStrategy int

const (
	StrictFilter       FilterStrategy = iota // 默认：完全过滤
	IgnoreRentalFields                       // 仅忽略租金相关字段
	NoFilter                                 // 不过滤
)

// parseFilterStrategy 从配置字符串解析租售过滤策略
// 配置值支持以下选项：
// - "strict" -> StrictFilter (默认)
// - "ignore" -> IgnoreRentalFields
// - "none" -> NoFilter
func parseFilterStrategy(configValue string) FilterStrategy {
	normalizedValue := strings.ToLower(strings.TrimSpace(configValue))

	switch normalizedValue {
	case "ignore":
		return IgnoreRentalFields
	case "none":
		return NoFilter
	case "strict", "":
		// 默认值和空值都使用严格过滤
		return StrictFilter
	default:
		// 未知值使用默认策略，并记录警告
		logrus.WithField("config_value", configValue).Warn("Unknown rental filter strategy value, using StrictFilter")
		return StrictFilter
	}
}

// Package-level regex rules variable
// Will be initialized in init() function with fail-fast strategy
var globalRegexRules map[string]*regexp.Regexp

// init 包初始化函数 - 采用fail-fast策略
// 如果任何正则表达式编译失败，程序将在启动时立即panic
// 这是有意为之 - 错误的正则表达式是必须立即修复的bug
func init() {
	globalRegexRules = map[string]*regexp.Regexp{
		// 增强的价格正则 - 支持多种上限表达方式（注意顺序：更具体的模式必须在前）
		// 重要：具体模式（带"以内"等限定词）必须在通用模式之前，否则会被错误匹配
		"price": regexp.MustCompile(`(?i)` +
			`(\d+(?:\.\d+)?)\s*[-到至~]\s*(\d+(?:\.\d+)?)(万|亿)|` + // 组1,2,3: 价格区间 "300-500万" 或 "1-2亿"
			`(\d+(?:\.\d+)?)(万|亿)\s*[-到至~]\s*(\d+(?:\.\d+)?)(万|亿)|` + // 组4,5,6,7: 混合区间 "5000万-1亿"
			`(\d+(?:\.\d+)?)万(?:以下|以内|之内|内)|` + // 组3: "xxx万以下/以内/之内/内"
			`(?:不超过?|最多|不超)(\d+(?:\.\d+)?)万|` + // 组4: "不超过/最多/不超xxx万"
			`(\d+(?:\.\d+)?)万以上|` + // 组5: "xxx万以上"
			`预算(\d+(?:\.\d+)?)[-到至~](\d+(?:\.\d+)?)万|` + // 组6,7: "预算300-500万"
			`预算(\d+(?:\.\d+)?)万(?:以内|以下|之内|内)|` + // 组8: "预算xxx万以内/以下/之内/内"
			`预算(?:不超过?|最多|不超)(\d+(?:\.\d+)?)万|` + // 组9: "预算不超过/最多xxx万"
			`预算(\d+(?:\.\d+)?)万左右|` + // 组10: "预算xxx万左右"
			`(\d+(?:\.\d+)?)万左右|` + // 组11: "xxx万左右"
			`预算(\d+(?:\.\d+)?)万元|` + // 组12: "预算xxx万元"（需在"预算xxx万"之前）
			`预算(\d+(?:\.\d+)?)万|` + // 组13: "预算xxx万"（通用模式）
			`(\d+(?:\.\d+)?)万元?` + // 组14: "xxx万/万元"（最通用的模式放最后）
			``),

		// 租金正则
		"rent_price": regexp.MustCompile(`(?i)月租金(\d+)[-到至~](\d+)|月租(\d+)[-到至~](\d+)|租金(\d+)[-到至~](\d+)|月租金(\d+)|月租(\d+)|租金(\d+)`),

		// 增强的面积正则 - 仿照价格模式支持operator（注意顺序：更具体的模式在前）
		"area": regexp.MustCompile(`(?i)` +
			// 先处理所有面积开头的模式，从最具体到最通用
			`面积(\d+(?:\.\d+)?)[-到至~](\d+(?:\.\d+)?)平(?:方米|米|㎡)?|` + // 组1,2: "面积80-120平"
			`面积(\d+(?:\.\d+)?)平(?:方米|米|㎡)?(?:以内|以下|之内)|` + // 组3: "面积xxx平以内"
			`面积(?:不超过?|最多|不超)(\d+(?:\.\d+)?)平(?:方米|米|㎡)?|` + // 组4: "面积不超过xxx平"
			`面积(\d+(?:\.\d+)?)平(?:方米|米|㎡)?左右|` + // 组5: "面积xxx平左右"
			`面积(\d+(?:\.\d+)?)平(?:方米|米|㎡)?|` + // 组6: "面积xxx平"（通用模式）
			// 再处理不带面积前缀的模式
			`(\d+(?:\.\d+)?)[-到至~](\d+(?:\.\d+)?)平(?:方米|米|㎡)?|` + // 组7,8: 面积区间 "80-120平"
			`(\d+(?:\.\d+)?)平(?:方米|米|㎡)?(?:以下|以内|之内)|` + // 组9: "xxx平以下/以内/之内"
			`(?:不超过?|最多|不超)(\d+(?:\.\d+)?)平(?:方米|米|㎡)?|` + // 组10: "不超过/最多xxx平"
			`(\d+(?:\.\d+)?)平(?:方米|米|㎡)?以上|` + // 组11: "xxx平以上"
			`(\d+(?:\.\d+)?)平(?:方米|米|㎡)?左右|` + // 组12: "xxx平左右"
			`(\d+(?:\.\d+)?)平方米?|` + // 组13: "xxx平方米"（通用模式）
			`(\d+(?:\.\d+)?)㎡` + // 组14: "xxx㎡"
			``),

		// 增强的房型正则（支持中文数字）- 与nlp_processor.go保持一致
		// 新增：单独的"N室"模式，支持阿拉伯数字、中文数字、中文大写数字
		"room_layout": regexp.MustCompile(`(?i)(\d+)室(\d+)厅(\d+)卫|(\d+)房(\d+)厅|([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾1-9])居室|([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾1-9])室([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾1-9])厅|(\d+)房(\d+)厅(\d+)卫|(\d+)室(\d+)厅|(\d+)居室|(\d+)房间|(\d+)室|([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾])室`),

		// 增强的楼层正则
		"floor": regexp.MustCompile(`(?i)(\d+)[-/](\d+)层|(\d+)楼|(\d+)层|第(\d+)层|楼层(\d+)|(\d+)楼层|高层|中层|低层`),

		// 年份正则
		"year": regexp.MustCompile(`(?i)(\d{4})年|建成(\d+)年|房龄(\d+)年|(\d+)年代|(\d{2})年建|(\d{4})年建成`),

		// 地铁交通正则 - 增强中文站名抓取能力
		"subway": regexp.MustCompile(`(?i)([\p{Han}\d]+)号线|地铁([\p{Han}\d]+)站|([\p{Han}\d]+)地铁站|距离([\p{Han}\d]+)站|轨交([\p{Han}\d]+)号线|([\p{Han}\d]+)号线([\p{Han}\d]+)站|地铁([\p{Han}\d]+)号线`),

		// 距离正则
		"distance": regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)米|(\d+(?:\.\d+)?)公里|(\d+(?:\.\d+)?)km|步行(\d+)分钟|车程(\d+)分钟|距离(\d+(?:\.\d+)?)`),

		// 车位正则
		"parking": regexp.MustCompile(`(?i)(\d+)个车位|车位(\d+)个|停车位(\d+)|(\d+)个停车位|车位比(\d+:\d+)|(\d+)比(\d+)`),

		// 物业费正则
		"property_fee": regexp.MustCompile(`(?i)物业费(\d+(?:\.\d+)?)元|(\d+(?:\.\d+)?)元物业费|物业管理费(\d+(?:\.\d+)?)`),

		// 地点模式规则 - 识别各种地标、园区、商圈等
		// 匹配格式：[0-8个中文字符] + [地标关键词]
		// 示例：科学城、张江科技城、浦东新区等
		"location_pattern": regexp.MustCompile(`([\p{Han}]{0,8}?(?:科学城|科技城|产业园|工业园|开发区|高新区|保税区|自贸区|新区|新城|园区|中心|广场|大厦|大楼|商圈|商业中心|CBD|金融城|创新城|智慧城|生态城|国际城))`),

		// 上下文地点规则 - 通过"靠近"、"附近"等词识别地点
		// 匹配格式：[位置词] + [空格(可选)] + [地名]
		// 示例：靠近科学城、附近购物中心、临近地铁站等
		"location_context": regexp.MustCompile(`(?:靠近|附近|旁边|临近|紧邻|毗邻|近|离|距离|到|去)[\s]*([\p{Han}]{0,8}?(?:城|园|区|圈|厦|楼|场|馆|站|中心|广场))`),
	}
}

// AdvancedFieldExtractor 高级字段提取器
type AdvancedFieldExtractor struct {
	dictionary          *KeywordDictionary
	segmenter           *AdvancedSegmenter
	nlpProcessor        *AdvancedNLPProcessor
	regexRules          map[string]*regexp.Regexp // 现在指向全局共享的正则表达式
	confidenceThreshold float64
	metrics             *ExtractionMetrics
	metricsMu           sync.RWMutex // 保护metrics的并发访问
	initialized         bool
	logger              *logrus.Logger
	// Pre-compiled regex for parseNumberWithUnit to avoid re-compilation on every call
	numberUnitRegex *regexp.Regexp
	// 租售过滤策略
	rentalFilter FilterStrategy
}

// NewAdvancedFieldExtractor 创建高级字段提取器
func NewAdvancedFieldExtractor() *AdvancedFieldExtractor {
	cfg := config.GetInstance()

	// 初始化组件
	dictionary := NewKeywordDictionary()
	segmenter := NewAdvancedSegmenter()
	nlpProcessor := NewAdvancedNLPProcessor(segmenter, dictionary)

	// 初始化日志记录器
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})

	extractor := &AdvancedFieldExtractor{
		dictionary:          dictionary,
		segmenter:           segmenter,
		nlpProcessor:        nlpProcessor,
		regexRules:          globalRegexRules, // 使用包级别初始化的正则表达式
		confidenceThreshold: 0.6,
		metrics: &ExtractionMetrics{
			FieldExtractionRate: make(map[string]float64),
			LastUpdated:         time.Now(),
		},
		logger: logger,
		// Pre-compile the regex for parseNumberWithUnit (performance optimization)
		numberUnitRegex: regexp.MustCompile(`^([\d一二两三四五六七八九十壹贰叁肆伍陆柒捌玖拾零]+)([十百千万拾佰仟萬])?`),
		// 从配置文件读取租售过滤策略配置
		rentalFilter: parseFilterStrategy(cfg.KeywordsConfig.RentalFilterStrategy),
	}

	extractor.initialized = true

	logger.Info("AdvancedFieldExtractor initialized successfully")

	return extractor
}

// ExtractFromMessage 从消息中提取关键词和结构化数据
func (afe *AdvancedFieldExtractor) ExtractFromMessage(msg string) (result *ExtractionResult, err error) {
	// panic恢复机制
	defer func() {
		if r := recover(); r != nil {
			afe.logger.WithFields(logrus.Fields{
				"panic": r,
				"msg":   msg,
			}).Error("Panic recovered in ExtractFromMessage")

			// 转换panic为错误
			switch x := r.(type) {
			case string:
				err = NewErrorf("panic: %s", x)
			case error:
				err = WrapError(x, "panic recovered")
			default:
				err = NewErrorf("panic: %v", x)
			}
			result = nil
		}
	}()

	if !afe.initialized {
		return nil, ErrNotInitialized
	}

	// 输入验证
	if msg == "" {
		return nil, ErrEmptyInput
	}

	// 限制输入长度（防止DoS）
	const maxInputLength = 10000
	if len(msg) > maxInputLength {
		return nil, WrapErrorf(ErrInputTooLong, "input length %d exceeds max %d", len(msg), maxInputLength)
	}

	// 根据租售过滤策略处理
	isRentalSelling := afe.containsRentalOrSellingKeywords(msg)
	if isRentalSelling && afe.rentalFilter == StrictFilter {
		// 严格过滤：完全返回空结果
		return &ExtractionResult{
			Fields:      make(map[string]interface{}),
			Confidence:  make(map[string]float64),
			Keywords:    []string{},
			Entities:    []Entity{},
			Numbers:     []NumberEntity{},
			ProcessTime: 0,
			Method:      "filtered_rental_selling",
		}, nil
	}

	startTime := time.Now()

	// 1. 文本预处理
	normalizedText := afe.nlpProcessor.NormalizeText(msg)

	// 2. 分词处理
	segments := afe.segmenter.Cut(normalizedText)

	// 3. 实体提取
	entities, err := afe.nlpProcessor.ExtractEntities(normalizedText)
	if err != nil {
		// 记录错误但继续处理
		afe.logger.WithFields(logrus.Fields{
			"error": err.Error(),
			"step":  "entity_extraction",
		}).Warn("Entity extraction failed, continuing with empty entities")
		entities = []Entity{}
	}

	// 4. 数字提取
	numbers, err := afe.nlpProcessor.ExtractNumbers(normalizedText)
	if err != nil {
		// 记录错误但继续处理
		afe.logger.WithFields(logrus.Fields{
			"error": err.Error(),
			"step":  "number_extraction",
		}).Warn("Number extraction failed, continuing with empty numbers")
		numbers = []NumberEntity{}
	}

	// 5. 综合分析提取字段
	fields := afe.extractFields(normalizedText, segments, entities, numbers, msg)

	// 6. 根据租售过滤策略处理字段
	if isRentalSelling && afe.rentalFilter == IgnoreRentalFields {
		afe.filterRentalFields(fields)
	}

	// 7. 计算置信度
	confidence := afe.calculateConfidence(fields, entities, numbers)

	// 8. 构建结果
	result = &ExtractionResult{
		Fields:      fields,
		Confidence:  confidence,
		Keywords:    segments,
		Entities:    entities,
		Numbers:     numbers,
		ProcessTime: time.Since(startTime),
		Method:      "advanced_hybrid",
	}

	// 9. 更新指标
	afe.updateMetrics(result)

	return result, nil
}

// ExtractFromMessageWithContext 从消息中提取关键词和结构化数据（带超时控制）
func (afe *AdvancedFieldExtractor) ExtractFromMessageWithContext(ctx context.Context, msg string) (*ExtractionResult, error) {
	// 创建带超时的内部context
	timeout := 5 * time.Second // 默认5秒超时
	if deadline, ok := ctx.Deadline(); ok {
		// 如果外部context有deadline，使用较小的那个
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 创建结果通道
	type result struct {
		res *ExtractionResult
		err error
	}
	resultChan := make(chan result, 1)

	// 异步执行提取
	go func() {
		res, err := afe.ExtractFromMessage(msg)
		select {
		case resultChan <- result{res, err}:
		case <-ctxWithTimeout.Done():
			// context已取消，不发送结果
		}
	}()

	// 等待结果或超时
	select {
	case <-ctxWithTimeout.Done():
		if ctxWithTimeout.Err() == context.DeadlineExceeded {
			return nil, WrapError(ErrTimeoutExceeded, "extraction exceeded timeout")
		}
		return nil, WrapError(ErrContextCancelled, "extraction was cancelled")
	case res := <-resultChan:
		return res.res, res.err
	}
}

// extractFields 提取字段数据
func (afe *AdvancedFieldExtractor) extractFields(text string, segments []string, entities []Entity, numbers []NumberEntity, originalText string) map[string]interface{} {
	fields := make(map[string]interface{})

	// 1. 从实体提取字段
	afe.extractFromEntities(fields, entities)

	// 2. 从正则表达式提取字段 (优先级高)
	afe.extractFromRegex(fields, text, originalText)

	// 3. 从数字提取字段 (作为补充)
	afe.extractFromNumbers(fields, numbers)

	// 4. 从词典匹配提取字段
	afe.extractFromDictionary(fields, text)

	// 5. 特殊字段处理
	afe.postProcessFields(fields, text, originalText)

	return fields
}

// extractFromEntities 从实体提取字段
func (afe *AdvancedFieldExtractor) extractFromEntities(fields map[string]interface{}, entities []Entity) {
	for _, entity := range entities {
		switch entity.Type {
		case "LOCATION", "PROVINCE", "DISTRICT", "PLATE":
			// 获取或创建location对象
			var location map[string]interface{}
			if existingLocation, ok := fields["location"].(map[string]interface{}); ok {
				location = existingLocation
			} else {
				location = make(map[string]interface{})
			}

			if entity.Type == "PROVINCE" {
				location["province"] = entity.Value
			} else if entity.Type == "DISTRICT" {
				if districtInfo, ok := entity.Value.(map[string]string); ok {
					location["province"] = districtInfo["province"]
					// 特殊处理"浦东新区"转换为"浦东"
					district := districtInfo["district"]
					if district == "浦东新区" {
						district = "浦东"
					}
					location["district"] = district
				}
			} else if entity.Type == "PLATE" {
				// 🆕 处理板块信息
				if plateInfo, ok := entity.Value.(map[string]string); ok {
					location["province"] = plateInfo["province"]
					location["district"] = plateInfo["district"]
					location["plate"] = plateInfo["plate"]
					location["landmark"] = plateInfo["plate"] // 板块也作为地标
				}
			} else if entity.Type == "LOCATION" {
				// 处理普通LOCATION类型
				locationValue := entity.Value
				if strValue, ok := locationValue.(string); ok {
					// 排除一些明显不是地理位置的词汇
					incorrectLocations := []string{"学区", "花园", "庭院", "车库", "停车场", "幼儿园"}
					isIncorrect := false
					for _, incorrect := range incorrectLocations {
						if strValue == incorrect {
							isIncorrect = true
							break
						}
					}

					if !isIncorrect {
						// 判断是省份还是区域
						if afe.isProvince(strValue) {
							location["province"] = strValue
						} else {
							location["district"] = strValue
							// 尝试找到对应的省份
							province := afe.findProvinceForDistrict(strValue)
							if province != "" {
								location["province"] = province
							}
						}
					}
				}
			}
			fields["location"] = location

		case "PROPERTY_TYPE":
			// 特殊处理别墅类型：智能组合别墅类型和子类型
			if existingType, ok := fields["property_type"].(string); ok {
				// 别墅类型列表
				villaSubTypes := []string{"独栋", "联排", "叠拼", "双拼"}
				// Jenny式防护：安全的类型断言
				currentValue, ok := entity.Value.(string)
				if !ok {
					// 类型断言失败，记录日志并跳过
					afe.logger.WithFields(logrus.Fields{
						"entity_type":  entity.Type,
						"entity_value": entity.Value,
						"value_type":   fmt.Sprintf("%T", entity.Value),
					}).Warn("Failed to assert entity.Value as string for PROPERTY_TYPE")
					return
				}

				// 辅助函数：检查字符串是否在切片中
				isVillaSubType := func(val string) bool {
					for _, subType := range villaSubTypes {
						if val == subType {
							return true
						}
					}
					return false
				}

				// 情况1：已有"别墅"，新实体是子类型 -> 组合成"子类型别墅"
				if existingType == "别墅" && isVillaSubType(currentValue) {
					fields["property_type"] = currentValue + "别墅"
				} else if currentValue == "别墅" && isVillaSubType(existingType) {
					// 情况2：已有子类型，新实体是"别墅" -> 组合成"子类型别墅"
					fields["property_type"] = existingType + "别墅"
				} else {
					// 其他情况，使用新值覆盖
					fields["property_type"] = currentValue
				}
			} else {
				fields["property_type"] = entity.Value
			}

		case "DECORATION":
			// 收集所有装修选项，支持多选
			// Jenny式防护：安全的类型断言
			decorationValue, ok := entity.Value.(string)
			if !ok {
				// 类型断言失败，记录日志并跳过
				afe.logger.WithFields(logrus.Fields{
					"entity_type":  entity.Type,
					"entity_value": entity.Value,
					"value_type":   fmt.Sprintf("%T", entity.Value),
				}).Warn("Failed to assert entity.Value as string for DECORATION")
				return
			}
			if existingDecoration, ok := fields["decoration"]; ok {
				// 如果已存在装修字段，转换为数组或添加到数组
				switch existing := existingDecoration.(type) {
				case string:
					// 如果现有值是字符串，转换为数组（智能去重）
					if !afe.isSameDecoration(existing, decorationValue) {
						fields["decoration"] = []string{existing, decorationValue}
					}
				case []string:
					// 如果已经是数组，检查是否重复再添加
					found := false
					for _, val := range existing {
						if afe.isSameDecoration(val, decorationValue) {
							found = true
							break
						}
					}
					if !found {
						fields["decoration"] = append(existing, decorationValue)
					}
				}
			} else {
				// 首次设置装修字段
				fields["decoration"] = decorationValue
			}

		case "ORIENTATION":
			fields["orientation"] = entity.Value

		case "PRICE_RANGE":
			if priceInfo, ok := entity.Value.(map[string]interface{}); ok {
				fields["price"] = priceInfo
			}

		case "SINGLE_PRICE":
			// 单值价格处理，优先级低于PRICE_RANGE
			if _, exists := fields["price"]; !exists {
				if m, ok := entity.Value.(map[string]interface{}); ok {
					fields["price"] = m
				}
			}

		case "RENT_PRICE":
			if rentInfo, ok := entity.Value.(map[string]interface{}); ok {
				fields["rent_price"] = rentInfo // 修复：将租金映射到 rent_price 字段
			}

		case "AREA_RANGE":
			if areaInfo, ok := entity.Value.(map[string]interface{}); ok {
				fields["area"] = areaInfo
			}

		case "ROOM_LAYOUT":
			if roomInfo, ok := entity.Value.(map[string]interface{}); ok {
				fields["room_layout"] = roomInfo
			}

		case "FLOOR_INFO":
			if floorInfo, ok := entity.Value.(map[string]interface{}); ok {
				fields["floor"] = floorInfo
			}

		case "EDUCATION":
			// 所有教育相关实体都添加到interest_points，不再设置education字段
			if value, ok := entity.Value.(string); ok {
				if strings.Contains(value, "幼儿园") {
					afe.addToInterestPoints(fields, "近幼儿园")
				} else {
					// 使用智能学校兴趣点处理逻辑
					afe.addSchoolInterestPoint(fields, value)
				}
			}

		case "COMMERCIAL":
			// 智能处理商业实体 - 组合多个相关商业特征
			afe.processCommercialEntity(fields, entity)

		case "INTEREST_POINT":
			// 直接添加兴趣点，使用智能学校兴趣点处理逻辑
			if value, ok := entity.Value.(string); ok {
				if strings.Contains(value, "幼儿园") {
					afe.addToInterestPoints(fields, "近幼儿园")
				} else if value == "学校好" || value == "学区房" || value == "重点小学" {
					// 对于学校相关的兴趣点，使用智能处理逻辑
					afe.addSchoolInterestPoint(fields, value)
				} else {
					// 其他兴趣点直接添加
					afe.addToInterestPoints(fields, value)
				}
			}
		}
	}
}

// extractFromNumbers 从数字提取字段
func (afe *AdvancedFieldExtractor) extractFromNumbers(fields map[string]interface{}, numbers []NumberEntity) {
	for _, number := range numbers {
		switch number.Type {
		case "price":
			if existingPrice, ok := fields["price"]; !ok {
				priceInfo := map[string]interface{}{
					"value": number.Value,
					"unit":  number.Unit,
				}
				fields["price"] = priceInfo
			} else if priceMap, ok := existingPrice.(map[string]interface{}); ok {
				// 如果已有价格信息，且没有operator字段，可能是区间
				if _, hasOperator := priceMap["operator"]; !hasOperator {
					if _, hasMin := priceMap["min"]; !hasMin {
						if number.Value < 50 { // 假设小于50的是万为单位
							priceMap["min"] = number.Value
						}
					}
				}
			}

		case "area":
			if _, ok := fields["area"]; !ok {
				// 额外检查：避免小数值（如1,2,3）覆盖合理的面积值
				if number.Value >= 10 && number.Value <= 2000 {
					areaInfo := map[string]interface{}{
						"value": number.Value,
						"unit":  number.Unit,
					}
					fields["area"] = areaInfo
				}
			}

		case "floor":
			if _, ok := fields["floor"]; !ok {
				floorInfo := map[string]interface{}{
					"current": int(number.Value),
				}
				fields["floor"] = floorInfo
			}

		case "year":
			fields["build_year"] = int(number.Value)
		}
	}
}

// normalizeText 标准化文本以提高提取稳定性
// TODO: 统一数字标准化处理 - 此函数的逻辑应整合到nlpProcessor.NormalizeText中
// 避免在多个地方维护类似的数字转换逻辑
func (afe *AdvancedFieldExtractor) normalizeText(text string) string {

	normalizedText := text

	// 首先处理带单位的数字（如"1千平" -> "1000平"）
	numberWithUnitPattern := regexp.MustCompile(`([\d一二两三四五六七八九十壹贰叁肆伍陆柒捌玖拾]+)([十百千万拾佰仟萬])(平|平米|平方米)`)
	normalizedText = numberWithUnitPattern.ReplaceAllStringFunc(normalizedText, func(match string) string {
		submatches := numberWithUnitPattern.FindStringSubmatch(match)
		if len(submatches) > 2 {
			numberWithUnit := submatches[1] + submatches[2]
			convertedValue := afe.parseNumberWithUnit(numberWithUnit)
			if convertedValue > 0 {
				// 保留原始单位（平/平米/平方米）
				unitSuffix := ""
				if len(submatches) > 3 {
					unitSuffix = submatches[3]
				}
				result := fmt.Sprintf("%.0f%s", convertedValue, unitSuffix)
				afe.logger.WithFields(logrus.Fields{
					"original":       match,
					"converted":      result,
					"numberWithUnit": numberWithUnit,
					"value":          convertedValue,
				}).Debug("Converted number with unit in area")
				return result
			}
		}
		return match
	})

	// 处理地铁线路中文数字转换 - 统一使用 common 包支持 1-99 完整覆盖
	// 🔧 优化：用正则捕获中文数字+号线，调用 common.ConvertChineseToArabic 统一转换
    subwayLinePattern := regexp.MustCompile(`([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾]+)号线`)
    normalizedText = subwayLinePattern.ReplaceAllStringFunc(normalizedText, func(match string) string {
        // 更安全：通过分组提取中文数字部分，避免按字节切分多字节字符
        parts := subwayLinePattern.FindStringSubmatch(match)
        if len(parts) >= 2 {
            chineseNumber := parts[1]
            arabicNumber := common.ConvertChineseToArabic(chineseNumber)
            return arabicNumber + "号线"
        }
        return match
    })

	// 处理"地铁X号线"格式
	subwayWithPrefixPattern := regexp.MustCompile(`(地铁|轨交)([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾]+)号线`)
	normalizedText = subwayWithPrefixPattern.ReplaceAllStringFunc(normalizedText, func(match string) string {
		// 解析：前缀 + 中文数字 + 号线
		parts := subwayWithPrefixPattern.FindStringSubmatch(match)
		if len(parts) >= 3 {
			prefix := parts[1]           // "地铁" 或 "轨交"
			chineseNumber := parts[2]    // 中文数字
			arabicNumber := common.ConvertChineseToArabic(chineseNumber)
			return prefix + arabicNumber + "号线"
		}
		return match // 如果解析失败，返回原文
	})

	// 处理房型相关的中文数字转换 - 统一使用 common.ConvertChineseToArabic 支持 1-99 完整覆盖
	roomTypePattern := regexp.MustCompile(`([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾]+)(居室|室|厅|卫|房)`)
	normalizedText = roomTypePattern.ReplaceAllStringFunc(normalizedText, func(match string) string {
		submatches := roomTypePattern.FindStringSubmatch(match)
		if len(submatches) >= 3 {
			chineseNumber := submatches[1]  // 中文数字
			suffix := submatches[2]         // 居室|室|厅|卫|房
			arabicNumber := common.ConvertChineseToArabic(chineseNumber)
			return arabicNumber + suffix
		}
		return match // 如果解析失败，返回原文
	})

	return strings.TrimSpace(normalizedText)
}

// extractRoomLayoutRobust 强化的房型提取，确保稳定性
func (afe *AdvancedFieldExtractor) extractRoomLayoutRobust(text string) interface{} {
	// 多重检测策略确保稳定性

	// 策略1：原有正则表达式
	if roomMatches := afe.regexRules["room_layout"].FindStringSubmatch(text); len(roomMatches) > 0 {
		roomInfo := afe.parseRoomFromRegex(roomMatches)
		if roomInfo != nil {
			return roomInfo
		}
	}

	// 策略2：简化的房型模式匹配（后备方案）
	simplePatterns := []string{
		`(\d+)居室`, // "1居室"
		`([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾])居室`, // "一居室"、"壹居室"
		`(\d+)室(\d+)厅`, // "2室1厅"
		`([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾])室([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾])厅`, // "三室两厅"、"叁室贰厅"
		`(\d+)室`, // "3室" - 新增
		`([一二三四五六七八九十两壹贰叁肆伍陆柒捌玖拾])室`, // "三室"、"叁室" - 新增
	}

	for _, pattern := range simplePatterns {
		regex := regexp.MustCompile(pattern)
		if matches := regex.FindStringSubmatch(text); len(matches) > 0 {
			// 简化解析
			roomInfo := make(map[string]interface{})

			if len(matches) >= 2 && matches[1] != "" {
				// 转换中文数字
				bedrooms := afe.convertChineseToNumber(matches[1])
				if bedrooms > 0 {
					roomInfo["bedrooms"] = bedrooms
					roomInfo["living_rooms"] = 1 // 默认1厅
					roomInfo["description"] = fmt.Sprintf("%d室1厅", bedrooms)

					// 如果有厅的信息
					if len(matches) >= 3 && matches[2] != "" {
						livingRooms := afe.convertChineseToNumber(matches[2])
						if livingRooms > 0 {
							roomInfo["living_rooms"] = livingRooms
							roomInfo["description"] = fmt.Sprintf("%d室%d厅", bedrooms, livingRooms)
						}
					}

					return roomInfo
				}
			}
		}
	}

	return nil
}

// convertChineseToNumber 中文数字转换为阿拉伯数字
func (afe *AdvancedFieldExtractor) convertChineseToNumber(chinese string) int {
	chineseToArabic := map[string]int{
		"一": 1, "二": 2, "两": 2, "三": 3, "四": 4, "五": 5,
		"六": 6, "七": 7, "八": 8, "九": 9, "十": 10,
		// 大写数字
		"壹": 1, "贰": 2, "叁": 3, "肆": 4, "伍": 5,
		"陆": 6, "柒": 7, "捌": 8, "玖": 9, "拾": 10,
		// 阿拉伯数字字符串
		"1": 1, "2": 2, "3": 3, "4": 4, "5": 5,
		"6": 6, "7": 7, "8": 8, "9": 9,
	}

	if num, exists := chineseToArabic[chinese]; exists {
		return num
	}

	if num, err := strconv.Atoi(chinese); err == nil {
		return num
	}

	return 0
}

// parseNumberWithUnit 解析带中文单位的数字（如"1千平"→1000，"5万"→5）
//
// 当前支持的格式：
//   - 单个阿拉伯数字+单位：1千、5万、3百
//   - 单个中文数字+单位：五千、三万、九百
//   - 纯阿拉伯数字：123、456.78
//
// 已知限制（需要更复杂算法才能支持）：
//   - 不支持复合中文数字：十二、三十五、一万二千
//   - 不支持带"点"的小数：三点一四
//   - 不支持更大单位：亿、兆
//
// 性能优化：正则表达式在结构体初始化时预编译，避免重复编译开销
func (afe *AdvancedFieldExtractor) parseNumberWithUnit(text string) float64 {
	// 处理简单的阿拉伯数字
	if num, err := strconv.ParseFloat(text, 64); err == nil {
		return num
	}

	// 单位映射
	units := map[string]float64{
		"十": 10,
		"百": 100,
		"千": 1000,
		"万": 10000,
		"拾": 10,    // 大写
		"佰": 100,   // 大写
		"仟": 1000,  // 大写
		"萬": 10000, // 大写
	}

	// 使用预编译的正则表达式（性能优化：避免每次调用都重新编译）
	matches := afe.numberUnitRegex.FindStringSubmatch(text)

	if len(matches) > 1 {
		// 解析数字部分
		numberPart := matches[1]
		var baseNum float64

		// 尝试解析阿拉伯数字
		if num, err := strconv.ParseFloat(numberPart, 64); err == nil {
			baseNum = num
		} else {
			// 尝试解析中文数字
			baseNum = float64(afe.convertChineseToNumber(numberPart))
		}

		// 处理单位
		if len(matches) > 2 && matches[2] != "" {
			if multiplier, ok := units[matches[2]]; ok {
				return baseNum * multiplier
			}
		}

		return baseNum
	}

	return 0
}

// extractFromRegex 从正则表达式提取字段
func (afe *AdvancedFieldExtractor) extractFromRegex(fields map[string]interface{}, text string, originalText string) {
	// 标准化文本以提高提取稳定性
	// TODO: 统一数字标准化处理 - 将normalizeText逻辑整合到nlpProcessor中
	// 当前保持独立实现以确保"千平"等特殊单位的正确处理
	normalizedText := afe.normalizeText(originalText)

	// DEBUG: 检查normalizeText的效果
	if strings.Contains(originalText, "千平") {
		afe.logger.WithFields(logrus.Fields{
			"originalText":   originalText,
			"normalizedText": normalizedText,
		}).Debug("DEBUG: normalizeText processing '千平'")
	}
	// 价格提取 - 让regex优先覆盖entity提取，因为regex更精确地处理限定词
	// 使用 FindAllStringSubmatchIndex 收集所有候选及位置信息，再择优选择
	if allPriceMatchesWithIndex := afe.regexRules["price"].FindAllStringSubmatchIndex(text, -1); len(allPriceMatchesWithIndex) > 0 {
		priceInfo := afe.parseBestPriceFromMatchesWithIndex(text, allPriceMatchesWithIndex)
		if priceInfo != nil {
			// 如果regex提取到了operator（如<=, >=），则覆盖entity结果
			if existingPrice, hasPrice := fields["price"]; hasPrice {
				if _, hasOperator := priceInfo["operator"]; hasOperator {
					// regex有operator，优先使用regex结果
					fields["price"] = priceInfo
				} else if existingPriceMap, ok := existingPrice.(map[string]interface{}); ok {
					// regex没有operator但entity有，保留entity结果
					if _, existingHasOperator := existingPriceMap["operator"]; !existingHasOperator {
						// 两者都没有operator，使用regex结果（更详细）
						fields["price"] = priceInfo
					}
				}
			} else {
				// 没有现有价格，直接使用regex结果
				fields["price"] = priceInfo
			}
		}
	}

	// 租金提取
	if _, hasRentPrice := fields["rent_price"]; !hasRentPrice {
		if allRentMatchesWithIndex := afe.regexRules["rent_price"].FindAllStringSubmatchIndex(text, -1); len(allRentMatchesWithIndex) > 0 {
			// 使用择优策略选择最佳租金匹配
			rentInfo := afe.parseBestRentFromMatchesWithIndex(text, allRentMatchesWithIndex)
			if rentInfo != nil {
				fields["rent_price"] = rentInfo
			}
		}
	}

	// 面积提取 - 让regex优先覆盖entity提取，因为regex更精确地处理限定词
	// 使用normalizedText而不是originalText，以便处理"1千平"这样的表达
	// 使用 FindAllStringSubmatchIndex 收集所有候选及位置信息，再择优选择
	if allAreaMatchesWithIndex := afe.regexRules["area"].FindAllStringSubmatchIndex(normalizedText, -1); len(allAreaMatchesWithIndex) > 0 {
		// DEBUG: 添加调试输出
		afe.logger.WithFields(map[string]interface{}{
			"originalText":   originalText,
			"normalizedText": normalizedText,
			"matches":        len(allAreaMatchesWithIndex),
			"match_count":    len(allAreaMatchesWithIndex),
		}).Debug("Area regex matched")

		areaInfo := afe.parseBestAreaFromMatchesWithIndex(normalizedText, allAreaMatchesWithIndex)

		// DEBUG: 显示解析结果
		afe.logger.WithFields(map[string]interface{}{
			"parsed_area": areaInfo,
		}).Debug("Area regex parsed")

		if areaInfo != nil {
			// 如果regex提取到了operator（如<=, >=），则覆盖entity结果
			if existingArea, hasArea := fields["area"]; hasArea {
				// DEBUG: 显示现有area字段
				afe.logger.WithFields(map[string]interface{}{
					"existing_area": existingArea,
					"regex_area":    areaInfo,
				}).Debug("Area field exists, checking priority")

				if _, hasOperator := areaInfo["operator"]; hasOperator {
					// regex有operator，优先使用regex结果
					afe.logger.Debug("Regex has operator, using regex result")
					fields["area"] = areaInfo
				} else if existingAreaMap, ok := existingArea.(map[string]interface{}); ok {
					// regex没有operator但entity有，保留entity结果
					if _, existingHasOperator := existingAreaMap["operator"]; !existingHasOperator {
						// 两者都没有operator，使用regex结果（更详细）
						afe.logger.Debug("Neither has operator, using regex result (more detailed)")
						fields["area"] = areaInfo
					} else {
						afe.logger.Debug("Entity has operator, keeping entity result")
					}
				}
			} else {
				// 没有现有面积，直接使用regex结果
				afe.logger.Debug("No existing area, using regex result")
				fields["area"] = areaInfo
			}
		} else {
			afe.logger.Debug("Area regex matched but parseAreaFromRegex returned nil")
		}
	}

	// 房型提取 - 强化检测，确保稳定性
	if roomInfo := afe.extractRoomLayoutRobust(normalizedText); roomInfo != nil {
		fields["room_layout"] = roomInfo
	}

	// 楼层提取
	if floorMatches := afe.regexRules["floor"].FindStringSubmatch(text); len(floorMatches) > 0 {
		if _, ok := fields["floor"]; !ok {
			floorInfo := afe.parseFloorFromRegex(floorMatches)
			if floorInfo != nil {
				fields["floor"] = floorInfo
			}
		}
	}

	// 地铁提取
	if subwayMatches := afe.regexRules["subway"].FindStringSubmatch(text); len(subwayMatches) > 0 {
		subwayInfo := afe.parseSubwayFromRegex(subwayMatches)
		if subwayInfo != nil {
			fields["subway"] = subwayInfo
		}
	}

	// 地点模式提取 - 识别"科学城"等地标
	// 先尝试匹配所有地点模式
	var landmarkFound string
	var isContextMatch bool

	// 首先检查是否包含上海区名（如"浦东新区"）
	shanghaiDistrictPatterns := []string{
		"浦东新区", "黄浦区", "徐汇区", "长宁区", "静安区", "普陀区", "虹口区", "杨浦区",
		"闵行区", "宝山区", "嘉定区", "金山区", "松江区", "青浦区", "奉贤区", "崇明区",
	}

	foundShanghaiDistrict := false
	for _, pattern := range shanghaiDistrictPatterns {
		if strings.Contains(text, pattern) {
			// 找到上海区名，不作为地标处理（会在后面作为district处理）
			foundShanghaiDistrict = true
			break
		}
	}

	// 如果不是上海区名，才尝试地点模式规则
	if !foundShanghaiDistrict {
		if locationMatches := afe.regexRules["location_pattern"].FindAllStringSubmatch(text, -1); len(locationMatches) > 0 {
			// 选择最短的匹配结果（避免过度匹配）
			landmarkFound = locationMatches[0][1]
			for _, match := range locationMatches {
				if len(match[1]) < len(landmarkFound) {
					landmarkFound = match[1]
				}
			}
			// 清理地标名称，去除不相关的前缀
			landmarkFound = afe.cleanLandmarkName(landmarkFound)
		}
	}

	// 如果没有找到，且不是上海区名，才尝试上下文规则
	if landmarkFound == "" && !foundShanghaiDistrict {
		if contextMatches := afe.regexRules["location_context"].FindStringSubmatch(text); len(contextMatches) > 0 {
			landmarkFound = contextMatches[1]
			isContextMatch = true
			// 清理地标名称
			landmarkFound = afe.cleanLandmarkName(landmarkFound)
		}
	}

	// 如果找到了地标，添加到fields中
	if landmarkFound != "" {
		// 检查是否是上海的区域名称（如"浦东新区"、"黄浦区"等）
		isShangHaiDistrict := false
		districtName := ""

		// 处理"浦东新区"这种特殊格式
		if landmarkFound == "浦东新区" {
			isShangHaiDistrict = true
			districtName = "浦东"
		} else {
			// 检查是否是标准的上海区名
			shanghaiDistricts := []string{
				"浦东", "黄浦", "徐汇", "长宁", "静安", "普陀", "虹口", "杨浦",
				"闵行", "宝山", "嘉定", "金山", "松江", "青浦", "奉贤", "崇明",
			}

			for _, district := range shanghaiDistricts {
				if strings.HasPrefix(landmarkFound, district) && strings.Contains(landmarkFound, "区") {
					isShangHaiDistrict = true
					districtName = district
					break
				}
			}
		}

		if isShangHaiDistrict {
			// 这是一个区域，应该放到district字段
			if _, hasLocation := fields["location"]; !hasLocation {
				locationInfo := make(map[string]interface{})
				locationInfo["province"] = "上海"
				locationInfo["district"] = districtName
				fields["location"] = locationInfo
			} else {
				// 如果已有location信息，补充district信息
				if locationMap, ok := fields["location"].(map[string]interface{}); ok {
					locationMap["province"] = "上海"
					locationMap["district"] = districtName
				}
			}
		} else {
			// 不是区域，作为普通地标处理
			if _, hasLocation := fields["location"]; !hasLocation {
				locationInfo := make(map[string]interface{})
				locationInfo["landmark"] = landmarkFound
				locationInfo["type"] = "地标"
				if isContextMatch {
					locationInfo["context"] = "上下文推断"
				}
				fields["location"] = locationInfo
			} else {
				// 如果已有location信息，尝试补充地标信息
				if locationMap, ok := fields["location"].(map[string]interface{}); ok {
					if _, hasLandmark := locationMap["landmark"]; !hasLandmark {
						locationMap["landmark"] = landmarkFound
						if isContextMatch {
							locationMap["context"] = "上下文推断"
						}
					}
				}
			}
		}
	}
}

// extractFromDictionary 从词典匹配提取字段
func (afe *AdvancedFieldExtractor) extractFromDictionary(fields map[string]interface{}, text string) {
	// 地理位置匹配
	if _, ok := fields["location"]; !ok {
		locationMatches := afe.dictionary.MatchKeywords(text, "location")
		if len(locationMatches) > 0 {
			locationInfo := make(map[string]interface{})
			for _, match := range locationMatches {
				// 标准化"浦东新区" → "浦东"
				if match == "浦东新区" {
					match = "浦东"
				}

				// 先检查是否为上海板块
				if isPlate, district := IsShanghaiPlate(match); isPlate {
					locationInfo["province"] = "上海"
					locationInfo["city"] = "上海"
					locationInfo["district"] = district
					locationInfo["plate"] = match
				} else if afe.isProvince(match) {
					// 省份处理
					locationInfo["province"] = match
					locationInfo["city"] = match
				} else {
					// 区级处理
					locationInfo["district"] = match
					// 查找对应的省份
					if locationInfo["province"] == nil {
						province := afe.findProvinceForDistrict(match)
						if province != "" {
							locationInfo["province"] = province
							locationInfo["city"] = province
						}
					}
				}
			}
			if len(locationInfo) > 0 {
				fields["location"] = locationInfo
			}
		}
	}

	// 房屋类型匹配
	if _, ok := fields["property_type"]; !ok {
		typeMatches := afe.dictionary.MatchKeywords(text, "property_type")
		if len(typeMatches) > 0 {
			// 对于别墅类型，尝试获取更具体的类型
			if typeMatches[0] == "别墅" {
				// 检查是否有更具体的别墅类型描述
				villaTypes := []string{"独栋别墅", "联排别墅", "叠拼别墅", "双拼别墅"}
				for _, villaType := range villaTypes {
					if strings.Contains(text, strings.Replace(villaType, "别墅", "", 1)) {
						fields["property_type"] = villaType
						break
					}
				}
				if _, ok := fields["property_type"]; !ok {
					fields["property_type"] = typeMatches[0]
				}
			} else {
				fields["property_type"] = typeMatches[0]
			}
		}
	}

	// 装修状况匹配
	if _, ok := fields["decoration"]; !ok {
		decorationMatches := afe.dictionary.MatchKeywords(text, "decoration")
		if len(decorationMatches) > 0 {
			fields["decoration"] = decorationMatches[0]
		}
	}

	// 朝向匹配
	if _, ok := fields["orientation"]; !ok {
		orientationMatches := afe.dictionary.MatchKeywords(text, "orientation")
		if len(orientationMatches) > 0 {
			fields["orientation"] = orientationMatches[0]
		}
	}

	// 兴趣点匹配
	interestMatches := afe.dictionary.MatchKeywords(text, "interest_point")
	if len(interestMatches) > 0 {
		fields["interest_points"] = interestMatches
	}
}

// postProcessFields 后处理字段
func (afe *AdvancedFieldExtractor) postProcessFields(fields map[string]interface{}, text string, originalText string) {
	// 房型标准化
	if roomLayout, ok := fields["room_layout"].(map[string]interface{}); ok {
		// 添加房型描述
		if bedrooms, ok := roomLayout["bedrooms"].(int); ok {
			if livingRooms, ok := roomLayout["living_rooms"].(int); ok {
				description := fmt.Sprintf("%d室%d厅", bedrooms, livingRooms)
				if bathrooms, ok := roomLayout["bathrooms"].(int); ok && bathrooms > 0 {
					description += fmt.Sprintf("%d卫", bathrooms)
				}
				roomLayout["description"] = description
			}
		}
	}

	// 价格标准化
	if priceInfo, ok := fields["price"].(map[string]interface{}); ok {
		// 确保单位一致
		if unit, ok := priceInfo["unit"].(string); !ok || unit == "" {
			priceInfo["unit"] = "万" // 默认单位
		}
	}

	// 面积标准化
	if areaInfo, ok := fields["area"].(map[string]interface{}); ok {
		// 确保单位一致
		if unit, ok := areaInfo["unit"].(string); !ok || unit == "" {
			areaInfo["unit"] = "平米" // 默认单位
		}
	}

	// 修复住宅租赁场景下的commercial字段错误识别问题
	afe.fixCommercialFieldInResidentialRental(fields, text)

	// 增强 interest_points 字段 - 从 commercial 字段转换相关内容
	afe.enhanceInterestPointsFromCommercial(fields, text, originalText)

	// 检测交通相关字段
	afe.detectTransportField(fields, text)

	// 修复错误的地理位置识别
	afe.fixIncorrectLocationDetection(fields, text)

	// 精细化别墅类型识别
	afe.refineVillaType(fields, text)

	// 处理学区房与学校好的冲突
	afe.resolveSchoolInterestPointConflicts(fields, text)
}

// fixCommercialFieldInResidentialRental 修复住宅租赁场景下commercial字段的错误识别
func (afe *AdvancedFieldExtractor) fixCommercialFieldInResidentialRental(fields map[string]interface{}, text string) {
	// 检查是否存在commercial字段
	commercial, hasCommercial := fields["commercial"]
	if !hasCommercial {
		return
	}

	// 检查是否存在rent_price字段（强烈表示租赁意图）
	_, hasRentPrice := fields["rent_price"]

	// 检查文本中的住宅租赁关键词
	residentialRentalKeywords := []string{
		"想租", "租个", "月租", "租房", "一居室", "二居室", "三居室",
		"居室", "室厅", "拎包入住", "交通便利", "住宅",
	}

	isResidentialRental := false
	lowercaseText := strings.ToLower(text)
	for _, keyword := range residentialRentalKeywords {
		if strings.Contains(lowercaseText, strings.ToLower(keyword)) {
			isResidentialRental = true
			break
		}
	}

	// 检查commercial字段的值是否为住宅租赁误识别的类型
	commercialValue, ok := commercial.(string)
	if !ok {
		return
	}

	// 这些commercial值通常是住宅租赁被误识别的结果
	mistakenCommercialValues := []string{
		"租金收益", "商业配套",
	}

	isMistakenValue := false
	for _, mistakenValue := range mistakenCommercialValues {
		if commercialValue == mistakenValue {
			isMistakenValue = true
			break
		}
	}

	// 如果满足以下条件，移除commercial字段：
	// 1. 存在rent_price字段，或者
	// 2. 文本中包含住宅租赁关键词，且commercial值是误识别的类型
	if hasRentPrice || (isResidentialRental && isMistakenValue) {
		delete(fields, "commercial")
	}
}

// 辅助方法
// convertToWan 将价格转换为万单位（健壮版本）
// 直接根据单位字符串转换，消除字符串查找的脆弱性
func (afe *AdvancedFieldExtractor) convertToWan(value float64, unit string) float64 {
	if unit == "亿" {
		return value * 10000 // 1亿 = 10000万
	}
	return value // 万或空字符串都按万处理
}

func (afe *AdvancedFieldExtractor) parsePriceFromRegex(matches []string) map[string]interface{} {
	result := make(map[string]interface{})

	// 新正则表达式的捕获组结构：
	// 组1,2,3: (\d+(?:\.\d+)?)\s*[-到至~]\s*(\d+(?:\.\d+)?)(万|亿) - 价格区间 "300-500万" 或 "1-2亿"
	// 组4,5,6,7: (\d+(?:\.\d+)?)(万|亿)\s*[-到至~]\s*(\d+(?:\.\d+)?)(万|亿) - 混合区间 "5000万-1亿"
	// 组8: (\d+(?:\.\d+)?)万(?:以下|以内|之内|内) - "xxx万以下"
	// 其他组: 各种预算和单价模式

	// 检查价格区间模式 (组1,2,3): "300-500万" 或 "1-2亿"
	if len(matches) > 3 && matches[1] != "" && matches[2] != "" && matches[3] != "" {
		if min, err := strconv.ParseFloat(matches[1], 64); err == nil {
			result["min"] = afe.convertToWan(min, matches[3]) // matches[3]是单位
		}
		if max, err := strconv.ParseFloat(matches[2], 64); err == nil {
			result["max"] = afe.convertToWan(max, matches[3]) // matches[3]是单位
		}
		result["unit"] = "万"
		return result
	}

	// 检查混合区间模式 (组4,5,6,7): "5000万-1亿"
	if len(matches) > 7 && matches[4] != "" && matches[5] != "" && matches[6] != "" && matches[7] != "" {
		if min, err := strconv.ParseFloat(matches[4], 64); err == nil {
			result["min"] = afe.convertToWan(min, matches[5]) // matches[5]是最小值的单位
		}
		if max, err := strconv.ParseFloat(matches[6], 64); err == nil {
			result["max"] = afe.convertToWan(max, matches[7]) // matches[7]是最大值的单位
		}
		result["unit"] = "万"
		return result
	}

	// 检查"xxx万以下"模式 (组8)
	if len(matches) > 8 && matches[8] != "" {
		if price, err := strconv.ParseFloat(matches[8], 64); err == nil {
			result["max"] = price // 这里已经是万单位，不需要转换
			result["operator"] = "<="
		}
		result["unit"] = "万"
		return result
	}

	// 检查"不超过xxx万"模式 (组9)
	if len(matches) > 9 && matches[9] != "" {
		if price, err := strconv.ParseFloat(matches[9], 64); err == nil {
			result["max"] = price
			result["operator"] = "<="
		}
		result["unit"] = "万"
		return result
	}

	// 检查"xxx万以上"模式 (组10)
	if len(matches) > 10 && matches[10] != "" {
		if price, err := strconv.ParseFloat(matches[10], 64); err == nil {
			result["min"] = price
			result["operator"] = ">="
		}
		result["unit"] = "万"
		return result
	}

	// 检查预算区间格式 "预算300-500万" (组11,12)
	if len(matches) > 12 && matches[11] != "" && matches[12] != "" {
		if min, err := strconv.ParseFloat(matches[11], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(matches[12], 64); err == nil {
			result["max"] = max
		}
		result["unit"] = "万"
		return result
	}

	// 检查其他预算相关模式 (组13-17)
	for i := 13; i <= 17 && i < len(matches); i++ {
		if matches[i] != "" {
			if price, err := strconv.ParseFloat(matches[i], 64); err == nil {
				// 根据不同的组确定操作符
				if i == 13 { // "预算xxx万以内"
					result["max"] = price
					result["operator"] = "<="
				} else if i == 14 { // "预算不超过xxx万"
					result["max"] = price
					result["operator"] = "<="
				} else if i == 15 { // "预算xxx万左右"
					result["value"] = price
					result["operator"] = "about"
				} else if i == 16 || i == 17 { // "xxx万左右", "预算xxx万元", "预算xxx万", "xxx万元"等
					if strings.Contains(matches[0], "左右") {
						result["value"] = price
						result["operator"] = "about"
					} else {
						result["value"] = price
					}
				} else {
					result["value"] = price
				}
				result["unit"] = "万"
				return result
			}
		}
	}

	// 后备：查找任何非空数字匹配组
	for i := 1; i < len(matches); i++ {
		if matches[i] != "" {
			if price, err := strconv.ParseFloat(matches[i], 64); err == nil {
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

// parseRentPriceFromRegex 解析租金正则匹配结果
func (afe *AdvancedFieldExtractor) parseRentPriceFromRegex(matches []string) map[string]interface{} {
	result := make(map[string]interface{})

	// 分析匹配组来确定租金类型
	// 正则: 月租金(\d+)[-到至~](\d+)|月租(\d+)[-到至~](\d+)|租金(\d+)[-到至~](\d+)|月租金(\d+)|月租(\d+)|租金(\d+)
	if len(matches) > 2 && matches[1] != "" && matches[2] != "" {
		// 组1,2: "月租金3000-5000"
		if min, err := strconv.ParseFloat(matches[1], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(matches[2], 64); err == nil {
			result["max"] = max
		}
		result["unit"] = "元"
	} else if len(matches) > 4 && matches[3] != "" && matches[4] != "" {
		// 组3,4: "月租3000-5000"
		if min, err := strconv.ParseFloat(matches[3], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(matches[4], 64); err == nil {
			result["max"] = max
		}
		result["unit"] = "元"
	} else if len(matches) > 6 && matches[5] != "" && matches[6] != "" {
		// 组5,6: "租金3000-5000"
		if min, err := strconv.ParseFloat(matches[5], 64); err == nil {
			result["min"] = min
		}
		if max, err := strconv.ParseFloat(matches[6], 64); err == nil {
			result["max"] = max
		}
		result["unit"] = "元"
	} else if len(matches) > 7 && matches[7] != "" {
		// 组7: "月租金3000"
		if price, err := strconv.ParseFloat(matches[7], 64); err == nil {
			result["value"] = price
		}
		result["unit"] = "元"
	} else if len(matches) > 8 && matches[8] != "" {
		// 组8: "月租3000"
		if price, err := strconv.ParseFloat(matches[8], 64); err == nil {
			result["value"] = price
		}
		result["unit"] = "元"
	} else if len(matches) > 9 && matches[9] != "" {
		// 组9: "租金3000"
		if price, err := strconv.ParseFloat(matches[9], 64); err == nil {
			result["value"] = price
		}
		result["unit"] = "元"
	} else {
		// 后备：查找任何非空匹配组
		for i := 1; i < len(matches); i++ {
			if matches[i] != "" {
				if price, err := strconv.ParseFloat(matches[i], 64); err == nil {
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

func (afe *AdvancedFieldExtractor) parseAreaFromRegex(matches []string) map[string]interface{} {
	result := make(map[string]interface{})

	// 分析匹配组来确定面积类型和操作符
	// 新的组织结构：面积开头的模式在前，不带面积前缀的在后
	if len(matches) > 2 && matches[1] != "" && matches[2] != "" {
		// 组1,2: "面积80-120平"
		min := afe.parseNumberWithUnit(matches[1])
		if min > 0 {
			result["min"] = min
		}
		max := afe.parseNumberWithUnit(matches[2])
		if max > 0 {
			result["max"] = max
		}
		result["unit"] = "平米"
	} else if len(matches) > 3 && matches[3] != "" {
		// 组3: "面积xxx平以内|以下|之内"
		area := afe.parseNumberWithUnit(matches[3])
		if area > 0 {
			result["max"] = area
			result["operator"] = "<="
		}
		result["unit"] = "平米"
	} else if len(matches) > 4 && matches[4] != "" {
		// 组4: "面积不超过xxx平|面积最多xxx平"
		area := afe.parseNumberWithUnit(matches[4])
		if area > 0 {
			result["max"] = area
			result["operator"] = "<="
		}
		result["unit"] = "平米"
	} else if len(matches) > 5 && matches[5] != "" {
		// 组5: "面积xxx平左右"
		area := afe.parseNumberWithUnit(matches[5])
		if area > 0 {
			result["value"] = area
			result["operator"] = "about"
		}
		result["unit"] = "平米"
	} else if len(matches) > 6 && matches[6] != "" {
		// 组6: "面积xxx平"（通用模式）
		area := afe.parseNumberWithUnit(matches[6])
		if area > 0 {
			result["value"] = area
		}
		result["unit"] = "平米"
	} else if len(matches) > 8 && matches[7] != "" && matches[8] != "" {
		// 组7,8: "80-120平"
		min := afe.parseNumberWithUnit(matches[7])
		if min > 0 {
			result["min"] = min
		}
		max := afe.parseNumberWithUnit(matches[8])
		if max > 0 {
			result["max"] = max
		}
		result["unit"] = "平米"
	} else if len(matches) > 9 && matches[9] != "" {
		// 组9: "xxx平以内|以下|之内"
		area := afe.parseNumberWithUnit(matches[9])
		if area > 0 {
			result["max"] = area
			result["operator"] = "<="
		}
		result["unit"] = "平米"
	} else if len(matches) > 10 && matches[10] != "" {
		// 组10: "不超过xxx平|最多xxx平"
		area := afe.parseNumberWithUnit(matches[10])
		if area > 0 {
			result["max"] = area
			result["operator"] = "<="
		}
		result["unit"] = "平米"
	} else if len(matches) > 11 && matches[11] != "" {
		// 组11: "xxx平以上"
		area := afe.parseNumberWithUnit(matches[11])
		if area > 0 {
			result["min"] = area
			result["operator"] = ">="
		}
		result["unit"] = "平米"
	} else if len(matches) > 12 && matches[12] != "" {
		// 组12: "xxx平左右"
		area := afe.parseNumberWithUnit(matches[12])
		if area > 0 {
			result["value"] = area
			result["operator"] = "about"
		}
		result["unit"] = "平米"
	} else if len(matches) > 13 && matches[13] != "" {
		// 组13: "xxx平方米"（通用模式）
		area := afe.parseNumberWithUnit(matches[13])
		if area > 0 {
			result["value"] = area
		}
		result["unit"] = "平米"
	} else if len(matches) > 14 && matches[14] != "" {
		// 组14: "xxx㎡"
		area := afe.parseNumberWithUnit(matches[14])
		if area > 0 {
			result["value"] = area
		}
		result["unit"] = "平米"
	} else {
		// 后备：查找任何非空匹配组
		for i := 1; i < len(matches); i++ {
			if matches[i] != "" {
				area := afe.parseNumberWithUnit(matches[i])
				if area > 0 {
					result["value"] = area
					result["unit"] = "平米"
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

func (afe *AdvancedFieldExtractor) parseRoomFromRegex(matches []string) map[string]interface{} {
	result := make(map[string]interface{})

	// 辅助函数：将中文数字转换为阿拉伯数字
	convertChineseNumber := func(chinese string) int {
		chineseToArabic := map[string]int{
			"一": 1, "二": 2, "两": 2, "三": 3, "四": 4, "五": 5,
			"六": 6, "七": 7, "八": 8, "九": 9, "十": 10,
			"1": 1, "2": 2, "3": 3, "4": 4, "5": 5,
			"6": 6, "7": 7, "8": 8, "9": 9,
		}
		if num, exists := chineseToArabic[chinese]; exists {
			return num
		}
		if num, err := strconv.Atoi(chinese); err == nil {
			return num
		}
		return 0
	}

	// 新的正则表达式分组：
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
	for i := 1; i < len(matches); i++ {
		if matches[i] != "" {
			switch i {
			case 1, 4, 9, 12: // 室/房的数量
				if num, err := strconv.Atoi(matches[i]); err == nil {
					result["bedrooms"] = num
				}
			case 2, 5, 10, 13: // 厅的数量
				if num, err := strconv.Atoi(matches[i]); err == nil {
					result["living_rooms"] = num
				}
			case 3, 11: // 卫的数量
				if num, err := strconv.Atoi(matches[i]); err == nil {
					result["bathrooms"] = num
				}
			case 6: // 中文数字居室的情况 - 特殊处理"一居室"等
				bedrooms := convertChineseNumber(matches[i])
				if bedrooms > 0 {
					result["bedrooms"] = bedrooms
					result["living_rooms"] = 1
					result["description"] = fmt.Sprintf("%d室1厅", bedrooms)
				}
			case 7: // 中文数字室
				bedrooms := convertChineseNumber(matches[i])
				if bedrooms > 0 {
					result["bedrooms"] = bedrooms
				}
			case 8: // 中文数字厅
				livingRooms := convertChineseNumber(matches[i])
				if livingRooms > 0 {
					result["living_rooms"] = livingRooms
				}
			case 14: // 阿拉伯数字居室 - 处理"1居室"等
				if num, err := strconv.Atoi(matches[i]); err == nil {
					result["bedrooms"] = num
					result["living_rooms"] = 1
					result["description"] = fmt.Sprintf("%d室1厅", num)
				}
			case 15: // 房间数量
				if num, err := strconv.Atoi(matches[i]); err == nil {
					result["bedrooms"] = num
					result["living_rooms"] = 1
					result["description"] = fmt.Sprintf("%d室1厅", num)
				}
			case 16: // 单独的阿拉伯数字"N室"
				if num, err := strconv.Atoi(matches[i]); err == nil {
					result["bedrooms"] = num
					result["living_rooms"] = 1
					result["description"] = fmt.Sprintf("%d室1厅", num)
				}
			case 17: // 单独的中文数字"N室"（包括大写）
				bedrooms := convertChineseNumber(matches[i])
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

	// 生成描述字符串（如果还没有）
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

func (afe *AdvancedFieldExtractor) parseFloorFromRegex(matches []string) map[string]interface{} {
	result := make(map[string]interface{})

	for i := 1; i < len(matches); i++ {
		if matches[i] != "" {
			if floor, err := strconv.Atoi(matches[i]); err == nil {
				if i == 1 && len(matches) > 2 && matches[2] != "" {
					// 可能是 "8/30层" 格式
					if total, err := strconv.Atoi(matches[2]); err == nil {
						result["current"] = floor
						result["total"] = total
						break
					}
				} else {
					result["current"] = floor
					break
				}
			}
		}
	}

	// 处理楼层描述
	if strings.Contains(strings.Join(matches, " "), "高层") {
		result["type"] = "高层"
	} else if strings.Contains(strings.Join(matches, " "), "中层") {
		result["type"] = "中层"
	} else if strings.Contains(strings.Join(matches, " "), "低层") {
		result["type"] = "低层"
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func (afe *AdvancedFieldExtractor) parseSubwayFromRegex(matches []string) interface{} {
	// 解析地铁信息为结构化数据
	subwayInfo := make(map[string]interface{})

	// 获取完整匹配文本
	fullMatch := matches[0]

	// 提取线路信息
	linePattern := regexp.MustCompile(`(\d+|[一二三四五六七八九十]+)号线`)
	if lineMatch := linePattern.FindStringSubmatch(fullMatch); len(lineMatch) > 0 {
		// 中文数字转换
		line := lineMatch[1]
		line = common.ConvertChineseToArabic(line)
		subwayInfo["line"] = line + "号线"
	}

	// 提取站点信息
	// 更精确的站点提取模式：在"号线"后面找站名
	stationPattern := regexp.MustCompile(`号线([^站]*站)`)
	if stationMatch := stationPattern.FindStringSubmatch(fullMatch); len(stationMatch) > 0 {
		station := stationMatch[1]
		// 清理站点名称
		station = strings.TrimSpace(station)
		if station != "" && station != "站" {
			subwayInfo["station"] = station
		}
	} else {
		// 备用模式：寻找任何以"站"结尾的词
		altStationPattern := regexp.MustCompile(`([^地铁轨交\s]+站)`)
		if altMatch := altStationPattern.FindStringSubmatch(fullMatch); len(altMatch) > 0 {
			station := altMatch[1]
			station = strings.TrimSpace(station)
			if station != "" && station != "站" && !strings.Contains(station, "号线") {
				subwayInfo["station"] = station
			}
		}
	}

	// 检查是否是"近地铁"这类泛指
	if strings.Contains(fullMatch, "近地铁") || strings.Contains(fullMatch, "靠近地铁") {
		subwayInfo["nearby"] = true
	}

	// 如果有任何信息，返回结构化数据
	if len(subwayInfo) > 0 {
		return subwayInfo
	}

	// 否则返回原始匹配文本（向后兼容）
	for i := 1; i < len(matches); i++ {
		if matches[i] != "" {
			return matches[i]
		}
	}
	return nil
}


func (afe *AdvancedFieldExtractor) isProvince(location string) bool {
	provinces := []string{"上海", "北京", "深圳", "广州", "杭州", "南京", "苏州", "天津", "重庆"}
	for _, province := range provinces {
		if location == province {
			return true
		}
	}
	return false
}

func (afe *AdvancedFieldExtractor) findProvinceForDistrict(district string) string {
	locationDict := afe.dictionary.GetCategoryKeywords("location")
	for province, districts := range locationDict {
		for _, d := range districts {
			if d == district {
				return province
			}
		}
	}
	return ""
}

// cleanLandmarkName 清理地标名称，去除不相关的前缀
func (afe *AdvancedFieldExtractor) cleanLandmarkName(landmark string) string {
	// 定义需要清理的无关前缀词
	prefixes := []string{
		"想在", "想去", "想到", "去", "到", "在",
		"来", "靠近", "附近", "临近", "离", "距离",
		"平米左右", "平米", "平方", "左右",
		"平", "米", "的", "个",
	}

	// 逐个尝试去除前缀
	cleaned := landmark
	for _, prefix := range prefixes {
		if strings.HasPrefix(cleaned, prefix) {
			cleaned = strings.TrimPrefix(cleaned, prefix)
		}
	}

	// 如果清理后为空，返回原始值
	if cleaned == "" {
		return landmark
	}

	// 确保结果仍然包含地标关键词
	landmarkKeywords := []string{
		"科学城", "科技城", "产业园", "工业园", "开发区",
		"高新区", "保税区", "自贸区", "新区", "新城",
		"园区", "中心", "广场", "大厦", "大楼",
		"商圈", "商业中心", "CBD", "金融城", "创新城",
		"智慧城", "生态城", "国际城",
	}

	// 验证清理后的结果是否仍包含地标关键词
	hasLandmark := false
	for _, keyword := range landmarkKeywords {
		if strings.Contains(cleaned, keyword) {
			hasLandmark = true
			break
		}
	}

	// 如果清理后不包含地标关键词，返回原始值
	if !hasLandmark {
		return landmark
	}

	return cleaned
}

// calculateConfidence 计算置信度
func (afe *AdvancedFieldExtractor) calculateConfidence(fields map[string]interface{}, entities []Entity, numbers []NumberEntity) map[string]float64 {
	confidence := make(map[string]float64)

	for fieldName := range fields {
		// 基于多种因素计算置信度
		conf := 0.5 // 基础置信度

		// 实体匹配加权
		for _, entity := range entities {
			if afe.isFieldRelatedToEntity(fieldName, entity.Type) {
				conf += entity.Confidence * 0.3
			}
		}

		// 数字匹配加权
		for _, number := range numbers {
			if afe.isFieldRelatedToNumber(fieldName, number.Type) {
				conf += number.Confidence * 0.2
			}
		}

		// 确保置信度在合理范围内
		if conf > 1.0 {
			conf = 1.0
		}
		if conf < 0.1 {
			conf = 0.1
		}

		confidence[fieldName] = conf
	}

	return confidence
}

func (afe *AdvancedFieldExtractor) isFieldRelatedToEntity(fieldName, entityType string) bool {
	relations := map[string][]string{
		"location":      {"LOCATION", "PROVINCE", "DISTRICT"},
		"property_type": {"PROPERTY_TYPE"},
		"decoration":    {"DECORATION"},
		"orientation":   {"ORIENTATION"},
		"price":         {"PRICE_RANGE", "SINGLE_PRICE"},
		"area":          {"AREA_RANGE"},
		"room_layout":   {"ROOM_LAYOUT"},
		"floor":         {"FLOOR_INFO"},
		"subway":        {"SUBWAY_INFO"},
	}

	if types, ok := relations[fieldName]; ok {
		for _, t := range types {
			if t == entityType {
				return true
			}
		}
	}
	return false
}

func (afe *AdvancedFieldExtractor) isFieldRelatedToNumber(fieldName, numberType string) bool {
	relations := map[string][]string{
		"price":       {"price"},
		"area":        {"area"},
		"floor":       {"floor"},
		"room_layout": {"room"},
		"build_year":  {"year"},
	}

	if types, ok := relations[fieldName]; ok {
		for _, t := range types {
			if t == numberType {
				return true
			}
		}
	}
	return false
}

// updateMetrics 更新提取指标
func (afe *AdvancedFieldExtractor) updateMetrics(result *ExtractionResult) {
	afe.metricsMu.Lock()
	defer afe.metricsMu.Unlock()

	afe.metrics.TotalExtractions++

	if len(result.Fields) > 0 {
		afe.metrics.SuccessfulExtractions++
	}

	// 更新平均处理时间
	processingTime := float64(result.ProcessTime.Milliseconds())
	currentAvg := afe.metrics.AvgProcessingTime
	totalExtractions := float64(afe.metrics.TotalExtractions)
	afe.metrics.AvgProcessingTime = (currentAvg*(totalExtractions-1) + processingTime) / totalExtractions

	// 更新平均置信度
	if len(result.Confidence) > 0 {
		totalConf := 0.0
		for _, conf := range result.Confidence {
			totalConf += conf
		}
		avgConf := totalConf / float64(len(result.Confidence))
		currentAvgConf := afe.metrics.AvgConfidence
		afe.metrics.AvgConfidence = (currentAvgConf*(totalExtractions-1) + avgConf) / totalExtractions
	}

	// 更新字段提取率
	for fieldName, conf := range result.Confidence {
		if conf >= afe.confidenceThreshold {
			if rate, ok := afe.metrics.FieldExtractionRate[fieldName]; ok {
				afe.metrics.FieldExtractionRate[fieldName] = (rate + 1) / 2
			} else {
				afe.metrics.FieldExtractionRate[fieldName] = 1.0
			}
		}
	}

	afe.metrics.LastUpdated = time.Now()
}

// 实现接口方法
func (afe *AdvancedFieldExtractor) ExtractKeywords(text string) (map[string][]string, error) {
	result, err := afe.ExtractFromMessage(text)
	if err != nil {
		return nil, err
	}

	keywords := make(map[string][]string)
	for fieldName := range result.Fields {
		keywords[fieldName] = []string{fmt.Sprintf("%v", result.Fields[fieldName])}
	}

	return keywords, nil
}

// ExtractKeywordsWithContext 从文本中提取关键词（带超时控制）
func (afe *AdvancedFieldExtractor) ExtractKeywordsWithContext(ctx context.Context, text string) (map[string][]string, error) {
	result, err := afe.ExtractFromMessageWithContext(ctx, text)
	if err != nil {
		return nil, err
	}

	keywords := make(map[string][]string)
	for fieldName := range result.Fields {
		keywords[fieldName] = []string{fmt.Sprintf("%v", result.Fields[fieldName])}
	}

	return keywords, nil
}

func (afe *AdvancedFieldExtractor) MatchFieldValues(keywords map[string][]string, fieldCategory string) map[string]interface{} {
	result := make(map[string]interface{})

	if values, ok := keywords[fieldCategory]; ok && len(values) > 0 {
		result[fieldCategory] = values[0]
	}

	return result
}

func (afe *AdvancedFieldExtractor) GetSuggestedQuestions(missingFields []string) []string {
	questions := make([]string, 0, len(missingFields))

	questionTemplates := map[string]string{
		"location":      "请问您希望在哪个城市和区域买房？",
		"price":         "请告诉我您的预算范围是多少？",
		"area":          "您对房屋面积有什么要求？",
		"room_layout":   "您希望要几室几厅的房子？",
		"property_type": "您是想买新房还是二手房？",
		"decoration":    "您对装修有什么要求？",
		"orientation":   "您对房屋朝向有偏好吗？",
		"floor":         "您对楼层有什么要求？",
		"subway":        "您希望距离地铁近一些吗？",
	}

	for _, field := range missingFields {
		if question, ok := questionTemplates[field]; ok {
			questions = append(questions, question)
		}
	}

	return questions
}

func (afe *AdvancedFieldExtractor) SetConfidenceThreshold(threshold float64) {
	if threshold >= 0 && threshold <= 1.0 {
		afe.confidenceThreshold = threshold
	}
}

// SetFilterStrategy 设置租售过滤策略
func (afe *AdvancedFieldExtractor) SetFilterStrategy(strategy FilterStrategy) {
	afe.rentalFilter = strategy
}

func (afe *AdvancedFieldExtractor) GetExtractionMetrics() *ExtractionMetrics {
	afe.metricsMu.RLock()
	defer afe.metricsMu.RUnlock()

	// 返回metrics的副本以避免外部修改
	metricsCopy := &ExtractionMetrics{
		TotalExtractions:      afe.metrics.TotalExtractions,
		SuccessfulExtractions: afe.metrics.SuccessfulExtractions,
		AvgProcessingTime:     afe.metrics.AvgProcessingTime,
		AvgConfidence:         afe.metrics.AvgConfidence,
		FieldExtractionRate:   make(map[string]float64),
		ErrorCount:            afe.metrics.ErrorCount,
		LastUpdated:           afe.metrics.LastUpdated,
	}

	// 深度复制map
	for k, v := range afe.metrics.FieldExtractionRate {
		metricsCopy.FieldExtractionRate[k] = v
	}

	return metricsCopy
}

// isSameDecoration 判断两个装修类型是否相同（智能去重）
func (afe *AdvancedFieldExtractor) isSameDecoration(decoration1, decoration2 string) bool {
	// 装修类型的同义词映射
	synonymMap := map[string]string{
		"毛坯房":  "毛坯",
		"毛胚房":  "毛坯",
		"毛胚":   "毛坯",
		"简装修":  "简装",
		"简单装修": "简装",
		"精装修":  "精装",
		"精装房":  "精装",
		"豪华装修": "豪装",
	}

	// 标准化装修类型
	normalize := func(decoration string) string {
		if standard, ok := synonymMap[decoration]; ok {
			return standard
		}
		return decoration
	}

	return normalize(decoration1) == normalize(decoration2)
}

// enhanceInterestPointsFromCommercial 从 commercial 字段增强 interest_points
func (afe *AdvancedFieldExtractor) enhanceInterestPointsFromCommercial(fields map[string]interface{}, text string, originalText string) {
	// 获取现有的 interest_points
	var interestPoints []string
	if existing, ok := fields["interest_points"].([]string); ok {
		interestPoints = existing
	}

	// 1. 检查 CBD 商圈相关 - 不依赖 commercial 字段，直接分析文本
	if afe.detectCBDArea(text) {
		if !afe.containsInterestPoint(interestPoints, "CBD商圈") {
			interestPoints = append(interestPoints, "CBD商圈")
		}
	}

	// 2. 检查具体的收益率信息 - 优先提取具体数据
	specificReturn := afe.extractSpecificReturn(originalText)
	if specificReturn != "" {
		if !afe.containsInterestPoint(interestPoints, specificReturn) {
			interestPoints = append(interestPoints, specificReturn)
		}
	} else if afe.detectRentalReturn(text) {
		// 根据文本内容确定具体的租金回报率标签
		rentalLabel := afe.getRentalReturnLabel(text)
		if !afe.containsInterestPoint(interestPoints, rentalLabel) {
			interestPoints = append(interestPoints, rentalLabel)
		}
	} else if afe.detectInvestmentAdvantageStrict(text) {
		if !afe.containsInterestPoint(interestPoints, "投资优势") {
			interestPoints = append(interestPoints, "投资优势")
		}
	}

	// 3. 检查地段优势 - 使用更精确的标签
	locationLabel := afe.getLocationLabel(text, originalText)
	if locationLabel != "" {
		if !afe.containsInterestPoint(interestPoints, locationLabel) {
			interestPoints = append(interestPoints, locationLabel)
		}
	}

	// 4. 检查教育设施相关 - 特别是幼儿园
	if afe.detectKindergartenFacility(text) {
		if !afe.containsInterestPoint(interestPoints, "近幼儿园") {
			interestPoints = append(interestPoints, "近幼儿园")
		}
	}

	// 5. 检查写字楼相关
	if afe.detectOfficeBuilding(text) {
		if !afe.containsInterestPoint(interestPoints, "写字楼里") {
			interestPoints = append(interestPoints, "写字楼里")
		}
	}

	// 6. 检查综合体投资需求
	if afe.detectComprehensiveComplexInvestment(originalText) {
		if !afe.containsInterestPoint(interestPoints, "综合体") {
			interestPoints = append(interestPoints, "综合体")
		}
	}

	// 7. 检查人流量相关
	if afe.detectHighTraffic(text) {
		if !afe.containsInterestPoint(interestPoints, "人流量大") {
			interestPoints = append(interestPoints, "人流量大")
		}
	}

	// 7. 检查商业街相关
	if afe.detectCommercialStreet(text) {
		if !afe.containsInterestPoint(interestPoints, "商业街") {
			interestPoints = append(interestPoints, "商业街")
		}
	}

	// 8. 清理错误的匹配项
	var cleanedPoints []string
	for _, point := range interestPoints {
		if afe.isValidInterestPointWithOriginal(text, originalText, point) {
			cleanedPoints = append(cleanedPoints, point)
		}
	}

	// 更新 interest_points 字段
	if len(cleanedPoints) > 0 {
		fields["interest_points"] = cleanedPoints
	}
}

// containsInterestPoint 检查是否已包含某个兴趣点
func (afe *AdvancedFieldExtractor) containsInterestPoint(points []string, target string) bool {
	for _, point := range points {
		if point == target {
			return true
		}
	}
	return false
}

// isValidInterestPoint 验证兴趣点是否有效
func (afe *AdvancedFieldExtractor) isValidInterestPoint(text, point string) bool {
	switch point {
	case "近综合体":
		// 只有文本中确实包含"综合体"相关词汇时才认为有效
		return strings.Contains(text, "综合体") || strings.Contains(text, "商业中心") || strings.Contains(text, "购物中心")
	default:
		return true
	}
}

// containsRentalOrSellingKeywords 检测是否包含租售相关关键词
// Jenny式设计：通过谓语动词判断，而非简单关键词匹配
func (afe *AdvancedFieldExtractor) containsRentalOrSellingKeywords(text string) bool {
	// 提取句子的谓语动词
	predicate := afe.nlpProcessor.ExtractPredicate(text)

	// 如果没有找到谓语，默认不过滤
	if predicate == "" {
		return false
	}

	// 租售相关的谓语动词
	rentalSellingVerbs := map[string]bool{
		"出租": true, "租": true, "求租": true,
		"出售": true, "售": true, "卖": true, "售卖": true,
		"转让": true, "出让": true, "转手": true,
	}

	// 检查谓语是否是租售动词
	return rentalSellingVerbs[predicate]
}

// detectCBDArea 检测CBD商圈
func (afe *AdvancedFieldExtractor) detectCBDArea(text string) bool {
	lowerText := strings.ToLower(text)

	// CBD相关关键词 - 包含原始词汇和标准化后的词汇
	cbdKeywords := []string{
		"cbd", "CBD", "中央商务区", "核心商务区", "商务中心区",
		"商业中心", // 标准化后的CBD
	}
	hasCBD := false
	for _, keyword := range cbdKeywords {
		if strings.Contains(text, keyword) || strings.Contains(lowerText, strings.ToLower(keyword)) {
			hasCBD = true
			break
		}
	}

	// 商圈相关关键词 - 包含原始词汇和标准化后的词汇
	areaKeywords := []string{
		"商圈", "核心", "中心", "商务区", "金融区", "商业区",
		"商业地段", // 标准化后的商圈
	}
	hasArea := false
	for _, keyword := range areaKeywords {
		if strings.Contains(text, keyword) {
			hasArea = true
			break
		}
	}

	return hasCBD && hasArea
}

// detectRentalReturn 检测租金回报率相关
func (afe *AdvancedFieldExtractor) detectRentalReturn(text string) bool {
	// 租金回报相关关键词
	rentalKeywords := []string{
		"租金回报率", "租金回报", "租金收益", "出租回报",
	}

	// 检查是否包含租金回报相关词汇
	hasRental := false
	for _, keyword := range rentalKeywords {
		if strings.Contains(text, keyword) {
			hasRental = true
			break
		}
	}

	if !hasRental {
		return false
	}

	// 高回报相关关键词
	highReturnKeywords := []string{
		"高", "要高", "较高", "很高", "更高",
	}

	// 稳定性关键词
	stabilityKeywords := []string{
		"稳定", "稳健", "可靠", "保证",
	}

	// 检查是否有高回报或稳定性要求
	for _, keyword := range highReturnKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	for _, keyword := range stabilityKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	// 如果只提到租金回报率但没有具体要求，也认为是相关的
	return true
}

// getRentalReturnLabel 根据文本内容返回具体的租金回报率标签
func (afe *AdvancedFieldExtractor) getRentalReturnLabel(text string) string {
	// 高回报相关关键词
	highReturnKeywords := []string{
		"高", "要高", "较高", "很高", "更高",
	}

	// 稳定性关键词
	stabilityKeywords := []string{
		"稳定", "稳健", "可靠", "保证",
	}

	// 检查是否强调高回报
	for _, keyword := range highReturnKeywords {
		if strings.Contains(text, keyword) {
			return "租金回报率高"
		}
	}

	// 检查是否强调稳定性
	for _, keyword := range stabilityKeywords {
		if strings.Contains(text, keyword) {
			return "租金回报率稳定"
		}
	}

	// 默认返回通用标签
	return "租金回报率高"
}

// detectInvestmentAdvantage 检测投资优势
func (afe *AdvancedFieldExtractor) detectInvestmentAdvantage(text string) bool {
	// 明确的投资回报相关关键词
	returnKeywords := []string{
		"投资回报", "回报率", "收益率", "年化收益",
		"投资收益", "现金流", "净收益", "ROI",
	}

	// 有明确投资回报词汇就认为是投资优势
	for _, keyword := range returnKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	// 投资优势的明确表达词汇
	investmentAdvantageKeywords := []string{
		"投资价值", "投资优势", "投资性价比",
		"投资前景", "投资潜力", "升值空间",
	}

	for _, keyword := range investmentAdvantageKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	// 避免误判：单纯的"投资"+"保值增值"等词汇不算投资优势
	// 以下情况不算投资优势：
	// 1. 只是提到"做投资"但没有具体收益表述
	// 2. 学区房+投资的组合（更多是教育需求）
	// 3. 单纯的保值增值表述
	if strings.Contains(text, "做投资") && !strings.Contains(text, "回报") && !strings.Contains(text, "收益") {
		return false
	}

	if (strings.Contains(text, "学区") || strings.Contains(text, "学校") || strings.Contains(text, "名校")) &&
		strings.Contains(text, "投资") && !strings.Contains(text, "回报") && !strings.Contains(text, "收益") {
		return false
	}

	// 必须有更明确的投资收益表达才算
	return false
}

// detectGoodLocation 检测地段优势
func (afe *AdvancedFieldExtractor) detectGoodLocation(text string) bool {
	// 只检测明确的地段好相关表述，避免通过商圈等间接推理
	locationKeywords := []string{
		"地段好", "位置好", "地理位置好", "区位优势", "地段优质",
		"黄金地段", "核心地段", "优质地段", "成熟地段", "繁华地段",
		"商业地段", // 添加转换后的词汇（"黄金地段" -> "商业地段"）
		// 移除"核心商圈", "一线商圈", "顶级商圈", "成熟商圈" - 这些不是直接的地段好表述
	}

	for _, keyword := range locationKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	return false
}

// detectKindergartenFacility 检测教育设施相关，特别是幼儿园
func (afe *AdvancedFieldExtractor) detectKindergartenFacility(text string) bool {
	// 更全面的幼儿园相关关键词
	kindergartenKeywords := []string{
		"靠近幼儿园", "临近幼儿园", "距离幼儿园", "幼儿园附近", "幼儿园边",
		"示范幼儿园", "国际幼儿园", "双语幼儿园", "公办幼儿园", "私立幼儿园",
		"一级幼儿园", "幼儿园旁边", "幼儿园周边", "步行到幼儿园",
		"走路到幼儿园", "幼儿园便利", "接送方便",
	}

	// 检查是否包含幼儿园且有位置关系词
	hasKindergarten := strings.Contains(text, "幼儿园")
	hasLocationWords := false
	locationWords := []string{"靠近", "临近", "距离", "附近", "边", "旁边", "周边", "便利"}

	for _, word := range locationWords {
		if strings.Contains(text, word) {
			hasLocationWords = true
			break
		}
	}

	// 如果同时包含幼儿园和位置关系词，则认为是近幼儿园
	if hasKindergarten && hasLocationWords {
		return true
	}

	// 直接匹配关键词
	for _, keyword := range kindergartenKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	return false
}

// isValidInterestPointFixed 验证兴趣点是否有效（修复版）
func (afe *AdvancedFieldExtractor) isValidInterestPointFixed(text string, point string) bool {
	switch point {
	case "近综合体":
		// 只有在文本中真的包含相关词汇时才认为有效
		return strings.Contains(text, "综合体") || strings.Contains(text, "商业中心") || strings.Contains(text, "购物中心")
	case "地段好":
		// 只有在文本中明确提到地段好相关词汇时才认为有效，不能通过CBD等间接推理
		directLocationKeywords := []string{
			"地段好", "位置好", "地理位置好", "区位优势", "地段优质",
			"黄金地段", "核心地段", "优质地段", "成熟地段", "繁华地段",
		}
		for _, keyword := range directLocationKeywords {
			if strings.Contains(text, keyword) {
				return true
			}
		}
		return false
	case "商业街":
		// 只有在文本中明确提到商业街相关词汇时才认为有效，不能通过商圈等间接推理
		directStreetKeywords := []string{
			"商业街", "步行街", "商业步行街", "商街",
			"购物街", "商业大街", "繁华商业街",
			"核心商业街", "主要商业街", "商业主街", "商业地带",
		}
		for _, keyword := range directStreetKeywords {
			if strings.Contains(text, keyword) {
				return true
			}
		}
		return false
	case "写字楼里":
		// 只有在文本中明确提到写字楼相关词汇时才认为有效
		// 需要同时检查原始关键词和转换后的关键词
		officeKeywords := []string{
			"写字楼", "写字楼门面", "写字楼底商", "办公楼", "办公楼门面",
			"商务楼", "商务楼门面", "甲级写字楼", "5A写字楼", "智能写字楼",
			"高端写字楼", "商务中心", "办公中心", "企业大厦", "商务大厦",
			// 转换后的关键词（因为同义词映射会转换文本）
			"商业地产", "商铺", "商业地产商铺",
		}
		for _, keyword := range officeKeywords {
			if strings.Contains(text, keyword) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// fixIncorrectLocationDetection 修复错误的地理位置识别
func (afe *AdvancedFieldExtractor) fixIncorrectLocationDetection(fields map[string]interface{}, text string) {
	// 检查location字段是否存在
	if location, ok := fields["location"].(map[string]interface{}); ok {
		// 检查是否错误地将特征词识别为地理位置
		incorrectLocations := []string{"花园", "庭院", "车库", "停车场", "学区", "幼儿园"}

		if district, ok := location["district"].(string); ok {
			for _, incorrect := range incorrectLocations {
				if district == incorrect {
					// 删除错误的地理位置信息
					delete(location, "district")
					if province, ok := location["province"].(string); ok && province == "" {
						delete(location, "province")
					}

					// 如果location为空，删除整个字段
					if len(location) == 0 {
						delete(fields, "location")
					}

					// 将这些特征添加到兴趣点中
					if incorrect == "花园" || incorrect == "庭院" {
						afe.addToInterestPoints(fields, "带花园")
					} else if incorrect == "车库" || incorrect == "停车场" {
						afe.addToInterestPoints(fields, "有车库")
					} else if incorrect == "学区" {
						afe.addToInterestPoints(fields, "学区房")
					} else if incorrect == "幼儿园" {
						afe.addToInterestPoints(fields, "学区房")
					}
					break
				}
			}
		}
	}
}

// addToInterestPoints 添加兴趣点
func (afe *AdvancedFieldExtractor) addToInterestPoints(fields map[string]interface{}, point string) {
	var interestPoints []string

	if existing, ok := fields["interest_points"].([]string); ok {
		interestPoints = existing
	}

	// 检查是否已存在
	for _, existing := range interestPoints {
		if existing == point {
			return
		}
	}

	// 添加新的兴趣点
	interestPoints = append(interestPoints, point)
	fields["interest_points"] = interestPoints
}

// refineVillaType 精细化别墅类型识别
func (afe *AdvancedFieldExtractor) refineVillaType(fields map[string]interface{}, text string) {
	if propertyType, ok := fields["property_type"].(string); ok && propertyType == "别墅" {
		// 检查是否有更具体的别墅类型描述
		villaTypes := []string{"独栋别墅", "联排别墅", "叠拼别墅", "双拼别墅"}
		for _, villaType := range villaTypes {
			villaTypeKeyword := strings.Replace(villaType, "别墅", "", 1)
			if strings.Contains(text, villaTypeKeyword) {
				fields["property_type"] = villaType
				break
			}
		}
	}
}

// detectOfficeBuilding 检测写字楼相关
func (afe *AdvancedFieldExtractor) detectOfficeBuilding(text string) bool {
	// 检测文本中明确提到的写字楼词汇
	officeKeywords := []string{
		"写字楼", "写字楼门面", "写字楼底商", "办公楼", "办公楼门面",
		"商务楼", "商务楼门面", "甲级写字楼", "5A写字楼", "智能写字楼",
		"高端写字楼", "商务中心", "办公中心", "企业大厦", "商务大厦",
	}

	// 检查转换后的文本：写字楼门面 -> 商业地产商铺
	if strings.Contains(text, "商业地产商铺") {
		return true
	}

	// 检查原始文本是否包含明确的写字楼词汇
	for _, keyword := range officeKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	return false
}

// detectHighTraffic 检测人流量大相关
func (afe *AdvancedFieldExtractor) detectHighTraffic(text string) bool {
	// 人流量相关关键词
	trafficKeywords := []string{
		"人流量大", "人流密集", "客流量大", "客流密集", "人气旺",
		"繁华", "热闹", "人潮", "客流多", "流量大", "访客多",
		"人员密集", "客户多", "顾客多", "行人多", "往来人员多",
	}

	for _, keyword := range trafficKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// detectCommercialStreet 检测商业街相关
func (afe *AdvancedFieldExtractor) detectCommercialStreet(text string) bool {
	// 只检测明确的商业街词汇，避免通过"商圈"等词汇间接推理
	streetKeywords := []string{
		"商业街", "步行街", "商业步行街", "商街",
		"购物街", "商业大街", "繁华商业街",
		"核心商业街", "主要商业街", "商业主街", "商业地带",
		// 移除"商业区", "商业中心", "商圈", "商业核心区" - 这些不是直接的商业街表述
	}

	for _, keyword := range streetKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// detectTransportField 检测交通字段
func (afe *AdvancedFieldExtractor) detectTransportField(fields map[string]interface{}, text string) {
	// 检测地铁相关 - 只有明确的地铁字段才创建transport
	if _, hasSubway := fields["subway"]; hasSubway {
		fields["transport"] = "地铁"
		return
	}

	// 检测具体的公交相关关键词 - 只有明确提到公交设施才创建transport字段
	busKeywords := []string{
		"公交", "公交车", "巴士", "班车", "公交站", "车站",
	}

	for _, keyword := range busKeywords {
		if strings.Contains(text, keyword) {
			fields["transport"] = "公交"
			return
		}
	}

	// 注意：移除了"交通便利"等通用描述的transport字段创建
	// 这些内容应该只存在于interest_points中，避免重复
}

// detectEducationField 函数已移除 - 所有教育相关内容通过实体识别添加到interest_points

// addSchoolInterestPoint 添加学校相关的兴趣点
func (afe *AdvancedFieldExtractor) addSchoolInterestPoint(fields map[string]interface{}, keyword string) {
	// 获取现有的兴趣点
	var interestPoints []string
	if existing, ok := fields["interest_points"].([]string); ok {
		interestPoints = existing
	}

	// 检查是否已经存在学区房标签
	hasSchoolDistrict := false
	for _, point := range interestPoints {
		if point == "学区房" {
			hasSchoolDistrict = true
			break
		}
	}

	// 根据关键词类型进行相应处理
	switch {
	case strings.Contains(keyword, "学区房") || strings.Contains(keyword, "学区"):
		afe.addToInterestPoints(fields, "学区房")

	case strings.Contains(keyword, "重点小学") || keyword == "重点小学":
		afe.addToInterestPoints(fields, "重点小学")

	case strings.Contains(keyword, "名校") || strings.Contains(keyword, "重点学校"):
		// 如果已经有学区房标签，不再添加"学校好"
		if !hasSchoolDistrict {
			afe.addToInterestPoints(fields, "学校好")
		}

	case strings.Contains(keyword, "学校好") || strings.Contains(keyword, "好学校"):
		// 如果已经有学区房标签，不再添加"学校好"
		if !hasSchoolDistrict {
			afe.addToInterestPoints(fields, "学校好")
		}

	default:
		// 其他教育相关内容
		if !hasSchoolDistrict {
			afe.addToInterestPoints(fields, "教育需求")
		}
	}
}

// resolveSchoolInterestPointConflicts 解决学区房与学校好的冲突
func (afe *AdvancedFieldExtractor) resolveSchoolInterestPointConflicts(fields map[string]interface{}, text string) {
	// 获取现有的 interest_points
	interestPoints, ok := fields["interest_points"].([]string)
	if !ok {
		return
	}

	// 检查是否同时包含"学区房"和"学校好"
	hasSchoolDistrict := false
	hasGoodSchool := false

	for _, point := range interestPoints {
		if point == "学区房" {
			hasSchoolDistrict = true
		} else if point == "学校好" {
			hasGoodSchool = true
		}
	}

	// 强制规则：如果同时存在"学区房"和"学校好"，移除"学校好"
	// 因为学区房本身就隐含了学校好的意思
	if hasSchoolDistrict && hasGoodSchool {
		var filteredPoints []string
		for _, point := range interestPoints {
			if point != "学校好" {
				filteredPoints = append(filteredPoints, point)
			}
		}
		fields["interest_points"] = filteredPoints
	}
}

// processCommercialEntity 智能处理商业实体
func (afe *AdvancedFieldExtractor) processCommercialEntity(fields map[string]interface{}, entity Entity) {
	// 安全类型转换
	commercialValue, ok := entity.Value.(string)
	if !ok {
		return
	}

	// 获取现有的commercial字段
	var existingCommercial []string
	if existing, ok := fields["commercial"]; ok {
		switch v := existing.(type) {
		case string:
			existingCommercial = []string{v}
		case []string:
			existingCommercial = v
		}
	}

	// 检查是否已存在相同值，避免重复
	for _, existing := range existingCommercial {
		if existing == commercialValue {
			return
		}
	}

	// 添加新的商业特征
	existingCommercial = append(existingCommercial, commercialValue)

	// 智能合成商业描述
	commercialDescription := afe.synthesizeCommercialDescription(existingCommercial)
	fields["commercial"] = commercialDescription
}

// synthesizeCommercialDescription 智能合成商业描述
func (afe *AdvancedFieldExtractor) synthesizeCommercialDescription(commercialFeatures []string) string {
	// 特征权重映射
	featureMap := make(map[string]bool)
	for _, feature := range commercialFeatures {
		featureMap[feature] = true
	}

	// 投资相关特征
	hasInvestment := featureMap["投资回报"] || featureMap["商业投资"]
	hasRentIncome := featureMap["租金收益"]
	hasCommercialProperty := featureMap["商业地产"]
	hasCommercialLocation := featureMap["商业地段"]
	hasCommercialSupport := featureMap["商业配套"]

	// 检查已合成的智能描述
	hasInvestmentProperty := false
	hasRevenuProperty := false
	for _, feature := range commercialFeatures {
		if feature == "投资物业" || feature == "投资性房产" {
			hasInvestmentProperty = true
		}
		if feature == "收益型房产" || feature == "商业地产投资" {
			hasRevenuProperty = true
		}
	}

	// 智能组合描述 - 优先保持已经智能合成的描述
	if hasInvestmentProperty && hasCommercialProperty {
		return "投资性房产"
	} else if hasRevenuProperty && hasCommercialProperty {
		return "投资性房产"
	} else if hasInvestment && hasRentIncome {
		return "投资性房产"
	} else if hasInvestment && hasCommercialProperty {
		return "商业投资"
	} else if hasRentIncome && hasCommercialProperty {
		return "商业地产投资"
	} else if hasCommercialLocation && hasCommercialProperty {
		return "优质商业地产"
	} else if hasInvestmentProperty {
		return "投资物业"
	} else if hasRevenuProperty {
		return "收益型房产"
	} else if hasInvestment {
		return "投资物业"
	} else if hasRentIncome {
		return "收益型房产"
	} else if hasCommercialSupport {
		return "商业配套完善"
	} else if hasCommercialProperty {
		return "商业地产"
	} else if hasCommercialLocation {
		return "商业地段"
	}

	// 默认返回第一个特征
	if len(commercialFeatures) > 0 {
		return commercialFeatures[0]
	}

	return "商业地产"
}

// extractSpecificReturn 提取具体的收益率信息
func (afe *AdvancedFieldExtractor) extractSpecificReturn(text string) string {
	// 使用正则表达式匹配具体的年化收益率
	returnPatterns := []string{
		`年化收益\s*(\d+(?:\.\d+)?)\s*%`,
		`年化\s*(\d+(?:\.\d+)?)\s*%`,
		`收益率\s*(\d+(?:\.\d+)?)\s*%`,
		`回报率\s*(\d+(?:\.\d+)?)\s*%`,
	}

	for _, pattern := range returnPatterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			return fmt.Sprintf("年化收益%s%%", matches[1])
		}
	}

	// 检查是否有"以上"等修饰词
	abovePatterns := []string{
		`年化收益\s*(\d+(?:\.\d+)?)\s*%\s*以上`,
		`年化\s*(\d+(?:\.\d+)?)\s*%\s*以上`,
	}

	for _, pattern := range abovePatterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			return fmt.Sprintf("年化收益%s%%以上", matches[1])
		}
	}

	return ""
}

// getLocationLabel 获取地段相关的精确标签
func (afe *AdvancedFieldExtractor) getLocationLabel(text string, originalText string) string {
	// 检查具体的地段表述，返回最精确的标签
	// 优先检查原始关键词
	if strings.Contains(originalText, "黄金地段") {
		return "黄金地段"
	}
	if strings.Contains(originalText, "核心地段") {
		return "核心地段"
	}
	if strings.Contains(originalText, "优质地段") {
		return "优质地段"
	}
	if strings.Contains(originalText, "成熟地段") {
		return "成熟地段"
	}
	if strings.Contains(originalText, "繁华地段") {
		return "繁华地段"
	}

	// 检查映射后的词汇
	if strings.Contains(text, "商业地段") {
		// 如果包含商业地段，但需要确认这不是其他词汇的映射结果
		// 通过检查上下文确定具体含义
		if strings.Contains(originalText, "黄金") {
			return "黄金地段"
		}
		if strings.Contains(originalText, "核心") {
			return "核心地段"
		}
		if strings.Contains(originalText, "优质") {
			return "优质地段"
		}
		if strings.Contains(originalText, "商圈") {
			return "地段好"
		}
		return "地段好"
	}

	// 检查其他地段好的表述
	if afe.detectGoodLocation(text) {
		return "地段好"
	}

	return ""
}

// detectInvestmentAdvantageStrict 严格检测投资优势
func (afe *AdvancedFieldExtractor) detectInvestmentAdvantageStrict(text string) bool {
	// 投资优势的明确表达词汇 - 排除具体收益率表述
	investmentAdvantageKeywords := []string{
		"投资价值", "投资优势", "投资性价比",
		"投资前景", "投资潜力", "升值空间",
		"保值增值", "增值潜力",
	}

	for _, keyword := range investmentAdvantageKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	// 如果有具体的收益率数字，不算通用的投资优势
	returnNumberPattern := `\d+(?:\.\d+)?%`
	re := regexp.MustCompile(returnNumberPattern)
	if re.MatchString(text) {
		return false
	}

	// 避免误判：单纯的"投资"+"保值增值"等词汇不算投资优势
	if strings.Contains(text, "做投资") && !strings.Contains(text, "优势") && !strings.Contains(text, "价值") {
		return false
	}

	return false
}

// isValidInterestPointWithOriginal 验证兴趣点是否有效（支持原始文本检查）
func (afe *AdvancedFieldExtractor) isValidInterestPointWithOriginal(text string, originalText string, point string) bool {
	switch point {
	case "近综合体":
		// 只有在原始文本中真的包含相关词汇时才认为有效
		// "投资商业综合体"中的"综合体"不应该被认为是"近综合体"
		nearKeywords := []string{"近综合体", "靠近综合体", "临近综合体", "综合体附近", "综合体旁边"}
		for _, keyword := range nearKeywords {
			if strings.Contains(originalText, keyword) {
				return true
			}
		}
		return false
	case "投资优势":
		// 如果有具体的收益率数字，不应该算通用的投资优势
		returnNumberPattern := `\d+(?:\.\d+)?%`
		re := regexp.MustCompile(returnNumberPattern)
		if re.MatchString(originalText) {
			return false
		}

		// 投资优势的明确表达词汇
		investmentAdvantageKeywords := []string{
			"投资价值", "投资优势", "投资性价比",
			"投资前景", "投资潜力", "升值空间",
			"保值增值", "增值潜力",
		}
		for _, keyword := range investmentAdvantageKeywords {
			if strings.Contains(originalText, keyword) {
				return true
			}
		}
		return false
	case "地段好":
		// 只有在文本中明确提到地段好相关词汇时才认为有效，不能通过CBD等间接推理
		directLocationKeywords := []string{
			"地段好", "位置好", "地理位置好", "区位优势", "地段优质",
			"黄金地段", "核心地段", "优质地段", "成熟地段", "繁华地段",
		}
		for _, keyword := range directLocationKeywords {
			if strings.Contains(originalText, keyword) {
				return true
			}
		}
		return false
	default:
		// 对于其他兴趣点，使用原有的验证逻辑
		return afe.isValidInterestPointFixed(text, point)
	}
}

// detectComprehensiveComplexInvestment 检测综合体投资需求
func (afe *AdvancedFieldExtractor) detectComprehensiveComplexInvestment(originalText string) bool {
	// 投资综合体的关键模式
	investmentPatterns := []string{
		"投资.*综合体",
		"买.*综合体",
		"购买.*综合体",
		"选择.*综合体",
		"要.*综合体",
		"看.*综合体",
		"找.*综合体",
	}

	for _, pattern := range investmentPatterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(originalText) {
			return true
		}
	}

	// 直接提到综合体作为投资目标
	directPatterns := []string{
		"商业综合体投资",
		"综合体项目",
		"综合体物业",
		"投资商业综合体",
	}

	for _, pattern := range directPatterns {
		if strings.Contains(originalText, pattern) {
			return true
		}
	}

	return false
}

// filterRentalFields 过滤租金相关字段（仅用于 IgnoreRentalFields 策略）
func (afe *AdvancedFieldExtractor) filterRentalFields(fields map[string]interface{}) {
	// 移除租金价格字段
	delete(fields, "rent_price")

	// 过滤兴趣点中的租赁专用项
	if interestPoints, ok := fields["interest_points"].([]string); ok {
		var filteredPoints []string
		rentalSpecificPoints := map[string]bool{
			"租金回报率高":  true,
			"租金回报率稳定": true,
			"出租方便":    true,
			"租赁需求":    true,
		}

		for _, point := range interestPoints {
			if !rentalSpecificPoints[point] {
				filteredPoints = append(filteredPoints, point)
			}
		}

		if len(filteredPoints) > 0 {
			fields["interest_points"] = filteredPoints
		} else {
			delete(fields, "interest_points")
		}
	}
}

// MatchCandidate 匹配候选信息（包含位置）
type MatchCandidate struct {
	Data     map[string]interface{} // 解析后的数据
	Position int                    // 在文本中的位置
}

// parseBestPriceFromMatchesWithIndex 从多个价格匹配中选择最佳结果（带位置信息）
// 择优策略: operator优先 > 区间优先 > 单值；更窄范围优先；位置靠后优先
func (afe *AdvancedFieldExtractor) parseBestPriceFromMatchesWithIndex(text string, allMatchesWithIndex [][]int) map[string]interface{} {
	if len(allMatchesWithIndex) == 0 {
		return nil
	}

	var candidates []MatchCandidate

	// 解析所有候选，提取匹配文本并转换为字符串切片
	for _, matchIndices := range allMatchesWithIndex {
		if len(matchIndices) >= 2 {
			// 提取匹配的字符串 - 构建matches数组来兼容parsePriceFromRegex
			var matches []string
			for i := 0; i < len(matchIndices); i += 2 {
				if i+1 < len(matchIndices) && matchIndices[i] >= 0 && matchIndices[i+1] <= len(text) {
					matches = append(matches, text[matchIndices[i]:matchIndices[i+1]])
				} else {
					matches = append(matches, "")
				}
			}

			if priceInfo := afe.parsePriceFromRegex(matches); priceInfo != nil {
				candidates = append(candidates, MatchCandidate{
					Data:     priceInfo,
					Position: matchIndices[0], // 使用匹配的起始位置
				})
			}
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	if len(candidates) == 1 {
		return candidates[0].Data
	}

	// 择优逻辑: 根据评分和位置权重
	best := candidates[0]
	bestScore := afe.calculatePriceScoreWithPosition(best.Data, best.Position, len(text))

	afe.logger.WithFields(map[string]interface{}{
		"total_candidates": len(candidates),
		"best_initial":     best.Data,
		"best_position":    best.Position,
		"best_score":       bestScore,
	}).Debug("Price candidate selection started")

	for i := 1; i < len(candidates); i++ {
		candidate := candidates[i]
		score := afe.calculatePriceScoreWithPosition(candidate.Data, candidate.Position, len(text))

		// 如果score更高，或相等但位置更靠后，则更新
		if score > bestScore || (score == bestScore && candidate.Position > best.Position) {
			afe.logger.WithFields(map[string]interface{}{
				"candidate":      candidate.Data,
				"candidate_pos":  candidate.Position,
				"candidate_score": score,
				"replaced_score": bestScore,
			}).Debug("Price candidate selection: new best found")

			best = candidate
			bestScore = score
		}
	}

	afe.logger.WithFields(map[string]interface{}{
		"selected":       best.Data,
		"selected_pos":   best.Position,
		"selected_score": bestScore,
	}).Debug("Price candidate selection completed")

	return best.Data
}

// parseBestAreaFromMatchesWithIndex 从多个面积匹配中选择最佳结果（带位置信息）
// 择优策略: operator优先 > 区间优先 > 单值；更窄范围优先；位置靠后优先
func (afe *AdvancedFieldExtractor) parseBestAreaFromMatchesWithIndex(text string, allMatchesWithIndex [][]int) map[string]interface{} {
	if len(allMatchesWithIndex) == 0 {
		return nil
	}

	var candidates []MatchCandidate

	// 解析所有候选
	for _, matchIndices := range allMatchesWithIndex {
		if len(matchIndices) >= 2 {
			// 提取匹配的字符串 - 构建matches数组来兼容parseAreaFromRegex
			var matches []string
			for i := 0; i < len(matchIndices); i += 2 {
				if i+1 < len(matchIndices) && matchIndices[i] >= 0 && matchIndices[i+1] <= len(text) {
					matches = append(matches, text[matchIndices[i]:matchIndices[i+1]])
				} else {
					matches = append(matches, "")
				}
			}

			if areaInfo := afe.parseAreaFromRegex(matches); areaInfo != nil {
				candidates = append(candidates, MatchCandidate{
					Data:     areaInfo,
					Position: matchIndices[0], // 使用匹配的起始位置
				})
			}
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	if len(candidates) == 1 {
		return candidates[0].Data
	}

	// 择优逻辑: 根据评分和位置权重
	best := candidates[0]
	bestScore := afe.calculateAreaScoreWithPosition(best.Data, best.Position, len(text))

	afe.logger.WithFields(map[string]interface{}{
		"total_candidates": len(candidates),
		"best_initial":     best.Data,
		"best_position":    best.Position,
		"best_score":       bestScore,
	}).Debug("Area candidate selection started")

	for i := 1; i < len(candidates); i++ {
		candidate := candidates[i]
		score := afe.calculateAreaScoreWithPosition(candidate.Data, candidate.Position, len(text))

		// 如果score更高，或相等但位置更靠后，则更新
		if score > bestScore || (score == bestScore && candidate.Position > best.Position) {
			afe.logger.WithFields(map[string]interface{}{
				"candidate":      candidate.Data,
				"candidate_pos":  candidate.Position,
				"candidate_score": score,
				"replaced_score": bestScore,
			}).Debug("Area candidate selection: new best found")

			best = candidate
			bestScore = score
		}
	}

	afe.logger.WithFields(map[string]interface{}{
		"selected":       best.Data,
		"selected_pos":   best.Position,
		"selected_score": bestScore,
	}).Debug("Area candidate selection completed")

	return best.Data
}

// parseBestRentFromMatchesWithIndex 从多个租金匹配中选择最佳结果（带位置信息）
// 择优策略: operator优先 > 区间优先 > 单值；更窄范围优先；位置靠后优先
func (afe *AdvancedFieldExtractor) parseBestRentFromMatchesWithIndex(text string, allMatchesWithIndex [][]int) map[string]interface{} {
	if len(allMatchesWithIndex) == 0 {
		return nil
	}

	var candidates []MatchCandidate

	// 解析所有候选
	for _, matchIndices := range allMatchesWithIndex {
		if len(matchIndices) >= 2 {
			// 提取匹配的字符串 - 构建matches数组来兼容parseRentPriceFromRegex
			var matches []string
			for i := 0; i < len(matchIndices); i += 2 {
				if i+1 < len(matchIndices) && matchIndices[i] >= 0 && matchIndices[i+1] <= len(text) {
					matches = append(matches, text[matchIndices[i]:matchIndices[i+1]])
				} else {
					matches = append(matches, "")
				}
			}

			if rentInfo := afe.parseRentPriceFromRegex(matches); rentInfo != nil {
				candidates = append(candidates, MatchCandidate{
					Data:     rentInfo,
					Position: matchIndices[0], // 使用匹配的起始位置
				})
			}
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	if len(candidates) == 1 {
		return candidates[0].Data
	}

	// 择优逻辑: 根据评分和位置权重
	best := candidates[0]
	bestScore := afe.calculateRentScoreWithPosition(best.Data, best.Position, len(text))

	afe.logger.WithFields(map[string]interface{}{
		"total_candidates": len(candidates),
		"best_initial":     best.Data,
		"best_position":    best.Position,
		"best_score":       bestScore,
	}).Debug("Rent candidate selection started")

	for i := 1; i < len(candidates); i++ {
		candidate := candidates[i]
		score := afe.calculateRentScoreWithPosition(candidate.Data, candidate.Position, len(text))

		// 如果score更高，或相等但位置更靠后，则更新
		if score > bestScore || (score == bestScore && candidate.Position > best.Position) {
			afe.logger.WithFields(map[string]interface{}{
				"candidate":      candidate.Data,
				"candidate_pos":  candidate.Position,
				"candidate_score": score,
				"replaced_score": bestScore,
			}).Debug("Rent candidate selection: new best found")

			best = candidate
			bestScore = score
		}
	}

	afe.logger.WithFields(map[string]interface{}{
		"selected":       best.Data,
		"selected_pos":   best.Position,
		"selected_score": bestScore,
	}).Debug("Rent candidate selection completed")

	return best.Data
}


// calculatePriceScore 计算价格信息的优先级分数
func (afe *AdvancedFieldExtractor) calculatePriceScore(priceInfo map[string]interface{}) int {
	// 评分常量定义
	const (
		operatorScore    = 100 // 有操作符的加分
		rangeScore       = 50  // 有区间的加分
		singleSideScore  = 30  // 单边限制加分
		valueScore       = 10  // 有单值加分
		maxRangeBonus    = 20  // 窄范围最大额外加分
		rangeDivisor     = 200.0 // 范围评分除数
	)

	score := 0

	// 有operator: +100分
	if _, hasOperator := priceInfo["operator"]; hasOperator {
		score += operatorScore
	}

	// 有区间(min+max): +50分
	_, hasMin := priceInfo["min"]
	_, hasMax := priceInfo["max"]
	if hasMin && hasMax {
		score += rangeScore
		// 更窄范围额外加分
		if minVal, ok := priceInfo["min"].(float64); ok {
			if maxVal, ok := priceInfo["max"].(float64); ok {
				range_ := maxVal - minVal
				// 范围越小分数越高，最多+20分
				if range_ > 0 {
					rangeBonus := int(rangeDivisor / range_) // 范围越小分越高
					if rangeBonus > maxRangeBonus {
						rangeBonus = maxRangeBonus
					}
					score += rangeBonus
				}
			}
		}
	} else if hasMin || hasMax {
		// 单边限制: +30分
		score += singleSideScore
	}

	// 有单值: +10分
	if _, hasValue := priceInfo["value"]; hasValue {
		score += valueScore
	}

	return score
}

// calculatePriceScoreWithPosition 计算价格信息的优先级分数（带位置权重）
func (afe *AdvancedFieldExtractor) calculatePriceScoreWithPosition(priceInfo map[string]interface{}, position int, textLength int) int {
	// 基础评分
	score := afe.calculatePriceScore(priceInfo)

	// 位置权重：靠后的位置获得微小加分（最多+5分）
	// 这确保在其他条件相等时，后出现的匹配优先
	if textLength > 0 {
		positionRatio := float64(position) / float64(textLength)
		positionBonus := int(positionRatio * 5) // 最多5分的位置加分
		score += positionBonus
	}

	return score
}


// calculateAreaScore 计算面积信息的优先级分数
func (afe *AdvancedFieldExtractor) calculateAreaScore(areaInfo map[string]interface{}) int {
	// 评分常量定义 (与价格相同的优先级体系)
	const (
		operatorScore    = 100 // 有操作符的加分
		rangeScore       = 50  // 有区间的加分
		singleSideScore  = 30  // 单边限制加分
		valueScore       = 10  // 有单值加分
		maxRangeBonus    = 20  // 窄范围最大额外加分
		rangeDivisor     = 100.0 // 面积范围评分除数（比价格更小）
	)

	score := 0

	// 有operator: +100分
	if _, hasOperator := areaInfo["operator"]; hasOperator {
		score += operatorScore
	}

	// 有区间(min+max): +50分
	_, hasMin := areaInfo["min"]
	_, hasMax := areaInfo["max"]
	if hasMin && hasMax {
		score += rangeScore
		// 更窄范围额外加分
		if minVal, ok := areaInfo["min"].(float64); ok {
			if maxVal, ok := areaInfo["max"].(float64); ok {
				range_ := maxVal - minVal
				// 范围越小分数越高，最多+20分
				if range_ > 0 {
					rangeBonus := int(rangeDivisor / range_) // 面积范围分母更小
					if rangeBonus > maxRangeBonus {
						rangeBonus = maxRangeBonus
					}
					score += rangeBonus
				}
			}
		}
	} else if hasMin || hasMax {
		// 单边限制: +30分
		score += singleSideScore
	}

	// 有单值: +10分
	if _, hasValue := areaInfo["value"]; hasValue {
		score += valueScore
	}

	return score
}

// calculateAreaScoreWithPosition 计算面积信息的优先级分数（带位置权重）
func (afe *AdvancedFieldExtractor) calculateAreaScoreWithPosition(areaInfo map[string]interface{}, position int, textLength int) int {
	// 基础评分
	score := afe.calculateAreaScore(areaInfo)

	// 位置权重：靠后的位置获得微小加分（最多+5分）
	// 这确保在其他条件相等时，后出现的匹配优先
	if textLength > 0 {
		positionRatio := float64(position) / float64(textLength)
		positionBonus := int(positionRatio * 5) // 最多5分的位置加分
		score += positionBonus
	}

	return score
}

// calculateRentScore 计算租金信息的优先级分数
// 评分策略：区间 > 单值；更窄范围 > 更宽范围
func (afe *AdvancedFieldExtractor) calculateRentScore(rentInfo map[string]interface{}) int {
	const (
		RENT_RANGE_BONUS = 50    // 区间类型基础分
		RENT_SINGLE_BONUS = 30   // 单值类型基础分
		RENT_VALUE_BONUS = 10    // 数值大小权重
	)

	score := 0

	// 检查是否为区间类型
	_, hasMin := rentInfo["min"]
	_, hasMax := rentInfo["max"]

	if hasMin && hasMax {
		// 区间类型
		score += RENT_RANGE_BONUS

		// 区间范围越小评分越高
		if min, ok := rentInfo["min"].(float64); ok {
			if max, ok := rentInfo["max"].(float64); ok && max > min {
				rangeWidth := max - min
				// 范围奖励：范围越小奖励越高（最多20分）
				if rangeWidth > 0 {
					rangeBonus := int(2000 / rangeWidth) // 2000元内的区间获得满分20分
					if rangeBonus > 20 {
						rangeBonus = 20
					}
					score += rangeBonus
				}
			}
		}
	} else {
		// 单值类型
		score += RENT_SINGLE_BONUS
	}

	// 基于数值大小的微调（确保有价格信息的优先）
	if value, ok := rentInfo["value"].(float64); ok {
		score += int(value/1000) % RENT_VALUE_BONUS // 每1000元加1分，最多10分
	} else if min, ok := rentInfo["min"].(float64); ok {
		score += int(min/1000) % RENT_VALUE_BONUS
	}

	return score
}

// calculateRentScoreWithPosition 计算租金信息的优先级分数（带位置权重）
func (afe *AdvancedFieldExtractor) calculateRentScoreWithPosition(rentInfo map[string]interface{}, position int, textLength int) int {
	// 基础评分
	score := afe.calculateRentScore(rentInfo)

	// 位置权重：靠后的位置获得微小加分（最多+5分）
	// 这确保在其他条件相等时，后出现的匹配优先
	if textLength > 0 {
		positionRatio := float64(position) / float64(textLength)
		positionBonus := int(positionRatio * 5) // 最多5分的位置加分
		score += positionBonus
	}

	return score
}

// Close 关闭提取器
func (afe *AdvancedFieldExtractor) Close() error {
	if afe.segmenter != nil {
		afe.segmenter.Close()
	}
	return nil
}
