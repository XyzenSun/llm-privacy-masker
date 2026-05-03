package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Gemini 实现 Gemini 协议处理器。
type Gemini struct{}

// NewGemini 创建 Gemini 协议实例。
func NewGemini() *Gemini {
	return &Gemini{}
}

// ExtractRequestTextNodes 提取 Gemini 请求中的文本节点，忽略 systemInstruction 字段。
func (g *Gemini) ExtractRequestTextNodes(body []byte) ([]TextNode, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Gemini 请求失败: %w", err)
	}

	contents, ok := payload["contents"].([]any)
	if !ok {
		return nil, fmt.Errorf("Gemini 请求中 contents 字段缺失或无效")
	}

	textNodes := make([]TextNode, 0)
	for contentIndex, rawContent := range contents {
		content, ok := rawContent.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 请求中第 %d 个 content 格式无效", contentIndex)
		}

		role, _ := content["role"].(string)
		parts, ok := content["parts"].([]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 请求中第 %d 个 content 的 parts 字段缺失或无效", contentIndex)
		}

		for partIndex, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("Gemini 请求中第 %d 个 content 的第 %d 个 part 格式无效", contentIndex, partIndex)
			}

			partText, _ := part["text"].(string)
			if partText == "" {
				continue
			}

			textNodes = append(textNodes, TextNode{
				Path: fmt.Sprintf("contents.%d.parts.%d.text", contentIndex, partIndex),
				Role: role,
				Text: partText,
			})
		}
	}

	return textNodes, nil
}

// LatestUserText 返回最后一个用户消息中的文本内容。
func (g *Gemini) LatestUserText(body []byte) (string, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false, fmt.Errorf("解析 Gemini 请求失败: %w", err)
	}

	contents, ok := payload["contents"].([]any)
	if !ok {
		return "", false, fmt.Errorf("Gemini 请求中 contents 字段缺失或无效")
	}

	// 从后向前遍历，找到最后一个用户消息
	for contentIndex := len(contents) - 1; contentIndex >= 0; contentIndex-- {
		content, ok := contents[contentIndex].(map[string]any)
		if !ok {
			return "", false, fmt.Errorf("Gemini 请求中第 %d 个 content 格式无效", contentIndex)
		}

		role, _ := content["role"].(string)
		if role != "user" {
			continue
		}

		parts, ok := content["parts"].([]any)
		if !ok {
			return "", false, fmt.Errorf("Gemini 请求中第 %d 个 content 的 parts 字段缺失或无效", contentIndex)
		}

		userTexts := make([]string, 0)
		for partIndex, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return "", false, fmt.Errorf("Gemini 请求中第 %d 个 content 的第 %d 个 part 格式无效", contentIndex, partIndex)
			}

			partText, _ := part["text"].(string)
			if partText != "" {
				userTexts = append(userTexts, partText)
			}
		}

		if len(userTexts) == 0 {
			return "", false, nil
		}

		return strings.Join(userTexts, "\n"), true, nil
	}

	return "", false, nil
}

// RewriteRequest 使用映射表改写 Gemini 请求文本。
func (g *Gemini) RewriteRequest(body []byte, originalToPlaceholder map[string]string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Gemini 请求失败: %w", err)
	}

	contents, ok := payload["contents"].([]any)
	if !ok {
		return nil, fmt.Errorf("Gemini 请求中 contents 字段缺失或无效")
	}

	for contentIndex, rawContent := range contents {
		content, ok := rawContent.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 请求中第 %d 个 content 格式无效", contentIndex)
		}

		parts, ok := content["parts"].([]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 请求中第 %d 个 content 的 parts 字段缺失或无效", contentIndex)
		}

		for partIndex, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("Gemini 请求中第 %d 个 content 的第 %d 个 part 格式无效", contentIndex, partIndex)
			}

			partText, _ := part["text"].(string)
			if partText != "" {
				part["text"] = ReplaceByMapping(partText, originalToPlaceholder)
			}
		}
	}

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 Gemini 请求失败: %w", err)
	}

	return rewritten, nil
}

// ExtractResponseTextNodes 提取 Gemini 响应中的文本节点。
func (g *Gemini) ExtractResponseTextNodes(body []byte) ([]TextNode, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Gemini 响应失败: %w", err)
	}

	candidates, ok := payload["candidates"].([]any)
	if !ok {
		return nil, fmt.Errorf("Gemini 响应中 candidates 字段缺失或无效")
	}

	textNodes := make([]TextNode, 0)
	for candidateIndex, rawCandidate := range candidates {
		candidate, ok := rawCandidate.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 响应中第 %d 个 candidate 格式无效", candidateIndex)
		}

		content, ok := candidate["content"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 响应中第 %d 个 candidate 的 content 字段无效", candidateIndex)
		}

		role, _ := content["role"].(string)
		parts, ok := content["parts"].([]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 响应中第 %d 个 candidate 的 parts 字段缺失或无效", candidateIndex)
		}

		for partIndex, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("Gemini 响应中第 %d 个 candidate 的第 %d 个 part 格式无效", candidateIndex, partIndex)
			}

			partText, _ := part["text"].(string)
			if partText == "" {
				continue
			}

			textNodes = append(textNodes, TextNode{
				Path: fmt.Sprintf("candidates.%d.content.parts.%d.text", candidateIndex, partIndex),
				Role: role,
				Text: partText,
			})
		}
	}

	return textNodes, nil
}

// RewriteResponse 使用映射表改写 Gemini 响应文本。
func (g *Gemini) RewriteResponse(body []byte, placeholderToOriginal map[string]string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 Gemini 响应失败: %w", err)
	}

	candidates, ok := payload["candidates"].([]any)
	if !ok {
		return nil, fmt.Errorf("Gemini 响应中 candidates 字段缺失或无效")
	}

	for candidateIndex, rawCandidate := range candidates {
		candidate, ok := rawCandidate.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 响应中第 %d 个 candidate 格式无效", candidateIndex)
		}

		content, ok := candidate["content"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 响应中第 %d 个 candidate 的 content 字段无效", candidateIndex)
		}

		parts, ok := content["parts"].([]any)
		if !ok {
			return nil, fmt.Errorf("Gemini 响应中第 %d 个 candidate 的 parts 字段缺失或无效", candidateIndex)
		}

		for partIndex, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("Gemini 响应中第 %d 个 candidate 的第 %d 个 part 格式无效", candidateIndex, partIndex)
			}

			partText, _ := part["text"].(string)
			if partText != "" {
				part["text"] = ReplaceByMapping(partText, placeholderToOriginal)
			}
		}
	}

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 Gemini 响应失败: %w", err)
	}

	return rewritten, nil
}

// ForceNonStream Gemini 协议将 URL 从流式端点改为非流式端点，请求体不变。
// :streamGenerateContent -> :generateContent
func (g *Gemini) ForceNonStream(url string, body []byte) (string, []byte, error) {
	newURL := strings.Replace(url, ":streamGenerateContent", ":generateContent", 1)
	return newURL, body, nil
}

// SetAPIKey Gemini 协议在 URL 中添加 key 参数。
func (g *Gemini) SetAPIKey(url string, apiKey string) string {
	if apiKey == "" {
		return url
	}
	// URL 已包含 key 参数，无需处理
	if strings.Contains(url, "key=") {
		return url
	}
	// 根据 URL 是否已有参数决定添加方式
	if strings.Contains(url, "?") {
		return url + "&key=" + apiKey
	}
	return url + "?key=" + apiKey
}