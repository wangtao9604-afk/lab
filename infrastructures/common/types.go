package common

type EncodeType int

const (
	_ EncodeType = iota
	XML
	JSON
)

const (
	SuiteTicketTimeOut = 1800
)

const (
	ErrCodeOK = 0
)

type Platform int

const (
	_ Platform = iota
	PLATFORM_WEB
	PLATFORM_MOBILE
	PLATFORM_IGNORE
)

type ParamUnit struct {
	Content       interface{}
	EncodeContent bool
	EncodeTitle   bool
}

// API params
type Params map[string]ParamUnit

// An interface to send http request
type IHttpClient interface {
	SetHttpHeaders(header map[string]string)
	Get(path string) ([]byte, error)
	Post(path string, data []byte, codeType EncodeType) ([]byte, error)
	CombineUrl(path string, params Params) (string, error)
}
