package log

import (
	"fmt"
	"os"
	"sync"
	"time"

	"qywx/infrastructures/common"
	"qywx/infrastructures/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type LogLevel int8

const (
	LogLevelNull    LogLevel = LogLevel(zap.FatalLevel)
	LogLevelDebug            = LogLevel(zap.DebugLevel)
	LogLevelInfo             = LogLevel(zap.InfoLevel)
	LogLevelWarning          = LogLevel(zap.WarnLevel)
	LogLevelError            = LogLevel(zap.ErrorLevel)
	LogLevelPanic            = LogLevel(zap.PanicLevel)
	LogLevelFatal            = LogLevel(zap.FatalLevel)
)

// Logger
type Logger struct {
	logger *zap.Logger
	Sugar  *zap.SugaredLogger
}

var (
	instance    *Logger
	once        sync.Once
	logFileName string
	logRootPath string
	serviceName string
	stdoutFile  *os.File

	// 日志轮转控制
	rotateTimer    *time.Timer
	currentLogPath string

	// 默认log级别
	logLevel = LogLevelNull

	// 是否打印调用堆栈
	enableStacktrace = false
)

// path无需以/结尾
func SetLogRootPath(path string) {
	logRootPath = path
}

func InitFileLogConf(fileName string) {
	logFileName = fileName
}

func InitLogFileBySvrName(svrName string) {
	serviceName = svrName
}

// SetStacktrace 是否开启堆栈打印
func SetStacktrace(enable bool) {
	enableStacktrace = enable
}

// InitLogLevel InitLogLevel
func InitLogLevel(l LogLevel) {
	logLevel = l
}

// GetInstance GetInstance
func GetInstance() *Logger {
	once.Do(func() {
		instance = createLogger()
	})
	return instance
}

// CreateLogger CreateLogger
func createLogger() *Logger {
	ret := &Logger{}
	var logConf zap.Config

	// 使用common/env进行环境判断
	cfg := config.GetInstance()
	currentEnv := common.GetCurrentEnvironment()

	if common.ShouldUseStderr() {
		// 开发和容器环境：使用开发配置，输出到stderr
		logConf = zap.NewDevelopmentConfig()
		logConf.OutputPaths = []string{"stderr"}
		logConf.ErrorOutputPaths = []string{"stderr"}
		fmt.Printf("use log DevelopmentConfig for %s environment\n", currentEnv)
	} else if currentEnv == common.EnvProd {
		// 传统生产环境：使用文件输出
		logConf = zap.NewProductionConfig()
		logConf.Encoding = "json"
		fmt.Println("use log ProductionConfig")

		// 生产环境：先调用createLogFile初始化日志文件和轮转机制
		createLogFile()

		// 构建完整的日志文件路径
		logRootDir := cfg.LogConfig.LogRootDir
		if len(logRootPath) > 0 {
			logRootDir = logRootPath
		}

		var logPath string
		fileName := getCurrentLogFileName()

		if fileName != "" {
			if len(serviceName) > 0 {
				logPath = fmt.Sprintf("%s/%s/%s", logRootDir, serviceName, fileName)
			} else {
				logPath = fmt.Sprintf("%s/%s", logRootDir, fileName)
			}
			currentLogPath = logPath
			logConf.OutputPaths = []string{logPath}
			logConf.ErrorOutputPaths = []string{logPath}
		} else {
			// 如果获取不到文件路径，fallback到stderr
			logConf.OutputPaths = []string{"stderr"}
			logConf.ErrorOutputPaths = []string{"stderr"}
			fmt.Println("Production environment fallback to stderr due to file path error")
		}
	} else {
		// 未知环境：默认使用开发配置，输出到stderr
		logConf = zap.NewDevelopmentConfig()
		logConf.OutputPaths = []string{"stderr"}
		logConf.ErrorOutputPaths = []string{"stderr"}
		fmt.Printf("use log DevelopmentConfig for unknown environment: %s\n", currentEnv)
	}

	logConf.DisableStacktrace = !enableStacktrace

	if logLevel == LogLevelNull {
		// 没有被显示指定，从配置文件中加载默认值
		logLevel = LogLevel(cfg.LogConfig.LogLevel)
	}
	logConf.Level = zap.NewAtomicLevelAt(zapcore.Level(logLevel))

	var err error
	ret.logger, err = logConf.Build(zap.AddCallerSkip(1))
	if err != nil {
		fmt.Println("logConf.Build err:", err)
	}
	ret.Sugar = ret.logger.Sugar()
	return ret
}

// Debugf uses fmt.Sprintf to log a templated message.
func Debugf(template string, args ...interface{}) {
	if template == "" {
		GetInstance().Sugar.Warn("Debugf called with empty template, use Debug() instead")
		if len(args) > 0 {
			GetInstance().Sugar.Debug(args...)
		}
		return
	}
	GetInstance().Sugar.Debugf(template, args...)
}

// Infof uses fmt.Sprintf to log a templated message.
func Infof(template string, args ...interface{}) {
	if template == "" {
		GetInstance().Sugar.Warn("Infof called with empty template, use Info() instead")
		if len(args) > 0 {
			GetInstance().Sugar.Info(args...)
		}
		return
	}
	GetInstance().Sugar.Infof(template, args...)
}

// Printf uses fmt.Sprint to construct and log a message.
func Printf(template string, args ...interface{}) {
	if template == "" {
		GetInstance().Sugar.Warn("Printf called with empty template, use Print() instead")
		if len(args) > 0 {
			GetInstance().Sugar.Info(args...)
		}
		return
	}
	GetInstance().Sugar.Infof(template, args...)
}

// Warnf uses fmt.Sprintf to log a templated message.
func Warnf(template string, args ...interface{}) {
	if template == "" {
		GetInstance().Sugar.Warn("Warnf called with empty template, use Warn() instead")
		if len(args) > 0 {
			GetInstance().Sugar.Warn(args...)
		}
		return
	}
	GetInstance().Sugar.Warnf(template, args...)
}

// Errorf uses fmt.Sprintf to log a templated message.
func Errorf(template string, args ...interface{}) {
	if template == "" {
		GetInstance().Sugar.Warn("Errorf called with empty template, use Error() instead")
		if len(args) > 0 {
			GetInstance().Sugar.Error(args...)
		}
		return
	}
	GetInstance().Sugar.Errorf(template, args...)
}

func DPanicf(template string, args ...interface{}) {
	if template == "" {
		GetInstance().Sugar.Warn("DPanicf called with empty template, use DPanic() instead")
		if len(args) > 0 {
			GetInstance().Sugar.DPanic(args...)
		}
		return
	}
	GetInstance().Sugar.DPanicf(template, args...)
}

// Panicf uses fmt.Sprintf to log a templated message, then panics.
func Panicf(template string, args ...interface{}) {
	if template == "" {
		GetInstance().Sugar.Warn("Panicf called with empty template, use Panic() instead")
		if len(args) > 0 {
			GetInstance().Sugar.Panic(args...)
		}
		return
	}
	GetInstance().Sugar.Panicf(template, args...)
}

// Fatalf uses fmt.Sprintf to log a templated message, then calls os.Exit.
func Fatalf(template string, args ...interface{}) {
	if template == "" {
		GetInstance().Sugar.Warn("Fatalf called with empty template, use Fatal() instead")
		if len(args) > 0 {
			GetInstance().Sugar.Fatal(args...)
		}
		return
	}
	GetInstance().Sugar.Fatalf(template, args...)
}
