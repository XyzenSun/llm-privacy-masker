package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	masker "github.com/xyzensun/llm-privacy-masker"
)

// ==================== 环境配置 ====================

type envConfig struct {
	trustedLLMURL    string
	trustedLLMAPIKey string
	trustedLLMModel  string
	cloudLLMURL      string
	cloudLLMAPIKey   string
	cloudLLMModel    string
}

const testTimeout = 180 * time.Second

// ==================== .env 文件加载 ====================

func loadEnvConfig(envFilePath string) (*envConfig, error) {
	fileContent, err := os.ReadFile(envFilePath)
	if err != nil {
		return nil, fmt.Errorf("读取 .env 文件失败: %w", err)
	}

	envMap := make(map[string]string)
	for _, line := range strings.Split(string(fileContent), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		equalIndex := strings.Index(line, "=")
		if equalIndex == -1 {
			continue
		}
		key := strings.TrimSpace(line[:equalIndex])
		value := strings.TrimSpace(line[equalIndex+1:])
		envMap[key] = value
	}

	config := &envConfig{
		trustedLLMURL:    envMap["trustedllm_url"],
		trustedLLMAPIKey: envMap["trustedllm_api_key"],
		trustedLLMModel:  envMap["trustedllm_model"],
		cloudLLMURL:      envMap["cloudllm_url"],
		cloudLLMAPIKey:   envMap["cloudllm_api_key"],
		cloudLLMModel:    envMap["cloudllm_model"],
	}

	if config.trustedLLMURL == "" {
		return nil, fmt.Errorf(".env 中缺少 trustedllm_url 配置")
	}
	if config.trustedLLMModel == "" {
		return nil, fmt.Errorf(".env 中缺少 trustedllm_model 配置")
	}
	if config.cloudLLMURL == "" {
		return nil, fmt.Errorf(".env 中缺少 cloudllm_url 配置")
	}
	if config.cloudLLMAPIKey == "" {
		return nil, fmt.Errorf(".env 中缺少 cloudllm_api_key 配置")
	}
	if config.cloudLLMModel == "" {
		return nil, fmt.Errorf(".env 中缺少 cloudllm_model 配置")
	}

	return config, nil
}

// ==================== Masker 实例创建 ====================

func createMaskerInstance(env *envConfig) (*masker.Masker, error) {
	return masker.New().
		WithTimeout(testTimeout).
		WithSessionStoreType("memory").
		WithTrustedLLMBaseURL(env.trustedLLMURL).
		WithTrustedLLMAPIKey(env.trustedLLMAPIKey).
		WithTrustedLLMModelName(env.trustedLLMModel).
		WithTrustedLLMTemperature(0.0).
		Build()
}

func createTrustedLLMClient(env *envConfig) *masker.TrustedLLMClient {
	return masker.NewTrustedLLMClient(masker.TrustedLLMClientConfig{
		BaseURL:     env.trustedLLMURL,
		APIKey:      env.trustedLLMAPIKey,
		ModelName:   env.trustedLLMModel,
		Temperature: 0.0,
	})
}

// ==================== 请求构建 ====================

func buildOpenAIChatRequest(cloudLLMURL string, cloudLLMAPIKey string, cloudLLMModel string, userMessages []map[string]string) (*http.Request, error) {
	messages := make([]map[string]any, 0, len(userMessages))
	for _, msg := range userMessages {
		messages = append(messages, map[string]any{
			"role":    msg["role"],
			"content": msg["content"],
		})
	}

	requestBody := map[string]any{
		"model":    cloudLLMModel,
		"messages": messages,
		"stream":   false,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	requestURL := strings.TrimRight(cloudLLMURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cloudLLMAPIKey)
	return req, nil
}

// ==================== 响应解析 ====================

func parseOpenAIChatResponse(resp *http.Response) (string, error) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应体失败: %w", err)
	}
	defer resp.Body.Close()

	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return "", fmt.Errorf("解析响应 JSON 失败: %w", err)
	}

	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("响应中 choices 字段缺失或为空")
	}

	choice, ok := choices[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("响应中第一个 choice 格式无效")
	}

	message, ok := choice["message"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("响应中 message 字段格式无效")
	}

	content, ok := message["content"].(string)
	if !ok {
		return "", fmt.Errorf("响应中 content 字段格式无效")
	}

	return content, nil
}

// ==================== 测试场景 ====================

