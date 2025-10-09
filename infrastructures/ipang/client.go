package ipang

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"qywx/infrastructures/config"
	"qywx/infrastructures/httplib"
	"qywx/infrastructures/log"
)

const (
	// queryPath API查询路径
	queryPath = "/product/mall/query"
)

// Client Ipang API客户端
//
// 并发安全说明：
// - Query方法：并发安全，可多goroutine同时调用
// - PostQuery方法：并发安全，使用临时session副本避免header竞争
// - 其他SearchBy*方法：并发安全，内部调用Query方法
// - Client实例可在多goroutine间安全共享使用
type Client struct {
	appID     string
	appSecret string
	baseURL   string
	session   *httplib.Session
}

// QueryParams 查询参数
type QueryParams struct {
	Area   string // 区域，如：普陀区、静安区等
	Amount string // 总价（万），单个值：1000，或者范围值：800,1000
	Count  string // 房间数，单个值：3，多个值：3,4,5
	Plate  string // 板块，如：浦江,七宝
	Size   string // 面积（㎡），单个值：100，或者范围值：80,100
	Qas    string // 问答列表，JSON字符串，如：'[{"q": "请问您有什么问题？", "a": "1000万在哪里买房子比较好"}]'
}

// HouseItem 房产列表项
type HouseItem struct {
	// 新房字段
	PanID      int         `json:"pan_id,omitempty"`
	PanTitle   string      `json:"pan_title,omitempty"`
	SizeID     int         `json:"size_id,omitempty"`
	HouseTitle string      `json:"house_title,omitempty"`
	SizeRange  interface{} `json:"size_range"` // 新房是string，二手房是float64
	Amount     interface{} `json:"amount"`     // 新房是string，二手房是float64
	HouseType  string      `json:"house_type"`
}

// House 房产信息
type House struct {
	ID    interface{} `json:"id"` // 新房是int，二手房是string
	Title string      `json:"title"`
	Area  string      `json:"area"`
	Plate string      `json:"plate"`
	List  []HouseItem `json:"list"`
}

// BaseQueryDetail 基础查询详情（GET接口使用）
type BaseQueryDetail struct {
	NewHouse []House `json:"new_house"`
	SecHouse []House `json:"sec_house"`
	PdfUrl   string  `json:"pdf_url"`
}

// PostQueryDetail POST查询详情（包含错误分析字段）
type PostQueryDetail struct {
    BaseQueryDetail                                                        // 继承基础字段
    NewHouseError           string `json:"new_house_error"`           // 新房查询不出结果的原因
    NewHouseExistParams     StrList `json:"new_house_exist_params"`    // 新房已查询出结果的参数字段（可能为数组或逗号分隔字符串）
    NewHouseNoExistParams   StrList `json:"new_house_no_exist_params"` // 新房查询不出结果的字段（可能为数组或逗号分隔字符串）
    SecHouseError           string `json:"sec_house_error"`           // 二手房查询不出结果的原因
    SecHouseExistParams     StrList `json:"sec_house_exist_params"`    // 二手房已查询出结果的参数字段（可能为数组或逗号分隔字符串）
    SecHouseNoExistParams   StrList `json:"sec_house_no_exist_params"` // 二手房查询不出结果的字段（可能为数组或逗号分隔字符串）
}

// StrList 兼容字符串或字符串数组的JSON字段。
// 允许服务端既返回 ["a","b"]，也返回 "a,b"。
type StrList []string

func (s *StrList) UnmarshalJSON(b []byte) error {
    // 如果是数组，直接解
    if len(b) > 0 && b[0] == '[' {
        var arr []string
        if err := json.Unmarshal(b, &arr); err != nil {
            return err
        }
        *s = arr
        return nil
    }
    // 否则按字符串处理
    var str string
    if err := json.Unmarshal(b, &str); err != nil {
        return err
    }
    if str == "" {
        *s = []string{}
        return nil
    }
    // 逗号分隔，去掉两端空格
    parts := strings.Split(str, ",")
    out := make([]string, 0, len(parts))
    for _, p := range parts {
        p = strings.TrimSpace(p)
        if p != "" {
            out = append(out, p)
        }
    }
    *s = out
    return nil
}

// QueryDetail 为了向后兼容，保留原名称（GET接口使用）
type QueryDetail = BaseQueryDetail

// QueryResponse 查询响应（GET接口使用）
type QueryResponse struct {
	Status int         `json:"status"`
	Detail QueryDetail `json:"detail"`
}

