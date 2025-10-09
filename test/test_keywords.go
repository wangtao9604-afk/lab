//go:build ignore
// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"qywx/infrastructures/config"
	"qywx/infrastructures/keywords"
	"qywx/models/schedule"
	"qywx/models/thirdpart/minimax"
)

// getRandomPlate 获取指定区域的随机板块
func getRandomPlate(district string) string {
	plates := keywords.GetShanghaiPlates(district)
	if len(plates) == 0 {
		return ""
	}
	index := rand.Intn(len(plates))
	return plates[index]
}

// getRandomPlateWithDistrict 获取随机区域和板块组合
func getRandomPlateWithDistrict() string {
	districts := []string{"浦东", "闵行", "宝山", "徐汇", "松江", "嘉定", "普陀", "黄浦", "青浦", "静安", "奉贤", "长宁", "杨浦", "崇明", "虹口"}
	districtIndex := rand.Intn(len(districts))
	district := districts[districtIndex]

	plates := keywords.GetShanghaiPlates(district)
	if len(plates) == 0 {
		return district
	}
	plateIndex := rand.Intn(len(plates))
	return district + plates[plateIndex]
}

func main() {
	fmt.Println("===== 关键字提取测试程序 =====")
	fmt.Printf("测试时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	// 初始化随机数生成器
	rand.Seed(time.Now().UnixNano())

	// 创建结果文件
	fileName := fmt.Sprintf("keyword_test_result_%s.txt", time.Now().Format("20060102_150405"))
	file, err := os.Create(fileName)
	if err != nil {
		fmt.Printf("创建结果文件失败: %v\n", err)
		return
	}
	defer file.Close()

	// 写入文件头
	file.WriteString("===== 关键字提取测试结果 =====\n")
	file.WriteString(fmt.Sprintf("测试时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	// 运行单句测试
	testSingleSentences(file)

	// 运行多语句测试
	testMultiMessages(file)

	// 运行转换测试
	testIpangConversion(file)

	// 运行租售过滤测试
	testRentalFilter(file)

	// 运行房型转换测试
	testRoomCountConversion(file)

	// 运行多匹配择优策略测试
	testMultiMatchPreference(file)

	fmt.Printf("\n测试结果已保存到: %s\n", fileName)
}

// testMultiMatchPreference 多匹配择优策略测试（用于回归对比）
// 目标：构造包含多个“价格/面积”表达的句子，便于在实现多匹配择优策略前后做回归对比
// 说明：当前实现可能仅命中首个匹配；优化后应按“更具体/有operator/更窄范围/就近出现”择优
func testMultiMatchPreference(file *os.File) {
	service := schedule.NewKeywordExtractorService(nil, "")

	cases := []struct {
		name    string
		message string
		note    string // 期望倾向/说明
	}{
		{
			name:    "价格-区间+上限（区间优先）",
			message: "预算300-500万，最好不超过600万",
			note:    "倾向选择区间300-500万；上限600万作为次要信息",
		},
		{
			name:    "价格-单值+区间（区间优先）",
			message: "预算800万，700-900万都可以",
			note:    "倾向选择区间700-900万；当前可能命中单值800万（用于回归对照）",
		},
		{
			name:    "价格-多区间（更窄范围优先）",
			message: "价格100-200万，也可150-180万",
			note:    "倾向选择更窄的150-180万",
		},
		{
			name:    "价格-混合单位与上限（更严格约束优先）",
			message: "5000万-1亿，不超过8000万",
			note:    "倾向选择不超过8000万（更严格上限）；或保留范围并叠加上限（取决于实现）",
		},
		{
			name:    "面积-区间+上限（有operator优先）",
			message: "面积80-120平，最好不超过100平",
			note:    "倾向选择上限<=100平（带operator）；当前可能命中区间80-120平",
		},
		{
			name:    "面积-左右+区间（区间优先）",
			message: "120平左右，100-140平都能接受",
			note:    "倾向选择区间100-140平；当前可能命中120平左右",
		},
		{
			name:    "面积-多区间（更窄范围优先）",
			message: "面积100-150平，最好110-130平",
			note:    "倾向选择更窄的110-130平",
		},
	}

	fmt.Println("\n【多匹配择优策略测试】")
	file.WriteString("\n【多匹配择优策略测试】\n\n")

	for i, tc := range cases {
		fmt.Printf("[%d/%d] %s\n", i+1, len(cases), tc.name)
		fmt.Printf("原始: %s\n", tc.message)
		fmt.Printf("期望倾向: %s\n", tc.note)

		file.WriteString(fmt.Sprintf("========================================\n"))
		file.WriteString(fmt.Sprintf("[%d/%d] %s\n", i+1, len(cases), tc.name))
		file.WriteString(fmt.Sprintf("原始消息: %s\n", tc.message))
		file.WriteString(fmt.Sprintf("期望倾向: %s\n", tc.note))

		history := []minimax.Message{{Role: "user", Content: tc.message}}
		result, err := service.ExtractFromConversationHistory(history)
		if err != nil {
			fmt.Printf("❌ 错误: %v\n", err)
			file.WriteString(fmt.Sprintf("错误: %v\n\n", err))
			continue
		}

		fmt.Println("✅ 提取结果:")
		// 定向打印 price/area 便于横向对比
		// 使用已有的格式化输出，保障和其他测试一致
		printResult(result, file)
		fmt.Println()
	}
}

func testSingleSentences(file *os.File) {
	service := schedule.NewKeywordExtractorService(nil, "")

	// 完整的17个单句测试用例
	testCases := []struct {
		name    string
		message string
	}{
		{
			name:    "测试1-浦东2室1厅",
			message: "我想在上海浦东" + getRandomPlate("浦东") + "找个2室1厅的房子，预算200万左右，最好南北通透",
		},
		{
			name:    "测试2-静安精装公寓",
			message: "帮我找一下静安区" + getRandomPlate("静安") + "的精装修公寓，3室2厅，预算300-500万",
		},
		{
			name:    "测试3-近地铁小户型",
			message: "有没有" + getRandomPlateWithDistrict() + "近地铁的小户型，1室1厅就行，总价150万以内",
		},
		{
			name:    "测试4-黄浦学区房",
			message: "我要买学区房，黄浦区" + getRandomPlate("黄浦") + "的，面积不限，预算充足",
		},
		{
			name:    "测试5-投资房产",
			message: "找个" + getRandomPlateWithDistrict() + "的投资房产，租金回报率要高，地段好的商铺也可以",
		},
		{
			name:    "测试6-徐汇120平三室",
			message: "我想在徐汇区" + getRandomPlate("徐汇") + "买一套120平米左右的三室两厅，毛坯房或简装都可以，价格600万以内，最好靠近地铁站",
		},
		{
			name:    "测试7-租一居室",
			message: "想在" + getRandomPlateWithDistrict() + "租个一居室，月租金3000-5000，交通便利，拎包入住",
		},
		{
			name:    "测试8-独栋别墅",
			message: "找" + getRandomPlateWithDistrict() + "的别墅，独栋的，带花园和车库，面积300平以上，价格2000万左右",
		},
		{
			name:    "测试9-浦东重点小学",
			message: "想要对口重点小学的学区房，浦东新区" + getRandomPlate("浦东") + "，3室2厅，预算800万",
		},
		{
			name:    "测试10-名校投资",
			message: "买一套" + getRandomPlateWithDistrict() + "的名校房源做投资，学位价值高的，保值增值",
		},
		{
			name:    "测试11-近幼儿园",
			message: "找" + getRandomPlateWithDistrict() + "靠近示范幼儿园的房子，准备给孩子上学用",
		},
		{
			name:    "测试12-CBD商铺",
			message: "想买个" + getRandomPlateWithDistrict() + "商铺做投资，CBD核心商圈的，租金回报率稳定",
		},
		{
			name:    "测试13-写字楼门面",
			message: "找个" + getRandomPlateWithDistrict() + "的写字楼门面出租，人流量大的商业街，月租金8000左右",
		},
		{
			name:    "测试14-商业综合体",
			message: "投资" + getRandomPlateWithDistrict() + "的商业综合体，黄金地段，年化收益6%以上",
		},
		{
			name:    "测试15-学校医院",
			message: "找个" + getRandomPlateWithDistrict() + "学校好的房子，孩子要上学，医院近点更好",
		},
		{
			name:    "测试16-地铁上盖",
			message: "要" + getRandomPlateWithDistrict() + "地铁上盖的房子，近综合体，吃饭方便",
		},
		{
			name:    "测试17-租一居室(重复)",
			message: "想在" + getRandomPlateWithDistrict() + "租个一居室，月租金3000-5000，交通便利，拎包入住",
		},
		{
			name:    "测试18-科学城地标",
			message: "我想买一套120平左右靠近" + getRandomPlateWithDistrict() + "科学城的房子",
		},
		{
			name:    "测试19-陆家嘴地标",
			message: "找一个浦东陆家嘴金融中心附近的办公室",
		},
		{
			name:    "测试20-谓语",
			message: "帮忙找一套" + getRandomPlateWithDistrict() + "的三室两厅，购买后用来出租",
		},
		{
			name:    "线上问题1",
			message: "一家三口，希望推荐在浦东新区靠近小学和初中的学区房",
		},
		{
			name:    "线上问题2",
			message: "一家三口，希望推荐在黄浦区靠近小学和初中的学区房，预算800万，3室",
		},
		{
			name:    "线上问题3",
			message: "房屋面积1千平，户型暂时没有要求",
		},
		{
			name:    "线上问题4",
			message: "5000万 到3亿",
		},
		{
			name:    "线上问题5",
			message: "一家三口，希望推荐在黄浦区靠近小学和初中的学区房，预算800万，3室",
		},
	}

	fmt.Println("【单句提取测试】")
	file.WriteString("【单句提取测试】\n\n")

	successCount := 0
	totalCount := len(testCases)

	for i, tc := range testCases {
		fmt.Printf("[%d/%d] %s\n", i+1, totalCount, tc.name)
		fmt.Printf("原始: %s\n", tc.message)

		// 写入文件
		file.WriteString(fmt.Sprintf("========================================\n"))
		file.WriteString(fmt.Sprintf("[%d/%d] %s\n", i+1, totalCount, tc.name))
		file.WriteString(fmt.Sprintf("原始消息: %s\n", tc.message))

		history := []minimax.Message{{Role: "user", Content: tc.message}}
		result, err := service.ExtractFromConversationHistory(history)
		if err != nil {
			fmt.Printf("❌ 错误: %v\n", err)
			file.WriteString(fmt.Sprintf("错误: %v\n\n", err))
			continue
		}

		successCount++
		fmt.Println("✅ 提取结果:")
		printResult(result, file)
		fmt.Println()
	}

	// 输出统计
	fmt.Printf("\n单句测试完成: %d/%d 成功\n", successCount, totalCount)
	file.WriteString(fmt.Sprintf("\n单句测试统计: %d/%d 成功\n\n", successCount, totalCount))
}

func testMultiMessages(file *os.File) {
	service := schedule.NewKeywordExtractorService(nil, "")

	// 多语句测试用例
	testCases := []struct {
		name     string
		messages []string
	}{
		{
			name: "成都高新区租房",
			messages: []string{
				"我想了解一下成都高新区的房源",
				"租房，月租3000左右",
				"120平米，三居室，靠近华府大道地铁的房源",
				"电梯房，精装房",
			},
		},
		{
			name: "成都天府新区买房",
			messages: []string{
				"你好，我想了解成都天府新区的房源",
				"我想买一套120平左右靠近科学城的房子",
				"预算200万内，套三",
				"一梯两户，精装修",
			},
		},
	}

	fmt.Println("\n【多语句提取测试】")
	file.WriteString("\n【多语句提取测试】\n\n")

	for _, tc := range testCases {
		fmt.Printf(">>> %s\n", tc.name)
		fmt.Println("对话内容:")

		file.WriteString(fmt.Sprintf("========================================\n"))
		file.WriteString(fmt.Sprintf("%s\n", tc.name))
		file.WriteString("对话内容:\n")

		history := make([]minimax.Message, 0)
		for i, msg := range tc.messages {
			fmt.Printf("  %d. %s\n", i+1, msg)
			file.WriteString(fmt.Sprintf("  %d. %s\n", i+1, msg))
			history = append(history, minimax.Message{Role: "user", Content: msg})
		}

		result, err := service.ExtractFromConversationHistory(history)
		if err != nil {
			fmt.Printf("❌ 错误: %v\n", err)
			file.WriteString(fmt.Sprintf("错误: %v\n\n", err))
			continue
		}

		fmt.Println("\n✅ 汇总结果:")
		file.WriteString("\n汇总结果:\n")
		printResult(result, file)
		fmt.Println()
	}
}

// jsonStringNoEscape 将对象转换为JSON字符串，不转义HTML字符
func jsonStringNoEscape(v interface{}) string {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.Encode(v)
	// 移除末尾的换行符
	result := buffer.String()
	if len(result) > 0 && result[len(result)-1] == '\n' {
		result = result[:len(result)-1]
	}
	return result
}

func printResult(result *schedule.KeywordExtractionResult, file *os.File) {
	// 计算提取的维度数
	dimensionCount := 0

	// 使用JSON格式化输出具体类型
	if len(result.Location) > 0 {
		dimensionCount++
		data := jsonStringNoEscape(result.Location)
		output := fmt.Sprintf("  📍 location: %s\n", data)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  location: %s\n", data))
	}
	if len(result.Decoration) > 0 {
		dimensionCount++
		output := fmt.Sprintf("  🏠 decoration: %v\n", result.Decoration)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  decoration: %v\n", result.Decoration))
	}
	if len(result.PropertyType) > 0 {
		dimensionCount++
		output := fmt.Sprintf("  🏢 property_type: %v\n", result.PropertyType)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  property_type: %v\n", result.PropertyType))
	}
	if len(result.RoomLayout) > 0 {
		dimensionCount++
		data := jsonStringNoEscape(result.RoomLayout)
		output := fmt.Sprintf("  🛏️ room_layout: %s\n", data)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  room_layout: %s\n", data))
	}
	if len(result.Price) > 0 {
		dimensionCount++
		data := jsonStringNoEscape(result.Price)
		output := fmt.Sprintf("  💰 price: %s\n", data)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  price: %s\n", data))
	}
	if len(result.RentPrice) > 0 {
		dimensionCount++
		data := jsonStringNoEscape(result.RentPrice)
		output := fmt.Sprintf("  💳 rent_price: %s\n", data)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  rent_price: %s\n", data))
	}
	if len(result.Area) > 0 {
		dimensionCount++
		data := jsonStringNoEscape(result.Area)
		output := fmt.Sprintf("  📏 area: %s\n", data)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  area: %s\n", data))
	}
	if len(result.InterestPoints) > 0 {
		dimensionCount++
		output := fmt.Sprintf("  ⭐ interest_points: %v\n", result.InterestPoints)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  interest_points: %v\n", result.InterestPoints))
	}
	if len(result.Commercial) > 0 {
		dimensionCount++
		output := fmt.Sprintf("  💼 commercial: %v\n", result.Commercial)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  commercial: %v\n", result.Commercial))
	}
	if len(result.Orientation) > 0 {
		dimensionCount++
		output := fmt.Sprintf("  🧭 orientation: %v\n", result.Orientation)
		fmt.Print(output)
		file.WriteString(fmt.Sprintf("  orientation: %v\n", result.Orientation))
	}

	// 输出统计
	summary := fmt.Sprintf("  [提取到 %d 个维度]\n", dimensionCount)
	fmt.Print(summary)
	file.WriteString(summary)
}

// testIpangConversion 测试Keywords到Ipang API参数的转换
func testIpangConversion(file *os.File) {
	fmt.Println("\n【Keywords到Ipang API转换测试】")
	file.WriteString("\n\n【Keywords到Ipang API转换测试】\n\n")

	service := schedule.NewKeywordExtractorService(nil, "")
	adapter := schedule.NewIpangAdapter()

	// 合并所有测试用例：18个单句 + 2个多语句
	testCases := []struct {
		name     string
		messages []string
	}{
		// === 18个单句测试用例 ===
		{
			name: "测试1-浦东2室1厅",
			messages: []string{
				"我想在上海浦东" + getRandomPlate("浦东") + "找个2室1厅的房子，预算200万左右，最好南北通透",
			},
		},
		{
			name: "测试2-静安精装公寓",
			messages: []string{
				"帮我找一下静安区" + getRandomPlate("静安") + "的精装修公寓，3室2厅，预算300-500万",
			},
		},
		{
			name: "测试3-近地铁小户型",
			messages: []string{
				"有没有" + getRandomPlateWithDistrict() + "近地铁的小户型，1室1厅就行，总价150万内",
			},
		},
		{
			name: "测试4-黄浦学区房",
			messages: []string{
				"我要买学区房，黄浦区" + getRandomPlate("黄浦") + "的，面积不限，预算充足",
			},
		},
		{
			name: "测试5-投资房产",
			messages: []string{
				"找个" + getRandomPlateWithDistrict() + "的投资房产，租金回报率要高，地段好的商铺也可以",
			},
		},
		{
			name: "测试6-徐汇120平三室",
			messages: []string{
				"我想在徐汇区" + getRandomPlate("徐汇") + "买一套120平米左右的三室两厅，毛坯房或简装都可以，价格600万以内，最好靠近地铁站",
			},
		},
		{
			name: "测试7-租一居室",
			messages: []string{
				"想在" + getRandomPlateWithDistrict() + "租个一居室，月租金3000-5000，交通便利，拎包入住",
			},
		},
		{
			name: "测试8-独栋别墅",
			messages: []string{
				"找" + getRandomPlateWithDistrict() + "的别墅，独栋的，带花园和车库，面积300平以上，价格2000万左右",
			},
		},
		{
			name: "测试9-浦东重点小学",
			messages: []string{
				"想要对口重点小学的学区房，浦东新区" + getRandomPlate("浦东") + "，3室2厅，预算800万",
			},
		},
		{
			name: "测试10-名校投资",
			messages: []string{
				"买一套" + getRandomPlateWithDistrict() + "的名校房源做投资，学位价值高的，保值增值",
			},
		},
		{
			name: "测试11-近幼儿园",
			messages: []string{
				"找" + getRandomPlateWithDistrict() + "靠近示范幼儿园的房子，准备给孩子上学用",
			},
		},
		{
			name: "测试12-CBD商铺",
			messages: []string{
				"想买个" + getRandomPlateWithDistrict() + "商铺做投资，CBD核心商圈的，租金回报率稳定",
			},
		},
		{
			name: "测试13-写字楼门面",
			messages: []string{
				"找个" + getRandomPlateWithDistrict() + "的写字楼门面出租，人流量大的商业街，月租金8000左右",
			},
		},
		{
			name: "测试14-商业综合体",
			messages: []string{
				"投资" + getRandomPlateWithDistrict() + "的商业综合体，黄金地段，年化收益6%以上",
			},
		},
		{
			name: "测试15-学校医院",
			messages: []string{
				"找个" + getRandomPlateWithDistrict() + "学校好的房子，孩子要上学，医院近点更好",
			},
		},
		{
			name: "测试16-地铁上盖",
			messages: []string{
				"要" + getRandomPlateWithDistrict() + "地铁上盖的房子，近综合体，吃饭方便",
			},
		},
		{
			name: "测试17-租一居室(重复)",
			messages: []string{
				"想在" + getRandomPlateWithDistrict() + "租个一居室，月租金3000-5000，交通便利，拎包入住",
			},
		},
		{
			name: "测试18-科学城地标",
			messages: []string{
				"我想买一套120平左右靠近" + getRandomPlateWithDistrict() + "科学城的房子",
			},
		},
		{
			name: "测试19-陆家嘴地标",
			messages: []string{
				"找一个浦东陆家嘴金融中心附近的办公室",
			},
		},
		// === 4个多语句测试用例 ===
		// {
		// 	name: "多语句1-成都高新区租房",
		// 	messages: []string{
		// 		"我想了解一下成都高新区的房源，特别是" + getRandomPlateWithDistrict() + "这边",
		// 		"租房，月租3000左右",
		// 		"120平米，三居室，靠近华府大道地铁的房源",
		// 		"电梯房，精装房",
		// 	},
		// },
		// {
		// 	name: "多语句2-成都天府新区买房",
		// 	messages: []string{
		// 		"你好，我想了解成都天府新区的房源，重点看" + getRandomPlateWithDistrict() + "区域",
		// 		"我想买一套120平左右靠近科学城的房子",
		// 		"预算200万内，套三",
		// 		"一梯两户，精装修",
		// 	},
		// },
		// {
		// 	name: "多语句3-上海不限制地段买房",
		// 	messages: []string{
		// 		"我想买房，地段没要求",
		// 		"预算2000万以内",
		// 		"面积100平方米左右",
		// 	},
		// },
		{
			name: "线上问题调试-Case 1",
			messages: []string{
				"400平",
				"5室3厅3卫",
				"浦东新区 新房 别墅 价格预算 5000万到8000万",
				"新房",
				"浦东新区 郊外",
				"5000万 到1亿",
				"房屋面积1千平，户型暂时没有要求",
				"浦东新区 别墅推荐 价格5000万",
				"杨浦区 18楼 精装房 4室两厅三卫 带阳台 价格1200万推荐一下",
				"杨浦区 18楼 精装房 4室两厅三卫 带阳台 价格600万推荐一下",
				"杨浦区 18楼 精装房 4室两厅三卫 带阳台  预算600万推荐一下",
				"杨浦区 18楼 精装房120平米  4室两厅三卫 带阳台  预算600万推荐一下",
				"需要怎么提？",
				"一家三口，希望推荐在浦东新区靠近小学和初中的学区房",
				"一家三口，希望推荐在黄浦区靠近小学和初中的学区房，预算800万",
				"一家三口，希望推荐在黄浦区靠近小学和初中的学区房，预算800万，3室",
			},
		},
		{
			name: "线上问题调试-Case 2",
			messages: []string{
				"我想咨询杨浦区房源",
				"新房购买",
				"预算1000万",
				"四室",
				"140平，电梯房",
				"没有其他补充",
				"1000万左右",
				"预算不限",
				"预算500万",
				"预算100万到800万，面积120平左右",
				"我想了解浦东新区的房源",
				"杨浦区 18楼 精装房120平米  4室两厅三卫 带阳台  预算600万推荐一下",
				"需要怎预算500万内，面积120平，新房购买",
				"要求电梯房，精装修",
				"一家三口，希望推荐在黄浦区靠近小学和初中的学区房，预算800万",
				"没有了",
			},
		},
	}

	for i, tc := range testCases {
		fmt.Printf("========================================\n")
		fmt.Printf("[%d] %s\n", i+1, tc.name)
		file.WriteString(fmt.Sprintf("========================================\n"))
		file.WriteString(fmt.Sprintf("[%d] %s\n", i+1, tc.name))

		// 打印原始消息
		fmt.Println("\n原始消息:")
		file.WriteString("\n原始消息:\n")
		for j, msg := range tc.messages {
			fmt.Printf("  %d. %s\n", j+1, msg)
			file.WriteString(fmt.Sprintf("  %d. %s\n", j+1, msg))
		}

		// 构建历史消息
		history := make([]minimax.Message, 0)
		for _, msg := range tc.messages {
			history = append(history, minimax.Message{Role: "user", Content: msg})
		}

		// 提取关键字
		result, err := service.ExtractFromConversationHistory(history)
		if err != nil {
			fmt.Printf("\n❌ 关键字提取错误: %v\n", err)
			file.WriteString(fmt.Sprintf("\n❌ 关键字提取错误: %v\n", err))
			continue
		}

		// 打印提取结果
		fmt.Println("\n✅ Keywords提取结果:")
		file.WriteString("\n✅ Keywords提取结果:\n")
		printCompactResult(result, file)

		// 转换为Ipang参数
		transformResult := adapter.Transform(result, nil)

		// 打印转换结果
		fmt.Println("\n🔄 Ipang API参数转换:")
		file.WriteString("\n🔄 Ipang API参数转换:\n")

		// 打印转换后的参数
		if transformResult.Params != nil {
			params := transformResult.Params
			fmt.Println("  API参数:")
			file.WriteString("  API参数:\n")

			if params.Area != "" {
				fmt.Printf("    Area (区域): %s\n", params.Area)
				file.WriteString(fmt.Sprintf("    Area (区域): %s\n", params.Area))
			}
			if params.Amount != "" {
				// 判断是否为范围
				if strings.Contains(params.Amount, ",") {
					parts := strings.Split(params.Amount, ",")
					fmt.Printf("    Amount (总价): %s-%s万\n", parts[0], parts[1])
					file.WriteString(fmt.Sprintf("    Amount (总价): %s-%s万\n", parts[0], parts[1]))
				} else {
					fmt.Printf("    Amount (总价): %s万\n", params.Amount)
					file.WriteString(fmt.Sprintf("    Amount (总价): %s万\n", params.Amount))
				}
			}
			if params.Count != "" {
				fmt.Printf("    Count (房间数): %s室\n", params.Count)
				file.WriteString(fmt.Sprintf("    Count (房间数): %s室\n", params.Count))
			}
			if params.Plate != "" {
				fmt.Printf("    Plate (板块): %s\n", params.Plate)
				file.WriteString(fmt.Sprintf("    Plate (板块): %s\n", params.Plate))
			}
			if params.Size != "" {
				// 判断是否为范围
				if strings.Contains(params.Size, ",") {
					parts := strings.Split(params.Size, ",")
					fmt.Printf("    Size (面积): %s-%s㎡\n", parts[0], parts[1])
					file.WriteString(fmt.Sprintf("    Size (面积): %s-%s㎡\n", parts[0], parts[1]))
				} else {
					fmt.Printf("    Size (面积): %s㎡\n", params.Size)
					file.WriteString(fmt.Sprintf("    Size (面积): %s㎡\n", params.Size))
				}
			}
		}

		// 打印后处理筛选条件
		if len(transformResult.PostFilters) > 0 {
			fmt.Println("\n  后处理筛选条件:")
			file.WriteString("\n  后处理筛选条件:\n")
			for _, filter := range transformResult.PostFilters {
				fmt.Printf("    %s %s %.0f%s\n",
					filter.Field,
					schedule.OperatorToText(filter.Operator),
					filter.Value,
					filter.Unit)
				file.WriteString(fmt.Sprintf("    %s %s %.0f%s\n",
					filter.Field,
					schedule.OperatorToText(filter.Operator),
					filter.Value,
					filter.Unit))
			}
		}

		// 打印警告信息
		if len(transformResult.Warnings) > 0 {
			fmt.Println("\n  ⚠️ 转换警告:")
			file.WriteString("\n  ⚠️ 转换警告:\n")
			for _, warning := range transformResult.Warnings {
				fmt.Printf("    - %s\n", warning)
				file.WriteString(fmt.Sprintf("    - %s\n", warning))
			}
		}

		fmt.Println()
		file.WriteString("\n")
	}

	// 手动构造测试用例验证转换逻辑
	testManualConversion(file)

	// 输出测试总结
	fmt.Println("\n【转换测试总结】")
	file.WriteString("\n【转换测试总结】\n")
	fmt.Println("✅ 测试完成，展示了以下转换特性：")
	file.WriteString("✅ 测试完成，展示了以下转换特性：\n")
	fmt.Println("  1. 区域映射：Location.District → Area")
	file.WriteString("  1. 区域映射：Location.District → Area\n")
	fmt.Println("  2. 价格转换：Price → Amount（含操作符处理）")
	file.WriteString("  2. 价格转换：Price → Amount（含操作符处理）\n")
	fmt.Println("  3. 房间数映射：RoomLayout.Bedrooms → Count")
	file.WriteString("  3. 房间数映射：RoomLayout.Bedrooms → Count\n")
	fmt.Println("  4. 板块提取：Location.Landmark + InterestPoints → Plate")
	file.WriteString("  4. 板块提取：Location.Landmark + InterestPoints → Plate\n")
	fmt.Println("  5. 面积范围：Area → Size（添加容差）")
	file.WriteString("  5. 面积范围：Area → Size（添加容差）\n")
	fmt.Println("  6. 操作符处理：<=/>=转换为范围+PostFilter")
	file.WriteString("  6. 操作符处理：<=/>=转换为范围+PostFilter\n")
	fmt.Println("  7. 未映射维度：装修/朝向等记录为warnings")
	file.WriteString("  7. 未映射维度：装修/朝向等记录为warnings\n")
}

// testManualConversion 手动构造数据测试转换逻辑的完整性
func testManualConversion(file *os.File) {
	fmt.Println("\n【手动构造数据转换测试】")
	file.WriteString("\n【手动构造数据转换测试】\n")

	adapter := schedule.NewIpangAdapter()

	// 构造一个包含所有字段的完整测试用例
	result := &schedule.KeywordExtractionResult{
		Location: []schedule.LocationInfo{
			{Province: "上海", District: "浦东新区", Landmark: "陆家嘴"},
		},
		Price: []schedule.PriceInfo{
			{Min: 0, Max: 800, Operator: "<=", Unit: "万"},
		},
		RoomLayout: []schedule.RoomLayoutInfo{
			{Bedrooms: 3, LivingRooms: 2, Description: "3室2厅"},
		},
		Area: []schedule.AreaInfo{
			{Value: 150, Unit: "平米"},
		},
		InterestPoints: []string{"地铁站", "学区房", "徐家汇"},
		Decoration:     []string{"精装"},
		Orientation:    []string{"南北通透"},
	}

	fmt.Println("\n构造的Keywords数据：")
	file.WriteString("\n构造的Keywords数据：\n")
	printCompactResult(result, file)

	// 执行转换
	transformResult := adapter.Transform(result, nil)

	fmt.Println("\n🔄 转换后的Ipang API参数：")
	file.WriteString("\n🔄 转换后的Ipang API参数：\n")

	if transformResult.Params != nil {
		params := transformResult.Params
		if params.Area != "" {
			fmt.Printf("  Area: %s\n", params.Area)
			file.WriteString(fmt.Sprintf("  Area: %s\n", params.Area))
		}
		if params.Amount != "" {
			// 判断是否为范围
			if strings.Contains(params.Amount, ",") {
				parts := strings.Split(params.Amount, ",")
				fmt.Printf("  Amount: %s-%s万\n", parts[0], parts[1])
				file.WriteString(fmt.Sprintf("  Amount: %s-%s万\n", parts[0], parts[1]))
			} else {
				fmt.Printf("  Amount: %s万\n", params.Amount)
				file.WriteString(fmt.Sprintf("  Amount: %s万\n", params.Amount))
			}
		}
		if params.Count != "" {
			fmt.Printf("  Count: %s室\n", params.Count)
			file.WriteString(fmt.Sprintf("  Count: %s室\n", params.Count))
		}
		if params.Plate != "" {
			fmt.Printf("  Plate: %s\n", params.Plate)
			file.WriteString(fmt.Sprintf("  Plate: %s\n", params.Plate))
		}
		if params.Size != "" {
			// 判断是否为范围
			if strings.Contains(params.Size, ",") {
				parts := strings.Split(params.Size, ",")
				fmt.Printf("  Size: %s-%s㎡\n", parts[0], parts[1])
				file.WriteString(fmt.Sprintf("  Size: %s-%s㎡\n", parts[0], parts[1]))
			} else {
				fmt.Printf("  Size: %s㎡\n", params.Size)
				file.WriteString(fmt.Sprintf("  Size: %s㎡\n", params.Size))
			}
		}
	}

	if len(transformResult.PostFilters) > 0 {
		fmt.Println("\n后处理筛选：")
		file.WriteString("\n后处理筛选：\n")
		for _, filter := range transformResult.PostFilters {
			fmt.Printf("  %s %s %.0f%s\n", filter.Field, schedule.OperatorToText(filter.Operator), filter.Value, filter.Unit)
			file.WriteString(fmt.Sprintf("  %s %s %.0f%s\n", filter.Field, schedule.OperatorToText(filter.Operator), filter.Value, filter.Unit))
		}
	}

	if len(transformResult.Warnings) > 0 {
		fmt.Println("\n警告信息：")
		file.WriteString("\n警告信息：\n")
		for _, warning := range transformResult.Warnings {
			fmt.Printf("  - %s\n", warning)
			file.WriteString(fmt.Sprintf("  - %s\n", warning))
		}
	}
}

// printCompactResult 紧凑打印提取结果（用于转换测试）
func printCompactResult(result *schedule.KeywordExtractionResult, file *os.File) {
	if len(result.Location) > 0 {
		data := jsonStringNoEscape(result.Location)
		fmt.Printf("  location: %s\n", data)
		file.WriteString(fmt.Sprintf("  location: %s\n", data))
	}
	if len(result.Price) > 0 {
		data := jsonStringNoEscape(result.Price)
		fmt.Printf("  price: %s\n", data)
		file.WriteString(fmt.Sprintf("  price: %s\n", data))
	}
	if len(result.RoomLayout) > 0 {
		data := jsonStringNoEscape(result.RoomLayout)
		fmt.Printf("  room_layout: %s\n", data)
		file.WriteString(fmt.Sprintf("  room_layout: %s\n", data))
	}
	if len(result.Area) > 0 {
		data := jsonStringNoEscape(result.Area)
		fmt.Printf("  area: %s\n", data)
		file.WriteString(fmt.Sprintf("  area: %s\n", data))
	}
	if len(result.InterestPoints) > 0 {
		fmt.Printf("  interest_points: %v\n", result.InterestPoints)
		file.WriteString(fmt.Sprintf("  interest_points: %v\n", result.InterestPoints))
	}
	if len(result.Decoration) > 0 {
		fmt.Printf("  decoration: %v\n", result.Decoration)
		file.WriteString(fmt.Sprintf("  decoration: %v\n", result.Decoration))
	}
	if len(result.Orientation) > 0 {
		fmt.Printf("  orientation: %v\n", result.Orientation)
		file.WriteString(fmt.Sprintf("  orientation: %v\n", result.Orientation))
	}
}

// testRentalFilter 测试租售过滤功能
func testRentalFilter(file *os.File) {
	fmt.Println("\n【租售过滤测试】")
	file.WriteString("\n\n【租售过滤测试】\n\n")

	service := schedule.NewKeywordExtractorService(nil, "")

	testCases := []struct {
		name         string
		messages     []string
		shouldFilter bool
	}{
		{
			name:         "正常购房查询",
			messages:     []string{"我想在浦东" + getRandomPlate("浦东") + "买个三室两厅的房子"},
			shouldFilter: false,
		},
		{
			name:         "包含租字的查询",
			messages:     []string{"我想在" + getRandomPlateWithDistrict() + "租个一居室"},
			shouldFilter: true,
		},
		{
			name:         "包含出租的查询",
			messages:     []string{"找个" + getRandomPlateWithDistrict() + "的写字楼门面出租"},
			shouldFilter: true,
		},
		{
			name:         "包含出售的查询",
			messages:     []string{"我要出售一套房产"},
			shouldFilter: true,
		},
		{
			name:         "包含卖字的查询",
			messages:     []string{"我想卖" + getRandomPlateWithDistrict() + "的房子"},
			shouldFilter: true,
		},
		{
			name: "多语句中包含租房",
			messages: []string{
				"我想了解一下成都的房源",
				"租房，月租3000左右",
			},
			shouldFilter: true,
		},
	}

	successCount := 0
	for i, tc := range testCases {
		fmt.Printf("[%d] %s\n", i+1, tc.name)
		file.WriteString(fmt.Sprintf("[%d] %s\n", i+1, tc.name))

		// 构建消息历史
		history := make([]minimax.Message, 0)
		for _, msg := range tc.messages {
			history = append(history, minimax.Message{Role: "user", Content: msg})
		}

		// 提取关键词
		result, err := service.ExtractFromConversationHistory(history)
		if err != nil {
			fmt.Printf("   ❌ 错误: %v\n", err)
			file.WriteString(fmt.Sprintf("   ❌ 错误: %v\n", err))
			continue
		}

		// 计算维度数
		dimensionCount := 0
		if len(result.Location) > 0 {
			dimensionCount++
		}
		if len(result.Decoration) > 0 {
			dimensionCount++
		}
		if len(result.PropertyType) > 0 {
			dimensionCount++
		}
		if len(result.RoomLayout) > 0 {
			dimensionCount++
		}
		if len(result.Price) > 0 {
			dimensionCount++
		}
		if len(result.Area) > 0 {
			dimensionCount++
		}
		if len(result.InterestPoints) > 0 {
			dimensionCount++
		}
		if len(result.Commercial) > 0 {
			dimensionCount++
		}
		if len(result.Orientation) > 0 {
			dimensionCount++
		}

		isFiltered := (dimensionCount == 0)

		if isFiltered == tc.shouldFilter {
			fmt.Printf("   ✅ 通过: %d个维度\n", dimensionCount)
			file.WriteString(fmt.Sprintf("   ✅ 通过: %d个维度\n", dimensionCount))
			successCount++
		} else {
			fmt.Printf("   ❌ 失败: 期望过滤=%v, 实际过滤=%v\n", tc.shouldFilter, isFiltered)
			file.WriteString(fmt.Sprintf("   ❌ 失败: 期望过滤=%v, 实际过滤=%v\n", tc.shouldFilter, isFiltered))
		}
	}

	fmt.Printf("\n租售过滤测试: %d/%d 通过\n", successCount, len(testCases))
	file.WriteString(fmt.Sprintf("\n租售过滤测试: %d/%d 通过\n", successCount, len(testCases)))
}

// testRoomCountConversion 测试房型转换功能
func testRoomCountConversion(file *os.File) {
	fmt.Println("\n【房型转换测试】")
	file.WriteString("\n\n【房型转换测试】\n\n")

	adapter := schedule.NewIpangAdapter()

	testCases := []struct {
		name     string
		input    schedule.KeywordExtractionResult
		expected string
	}{
		{
			name: "1室1厅→2",
			input: schedule.KeywordExtractionResult{
				RoomLayout: []schedule.RoomLayoutInfo{
					{Bedrooms: 1, LivingRooms: 1, Description: "1室1厅"},
				},
			},
			expected: "2",
		},
		{
			name: "3室2厅→5",
			input: schedule.KeywordExtractionResult{
				RoomLayout: []schedule.RoomLayoutInfo{
					{Bedrooms: 3, LivingRooms: 2, Description: "3室2厅"},
				},
			},
			expected: "5",
		},
		{
			name: "2室1厅→3",
			input: schedule.KeywordExtractionResult{
				RoomLayout: []schedule.RoomLayoutInfo{
					{Bedrooms: 2, LivingRooms: 1, Description: "2室1厅"},
				},
			},
			expected: "3",
		},
		{
			name: "仅Description解析",
			input: schedule.KeywordExtractionResult{
				RoomLayout: []schedule.RoomLayoutInfo{
					{Description: "4室2厅"},
				},
			},
			expected: "6",
		},
	}

	successCount := 0
	for i, tc := range testCases {
		fmt.Printf("[%d] %s\n", i+1, tc.name)
		file.WriteString(fmt.Sprintf("[%d] %s\n", i+1, tc.name))

		// 转换
		result := adapter.Transform(&tc.input, nil)

		if result.Params.Count == tc.expected {
			fmt.Printf("   ✅ 通过: Count=%s\n", result.Params.Count)
			file.WriteString(fmt.Sprintf("   ✅ 通过: Count=%s\n", result.Params.Count))
			successCount++
		} else {
			fmt.Printf("   ❌ 失败: 得到=%s, 期望=%s\n", result.Params.Count, tc.expected)
			file.WriteString(fmt.Sprintf("   ❌ 失败: 得到=%s, 期望=%s\n", result.Params.Count, tc.expected))
		}
	}

	fmt.Printf("\n房型转换测试: %d/%d 通过\n", successCount, len(testCases))
	file.WriteString(fmt.Sprintf("\n房型转换测试: %d/%d 通过\n", successCount, len(testCases)))
}
