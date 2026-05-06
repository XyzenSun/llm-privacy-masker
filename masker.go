package masker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xyzensun/llm-privacy-masker/protocol"
	"github.com/xyzensun/llm-privacy-masker/store"
)

const defaultRequestTTL = 5 * time.Minute

// Masker 编排脱敏、转发与反脱敏流程。
type Masker struct {
	trustedLLMConfig TrustedLLMConfig
	store            store.Store
	trustedLLMClient *TrustedLLMClient
	httpClient       *http.Client
	protocolHandlers map[protocol.ProtocolType]protocol.Protocol
}

// Builder 创建 Masker Builder 实例，用于通过链式调用配置并构建 Masker。
func Builder() *Masker {
	masker := &Masker{trustedLLMConfig: *DefaultTrustedLLMConfig()}
	masker.protocolHandlers = map[protocol.ProtocolType]protocol.Protocol{
		protocol.ProtocolTypeOpenAI:    protocol.NewOpenAI(),
		protocol.ProtocolTypeAnthropic: protocol.NewAnthropic(),
		protocol.ProtocolTypeGemini:    protocol.NewGemini(),
	}
	return masker
}

// New 创建并初始化 Masker 实例。
func New(timeout time.Duration, sessionStoreType string, redisConnectionURL string, sessionTTL time.Duration, trustedLLMBaseURL string, trustedLLMAPIKey string, trustedLLMModelName string, trustedLLMSystemPrompt string, trustedLLMTemperature float64) (*Masker, error) {
	if sessionStoreType == "" {
		sessionStoreType = "memory"
	}

	masker := Builder().
		WithTimeout(timeout).
		WithRedisConnectionURL(redisConnectionURL).
		WithSessionStoreType(sessionStoreType).
		WithSessionTTL(sessionTTL).
		WithTrustedLLMBaseURL(trustedLLMBaseURL).
		WithTrustedLLMAPIKey(trustedLLMAPIKey).
		WithTrustedLLMModelName(trustedLLMModelName).
		WithTrustedLLMSystemPrompt(trustedLLMSystemPrompt).
		WithTrustedLLMTemperature(trustedLLMTemperature)

	return masker.Build()
}

// WithTimeout 设置请求超时时间。
func (g *Masker) WithTimeout(timeout time.Duration) *Masker {
	g.trustedLLMConfig.Timeout = timeout
	return g
}

// WithSessionStoreType 设置会话存储类型。
func (g *Masker) WithSessionStoreType(storeType string) *Masker {
	g.trustedLLMConfig.SessionStoreType = storeType
	return g
}

// WithRedisConnectionURL 设置 Redis 连接地址。
func (g *Masker) WithRedisConnectionURL(redisURL string) *Masker {
	g.trustedLLMConfig.RedisConnectionURL = redisURL
	return g
}

// WithSessionTTL 设置 session 映射在 Redis 中的过期时间，0 表示不过期。
func (g *Masker) WithSessionTTL(ttl time.Duration) *Masker {
	g.trustedLLMConfig.SessionTTL = ttl
	return g
}

// WithTrustedLLMBaseURL 设置可信 LLM 的基础地址。
func (g *Masker) WithTrustedLLMBaseURL(baseURL string) *Masker {
	g.trustedLLMConfig.ClientConfig.BaseURL = baseURL
	return g
}

// WithTrustedLLMAPIKey 设置可信 LLM 的 API Key。
func (g *Masker) WithTrustedLLMAPIKey(apiKey string) *Masker {
	g.trustedLLMConfig.ClientConfig.APIKey = apiKey
	return g
}

// WithTrustedLLMModelName 设置可信 LLM 的模型名称。
func (g *Masker) WithTrustedLLMModelName(modelName string) *Masker {
	g.trustedLLMConfig.ClientConfig.ModelName = modelName
	return g
}

// WithTrustedLLMSystemPrompt 设置可信 LLM 的系统提示词。
func (g *Masker) WithTrustedLLMSystemPrompt(systemPrompt string) *Masker {
	g.trustedLLMConfig.ClientConfig.SystemPrompt = systemPrompt
	return g
}

// WithTrustedLLMTemperature 设置可信 LLM 的温度参数。
func (g *Masker) WithTrustedLLMTemperature(temperature float64) *Masker {
	g.trustedLLMConfig.ClientConfig.Temperature = temperature
	return g
}

