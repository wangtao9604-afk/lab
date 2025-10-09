package schedule

import (
	"qywx/infrastructures/config"
	"qywx/models/kf"
)

func appIDForMetrics() string {
	cfg := config.GetInstance().SuiteConfig
	if cfg.KfID != "" {
		return cfg.KfID
	}
	return cfg.SuiteId
}

func reportKefuMessage(msgType string) {
	kf.ReportMessage(appIDForMetrics(), msgType, "kefu")
}
