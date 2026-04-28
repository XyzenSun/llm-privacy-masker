package masker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// TrustedLLMClient 调用可信 LLM 生成脱敏映射。
type TrustedLLMClient struct {
	clientConfig TrustedLLMClientConfig
	httpClient   *http.Client
}

// NewTrustedLLMClient 创建可信 LLM 客户端。
func NewTrustedLLMClient(config TrustedLLMClientConfig) *TrustedLLMClient {
	return &TrustedLLMClient{
		clientConfig: config,
		httpClient:   &http.Client{},
	}
}

// DetectMappings 调用可信 LLM 检测新增映射。
func (c *TrustedLLMClient) DetectMappings(ctx context.Context, knownMappings map[string]string, newMessage string) (*TrustedLLMMappingResponse, error) {
	requestBody := map[string]any{
		"model": c.clientConfig.ModelName,
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": buildTrustedLLMSystemPrompt(c.clientConfig.SystemPrompt),
			},
			{
				"role":    "user",
				"content": buildTrustedLLMUserPrompt(knownMappings, newMessage),
			},
		},
		"temperature": c.clientConfig.Temperature,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "trusted_llm_mapping_response",
				"strict": true,
				"schema": trustedLLMResponseSchema(),
			},
		},
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("序列化可信 LLM 请求失败: %w", err)
	}

	targetURL := strings.TrimRight(c.clientConfig.BaseURL, "/") + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("创建可信 LLM 请求失败: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	if c.clientConfig.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.clientConfig.APIKey)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("执行可信 LLM 请求失败: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("读取可信 LLM 响应失败: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("可信 LLM 返回状态码 %d: %s", response.StatusCode, string(responseBody))
	}

	responseContent, err := extractOpenAIResponseContent(responseBody)
	if err != nil {
		return nil, err
	}

	return parseTrustedLLMMappingResponse([]byte(responseContent))
}

// parseTrustedLLMMappingResponse 解析并校验可信 LLM 返回的映射。
func parseTrustedLLMMappingResponse(body []byte) (*TrustedLLMMappingResponse, error) {
	var rawResponse map[string]any
	if err := json.Unmarshal(body, &rawResponse); err != nil {
		return nil, fmt.Errorf("可信 LLM 响应内容不是严格 JSON: %w", err)
	}

	entriesValue, ok := rawResponse["entries"]
	if !ok {
		return nil, fmt.Errorf("可信 LLM 响应缺少 entries 字段")
	}

	entries, ok := entriesValue.([]any)
	if !ok {
		return nil, fmt.Errorf("可信 LLM 响应中的 entries 字段无效")
	}

	var response TrustedLLMMappingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("解析可信 LLM 映射响应失败: %w", err)
	}

	for entryIndex, entry := range response.Entries {
		if entry.Original == "" {
			return nil, fmt.Errorf("可信 LLM 映射的第 %d 条 original 为空", entryIndex)
		}
		if entry.Placeholder == "" {
			return nil, fmt.Errorf("可信 LLM 映射的第 %d 条 placeholder 为空", entryIndex)
		}
		if entry.Type == "" {
			return nil, fmt.Errorf("可信 LLM 映射的第 %d 条 type 为空", entryIndex)
		}
		if err := ValidatePlaceholder(entry.Placeholder); err != nil {
			return nil, err
		}
	}

	if len(response.Entries) != len(entries) {
		return nil, fmt.Errorf("可信 LLM 响应中的 entries 字段存在无效条目")
	}

	return &response, nil
}

// extractOpenAIResponseContent 从 OpenAI 格式响应中提取内容。
func extractOpenAIResponseContent(body []byte) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("解析可信 LLM OpenAI 响应失败: %w", err)
	}

	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("可信 LLM 响应中 choices 字段缺失或无效")
	}

	choice, ok := choices[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("可信 LLM 响应中第一个 choice 格式无效")
	}

	message, ok := choice["message"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("可信 LLM 响应中 message 字段无效")
	}

	if content, ok := message["content"].(string); ok && content != "" {
		return content, nil
	}

	// 如果 content 是数组格式
	if parts, ok := message["content"].([]any); ok {
		texts := make([]string, 0)
		for partIndex, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return "", fmt.Errorf("可信 LLM 响应中 content 的第 %d 个部分格式无效", partIndex)
			}
			partText, _ := part["text"].(string)
			if partText != "" {
				texts = append(texts, partText)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n"), nil
		}
	}

	return "", fmt.Errorf("可信 LLM 响应中 content 字段缺失或无效: %s", string(body))
}

// buildTrustedLLMSystemPrompt 构建可信 LLM 的系统提示词。
func buildTrustedLLMSystemPrompt(configuredPrompt string) string {
	if configuredPrompt != "" {
		return configuredPrompt
	}

	return `你是一个隐私信息识别器。请从用户文本中识别手机号、邮箱、身份证号等敏感信息。
只允许返回严格 JSON 对象，不要输出任何额外文本、markdown、代码块或解释。

规则：
1. 顶层必须是对象，且必须包含 entries 数组字段
2. 每个 entry 必须包含 original、placeholder、type 三个非空字符串字段
3. placeholder 必须使用具体类型加数字的格式，例如 ${PHONE_1}、${EMAIL_2}、${ID_CARD_3}
4. placeholder 必须符合当前运行时校验规则，不要返回 ${TYPE_N} 这种示例占位符
5. 如果没有识别到新的敏感信息，返回 {"entries":[]}

正确示例：
{"entries":[{"original":"13812345678","placeholder":"${PHONE_1}","type":"PHONE"}]}

错误示例（禁止返回）：
这里是结果：{"entries":[{"original":"13812345678","placeholder":"${PHONE_1}","type":"PHONE"}]}
{"entries":[{"original":"13812345678","placeholder":"${TYPE_N}","type":"PHONE"}]}`
}

func trustedLLMResponseSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"entries"},
		"properties": map[string]any{
			"entries": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"original", "placeholder", "type"},
					"properties": map[string]any{
						"original":    map[string]any{"type": "string"},
						"placeholder": map[string]any{"type": "string"},
						"type":        map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

// buildTrustedLLMUserPrompt 构建可信 LLM 的用户提示词。
func buildTrustedLLMUserPrompt(knownMappings map[string]string, newMessage string) string {
	knownMappingsJSON, _ := json.Marshal(knownMappings)
	return fmt.Sprintf("已知映射：%s\n待检测文本：%s\n请识别新的敏感信息，并仅返回严格 JSON 对象。", string(knownMappingsJSON), newMessage)
}
