package keywords

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// KeywordDictionary 关键词字典管理
type KeywordDictionary struct {
	// 地理位置词典
	LocationDict map[string][]string `json:"location_dict"`
	// 房型词典
	PropertyTypeDict map[string][]string `json:"property_type_dict"`
	// 价格相关词典
	PriceDict map[string][]string `json:"price_dict"`
	// 面积词典
	AreaDict map[string][]string `json:"area_dict"`
	// 装修词典
	DecorationDict map[string][]string `json:"decoration_dict"`
	// 朝向词典
	OrientationDict map[string][]string `json:"orientation_dict"`
	// 配套设施词典
	FacilityDict map[string][]string `json:"facility_dict"`
	// 楼层词典
	FloorDict map[string][]string `json:"floor_dict"`
	// 商业地产词典
	CommercialDict map[string][]string `json:"commercial_dict"`
	// 兴趣点词典
	InterestPointDict map[string][]string `json:"interest_point_dict"`
	// 自定义词典
	CustomDict map[string][]string `json:"custom_dict"`

	// 同义词映射
	SynonymMap map[string]string `json:"synonym_map"`
	// 词频统计
	WordFreq map[string]int `json:"word_freq"`

	mu sync.RWMutex
}

// NewKeywordDictionary 创建关键词字典
func NewKeywordDictionary() *KeywordDictionary {
	kd := &KeywordDictionary{
		LocationDict:     make(map[string][]string),
		PropertyTypeDict: make(map[string][]string),
		PriceDict:        make(map[string][]string),
		AreaDict:         make(map[string][]string),
		DecorationDict:   make(map[string][]string),
		OrientationDict:  make(map[string][]string),
		FacilityDict:     make(map[string][]string),
		FloorDict:        make(map[string][]string),

		CommercialDict:    make(map[string][]string),
		InterestPointDict: make(map[string][]string),
		CustomDict:        make(map[string][]string),
		SynonymMap:        make(map[string]string),
		WordFreq:          make(map[string]int),
	}

	kd.initializeDefaultDict()
	return kd
}