// testWithoutSessionID 无 sessionID 的单次请求测试
func testWithoutSessionID(m *masker.Masker, trustedClient *masker.TrustedLLMClient, env *envConfig) {
	fmt.Println("\n========== 场景1：sessionID 为空（单次无状态请求）==========")

	userContent := "我的手机号是13812345678，邮箱是test@example.com，身份证号是320102199001011234"
	userMessages := []map[string]string{
		{"role": "user", "content": userContent},
	}

	// 打印发送的提示词
	fmt.Printf("\n[发送提示词]\n%s\n", userContent)

	// 调用 trustedLLM 检测敏感信息并打印结果
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	detectedMappings, err := trustedClient.DetectMappings(ctx, map[string]string{}, userContent)
	if err != nil {
		fmt.Printf("[TrustedLLM 检测失败] %v\n", err)
		return
	}
	fmt.Printf("\n[TrustedLLM 检测结果]\n")
	for _, entry := range detectedMappings.Entries {
		fmt.Printf("  %s -> %s (类型: %s)\n", entry.Original, entry.Placeholder, entry.Type)
	}
	if len(detectedMappings.Entries) == 0 {
		fmt.Println("  (未检测到敏感信息)")
	}

	// 调用 Masker.Process 并打印云端 LLM 响应
	req, err := buildOpenAIChatRequest(env.cloudLLMURL, env.cloudLLMAPIKey, env.cloudLLMModel, userMessages)
	if err != nil {
		fmt.Printf("构造请求失败: %v\n", err)
		return
	}

	resp, err := m.Process(req)
	if err != nil {
		fmt.Printf("Masker.Process 执行失败: %v\n", err)
		return
	}

	finalContent, err := parseOpenAIChatResponse(resp)
	if err != nil {
		fmt.Printf("解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("\n[经过 llm-privacy-masker 脱敏-转发-反脱敏后的最终响应]\n%s\n", finalContent)
}

// testWithSessionID 有 sessionID 的多轮对话测试
func testWithSessionID(m *masker.Masker, trustedClient *masker.TrustedLLMClient, env *envConfig) {
	fmt.Println("\n========== 场景2：sessionID 不为空（多轮有状态对话）==========")

	sessionID := "real-test-session-001"

	// ---- 第一轮 ----
	fmt.Println("\n--- 第一轮 ---")
	firstContent := "我的手机号是13812345678，邮箱是test@example.com"
	firstMessages := []map[string]string{
		{"role": "user", "content": firstContent},
	}

	fmt.Printf("\n[发送提示词]\n%s\n", firstContent)

	ctx1, cancel1 := context.WithTimeout(context.Background(), testTimeout)
	defer cancel1()
	detected1, err := trustedClient.DetectMappings(ctx1, map[string]string{}, firstContent)
	if err != nil {
		fmt.Printf("[TrustedLLM 检测失败] %v\n", err)
		return
	}
	fmt.Printf("\n[TrustedLLM 检测结果]\n")
	for _, entry := range detected1.Entries {
		fmt.Printf("  %s -> %s (类型: %s)\n", entry.Original, entry.Placeholder, entry.Type)
	}
	if len(detected1.Entries) == 0 {
		fmt.Println("  (未检测到敏感信息)")
	}

	req1, err := buildOpenAIChatRequest(env.cloudLLMURL, env.cloudLLMAPIKey, env.cloudLLMModel, firstMessages)
	if err != nil {
		fmt.Printf("构造请求失败: %v\n", err)
		return
	}

	resp1, err := m.Process(req1, sessionID)
	if err != nil {
		fmt.Printf("Masker.Process 执行失败: %v\n", err)
		return
	}

	content1, err := parseOpenAIChatResponse(resp1)
	if err != nil {
		fmt.Printf("解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("\n[经过 llm-privacy-masker 脱敏-转发-反脱敏后的最终响应]\n%s\n", content1)

	// ---- 第二轮 ----
	fmt.Println("\n--- 第二轮 ---")
	secondMessages := []map[string]string{
		{"role": "user", "content": "我的手机号是13812345678"},
		{"role": "assistant", "content": "已记录您的手机号和邮箱信息"},
		{"role": "user", "content": "我的身份证号是320102199001011234"},
	}

	// 第二轮发送的提示词是最后一条用户消息
	fmt.Printf("\n[发送提示词]\n%s\n", "我的身份证号是320102199001011234")

	// 构建已知映射用于增量检测
	knownMappings := map[string]string{}
	for _, entry := range detected1.Entries {
		knownMappings[entry.Original] = entry.Placeholder
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), testTimeout)
	defer cancel2()
	detected2, err := trustedClient.DetectMappings(ctx2, knownMappings, "我的手机号是13812345678\n我的身份证号是320102199001011234")
	if err != nil {
		fmt.Printf("[TrustedLLM 检测失败] %v\n", err)
		return
	}
	fmt.Printf("\n[TrustedLLM 检测结果（增量）]\n")
	for _, entry := range detected2.Entries {
		fmt.Printf("  %s -> %s (类型: %s)\n", entry.Original, entry.Placeholder, entry.Type)
	}
	if len(detected2.Entries) == 0 {
		fmt.Println("  (无新增敏感信息)")
	}

	req2, err := buildOpenAIChatRequest(env.cloudLLMURL, env.cloudLLMAPIKey, env.cloudLLMModel, secondMessages)
	if err != nil {
		fmt.Printf("构造请求失败: %v\n", err)
		return
	}

	resp2, err := m.Process(req2, sessionID)
	if err != nil {
		fmt.Printf("Masker.Process 执行失败: %v\n", err)
		return
	}

	content2, err := parseOpenAIChatResponse(resp2)
	if err != nil {
		fmt.Printf("解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("\n[经过 llm-privacy-masker 脱敏-转发-反脱敏后的最终响应]\n%s\n", content2)
}

// ==================== 主函数 ====================

func main() {
	env, err := loadEnvConfig(".env")
	if err != nil {
		fmt.Printf("加载环境配置失败: %v\n", err)
		os.Exit(1)
	}

	maskerInstance, err := createMaskerInstance(env)
	if err != nil {
		fmt.Printf("创建 Masker 实例失败: %v\n", err)
		os.Exit(1)
	}

	trustedClient := createTrustedLLMClient(env)

	testWithoutSessionID(maskerInstance, trustedClient, env)
	testWithSessionID(maskerInstance, trustedClient, env)

	fmt.Println("\n测试执行完毕")
}