// Build 构建并校验 Masker 实例，完成依赖初始化。
func (g *Masker) Build() (*Masker, error) {
	if err := ValidateTrustedLLMConfig(&g.trustedLLMConfig); err != nil {
		return nil, err
	}

	switch g.trustedLLMConfig.SessionStoreType {
	case "redis":
		redisStore, err := store.NewRedisStore(g.trustedLLMConfig.RedisConnectionURL, g.trustedLLMConfig.SessionTTL, defaultRequestTTL)
		if err != nil {
			return nil, fmt.Errorf("创建 Redis 存储失败: %w", err)
		}
		g.store = redisStore
	default:
		g.store = store.NewMemoryStore()
	}

	g.trustedLLMClient = NewTrustedLLMClient(g.trustedLLMConfig.ClientConfig)
	g.httpClient = &http.Client{Timeout: g.trustedLLMConfig.Timeout}

	return g, nil
}

// Process 处理一次完整的请求-响应脱敏流程。
// sessionID 为可选参数，用于多轮对话场景的会话状态保持。
func (g *Masker) Process(req *http.Request, sessionID ...string) (*http.Response, error) {
	if g == nil {
		return nil, fmt.Errorf("Masker 实例为空")
	}

	if req == nil {
		return nil, fmt.Errorf("HTTP 请求为空")
	}

	// 读取并恢复请求体（Body 只能读取一次）
	requestBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("读取请求体失败: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(requestBody))

	// 提取可选的 sessionID
	var sid string
	if len(sessionID) > 0 {
		sid = sessionID[0]
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), g.trustedLLMConfig.Timeout)
	defer cancelFunc()

	targetURL := req.URL.String()
	requestProtocolType, err := protocol.JudgeProtocol(targetURL, requestBody)
	if err != nil {
		return nil, fmt.Errorf("判断请求协议失败: %w", err)
	}

	requestProtocol, ok := g.protocolHandlers[requestProtocolType]
	if !ok {
		return nil, fmt.Errorf("不支持的协议类型: %s", requestProtocolType)
	}

	// 将请求体中的用户prompt与LLM的回复进行语义化变量替换脱敏 并维护双射哈希表
	maskedRequestBody, _, placeholderToOriginal, requestID, err := g.processRequest(ctx, requestProtocol, requestBody, sid)
	if err != nil {
		return nil, fmt.Errorf("处理请求失败: %w", err)
	}

	// 使用传入请求的 URL 和请求体，并强制改写为非流式请求。
	targetURL, maskedRequestBody, err = requestProtocol.ForceNonStream(targetURL, maskedRequestBody)
	if err != nil {
		return nil, fmt.Errorf("强制关闭流式传输失败: %w", err)
	}

	upstreamRequest, err := g.buildUpstreamRequest(req.Method, targetURL, req.Header, maskedRequestBody)
	if err != nil {
		return nil, fmt.Errorf("构建上游请求失败: %w", err)
	}

	upstreamResponse, err := g.httpClient.Do(upstreamRequest.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("执行上游请求失败: %w", err)
	}
	defer upstreamResponse.Body.Close()

	upstreamResponseBody, err := io.ReadAll(upstreamResponse.Body)
	if err != nil {
		return nil, fmt.Errorf("读取上游响应失败: %w", err)
	}

	// 上游非正常响应，将原始响应返回给调用者，不再进行语义化变量替换为原始值
	if upstreamResponse.StatusCode < 200 || upstreamResponse.StatusCode >= 300 {
		// 清理请求级映射，避免残留脏数据
		if requestID != "" {
			if deleteErr := g.store.DeleteRequestMappings(requestID); deleteErr != nil {
				// 清理失败不影响错误响应的返回，仅记录错误
				return &http.Response{
					StatusCode:    upstreamResponse.StatusCode,
					Header:        upstreamResponse.Header,
					Body:          io.NopCloser(bytes.NewReader(upstreamResponseBody)),
				}, fmt.Errorf("删除请求映射失败: %w", deleteErr)
			}
		}
		return &http.Response{
			StatusCode:    upstreamResponse.StatusCode,
			Header:        upstreamResponse.Header,
			Body:          io.NopCloser(bytes.NewReader(upstreamResponseBody)),
		}, nil
	}

	responseBody, err := requestProtocol.RewriteResponse(upstreamResponseBody, placeholderToOriginal)
	if err != nil {
		return nil, fmt.Errorf("处理响应失败: %w", err)
	}

	if requestID != "" {
		if err := g.store.DeleteRequestMappings(requestID); err != nil {
			return nil, fmt.Errorf("删除请求映射失败: %w", err)
		}
	}

	return &http.Response{
		StatusCode: upstreamResponse.StatusCode,
		Header:     upstreamResponse.Header,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}, nil
}

