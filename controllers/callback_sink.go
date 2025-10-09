package controllers

import (
	"context"
	"fmt"
	"sync"

	"qywx/infrastructures/config"
	"qywx/infrastructures/wxmsg/kefu"
	"qywx/models/kf"
)

// CallbackSink 抽象出客服回调的投递目标，便于在不同服务中替换具体实现。
type CallbackSink interface {
	Deliver(ctx context.Context, event *kefu.KFCallbackMessage) error
}

// CallbackSinkFunc 让普通函数实现 CallbackSink 接口。
type CallbackSinkFunc func(ctx context.Context, event *kefu.KFCallbackMessage) error

// Deliver 调用函数自身满足接口要求。
func (f CallbackSinkFunc) Deliver(ctx context.Context, event *kefu.KFCallbackMessage) error {
	return f(ctx, event)
}

var (
	sinkMu sync.RWMutex
	sink   CallbackSink = CallbackSinkFunc(defaultDeliver)
)

// SetCallbackSink 替换当前回调下游实现。
func SetCallbackSink(s CallbackSink) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	if s == nil {
		sink = CallbackSinkFunc(defaultDeliver)
		return
	}
	sink = s
}

func DeliverCallback(ctx context.Context, event *kefu.KFCallbackMessage) error {
	sinkMu.RLock()
	current := sink
	sinkMu.RUnlock()
	return current.Deliver(ctx, event)
}

func defaultDeliver(ctx context.Context, event *kefu.KFCallbackMessage) error {
	if event == nil {
		return fmt.Errorf("nil callback event")
	}
	cfg := config.GetInstance()
	if cfg.Fetcher.Enabled {
		return produceRawCallbackMessage(event)
	}
	return kf.GetInstance().Receive(event)
}
