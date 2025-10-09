package keywords

import "sync"

// ShanghaiPlates 上海板块数据 - 从Excel导入的完整板块列表
// 数据来源：上海板块列表.xlsx
// 总计：16个区域，201个板块
var ShanghaiPlates = map[string][]string{
	"浦东": {
		"万祥镇", "三林", "世博", "临港新城", "书院镇", "北蔡", "南码头", "合庆",
		"周浦", "唐镇", "塘桥", "外高桥", "大团镇", "宣桥", "川沙", "康桥",
		"张江", "御桥", "惠南", "新场", "曹路", "杨东", "杨思前滩", "梅园",
		"泥城镇", "洋泾", "源深", "潍坊", "碧云", "祝桥", "老港镇", "联洋",
		"航头", "花木", "金杨", "金桥", "陆家嘴", "高东", "高行",
	},
	"闵行": {
		"七宝", "华漕", "古美", "吴泾", "春申", "梅陇", "浦江", "老闵行",
		"航华", "莘庄北广场", "莘庄南广场", "金汇", "金虹桥", "闵浦", "静安新城",
		"颛桥", "马桥", "龙柏",
	},
	"宝山": {
		"上大", "共富", "共康", "大华", "大场镇", "张庙", "月浦", "杨行",
		"淞南", "淞宝", "罗店", "罗泾", "通河", "顾村", "高境",
	},
	"徐汇": {
		"万体馆", "上海南站", "华东理工", "华泾", "康健", "建国西路", "徐家汇",
		"徐汇滨江", "斜土路", "植物园", "漕河泾", "田林", "衡山路", "长桥", "龙华",
	},
	"松江": {
		"九亭", "佘山", "叶榭", "小昆山", "新桥", "新浜", "松江大学城", "松江新城",
		"松江老城", "泖港", "泗泾", "洞泾", "石湖荡", "莘闵别墅", "车墩",
	},
	"嘉定": {
		"丰庄", "华亭", "南翔", "嘉定新城", "嘉定老城", "外冈", "安亭", "徐行",
		"新成路", "江桥", "菊园新区", "马陆",
	},
	"普陀": {
		"万里", "中远两湾城", "光新", "曹杨", "桃浦", "武宁", "甘泉宜川", "真光",
		"真如", "长寿路", "长征", "长风",
	},
	"黄浦": {
		"世博滨江", "五里桥", "人民广场", "南京东路", "打浦桥", "新天地", "淮海中路",
		"老西门", "董家渡", "蓬莱公园", "豫园", "黄浦滨江",
	},
	"青浦": {
		"华新", "夏阳", "徐泾", "朱家角", "白鹤", "盈浦", "练塘", "赵巷",
		"重固", "金泽", "香花桥",
	},
	"静安": {
		"不夜城", "南京西路", "大宁", "彭浦", "曹家渡", "永和", "江宁路",
		"西藏北路", "闸北公园", "阳城", "静安寺",
	},
	"奉贤": {
		"南桥", "四团", "奉城", "奉贤金汇", "庄行", "柘林", "海湾", "西渡", "青村",
	},
	"长宁": {
		"中山公园", "仙霞", "北新泾", "古北", "天山", "新华路", "虹桥", "西郊", "镇宁路",
	},
	"杨浦": {
		"东外滩", "中原", "五角场", "周家嘴路", "控江路", "新江湾城", "鞍山", "黄兴公园",
	},
	"崇明": {
		"堡镇", "崇明", "崇明其他", "崇明新城", "横沙岛", "长兴岛", "陈家镇",
	},
	"虹口": {
		"临平路", "凉城", "北外滩", "四川北路", "曲阳", "江湾镇", "鲁迅公园",
	},
	"金山": {
		"金山",
	},
}

var (
	plateToDistrictMap map[string]string
	once               sync.Once
)

// buildPlateToDistrictMap 创建一个从板块到其所属区域的反向映射，用于O(1)查询
func buildPlateToDistrictMap() {
	plateToDistrictMap = make(map[string]string)
	for district, plates := range ShanghaiPlates {
		for _, plate := range plates {
			plateToDistrictMap[plate] = district
		}
	}
}

// GetShanghaiPlates 获取指定区域的板块列表
func GetShanghaiPlates(district string) []string {
	if plates, exists := ShanghaiPlates[district]; exists {
		return plates
	}
	return []string{}
}

// GetAllShanghaiPlates 获取所有上海板块（平铺）
func GetAllShanghaiPlates() []string {
	var allPlates []string
	for _, plates := range ShanghaiPlates {
		allPlates = append(allPlates, plates...)
	}
	return allPlates
}

// IsShanghaiPlate 判断是否为上海板块，并返回其所属的区
// 使用O(1)时间复杂度的查询
func IsShanghaiPlate(plate string) (isPlate bool, district string) {
	once.Do(buildPlateToDistrictMap) // 确保映射只构建一次
	
	district, isPlate = plateToDistrictMap[plate]
	return isPlate, district
}