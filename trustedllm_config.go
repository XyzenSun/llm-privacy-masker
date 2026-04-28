package masker

import (
	"fmt"
	"time"
)

// TrustedLLMConfig 可信 LLM 的配置结构体。
type TrustedLLMConfig struct {
	// ClientConfig 可信 LLM 客户端配置
	ClientConfig TrustedLLMClientConfig

	// Timeout 请求可信 LLM 的超时时间
	Timeout time.Duration

	// SessionStoreType 会话存储类型："memory" 或 "redis"
	SessionStoreType string

	// RedisConnectionURL Redis 连接地址（当 SessionStoreType="redis" 时必须）
	RedisConnectionURL string

	// SessionTTL session 映射在 Redis 中的过期时间，0 表示不过期
	SessionTTL time.Duration
}

// TrustedLLMClientConfig 可信 LLM 客户端配置结构体（OpenAI Completion 格式）。
type TrustedLLMClientConfig struct {
	// BaseURL API 基础地址
	BaseURL string

	// APIKey API 密钥（本地 LLM 可为空）
	APIKey string

	// ModelName 模型名称
	ModelName string

	// SystemPrompt 系统提示词（可选，用于 NER 检测，如不提供则使用默认值）
	SystemPrompt string

	// Temperature 可信 LLM 的温度参数
	Temperature float64
}

// DefaultTrustedLLMConfig 返回默认的可信 LLM 配置。
func DefaultTrustedLLMConfig() *TrustedLLMConfig {
	return &TrustedLLMConfig{
		ClientConfig: TrustedLLMClientConfig{
			Temperature: 0.7,
		},
		Timeout:          120 * time.Second,
		SessionStoreType: "memory",
		SessionTTL:       0,
	}
}

// ValidateTrustedLLMConfig 校验可信 LLM 配置的有效性。
func ValidateTrustedLLMConfig(config *TrustedLLMConfig) error {
	if config == nil {
		return fmt.Errorf("可信 LLM 配置为空")
	}

	if config.Timeout <= 0 {
		return fmt.Errorf("超时时间必须大于零")
	}

	switch config.SessionStoreType {
	case "memory", "":
	case "redis":
		if config.RedisConnectionURL == "" {
			return fmt.Errorf("使用 Redis 存储时必须提供 Redis 连接地址")
		}
	default:
		return fmt.Errorf("不支持的会话存储类型: %s", config.SessionStoreType)
	}

	if config.ClientConfig.BaseURL == "" {
		return fmt.Errorf("可信 LLM 的 API 地址不能为空")
	}

	if config.ClientConfig.ModelName == "" {
		return fmt.Errorf("可信 LLM 的模型名称不能为空")
	}

	return nil
}
