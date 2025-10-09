// @description wechat 是腾讯微信公众平台 api 的 golang 语言封装
// @link        https://github.com/chanxuehong/wechat for the canonical source repository
// @license     https://github.com/chanxuehong/wechat/blob/master/LICENSE
// @authors     chanxuehong(chanxuehong@gmail.com)

package jssdk

import (
	"qywx/infrastructures/httplib"
)

type TicketInfo struct {
	Ticket    string `json:"ticket"`
	ExpiresIn int64  `json:"expires_in"` // 有效时间, seconds
}

// JS Ticket
func GetJsTicket(corpToken string) (ticket *TicketInfo, err error) {

	incompleteURL := "https://qyapi.weixin.qq.com/cgi-bin/get_jsapi_ticket?access_token=" + corpToken

	ticket = &TicketInfo{}
	if err = httplib.GetJSON(incompleteURL, ticket); err != nil {
		return nil, err
	}

	return ticket, err
}
