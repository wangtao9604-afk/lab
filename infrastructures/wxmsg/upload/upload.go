package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"

	"qywx/infrastructures/log"
)

const (
	MaterialUploadTemporaryURL = "https://qyapi.weixin.qq.com/cgi-bin/media/upload?access_token=%s&type=%s"
	MaterialGetTemporaryURL    = "https://qyapi.weixin.qq.com/cgi-bin/media/get?access_token=%s&media_id=%s"
	WeixinErrCodeSystemBusy    = -1
	WeixinErrCodeSuccess       = 0
)

func UploadTemporaryMaterial(ctx context.Context, accessToken, mtype string, file *os.File, displayName string) (mediaId, createAt string, err error) {
	url := fmt.Sprintf(MaterialUploadTemporaryURL, accessToken, mtype)
	wrapper := &struct {
		ErrCode   int    `json:"errcode"`
		ErrMsg    string `json:"errmsg"`
		Type      string `json:"type"`
		MediaID   string `json:"media_id"`
		CreatedAt string `json:"created_at"`
	}{}

	err = upload(ctx, url, "media", file, wrapper, displayName)
	if err == nil {
		// 进一步检查临时材料是否上传成功
		log.GetInstance().Sugar.Debug("进一步检查临时材料是否上传成功")
		if wrapper.ErrCode != WeixinErrCodeSuccess {
			// 保留完整的错误信息，包括errmsg
			err = fmt.Errorf("微信API错误: errcode=%d, errmsg=%s", wrapper.ErrCode, wrapper.ErrMsg)
		}
	}
	return wrapper.MediaID, wrapper.CreatedAt, err
}


func upload(ctx context.Context, url, fieldName string, file *os.File, ret interface{}, displayName string, desc ...string) (err error) {
	// 确保文件指针在开始位置，防止之前被读取过
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("重置文件指针失败: %w", err)
	}

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %w", err)
	}

	// 文件大小验证（根据微信文档，临时素材大小限制）
	fileSize := fileInfo.Size()
	if fileSize > 20*1024*1024 { // 20MB限制
		return fmt.Errorf("文件大小超出限制，最大支持20MB，当前文件: %d字节", fileSize)
	}

	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)

	// 使用displayName作为用户看到的文件名，如果为空则使用原文件名
	fileName := displayName
	if fileName == "" {
		fileName = file.Name()
	}
	fw, err := w.CreateFormFile(fieldName, fileName)
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, file)
	if err != nil {
		return err
	}

	// 添加filelength字段（微信文档要求）
	err = w.WriteField("filelength", strconv.FormatInt(fileSize, 10))
	if err != nil {
		return fmt.Errorf("添加filelength字段失败: %w", err)
	}

	contentType := w.FormDataContentType()
	if len(desc) > 0 {
		w.WriteField("description", desc[0])
	}
	w.Close()

	info := fmt.Sprintf("url=%s, fieldName=%s, fileName=%s", url, fieldName, fileName)
	log.GetInstance().Sugar.Debug(info)
	
	// 使用context创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, buf)
	if err != nil {
		return fmt.Errorf("创建HTTP请求失败: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP请求失败，状态码: %d", resp.StatusCode)
	}

	err = json.NewDecoder(resp.Body).Decode(ret)
	if err != nil {
		return err
	}
	return nil
}
