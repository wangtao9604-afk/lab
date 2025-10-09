package config

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/BurntSushi/toml"
)

var (
	instance *QywxConfig
	once     sync.Once
)

type log struct {
	LogRootDir       string `toml:"logRootDir"`       // 日志根目录
	LogLevel         int    `toml:"logLevel"`         //= LogLevelDebug //默认log级别
	EnableStacktrace bool   `toml:"enableStacktrace"` //= false //是否打印调用堆栈
}

// redis Redis配置结构体
type redis struct {
	Addr             string   `toml:"addr"`     // redis服务器IP地址+端口号
	User             string   `toml:"user"`     // redis服务器登录用户名
	Password         string   `toml:"password"` // redis服务器登录密码
	DB               int      `toml:"db"`       // 数据库号，默认0
	UseSentinel      bool     `toml:"useSentinel"`
	SentinelAddrs    []string `toml:"sentinelAddrs"`
	MasterName       string   `toml:"masterName"`
	SentinelPassword string   `toml:"sentinelPassword"`

	// 高级配置
	PoolSize     int `toml:"poolSize"`     // 连接池大小，默认10
	MinIdleConns int `toml:"minIdleConns"` // 最小空闲连接数，默认0
	MaxRetries   int `toml:"maxRetries"`   // 最大重试次数，默认3
	DialTimeout  int `toml:"dialTimeout"`  // 连接超时（秒），默认5
	ReadTimeout  int `toml:"readTimeout"`  // 读超时（秒），默认3
	WriteTimeout int `toml:"writeTimeout"` // 写超时（秒），默认3
}

type suiteConfig struct {
	Token          string `toml:"token"`
	EncodingAESKey string `toml:"encodingAESKey"`
	SuiteId        string `toml:"suiteId"`
	AgentId        string `toml:"agentId"`
	Secret         string `toml:"secret"`
	TicketTimeOut  int    `toml:"ticketTimeOut"`
	KfID           string `toml:"kfId"`
}

type miscConfig struct {
	TemplateId      string `toml:"templateId"`
	AuthTokenSecret string `toml:"authTokenSecret"`
}

// KFConfig 客服模块配置
type kfConfig struct {
	BufferSize int `toml:"bufferSize"` // channel缓冲大小，默认1024
	Timeout    int `toml:"timeout"`    // 消息处理超时（秒），默认30
}

// ScheduleConfig 调度器配置
type scheduleConfig struct {
	ProcessorLifetimeMinutes int  `toml:"processorLifetimeMinutes"` // Processor生命周期（分钟），默认30
	CompactHistory           bool `toml:"compactHistory"`           // 是否压缩历史消息，默认false
	MaxHistoryLength         int  `toml:"maxHistoryLength"`         // 压缩时保留的最大消息数，默认20
}

// miniMaxConfig MiniMax智能体配置
type miniMaxConfig struct {
	APIKey      string  `toml:"apiKey"`      // MiniMax API密钥
	GroupID     string  `toml:"groupId"`     // MiniMax Group ID（流式请求可选）
	Model       string  `toml:"model"`       // 模型名称，默认minimax-text-01
	BaseURL     string  `toml:"baseUrl"`     // API基础URL，默认https://api.minimaxi.com
	MaxTokens   int     `toml:"maxTokens"`   // 最大token数，默认256（快速响应优化）
	Temperature float64 `toml:"temperature"` // 温度参数，默认0.1（快速响应优化）
	TopP        float64 `toml:"topP"`        // 采样参数，默认0.95（快速响应优化）
	Stream      bool    `toml:"stream"`      // 是否流式返回，默认false
	Timeout     int     `toml:"timeout"`     // 请求超时（秒），默认600

	// 系统Prompt配置（用于房源助手等场景）
	SystemPrompt       string `toml:"systemPrompt"`       // 系统Prompt内容
	EnableSystemPrompt bool   `toml:"enableSystemPrompt"` // 是否启用系统Prompt，默认false
	SystemRoleName     string `toml:"systemRoleName"`     // 系统角色名称，默认"小胖"
}

