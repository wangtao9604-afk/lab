package corp

import (
	"errors"

	"qywx/infrastructures/common"
	"qywx/infrastructures/httplib"
	"qywx/infrastructures/log"
)

// 发送消息返回的数据结构
type Result struct {
	ErrCode      int    `json:"errcode"`
	ErrMsg       string `json:"errmsg"`
	InvalidUser  string `json:"invaliduser"`
	InvalidParty string `json:"invalidparty,,omitempty"`
	InvalidTag   string `json:"invalidtag,omitempty"`
}

func SendText(msg *Text) (r *Result, err error) {
	if msg == nil {
		err = errors.New("nil msg")
		return
	}
	msg.MsgType = MsgTypeText
	return send(msg, msg.CorpToken)
}

func SendCardText(msg *CardText) (r *Result, err error) {
	if msg == nil {
		err = errors.New("nil msg")
		return
	}
	return send(msg, msg.CorpToken)
}

func SendImage(msg *Image) (r *Result, err error) {
	if msg == nil {
		err = errors.New("nil msg")
		return
	}
	msg.MsgType = MsgTypeImage
	return send(msg, msg.CorpToken)
}

func SendVoice(msg *Voice) (r *Result, err error) {
	if msg == nil {
		err = errors.New("nil msg")
		return
	}
	return send(msg, msg.CorpToken)
}

func SendVideo(msg *Video) (r *Result, err error) {
	if msg == nil {
		err = errors.New("nil msg")
		return
	}
	msg.MsgType = MsgTypeVideo
	return send(msg, msg.CorpToken)
}

func SendFile(msg *File) (r *Result, err error) {
	if msg == nil {
		err = errors.New("nil msg")
		return
	}
	msg.MsgType = MsgTypeFile
	return send(msg, msg.CorpToken)
}

func SendNews(msg *News) (r *Result, err error) {
	if msg == nil {
		err = errors.New("nil msg")
		return
	}
	if err = msg.CheckValid(); err != nil {
		return
	}
	msg.MsgType = MsgTypeNews
	return send(msg, msg.CorpToken)
}

func SendMPNews(msg *MPNews) (r *Result, err error) {
	if msg == nil {
		err = errors.New("nil msg")
		return
	}
	if err = msg.CheckValid(); err != nil {
		return
	}
	msg.MsgType = MsgTypeMPNews
	return send(msg, msg.CorpToken)
}

func send(msg interface{}, token string) (r *Result, err error) {
	r = &Result{}
	incompleteURL := "https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=" + token
	log.GetInstance().Sugar.Debug("post msg is :", msg)
	if err = httplib.PostJSON(incompleteURL, msg, r); err != nil {
		return
	}
	if r.ErrCode != common.ErrCodeOK {
		err = errors.New(r.ErrMsg)
		return
	}

	return
}