// initializeDefaultDict 初始化默认词典
func (kd *KeywordDictionary) initializeDefaultDict() {
	// 地理位置词典 - 扩展版
	// 🆕 整合上海板块数据
	shanghaiLocations := []string{
		"浦东", "浦东新区", "黄浦", "黄浦区", "徐汇", "徐汇区", "长宁", "长宁区",
		"静安", "静安区", "普陀", "普陀区", "虹口", "虹口区", "杨浦", "杨浦区",
		"闵行", "闵行区", "宝山", "宝山区", "嘉定", "嘉定区", "金山", "金山区",
		"松江", "松江区", "青浦", "青浦区", "奉贤", "奉贤区", "崇明", "崇明区",
	}
	// 添加所有板块
	shanghaiLocations = append(shanghaiLocations, GetAllShanghaiPlates()...)

	kd.LocationDict = map[string][]string{
		"上海": shanghaiLocations,
		"北京": {
			"东城", "东城区", "西城", "西城区", "朝阳", "朝阳区", "丰台", "丰台区",
			"石景山", "石景山区", "海淀", "海淀区", "门头沟", "门头沟区", "房山", "房山区",
			"通州", "通州区", "顺义", "顺义区", "昌平", "昌平区", "大兴", "大兴区",
			"怀柔", "怀柔区", "平谷", "平谷区", "密云", "密云区", "延庆", "延庆区",
			"国贸", "三里屯", "望京", "亦庄", "回龙观", "天通苑", "西二旗",
		},
		"深圳": {
			"罗湖", "罗湖区", "福田", "福田区", "南山", "南山区", "宝安", "宝安区",
			"龙岗", "龙岗区", "盐田", "盐田区", "龙华", "龙华区", "坪山", "坪山区",
			"光明", "光明区", "大鹏", "大鹏新区", "前海", "蛇口", "科技园", "华强北",
		},
		"广州": {
			"越秀", "越秀区", "荔湾", "荔湾区", "海珠", "海珠区", "天河", "天河区",
			"白云", "白云区", "黄埔", "黄埔区", "番禺", "番禺区", "花都", "花都区",
			"南沙", "南沙区", "从化", "从化区", "增城", "增城区", "珠江新城", "天河北",
		},
	}

	// 房型词典 - 更详细
	kd.PropertyTypeDict = map[string][]string{
		"新房": {
			"新楼盘", "期房", "现房", "新开盘", "首开", "新项目", "在建", "预售",
			"准现房", "新盘", "楼盘", "新开发", "新建", "待售",
		},
		"二手房": {
			"二手", "已住", "转手", "置换", "老房子", "次新房", "满五唯一",
			"满二", "已入住", "现房", "即买即住", "业主自售", "中介房源",
		},
		"别墅": {
			"独栋", "联排", "叠拼", "合院", "花园洋房", "洋房", "别墅区", "墅区",
			"双拼", "类别墅", "庭院", "带花园", "独立别墅", "叠墅",
		},
		"公寓": {
			"酒店式公寓", "商务公寓", "loft", "复式", "单身公寓",
			"开间", "单间", "精装公寓", "服务式公寓",
		},
		"小户型": {
			"小户型", "小面积", "紧凑型", "一居室", "单间配套",
		},
		"商铺": {
			"商铺", "店铺", "临街商铺", "底商", "写字楼", "办公楼", "商业地产",
			"投资性房产", "商用房", "门面房",
		},
	}

	// 装修词典 - 详细分类
	kd.DecorationDict = map[string][]string{
		"毛坯": {
			"毛坯房", "清水房", "未装修", "毛胚", "原始户型", "水泥地面",
			"白墙", "简单处理", "基础设施", "毛坯交付",
		},
		"简装": {
			"简单装修", "简装修", "基础装修", "普通装修", "刷墙铺地",
			"基本设施", "简单家具", "能住", "基础配置",
		},
		"精装": {
			"精装修", "精装房", "拎包入住", "豪华装修", "全装修",
			"品牌装修", "高档装修", "整装", "全屋定制", "精装交付",
		},
		"豪装": {
			"豪华装修", "高端装修", "顶级装修", "奢华装修", "定制装修",
			"名师设计", "进口材料", "智能家居", "全屋智能",
		},
	}

	// 朝向词典 - 更全面
	kd.OrientationDict = map[string][]string{
		"南向": {
			"朝南", "向南", "南朝向", "正南", "南面", "坐北朝南", "主卧朝南",
			"客厅朝南", "正南向", "偏南", "东南", "西南",
		},
		"北向": {
			"朝北", "向北", "北朝向", "正北", "北面", "坐南朝北",
			"正北向", "偏北", "东北", "西北",
		},
		"东向": {
			"朝东", "向东", "东朝向", "正东", "东面", "坐西朝东",
			"正东向", "偏东", "东南", "东北",
		},
		"西向": {
			"朝西", "向西", "西朝向", "正西", "西面", "坐东朝西",
			"正西向", "偏西", "西南", "西北",
		},
		"南北": {
			"南北通透", "南北朝向", "南北通风", "南北对流", "双朝向",
			"通透户型", "对流", "通风好",
		},
		"东西": {
			"东西朝向", "东西通透", "东西向", "横厅", "东西户型",
		},
	}

	// 价格相关词典
	kd.PriceDict = map[string][]string{
		"总价": {
			"总价", "房价", "售价", "挂牌价", "成交价", "市场价",
			"一口价", "到手价", "实际价格", "房款", "购房款",
		},
		"单价": {
			"单价", "均价", "每平", "平米价", "每平米", "平方米价格",
			"单位价格", "楼面价", "地板价",
		},
		"预算": {
			"预算", "资金", "首付", "可承受价格", "心理价位", "期望价格",
			"能接受", "价格区间", "价位", "购房预算", "资金预算",
		},
	}

	// 面积词典
	kd.AreaDict = map[string][]string{
		"建筑面积": {
			"建筑面积", "建面", "总面积", "房屋面积", "使用面积",
			"实用面积", "套内面积", "得房率", "公摊面积",
		},
		"使用面积": {
			"使用面积", "实用面积", "套内面积", "室内面积", "净面积",
			"居住面积", "有效面积", "实际面积",
		},
	}

	// 楼层词典
	kd.FloorDict = map[string][]string{
		"楼层": {
			"楼层", "层", "楼", "第几层", "哪一层", "楼高", "层高",
			"总楼层", "建筑高度", "层数",
		},
		"低层": {
			"低层", "底层", "1-3层", "低楼层", "一层", "二层", "三层",
			"地面层", "花园层",
		},
		"中层": {
			"中层", "中间层", "4-10层", "中等楼层", "中间楼层",
		},
		"高层": {
			"高层", "高楼层", "10层以上", "顶层", "景观层", "无遮挡",
		},
	}


	// 商业地产词典
	kd.CommercialDict = map[string][]string{
		"商业地产": {
			"商业地产", "商铺", "商业", "商用", "门面", "店面", "商业房",
			"商务楼", "写字楼", "办公楼", "商业综合体", "商业中心",
			"购物中心", "商场", "底商", "沿街商铺", "临街商铺",
		},
		"商业地段": {
			"核心商圈", "黄金地段", "商业中心", "CBD", "金融区",
			"商务区", "繁华地段", "商业街", "步行街", "商业广场",
			"市中心", "downtown", "商业核心", "商圈", "商业带",
			"一线商圈", "二线商圈", "成熟商圈", "新兴商圈",
		},
		"租金收益": {
			"租金", "月租", "年租", "租赁", "出租", "承租",
			"租金收入", "租赁收入", "租金水平", "市场租金", "合理租金",
			"租金上涨", "租金下跌", "租金稳定", "高租金", "低租金",
			"免租期", "递增租金", "固定租金", "浮动租金",
		},
		"投资回报": {
			"回报率", "收益率", "投资回报", "年化收益", "租金回报率",
			"投资收益", "现金流", "净收益", "毛收益", "ROI",
			"IRR", "投资性价比", "投资价值", "增值潜力", "保值增值",
			"资产升值", "长期投资", "短期投资", "稳定收益",
		},
		"商业类型": {
			"零售商铺", "餐饮商铺", "服务业", "美容美发", "服装店",
			"便利店", "超市", "药店", "银行", "咖啡厅", "茶楼",
			"培训机构", "健身房", "医疗诊所", "办公室", "工作室",
			"仓储", "物流", "展厅", "showroom",
		},
		"商业配套": {
			"人流量", "客流", "消费人群", "目标客户", "商业氛围",
			"配套完善", "交通便利", "停车方便", "地铁口", "公交站",
			"商业配套", "周边配套", "生活配套", "成熟社区",
		},
		"商业政策": {
			"商业用地", "商住混合", "产权年限", "70年产权", "40年产权",
			"商业贷款", "首付比例", "贷款利率", "税费", "营业税",
			"增值税", "个人所得税", "印花税", "契税", "过户费",
		},
		"商业投资": {
			"商业投资", "投资商铺", "商铺投资", "以租养房", "资产配置",
			"多元化投资", "风险投资", "稳健投资", "长期持有", "短期套利",
			"抄底", "逢低买入", "高位套现", "投资时机", "市场时机",
		},
	}

	// 配套设施词典 - 保留但简化学校相关内容
	kd.FacilityDict = map[string][]string{
		"学校": {
			"学校", "教育机构", "培训机构", "补习班", "兴趣班",
		},
		"医院": {
			"医院", "医疗", "诊所", "卫生院", "三甲医院", "社区医院",
			"医疗设施", "就医方便",
		},
		"商场": {
			"商场", "购物中心", "超市", "商业街", "百货", "商业配套",
			"购物方便", "商业设施", "生活便利",
		},
		"停车": {
			"停车", "车位", "停车场", "停车库", "地下车库", "露天停车",
			"停车方便", "车位配比", "停车费",
		},
	}

	// 兴趣点词典 - 地理位置特征
	kd.InterestPointDict = map[string][]string{
		"交通便利": {
			"交通便利", "交通方便", "出行方便", "出行便利", "通勤方便",
			"交通发达", "交通网络", "出行便捷", "公共交通", "交通枢纽",
			"多条线路", "换乘方便", "通勤便利",
		},
		"近地铁": {
			"近地铁", "地铁附近", "地铁边", "地铁口", "地铁旁", "临近地铁",
			"地铁沿线", "地铁站附近", "地铁便利", "地铁上盖", "地铁物业",
			"轨道交通", "距离地铁", "步行到地铁", "地铁覆盖",
		},
		"地段好": {
			"地段好", "位置好", "地理位置好", "区位优势", "地段优质",
			"黄金地段", "核心地段", "优质地段", "成熟地段", "繁华地段",
			// 移除"商业地段", "CBD", "市中心", "核心区域", "热门地段" - 这些会导致过度匹配
		},
		"CBD商圈": {
			"CBD", "CBD核心商圈", "CBD商圈", "CBD中心", "中央商务区",
			"核心商务区", "商务中心区", "金融中心", "商业核心区",
			"核心商圈", "一线商圈", "顶级商圈", "成熟商圈",
		},
		"投资优势": {
			"投资价值", "投资性价比", "增值潜力", "长期收益",
			"投资收益好", "投资回报", "投资回报率", "年化收益",
		},
		"学校好": {
			"学校好", "名校", "重点学校", "名校学区", "优质学区",
			"教育资源好", "学区优质", "对口名校", "重点中学", "重点小学",
			"985", "211", "一本", "示范学校", "实验学校", "附属学校",
		},
		"学区房": {
			"学区房", "学区", "学位房", "教育地产", "名校房", "重点学校房源",
			"学位价值", "对口学校", "划片学校", "学区划分", "学区房源",
		},
		"重点小学": {
			"重点小学", "对口重点小学", "优质小学", "示范小学", "实验小学",
		},
		"近幼儿园": {
			"近幼儿园", "幼儿园附近", "幼儿园边", "临近幼儿园", "靠近幼儿园",
			"幼儿园旁边", "距离幼儿园", "幼儿园周边", "示范幼儿园附近", "国际幼儿园附近",
			"双语幼儿园附近", "公办幼儿园附近", "私立幼儿园附近", "一级幼儿园附近",
			"步行到幼儿园", "走路到幼儿园", "幼儿园便利", "接送方便",
		},
		"医院近": {
			"医院近", "医疗方便", "就医方便", "医院附近", "三甲医院",
			"医疗配套", "医疗资源", "医院便利", "看病方便", "医疗设施",
			"社区医院", "医疗中心", "综合医院", "专科医院",
		},
		"近公园": {
			"近公园", "公园附近", "公园边", "临近公园", "公园景观",
			"绿化好", "环境优美", "生态环境", "园林景观", "景观房",
			"湖景", "江景", "河景", "山景", "绿化率高", "空气好",
		},
		"吃饭方便": {
			"吃饭方便", "餐饮方便", "美食多", "餐厅多", "小吃街",
			"美食街", "餐饮配套", "用餐便利", "食堂", "cafeteria",
			"外卖方便", "餐饮丰富", "饮食便利",
		},
		"酒吧多": {
			"酒吧多", "夜生活", "娱乐配套", "酒吧街", "夜市",
			"娱乐场所", "KTV", "夜店", "清吧", "酒廊", "夜生活丰富",
			"娱乐设施", "休闲娱乐", "夜间娱乐", "酒吧聚集",
		},
		"商超近": {
			"商超近", "超市近", "购物方便", "商场附近", "百货附近",
			"商业配套", "购物中心", "生活便利", "商业设施", "买菜方便",
			"日用品方便", "生活配套",
		},
		"地铁上盖": {
			"地铁上盖", "TOD", "轨道上盖", "站点上盖", "交通枢纽",
			"直达地铁", "零换乘", "地铁直通",
		},
		"近综合体": {
			"近综合体", "商业综合体", "购物综合体", "城市综合体",
			"万达", "万象城", "海港城", "太古里", "银泰", "大悦城",
			"综合商场", "一站式", "吃喝玩乐",
			// 移除"商业中心" - 这会导致CBD等商业地段被误判为近综合体
		},
		"带花园": {
			"带花园", "有花园", "私家花园", "独立花园", "庭院", "花园别墅",
			"露台花园", "屋顶花园", "前后花园", "花园洋房", "园林", "绿化带",
			"景观花园", "私人花园", "花坛", "绿地", "草坪",
		},
		"有车库": {
			"有车库", "带车库", "私家车库", "独立车库", "地下车库", "停车库",
			"车位", "停车位", "汽车库", "双车库", "单车库", "车库门",
			"停车间", "车房", "停车棚", "车位配套", "车库",
		},
		"写字楼里": {
			"写字楼", "写字楼门面", "写字楼底商", "办公楼", "办公楼门面",
			"商务楼", "商务楼门面", "甲级写字楼", "5A写字楼", "智能写字楼",
			"高端写字楼", "商务中心", "办公中心", "企业大厦", "商务大厦",
		},
		"人流量大": {
			"人流量大", "人流密集", "客流量大", "客流密集", "人气旺",
			"繁华", "热闹", "人潮", "客流多", "流量大", "访客多",
			"人员密集", "客户多", "顾客多", "行人多", "往来人员多",
		},
		"商业街": {
			"商业街", "步行街", "商业步行街", "商街", "商业区",
			"商业中心", "商圈", "购物街", "商业大街", "繁华商业街",
			"核心商业街", "主要商业街", "商业主街", "商业地带", "商业核心区",
		},
	}

	// 初始化同义词映射
	kd.initSynonymMap()
}

