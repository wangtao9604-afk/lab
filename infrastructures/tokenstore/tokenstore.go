package tokenstore

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"qywx/infrastructures/cache"
	"qywx/infrastructures/common"
	"qywx/infrastructures/config"
	"qywx/infrastructures/httplib"
	"qywx/infrastructures/jssdk"
	"qywx/infrastructures/log"
	"qywx/infrastructures/wxmsg/wxsuite"
)

const (
	retryNum  = 3
	expireErr = "nil returned"
)

type TokenStore struct {
	cache   *cache.Cache
	session common.IHttpClient
}

var (
	instance   *TokenStore
	createLock sync.Mutex
)

func Instance() *TokenStore {
	if instance == nil {
		newInstance()
	}
	return instance
}

func newInstance() {
	createLock.Lock()
	defer createLock.Unlock()
	if instance == nil {
		instance = &TokenStore{
			session: &httplib.Session{},
			cache:   cache.GetInstance(),
		}
	}
}

func makeSuiteAccessTokenRedisKey(suite_id string) string {
	return "qywx:suite_access_token:" + suite_id
}

func makeSuiteTicketRedisKey(suite_id string) string {
	return "qywx:suite_ticket:" + suite_id
}

func makePreAuthCodeRedisKey(suite_id string) string {
	return "qywx:pre_auth_code:" + suite_id
}

func makeCorpTokenRedisKey(corp_id string) string {
	return "qywx:corp:access_token:" + corp_id
}

func makeProviderAccessToken() string {
	return "qywx:provider_access_token:"
}

func makeContactsSyncRedisKey(corp_id string) string {
	return "qywx:sync_contacts:" + corp_id
}

func (t *TokenStore) GetSyncStatus(corp_id string) (string, error) {
	var obj any
	e := t.cache.Fetch(makeContactsSyncRedisKey(corp_id), &obj)
	if e != nil {
		log.GetInstance().Sugar.Debug("get sync status from redis met error:", "err:", e)
		return "", e
	}

	if str, ok := obj.(string); ok {
		log.GetInstance().Sugar.Debug("get sync status from redis result: ", str)
		return str, nil
	} else {
		log.GetInstance().Sugar.Error("cache value is not string type:", "actual_type:", fmt.Sprintf("%T", obj), "value:", obj)
		return "", fmt.Errorf("expected string but got %T: %v", obj, obj)
	}
}

func makeCorpJsTicketRedisKey(corpId string) string {
	return "qywx:corp:corp_js_ticket:" + corpId
}

func (t *TokenStore) SetSyncStatus(corp_id, status string) error {
	return t.cache.Store(makeContactsSyncRedisKey(corp_id), status, 5*time.Minute)
}

func (t *TokenStore) DelSyncStatus(corp_id string) {
	t.cache.Delete(makeContactsSyncRedisKey(corp_id))
}

func (t *TokenStore) FetchPreAuthToken(suiteId string) (string, error) {
	var (
		val string
		err error
	)
	for i := 0; i < retryNum; i++ {
		val, err = t.doFetchPreAuthCode(suiteId)
		if i > 0 {
			log.GetInstance().Sugar.Debug("retry to get pre-auth token, round: ", i)
		}
		if err == nil {
			break
		}
	}
	return val, err
}

func (t *TokenStore) DeletePreAuthToken(suiteId string) error {
	key := makePreAuthCodeRedisKey(suiteId)
	return t.cache.Delete(key)
}

func (t *TokenStore) FetchSuiteToken(suiteId string) (string, error) {
	var (
		val string
		err error
	)
	for i := 0; i < retryNum; i++ {
		val, err = t.doFetchSuiteToken(suiteId)
		if i > 0 {
			log.GetInstance().Sugar.Debug("retry to get suite token, round: ", i)
		}
		if err == nil {
			break
		}
	}
	return val, err
}

func (t *TokenStore) DeleteSuiteToken(suiteId string) error {
	key := makeSuiteAccessTokenRedisKey(suiteId)
	return t.cache.Delete(key)
}

