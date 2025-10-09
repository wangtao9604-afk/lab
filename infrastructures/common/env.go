package common

import (
	"fmt"
	"os"

	"qywx/infrastructures/config"
)

// Environment 运行环境类型
type Environment string

const (
	EnvDev       Environment = "dev"       // 开发环境
	EnvProd      Environment = "prod"      // 生产环境
	EnvContainer Environment = "container" // 容器环境
)

// validEnvironments 合法的环境值列表
var validEnvironments = map[string]bool{
	"dev":       true,
	"prod":      true,
	"container": true,
}

// GetCurrentEnvironment 从配置文件读取当前运行环境，带验证和fallback逻辑
func GetCurrentEnvironment() Environment {
	configEnv := config.GetInstance().Environment

	// 验证配置值是否合法
	if configEnv == "" || !validEnvironments[configEnv] {
		// 记录错误日志
		if configEnv == "" {
			fmt.Fprintf(os.Stderr, "[ENV_ERROR] 配置文件中environment字段为空，将使用fallback逻辑推导环境\n")
		} else {
			fmt.Fprintf(os.Stderr, "[ENV_ERROR] 配置文件中environment值'%s'非法，合法值为: dev, prod, container。将使用fallback逻辑推导环境\n", configEnv)
		}

		// fallback逻辑：推导合理值
		return deriveEnvironmentFromSystem()
	}

	return Environment(configEnv)
}

// deriveEnvironmentFromSystem 通过系统环境推导合理的环境值
func deriveEnvironmentFromSystem() Environment {
	// 1. 检测容器环境
	if IsRunningInContainer() {
		fmt.Fprintf(os.Stderr, "[ENV_FALLBACK] 检测到容器环境，推导为: container\n")
		return EnvContainer
	}

	// 2. 检测GIN_MODE
	if os.Getenv("GIN_MODE") == "release" {
		fmt.Fprintf(os.Stderr, "[ENV_FALLBACK] 检测到GIN_MODE=release，推导为: prod\n")
		return EnvProd
	}

	// 3. 默认开发环境
	fmt.Fprintf(os.Stderr, "[ENV_FALLBACK] 无法确定环境，默认为: dev\n")
	return EnvDev
}

// ShouldUseStderr 判断是否应该使用stderr输出（dev和container环境）
func ShouldUseStderr() bool {
	env := GetCurrentEnvironment()
	return env == EnvDev || env == EnvContainer
}

// IsRunningInContainer 检测是否在容器环境中运行
func IsRunningInContainer() bool {
	// 检测常见的容器环境变量
	containerIndicators := []string{
		"KUBERNETES_SERVICE_HOST", // Kubernetes环境
		"DOCKER_CONTAINER",        // Docker环境
		"container",               // 通用容器环境变量
	}

	for _, indicator := range containerIndicators {
		if os.Getenv(indicator) != "" {
			return true
		}
	}

	// 检测Docker容器的cgroup特征
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	return false
}