// PostQueryResponse POST查询响应
type PostQueryResponse struct {
	Status int             `json:"status"`
	Detail PostQueryDetail `json:"detail"`
}

// queryEnvelope 用于解包可能是字符串或对象的detail字段
type queryEnvelope struct {
	Status int             `json:"status"`
	Detail json.RawMessage `json:"detail"`
}

// NewClient 创建新的Ipang客户端实例
func NewClient() *Client {
	cfg := config.GetInstance()
	return NewClientWithConfig(
		cfg.IpangConfig.AppID,
		cfg.IpangConfig.AppSecret,
		cfg.IpangConfig.BaseURL,
	)
}

// NewClientWithConfig 使用指定配置创建客户端实例
func NewClientWithConfig(appID, appSecret, baseURL string) *Client {
	// 设置默认值
	if baseURL == "" {
		baseURL = "https://ai.ipangsell.com"
	}

	return &Client{
		appID:     appID,
		appSecret: appSecret,
		baseURL:   baseURL,
		session:   httplib.NewSession(),
	}
}

// generateNonce 生成32位随机字符串
func generateNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 如果随机数生成失败，使用时间戳作为备用方案，确保32位长度
		timestamp := fmt.Sprintf("%x", time.Now().UnixNano())
		// 如果长度不足32位，用0填充；如果超过32位，截断
		if len(timestamp) < 32 {
			timestamp = timestamp + strings.Repeat("0", 32-len(timestamp))
		} else if len(timestamp) > 32 {
			timestamp = timestamp[:32]
		}
		return timestamp
	}
	return hex.EncodeToString(b)
}

// buildAndSignParams 构建请求参数并计算签名
func (c *Client) buildAndSignParams(params *QueryParams, isPost bool) map[string]string {
	// 构建请求参数（严格按字母顺序）
	reqParams := make(map[string]string)
	reqParams["amount"] = params.Amount
	reqParams["apikey"] = c.appID
	reqParams["area"] = params.Area
	reqParams["count"] = params.Count
	reqParams["nonce"] = generateNonce()
	reqParams["plate"] = params.Plate

	// POST请求始终包含qas字段（即使为空），确保签名一致性
	if isPost {
		reqParams["qas"] = params.Qas // 可能为空字符串，但仍参与签名
	} else if params.Qas != "" {
		reqParams["qas"] = params.Qas // GET请求只在非空时包含
	}

	reqParams["size"] = params.Size
	reqParams["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)

	// 计算签名
	reqParams["signature"] = c.calculateSignature(reqParams)
	return reqParams
}

// calculateSignature 计算签名
func (c *Client) calculateSignature(params map[string]string) string {
	// 1. 对参数按字母顺序排序（包括空参数）
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 2. 拼接参数（包括空参数）
	var parts []string
	for _, k := range keys {
		v := params[k]
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	paramStr := strings.Join(parts, "&")

	// 3. 拼接app_secret
	signStr := paramStr + "&" + c.appSecret

	// 4. 计算MD5
	h := md5.New()
	h.Write([]byte(signStr))
	signature := hex.EncodeToString(h.Sum(nil))

	// 安全日志：不泄露密钥
	maskedStr := paramStr + "&" + strings.Repeat("*", len(c.appSecret))
	log.GetInstance().Sugar.Debugf("Sign string (masked): %s", maskedStr)
	log.GetInstance().Sugar.Debugf("Signature: %s", signature)

	return signature
}

// parseResponse 解析GET API响应，处理detail字段的联合类型
func (c *Client) parseResponse(respBytes []byte) (*QueryResponse, error) {
	// 先解包到中间结构体
	var envelope queryEnvelope
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		log.GetInstance().Sugar.Errorf("Failed to parse envelope: %v, body: %s", err, string(respBytes))
		return nil, fmt.Errorf("解析响应envelope失败: %w", err)
	}

	// 检查响应状态
	if envelope.Status != 0 {
		// status != 0 时，detail为错误消息字符串
		var errMsg string
		if err := json.Unmarshal(envelope.Detail, &errMsg); err != nil {
			// 如果无法解析为字符串，直接返回状态码错误
			log.GetInstance().Sugar.Errorf("Ipang API returned error: status=%d, detail parse failed", envelope.Status)
			return nil, fmt.Errorf("Ipang API错误: status=%d", envelope.Status)
		}
		log.GetInstance().Sugar.Errorf("Ipang API returned error: status=%d, message=%s", envelope.Status, errMsg)
		return nil, fmt.Errorf("Ipang API错误: %s", errMsg)
	}

	// status == 0 时，detail为QueryDetail对象
	var detail QueryDetail
	if err := json.Unmarshal(envelope.Detail, &detail); err != nil {
		log.GetInstance().Sugar.Errorf("Failed to parse QueryDetail: %v, body: %s", err, string(respBytes))
		return nil, fmt.Errorf("解析QueryDetail失败: %w", err)
	}

	return &QueryResponse{
		Status: envelope.Status,
		Detail: detail,
	}, nil
}

// parsePostResponse 解析POST API响应，处理detail字段的联合类型和扩展字段
func (c *Client) parsePostResponse(respBytes []byte) (*PostQueryResponse, error) {
	// 先解包到中间结构体
	var envelope queryEnvelope
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		log.GetInstance().Sugar.Errorf("Failed to parse envelope: %v, body: %s", err, string(respBytes))
		return nil, fmt.Errorf("解析响应envelope失败: %w", err)
	}

	// 检查响应状态
	if envelope.Status != 0 {
		// status != 0 时，detail为错误消息字符串
		var errMsg string
		if err := json.Unmarshal(envelope.Detail, &errMsg); err != nil {
			// 如果无法解析为字符串，直接返回状态码错误
			log.GetInstance().Sugar.Errorf("Ipang API returned error: status=%d, detail parse failed", envelope.Status)
			return nil, fmt.Errorf("Ipang API错误: status=%d", envelope.Status)
		}
		log.GetInstance().Sugar.Errorf("Ipang API returned error: status=%d, message=%s", envelope.Status, errMsg)
		return nil, fmt.Errorf("Ipang API错误: %s", errMsg)
	}

	// status == 0 时，detail为PostQueryDetail对象
	var detail PostQueryDetail
	if err := json.Unmarshal(envelope.Detail, &detail); err != nil {
		log.GetInstance().Sugar.Errorf("Failed to parse PostQueryDetail: %v, body: %s", err, string(respBytes))
		return nil, fmt.Errorf("解析PostQueryDetail失败: %w", err)
	}

	return &PostQueryResponse{
		Status: envelope.Status,
		Detail: detail,
	}, nil
}

