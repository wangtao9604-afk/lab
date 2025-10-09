package httplib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"qywx/infrastructures/common"
	"qywx/infrastructures/log"
)

const (
	MaxIdleConns        int = 100
	MaxIdleConnsPerHost int = 100
	IdleConnTimeout     int = 90
)

type Params map[string]interface{}

// Encode params to query string
func (params Params) EncodeUrlParam(writer io.Writer) {
	if params == nil || len(params) == 0 {
		return
	}

	written := false

	for k, v := range params {
		if written {
			io.WriteString(writer, "&")
		}

		io.WriteString(writer, url.QueryEscape(k))
		io.WriteString(writer, "=")

		if reflect.TypeOf(v).Kind() == reflect.String {
			io.WriteString(writer, url.QueryEscape(reflect.ValueOf(v).String()))
		} else {
			jsonStr, err := json.Marshal(v)

			if err != nil {
				return
			}

			io.WriteString(writer, url.QueryEscape(string(jsonStr)))
		}

		written = true
	}
}

type Session struct {
	HttpHeaders map[string]string
}

type errorReply struct {
	ErrorCode int    `json:"errcode"`
	ErrorMsg  string `json:"errmsg"`
}

func NewSession() *Session {
	session := &Session{
		HttpHeaders: map[string]string{},
	}
	return session
}

func (session *Session) Get(path string) ([]byte, error) {
	return session.sendGetRequest(path)
}

func (session *Session) GetWithParams(path string, params Params) ([]byte, error) {
	empty := []byte{}
	urlStr := session.getUrl(path, params)
	log.GetInstance().Sugar.Debug("url is:", urlStr)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		log.GetInstance().Sugar.Error("Error Occured. %+v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// use httpClient to send request
	client := createHTTPClient()
	response, err := client.Do(req)
	if err != nil && response == nil {
		log.GetInstance().Sugar.Error("Error sending request to API endpoint. %+v", err)
		return empty, err
	} else {
		// Close the connection to reuse it
		defer response.Body.Close()

		// Let's check if the work actually is done
		// We have seen inconsistencies even when we get 200 OK response
		body, err := io.ReadAll(response.Body)
		if err != nil {
			log.GetInstance().Sugar.Error("Couldn't parse response body. %+v", err)
			return empty, err
		}
		return body, err
	}
}

func (session *Session) Post(path string, data []byte, codeType common.EncodeType) ([]byte, error) {
	return session.sendPostRequest(path, data, codeType)
}

func (session *Session) SetHttpHeaders(headers map[string]string) {
	for k, _ := range session.HttpHeaders {
		delete(session.HttpHeaders, k)
	}
	for k, v := range headers {
		session.HttpHeaders[k] = v
	}
}

func (session *Session) CombineUrl(path string, params common.Params) (string, error) {
	buf := &bytes.Buffer{}
	buf.WriteString(path)

	if params != nil {
		buf.WriteString("?")

		written := false
		for k, v := range params {
			if v.Content == nil {
				continue
			}
			if written {
				buf.WriteString("&")
			}
			if v.EncodeTitle {
				buf.WriteString(url.QueryEscape(k))
			} else {
				buf.WriteString(k)
			}
			buf.WriteString("=")

			if reflect.TypeOf(v.Content).Kind() == reflect.String {
				if v.EncodeContent {
					buf.WriteString(url.QueryEscape(reflect.ValueOf(v.Content).String()))
				} else {
					buf.WriteString(reflect.ValueOf(v.Content).String())
				}
			} else {
				jsonStr, err := json.Marshal(v.Content)
				if err != nil {
					return path, err
				}
				if v.EncodeContent {
					buf.WriteString(url.QueryEscape(string(jsonStr)))
				} else {
					buf.WriteString(string(jsonStr))
				}
			}
			written = true
		}
	}
	return buf.String(), nil
}

func (session *Session) sendPostRequest(url string, data []byte, codeType common.EncodeType) ([]byte, error) {
	buf := &bytes.Buffer{}
	empty := []byte{}
	buf.Write(data)

	var request *http.Request
	request, err := http.NewRequest("POST", url, buf)
	if err != nil {
		return empty, err
	}

	for k, v := range session.HttpHeaders {
		request.Header.Set(k, v)
	}
	if codeType == common.JSON {
		fmt.Println("type is Json")
		request.Header.Set("Content-Type", "application/json")
	} else if codeType == common.XML {
		fmt.Println("type is xml")
		request.Header.Set("Content-Type", "application/xml")
	}

	return session.sendRequest(request)
}

func (session *Session) sendGetRequest(url string) ([]byte, error) {
	empty := []byte{}

	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return empty, err
	}

	for k, v := range session.HttpHeaders {
		request.Header.Set(k, v)
	}
	return session.sendRequest(request)
}

func (session *Session) sendRequest(request *http.Request) ([]byte, error) {
	empty := []byte{}
	response, err := http.DefaultClient.Do(request)

	if err != nil {
		return empty, err
	}

	defer response.Body.Close()

	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, response.Body)

	if err != nil {
		return empty, err
	}

	ret := &errorReply{}
	if err := json.Unmarshal(buf.Bytes(), ret); err == nil {
		if ret.ErrorCode > 0 {
			return empty, fmt.Errorf("errcode is: %d, errmsg is: %s", ret.ErrorCode, ret.ErrorMsg)
		}
	}

	return buf.Bytes(), nil
}

func (session *Session) getUrl(path string, params Params) string {
	buf := &bytes.Buffer{}
	buf.WriteString(path)

	if params != nil {
		buf.WriteRune('?')

		params.EncodeUrlParam(buf)
	}

	return buf.String()
}

func createHTTPClient() *http.Client {
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        MaxIdleConns,
			MaxIdleConnsPerHost: MaxIdleConnsPerHost,
			IdleConnTimeout:     time.Duration(IdleConnTimeout) * time.Second,
		},
	}
	return client
}
