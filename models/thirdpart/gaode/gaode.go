package gaode

import (
	"context"
	"errors"
	"fmt"
	"strings"

	infra "qywx/infrastructures/gaode"
)

const (
	// defaultResidentialPOIRadius is the fallback radius (in meters) when caller passes <=0.
	defaultResidentialPOIRadius = 5000

	// residentialPOITypes combines all residential-related POI codes documented in
	// infrastructures/gaode/POI_RESIDENTIAL_CODES.md.
	residentialPOITypes = "120203|120301|120302|120303"
)

var (
	errEmptyAddress        = errors.New("gaode: address must not be empty")
	errNoGeocodeResult     = errors.New("gaode: no geocode result returned")
	errEmptyGeoLocation    = errors.New("gaode: geocode location is empty")
	errNilUnderlyingClient = errors.New("gaode: underlying client is nil")
)

// poiClient defines the subset of the infrastructures/gaode client that we rely on.
type poiClient interface {
	Geocode(ctx context.Context, req *infra.GeocodeRequest) (*infra.GeocodeResponse, error)
	AroundSearch(ctx context.Context, req *infra.AroundSearchRequest) (*infra.POISearchResponse, error)
}

// Service provides high-level helpers for Gaode POI workflows.
type Service struct {
	client poiClient
}

// NewService creates a POI service backed by the default Gaode client configured from config.toml.
func NewService() (*Service, error) {
	client, err := infra.NewClient()
	if err != nil {
		return nil, fmt.Errorf("create gaode client: %w", err)
	}
	return NewServiceWithClient(client), nil
}

// NewServiceWithClient allows injecting a custom Gaode client (primarily for testing).
func NewServiceWithClient(client poiClient) *Service {
	return &Service{client: client}
}

// ResidentialPOIResult holds the outcome of a residential POI search.
type ResidentialPOIResult struct {
	Geocode     infra.Geocode
	POIs        []infra.POI
	RadiusMeter int
	Raw         *infra.POISearchResponse
}

// GeocodeFirst resolves the first geocode result for the given address/city.
func (s *Service) GeocodeFirst(ctx context.Context, address, city string) (*infra.Geocode, error) {
	if s == nil || s.client == nil {
		return nil, errNilUnderlyingClient
	}
	if strings.TrimSpace(address) == "" {
		return nil, errEmptyAddress
	}

	req := &infra.GeocodeRequest{Address: address}
	if trimmed := strings.TrimSpace(city); trimmed != "" {
		req.City = trimmed
	}

	resp, err := s.client.Geocode(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("geocode address %q failed: %w", address, err)
	}
	if len(resp.Geocodes) == 0 {
		return nil, errNoGeocodeResult
	}

	first := resp.Geocodes[0]
	if strings.TrimSpace(first.Location) == "" {
		return nil, errEmptyGeoLocation
	}

	return &first, nil
}

// SearchResidentialPOI takes an address, resolves it to coordinates, and performs an around-search
// limited to residential POI types. When radiusMeter <= 0 it falls back to 5000 meters.
func (s *Service) SearchResidentialPOI(ctx context.Context, address, city string, radiusMeter int) (*ResidentialPOIResult, error) {
	if s == nil || s.client == nil {
		return nil, errNilUnderlyingClient
	}
	if address == "" {
		return nil, errEmptyAddress
	}

	geoReq := &infra.GeocodeRequest{Address: address}
	if trimmed := strings.TrimSpace(city); trimmed != "" {
		geoReq.City = trimmed
	}

	geoResp, err := s.client.Geocode(ctx, geoReq)
	if err != nil {
		return nil, fmt.Errorf("geocode address %q failed: %w", address, err)
	}

	if len(geoResp.Geocodes) == 0 {
		return nil, errNoGeocodeResult
	}

	geo := geoResp.Geocodes[0]
	if geo.Location == "" {
		return nil, errEmptyGeoLocation
	}

	effectiveRadius := radiusMeter
	if effectiveRadius <= 0 {
		effectiveRadius = defaultResidentialPOIRadius
	}

	aroundReq := &infra.AroundSearchRequest{
		Location: geo.Location,
		Types:    residentialPOITypes,
		Radius:   &effectiveRadius,
		SortRule: infra.SortByDistance,
	}

	aroundResp, err := s.client.AroundSearch(ctx, aroundReq)
	if err != nil {
		return nil, fmt.Errorf("poi around search failed: %w", err)
	}

	return &ResidentialPOIResult{
		Geocode:     geo,
		POIs:        aroundResp.POIs,
		RadiusMeter: effectiveRadius,
		Raw:         aroundResp,
	}, nil
}
