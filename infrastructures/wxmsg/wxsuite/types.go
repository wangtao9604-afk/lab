package wxsuite

type SuiteMsgType int

const (
	_ SuiteMsgType = iota
	SuiteTicket
	CreateAuth
	ChangeAuth
	CancelAuth
	CreateUser
	UpdateUser
	DeleteUser
	CreateParty
	UpdateParty
	DeleteParty
	RegisterComplete
	Login
	GeneralEvent
	TextEvent
	KFMsgOrEvent
	NotSupport
)

type AppMsgType int

const (
	_ AppMsgType = iota
	SubscribeEvent
	UnsubscribeEvent
)

const (
	URL_SUITE_ACCESS_TOKEN   = "https://qyapi.weixin.qq.com/cgi-bin/service/get_suite_token"
	URL_PRE_AUTH_TOKEN       = "https://qyapi.weixin.qq.com/cgi-bin/service/get_pre_auth_code"
	URL_SET_SESSION_INFO     = "https://qyapi.weixin.qq.com/cgi-bin/service/set_session_info"
	URL_GET_PERMANENT_CODE   = "https://qyapi.weixin.qq.com/cgi-bin/service/get_permanent_code"
	URL_GET_AUTH_INFO        = "https://qyapi.weixin.qq.com/cgi-bin/service/get_auth_info"
	URL_GET_CORP_TOKEN       = "https://qyapi.weixin.qq.com/cgi-bin/service/get_corp_token"
	URL_GET_CORP_TOKEN_V2    = "https://qyapi.weixin.qq.com/cgi-bin/gettoken"
	URL_GET_PROVIDER_TOKEN   = "https://qyapi.weixin.qq.com/cgi-bin/service/get_provider_token"
	URL_GET_REGISTER_CODE    = "https://qyapi.weixin.qq.com/cgi-bin/service/get_register_code"
	URL_CONTACT_SYNC_SUCCESS = "https://qyapi.weixin.qq.com/cgi-bin/sync/contact_sync_success"
	URL_GET_ORDER_DETAIL     = "https://qyapi.weixin.qq.com/cgi-bin/service/get_order"
)

type ErrorReply struct {
	Errcode int    `json:"errcode"`
	Errmsg  string `json:"errmsg"`
}

// CDATAText 用于在 xml 解析时避免转义
type CDATAText struct {
	Text string `xml:",innerxml"`
}

// RecvHTTPReqBody 为回调数据
type RecvHTTPReqBody struct {
	ToUserName string
	AgentID    string
	Encrypt    string
}

// RecvSuiteTicket 用于记录应用套件 ticket 的被动响应结果
type RecvSuiteTicket struct {
	SuiteId     string
	InfoType    string
	TimeStamp   int64
	SuiteTicket string
}

type AuthCreated struct {
	SuiteId   string
	AuthCode  string
	InfoType  string
	TimeStamp int64
}

// RecvSuiteAuth 用于记录应用套件授权变更和授权撤销的被动响应结果
type RecvSuiteAuth struct {
	SuiteId    string
	InfoType   string
	TimeStamp  int64
	AuthCorpId string
}

type Item struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ExtAttr struct {
	Attrs []Item `json:"attrs"`
}

type AttrItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Attrs struct {
	Attrs []AttrItem `json:"attrs"`
}

type ContactCreateUser struct {
	SuiteId       string
	AuthCorpId    string
	InfoType      string
	TimeStamp     int64
	ChangeType    string
	UserID        string
	Name          string
	Department    string
	Mobile        string
	Position      string
	Gender        int
	Email         string
	Avatar        string
	EnglishName   string
	IsLeader      int
	Enable        int
	AvatarMediaID string
	Telephone     string
	ExtAttr       Attrs
}

type ContactUpdateUser struct {
	SuiteId       string
	AuthCorpId    string
	InfoType      string
	TimeStamp     int64
	ChangeType    string
	UserID        string
	NewUserID     string
	Name          string
	Department    string
	Mobile        string
	Position      string
	Gender        int
	Email         string
	Avatar        string
	EnglishName   string
	IsLeader      int
	Enable        int
	AvatarMediaID string
	Telephone     string
	ExtAttr       ExtAttr
}

type ContactDeleteUser struct {
	SuiteId    string
	AuthCorpId string
	InfoType   string
	TimeStamp  int64
	ChangeType string
	UserID     string
}

type PartyCreate struct {
	SuiteId    string
	AuthCorpId string
	InfoType   string
	TimeStamp  int64
	ChangeType string
	Id         int
	Name       string
	ParentId   string
	Order      int
}

type PartyUpdate struct {
	SuiteId    string
	AuthCorpId string
	InfoType   string
	TimeStamp  int64
	ChangeType string
	Id         int
	Name       string
	ParentId   string
}

type PartyDelete struct {
	SuiteId    string
	AuthCorpId string
	InfoType   string
	TimeStamp  int64
	ChangeType string
	Id         int
}

type ReqSuiteAccessToken struct {
	SuiteId     string `json:"suite_id"`
	SuiteSecret string `json:"suite_secret"`
	SuiteTicket string `json:"suite_ticket"`
}

type RecvSuiteAccessToken struct {
	SuiteAccessToken string `json:"suite_access_token"`
	ExpiresIn        int    `json:"expires_in"`
}

// 服务商的token
type RecvProviderAccessToken struct {
	ProviderAccessToken string `json:"provider_access_token"`
	ExpiresIn           int64  `json:"expires_in"`
}
type ReqProviderAccessToken struct {
	CorpId string `json:"corpid"`
	Secret string `json:"provider_secret"`
}

