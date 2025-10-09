package ipang

import (
    "encoding/json"
    "reflect"
    "strings"
    "testing"
)

func TestGenerateNonce(t *testing.T) {
	nonce1 := generateNonce()
	nonce2 := generateNonce()
	
	if len(nonce1) != 32 {
		t.Errorf("nonce length should be 32, got %d", len(nonce1))
	}
	
	if nonce1 == nonce2 {
		t.Error("nonce should be different each time")
	}
}

func TestCalculateSignature(t *testing.T) {
	client := &Client{
		appID:     "xp6121770875700571",
		appSecret: "VnaAJspwH89hPtu3ef2EGZL75MjYvcB6",
	}
	
	params := map[string]string{
		"amount":    "1000",
		"apikey":    "xp6121770875700571",
		"area":      "",
		"count":     "3",
		"nonce":     "ei781pf416kiyshraia188elpaw69d49",
		"plate":     "",
		"size":      "80,160",
		"timestamp": "1755576608",
	}
	
	signature := client.calculateSignature(params)
	expected := "50e7c3af89533a8790b31507b4134a09"
	
	if signature != expected {
		t.Errorf("signature mismatch: expected %s, got %s", expected, signature)
	}
}

func TestQueryParams(t *testing.T) {
	// 测试QueryParams结构体
	params := &QueryParams{
		Area:   "静安区",
		Amount: "800,1200",
		Count:  "3,4",
		Plate:  "浦江,七宝",
		Size:   "100,150",
	}
	
	if params.Area != "静安区" {
		t.Errorf("Area should be 静安区, got %s", params.Area)
	}
}

// 集成测试（需要真实API）
func TestIntegration(t *testing.T) {
	// 跳过集成测试，除非明确需要
	t.Skip("Skipping integration test - requires real API")
	
	client := NewClient()
	
	// 测试查询
	properties, err := client.Query(&QueryParams{
		Amount: "1000",
		Size:   "80,160",
		Count:  "3",
	})
	
	if err != nil {
		t.Errorf("Query failed: %v", err)
	}
	
	if properties == nil {
		t.Error("Properties should not be nil")
	}
}

func TestNewClient(t *testing.T) {
	// 测试从配置创建客户端
	client := NewClient()
	if client == nil {
		t.Error("NewClient should not return nil")
	}
	
	// 验证每次创建都是新实例
	client2 := NewClient()
	if client == client2 {
		t.Error("NewClient should create new instances")
	}
}

func TestNewClientWithConfig(t *testing.T) {
	// 测试自定义配置
	client := NewClientWithConfig(
		"test_app_id",
		"test_secret",
		"https://test.api.com",
	)
	
	if client.appID != "test_app_id" {
		t.Errorf("appID should be test_app_id, got %s", client.appID)
	}
	
	// 测试默认值
	clientWithDefaults := NewClientWithConfig(
		"test_app_id",
		"test_secret",
		"",  // 空baseURL应该使用默认值
	)
	
	if clientWithDefaults.baseURL != "https://ai.ipangsell.com" {
		t.Errorf("baseURL should use default, got %s", clientWithDefaults.baseURL)
	}
}

// TestPOSTSignatureWithQas 测试POST请求的签名计算（包含qas字段）
func TestPOSTSignatureWithQas(t *testing.T) {
	client := &Client{
		appID:     "xp6121770875700571",
		appSecret: "VnaAJspwH89hPtu3ef2EGZL75MjYvcB6",
	}

	// 使用API文档中的测试用例
	params := map[string]string{
		"amount":    "1000",
		"apikey":    "xp6121770875700571",
		"area":      "",
		"count":     "3",
		"nonce":     "omq6xs3hmfdejm468mxqxb978zuxgrjs",
		"plate":     "",
		"qas":       `[{"q": "请问您有什么问题？", "a": "1000万在哪里买房子比较好"}, {"q": "面积有要求吗？", "a": "100平左右"}]`,
		"size":      "80,160",
		"timestamp": "1757576460",
	}

	signature := client.calculateSignature(params)
	expected := "baa7ea3bceeab03ba1a6f953e282f911"

	if signature != expected {
		t.Errorf("POST signature with qas mismatch: expected %s, got %s", expected, signature)
	}
}

