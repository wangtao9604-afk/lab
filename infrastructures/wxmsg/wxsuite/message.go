package wxsuite

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"

	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	"qywx/infrastructures/crypter"
	"qywx/infrastructures/httplib"
	"qywx/infrastructures/log"
	"qywx/infrastructures/wxmsg/kefu"
)

// Suite 结构体包含了应用套件的相关操作
type Suite struct {
	id             string
	secret         string
	token          string
	encodingAESKey string
	msgCrypter     crypter.MessageCrypter
	client         common.IHttpClient
}

func NewSuite(id, secret, token, encodingAESKey string) (*Suite, error) {
	suite := &Suite{
		id:             id,
		secret:         secret,
		token:          token,
		encodingAESKey: encodingAESKey,
		client:         httplib.NewSession(),
	}
	var err error
	suite.msgCrypter, err = crypter.NewMessageCrypter(token, encodingAESKey, id)
	return suite, err
}

// Parse 方法用于解析应用套件的消息回调
func (s *Suite) Parse(body []byte, signature, timestamp, nonce string) (interface{}, SuiteMsgType, error) {
	var err error
	var msgType SuiteMsgType

	reqBody := &RecvHTTPReqBody{}
	if err = xml.Unmarshal(body, reqBody); err != nil {
		return nil, msgType, err
	}

	if signature != s.msgCrypter.GetSignature(timestamp, nonce, reqBody.Encrypt) {
		return nil, msgType, fmt.Errorf("validate signature error")
	}

	origData, suiteID, err := s.msgCrypter.Decrypt(reqBody.Encrypt)
	if err != nil {
		return nil, msgType, err
	}

	if suiteID != s.id {
		return nil, msgType, fmt.Errorf("the request is from suite[%s], not from suite[%s]", suiteID, s.id)
	}

	probeData := &struct {
		InfoType string
	}{}

	if err = xml.Unmarshal(origData, probeData); err != nil {
		return nil, msgType, err
	}

	infoType := probeData.InfoType
	if len(infoType) == 0 {
		probeData := &struct {
			MsgType string
		}{}
		if err = xml.Unmarshal(origData, probeData); err != nil {
			return nil, msgType, err
		}

		infoType = probeData.MsgType
	}

	var data interface{}
	switch infoType {
	case "register_corp":
		msgType = RegisterComplete
		data = &RecvRegisterComplete{}
	case "suite_ticket":
		msgType = SuiteTicket
		data = &RecvSuiteTicket{}
	case "create_auth":
		msgType = CreateAuth
		data = &AuthCreated{}
	case "change_auth":
		msgType = ChangeAuth
		data = &RecvSuiteAuth{}
	case "cancel_auth":
		msgType = CancelAuth
		data = &RecvSuiteAuth{}
	case "change_contact":
		contactProbe := &struct {
			ChangeType string
		}{}
		if err = xml.Unmarshal(origData, contactProbe); err != nil {
			return nil, msgType, err
		}
		return parseContactEvent(origData, contactProbe.ChangeType)
	case "text":
		msgType = TextEvent
		data = &TextEventMessage{}
	case "event":
		// 需要进一步判断是kf_msg_or_event还是其他event
		msgType = GeneralEvent
		eventTypeProbe := &struct {
			Event string
		}{}
		if err = xml.Unmarshal(origData, eventTypeProbe); err != nil {
			return nil, msgType, err
		}
		if eventTypeProbe.Event == "kf_msg_or_event" {
			msgType = KFMsgOrEvent
			data = &kefu.KFCallbackMessage{}
		} else {
			data = &GeneralEventMessage{}
		}
	default:
		log.GetInstance().Sugar.Debug("msg data: ", string(origData))
		return nil, msgType, fmt.Errorf("unknown message type: %s", probeData.InfoType)
	}

	if err = xml.Unmarshal(origData, data); err != nil {
		return nil, msgType, err
	}

	return data, msgType, nil
}

