package message

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	"qywx/infrastructures/tokenstore"
	"qywx/infrastructures/wxmsg/corp"

	"qywx/infrastructures/log"
)

func SendSelfMsgMobile(toUserIds []string, content string) error {
	toUser, err := buildToUserList(toUserIds)
	if err != nil {
		return err
	}
	msg := corp.Text{}
	msg.ToUser = toUser
	msg.MsgType = "txt"
	msg.AgentId, _ = strconv.Atoi(config.GetInstance().SuiteConfig.AgentId)
	msg.Text.Content = content
	token, err := tokenstore.Instance().FetchCorpToken(common.PLATFORM_MOBILE, config.GetInstance().SuiteConfig.SuiteId,
		config.GetInstance().SuiteConfig.SuiteId,
		"", config.GetInstance().SuiteConfig.Secret)
	if err != nil {
		return err
	}
	msg.CorpToken = token
	r, err := corp.SendText(&msg)
	if err != nil {
		log.GetInstance().Sugar.Debug("send msg err:", err, " r:", r)
	}
	return err
}

// 发送文本消息
func SendMsgTxt(toUserIds []string, platform common.Platform, content string, agentId int, corpToken string) error {
	toUser, err := buildToUserList(toUserIds)
	if err != nil {
		return err
	}
	msg := corp.Text{}
	msg.ToUser = toUser
	msg.MsgType = "txt"
	msg.AgentId = agentId
	msg.CorpToken = corpToken
	msg.Text.Content = content
	r, err := corp.SendText(&msg)
	if err != nil {
		log.GetInstance().Sugar.Debug("send msg err:", err, " r:", r)
	}
	return err
}

// 发送卡片式文本消息
// param toparty 预留给将来使用
func SendCardMsg(toUserIds []string, msgId, title string, agentId int, corpToken, corpId string) error {
	toUser, err := buildToUserList(toUserIds)
	if err != nil {
		return err
	}

	msg := corp.CardText{}
	msg.ToUser = toUser
	msg.MsgType = "textcard"
	// TODO: get agent id from config
	msg.AgentId = agentId
	// TODO: get corp token from redis
	msg.CorpToken = corpToken
	msg.TextCard.Title = title
	msg.TextCard.Description = buildDescription(title, "normal")
	msg.TextCard.URL = buildUrl(msgId, corpId, agentId)
	r, err := corp.SendCardText(&msg)
	if err != nil {
		log.GetInstance().Sugar.Debug("send msg err:", err, " r:", r)
	}
	return err
}

// 发送图文消息
// toUsersId 为nil时发送给所有人
func SendMsgNews(toUserIds []string, newsArticeles []corp.NewsArticle, corpToken string, agentId int) error {
	toUser, err := buildToUserList(toUserIds)
	if err != nil {
		return err
	}

	msg := corp.News{}
	msg.ToUser = toUser
	msg.MsgType = "news"
	// TODO: get agentId from config
	msg.AgentId = agentId
	// TODO: get corpToken from redis
	msg.CorpToken = corpToken
	msg.News.Articles = newsArticeles
	r, err := corp.SendNews(&msg)
	if err != nil {
		log.GetInstance().Sugar.Debug("send msg new err:", err, " r:", r)
	}
	return err
}

func buildDescription(title, style string) string {
	var content string
	if style == "normal" {
		content = title
	} else {
		content = fmt.Sprintf(`<div class="%s">%s</div>`, "gray", title)
	}
	log.GetInstance().Sugar.Debug("content is:", content)
	return content
}

func buildUrl(key, corpId string, agentId int) string {
	url := fmt.Sprintf("%s/api/v2/message/%s?corpid=%s&agentid=%v", config.GetInstance().Domain, key, corpId, agentId)
	log.GetInstance().Sugar.Debug("Url is:", url)
	return url
}

func buildToUserList(userIds []string) (string, error) {
	var toUser string
	if len(userIds) > 0 {
		if len(userIds) > 1000 {
			return "", errors.New("接收对象超过1000个")
		}
		log.GetInstance().Sugar.Debug("userIds:", userIds)
		toUser = strings.Join(userIds, "|")
	} else {
		toUser = "@all"
	}
	return toUser, nil
}