// ipangConfig 爱胖售房产查询API配置
type ipangConfig struct {
	AppID     string `toml:"appId"`     // 应用ID
	AppSecret string `toml:"appSecret"` // 应用密钥
	BaseURL   string `toml:"baseUrl"`   // API基础URL，默认https://ai.ipangsell.com
}

// keywordsConfig 关键词提取器配置
type keywordsConfig struct {
	RentalFilterStrategy string `toml:"rentalFilterStrategy"` // 租售过滤策略：strict|ignore|none，默认strict
}

// KafkaProducerConfig 定义 Kafka Producer 的配置项。
type KafkaProducerConfig struct {
	ClientID               string `toml:"clientId"`
	Acks                   string `toml:"acks"`
	EnableIdempotence      *bool  `toml:"enableIdempotence"`
	MessageSendMaxRetries  int    `toml:"messageSendMaxRetries"`
	MessageTimeoutMs       int    `toml:"messageTimeoutMs"`
	LingerMs               int    `toml:"lingerMs"`
	BatchNumMessages       int    `toml:"batchNumMessages"`
	QueueBufferingMaxMs    int    `toml:"queueBufferingMaxMs"`
	QueueBufferingMaxKB    int    `toml:"queueBufferingMaxKbytes"`
	CompressionType        string `toml:"compressionType"`
	SocketKeepaliveEnable  *bool  `toml:"socketKeepaliveEnable"`
	ConnectionsMaxIdleMs   int    `toml:"connectionsMaxIdleMs"`
	StatisticsIntervalMs   int    `toml:"statisticsIntervalMs"`
	GoDeliveryReportFields string `toml:"goDeliveryReportFields"`
	Partitioner            string `toml:"partitioner"`
	MessageMaxBytes        int    `toml:"messageMaxBytes"`
}

type kafkaProducersConfig struct {
	Chat     KafkaProducerConfig `toml:"chat"`
	Recorder KafkaProducerConfig `toml:"recorder"`
}

// KafkaConsumerConfig 定义 Kafka Consumer 的配置项。
type KafkaConsumerConfig struct {
	Debug                       string `toml:"debug"`
	GroupID                     string `toml:"groupId"`
	PartitionAssignmentStrategy string `toml:"partitionAssignmentStrategy"`
	EnableAutoCommit            *bool  `toml:"enableAutoCommit"`
	EnableAutoOffsetStore       *bool  `toml:"enableAutoOffsetStore"`
	AutoOffsetReset             string `toml:"autoOffsetReset"`
	SessionTimeoutMs            int    `toml:"sessionTimeoutMs"`
	HeartbeatIntervalMs         int    `toml:"heartbeatIntervalMs"`
	MaxPollIntervalMs           int    `toml:"maxPollIntervalMs"`
	FetchMinBytes               int    `toml:"fetchMinBytes"`
	FetchWaitMaxMs              int    `toml:"fetchWaitMaxMs"`
	StatisticsIntervalMs        int    `toml:"statisticsIntervalMs"`
	SocketKeepaliveEnable       *bool  `toml:"socketKeepaliveEnable"`
	MaxPartitionFetchBytes      int    `toml:"maxPartitionFetchBytes"`
	FetchMaxBytes               int    `toml:"fetchMaxBytes"`
}

type kafkaConsumersConfig struct {
	Chat     KafkaConsumerConfig `toml:"chat"`
	Recorder KafkaConsumerConfig `toml:"recorder"`
}

// KafkaDLQConfig 定义 Kafka DLQ 的配置项。
type KafkaDLQConfig struct {
	Topic          string `toml:"topic"`
	ClientID       string `toml:"clientId"`
	ClientIDSuffix string `toml:"clientIdSuffix"`
}

// KafkaTopicConfig 定义 Kafka Topic 的配置项。
type KafkaTopicConfig struct {
	Name              string `toml:"name"`
	NumPartitions     int    `toml:"numPartitions"`
	ReplicationFactor int    `toml:"replicationFactor"`
	CleanupPolicy     string `toml:"cleanupPolicy"`
	RetentionMs       int64  `toml:"retentionMs"`
	MaxMessageBytes   int    `toml:"maxMessageBytes"`
}

