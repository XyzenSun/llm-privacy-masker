# LLM Privacy Masker

一个用于 LLM API 请求的隐私脱敏Go库，通过可信 LLM 识别并将敏感信息替换为语义变量发送给上游LLM后，再将响应中的语义化变量替换为原始值，保护用户隐私数据的同时，不影响用户看到的消息结果。

## 功能特性

- 自动识别敏感信息（密码、IP地址、手机号、邮箱等）
- 语义化变量替换脱敏
- 支持多轮对话的会话状态保持（可只将每轮的新对话交给TrustedLLM处理，加快处理速度，减少Token消耗）
- 支持 OpenAI、Anthropic、Gemini 协议 （使用时无需指定，自动识别）
- 支持 Memory 和 Redis 两种存储方式
- 零信任，敏感数据不会泄露到不可信 LLM，通过可信LLM对数据中的隐私信息做语义化识别替换

## 安装

```bash
go get github.com/xyzensun/llm-privacy-masker
```

## 快速开始

### 方式一：使用配置函数

```go
package main

import (
    "bytes"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/xyzensun/llm-privacy-masker"
)

func main() {
    // 创建 Masker 实例
    m, err := masker.NewMaskerdWithConfig(
        120*time.Second,        // timeout - 请求超时时间
        "memory",               // sessionStoreType - 存储类型 ("memory" 或 "redis")
        "",                     // redisConnectionURL - Redis 连接地址（使用 memory 时可为空）
        0,                      // sessionTTL - Session 过期时间，0 表示不过期
        "http://127.0.0.1:8888/v1",  // trustedLLMBaseURL - 可信 LLM API 地址，只支持openai格式
        "sk-your-api-key",      // trustedLLMAPIKey - 可信 LLM API Key
        "gpt-4o",               // trustedLLMModelName - 可信 LLM 模型名称
        "",                     // trustedLLMSystemPrompt - 系统提示词（可选）
        0.7,                    // trustedLLMTemperature - 温度参数
    )
    if err != nil {
        panic(err)
    }

    // 构造请求
    body := `{
        "model": "gpt-4o",
        "messages": [
            {"role": "user", "content": "我的密码是 abc123，帮我记住它"}
        ]
    }`

    req, err := http.NewRequest(
        "POST",
        "https://api.openai.com/v1/chat/completions",
        bytes.NewReader([]byte(body)),
    )
    if err != nil {
        panic(err)
    }
    req.Header.Set("Authorization", "Bearer sk-your-upstream-key")
    req.Header.Set("Content-Type", "application/json")

    // 处理请求（不带 sessionID）
    resp, err := m.Process(req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    respBody, _ := io.ReadAll(resp.Body)
    fmt.Println("Response:", string(respBody))
}
```

### 方式二：使用 Builder 模式

```go
m, err := masker.New().
    WithTimeout(120 * time.Second).
    WithSessionStoreType("redis").
    WithRedisConnectionURL("redis://localhost:6379").
    WithSessionTTL(30 * time.Minute).
    WithTrustedLLMBaseURL("http://127.0.0.1:8888/v1").
    WithTrustedLLMAPIKey("sk-your-api-key").
    WithTrustedLLMModelName("gpt-4o").
    WithTrustedLLMTemperature(0.7).
    Build()
if err != nil {
    panic(err)
}
```

## 多轮对话

对于需要保持上下文的多轮对话场景，传入 `sessionID` 参数：

```go
sessionID := "user-session-123"

// 第一轮对话
resp1, err := m.Process(req1, sessionID)

// 第二轮对话 - 会复用第一轮的敏感信息映射
resp2, err := m.Process(req2, sessionID)

// 第三轮对话 - 继续复用已有的映射
resp3, err := m.Process(req3, sessionID)
```

`sessionID` 是可选参数，不传则每次请求独立处理：

```go
// 单次请求，不保持会话状态
resp, err := m.Process(req)
```

## 配置参数说明

| 参数 | 类型 | 说明 |
|------|------|------|
| `timeout` | `time.Duration` | 请求超时时间，默认 120s |
| `sessionStoreType` | `string` | 存储类型：`"memory"` 或 `"redis"` |
| `redisConnectionURL` | `string` | Redis 连接地址，如 `redis://localhost:6379` |
| `sessionTTL` | `time.Duration` | Session 过期时间，0 表示不过期 |
| `trustedLLMBaseURL` | `string` | 可信 LLM API 地址 |
| `trustedLLMAPIKey` | `string` | 可信 LLM API Key |
| `trustedLLMModelName` | `string` | 可信 LLM 模型名称 |
| `trustedLLMSystemPrompt` | `string` | 自定义系统提示词（可选） |
| `trustedLLMTemperature` | `float64` | 温度参数，默认 0.7 |

## 支持的协议

| 协议 | API 路径特征 |
|------|-------------|
| OpenAI | `/v1/chat/completions` |
| Anthropic | `/v1/messages` |
| Gemini | `/v1beta/models/` 或 `/models/` |

## 存储类型

### Memory 存储

适用于单机部署、测试环境：

```go
m, err := masker.NewMaskerdWithConfig(
    120*time.Second,
    "memory",
    "",
    0,
    // ... 其他参数
)
```

### Redis 存储

适用于分布式部署、生产环境：

```go
m, err := masker.NewMaskerdWithConfig(
    120*time.Second,
    "redis",
    "redis://localhost:6379",
    30*time.Minute,  // Session 30分钟后过期
    // ... 其他参数
)
```

## 工作原理

1. **请求阶段**：解析请求内容，调用可信 LLM 进行 NER 检测，识别敏感信息
2. **脱敏阶段**：将敏感信息替换为语义化占位符（如 `PASSWORD_1`、`IP_1`）
3. **转发阶段**：将脱敏后的请求转发到目标 LLM API
4. **反脱敏阶段**：将响应中的占位符还原为原始值

## API 签名

```go
func (m *Masker) Process(req *http.Request, sessionID ...string) (*http.Response, error)
```

- `req`: 标准 `http.Request`，包含目标 LLM 的请求信息
- `sessionID`: 可选参数，用于多轮对话的会话状态保持
- 返回: 标准 `http.Response`，调用方需要 `defer resp.Body.Close()`

## License

MIT