func (t *TokenStore) FetchCorpToken(platform common.Platform, suiteId, corpId, permanentCode, corpSecret string) (string, error) {
	var (
		val string
		err error
	)
	for i := 0; i < retryNum; i++ {
		val, err = t.doFetchCorpToken(platform, suiteId, corpId, permanentCode, corpSecret)
		if i > 0 {
			log.GetInstance().Sugar.Debug("retry to get corp token, round: ", i)
		}
		if err == nil {
			break
		}
	}
	return val, err
}

// 服务商的provider_token
func (t *TokenStore) FetchProviderAccessToken() (string, error) {
	key := makeProviderAccessToken()
	var obj any
	err := t.cache.Fetch(key, &obj)
	if err != nil {
		if errors.Is(err, cache.ErrKeyNotFound) {
			log.GetInstance().Sugar.Debug("begin to get provider access token")
			resultToken, err := wxsuite.GetProviderAccessToken()
			if err != nil {
				log.GetInstance().Sugar.Error("get provider access token met error:", err)
				return "", err
			}
			t.cache.Store(key, resultToken.ProviderAccessToken, time.Second*time.Duration(config.GetInstance().SuiteConfig.TicketTimeOut))
			return resultToken.ProviderAccessToken, nil
		} else {
			log.GetInstance().Sugar.Debug("get provider access token from redis met error:", err)
			return "", err
		}
	}

	if str, ok := obj.(string); ok {
		log.GetInstance().Sugar.Debug("get provider access token from redis result: ", str)
		return str, nil
	} else {
		log.GetInstance().Sugar.Error("cache value is not string type:", "actual_type:", fmt.Sprintf("%T", obj), "value:", obj)
		return "", fmt.Errorf("expected string but got %T: %v", obj, obj)
	}
}

// 获取 js Ticket
func (t *TokenStore) FetchCorpJsTicket(corpId, corpToken string) (string, error) {
	key := makeCorpJsTicketRedisKey(corpId)
	var obj any
	err := t.cache.Fetch(key, &obj)
	if err != nil {
		if errors.Is(err, cache.ErrKeyNotFound) {
			log.GetInstance().Sugar.Debug("begin to get js ticket")
			tjs, err := jssdk.GetJsTicket(corpToken)
			if err != nil {
				log.GetInstance().Sugar.Error("get js ticket met error:", err)
				return "", err
			}
			t.cache.Store(key, tjs.Ticket, time.Second*time.Duration(60*60))
			return tjs.Ticket, nil
		} else {
			log.GetInstance().Sugar.Debug("get js ticket from redis met error:", err)
			return "", err
		}
	}

	if str, ok := obj.(string); ok {
		log.GetInstance().Sugar.Debug("get corp js ticket from redis result: ", str)
		return str, nil
	} else {
		log.GetInstance().Sugar.Error("cache value is not string type:", "actual_type:", fmt.Sprintf("%T", obj), "value:", obj)
		return "", fmt.Errorf("expected string but got %T: %v", obj, obj)
	}
}

func (t *TokenStore) DeleteCorpToken(suiteId string) error {
	key := makeCorpTokenRedisKey(suiteId)
	return t.cache.Delete(key)
}

func (t *TokenStore) Expired(errMsg string) bool {
	return strings.Contains(errMsg, expireErr)
}

func (t *TokenStore) PutSuiteTicketToRedis(suiteId, suiteTicket string, timeout int) error {
	key := makeSuiteTicketRedisKey(suiteId)
	return t.cache.Store(key, suiteTicket, time.Second*time.Duration(timeout))
}

func (t *TokenStore) GetSuiteTicketFromRedis(suiteId string) (string, error) {
	key := makeSuiteTicketRedisKey(suiteId)
	var obj any
	err := t.cache.Fetch(key, &obj)
	if err != nil {
		return "", err
	}

	if str, ok := obj.(string); ok {
		log.GetInstance().Sugar.Debug("get suite ticket from redis result: ", str)
		return str, nil
	} else {
		log.GetInstance().Sugar.Error("cache value is not string type:", "actual_type:", fmt.Sprintf("%T", obj), "value:", obj)
		return "", fmt.Errorf("expected string but got %T: %v", obj, obj)
	}
}