func (s *Suite) ParseAppMsg(body []byte, signature, timestamp, nonce string) (interface{}, AppMsgType, error) {
	var msgType AppMsgType

	decryptOne, err := s.doDecryptAppMsg(body, signature, timestamp, nonce)
	if err != nil {
		return nil, msgType, fmt.Errorf("first round decrypt met error: %s", err.Error())
	}

	probeData := &struct {
		MsgType string
	}{}

	if err = xml.Unmarshal(decryptOne, probeData); err != nil {
		return nil, msgType, err
	}

	switch probeData.MsgType {
	case "event":
		return parseEventMsg(decryptOne)
	default:
		return nil, msgType, fmt.Errorf("unknown message type: %s", probeData.MsgType)
	}
}

func parseEventMsg(content []byte) (interface{}, AppMsgType, error) {
	var msgType AppMsgType
	var data interface{}
	eventProbe := &struct {
		Event string
	}{}

	if err := xml.Unmarshal(content, eventProbe); err != nil {
		return nil, msgType, err
	}

	switch eventProbe.Event {
	case "subscribe":
		msgType = SubscribeEvent
		data = &EventSubscribe{}
	case "unsubscribe":
		msgType = UnsubscribeEvent
		data = &EventSubscribe{}
	default:
		return nil, msgType, fmt.Errorf("unknown message type: %s", eventProbe.Event)
	}

	if err := xml.Unmarshal(content, data); err != nil {
		return nil, msgType, err
	}

	return data, msgType, nil
}

func (s *Suite) doDecryptAppMsg(encryptBody []byte, signature, timestamp, nonce string) ([]byte, error) {
	var err error
	decryptData := []byte{}
	reqBody := &RecvHTTPReqBody{}

	if err = xml.Unmarshal(encryptBody, reqBody); err != nil {
		return decryptData, err
	}

	if signature != s.msgCrypter.GetSignature(timestamp, nonce, reqBody.Encrypt) {
		return decryptData, fmt.Errorf("validate signature error")
	}

	decryptData, _, err = s.msgCrypter.Decrypt(reqBody.Encrypt)
	if err != nil {
		return decryptData, err
	}

	//if suiteID != s.id {
	//return decryptData, fmt.Errorf("the request is from suite[%s], not from suite[%s]", suiteID, s.id)
	//}
	return decryptData, nil
}

// 得到Suite的AccessToken
func (s *Suite) GetSuiteAccessToken(suiteTicket string) (*RecvSuiteAccessToken, error) {
	recvBody := &RecvSuiteAccessToken{}
	reqBody := &ReqSuiteAccessToken{
		SuiteId:     config.GetInstance().SuiteConfig.SuiteId,
		SuiteSecret: config.GetInstance().SuiteConfig.Secret,
		SuiteTicket: suiteTicket,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return recvBody, err
	}
	recv, err := s.client.Post(URL_SUITE_ACCESS_TOKEN, body, common.JSON)
	if err != nil {
		return recvBody, err
	}
	err = json.Unmarshal(recv, recvBody)
	if err != nil {
		return recvBody, err
	}
	return recvBody, nil
}

func (s *Suite) GetPreAuthCode(suiteAccessToken string) (*RecvPreAuthCode, error) {
	recvBody := &RecvPreAuthCode{}
	reqBody := &ReqPreAuthCode{
		SuiteId: config.GetInstance().SuiteConfig.SuiteId,
	}
	unit := common.ParamUnit{
		Content:       suiteAccessToken,
		EncodeContent: false,
		EncodeTitle:   false,
	}
	params := common.Params{
		"suite_access_token": unit,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return recvBody, err
	}
	url, err := s.client.CombineUrl(URL_PRE_AUTH_TOKEN, params)
	if err != nil {
		return recvBody, err
	}
	fmt.Println("GetPreAuthCode url is: ", url)

	recv, err := s.client.Post(url, body, common.JSON)
	if err != nil {
		return recvBody, err
	}
	err = json.Unmarshal(recv, recvBody)
	return recvBody, err
}

