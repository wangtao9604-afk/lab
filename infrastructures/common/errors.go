package common

// 错误状态码
const (
	ReadRequestErr       = 1
	VerifyUrlErr         = 1000
	RecvAppMsgErr        = 1001
	UnSupportAppMsgErr   = 1002
	ConvAppMsgErr        = 1003
	RecvSuiteMsgErr      = 1004
	UnSupportSuiteMsgErr = 1005
	HandleSuiteMsgErr    = 1006
	GetPreAuthCodeErr    = 1007
	SetSessionInfoErr    = 1008
	InputParamErr        = 1009
	GetPermanentCodeErr  = 1010
	GetAuthInfoErr       = 1011
	GetCorpTokenErr      = 1012
	TokenExpiredErr      = 1013
	GetRegisterCodeErr   = 1014
	UnmarshalErr         = 1015
	HadApplied           = 2001 //已经申请过了
	GetCamCardErr        = 3001
	GetMobileUserInfoErr = 4001
	GetWebUserInfoErr    = 4002
)

// error msg
const (
	HadAppliedMsg = "Had Applied"
)