// Query 查询房产信息
func (c *Client) Query(params *QueryParams) (*QueryResponse, error) {
	// 如果没有提供参数，使用空结构体
	if params == nil {
		params = &QueryParams{}
	}

	// 构建和签名参数
	reqParams := c.buildAndSignParams(params, false)

	// 构建URL - 动态拼接参数并进行URL编码
	keys := make([]string, 0, len(reqParams))
	for k := range reqParams {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 按字母顺序排序，确保URL对于相同参数总是相同的

	var urlParams []string
	for _, k := range keys {
		urlParams = append(urlParams, fmt.Sprintf("%s=%s", k, url.QueryEscape(reqParams[k])))
	}
	fullURL := fmt.Sprintf("%s%s?%s", c.baseURL, queryPath, strings.Join(urlParams, "&"))

	// 发送请求
	log.GetInstance().Sugar.Infof("Requesting Ipang API: %s", fullURL)

	respBytes, err := c.session.Get(fullURL)
	if err != nil {
		log.GetInstance().Sugar.Errorf("Failed to request Ipang API: %v", err)
		return nil, fmt.Errorf("请求Ipang API失败: %w", err)
	}

	// 解析响应
	response, err := c.parseResponse(respBytes)
	if err != nil {
		return nil, err
	}

	// 统计房产数量
	totalCount := 0
	for _, house := range response.Detail.NewHouse {
		totalCount += len(house.List)
	}
	for _, house := range response.Detail.SecHouse {
		totalCount += len(house.List)
	}
	log.GetInstance().Sugar.Infof("Ipang API returned %d new houses, %d sec houses, total %d items",
		len(response.Detail.NewHouse), len(response.Detail.SecHouse), totalCount)
	return response, nil
}

// SearchByPrice 按价格搜索
func (c *Client) SearchByPrice(minPrice, maxPrice int) (*QueryResponse, error) {
	var amount string
	if minPrice > 0 && maxPrice > 0 {
		amount = fmt.Sprintf("%d,%d", minPrice, maxPrice)
	} else if minPrice > 0 {
		amount = strconv.Itoa(minPrice)
	} else if maxPrice > 0 {
		amount = strconv.Itoa(maxPrice)
	}

	return c.Query(&QueryParams{
		Amount: amount,
	})
}

// SearchBySize 按面积搜索
func (c *Client) SearchBySize(minSize, maxSize int) (*QueryResponse, error) {
	var size string
	if minSize > 0 && maxSize > 0 {
		size = fmt.Sprintf("%d,%d", minSize, maxSize)
	} else if minSize > 0 {
		size = strconv.Itoa(minSize)
	} else if maxSize > 0 {
		size = strconv.Itoa(maxSize)
	}

	return c.Query(&QueryParams{
		Size: size,
	})
}

// SearchByRooms 按房间数搜索
func (c *Client) SearchByRooms(rooms ...int) (*QueryResponse, error) {
	if len(rooms) == 0 {
		return nil, fmt.Errorf("至少需要指定一个房间数")
	}

	roomStrs := make([]string, len(rooms))
	for i, room := range rooms {
		roomStrs[i] = strconv.Itoa(room)
	}

	return c.Query(&QueryParams{
		Count: strings.Join(roomStrs, ","),
	})
}

// SearchByArea 按区域搜索
func (c *Client) SearchByArea(area string) (*QueryResponse, error) {
	return c.Query(&QueryParams{
		Area: area,
	})
}

// SearchByPlate 按板块搜索
func (c *Client) SearchByPlate(plates ...string) (*QueryResponse, error) {
	if len(plates) == 0 {
		return nil, fmt.Errorf("至少需要指定一个板块")
	}

	return c.Query(&QueryParams{
		Plate: strings.Join(plates, ","),
	})
}

// PostQuery 发送POST请求查询房产信息（支持问答式查询）
func (c *Client) PostQuery(params *QueryParams) (*PostQueryResponse, error) {
	// 如果没有提供参数，使用空结构体
	if params == nil {
		params = &QueryParams{}
	}

	// 构建和签名参数
	reqParams := c.buildAndSignParams(params, true)

	// 使用url.Values动态构建form-encoded数据
	formVals := url.Values{}
	for k, v := range reqParams {
		formVals.Set(k, v)
	}
	formData := formVals.Encode()

	// 构建完整URL
	fullURL := fmt.Sprintf("%s%s", c.baseURL, queryPath)

	// 创建临时session副本以避免并发竞争
	postSession := httplib.NewSession()
	// 复制原始headers
	for k, v := range c.session.HttpHeaders {
		postSession.HttpHeaders[k] = v
	}
	// 添加POST专用的Content-Type头
	postSession.HttpHeaders["Content-Type"] = "application/x-www-form-urlencoded"

	// 发送POST请求
	log.GetInstance().Sugar.Infof("Sending POST to Ipang API: %s", fullURL)
	log.GetInstance().Sugar.Debugf("Form data: %s", formData)

	respBytes, err := postSession.Post(fullURL, []byte(formData), 0) // 使用0作为默认类型，因为我们手动设置了Content-Type
	if err != nil {
		log.GetInstance().Sugar.Errorf("Failed to post to Ipang API: %v", err)
		return nil, fmt.Errorf("POST请求Ipang API失败: %w", err)
	}

	// 解析响应
	response, err := c.parsePostResponse(respBytes)
	if err != nil {
		return nil, err
	}

	// 统计房产数量
	totalCount := 0
	for _, house := range response.Detail.NewHouse {
		totalCount += len(house.List)
	}
	for _, house := range response.Detail.SecHouse {
		totalCount += len(house.List)
	}
	log.GetInstance().Sugar.Infof("Ipang POST API returned %d new houses, %d sec houses, total %d items",
		len(response.Detail.NewHouse), len(response.Detail.SecHouse), totalCount)
	return response, nil
}

// QA 问答结构
type QA struct {
	Q string `json:"q"` // AI的问题
	A string `json:"a"` // 用户的回答
}

// SearchWithQA 基于问答式查询房产信息
func (c *Client) SearchWithQA(qas []QA, params *QueryParams) (*PostQueryResponse, error) {
	// 如果没有提供基础参数，使用空结构体
	if params == nil {
		params = &QueryParams{}
	}

	// 将问答列表转换为JSON字符串
	qasBytes, err := json.Marshal(qas)
	if err != nil {
		return nil, fmt.Errorf("问答列表JSON序列化失败: %w", err)
	}
	params.Qas = string(qasBytes)

	return c.PostQuery(params)
}
