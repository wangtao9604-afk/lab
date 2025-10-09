//go:build ignore
// +build ignore

package main

import (
	"encoding/json"
	"fmt"

	"qywx/infrastructures/ipang"
)

func main() {
	fmt.Println("测试Ipang真实API调用")
	fmt.Println("====================")

	// 创建客户端
	client := ipang.NewClient()

	// 测试查询：总价1000万附近、面积80-160㎡的3房楼盘
	params := &ipang.QueryParams{
		Area:   "",       // 不限区域
		Plate:  "",       // 不限板块
		Amount: "1000",   // 1000万
		Size:   "80,160", // 80-160平米
		Count:  "3",      // 3房
	}

	fmt.Printf("查询参数: %+v\n", params)
	fmt.Println("正在调用Ipang API...")

	// 调用API
	response, err := client.Query(params)
	if err != nil {
		fmt.Printf("API调用失败: %v\n", err)
		return
	}

	fmt.Printf("\n查询成功！返回结果:\n")
	fmt.Printf("- 新房数量: %d\n", len(response.Detail.NewHouse))
	fmt.Printf("- 二手房数量: %d\n", len(response.Detail.SecHouse))

	// 打印前3个新房
	if len(response.Detail.NewHouse) > 0 {
		fmt.Println("\n【新房列表】")
		for i, house := range response.Detail.NewHouse {
			if i >= 3 {
				break
			}
			fmt.Printf("%d. %s (%s %s)\n", i+1, house.Title, house.Area, house.Plate)
			for j, item := range house.List {
				if j >= 2 {
					break
				}
				fmt.Printf("   - %s: %v㎡, %v万, %s\n",
					item.PanTitle, item.SizeRange, item.Amount, item.HouseType)
			}
		}
	}

	// 打印前3个二手房
	if len(response.Detail.SecHouse) > 0 {
		fmt.Println("\n【二手房列表】")
		for i, house := range response.Detail.SecHouse {
			if i >= 3 {
				break
			}
			fmt.Printf("%d. %s (%s %s)\n", i+1, house.Title, house.Area, house.Plate)
			for j, item := range house.List {
				if j >= 2 {
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
				fmt.Printf("   - %s㎡, %s万, %s\n", sizeStr, amountStr, item.HouseType)
			}
		}
	}

	// 输出PDF链接
	if response.Detail.PdfUrl != "" {
		fmt.Printf("\nPDF报告: %s\n", response.Detail.PdfUrl)
	}

	// 输出原始JSON（调试用）
	fmt.Println("\n原始JSON响应:")
	jsonBytes, _ := json.MarshalIndent(response, "", "  ")
	// 只输出前1000个字符
	jsonStr := string(jsonBytes)
	if len(jsonStr) > 1000 {
		jsonStr = jsonStr[:1000] + "..."
	}
	fmt.Println(jsonStr)
}
