package protocol

import (
	"fmt"
	"strings"
)

// ReplaceByMapping 使用映射表替换字符串中的值。
func ReplaceByMapping(input string, mapping map[string]string) string {
	output := input
	for original, placeholder := range mapping {
		output = strings.ReplaceAll(output, original, placeholder)
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
