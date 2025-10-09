package controllers

import (
	"qywx/infrastructures/common"
	"qywx/infrastructures/log"
	"qywx/models/message"

	"github.com/gin-gonic/gin"
)

func VerifyUrl(ctx *gin.Context) {
	rev_signature := ctx.Query("msg_signature")
	timestamp := ctx.Query("timestamp")
	nonce := ctx.Query("nonce")
	encrypt_message := ctx.Query("echostr")

	log.GetInstance().Sugar.Debug("received rev_signature is: ", rev_signature)
	log.GetInstance().Sugar.Debug("timestamp is: ", timestamp)
	log.GetInstance().Sugar.Debug("nonce is: ", nonce)
	log.GetInstance().Sugar.Debug("encrypt_message is: ", encrypt_message)

	echoData, err := message.VerifySuiteUrl(rev_signature, timestamp, nonce, encrypt_message)
	if err != nil {
		replyWithError(ctx, common.VerifyUrlErr, err.Error())
		return
	}

	log.GetInstance().Sugar.Debug("decrypt_data is: ", string(echoData))

	//output decrypted data
	ctx.Writer.Write(echoData)
}