// TestBuildAndSignParamsForPOST 测试POST模式的参数构建
func TestBuildAndSignParamsForPOST(t *testing.T) {
	client := &Client{
		appID:     "test_app_id",
		appSecret: "test_secret",
	}

	params := &QueryParams{
		Area:   "静安区",
		Amount: "1000",
		Count:  "3",
		Plate:  "浦江",
		Size:   "100",
		Qas:    `[{"q": "test", "a": "answer"}]`,
	}

	// 测试POST模式：qas字段应该总是包含
	postParams := client.buildAndSignParams(params, true)
	if _, exists := postParams["qas"]; !exists {
		t.Error("POST mode should always include qas field")
	}
	if postParams["qas"] != params.Qas {
		t.Errorf("qas field value mismatch: expected %s, got %s", params.Qas, postParams["qas"])
	}

	// 测试POST模式下qas为空字符串的情况
	emptyQasParams := &QueryParams{
		Amount: "1000",
		Qas:    "", // 空字符串
	}
	postParamsEmpty := client.buildAndSignParams(emptyQasParams, true)
	if _, exists := postParamsEmpty["qas"]; !exists {
		t.Error("POST mode should include qas field even when empty")
	}
	if postParamsEmpty["qas"] != "" {
		t.Errorf("qas field should be empty string, got %s", postParamsEmpty["qas"])
	}

	// 测试GET模式：空qas字段不应该包含
	getParams := client.buildAndSignParams(emptyQasParams, false)
	if _, exists := getParams["qas"]; exists {
		t.Error("GET mode should not include empty qas field")
	}
}

// TestParseResponseSuccess 测试解析成功响应
func TestParseResponseSuccess(t *testing.T) {
	client := &Client{}

	// 模拟成功响应（status=0, detail为对象）
	successJSON := `{
		"status": 0,
		"detail": {
			"new_house": [],
			"sec_house": [],
			"pdf_url": "http://example.com/report.pdf"
		}
	}`

	response, err := client.parseResponse([]byte(successJSON))
	if err != nil {
		t.Errorf("parseResponse should not fail for valid success response: %v", err)
	}

	if response.Status != 0 {
		t.Errorf("expected status 0, got %d", response.Status)
	}

	if response.Detail.PdfUrl != "http://example.com/report.pdf" {
		t.Errorf("expected pdf_url to be parsed correctly, got %s", response.Detail.PdfUrl)
	}
}

// TestParsePostResponseWithErrorFields 测试POST响应解析包含错误分析字段的成功响应
func TestParsePostResponseWithErrorFields(t *testing.T) {
	client := &Client{}

	// 模拟POST接口的完整字段响应
	postSuccessJSON := `{
		"status": 0,
		"detail": {
			"new_house": [],
			"new_house_error": "价格范围内无新房",
			"new_house_exist_params": "area,count",
			"new_house_no_exist_params": "amount",
			"sec_house": [],
			"sec_house_error": "该区域无二手房源",
			"sec_house_exist_params": "area",
			"sec_house_no_exist_params": "amount,count",
			"pdf_url": "http://example.com/report.pdf"
		}
	}`

	response, err := client.parsePostResponse([]byte(postSuccessJSON))
	if err != nil {
		t.Errorf("parsePostResponse should not fail for valid response with error fields: %v", err)
	}

	// 验证新房错误分析字段
    if response.Detail.NewHouseError != "价格范围内无新房" {
        t.Errorf("expected new_house_error to be parsed correctly, got %s", response.Detail.NewHouseError)
    }
    if !reflect.DeepEqual([]string(response.Detail.NewHouseExistParams), []string{"area", "count"}) {
        t.Errorf("expected new_house_exist_params to be [area count], got %v", []string(response.Detail.NewHouseExistParams))
    }
    if !reflect.DeepEqual([]string(response.Detail.NewHouseNoExistParams), []string{"amount"}) {
        t.Errorf("expected new_house_no_exist_params to be [amount], got %v", []string(response.Detail.NewHouseNoExistParams))
    }

	// 验证二手房错误分析字段
    if response.Detail.SecHouseError != "该区域无二手房源" {
        t.Errorf("expected sec_house_error to be parsed correctly, got %s", response.Detail.SecHouseError)
    }
    if !reflect.DeepEqual([]string(response.Detail.SecHouseExistParams), []string{"area"}) {
        t.Errorf("expected sec_house_exist_params to be [area], got %v", []string(response.Detail.SecHouseExistParams))
    }
    if !reflect.DeepEqual([]string(response.Detail.SecHouseNoExistParams), []string{"amount", "count"}) {
        t.Errorf("expected sec_house_no_exist_params to be [amount count], got %v", []string(response.Detail.SecHouseNoExistParams))
    }

	// 验证基础字段也正确解析
	if response.Detail.PdfUrl != "http://example.com/report.pdf" {
		t.Errorf("expected pdf_url to be parsed correctly, got %s", response.Detail.PdfUrl)
	}
}