// initSynonymMap 初始化同义词映射
func (kd *KeywordDictionary) initSynonymMap() {
	kd.SynonymMap = map[string]string{
		// 🔧 修复：移除单字符中文数字映射，避免"八十"被错误处理为"810"
		// 原代码会导致：八十 → 八:"8" + 十:"10" = "810" 的错误
		// 单字符数字映射已由NLP处理器的复合数字标准化处理
		//
		// 原有代码：
		// "一": "1", "二": "2", "三": "3", "四": "4", "五": "5",
		// "六": "6", "七": "7", "八": "8", "九": "9", "十": "10",
		// "两": "2", "俩": "2", "仨": "3",

		// 房型同义词 - 单向标准化规则
		// "卫生间" → "卫" (保持与正则"室/厅/卫"的一致性)
		// "房/间/卧室" → "室" (正则使用"室/厅/卫")
		// 删除循环映射和恒等映射，确保替换结果稳定
		"卫生间": "卫",
		"房":   "室", "间": "室", "卧室": "室",

		// 朝向同义词 - 避免单字符映射导致误匹配
		"东南": "东南向", "东北": "东北向", "西南": "西南向", "西北": "西北向",

		// 装修同义词
		"装修好": "精装", "已装修": "精装", "带装修": "精装",
		"没装修": "毛坯", "未装": "毛坯", "空房": "毛坯",

		// 面积单位同义词
		"平": "平米", "平方": "平米", "㎡": "平米", "平方米": "平米",
		"方": "平米", "个平方": "平米",

		// 价格单位同义词
		"w": "万", "W": "万", "万块": "万元",
		"千": "千元", "k": "千", "K": "千",

		// 学区房相关同义词
		"学位": "学区房", "名校": "重点学校", "对口": "学区",
		"划片": "学区", "入学": "学区房", "择校": "学区房",

		// 商业地产相关同义词
		"商铺": "商业地产", "门面": "商铺", "店面": "商铺", "商用": "商业地产",
		"写字楼": "商业地产", "办公楼": "商业地产", "商务楼": "商业地产",
		"CBD": "商业中心", "商圈": "商业地段", "黄金地段": "商业地段",
		// 注意：移除了"租金"和"收益"的映射，避免破坏租金价格的正则匹配
	}
}

