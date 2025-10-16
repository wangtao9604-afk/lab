package gaode

import (
	"encoding/json"
)

// BaseResponse is common response structure for all Gaode APIs
type BaseResponse struct {
	Status   string `json:"status"`
	Info     string `json:"info"`
	InfoCode string `json:"infocode"`
}

func (r *BaseResponse) IsSuccess() bool {
	return r.Status == "1" && r.InfoCode == "10000"
}

// StringOrArray handles Gaode's inconsistent city field that can be either string or empty array
type StringOrArray string

func (s *StringOrArray) UnmarshalJSON(data []byte) error {
	// Try string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = StringOrArray(str)
		return nil
	}

	// Try array (for municipalities that return [])
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		if len(arr) > 0 {
			*s = StringOrArray(arr[0])
		} else {
			*s = ""
		}
		return nil
	}

	// Invalid format
	return json.Unmarshal(data, &str) // Return original error
}

func (s StringOrArray) String() string {
	return string(s)
}

// Pagination holds common pagination parameters
type Pagination struct {
	PageSize *int
	PageNum  *int
}

// FieldMask holds show_fields parameter
type FieldMask struct {
	ShowFields string
}

// Route planning request/response structures

type RouteRequest struct {
	Origin        string
	Destination   string
	OriginID      string
	DestinationID string
}

type DrivingRouteRequest struct {
	RouteRequest
	Strategy      *int
	Waypoints     string
	AvoidPolygons string
	Plate         string
	CarType       *int
	FieldMask
}

type Step struct {
	Instruction  string `json:"instruction"`
	Orientation  string `json:"orientation"`
	RoadName     string `json:"road_name"`
	StepDistance string `json:"step_distance"`
	Duration     string `json:"duration,omitempty"`
}

type Path struct {
	Distance string `json:"distance"`
	Steps    []Step `json:"steps"`
	Duration string `json:"duration,omitempty"`
	Tolls    string `json:"tolls,omitempty"`
}

type Route struct {
	Origin      string `json:"origin"`
	Destination string `json:"destination"`
	Paths       []Path `json:"paths"`
}

type DrivingRouteResponse struct {
	BaseResponse
	Count string `json:"count"`
	Route Route  `json:"route"`
}

func (r *DrivingRouteResponse) Base() *BaseResponse {
	return &r.BaseResponse
}

type WalkingRouteRequest struct {
	RouteRequest
	IsIndoor *int
	FieldMask
}

type WalkingRouteResponse struct {
	BaseResponse
	Count string `json:"count"`
	Route Route  `json:"route"`
}

func (r *WalkingRouteResponse) Base() *BaseResponse {
	return &r.BaseResponse
}

// POI search request/response structures

type TextSearchRequest struct {
	Keywords  string
	Types     string
	Region    string
	CityLimit *bool
	FieldMask
	Pagination
}

type AroundSearchRequest struct {
	Location string
	Keywords string
	Types    string
	Radius   *int
	SortRule string
	FieldMask
	Pagination
}

type PolygonSearchRequest struct {
	Polygon  string
	Keywords string
	Types    string
	FieldMask
	Pagination
}

type DetailSearchRequest struct {
	ID string
	FieldMask
}

type POI struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	Location string `json:"location"`
	Type     string `json:"type"`
	Typecode string `json:"typecode"`
	Pname    string `json:"pname"`
	Cityname string `json:"cityname"`
	Adname   string `json:"adname"`
	Address  string `json:"address"`
	Pcode    string `json:"pcode"`
	Adcode   string `json:"adcode"`
	Citycode string `json:"citycode"`
}

type POISearchResponse struct {
	BaseResponse
	Count string `json:"count"`
	POIs  []POI  `json:"pois"`
}

func (r *POISearchResponse) Base() *BaseResponse {
	return &r.BaseResponse
}

// Geocoding request/response structures

type GeocodeRequest struct {
	Address string
	City    string
}

type Geocode struct {
	FormattedAddress string        `json:"formatted_address"`
	Country          string        `json:"country"`
	Province         string        `json:"province"`
	City             StringOrArray `json:"city"`
	Citycode         string        `json:"citycode"`
	District         string        `json:"district"`
	Street           StringOrArray `json:"street"`
	Number           StringOrArray `json:"number"`
	Adcode           string        `json:"adcode"`
	Location         string        `json:"location"`
	Level            string        `json:"level"`
}

type GeocodeResponse struct {
	BaseResponse
	Count    string    `json:"count"`
	Geocodes []Geocode `json:"geocodes"`
}

func (r *GeocodeResponse) Base() *BaseResponse {
	return &r.BaseResponse
}

type RegeocodeRequest struct {
	Location   string
	PoiType    string
	Radius     *int
	Extensions string
	Roadlevel  *int
	HomeOrCorp *int
}

type AddressComponent struct {
	Country       string         `json:"country"`
	Province      string         `json:"province"`
	City          StringOrArray  `json:"city"`
	Citycode      string         `json:"citycode"`
	District      string         `json:"district"`
	Adcode        string         `json:"adcode"`
	Township      string         `json:"township"`
	Towncode      string         `json:"towncode"`
	Neighborhood  Neighborhood   `json:"neighborhood"`
	Building      Building       `json:"building"`
	StreetNumber  StreetNumber   `json:"streetNumber"`
	SeaArea       string         `json:"seaArea"`
	BusinessAreas []BusinessArea `json:"businessAreas"`
}

type Neighborhood struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Building struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type StreetNumber struct {
	Street    string `json:"street"`
	Number    string `json:"number"`
	Location  string `json:"location"`
	Direction string `json:"direction"`
	Distance  string `json:"distance"`
}

type BusinessArea struct {
	Location string `json:"location"`
	Name     string `json:"name"`
	ID       string `json:"id"`
}

type Road struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Distance  string `json:"distance"`
	Direction string `json:"direction"`
	Location  string `json:"location"`
}

type RoadInter struct {
	Distance   string `json:"distance"`
	Direction  string `json:"direction"`
	Location   string `json:"location"`
	FirstID    string `json:"first_id"`
	FirstName  string `json:"first_name"`
	SecondID   string `json:"second_id"`
	SecondName string `json:"second_name"`
}

type RegeoPOI struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Type         string        `json:"type"`
	Tel          StringOrArray `json:"tel"` // Tel can be string or array
	Distance     string        `json:"distance"`
	Direction    string        `json:"direction"`
	Address      string        `json:"address"`
	Location     string        `json:"location"`
	Businessarea string        `json:"businessarea"`
}

type AOI struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Adcode   string `json:"adcode"`
	Location string `json:"location"`
	Area     string `json:"area"`
	Distance string `json:"distance"`
	Type     string `json:"type"`
}

type Regeocode struct {
	FormattedAddress string           `json:"formatted_address"`
	AddressComponent AddressComponent `json:"addressComponent"`
	Roads            []Road           `json:"roads"`
	RoadInters       []RoadInter      `json:"roadinters"`
	POIs             []RegeoPOI       `json:"pois"`
	AOIs             []AOI            `json:"aois"`
}

type RegeocodeResponse struct {
	BaseResponse
	Regeocode Regeocode `json:"regeocode"`
}

func (r *RegeocodeResponse) Base() *BaseResponse {
	return &r.BaseResponse
}