func (s *Suite) SetSessionInfo(suiteAccessToken, preAuthCode string, appIds []int, authType int) error {
	reqBody := &ReqSetSessionInfo{
		PreAuthCode: preAuthCode,
	}
	reqBody.SessionInfo.AppIds = appIds
	reqBody.SessionInfo.AuthType = authType
	recvBody := &ErrorReply{}
	unit := common.ParamUnit{
		Content:       suiteAccessToken,
		EncodeContent: false,
		EncodeTitle:   false,
	}
	params := common.Params{
		"suite_access_token": unit,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	url, err := s.client.CombineUrl(URL_SET_SESSION_INFO, params)
	if err != nil {
		return err
	}
	fmt.Println("SetSessionInfo url is: ", url)

	recv, err := s.client.Post(url, body, common.JSON)
	if err != nil {
		return err
	}
	err = json.Unmarshal(recv, recvBody)
	if err != nil {
		return err
	}
	if recvBody.Errcode > 0 {
		return fmt.Errorf("error code is: %d, error message is: %s", recvBody.Errcode, recvBody.Errmsg)
	}
	return nil
}

func (s *Suite) GetPermanentCode(suiteAccessToken, authCode string) (*RecvGetPermanetCode, error) {
	reqBody := &ReqGetPermanetCode{
		SuiteId:  config.GetInstance().SuiteConfig.SuiteId,
		AuthCode: authCode,
	}
	recvBody := &RecvGetPermanetCode{}
	unit := common.ParamUnit{
		Content:       suiteAccessToken,
		EncodeContent: false,
		EncodeTitle:   false,
	}
	params := common.Params{
		"suite_access_token": unit,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return recvBody, err
	}
	url, err := s.client.CombineUrl(URL_GET_PERMANENT_CODE, params)
	if err != nil {
		return recvBody, err
	}
	fmt.Println("GetPermanentCode url is: ", url)

	recv, err := s.client.Post(url, body, common.JSON)
	fmt.Println("result...............", string(recv))
	if err != nil {
		return recvBody, err
	}
	err = json.Unmarshal(recv, recvBody)
	if err != nil {
		return recvBody, err
	}
	return recvBody, nil
}

func (s *Suite) GetAuthInfo(suiteAccessToken, authCorpId, permanentCode string) (*RecvGetAuthInfo, error) {
	reqBody := &ReqGetAuthInfo{
		SuiteId:       config.GetInstance().SuiteConfig.SuiteId,
		AuthCorpid:    authCorpId,
		PermanentCode: permanentCode,
	}
	recvBody := &RecvGetAuthInfo{}
	unit := common.ParamUnit{
		Content:       suiteAccessToken,
		EncodeContent: false,
		EncodeTitle:   false,
	}
	params := common.Params{
		"suite_access_token": unit,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return recvBody, err
	}
	url, err := s.client.CombineUrl(URL_GET_AUTH_INFO, params)
	if err != nil {
		return recvBody, err
	}
	fmt.Println("GetAuthInfo url is: ", url)

	recv, err := s.client.Post(url, body, common.JSON)
	if err != nil {
		return recvBody, err
	}
	err = json.Unmarshal(recv, recvBody)
	if err != nil {
		return recvBody, err
	}
	return recvBody, nil
}

func (s *Suite) GetCorpToken(suiteAccessToken, authCorpId, permanentCode string) (*RecvGetCorpToken, error) {
	reqBody := &ReqGetCorpToken{
		SuiteId:       config.GetInstance().SuiteConfig.SuiteId,
		AuthCorpid:    authCorpId,
		PermanentCode: permanentCode,
	}
	recvBody := &RecvGetCorpToken{}
	unit := common.ParamUnit{
		Content:       suiteAccessToken,
		EncodeContent: false,
		EncodeTitle:   false,
	}
	params := common.Params{
		"suite_access_token": unit,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return recvBody, err
	}
	url, err := s.client.CombineUrl(URL_GET_CORP_TOKEN, params)
	if err != nil {
		return recvBody, err
	}

	recv, err := s.client.Post(url, body, common.JSON)
	if err != nil {
		return recvBody, err
	}
	err = json.Unmarshal(recv, recvBody)
	if err != nil {
		return recvBody, err
	}
	log.GetInstance().Sugar.Debug("recvBody:", recvBody)
	return recvBody, nil
}

func (s *Suite) GetCorpTokenV2(corpId, corpSecret string) (*RecvGetCorpToken, error) {
	if len(corpId) == 0 {
		return nil, errors.New("corpId is empty")
	}
	if len(corpSecret) == 0 {
		return nil, errors.New("corpSecret is empty")
	}

	params := common.Params{
		"corpid": common.ParamUnit{
			Content:       corpId,
			EncodeContent: false,
			EncodeTitle:   false,
		},
		"corpsecret": common.ParamUnit{
			Content:       corpSecret,
			EncodeContent: false,
			EncodeTitle:   false,
		},
	}
	url, err := s.client.CombineUrl(URL_GET_CORP_TOKEN_V2, params)
	if err != nil {
		return nil, err
	}
	data, err := s.client.Get(url)
	if err != nil {
		return nil, err
	}

	recv := &RecvGetCorpToken{}
	err = json.Unmarshal(data, recv)
	if err != nil {
		return nil, err
	}

	if recv.ErrCode != 0 {
		return nil, errors.New(recv.ErrMsg)
	}

	return recv, nil
}

func parseContactEvent(origData []byte, changeType string) (interface{}, SuiteMsgType, error) {
	var (
		msgType SuiteMsgType
		data    interface{}
	)
	switch changeType {
	case "create_user":
		msgType = CreateUser
		data = &ContactCreateUser{}
	case "update_user":
		msgType = UpdateUser
		data = &ContactUpdateUser{}
	case "delete_user":
		msgType = DeleteUser
		data = &ContactDeleteUser{}
	case "create_party":
		msgType = CreateParty
		data = &PartyCreate{}
	case "update_party":
		msgType = UpdateParty
		data = &PartyUpdate{}
	case "delete_party":
		msgType = DeleteParty
		data = &PartyDelete{}
	default:
		return nil, msgType, fmt.Errorf("unknown contact change type: %s", changeType)
	}

	if err := xml.Unmarshal(origData, data); err != nil {
		return nil, msgType, err
	}

	return data, msgType, nil
}

func GetProviderAccessToken() (*RecvProviderAccessToken, error) {
	recvBody := &RecvProviderAccessToken{}
	reqBody := &ReqProviderAccessToken{
		CorpId: common.CorpId,
		Secret: common.SuiteProviderSecret,
	}
	err := httplib.PostJSON(URL_GET_PROVIDER_TOKEN, reqBody, recvBody)
	if err != nil {
		return nil, err
	}
	return recvBody, err
}

func GetRegiterCode() (*RecvGetRegisterCode, error) {
	reqBody := &ReqGetRegisterCode{
		TemplateId: config.GetInstance().MiscConfig.TemplateId,
	}
	log.GetInstance().Sugar.Info("......", reqBody)
	recvBody := &RecvGetRegisterCode{}
	providerAccesstoken, err := GetProviderAccessToken()
	if err != nil {
		return nil, err
	}
	log.GetInstance().Sugar.Info("access_token:", providerAccesstoken)
	reqUrl := URL_GET_REGISTER_CODE + "?provider_access_token=" + providerAccesstoken.ProviderAccessToken
	log.GetInstance().Sugar.Info("reqUrl:", reqUrl)
	err = httplib.PostJSON(reqUrl, reqBody, recvBody)
	if err != nil {
		return nil, err
	}
	log.GetInstance().Sugar.Debug("recvBody:", recvBody)
	return recvBody, err
}

// 该API用于设置通讯录同步完成，解除通讯录锁定状态，同时使通讯录迁移access_token失效。
func ContactSyncSuccess(access_token string) error {
	reqUrl := URL_CONTACT_SYNC_SUCCESS + "?access_token=" + access_token
	log.GetInstance().Sugar.Info("reqUrl:", reqUrl)
	recv := struct {
		Errcode string `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}{}
	err := httplib.GetJSON(reqUrl, &recv)
	log.GetInstance().Sugar.Debug("recv:", recv)
	return err
}