// MatchKeywords 匹配关键词
func (kd *KeywordDictionary) MatchKeywords(text string, category string) []string {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	text = strings.ToLower(text)
	var matches []string

	// 获取对应类别的词典
	var dict map[string][]string
	switch category {
	case "location":
		dict = kd.LocationDict
	case "property_type":
		dict = kd.PropertyTypeDict
	case "decoration":
		dict = kd.DecorationDict
	case "orientation":
		dict = kd.OrientationDict
	case "price":
		dict = kd.PriceDict
	case "area":
		dict = kd.AreaDict
	case "floor":
		dict = kd.FloorDict
	case "facility":
		dict = kd.FacilityDict
	case "commercial":
		dict = kd.CommercialDict
	case "interest_point":
		dict = kd.InterestPointDict
	default:
		// 搜索所有词典
		for _, d := range []map[string][]string{
			kd.LocationDict, kd.PropertyTypeDict, kd.DecorationDict,
			kd.OrientationDict, kd.PriceDict, kd.AreaDict,
			kd.FloorDict, kd.FacilityDict, kd.CommercialDict, kd.InterestPointDict,
		} {
			for standardKey, keywords := range d {
				for _, keyword := range keywords {
					if strings.Contains(text, strings.ToLower(keyword)) {
						matches = append(matches, standardKey)
						break
					}
				}
			}
		}
		return matches
	}

	// 在指定词典中匹配 - 优先匹配长词
	// 先收集所有匹配项，然后按长度排序
	type matchItem struct {
		standardKey string
		keyword     string
		length      int
	}
	var allMatches []matchItem

	for standardKey, keywords := range dict {
		for _, keyword := range keywords {
			if strings.Contains(strings.ToLower(text), strings.ToLower(keyword)) {
				allMatches = append(allMatches, matchItem{
					standardKey: standardKey,
					keyword:     keyword,
					length:      len(keyword),
				})
			}
		}
	}

	// 按关键词长度降序排序，优先选择最长的匹配
	for i := 0; i < len(allMatches); i++ {
		for j := i + 1; j < len(allMatches); j++ {
			if allMatches[i].length < allMatches[j].length {
				allMatches[i], allMatches[j] = allMatches[j], allMatches[i]
			}
		}
	}

	// 添加不重复的匹配结果
	addedKeys := make(map[string]bool)
	for _, match := range allMatches {
		if !addedKeys[match.standardKey] {
			matches = append(matches, match.standardKey)
			addedKeys[match.standardKey] = true
		}
	}

	return matches
}