type kafkaTopicsConfig struct {
	Chat            KafkaTopicConfig `toml:"chat"`
	CallbackInbound KafkaTopicConfig `toml:"callbackInbound"`
	Recorder        KafkaTopicConfig `toml:"recorder"`
}

// kafkaConfig 聚合 Kafka 相关的公共配置。
type kafkaConfig struct {
	Brokers     string               `toml:"brokers"`
	Producer    kafkaProducersConfig `toml:"producer"`
	Consumer    kafkaConsumersConfig `toml:"consumer"`
	ChatDLQ     KafkaDLQConfig       `toml:"chatDLQ"`     // 聊天/客服 DLQ
	RecorderDLQ KafkaDLQConfig       `toml:"recorderDLQ"` // Recorder 微服务 DLQ
	Topics      kafkaTopicsConfig    `toml:"topics"`
}

type serviceEndpointConfig struct {
	HTTPAddr string `toml:"httpAddr"`
}

type servicesConfig struct {
	Producer serviceEndpointConfig `toml:"producer"`
	Consumer serviceEndpointConfig `toml:"consumer"`
	Recorder serviceEndpointConfig `toml:"recorder"`
}

type fetcherRedisConfig struct {
	Addr             string   `toml:"addr"`
	Password         string   `toml:"password"`
	DB               int      `toml:"db"`
	UseSentinel      bool     `toml:"useSentinel"`
	SentinelAddrs    []string `toml:"sentinelAddrs"`
	MasterName       string   `toml:"masterName"`
	SentinelPassword string   `toml:"sentinelPassword"`
}

type fetcherConfig struct {
	Enabled         bool               `toml:"enabled"`
	KeyPrefix       string             `toml:"keyPrefix"`
	LeaseTTLSeconds int                `toml:"leaseTTLSeconds"`
	PollIntervalMs  int                `toml:"pollIntervalMs"`
	KafkaGroupID    string             `toml:"kafkaGroupId"`
	KafkaClientID   string             `toml:"kafkaClientId"`
	LocalShadowPath string             `toml:"localShadowPath"`
	Redis           fetcherRedisConfig `toml:"redis"`
}

type mysqlConfig struct {
	DSN             string `toml:"dsn"`
	MaxOpenConns    int    `toml:"maxOpenConns"`
	MaxIdleConns    int    `toml:"maxIdleConns"`
	ConnMaxIdleTime int    `toml:"connMaxIdleTime"`
	ConnMaxLifetime int    `toml:"connMaxLifetime"`
}

type QywxConfig struct {
	Platform       int              `toml:"platform"`    // 平台类型：1=web, 2=mobile, 3=ignore
	Environment    string           `toml:"environment"` // 环境配置 [dev, prod, container]
	Domain         string           `toml:"domain"`      // 域名
	Stress         bool             `toml:"stress"`      // 压测模式，默认 false
	LogConfig      log              `toml:"log"`
	Redises        map[string]redis `toml:"redises"`
	SuiteConfig    suiteConfig      `toml:"suite"`
	MiscConfig     miscConfig       `toml:"misc"`
	KFConfig       kfConfig         `toml:"kf"`       // 客服配置
	ScheduleConfig scheduleConfig   `toml:"schedule"` // 调度器配置
	MiniMaxConfig  miniMaxConfig    `toml:"minimax"`  // MiniMax配置
	IpangConfig    ipangConfig      `toml:"ipang"`    // Ipang配置
	KeywordsConfig keywordsConfig   `toml:"keywords"` // Keywords配置
	Kafka          kafkaConfig      `toml:"kafka"`    // Kafka配置
	MySQL          mysqlConfig      `toml:"mysql"`    // MySQL配置
	Fetcher        fetcherConfig    `toml:"fetcher"`  // Fetcher配置
	Services       servicesConfig   `toml:"services"`
}

func GetInstance() *QywxConfig {
	once.Do(func() {
		var err error
		instance, err = parseConfig("/etc/qywx/config.toml")
		if err != nil {
			panic(err.Error())
		}
	})
	return instance
}

