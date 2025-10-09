package controllers

import (
	"fmt"
	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	"qywx/infrastructures/log"
	"qywx/infrastructures/oauth2"
	"qywx/models/login"

	"github.com/gin-gonic/gin"
)

// APP端登录的回调处理函数
func MobileShowLogin(ctx *gin.Context) {
	code := ctx.Query("code")
	log.GetInstance().Sugar.Debug("code from login: ", code)
	user, err := login.GetUserInfo(code, common.PLATFORM_MOBILE, config.GetInstance().SuiteConfig.SuiteId, "", config.GetInstance().SuiteConfig.Secret)
	if err != nil {
		ctx.String(200, "获取账号信息失败: ", err.Error())
		return
	}

	//根据获得的微信用户Id验证该用户是否在本地系统中有关联账户
	mobileUser := user.(*oauth2.MobileUserInfo)
	output := fmt.Sprintf("Hello %s，您的微信Id: %s,\n您在本地系统的Id: %s 请确保是本人登录。", "Jenny", mobileUser.UserId, "666")
	ctx.String(200, output)
}

func replyWithError(ctx *gin.Context, errCode int, errMsg string) {
	ctx.JSON(200, struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}{
		ErrCode: errCode,
		ErrMsg:  errMsg,
	})
}

func replyWithOK(ctx *gin.Context) {
	ctx.String(200, "success")
}