// processRequest 处理请求阶段的脱敏逻辑。
func (g *Masker) processRequest(ctx context.Context, requestProtocol protocol.Protocol, requestBody []byte, sessionID string) ([]byte, map[string]string, map[string]string, string, error) {
	originalToPlaceholder := make(map[string]string)
	placeholderToOriginal := make(map[string]string)
	requestID := ""
	cacheHit := false

	if sessionID != "" {
		// 从存储中加载已有的会话映射
		loadedOriginalToPlaceholder, loadedPlaceholderToOriginal, err := g.store.LoadSessionMappings(sessionID)
		if err != nil {
			return nil, nil, nil, "", fmt.Errorf("加载会话映射失败: %w", err)
		}
		originalToPlaceholder = loadedOriginalToPlaceholder
		placeholderToOriginal = loadedPlaceholderToOriginal
		cacheHit = len(originalToPlaceholder) > 0 && len(placeholderToOriginal) > 0
	} else {
		requestID = generateRequestID()
	}

	if sessionID != "" && cacheHit {
		//注释调试注释
		//fmt.Printf("processRequest sessionID=%q cacheHit=%t mode=incremental o2p=%d p2o=%d\n", sessionID, cacheHit, len(originalToPlaceholder), len(placeholderToOriginal))
		latestUserText, hasUserText, err := requestProtocol.LatestUserText(requestBody)
		if err != nil {
			return nil, nil, nil, "", err
		}
		if hasUserText && latestUserText != "" {
			preMaskedText := ApplyOriginalToPlaceholder(latestUserText, originalToPlaceholder)
			detectedMappings, detectionError := g.trustedLLMClient.DetectMappings(ctx, originalToPlaceholder, preMaskedText)
			if detectionError != nil {
				return nil, nil, nil, "", fmt.Errorf("可信 LLM 检测失败: %w", detectionError)
			}

			if len(detectedMappings.Entries) > 0 {
				mergeError := MergeMappings(originalToPlaceholder, placeholderToOriginal, detectedMappings.Entries)
				if mergeError != nil {
					return nil, nil, nil, "", fmt.Errorf("合并映射失败: %w", mergeError)
				}
			}

			saveError := g.store.SaveSessionMappings(sessionID, originalToPlaceholder, placeholderToOriginal)
			if saveError != nil {
				return nil, nil, nil, "", fmt.Errorf("保存会话映射失败: %w", saveError)
			}
		}
	} else {
		//注释调试注释
		//fmt.Printf("processRequest sessionID=%q cacheHit=%t mode=full o2p=%d p2o=%d\n", sessionID, cacheHit, len(originalToPlaceholder), len(placeholderToOriginal))
		textNodes, extractionError := requestProtocol.ExtractRequestTextNodes(requestBody)
		if extractionError != nil {
			return nil, nil, nil, "", extractionError
		}

		allTexts := make([]string, 0)
		for _, node := range textNodes {
			if node.Text != "" {
				allTexts = append(allTexts, node.Text)
			}
		}

		if len(allTexts) > 0 {
			combinedText := strings.Join(allTexts, "\n---\n")
			detectedMappings, detectionError := g.trustedLLMClient.DetectMappings(ctx, originalToPlaceholder, combinedText)
			if detectionError != nil {
				return nil, nil, nil, "", fmt.Errorf("可信 LLM 检测失败（全量模式）: %w", detectionError)
			}

			if len(detectedMappings.Entries) > 0 {
				mergeError := MergeMappings(originalToPlaceholder, placeholderToOriginal, detectedMappings.Entries)
				if mergeError != nil {
					return nil, nil, nil, "", fmt.Errorf("合并映射失败（全量模式）: %w", mergeError)
				}
			}
		}

		if sessionID != "" {
			saveError := g.store.SaveSessionMappings(sessionID, originalToPlaceholder, placeholderToOriginal)
			if saveError != nil {
				return nil, nil, nil, "", fmt.Errorf("保存会话映射失败: %w", saveError)
			}
		} else {
			saveError := g.store.SaveRequestMappings(requestID, originalToPlaceholder, placeholderToOriginal)
			if saveError != nil {
				return nil, nil, nil, "", fmt.Errorf("保存请求映射失败: %w", saveError)
			}
		}
	}

	rewrittenBody, err := requestProtocol.RewriteRequest(requestBody, originalToPlaceholder)
	if err != nil {
		return nil, nil, nil, "", err
	}

	return rewrittenBody, originalToPlaceholder, placeholderToOriginal, requestID, nil
}

// buildUpstreamRequest 构建发送给上游 LLM 的 HTTP 请求。
func (g *Masker) buildUpstreamRequest(method string, targetURL string, headers http.Header, requestBody []byte) (*http.Request, error) {
	upstreamRequest, err := http.NewRequest(method, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("创建上游 HTTP 请求失败: %w", err)
	}

	// 复制原始请求的 Header
	for key, values := range headers {
		for _, value := range values {
			upstreamRequest.Header.Add(key, value)
		}
	}
	if upstreamRequest.Header.Get("Content-Type") == "" {
		upstreamRequest.Header.Set("Content-Type", "application/json")
	}

	return upstreamRequest, nil
}

// generateRequestID 生成唯一的请求 ID。
func generateRequestID() string {
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}
