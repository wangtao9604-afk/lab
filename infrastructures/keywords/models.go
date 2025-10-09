package keywords

import "time"

// HousingRequirement 房产需求数据结构
type HousingRequirement struct {
	UserID    string    `json:"user_id"`
	Building  Building  `json:"building"`
	Opening   Opening   `json:"opening"`
	UnitType  UnitType  `json:"unit_type"`
	Community Community `json:"community"`
	Property  Property  `json:"property"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Building 楼盘信息
type Building struct {
	Name      string  `json:"name"`      // 名称
	Province  string  `json:"province"`  // 省
	City      string  `json:"city"`      // 市
	District  string  `json:"district"`  // 区
	Address   string  `json:"address"`   // 地址
	Longitude float64 `json:"longitude"` // 经度
	Latitude  float64 `json:"latitude"`  // 纬度
	MinArea   float64 `json:"min_area"`  // 最小面积
	MaxArea   float64 `json:"max_area"`  // 最大面积
	Price     int64   `json:"price"`     // 价格(元)
	MinTotal  float64 `json:"min_total"` // 最低总价(万)
	MaxTotal  float64 `json:"max_total"` // 最高总价(万)
	Score     float64 `json:"score"`     // 入围分数
	Block     string  `json:"block"`     // 板块
}

// Opening 开盘信息
type Opening struct {
	Title          string `json:"title"`            // 开盘标题
	SaleBuilding   string `json:"sale_building"`    // 销售楼栋
	PropertyType   string `json:"property_type"`    // 房源类型
	Area           string `json:"area"`             // 面积
	SaleUnit       string `json:"sale_unit"`        // 销售户型
	Developer      string `json:"developer"`        // 开发商
	OpenTime       string `json:"open_time"`        // 开盘时间
	BookingStart   string `json:"booking_start"`    // 认购开始时间
	BookingRules   string `json:"booking_rules"`    // 认购规则
	Facade         string `json:"facade"`           // 外立面
	UsableRate     string `json:"usable_rate"`      // 得房率
	FloorAreaRatio string `json:"floor_area_ratio"` // 容积率
	GreenRate      string `json:"green_rate"`       // 绿化率
	ElevatorRatio  string `json:"elevator_ratio"`   // 梯户比
	Corridor       string `json:"corridor"`         // 连廊
	ParkingRatio   string `json:"parking_ratio"`    // 车位比
	Clubhouse      string `json:"clubhouse"`        // 会所
	CarSeparation  string `json:"car_separation"`   // 人车分流
}

// UnitType 户型信息
type UnitType struct {
	Name         string   `json:"name"`          // 名称
	RoomType     string   `json:"room_type"`     // 房型
	PriceType    string   `json:"price_type"`    // 价格类型
	Layout       string   `json:"layout"`        // 户型
	Orientation  string   `json:"orientation"`   // 朝向
	BuildingArea float64  `json:"building_area"` // 建筑面积
	MinArea      float64  `json:"min_area"`      // 最小面积
	MaxArea      float64  `json:"max_area"`      // 最大面积
	MinTotal     float64  `json:"min_total"`     // 最低总价(万)
	MaxTotal     float64  `json:"max_total"`     // 最高总价(万)
	TotalRange   string   `json:"total_range"`   // 总价范围(万)
	Images       []string `json:"images"`        // 图片列表
	SaleType     string   `json:"sale_type"`     // 销售类型
}

// Community 小区信息
type Community struct {
	Name            string  `json:"name"`             // 名称
	City            string  `json:"city"`             // 市
	Area            string  `json:"area"`             // 地区
	Address         string  `json:"address"`          // 地址
	Town            string  `json:"town"`             // 村镇
	Subway          string  `json:"subway"`           // 地铁
	AvgPrice        int64   `json:"avg_price"`        // 均价
	DealAvgPrice    int64   `json:"deal_avg_price"`   // 成交均价
	LowestPrice     int64   `json:"lowest_price"`     // 最低价格
	AveragePrice    int64   `json:"average_price"`    // 平均价格
	OnSaleCount     int     `json:"on_sale_count"`    // 在售数量
	RentCount       int     `json:"rent_count"`       // 租房数量
	BuildingAge     int     `json:"building_age"`     // 建筑年限
	BuildingTotal   int     `json:"building_total"`   // 楼栋总数
	HouseTotal      int     `json:"house_total"`      // 房屋总数
	BuildingType    string  `json:"building_type"`    // 建筑类型
	PropertyFee     string  `json:"property_fee"`     // 物业费
	PropertyCompany string  `json:"property_company"` // 物业公司
	Developer       string  `json:"developer"`        // 开发商
	Longitude       float64 `json:"longitude"`        // 经度
	Latitude        float64 `json:"latitude"`         // 纬度
	Block           string  `json:"block"`            // 板块
}

// Property 房源信息
type Property struct {
	Community       string  `json:"community"`        // 小区
	TotalFloor      int     `json:"total_floor"`      // 总楼层
	CurrentFloor    int     `json:"current_floor"`    // 当前楼层
	AvgPrice        int64   `json:"avg_price"`        // 均价
	Area            float64 `json:"area"`             // 面积
	TotalPrice      float64 `json:"total_price"`      // 总价
	PropertyType    string  `json:"property_type"`    // 房屋类型
	VillaType       string  `json:"villa_type"`       // 别墅类型
	Description     string  `json:"description"`      // 介绍
	Bedrooms        int     `json:"bedrooms"`         // 卧室数
	LivingRooms     int     `json:"living_rooms"`     // 客厅数
	Kitchens        int     `json:"kitchens"`         // 厨房数
	Bathrooms       int     `json:"bathrooms"`        // 卫生间数
	City            string  `json:"city"`             // 市
	District        string  `json:"district"`         // 地区
	Town            string  `json:"town"`             // 村镇
	ViewTime        string  `json:"view_time"`        // 看房时间
	Subway          string  `json:"subway"`           // 地铁
	MortgageInfo    string  `json:"mortgage_info"`    // 抵押信息
	PropertyCert    string  `json:"property_cert"`    // 房本备件
	PropertyCode    string  `json:"property_code"`    // 房协编号
	LayoutStructure string  `json:"layout_structure"` // 户型结构
	InnerArea       float64 `json:"inner_area"`       // 套内面积
	BuildingType    string  `json:"building_type"`    // 建筑类型
	Orientation     string  `json:"orientation"`      // 房屋朝向
	Decoration      string  `json:"decoration"`       // 装修情况
	Structure       string  `json:"structure"`        // 建筑结构
	Heating         string  `json:"heating"`          // 供暖
	Water           string  `json:"water"`            // 供水
	Electricity     string  `json:"electricity"`      // 供电
	Gas             string  `json:"gas"`              // 供气
	ElevatorRatio   string  `json:"elevator_ratio"`   // 梯户比例
	Elevator        string  `json:"elevator"`         // 配备电梯
	TransferRight   string  `json:"transfer_right"`   // 交易权属
	ListTime        string  `json:"list_time"`        // 挂牌时间
	Usage           string  `json:"usage"`            // 房屋用途
	PropertyYears   int     `json:"property_years"`   // 房屋年限
	Ownership       string  `json:"ownership"`        // 房权所属
	RingPosition    string  `json:"ring_position"`    // 环线位置
	BuildingYear    int     `json:"building_year"`    // 建筑年代
	Features        string  `json:"features"`         // 房源特色
}

// RoomInfo 房型信息
type RoomInfo struct {
	Bedrooms    int `json:"bedrooms"`
	LivingRooms int `json:"living_rooms"`
	Bathrooms   int `json:"bathrooms"`
	Kitchens    int `json:"kitchens"`
}

// PriceRange 价格区间
type PriceRange struct {
	Value    float64 `json:"value,omitempty"`    // 单个价格值
	Min      float64 `json:"min,omitempty"`      // 最小价格
	Max      float64 `json:"max,omitempty"`      // 最大价格
	Unit     string  `json:"unit"`               // 万、元
	Operator string  `json:"operator,omitempty"` // 操作符：about, <=, >=
}

// FloorInfo 楼层信息
type FloorInfo struct {
	Current int `json:"current"`
	Total   int `json:"total"`
}

// LocationInfo 位置信息
type LocationInfo struct {
	Province string `json:"province"`
	City     string `json:"city"`
	District string `json:"district"`
	Address  string `json:"address"`
}
