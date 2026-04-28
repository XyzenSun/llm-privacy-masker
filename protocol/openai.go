package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OpenAI 实现 OpenAI 协议处理器。
type OpenAI struct{}

// NewOpenAI 创建 OpenAI 协议实例。
func NewOpenAI() *OpenAI {
	return &OpenAI{}
}

// ExtractRequestTextNodes 提取 OpenAI 请求中的文本节点，忽略 system 消息。
func (o *OpenAI) ExtractRequestTextNodes(body []byte) ([]TextNode, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 请求失败: %w", err)
	}

	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("OpenAI 请求中 messages 字段缺失或无效")
	}

	textNodes := make([]TextNode, 0)
	for msgIndex, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("OpenAI 请求中第 %d 条消息格式无效", msgIndex)
		}

		role, _ := message["role"].(string)
		// 忽略 system 消息
		if role == "system" {
			continue
		}

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
					return nil, fmt.Errorf("OpenAI 请求中第 %d 条消息的第 %d 个内容块格式无效", msgIndex, partIndex)
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
func (o *OpenAI) LatestUserText(body []byte) (string, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false, fmt.Errorf("解析 OpenAI 请求失败: %w", err)
	}

	messages, ok := payload["messages"].([]any)
	if !ok {
		return "", false, fmt.Errorf("OpenAI 请求中 messages 字段缺失或无效")
	}

	// 从后向前遍历，找到最后一个用户消息
	for msgIndex := len(messages) - 1; msgIndex >= 0; msgIndex-- {
		message, ok := messages[msgIndex].(map[string]any)
		if !ok {
			return "", false, fmt.Errorf("OpenAI 请求中第 %d 条消息格式无效", msgIndex)
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
					return "", false, fmt.Errorf("OpenAI 请求中第 %d 条消息的第 %d 个内容块格式无效", msgIndex, partIndex)
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

// RewriteRequest 使用映射表改写 OpenAI 请求文本，并强制关闭流式传输。
func (o *OpenAI) RewriteRequest(body []byte, originalToPlaceholder map[string]string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 请求失败: %w", err)
	}

	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("OpenAI 请求中 messages 字段缺失或无效")
	}

	for msgIndex, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("OpenAI 请求中第 %d 条消息格式无效", msgIndex)
		}

		role, _ := message["role"].(string)
		// 忽略 system 消息
		if role == "system" {
			continue
		}

		switch content := message["content"].(type) {
		case string:
			message["content"] = ReplaceByMapping(content, originalToPlaceholder)
		case []any:
			for partIndex, rawPart := range content {
				part, ok := rawPart.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("OpenAI 请求中第 %d 条消息的第 %d 个内容块格式无效", msgIndex, partIndex)
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
		return nil, fmt.Errorf("序列化 OpenAI 请求失败: %w", err)
	}

	return rewritten, nil
}

// ExtractResponseTextNodes 提取 OpenAI 响应中的文本节点。
func (o *OpenAI) ExtractResponseTextNodes(body []byte) ([]TextNode, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 响应失败: %w", err)
	}

	choices, ok := payload["choices"].([]any)
	if !ok {
		return nil, fmt.Errorf("OpenAI 响应中 choices 字段缺失或无效")
	}

	textNodes := make([]TextNode, 0)
	for choiceIndex, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("OpenAI 响应中第 %d 个 choice 格式无效", choiceIndex)
		}

		message, ok := choice["message"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("OpenAI 响应中第 %d 个 choice 的 message 字段无效", choiceIndex)
		}

		content, ok := message["content"].(string)
		if !ok || content == "" {
			continue
		}

		textNodes = append(textNodes, TextNode{
			Path: fmt.Sprintf("choices.%d.message.content", choiceIndex),
			Role: "assistant",
			Text: content,
		})
	}

	return textNodes, nil
}

// RewriteResponse 使用映射表改写 OpenAI 响应文本。
func (o *OpenAI) RewriteResponse(body []byte, placeholderToOriginal map[string]string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 响应失败: %w", err)
	}

	choices, ok := payload["choices"].([]any)
	if !ok {
		return nil, fmt.Errorf("OpenAI 响应中 choices 字段缺失或无效")
	}

	for choiceIndex, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("OpenAI 响应中第 %d 个 choice 格式无效", choiceIndex)
		}

		message, ok := choice["message"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("OpenAI 响应中第 %d 个 choice 的 message 字段无效", choiceIndex)
		}

		content, ok := message["content"].(string)
		if !ok {
			continue
		}

		message["content"] = ReplaceByMapping(content, placeholderToOriginal)
	}

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 OpenAI 响应失败: %w", err)
	}

	return rewritten, nil
}

// ForceNonStream OpenAI 协议在请求体中设置 stream=false，URL 不变。
func (o *OpenAI) ForceNonStream(url string, body []byte) (string, []byte) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		// 解析失败，返回原值
		return url, body
	}
	payload["stream"] = false
	newBody, err := json.Marshal(payload)
	if err != nil {
		return url, body
	}
	return url, newBody
}

// SetAPIKey OpenAI 协议无需在 URL 中设置 API Key，URL 不变。
// OpenAI 的 API Key 通过 HTTP 请求头 Authorization: Bearer <key> 设置。
func (o *OpenAI) SetAPIKey(url string, apiKey string) string {
	// OpenAI 协议的 API Key 通过请求头设置，URL 不需要修改
	return url
}