// NormalizeText 文本标准化
func (kd *KeywordDictionary) NormalizeText(text string) string {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	normalized := text

	// 🔧 修复稳定性问题：确保确定性的遍历顺序
	// 应用同义词映射 - 按键排序确保稳定性
	var synonymKeys []string
	for synonym := range kd.SynonymMap {
		synonymKeys = append(synonymKeys, synonym)
	}
	sort.Strings(synonymKeys) // 按字典序排序确保稳定顺序

	for _, synonym := range synonymKeys {
		standard := kd.SynonymMap[synonym]
		normalized = strings.ReplaceAll(normalized, synonym, standard)
	}

	return normalized
}

// AddCustomKeywords 添加自定义关键词
func (kd *KeywordDictionary) AddCustomKeywords(category, standardKey string, keywords []string) {
	kd.mu.Lock()
	defer kd.mu.Unlock()

	switch category {
	case "location":
		kd.LocationDict[standardKey] = append(kd.LocationDict[standardKey], keywords...)
	case "property_type":
		kd.PropertyTypeDict[standardKey] = append(kd.PropertyTypeDict[standardKey], keywords...)
	case "decoration":
		kd.DecorationDict[standardKey] = append(kd.DecorationDict[standardKey], keywords...)
	case "orientation":
		kd.OrientationDict[standardKey] = append(kd.OrientationDict[standardKey], keywords...)
	case "commercial":
		kd.CommercialDict[standardKey] = append(kd.CommercialDict[standardKey], keywords...)
	case "custom":
		kd.CustomDict[standardKey] = append(kd.CustomDict[standardKey], keywords...)
	}
}

