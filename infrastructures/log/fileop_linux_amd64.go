//go:build linux && amd64 && !noattr
// +build linux,amd64,!noattr

package log

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"qywx/infrastructures/config"
)

var lastLogFileFullPath string
var oldMask int
var currentTimer *time.Timer // 用于管理定时器，避免重复设置

func umask(mask int) int {
	oldMask = syscall.Umask(mask)
	return oldMask
}

func getCurrentLogFileName() string {
	fileName := logFileName
	if len(serviceName) > 0 {
		fileName = fmt.Sprintf("%s-log", serviceName)
	}

	//日志文件以小时分割
	n := time.Now()
	return fmt.Sprintf("%s_%s", fileName, n.Format("2006-01-02_15"))
}

func createLogFile() {
	if len(logFileName) <= 0 && len(serviceName) <= 0 {
		fmt.Println("createPanicFile len(logFileName serviceName) <= 0")
		return
	}

	var fullLogPath string
	logRootDir := config.GetInstance().LogConfig.LogRootDir
	if len(logRootPath) > 0 {
		logRootDir = logRootPath
	}
	if len(serviceName) > 0 {
		logRootPath := fmt.Sprintf("%s/%s/", logRootDir, serviceName)
		if os.MkdirAll(logRootPath, 0777) == nil {
			fullLogPath = fmt.Sprintf("%s%s", logRootPath, getCurrentLogFileName())
		}
	} else {
		if os.MkdirAll(logRootDir, 0777) == nil {
			fullLogPath = fmt.Sprintf("%s/%s", logRootDir, getCurrentLogFileName())
		}
	}

	// 关键优化：检查是否真的需要创建/轮转日志文件
	if fullLogPath == lastLogFileFullPath && stdoutFile != nil {
		// 文件路径相同且已有有效的文件句柄，只需要重新设置定时器
		scheduleNextRotation()
		return
	}
	//
	//这里保证创建出来的文件权限是0666，不会被减去umask的值
	//
	oldMask := umask(0)
	file, err := os.OpenFile(fullLogPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if stdoutFile != nil {
		toCloseFile := stdoutFile
		stdoutFile = nil
		toCloseFileFullPath := lastLogFileFullPath
		time.AfterFunc(2*time.Second, func() {
			if toCloseFile != nil {
				lastLogFileInfo, err := toCloseFile.Stat()
				if err != nil {
					fmt.Println("Stat:", err)
				} else {
					//切换日志文件的时候，如果发现上一个日志是空就删除了
					if lastLogFileInfo.Size() <= 0 {
						os.Remove(toCloseFileFullPath)
					}
				}
				err = toCloseFile.Close()
				if err != nil {
					fmt.Println("CloseFile:", err)
				}
			}
		})
	}
	umask(oldMask)
	stdoutFile = file
	lastLogFileFullPath = fullLogPath
	if err != nil {
		fmt.Println("OpenFile:", err)
		return
	}

	if currentLogPath != "" {
		notifyLoggerRotation(fullLogPath)
	} else {
		// 首次创建：只更新路径，不执行轮转
		currentLogPath = fullLogPath
	}

	// 设置下次轮转定时器
	scheduleNextRotation()
}

// scheduleNextRotation 安全地设置下次日志轮转定时器，避免重复定时器
func scheduleNextRotation() {
	// 取消之前的定时器（如果存在）
	if currentTimer != nil {
		currentTimer.Stop()
	}

	//计算当前距离下个小时还有多少时间，来启动定时器来切换日志
	now := time.Now()
	tHLatter := now.Add(1 * time.Hour)
	tHLatter = time.Date(tHLatter.Year(), tHLatter.Month(), tHLatter.Day(), tHLatter.Hour(), 0, 0, 0, tHLatter.Location())
	remain := time.Until(tHLatter)

	// 设置新的定时器
	currentTimer = time.AfterFunc(remain, createLogFile)
	fmt.Printf("Next log rotation scheduled in %v (at %s)\n", remain, tHLatter.Format("15:04:05"))
}

// notifyLoggerRotation 通知logger使用新的日志文件
func notifyLoggerRotation(newFilePath string) {
	// 如果当前路径已经是新路径，无需轮转
	if currentLogPath == newFilePath {
		return
	}

	// 同步当前logger（确保之前的日志都写入完成）
	if instance != nil && instance.logger != nil {
		instance.logger.Sync()
	}

	// 更新全局路径
	currentLogPath = newFilePath

	// 重新创建logger实例以使用新的日志文件
	cfg := config.GetInstance()
	if cfg.Environment == "prod" {
		// 只有生产环境才需要文件轮转
		// 开发和容器环境使用stderr，不需要轮转
		once = sync.Once{}
		instance = nil
		GetInstance()
		fmt.Printf("Log rotated to: %s\n", newFilePath)
	}
}
