package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Anthropic 实现 Anthropic 协议处理器。
type Anthropic struct{}

// NewAnthropic 创建 Anthropic 协议实例。
func NewAnthropic() *Anthropic {
	return &Anthropic{}
}

// ExtractRequestTextNodes 提取 Anthropic 请求中的文本节点，忽略顶层 system 字段。
func (a *Anthropic) ExtractRequestTextNodes(body []byte) ([]TextNode, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Anthropic 请求失败: %w", err)
	}

	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("Anthropic 请求中 messages 字段缺失或无效")
	}

	textNodes := make([]TextNode, 0)
	for msgIndex, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Anthropic 请求中第 %d 条消息格式无效", msgIndex)
		}

		role, _ := message["role"].(string)
		content := message["content"]

		switch typedContent := content.(type) {
		case string:
			textNodes = append(textNodes, TextNode{
				Path: fmt.Sprintf("messages.%d.content", msgIndex),
				Role: role,
				Text: typedContent,
			})
		case []any:
			for partIndex, rawPart := range typedContent {
				part, ok := rawPart.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("Anthropic 请求中第 %d 条消息的第 %d 个内容块格式无效", msgIndex, partIndex)
				}

				partType, _ := part["type"].(string)
				partText, _ := part["text"].(string)
				if partType != "text" || partText == "" {
					continue
				}

				textNodes = append(textNodes, TextNode{
					Path: fmt.Sprintf("messages.%d.content.%d.text", msgIndex, partIndex),
					Role: role,
					Text: partText,
				})
			}
		}
	}

	return textNodes, nil
}

// LatestUserText 返回最后一个用户消息中的文本内容。
func (a *Anthropic) LatestUserText(body []byte) (string, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false, fmt.Errorf("解析 Anthropic 请求失败: %w", err)
	}

	messages, ok := payload["messages"].([]any)
	if !ok {
		return "", false, fmt.Errorf("Anthropic 请求中 messages 字段缺失或无效")
	}

	// 从后向前遍历，找到最后一个用户消息
	for msgIndex := len(messages) - 1; msgIndex >= 0; msgIndex-- {
		message, ok := messages[msgIndex].(map[string]any)
		if !ok {
			return "", false, fmt.Errorf("Anthropic 请求中第 %d 条消息格式无效", msgIndex)
		}

		role, _ := message["role"].(string)
		if role != "user" {
			continue
		}

		content := message["content"]
		switch typedContent := content.(type) {
		case string:
			return typedContent, true, nil
		case []any:
			userTexts := make([]string, 0)
			for partIndex, rawPart := range typedContent {
				part, ok := rawPart.(map[string]any)
				if !ok {
					return "", false, fmt.Errorf("Anthropic 请求中第 %d 条消息的第 %d 个内容块格式无效", msgIndex, partIndex)
				}

				partType, _ := part["type"].(string)
				partText, _ := part["text"].(string)
				if partType == "text" && partText != "" {
					userTexts = append(userTexts, partText)
				}
			}

			if len(userTexts) == 0 {
				return "", false, nil
			}

			return strings.Join(userTexts, "\n"), true, nil
		}
	}

	return "", false, nil
}

// RewriteRequest 使用映射表改写 Anthropic 请求文本，并强制关闭流式传输。
func (a *Anthropic) RewriteRequest(body []byte, originalToPlaceholder map[string]string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Anthropic 请求失败: %w", err)
	}

	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("Anthropic 请求中 messages 字段缺失或无效")
	}

	for msgIndex, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Anthropic 请求中第 %d 条消息格式无效", msgIndex)
		}

		switch content := message["content"].(type) {
		case string:
			message["content"] = ReplaceByMapping(content, originalToPlaceholder)
		case []any:
			for partIndex, rawPart := range content {
				part, ok := rawPart.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("Anthropic 请求中第 %d 条消息的第 %d 个内容块格式无效", msgIndex, partIndex)
				}

				partType, _ := part["type"].(string)
				partText, _ := part["text"].(string)
				if partType == "text" {
					part["text"] = ReplaceByMapping(partText, originalToPlaceholder)
				}
			}
		}
	}

	// 强制关闭流式传输
	payload["stream"] = false

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 Anthropic 请求失败: %w", err)
	}

	return rewritten, nil
}

// ExtractResponseTextNodes 提取 Anthropic 响应中的文本节点。
func (a *Anthropic) ExtractResponseTextNodes(body []byte) ([]TextNode, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Anthropic 响应失败: %w", err)
	}

	content, ok := payload["content"].([]any)
	if !ok {
		return nil, fmt.Errorf("Anthropic 响应中 content 字段缺失或无效")
	}

	textNodes := make([]TextNode, 0)
	for partIndex, rawPart := range content {
		part, ok := rawPart.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Anthropic 响应中第 %d 个内容块格式无效", partIndex)
		}

		partType, _ := part["type"].(string)
		partText, _ := part["text"].(string)
		if partType != "text" || partText == "" {
			continue
		}

		textNodes = append(textNodes, TextNode{
			Path: fmt.Sprintf("content.%d.text", partIndex),
			Role: "assistant",
			Text: partText,
		})
	}

	return textNodes, nil
}

// RewriteResponse 使用映射表改写 Anthropic 响应文本。
func (a *Anthropic) RewriteResponse(body []byte, placeholderToOriginal map[string]string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Anthropic 响应失败: %w", err)
	}

	content, ok := payload["content"].([]any)
	if !ok {
		return nil, fmt.Errorf("Anthropic 响应中 content 字段缺失或无效")
	}

	for partIndex, rawPart := range content {
		part, ok := rawPart.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Anthropic 响应中第 %d 个内容块格式无效", partIndex)
		}

		partType, _ := part["type"].(string)
		partText, _ := part["text"].(string)
		if partType == "text" {
			part["text"] = ReplaceByMapping(partText, placeholderToOriginal)
		}
	}

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 Anthropic 响应失败: %w", err)
	}

	return rewritten, nil
}

// ForceNonStream Anthropic 协议在请求体中设置 stream=false，URL 不变。
func (a *Anthropic) ForceNonStream(url string, body []byte) (string, []byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, fmt.Errorf("Anthropic 强制关闭流式传输失败：解析请求体失败: %w", err)
	}
	payload["stream"] = false
	newBody, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("Anthropic 强制关闭流式传输失败：序列化请求体失败: %w", err)
	}
	return url, newBody, nil
}

// SetAPIKey Anthropic 协议无需在 URL 中设置 API Key，URL 不变。
// Anthropic 的 API Key 通过 HTTP 请求头 x-api-key 设置。
func (a *Anthropic) SetAPIKey(url string, apiKey string) string {
	// Anthropic 协议的 API Key 通过请求头设置，URL 不需要修改
	return url
}