func (t *TokenStore) getSuiteAccessTokenFromRedis(suite_id string) (string, error) {
	var obj any
	err := t.cache.Fetch(makeSuiteAccessTokenRedisKey(suite_id), &obj)
	if err != nil {
		return "", err
	}

	if str, ok := obj.(string); ok {
		log.GetInstance().Sugar.Debug("get suite access token from redis result: ", str)
		return str, nil
	} else {
		log.GetInstance().Sugar.Error("cache value is not string type:", "actual_type:", fmt.Sprintf("%T", obj), "value:", obj)
		return "", fmt.Errorf("expected string but got %T: %v", obj, obj)
	}
}

func (t *TokenStore) getCorpTokenFromRedis(corp_id string) (string, error) {
	var obj any
	err := t.cache.Fetch(makeCorpTokenRedisKey(corp_id), &obj)
	if err != nil {
		return "", err
	}

	if str, ok := obj.(string); ok {
		log.GetInstance().Sugar.Debug("get corp access token from redis result: ", str)
		return str, nil
	} else {
		log.GetInstance().Sugar.Error("cache value is not string type:", "actual_type:", fmt.Sprintf("%T", obj), "value:", obj)
		return "", fmt.Errorf("expected string but got %T: %v", obj, obj)
	}
}

func (t *TokenStore) getPreAuthCodeFromRedis(suite_id string) (string, error) {
	var obj any
	err := t.cache.Fetch(makePreAuthCodeRedisKey(suite_id), &obj)
	if err != nil {
		return "", err
	}

	if str, ok := obj.(string); ok {
		log.GetInstance().Sugar.Debug("get pre auth code from redis result: ", str)
		return str, nil
	} else {
		log.GetInstance().Sugar.Error("cache value is not string type:", "actual_type:", fmt.Sprintf("%T", obj), "value:", obj)
		return "", fmt.Errorf("expected string but got %T: %v", obj, obj)
	}
}

func (t *TokenStore) doFetchSuiteToken(suiteId string) (string, error) {
	// 从redis获取
	suiteAccessToken, err := t.getSuiteAccessTokenFromRedis(suiteId)
	if err == nil {
		return suiteAccessToken, err
	}
	log.GetInstance().Sugar.Error("fetch suite access_token from redis met error: ", err.Error())

	// 从redis获取失败了，用suite_ticket向企业微信获取
	// 获取suite_ticket
	suiteTicket, err := t.GetSuiteTicketFromRedis(suiteId)
	if err == nil {
		// 用suite_ticket获取suite access_token
		return t.refreshToken(makeSuiteAccessTokenRedisKey(suiteId), getSuiteAccessToken, common.PLATFORM_IGNORE, suiteTicket)
	}
	return "", err
}

func (t *TokenStore) doFetchCorpToken(platform common.Platform, suiteId, corpId, permanentCode, corpSecret string) (string, error) {
	// 先从redis获取
	corpAccessToken, err := t.getCorpTokenFromRedis(corpId)
	if err == nil {
		return corpAccessToken, err
	}

	// 未获取到
	log.GetInstance().Sugar.Error("fetch corp access_token from redis met error: ", err)
	if platform != common.PLATFORM_MOBILE {
		suiteAccessToken, err := t.doFetchSuiteToken(suiteId)
		if err == nil {
			return t.refreshToken(makeCorpTokenRedisKey(corpId), getCorpAccessToken, platform, suiteAccessToken, corpId, permanentCode, corpSecret)
		} else {
			log.GetInstance().Sugar.Error("get suiteAccessToken error :", err)
			return "", err
		}
	} else {
		return t.refreshToken(makeCorpTokenRedisKey(corpId), getCorpAccessToken, platform, "", corpId, "", corpSecret)
	}
}

func (t *TokenStore) doFetchPreAuthCode(suiteId string) (string, error) {
	preAuthCode, err := t.getPreAuthCodeFromRedis(suiteId)
	if err == nil {
		return preAuthCode, err
	}
	log.GetInstance().Sugar.Info("fetch pre_auth_code from redis met error: ", err.Error())

	// refresh token
	suiteToken, err := t.doFetchSuiteToken(suiteId)
	if err == nil {
		return t.refreshToken(makePreAuthCodeRedisKey(suiteId), getPreAuthToken, common.PLATFORM_IGNORE, suiteToken)
	}

	return "", err
}

