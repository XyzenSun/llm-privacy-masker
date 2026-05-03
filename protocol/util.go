package protocol

import (
	"fmt"
	"sort"
	"strings"
)

// ReplaceByMapping 使用映射表替换字符串中的值，按长字符串优先排序以避免短字符串误匹配。
func ReplaceByMapping(input string, mapping map[string]string) string {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}

	// 按长度降序排序，相同长度按字典序排序，确保长字符串优先替换
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})

	output := input
	for _, key := range keys {
		output = strings.ReplaceAll(output, key, mapping[key])
	}
	return output
}

// judgeProtocolByURL 根据 URL 判断协议类型。
func judgeProtocolByURL(url string) (ProtocolType, error) {
	if strings.Contains(url, "generateContent") || strings.Contains(url, "/models/") || strings.Contains(url, "streamGenerateContent") {
		return ProtocolTypeGemini, nil
	}
	if strings.Contains(url, "/v1/messages") {
		return ProtocolTypeAnthropic, nil
	}
	if strings.Contains(url, "/v1/chat/completions") || strings.Contains(url, "/chat/completions") {
		return ProtocolTypeOpenAI, nil
	}
	return "", fmt.Errorf("无法根据 URL 判断协议: %s", url)
}

// JudgeProtocolByRequestBody 暂不实现。
func JudgeProtocolByRequestBody(body []byte) ProtocolType {
	_ = body
	return ""
}

// JudgeProtocol 综合判断协议类型。
func JudgeProtocol(url string, body []byte) (ProtocolType, error) {
	protocolType, err := judgeProtocolByURL(url)
	if err == nil {
		return protocolType, nil
	}

	protocolType = JudgeProtocolByRequestBody(body)
	if protocolType != "" {
		return protocolType, nil
	}

	return "", fmt.Errorf("无法判断协议: %w", err)
}