func parseConfig(path string) (*QywxConfig, error) {
	if len(path) == 0 {
		return nil, errors.New("config file path is null")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		msg := fmt.Sprintf("read config file met error: %s", err.Error())
		return nil, errors.New(msg)
	}

	conf := &QywxConfig{}
	_, err = toml.Decode(string(data), conf)
	if err != nil {
		conf = nil
		return nil, err
	}

	// 设置Platform默认值
	if conf.Platform == 0 {
		conf.Platform = 2 // 默认使用Mobile平台 (PLATFORM_MOBILE)
	}

	// 设置KF配置默认值
	if conf.KFConfig.BufferSize <= 0 {
		conf.KFConfig.BufferSize = 1024
	}
	if conf.KFConfig.Timeout <= 0 {
		conf.KFConfig.Timeout = 30
	}

	// 设置Schedule配置默认值
	if conf.ScheduleConfig.ProcessorLifetimeMinutes <= 0 {
		conf.ScheduleConfig.ProcessorLifetimeMinutes = 30 // 默认30分钟
	}
	// CompactHistory默认为false，无需显式设置
	if conf.ScheduleConfig.MaxHistoryLength <= 0 {
		conf.ScheduleConfig.MaxHistoryLength = 20 // 默认保留20条消息
	}

	// 设置MiniMax配置默认值（快速响应优化版）
	if conf.MiniMaxConfig.Model == "" {
		conf.MiniMaxConfig.Model = "MiniMax-M1" // 使用经过验证的模型
	}
	if conf.MiniMaxConfig.BaseURL == "" {
		conf.MiniMaxConfig.BaseURL = "https://api.minimaxi.com" // 官方API端点
	}
	if conf.MiniMaxConfig.MaxTokens <= 0 {
		conf.MiniMaxConfig.MaxTokens = 256 // 快速响应优化：较小的token数
	}
	if conf.MiniMaxConfig.Temperature <= 0 {
		conf.MiniMaxConfig.Temperature = 0.1 // 快速响应优化：降低随机性
	}
	if conf.MiniMaxConfig.TopP <= 0 {
		conf.MiniMaxConfig.TopP = 0.95 // 快速响应优化：限制候选词
	}
	// Stream默认为false，不需要设置
	if conf.MiniMaxConfig.Timeout <= 0 {
		conf.MiniMaxConfig.Timeout = 600 // 10分钟超时
	}

	// 设置系统Prompt默认值
	// EnableSystemPrompt默认为false，保持向后兼容
	if conf.MiniMaxConfig.SystemRoleName == "" {
		conf.MiniMaxConfig.SystemRoleName = "小胖" // 默认角色名称
	}

	// 设置Ipang配置默认值
	if conf.IpangConfig.BaseURL == "" {
		conf.IpangConfig.BaseURL = "https://ai.ipangsell.com"
	}

	// 设置Keywords配置默认值
	if conf.KeywordsConfig.RentalFilterStrategy == "" {
		conf.KeywordsConfig.RentalFilterStrategy = "strict" // 默认严格过滤模式
	}

	conf.Kafka.setDefaults()
	conf.Fetcher.setDefaults()

	return conf, nil
}

func (k *kafkaConfig) setDefaults() {
	if k == nil {
		return
	}

	if k.Brokers == "" {
		k.Brokers = "127.0.0.1:9092"
	}

	// Set defaults for Chat producer
	setProducerDefaults(&k.Producer.Chat)
	// Set defaults for Recorder producer
	setProducerDefaults(&k.Producer.Recorder)

	// Set defaults for Chat consumer
	setConsumerDefaults(&k.Consumer.Chat)
	// Set defaults for Recorder consumer
	setConsumerDefaults(&k.Consumer.Recorder)

	// DLQ defaults
	if k.ChatDLQ.Topic == "" {
		k.ChatDLQ.Topic = "qywx-kf-ipang-dlq"
	}
	if k.ChatDLQ.ClientIDSuffix == "" {
		k.ChatDLQ.ClientIDSuffix = "-dlq"
	}
	if k.RecorderDLQ.Topic == "" {
		k.RecorderDLQ.Topic = "qywx-recorder-dead-letter"
	}
	if k.RecorderDLQ.ClientIDSuffix == "" {
		k.RecorderDLQ.ClientIDSuffix = "-dlq"
	}

	if k.Topics.CallbackInbound.Name == "" {
		k.Topics.CallbackInbound.Name = "wx_raw_event"
	}
}

