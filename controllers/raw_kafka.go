package controllers

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	qconfig "qywx/infrastructures/config"
	"qywx/infrastructures/log"
	"qywx/infrastructures/mq/kmq"
	"qywx/infrastructures/wxmsg/kefu"
	"qywx/models/kf"
)

var (
	rawProducerOnce sync.Once
	rawProducer     *kmq.Producer
	rawProducerErr  error
)

func getRawKafkaProducer() (*kmq.Producer, error) {
	rawProducerOnce.Do(func() {
		cfg := qconfig.GetInstance()
		clientID := cfg.Kafka.Producer.Chat.ClientID
		if clientID == "" {
			clientID = "qywx-raw-producer"
		} else if !strings.HasSuffix(clientID, "-raw") {
			clientID = clientID + "-raw"
		}

		rawProducer, rawProducerErr = kmq.NewProducer(cfg.Kafka.Brokers, clientID)
		if rawProducerErr != nil {
			log.GetInstance().Sugar.Errorf("init raw kafka producer failed: %v", rawProducerErr)
		}
	})
	return rawProducer, rawProducerErr
}

func produceRawCallbackMessage(event *kefu.KFCallbackMessage) error {
	producer, err := getRawKafkaProducer()
	if err != nil {
		return err
	}
	if event == nil {
		return fmt.Errorf("nil callback event")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	key := event.OpenKFID
	if key == "" {
		key = event.Token
	}
	if key == "" {
		key = event.Event
	}
	return producer.Produce(kf.RawKafkaTopic, []byte(key), payload, nil)
}
