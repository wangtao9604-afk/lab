package controllers

import (
	"encoding/json"
	"io"
	"qywx/infrastructures/common"
	"qywx/infrastructures/log"

	"github.com/gin-gonic/gin"
)

func HandleUpstreamReq(ctx *gin.Context) {
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		log.GetInstance().Sugar.Error("read request met error:", err.Error())
		replyWithError(ctx, common.ReadRequestErr, err.Error())
		return
	}

	req := &struct {
		UserName string `json:"userName"`
		Password string `json:"passWord"`
	}{}

	err = json.Unmarshal(body, req)
	if err != nil {
		log.GetInstance().Sugar.Error("unmarshal met error:", err.Error())
		replyWithError(ctx, common.UnmarshalErr, err.Error())
		return
	}

	reply := &struct {
		LoginResult string `json:"loginResult"`
		UserName    string `json:"userName"`
		Password    string `json:"passWord"`
	}{
		LoginResult: "jjyy",
		UserName:    req.UserName,
		Password:    req.Password,
	}

	ctx.JSON(200, reply)
}
