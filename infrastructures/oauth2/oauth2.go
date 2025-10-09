package oauth2

import (
	"encoding/json"
	"net/url"
	"qywx/infrastructures/httplib"
)

// OAuth2 移动端登录是返回的用户信息
type MobileUserInfo struct {
	UserId     string `json:"UserId"`
	OpenId     string `json:"OpenId"`
	DeviceId   string `json:"DeviceId"`
	UserTicket string `json:"user_ticket"`
	ExpiresIn  int    `json:"expires_in"`
}

type ServiceUserInfo struct {
	UserType int              `json:"usertype"`
	UserInfo *CorpUserInfo    `json:"user_info"`
	CorpInfo *CorpInfo        `json:"corp_info"`
	Agent    []*CorpAgent     `json:"agent"`
	AuthInfo *CorpDepartments `json:"auth_info"`
}

type CorpUserInfo struct {
	UserId string `json:"userid"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
	Email  string `json:"email"`
}
type CorpInfo struct {
	CorpId string `json:"corpid"`
}
type CorpAgent struct {
	AgentId  int `json:"agentid"`
	AuthType int `json:"auth_type"`
}

type CorpDepartments struct {
	Department []*CorpDepartment `json:"department"`
}
type CorpDepartment struct {
	Id       int  `json:"id"`
	Writable bool `json:"writable"`
}

// OAuth2 信息返回，从企业微信中进入时的登陆
type UserResult struct {
	UserId   string `json:"UserId"`
	DeviceId string `json:"DeviceId"`
	ErrCode  int    `json:"errcode"`
	ErrMsg   string `json:"errmsg"`
}

// 构造获取code的URL.
//
//	corpId:      企业的CorpID
//	redirectURL: 授权后重定向的回调链接地址, 员工点击后, 页面将跳转至
//	             redirect_uri/?code=CODE&state=STATE, 企业可根据code参数获得员工的userid.
//	scope:       应用授权作用域, 此时固定为: snsapi_base
//	state:       重定向后会带上state参数, 企业可以填写a-zA-Z0-9的参数值, 长度不可超过128个字节
//
// agentid:agentid
func AuthCodeURL(corpId, agentid, redirectURL, scope, state string) string {
	reUrl := "https://open.weixin.qq.com/connect/oauth2/authorize" +
		"?appid=" + url.QueryEscape(corpId) +
		"&redirect_uri=" + url.QueryEscape(redirectURL) +
		"&response_type=code&scope=" + url.QueryEscape(scope) +
		"&state=" + url.QueryEscape(state)
	if len(agentid) > 0 {
		reUrl += "&agentid=" + url.QueryEscape(agentid)
	}
	reUrl += "#wechat_redirect"
	return reUrl
}

// OAuth2 信息返回，从企业微信中进入时的登陆
func GetUserId(code string, corpToken string) (info *UserResult, err error) {
	incompleteURL := "https://qyapi.weixin.qq.com/cgi-bin/user/getuserinfo?code=" + url.QueryEscape(code) +
		"&access_token=" + corpToken

	info = &UserResult{}
	if err = httplib.GetJSON(incompleteURL, info); err != nil {
		return nil, err
	}

	return info, err
}

// 供web端口使用
func GetWebUserInfo(code, provideAccessToken string) (res *ServiceUserInfo, err error) {
	incompleteURL := "https://qyapi.weixin.qq.com/cgi-bin/service/get_login_info?access_token=" + provideAccessToken
	parms := map[string]interface{}{
		"auth_code": code,
	}
	res = &ServiceUserInfo{}
	if err = httplib.PostJSON(incompleteURL, parms, res); err != nil {
		return nil, err
	}
	return
}

// OAuth2 获取用户信息
func GetMobileUserInfo(code string, accessToken string) (*MobileUserInfo, error) {
	userInfo := &MobileUserInfo{}
	url := "https://qyapi.weixin.qq.com/cgi-bin/user/getuserinfo?code=" + code +
		"&access_token=" + accessToken
	session := httplib.NewSession()
	data, err := session.Get(url)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, userInfo)
	if err != nil {
		return &MobileUserInfo{}, err
	}

	return userInfo, nil
}
