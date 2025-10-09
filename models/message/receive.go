package message

import (
	"errors"

	"qywx/infrastructures/common"
	"qywx/infrastructures/crypter"
	"qywx/infrastructures/tokenstore"
	"qywx/infrastructures/wxmsg/wxsuite"

	"qywx/infrastructures/config"
	"qywx/infrastructures/log"
)

// @Title 验证系统事件接收URL合法性
// @Description 验证系统事件接收URL合法性
// @Param msg_signature string query true "企业微信加密签名"
// @Param timestamp string query true "时间戳"
// @Param nonce string query true "随机数"
// @Param echostr string query true "加密的随机字符串"
func VerifySuiteUrl(rev_signature, timestamp, nonce, encrypt_message string) ([]byte, error) {
	echoData := []byte{}
	mscrypter, err := crypter.NewMessageCrypter(config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey,
		config.GetInstance().SuiteConfig.SuiteId)
	if err != nil {
		return echoData, errors.New("met error during NewMessageCrypter: " + err.Error())
	}

	cal_signature := mscrypter.GetSignature(timestamp, nonce, encrypt_message)
	if cal_signature != rev_signature {
		return echoData, errors.New("message signature not match!")
	}

	echoData, _, err = mscrypter.Decrypt(encrypt_message)
	if err != nil {
		return echoData, errors.New("decrypt data met error: " + err.Error())
	}

	return echoData, nil
}

// @Title  接收Suite消息
// @Description 接收Suite消息
// @Param body []byte query true "来自微信的消息体"
// @Param msg_signature string query true "企业微信加密签名"
// @Param timestamp string query true "时间戳"
// @Param nonce string query true "随机数"
func RecvSuiteMessage(body []byte, signature, timestamp, nonce string) (interface{}, wxsuite.SuiteMsgType, error) {
	var msgType wxsuite.SuiteMsgType
	log.GetInstance().Sugar.Debug("setting.Suite.Id:", config.GetInstance().SuiteConfig.SuiteId)
	suite, err := wxsuite.NewSuite(config.GetInstance().SuiteConfig.SuiteId,
		config.GetInstance().SuiteConfig.Secret,
		config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey)
	if err != nil {
		return nil, msgType, err
	}
	data, msgType, err := suite.Parse(body, signature, timestamp, nonce)
	if err != nil {
		return nil, msgType, err
	}
	return data, msgType, nil
}

// @Title  接收App消息
// @Description 接收App消息
// @Param body []byte query true "来自微信的消息体"
// @Param msg_signature string query true "企业微信加密签名"
// @Param timestamp string query true "时间戳"
// @Param nonce string query true "随机数"
func RecvAppMessage(body []byte, signature, timestamp, nonce string) (interface{}, wxsuite.AppMsgType, error) {
	var msgType wxsuite.AppMsgType

	suite, err := wxsuite.NewSuite(common.CorpId,
		config.GetInstance().SuiteConfig.Secret,
		config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey)
	if err != nil {
		return nil, msgType, err
	}
	data, msgType, err := suite.ParseAppMsg(body, signature, timestamp, nonce)
	if err != nil {
		return nil, msgType, err
	}
	return data, msgType, nil
}

// @Title  获取应用套件的预授权码
// @Description 获取应用套件的预授权码
// @Param suiteAccessToken string query true "套件访问Token"
func GetPreAuthCode() (string, error) {
	// 不缓存在Redis里面，每次直接向企业微信服务器请求
	suite, err := wxsuite.NewSuite(config.GetInstance().SuiteConfig.SuiteId,
		config.GetInstance().SuiteConfig.Secret,
		config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey)
	if err != nil {
		return "", err
	}

	suiteToken, err := tokenstore.Instance().FetchSuiteToken(config.GetInstance().SuiteConfig.SuiteId)
	if err != nil {
		return "", err
	}

	preAuthCode, err := suite.GetPreAuthCode(suiteToken)
	if err != nil {
		return "", err
	}
	return preAuthCode.PreAuthCode, nil
}

func SetSessionInfo(appIds []int, authType int) error {
	suite, err := wxsuite.NewSuite(config.GetInstance().SuiteConfig.SuiteId,
		config.GetInstance().SuiteConfig.Secret,
		config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey)
	if err != nil {
		return err
	}

	suiteToken, err := tokenstore.Instance().FetchSuiteToken(config.GetInstance().SuiteConfig.SuiteId)
	if err != nil {
		return err
	}
	preAuthCode, err := suite.GetPreAuthCode(suiteToken)
	if err != nil {
		return err
	}

	return suite.SetSessionInfo(suiteToken, preAuthCode.PreAuthCode, appIds, authType)
}

func GetPermanentCode(authCode string) (*wxsuite.RecvGetPermanetCode, error) {
	recvBody := &wxsuite.RecvGetPermanetCode{}

	suite, err := wxsuite.NewSuite(config.GetInstance().SuiteConfig.SuiteId,
		config.GetInstance().SuiteConfig.Secret,
		config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey)
	if err != nil {
		return recvBody, err
	}

	token, err := tokenstore.Instance().FetchSuiteToken(config.GetInstance().SuiteConfig.SuiteId)
	if err != nil {
		return recvBody, err
	}
	return suite.GetPermanentCode(token, authCode)
}

func GetAuthInfo(authCorpId, permanentCode string) (*wxsuite.RecvGetAuthInfo, error) {
	recvBody := &wxsuite.RecvGetAuthInfo{}

	suite, err := wxsuite.NewSuite(config.GetInstance().SuiteConfig.SuiteId,
		config.GetInstance().SuiteConfig.Secret,
		config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey)
	if err != nil {
		return recvBody, err
	}

	token, err := tokenstore.Instance().FetchSuiteToken(config.GetInstance().SuiteConfig.SuiteId)
	if err != nil {
		return recvBody, err
	}
	return suite.GetAuthInfo(token, authCorpId, permanentCode)
}

func GetCorpToken(platform common.Platform, authCorpId, permanentCode, corpSecret string) (string, error) {
	return tokenstore.Instance().FetchCorpToken(platform, config.GetInstance().SuiteConfig.SuiteId, authCorpId, permanentCode, corpSecret)
}