func (t *TokenStore) refreshToken(key string, refresher func(platform common.Platform, params ...string) (string, int, error), platform common.Platform, params ...string) (string, error) {
	createLock.Lock()
	defer createLock.Unlock()
	var obj any
	err := t.cache.Fetch(key, &obj)
	if err != nil {
		val, expire, err := refresher(platform, params...)
		if err != nil {
			return "", err
		}

		// 生产环境是多机集群配置，在存入Redis前，再次用同样的Key获取一次
		// 如果得到了数据，表明另外一台机器上的tokenstore已经写入了新的Token
		// 就没必要再次存入了
		if err := t.cache.Fetch(key, &obj); err == nil {
			if str, ok := obj.(string); ok {
				log.GetInstance().Sugar.Debug("token refreshed, value: ", str)
				return str, nil
			} else {
				log.GetInstance().Sugar.Error("cache value is not string type:", "actual_type:", fmt.Sprintf("%T", obj), "value:", obj)
				return "", fmt.Errorf("expected string but got %T: %v", obj, obj)
			}
		}
		t.cache.Store(key, val, time.Second*time.Duration(expire))
		return val, nil
	}

	if str, ok := obj.(string); ok {
		log.GetInstance().Sugar.Debug("get corp token from redis result: ", str)
		return str, nil
	} else {
		log.GetInstance().Sugar.Error("cache value is not string type:", "actual_type:", fmt.Sprintf("%T", obj), "value:", obj)
		return "", fmt.Errorf("expected string but got %T: %v", obj, obj)
	}
}

func (t *TokenStore) delRedisKey(key string) error {
	return t.cache.Delete(key)
}

func (t *TokenStore) DelCorpTokenFromRedis(corp_id string) error {
	return t.delRedisKey(makeCorpTokenRedisKey(corp_id))
}

func getSuiteAccessToken(platform common.Platform, params ...string) (string, int, error) {
	suite, err := wxsuite.NewSuite(config.GetInstance().SuiteConfig.SuiteId,
		config.GetInstance().SuiteConfig.Secret,
		config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey)
	if err != nil {
		return "", 0, err
	}

	token, err := suite.GetSuiteAccessToken(params[0])
	if err != nil {
		return "", 0, err
	}
	return token.SuiteAccessToken, token.ExpiresIn, nil
}

func getCorpAccessToken(platform common.Platform, params ...string) (string, int, error) {
	if platform != common.PLATFORM_MOBILE && platform != common.PLATFORM_WEB {
		return "", 0, errors.New("invalid platform")
	}

	log.GetInstance().Sugar.Debug("params:", params)
	suite, err := wxsuite.NewSuite(config.GetInstance().SuiteConfig.SuiteId,
		config.GetInstance().SuiteConfig.Secret,
		config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey)
	if err != nil {
		return "", 0, err
	}

	var token *wxsuite.RecvGetCorpToken
	if platform == common.PLATFORM_WEB {
		token, err = suite.GetCorpToken(params[0], params[1], params[2])
	} else {
		token, err = suite.GetCorpTokenV2(params[1], params[3])
	}
	if err != nil {
		return "", 0, err
	}
	return token.AccessToken, token.ExpiresIn, nil
}

func getPreAuthToken(platform common.Platform, params ...string) (string, int, error) {
	suite, err := wxsuite.NewSuite(config.GetInstance().SuiteConfig.SuiteId,
		config.GetInstance().SuiteConfig.Secret,
		config.GetInstance().SuiteConfig.Token,
		config.GetInstance().SuiteConfig.EncodingAESKey)
	if err != nil {
		return "", 0, err
	}
	// token, err := suite.GetCorpToken(suiteToken, corpId, permanentCode)
	token, err := suite.GetPreAuthCode(params[0])
	if err != nil {
		return "", 0, err
	}
	return token.PreAuthCode, token.ExpiresIn, nil
}
