# Gaode Map API Client

高德地图 Web 服务 API 的 Go 语言封装库。

## 功能特性

### 地理编码
- 地理编码 (Geocode) - 地址转坐标
- 逆地理编码 (Regeocode) - 坐标转地址

### 路线规划
- 驾车路线规划 (DrivingRoute)
- 步行路线规划 (WalkingRoute)
- 支持策略选择、途经点、避让区域等高级功能

### POI 搜索
- 关键字搜索 (TextSearch)
- 周边搜索 (AroundSearch)
- 多边形区域搜索 (PolygonSearch)
- ID 详情查询 (DetailSearch)

## 快速开始

### 初始化客户端

```go
import (
    "context"
    "qywx/infrastructures/gaode"
)

client := gaode.NewClient("your_gaode_api_key")
ctx := context.Background()
```

### 地理编码（地址转坐标）

```go
req := &gaode.GeocodeRequest{
    Address: "北京市朝阳区阜通东大街6号",
    City:    "北京",
}

resp, err := client.Geocode(ctx, req)
if err != nil {
    log.Fatal(err)
}

if len(resp.Geocodes) > 0 {
    geo := resp.Geocodes[0]
    fmt.Printf("坐标: %s\n", geo.Location)
    fmt.Printf("地址: %s\n", geo.FormattedAddress)
}
```

### 逆地理编码（坐标转地址）

```go
radius := 1000
req := &gaode.RegeocodeRequest{
    Location:   "116.310003,39.991957",
    Radius:     &radius,
    Extensions: gaode.ExtensionsAll, // 返回详细信息
}

resp, err := client.Regeocode(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("地址: %s\n", resp.Regeocode.FormattedAddress)
fmt.Printf("省份: %s\n", resp.Regeocode.AddressComponent.Province)
fmt.Printf("城市: %s\n", resp.Regeocode.AddressComponent.City.String())
```

**注意**: `City` 字段使用 `StringOrArray` 类型,因为高德在直辖市场景下可能返回空数组。使用 `.String()` 方法获取字符串值。

### 驾车路线规划

```go
req := &gaode.DrivingRouteRequest{
    RouteRequest: gaode.RouteRequest{
        Origin:      "116.434307,39.90909",
        Destination: "116.434446,39.90816",
    },
    Strategy: 32, // 高德推荐
}

resp, err := client.DrivingRoute(req)
if err != nil {
    log.Fatal(err)
}

for _, path := range resp.Route.Paths {
    fmt.Printf("距离: %s 米\n", path.Distance)
    for _, step := range path.Steps {
        fmt.Printf("  - %s\n", step.Instruction)
    }
}
```

### POI 关键字搜索

```go
req := &gaode.TextSearchRequest{
    Keywords: "北京大学",
    Region:   "北京市",
    PageSize: 10,
}

resp, err := client.TextSearch(req)
if err != nil {
    log.Fatal(err)
}

for _, poi := range resp.POIs {
    fmt.Printf("%s - %s\n", poi.Name, poi.Address)
}
```

### POI 周边搜索

```go
req := &gaode.AroundSearchRequest{
    Location: "116.473168,39.993015",
    Radius:   5000, // 5公里
    Types:    "050000", // 餐饮服务
}

resp, err := client.AroundSearch(req)
```

## API 参数说明

### 驾车策略 (Strategy)

- `0`: 速度优先
- `1`: 费用优先
- `2`: 常规最快
- `32`: 默认，高德推荐
- `33`: 躲避拥堵
- `34`: 高速优先
- `35`: 不走高速
- `36`: 少收费
- `37`: 大路优先

### 坐标格式

所有坐标均为 `经度,纬度` 格式，例如：`116.434307,39.90909`

### 分页参数

- `PageSize`: 每页数据量，取值 1-25，默认 10
- `PageNum`: 页码，从 1 开始

## 错误处理

所有 API 方法返回 `error`，建议检查：

```go
resp, err := client.TextSearch(req)
if err != nil {
    // 处理网络错误或 API 错误
    log.Printf("API error: %v", err)
    return
}

// API 成功返回
```

## 依赖

- `qywx/infrastructures/httplib`: 项目 HTTP 客户端库

## 许可

内部使用
