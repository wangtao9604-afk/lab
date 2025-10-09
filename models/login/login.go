package login

import (
	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	"qywx/infrastructures/oauth2"
	"qywx/infrastructures/tokenstore"
)

func GetUserInfo(code string, platform common.Platform, corpId, permanentCode, corpSecret string) (interface{}, error) {
	token, err := tokenstore.Instance().FetchCorpToken(platform, config.GetInstance().SuiteConfig.SuiteId, corpId, permanentCode, corpSecret)
	if err != nil {
		return nil, err
	}
	var (
		data interface{}
	)
	if platform == common.PLATFORM_MOBILE {
		data, err = oauth2.GetMobileUserInfo(code, token)
	} else if platform == common.PLATFORM_WEB {
		data, err = oauth2.GetWebUserInfo(code, token)
	}

	if err != nil {
		return nil, err
	}

	return data, nil
}