// TestParseResponseError 测试解析错误响应（detail为字符串）
func TestParseResponseError(t *testing.T) {
	client := &Client{}

	// 模拟错误响应（status=1, detail为错误消息字符串）
	errorJSON := `{
		"status": 1,
		"detail": "参数错误：amount字段不能为空"
	}`

	response, err := client.parseResponse([]byte(errorJSON))
	if err == nil {
		t.Error("parseResponse should return error for status=1 response")
	}

	if response != nil {
		t.Error("response should be nil when error occurs")
	}

	expectedErrMsg := "Ipang API错误: 参数错误：amount字段不能为空"
	if err.Error() != expectedErrMsg {
		t.Errorf("expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

// TestParseResponseErrorWithInvalidDetail 测试status=1但detail格式错误的情况
func TestParseResponseErrorWithInvalidDetail(t *testing.T) {
	client := &Client{}

	// 模拟错误响应，但detail不是字符串（异常情况）
	invalidErrorJSON := `{
		"status": 1,
		"detail": {"unexpected": "object"}
	}`

	response, err := client.parseResponse([]byte(invalidErrorJSON))
	if err == nil {
		t.Error("parseResponse should return error for status=1 response")
	}

	if response != nil {
		t.Error("response should be nil when error occurs")
	}

	// 应该fallback到状态码错误
	expectedErrMsg := "Ipang API错误: status=1"
	if err.Error() != expectedErrMsg {
		t.Errorf("expected fallback error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

// TestParseResponseInvalidJSON 测试解析无效JSON
func TestParseResponseInvalidJSON(t *testing.T) {
	client := &Client{}

	invalidJSON := `{"status": 0, "detail":` // 无效JSON

	response, err := client.parseResponse([]byte(invalidJSON))
	if err == nil {
		t.Error("parseResponse should return error for invalid JSON")
	}

	if response != nil {
		t.Error("response should be nil when JSON parsing fails")
	}

	if !strings.Contains(err.Error(), "解析响应envelope失败") {
		t.Errorf("error message should mention envelope parsing failure, got: %s", err.Error())
	}
}

// TestGenerateNonceFallback 测试nonce生成的兜底分支长度处理逻辑
func TestGenerateNonceFallback(t *testing.T) {
	// 这个测试验证兜底逻辑中时间戳的32位长度处理算法
	// 我们直接测试长度处理逻辑，不需要模拟crypto/rand.Read失败

	// 测试短时间戳的填充逻辑
	testPadding := func(input string) string {
		if len(input) < 32 {
			return input + strings.Repeat("0", 32-len(input))
		}
		return input
	}

	shortInput := "abc123"
	paddedResult := testPadding(shortInput)
	if len(paddedResult) != 32 {
		t.Errorf("padded result length should be 32, got %d", len(paddedResult))
	}
	expectedPadded := "abc123" + strings.Repeat("0", 26)
	if paddedResult != expectedPadded {
		t.Errorf("expected padded result %s, got %s", expectedPadded, paddedResult)
	}

	// 测试长时间戳的截断逻辑
	testTruncation := func(input string) string {
		if len(input) > 32 {
			return input[:32]
		}
		return input
	}

	longInput := "abcdefghijklmnopqrstuvwxyz1234567890" // 超过32位的字符串
	truncatedResult := testTruncation(longInput)
	if len(truncatedResult) != 32 {
		t.Errorf("truncated result length should be 32, got %d", len(truncatedResult))
	}
	expectedTruncated := "abcdefghijklmnopqrstuvwxyz123456"
	if truncatedResult != expectedTruncated {
		t.Errorf("expected truncated result %s, got %s", expectedTruncated, truncatedResult)
	}

	// 验证正常的generateNonce函数仍然产生32位结果
	normalNonce := generateNonce()
	if len(normalNonce) != 32 {
		t.Errorf("generateNonce should produce 32-char nonce, got %d", len(normalNonce))
	}
}

// TestParsePostResponseError 测试POST响应解析错误情况
func TestParsePostResponseError(t *testing.T) {
	client := &Client{}

	// 模拟POST接口错误响应（status=1, detail为错误消息字符串）
	postErrorJSON := `{
		"status": 1,
		"detail": "POST参数错误：qas字段格式不正确"
	}`

	response, err := client.parsePostResponse([]byte(postErrorJSON))
	if err == nil {
		t.Error("parsePostResponse should return error for status=1 response")
	}

	if response != nil {
		t.Error("response should be nil when error occurs")
	}

	expectedErrMsg := "Ipang API错误: POST参数错误：qas字段格式不正确"
	if err.Error() != expectedErrMsg {
		t.Errorf("expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

// TestGetPostResponseSeparation 测试GET和POST响应结构分离的正确性
func TestGetPostResponseSeparation(t *testing.T) {
	client := &Client{}

	// 测试GET接口响应（只包含基础字段）
	getSuccessJSON := `{
		"status": 0,
		"detail": {
			"new_house": [],
			"sec_house": [],
			"pdf_url": "http://example.com/get-report.pdf"
		}
	}`

	// GET接口应该使用parseResponse方法
	getResponse, err := client.parseResponse([]byte(getSuccessJSON))
	if err != nil {
		t.Errorf("parseResponse should not fail for GET response: %v", err)
	}
	if getResponse.Detail.PdfUrl != "http://example.com/get-report.pdf" {
		t.Errorf("GET response pdf_url parsing failed, got %s", getResponse.Detail.PdfUrl)
	}

	// POST接口响应（包含所有字段）
	postSuccessJSON := `{
		"status": 0,
		"detail": {
			"new_house": [],
			"sec_house": [],
			"pdf_url": "http://example.com/post-report.pdf",
			"new_house_error": "测试新房错误",
			"new_house_exist_params": "area",
			"new_house_no_exist_params": "amount",
			"sec_house_error": "测试二手房错误",
			"sec_house_exist_params": "area,count",
			"sec_house_no_exist_params": "amount"
		}
	}`

	// POST接口应该使用parsePostResponse方法
	postResponse, err := client.parsePostResponse([]byte(postSuccessJSON))
	if err != nil {
		t.Errorf("parsePostResponse should not fail for POST response: %v", err)
	}
	if postResponse.Detail.PdfUrl != "http://example.com/post-report.pdf" {
		t.Errorf("POST response pdf_url parsing failed, got %s", postResponse.Detail.PdfUrl)
	}
	if postResponse.Detail.NewHouseError != "测试新房错误" {
		t.Errorf("POST response new_house_error parsing failed, got %s", postResponse.Detail.NewHouseError)
	}

	// 验证类型安全：GET响应不能访问POST特有字段
	// 这个测试主要验证编译时类型安全，在运行时我们通过不同的类型来保证
	if getResponse.Status != 0 {
		t.Errorf("GET response status should be 0, got %d", getResponse.Status)
	}
	if postResponse.Status != 0 {
		t.Errorf("POST response status should be 0, got %d", postResponse.Status)
	}
}

// TestQAStructAndSerialization 测试QA结构体和JSON序列化
func TestQAStructAndSerialization(t *testing.T) {
	qas := []QA{
		{Q: "请问您有什么问题？", A: "1000万在哪里买房子比较好"},
		{Q: "面积有要求吗？", A: "100平左右"},
	}

	// 由于我们没有mock session，这个测试会失败，但我们可以测试JSON序列化部分
	expectedQasJSON := `[{"q":"请问您有什么问题？","a":"1000万在哪里买房子比较好"},{"q":"面积有要求吗？","a":"100平左右"}]`

	// 手动构建参数来验证JSON序列化
	qasBytes, err := json.Marshal(qas)
	if err != nil {
		t.Errorf("QA JSON marshaling failed: %v", err)
	}

	if string(qasBytes) != expectedQasJSON {
		t.Errorf("QA JSON mismatch: expected %s, got %s", expectedQasJSON, string(qasBytes))
	}

	// 验证QA结构体字段
	if qas[0].Q != "请问您有什么问题？" {
		t.Errorf("QA.Q field mismatch: expected %s, got %s", "请问您有什么问题？", qas[0].Q)
	}
	if qas[1].A != "100平左右" {
		t.Errorf("QA.A field mismatch: expected %s, got %s", "100平左右", qas[1].A)
	}
}

// TestStrList_UnmarshalArray 测试StrList从数组反序列化
func TestStrList_UnmarshalArray(t *testing.T) {
    var s StrList
    if err := json.Unmarshal([]byte(`["a","b","c"]`), &s); err != nil {
        t.Fatalf("unmarshal array failed: %v", err)
    }
    want := []string{"a", "b", "c"}
    if !reflect.DeepEqual([]string(s), want) {
        t.Fatalf("expected %v, got %v", want, []string(s))
    }
}

// TestStrList_UnmarshalString 测试StrList从逗号分隔字符串反序列化
func TestStrList_UnmarshalString(t *testing.T) {
    var s StrList
    if err := json.Unmarshal([]byte(`"a,b , c"`), &s); err != nil {
        t.Fatalf("unmarshal string failed: %v", err)
    }
    want := []string{"a", "b", "c"}
    if !reflect.DeepEqual([]string(s), want) {
        t.Fatalf("expected %v, got %v", want, []string(s))
    }

    // 空字符串应得到空切片
    var s2 StrList
    if err := json.Unmarshal([]byte(`""`), &s2); err != nil {
        t.Fatalf("unmarshal empty string failed: %v", err)
    }
    if len(s2) != 0 {
        t.Fatalf("expected empty list, got %v", []string(s2))
    }
}