func setProducerDefaults(p *KafkaProducerConfig) {
	if p.Acks == "" {
		p.Acks = "all"
	}
	if p.EnableIdempotence == nil {
		p.EnableIdempotence = boolPtr(true)
	}
	if p.MessageSendMaxRetries <= 0 {
		p.MessageSendMaxRetries = 10
	}
	if p.MessageTimeoutMs <= 0 {
		p.MessageTimeoutMs = 30000
	}
	if p.LingerMs <= 0 {
		p.LingerMs = 10
	}
	if p.BatchNumMessages <= 0 {
		p.BatchNumMessages = 1000
	}
	if p.QueueBufferingMaxMs <= 0 {
		p.QueueBufferingMaxMs = 50
	}
	if p.QueueBufferingMaxKB <= 0 {
		p.QueueBufferingMaxKB = 102400
	}
	if p.CompressionType == "" {
		p.CompressionType = "lz4"
	}
	if p.SocketKeepaliveEnable == nil {
		p.SocketKeepaliveEnable = boolPtr(true)
	}
	if p.ConnectionsMaxIdleMs <= 0 {
		p.ConnectionsMaxIdleMs = 300000
	}
	if p.StatisticsIntervalMs <= 0 {
		p.StatisticsIntervalMs = 60000
	}
	if p.GoDeliveryReportFields == "" {
		p.GoDeliveryReportFields = "key,value,headers"
	}
	if p.Partitioner == "" {
		p.Partitioner = "murmur2_random"
	}
}

func setConsumerDefaults(c *KafkaConsumerConfig) {
	if c.PartitionAssignmentStrategy == "" {
		c.PartitionAssignmentStrategy = "cooperative-sticky"
	}
	if c.EnableAutoCommit == nil {
		c.EnableAutoCommit = boolPtr(false)
	}
	if c.EnableAutoOffsetStore == nil {
		c.EnableAutoOffsetStore = boolPtr(false)
	}
	if c.AutoOffsetReset == "" {
		c.AutoOffsetReset = "earliest"
	}
	if c.SessionTimeoutMs <= 0 {
		c.SessionTimeoutMs = 30000
	}
	if c.HeartbeatIntervalMs <= 0 {
		c.HeartbeatIntervalMs = 3000
	}
	if c.MaxPollIntervalMs <= 0 {
		c.MaxPollIntervalMs = 300000
	}
	if c.FetchMinBytes <= 0 {
		c.FetchMinBytes = 1024
	}
	if c.FetchWaitMaxMs <= 0 {
		c.FetchWaitMaxMs = 500
	}
	if c.StatisticsIntervalMs <= 0 {
		c.StatisticsIntervalMs = 60000
	}
	if c.SocketKeepaliveEnable == nil {
		c.SocketKeepaliveEnable = boolPtr(true)
	}
}

func (f *fetcherConfig) setDefaults() {
	if f == nil {
		return
	}

	if f.KeyPrefix == "" {
		f.KeyPrefix = "kf"
	}
	if f.LeaseTTLSeconds <= 0 {
		f.LeaseTTLSeconds = 15
	}
	if f.PollIntervalMs <= 0 {
		f.PollIntervalMs = 1000
	}
	if f.KafkaGroupID == "" {
		f.KafkaGroupID = "kf-fetcher-group"
	}
	if f.KafkaClientID == "" {
		f.KafkaClientID = "kf-fetcher-client"
	}
	if f.Redis.DB < 0 {
		f.Redis.DB = 0
	}
}

func boolPtr(v bool) *bool {
	return &v
}