type ReqPreAuthCode struct {
	SuiteId string `json:"suite_id"`
}

type RecvPreAuthCode struct {
	ErrorCode   int    `json:"errcode"`
	ErrorMsg    string `json:"errmsg"`
	PreAuthCode string `json:"pre_auth_code"`
	ExpiresIn   int    `json:"expires_in"`
}

type SessionInfo struct {
	AppIds   []int `json:"appid"`
	AuthType int   `json:"auth_type"`
}

type ReqSetSessionInfo struct {
	PreAuthCode string      `json:"pre_auth_code"`
	SessionInfo SessionInfo `json:"session_info"`
}

type ReqGetPermanetCode struct {
	SuiteId  string `json:"suite_id"`
	AuthCode string `json:"auth_code"`
}

type AuthInfo struct {
	Agent []AgentItem `bson:"agent" json:"agent"`
}

type AgentItem struct {
	Agentid       int       `bson:"agentid" json:"agentid"`
	Name          string    `bson:"name" json:"name"`
	RoundLogoURL  string    `bson:"round_logo_url" json:"round_logo_url"`
	SquareLogoURL string    `bson:"square_logo_url" json:"square_logo_url"`
	Appid         int       `bson:"appid" json:"appid"`
	Privilege     Privilege `bson:"privilege" json:"privilege"`
}

type Privilege struct {
	Level      int      `bson:"level" json:"level"`
	AllowParty []int    `bson:"allow_party" json:"allow_party"`
	AllowUser  []string `bson:"allow_user" json:"allow_user"`
	AllowTag   []int    `bson:"allow_tag" json:"allow_tag"`
	ExtraParty []int    `bson:"extra_party" json:"extra_party"`
	ExtraUser  []string `bson:"extra_user" json:"extra_user"`
	ExtraTag   []int    `bson:"extra_tag" json:"extra_tag"`
}
type AuthCorpInfo struct {
	Corpid            string `json:"corpid"`
	CorpName          string `json:"corp_name"`
	CorpType          string `json:"corp_type"`
	CorpSquareLogoURL string `json:"corp_square_logo_url"`
	CorpUserMax       int    `json:"corp_user_max"`
	CorpAgentMax      int    `json:"corp_agent_max"`
	CorpFullName      string `json:"corp_full_name"`
	VerifiedEndTime   int64  `json:"verified_end_time"`
	SubjectType       int    `json:"subject_type"`
	CorpWxqrcode      string `json:"corp_wxqrcode"`
}

type AuthUserInfo struct {
	Email  string `json:"email"`
	Mobile string `json:"mobile"`
	Userid string `json:"userid"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}
type RecvGetPermanetCode struct {
	AccessToken   string       `json:"access_token"`
	ExpiresIn     int64        `json:"expires_in"`
	PermanentCode string       `json:"permanent_code"`
	AuthCorpInfo  AuthCorpInfo `json:"auth_corp_info"`
	AuthInfo      AuthInfo     `json:"auth_info"`
	AuthUserInfo  AuthUserInfo `json:"auth_user_info"`
}

type ReqGetAuthInfo struct {
	SuiteId       string `json:"suite_id"`
	AuthCorpid    string `json:"auth_corpid"`
	PermanentCode string `json:"permanent_code"`
}

type RecvGetAuthInfo struct {
	AuthCorpInfo AuthCorpInfo `json:"auth_corp_info"`
	AuthInfo     AuthInfo     `json:"auth_info"`
}

type ReqGetCorpToken struct {
	SuiteId       string `json:"suite_id"`
	AuthCorpid    string `json:"auth_corpid"`
	PermanentCode string `json:"permanent_code"`
}

type ReqGetRegisterCode struct {
	TemplateId  string `json:"template_id"`
	CorpName    string `json:"corp_name"`
	AdminName   string `json:"admin_name"`
	AdminMobile string `json:"admin_mobile"`
}

type RecvGetRegisterCode struct {
	Errcode      int    `json:"errcode"`
	Errmsg       string `json:"errmsg"`
	RegisterCode string `json:"register_code"`
	ExpiresIn    int    `json:"expires_in"`
}

type RecvRegisterComplete struct {
	ServiceCorpID string `json:"ServiceCorpId"`
	InfoType      string `json:"InfoType"`
	TimeStamp     string `json:"TimeStamp"`
	RegisterCode  string `json:"RegisterCode"`
	AuthCorpID    string `json:"AuthCorpId"`
	ContactSync   struct {
		AccessToken string `json:"AccessToken"`
		ExpiresIn   string `json:"ExpiresIn"`
	} `json:"ContactSync"`
	AuthUserInfo struct {
		Email  string `json:"Email"`
		Mobile string `json:"LoginName"`
		UserID string `json:"UserId"`
	} `json:"AuthUserInfo"`
}

type RecvGetCorpToken struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type EventSubscribe struct {
	ToUserName   string
	FromUserName string
	CreateTime   int64
	MsgType      string
	Event        string
	AgentID      int
}

type TextEventMessage struct {
	ToUserName   string
	FromUserName string
	CreateTime   int64
	MsgType      string
	Content      string
	MsgId        int64
	AgentID      int
}

type GeneralEventMessage struct {
	ToUserName   string
	FromUserName string
	CreateTime   int64
	MsgType      string
	AgentID      int
	Event        string
	EventKey     string
}

type PassiveReplyGeneralWrapper struct {
	Encrypt      string
	MsgSignature string
	TimeStamp    int64
	Nonce        string
}

type PassiveReplyTextMessage struct {
	ToUserName   string
	FromUserName string
	CreateTime   int64
	MsgType      string
	Content      string
}
