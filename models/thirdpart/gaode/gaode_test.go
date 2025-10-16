package gaode

import (
	"context"
	"errors"
	"testing"

	infra "qywx/infrastructures/gaode"
)

type mockClient struct {
	geocodeResp *infra.GeocodeResponse
	geocodeErr  error
	aroundResp  *infra.POISearchResponse
	aroundErr   error

	lastGeocodeReq *infra.GeocodeRequest
	lastAroundReq  *infra.AroundSearchRequest
}

func (m *mockClient) Geocode(ctx context.Context, req *infra.GeocodeRequest) (*infra.GeocodeResponse, error) {
	m.lastGeocodeReq = req
	if m.geocodeErr != nil {
		return nil, m.geocodeErr
	}
	return m.geocodeResp, nil
}

func (m *mockClient) AroundSearch(ctx context.Context, req *infra.AroundSearchRequest) (*infra.POISearchResponse, error) {
	m.lastAroundReq = req
	if m.aroundErr != nil {
		return nil, m.aroundErr
	}
	return m.aroundResp, nil
}

func TestSearchResidentialPOI_UsesProvidedRadius(t *testing.T) {
	mock := &mockClient{
		geocodeResp: &infra.GeocodeResponse{
			Geocodes: []infra.Geocode{{Location: "116.1,39.1"}},
		},
		aroundResp: &infra.POISearchResponse{},
	}

	service := NewServiceWithClient(mock)
	const expectedRadius = 800

	const city = "上海"

	result, err := service.SearchResidentialPOI(context.Background(), "test address", city, expectedRadius)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatalf("result is nil")
	}

	if mock.lastAroundReq == nil {
		t.Fatalf("around search request was not sent")
	}

	if mock.lastAroundReq.Radius == nil {
		t.Fatalf("radius pointer should not be nil")
	}

	if got := *mock.lastAroundReq.Radius; got != expectedRadius {
		t.Fatalf("radius mismatch, got %d want %d", got, expectedRadius)
	}

	if result.RadiusMeter != expectedRadius {
		t.Fatalf("result radius mismatch, got %d want %d", result.RadiusMeter, expectedRadius)
	}

	if mock.lastAroundReq.Types != residentialPOITypes {
		t.Fatalf("types mismatch, got %s want %s", mock.lastAroundReq.Types, residentialPOITypes)
	}

	if mock.lastAroundReq.SortRule != infra.SortByDistance {
		t.Fatalf("sort rule mismatch, got %s want %s", mock.lastAroundReq.SortRule, infra.SortByDistance)
	}

	if mock.lastGeocodeReq == nil {
		t.Fatalf("geocode request not sent")
	}
	if mock.lastGeocodeReq.City != city {
		t.Fatalf("geocode city mismatch, got %s want %s", mock.lastGeocodeReq.City, city)
	}
}

func TestSearchResidentialPOI_DefaultsRadiusWhenZero(t *testing.T) {
	mock := &mockClient{
		geocodeResp: &infra.GeocodeResponse{
			Geocodes: []infra.Geocode{{Location: "116.1,39.1"}},
		},
		aroundResp: &infra.POISearchResponse{},
	}

	service := NewServiceWithClient(mock)

	result, err := service.SearchResidentialPOI(context.Background(), "test address", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RadiusMeter != defaultResidentialPOIRadius {
		t.Fatalf("default radius mismatch, got %d want %d", result.RadiusMeter, defaultResidentialPOIRadius)
	}

	if mock.lastAroundReq == nil || mock.lastAroundReq.Radius == nil {
		t.Fatalf("around search request radius is nil")
	}

	if got := *mock.lastAroundReq.Radius; got != defaultResidentialPOIRadius {
		t.Fatalf("default radius mismatch, got %d want %d", got, defaultResidentialPOIRadius)
	}
}

func TestSearchResidentialPOI_GeocodeErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	mock := &mockClient{
		geocodeErr: wantErr,
	}

	service := NewServiceWithClient(mock)

	_, err := service.SearchResidentialPOI(context.Background(), "test address", "", 100)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if !errors.Is(err, wantErr) {
		t.Fatalf("error mismatch, got %v want %v", err, wantErr)
	}
}

func TestSearchResidentialPOI_NoGeocodeResult(t *testing.T) {
	mock := &mockClient{
		geocodeResp: &infra.GeocodeResponse{},
	}
	service := NewServiceWithClient(mock)

	_, err := service.SearchResidentialPOI(context.Background(), "test address", "", 100)
	if !errors.Is(err, errNoGeocodeResult) {
		t.Fatalf("expected errNoGeocodeResult, got %v", err)
	}
}
