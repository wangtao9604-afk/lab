package controllers

import (
	"io"
	"runtime/debug"

	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	"qywx/infrastructures/log"
	"qywx/infrastructures/tokenstore"
	"qywx/infrastructures/wxmsg/kefu"
	"qywx/infrastructures/wxmsg/wxsuite"
	"qywx/models/message"

	"github.com/gin-gonic/gin"
)

// 消息处理函数
func RecvSuiteMessage(ctx *gin.Context) {
	signature := ctx.Query("msg_signature")
	timestamp := ctx.Query("timestamp")
	nonce := ctx.Query("nonce")
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		log.GetInstance().Sugar.Error("read request met error:", err.Error())
		replyWithError(ctx, common.ReadRequestErr, err.Error())
		return
	}
	data, msgType, err := message.RecvSuiteMessage(body, signature, timestamp, nonce)
	if err != nil {
		log.GetInstance().Sugar.Error("can not parse suite message: ", err.Error())
		log.GetInstance().Sugar.Error("received body is: ", string(body))
		replyWithError(ctx, common.RecvSuiteMsgErr, err.Error())
		return
	}
	failed := false
	errMsg := ""

	switch msgType {
	case wxsuite.SuiteTicket:
		log.GetInstance().Sugar.Debug("msgType: ", "wxsuite.SuiteTicket")
		ticket, ok := data.(*wxsuite.RecvSuiteTicket)
		if ok {
			go func(ticket string, timeout int) {
				defer func() {
					if err := recover(); err != nil {
						debug.PrintStack()
						log.GetInstance().Sugar.Error("*******Panic error:", err)
					}
				}()
				tokenstore.Instance().PutSuiteTicketToRedis(config.GetInstance().SuiteConfig.SuiteId, ticket, timeout)
				ticket, err := tokenstore.Instance().GetSuiteTicketFromRedis(config.GetInstance().SuiteConfig.SuiteId)
				if err != nil {
					log.GetInstance().Sugar.Debug("GetSuiteTicket met error: ", err.Error())
					return
				}
				log.GetInstance().Sugar.Debug("got suite ticket from redis: ", ticket)
			}(ticket.SuiteTicket, common.SuiteTicketTimeOut)
			replyWithOK(ctx)
		} else {
			failed = true
		}
	case wxsuite.GeneralEvent:
		log.GetInstance().Sugar.Debug("msgType: ", "wxsuite.GeneralEvent")
		event, ok := data.(*wxsuite.GeneralEventMessage)
		if ok {
			log.GetInstance().Sugar.Debug("event is: ", event.Event)
			replyWithOK(ctx)
		} else {
			failed = true
		}
	case wxsuite.TextEvent:
		log.GetInstance().Sugar.Debug("msgType: ", "wxsuite.TextEvent")
		text, ok := data.(*wxsuite.TextEventMessage)
		if ok {
			log.GetInstance().Sugar.Debug("event is: ", text.Content)
			replyWithOK(ctx)
		} else {
			failed = true
		}
	case wxsuite.KFMsgOrEvent:
		log.GetInstance().Sugar.Debug("msgType: ", "wxsuite.KFMsgOrEvent")
		callbackEvent, ok := data.(*kefu.KFCallbackMessage)
		if ok {
			log.GetInstance().Sugar.Info("treat as customer service message")
			if err := DeliverCallback(ctx.Request.Context(), callbackEvent); err != nil {
				log.GetInstance().Sugar.Errorf("deliver kf callback failed: %v", err)
				replyWithError(ctx, common.HandleSuiteMsgErr, err.Error())
				return
			}
			replyWithOK(ctx)
		} else {
			failed = true
		}
	default:
		log.GetInstance().Sugar.Error("unsupported msg")
		replyWithError(ctx, common.UnSupportSuiteMsgErr, errMsg)
	}
	if failed {
		replyWithError(ctx, common.HandleSuiteMsgErr, errMsg)
	}
}