// UpdateWordFrequency 更新词频统计
func (kd *KeywordDictionary) UpdateWordFrequency(words []string) {
	kd.mu.Lock()
	defer kd.mu.Unlock()

	for _, word := range words {
		kd.WordFreq[word]++
	}
}

// GetPopularWords 获取高频词汇
func (kd *KeywordDictionary) GetPopularWords(limit int) []string {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	type wordFreq struct {
		word string
		freq int
	}

	var words []wordFreq
	for word, freq := range kd.WordFreq {
		words = append(words, wordFreq{word, freq})
	}

	// 简单排序
	for i := 0; i < len(words)-1; i++ {
		for j := i + 1; j < len(words); j++ {
			if words[i].freq < words[j].freq {
				words[i], words[j] = words[j], words[i]
			}
		}
	}

	var result []string
	for i := 0; i < limit && i < len(words); i++ {
		result = append(result, words[i].word)
	}

	return result
}

// SaveToFile 保存词典到文件
func (kd *KeywordDictionary) SaveToFile(filename string) error {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	data, err := json.MarshalIndent(kd, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化词典失败: %w", err)
	}

	return os.WriteFile(filename, data, 0o644)
}

// LoadFromFile 从文件加载词典
func (kd *KeywordDictionary) LoadFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("读取词典文件失败: %w", err)
	}

	kd.mu.Lock()
	defer kd.mu.Unlock()

	return json.Unmarshal(data, kd)
}

// GetAllCategories 获取所有类别
func (kd *KeywordDictionary) GetAllCategories() []string {
	return []string{
		"location", "property_type", "decoration", "orientation",
		"price", "area", "floor", "facility", "custom",
		"commercial", "interest_point",
	}
}

// GetCategoryKeywords 获取指定类别的所有关键词
func (kd *KeywordDictionary) GetCategoryKeywords(category string) map[string][]string {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	switch category {
	case "location":
		return kd.LocationDict
	case "property_type":
		return kd.PropertyTypeDict
	case "decoration":
		return kd.DecorationDict
	case "orientation":
		return kd.OrientationDict
	case "price":
		return kd.PriceDict
	case "area":
		return kd.AreaDict
	case "floor":
		return kd.FloorDict
	case "facility":
		return kd.FacilityDict
	case "commercial":
		return kd.CommercialDict
	case "interest_point":
		return kd.InterestPointDict
	case "custom":
		return kd.CustomDict
	default:
		return nil
	}
}
