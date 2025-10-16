package gaode

import (
	"net/url"
	"strconv"
)

// paramsBuilder simplifies URL parameter building
type paramsBuilder struct {
	values url.Values
}

func newParamsBuilder() *paramsBuilder {
	return &paramsBuilder{
		values: url.Values{},
	}
}

// Str adds a string parameter if value is not empty
func (b *paramsBuilder) Str(key, value string) *paramsBuilder {
	if value != "" {
		b.values.Set(key, value)
	}
	return b
}

// Int adds an int parameter if value is not nil
func (b *paramsBuilder) IntPtr(key string, value *int) *paramsBuilder {
	if value != nil {
		b.values.Set(key, strconv.Itoa(*value))
	}
	return b
}

// BoolPtr adds a bool parameter if value is not nil
func (b *paramsBuilder) BoolPtr(key string, value *bool) *paramsBuilder {
	if value != nil {
		b.values.Set(key, strconv.FormatBool(*value))
	}
	return b
}

// Build returns the encoded query string
func (b *paramsBuilder) Build() string {
	return b.values.Encode()
}

// BuildURL constructs full URL with parameters
func (b *paramsBuilder) BuildURL(baseURL string) string {
	if len(b.values) == 0 {
		return baseURL
	}
	return baseURL + "?" + b.Build()
}
