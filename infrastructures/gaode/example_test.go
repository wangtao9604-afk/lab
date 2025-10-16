package gaode

import (
	"context"
	"fmt"
	"testing"
)

// Example usage for driving route planning
func ExampleClient_DrivingRoute() {
	client := NewClientWithConfig("your_api_key_here", "https://restapi.amap.com", 10)
	ctx := context.Background()

	strategy := int(StrategyRecommended)
	req := &DrivingRouteRequest{
		RouteRequest: RouteRequest{
			Origin:      "116.434307,39.90909",
			Destination: "116.434446,39.90816",
		},
		Strategy: &strategy,
	}

	resp, err := client.DrivingRoute(ctx, req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %s routes\n", resp.Count)
	for i, path := range resp.Route.Paths {
		fmt.Printf("Route %d: distance=%s\n", i+1, path.Distance)
	}
}

// Example usage for walking route planning
func ExampleClient_WalkingRoute() {
	client := NewClientWithConfig("your_api_key_here", "https://restapi.amap.com", 10)
	ctx := context.Background()

	isIndoor := 0
	req := &WalkingRouteRequest{
		RouteRequest: RouteRequest{
			Origin:      "116.466485,39.995197",
			Destination: "116.46424,40.020642",
		},
		IsIndoor: &isIndoor,
	}

	resp, err := client.WalkingRoute(ctx, req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %s routes\n", resp.Count)
}

// Example usage for text search
func ExampleClient_TextSearch() {
	client := NewClientWithConfig("your_api_key_here", "https://restapi.amap.com", 10)
	ctx := context.Background()

	pageSize := 10
	pageNum := 1
	req := &TextSearchRequest{
		Keywords: "北京大学",
		Types:    "141201",
		Region:   "北京市",
		Pagination: Pagination{
			PageSize: &pageSize,
			PageNum:  &pageNum,
		},
	}

	resp, err := client.TextSearch(ctx, req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %s POIs\n", resp.Count)
	for _, poi := range resp.POIs {
		fmt.Printf("POI: %s at %s\n", poi.Name, poi.Location)
	}
}

// Example usage for around search
func ExampleClient_AroundSearch() {
	client := NewClientWithConfig("your_api_key_here", "https://restapi.amap.com", 10)
	ctx := context.Background()

	radius := 10000
	pageSize := 10
	req := &AroundSearchRequest{
		Location: "116.473168,39.993015",
		Radius:   &radius,
		Types:    "011100",
		Pagination: Pagination{
			PageSize: &pageSize,
		},
	}

	resp, err := client.AroundSearch(ctx, req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %s POIs nearby\n", resp.Count)
}

// Example usage for polygon search
func ExampleClient_PolygonSearch() {
	client := NewClientWithConfig("your_api_key_here", "https://restapi.amap.com", 10)
	ctx := context.Background()

	pageSize := 10
	req := &PolygonSearchRequest{
		Polygon:  "116.460988,40.006919|116.48231,40.007381|116.47516,39.99713|116.460988,40.006919",
		Keywords: "肯德基",
		Types:    "050301",
		Pagination: Pagination{
			PageSize: &pageSize,
		},
	}

	resp, err := client.PolygonSearch(ctx, req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %s POIs in polygon\n", resp.Count)
}

// Example usage for detail search
func ExampleClient_DetailSearch() {
	client := NewClientWithConfig("your_api_key_here", "https://restapi.amap.com", 10)
	ctx := context.Background()

	req := &DetailSearchRequest{
		ID: "B000A7BM4H",
	}

	resp, err := client.DetailSearch(ctx, req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if len(resp.POIs) > 0 {
		poi := resp.POIs[0]
		fmt.Printf("POI Details: %s at %s\n", poi.Name, poi.Address)
	}
}

func TestClientCreation(t *testing.T) {
	client := NewClientWithConfig("test_key", "https://restapi.amap.com", 5)
	if client == nil {
		t.Error("Failed to create client")
	}
	if client.apiKey != "test_key" {
		t.Error("API key not set correctly")
	}
	if client.baseURL != "https://restapi.amap.com" {
		t.Error("BaseURL not set correctly")
	}
	if client.httpClient == nil {
		t.Error("HTTP client not initialized")
	}
}

func TestParamsBuilder(t *testing.T) {
	builder := newParamsBuilder()

	pageSize := 10
	cityLimit := true

	builder.
		Str("key", "test_key").
		Str("keywords", "test").
		IntPtr("page_size", &pageSize).
		BoolPtr("city_limit", &cityLimit)

	result := builder.Build()

	if result == "" {
		t.Error("Params should not be empty")
	}
}

// Example usage for geocoding
func ExampleClient_Geocode() {
	client := NewClientWithConfig("your_api_key_here", "https://restapi.amap.com", 10)
	ctx := context.Background()

	req := &GeocodeRequest{
		Address: "北京市朝阳区阜通东大街6号",
		City:    "北京",
	}

	resp, err := client.Geocode(ctx, req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if len(resp.Geocodes) > 0 {
		geo := resp.Geocodes[0]
		fmt.Printf("Location: %s\n", geo.Location)
		fmt.Printf("Address: %s\n", geo.FormattedAddress)
	}
}

// Example usage for reverse geocoding
func ExampleClient_Regeocode() {
	client := NewClientWithConfig("your_api_key_here", "https://restapi.amap.com", 10)
	ctx := context.Background()

	radius := 1000
	req := &RegeocodeRequest{
		Location:   "116.310003,39.991957",
		Radius:     &radius,
		Extensions: ExtensionsAll,
	}

	resp, err := client.Regeocode(ctx, req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Address: %s\n", resp.Regeocode.FormattedAddress)
	fmt.Printf("Province: %s\n", resp.Regeocode.AddressComponent.Province)
	fmt.Printf("City: %s\n", resp.Regeocode.AddressComponent.City.String())
	fmt.Printf("District: %s\n", resp.Regeocode.AddressComponent.District)
}
