package gaode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"qywx/infrastructures/config"
)

// API path constants
const (
	DrivingRoutePath  = "/v5/direction/driving"
	WalkingRoutePath  = "/v5/direction/walking"
	TextSearchPath    = "/v5/place/text"
	AroundSearchPath  = "/v5/place/around"
	PolygonSearchPath = "/v5/place/polygon"
	DetailSearchPath  = "/v5/place/detail"
	GeocodePath       = "/v3/geocode/geo"
	RegeocodePath     = "/v3/geocode/regeo"
)

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a Gaode client from global config
func NewClient() (*Client, error) {
	cfg := config.GetInstance().GaodeConfig
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gaode api key is required")
	}
	return NewClientWithConfig(cfg.APIKey, cfg.BaseURL, cfg.Timeout), nil
}

// NewClientWithConfig creates a Gaode client with custom parameters
func NewClientWithConfig(apiKey, baseURL string, timeoutSec int) *Client {
	if baseURL == "" {
		baseURL = "https://restapi.amap.com"
	}
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
	}
}

// Route planning methods

func (c *Client) DrivingRoute(ctx context.Context, req *DrivingRouteRequest) (*DrivingRouteResponse, error) {
	params := newParamsBuilder().
		Str("key", c.apiKey).
		Str("origin", req.Origin).
		Str("destination", req.Destination).
		Str("origin_id", req.OriginID).
		Str("destination_id", req.DestinationID).
		IntPtr("strategy", req.Strategy).
		Str("waypoints", req.Waypoints).
		Str("avoidpolygons", req.AvoidPolygons).
		Str("plate", req.Plate).
		IntPtr("cartype", req.CarType).
		Str("show_fields", req.ShowFields)

	resp := &DrivingRouteResponse{}
	if err := c.doGet(ctx, c.baseURL+DrivingRoutePath, params, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) WalkingRoute(ctx context.Context, req *WalkingRouteRequest) (*WalkingRouteResponse, error) {
	params := newParamsBuilder().
		Str("key", c.apiKey).
		Str("origin", req.Origin).
		Str("destination", req.Destination).
		Str("origin_id", req.OriginID).
		Str("destination_id", req.DestinationID).
		IntPtr("isindoor", req.IsIndoor).
		Str("show_fields", req.ShowFields)

	resp := &WalkingRouteResponse{}
	if err := c.doGet(ctx, c.baseURL+WalkingRoutePath, params, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

// POI search methods

func (c *Client) TextSearch(ctx context.Context, req *TextSearchRequest) (*POISearchResponse, error) {
	params := newParamsBuilder().
		Str("key", c.apiKey).
		Str("keywords", req.Keywords).
		Str("types", req.Types).
		Str("region", req.Region).
		BoolPtr("city_limit", req.CityLimit).
		Str("show_fields", req.ShowFields).
		IntPtr("page_size", req.PageSize).
		IntPtr("page_num", req.PageNum)

	resp := &POISearchResponse{}
	if err := c.doGet(ctx, c.baseURL+TextSearchPath, params, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) AroundSearch(ctx context.Context, req *AroundSearchRequest) (*POISearchResponse, error) {
	params := newParamsBuilder().
		Str("key", c.apiKey).
		Str("location", req.Location).
		Str("keywords", req.Keywords).
		Str("types", req.Types).
		IntPtr("radius", req.Radius).
		Str("sortrule", req.SortRule).
		Str("show_fields", req.ShowFields).
		IntPtr("page_size", req.PageSize).
		IntPtr("page_num", req.PageNum)

	resp := &POISearchResponse{}
	if err := c.doGet(ctx, c.baseURL+AroundSearchPath, params, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) PolygonSearch(ctx context.Context, req *PolygonSearchRequest) (*POISearchResponse, error) {
	params := newParamsBuilder().
		Str("key", c.apiKey).
		Str("polygon", req.Polygon).
		Str("keywords", req.Keywords).
		Str("types", req.Types).
		Str("show_fields", req.ShowFields).
		IntPtr("page_size", req.PageSize).
		IntPtr("page_num", req.PageNum)

	resp := &POISearchResponse{}
	if err := c.doGet(ctx, c.baseURL+PolygonSearchPath, params, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) DetailSearch(ctx context.Context, req *DetailSearchRequest) (*POISearchResponse, error) {
	params := newParamsBuilder().
		Str("key", c.apiKey).
		Str("id", req.ID).
		Str("show_fields", req.ShowFields)

	resp := &POISearchResponse{}
	if err := c.doGet(ctx, c.baseURL+DetailSearchPath, params, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

// Geocoding methods

func (c *Client) Geocode(ctx context.Context, req *GeocodeRequest) (*GeocodeResponse, error) {
	params := newParamsBuilder().
		Str("key", c.apiKey).
		Str("address", req.Address).
		Str("city", req.City)

	resp := &GeocodeResponse{}
	if err := c.doGet(ctx, c.baseURL+GeocodePath, params, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *Client) Regeocode(ctx context.Context, req *RegeocodeRequest) (*RegeocodeResponse, error) {
	params := newParamsBuilder().
		Str("key", c.apiKey).
		Str("location", req.Location).
		Str("poitype", req.PoiType).
		IntPtr("radius", req.Radius).
		Str("extensions", req.Extensions).
		IntPtr("roadlevel", req.Roadlevel).
		IntPtr("homeorcorp", req.HomeOrCorp)

	resp := &RegeocodeResponse{}
	if err := c.doGet(ctx, c.baseURL+RegeocodePath, params, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

// doGet performs GET request and checks response status
func (c *Client) doGet(ctx context.Context, baseURL string, params *paramsBuilder, response interface{}) error {
	urlStr := params.BuildURL(baseURL)

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body failed: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status %d: %s", resp.StatusCode, string(body))
	}

	// Unmarshal JSON
	if err := json.Unmarshal(body, response); err != nil {
		return fmt.Errorf("unmarshal response failed: %w", err)
	}

	// Check if response implements BaseResponse interface
	if baseResp, ok := response.(interface {
		IsSuccess() bool
		Base() *BaseResponse
	}); ok {
		if !baseResp.IsSuccess() {
			base := baseResp.Base()
			return fmt.Errorf("gaode api error: %s - %s", base.InfoCode, base.Info)
		}
	} else if baseResp, ok := response.(interface{ IsSuccess() bool }); ok {
		if !baseResp.IsSuccess() {
			return fmt.Errorf("gaode api error")
		}
	}

	return nil
}
