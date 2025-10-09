package httplib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"qywx/infrastructures/log"
)

func PostJSON(url string, request interface{}, response interface{}) (err error) {
	body, err := json.Marshal(request)
	if err != nil {
		log.GetInstance().Sugar.Error("marshal weixin request error:", err)
		return err
	}
	httpResp, err := http.DefaultClient.Post(url, "application/json; charset=utf-8", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		log.GetInstance().Sugar.Error("Post weixin http.Status error:", httpResp.StatusCode)
		return fmt.Errorf("http.Status: %s", httpResp.Status)
	}
	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, httpResp.Body)
	log.GetInstance().Sugar.Debug("post json result body is:", string(buf.Bytes()))
	if err = json.Unmarshal(buf.Bytes(), response); err != nil {
		log.GetInstance().Sugar.Debug("json Unmarshal  error:", err)
		return err
	}
	return nil
}

func GetJSON(url string, response interface{}) (err error) {
	httpResp, err := http.DefaultClient.Get(url)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		log.GetInstance().Sugar.Error("get weixin http.Status error:", httpResp.StatusCode)
		return fmt.Errorf("http.Status: %s", httpResp.Status)
	}
	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, httpResp.Body)
	log.GetInstance().Sugar.Debug("url:", url, " result:", string(buf.Bytes()))
	if err = json.Unmarshal(buf.Bytes(), response); err != nil {
		log.GetInstance().Sugar.Debug("jon Unmarshal  error:", err, " resultBody is:", string(buf.Bytes()), " url:", url)
		return err
	}
	return nil
}
