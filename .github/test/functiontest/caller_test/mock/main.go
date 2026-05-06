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

// ==================== 常量定义 ====================

// mockLLMPort Mock TrustedLLM 服务监听端口
const mockLLMPort = 18080

// mockLLMBaseURL Mock TrustedLLM 服务基础地址
const mockLLMBaseURL = "http://127.0.0.1:18080"

// mockLLMModelName Mock TrustedLLM 模型名称
const mockLLMModelName = "mock-trusted-llm"

// mockLLMAPIKey Mock TrustedLLM API 密钥
const mockLLMAPIKey = "mock-api-key"

// mockLLMTemperature Mock TrustedLLM 温度参数
const mockLLMTemperature = 0.0

// mockUpstreamPort Mock 上游 LLM（接收脱敏后请求的真实 LLM）服务监听端口
const mockUpstreamPort = 18081

// mockUpstreamBaseURL Mock 上游 LLM 服务基础地址
const mockUpstreamBaseURL = "http://127.0.0.1:18081"

// testTimeout 测试请求超时时间
const testTimeout = 30 * time.Second

// ==================== Mock 固定检测结果定义 ====================
// Mock TrustedLLM 对于包含敏感信息的文本，始终返回以下固定映射：
// - 手机号 "13812345678" -> "${PHONE_1}"
// - 邮箱 "test@example.com" -> "${EMAIL_1}"
// - 身份证号 "320102199001011234" -> "${ID_CARD_1}"

// mockFixedMappingEntries Mock TrustedLLM 固定返回的映射条目
var mockFixedMappingEntries = []map[string]string{
	{"original": "13812345678", "placeholder": "${PHONE_1}", "type": "PHONE"},
	{"original": "test@example.com", "placeholder": "${EMAIL_1}", "type": "EMAIL"},
	{"original": "320102199001011234", "placeholder": "${ID_CARD_1}", "type": "ID_CARD"},
}

// ==================== Mock TrustedLLM 服务端 ====================

// runMockLLM 启动 Mock TrustedLLM 服务端。
// 该服务模拟一个 OpenAI 兼格式的 /chat/completions 接口，
// 对任何请求均返回固定的敏感信息映射结果，无需真实 LLM 模型参与。
func runMockLLM() *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/chat/completions", handleMockLLMChatCompletions)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", mockLLMPort),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("❌ Mock TrustedLLM 服务启动失败: %v\n", err)
		}
	}()

	// 等待服务就绪
	waitForServerReady(mockLLMBaseURL)
	fmt.Printf("✅ Mock TrustedLLM 服务已启动，监听端口: %d\n", mockLLMPort)

	return server
}

// handleMockLLMChatCompletions 处理 Mock TrustedLLM 的 /chat/completions 请求。
// 无论请求内容如何，始终返回固定的敏感信息映射结果，格式遵循 OpenAI Chat Completions 响应规范。
func handleMockLLMChatCompletions(w http.ResponseWriter, r *http.Request) {
	// 校验请求方法必须为 POST
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 方法", http.StatusMethodNotAllowed)
		return
	}

	// 校验 Authorization 备注头中的 API Key
	authHeader := r.Header.Get("Authorization")
	expectedAuth := "Bearer " + mockLLMAPIKey
	if authHeader != expectedAuth {
		http.Error(w, "API Key 校验失败", http.StatusUnauthorized)
		return
	}

	// 读取请求体以确认格式合法（不使用内容，仅做解析校验）
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "读取请求体失败", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var requestPayload map[string]any
	if err := json.Unmarshal(requestBody, &requestPayload); err != nil {
		http.Error(w, "请求体 JSON 解析失败", http.StatusBadRequest)
		return
	}

	// 从请求中提取 user 消息，用于判断是否需要返回检测结果
	// 如果用户文本中包含任何已知敏感信息关键词，则返回映射；否则返回空映射
	shouldReturnMappings := false
	userContent := extractUserContentFromRequest(requestPayload)

	sensitiveKeywords := []string{"13812345678", "test@example.com", "320102199001011234"}
	for _, keyword := range sensitiveKeywords {
		if strings.Contains(userContent, keyword) {
			shouldReturnMappings = true
			break
		}
	}

	// 构建固定映射响应内容
	var entries []map[string]string
	if shouldReturnMappings {
		entries = mockFixedMappingEntries
	} else {
		entries = []map[string]string{}
	}

	mappingResponse := map[string]any{
		"entries": entries,
	}
	mappingJSON, _ := json.Marshal(mappingResponse)

	// 构造 OpenAI Chat Completions 格式的响应外壳
	openAIResponse := map[string]any{
		"id":      "mock-chatcmpl-001",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   mockLLMModelName,
		"choices": []map[string]any{
			{
				"index":   0,
				"message": map[string]any{
					"role":    "assistant",
					"content": string(mappingJSON),
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 20,
			"total_tokens":      30,
		},
	}

	responseJSON, err := json.Marshal(openAIResponse)
	if err != nil {
		http.Error(w, "序列化响应失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseJSON)
}

// extractUserContentFromRequest 从 OpenAI 格式的请求体中提取用户消息内容
func extractUserContentFromRequest(requestPayload map[string]any) string {
	messages, ok := requestPayload["messages"].([]any)
	if !ok {
		return ""
	}

	// 从后向前遍历，提取最后一条 user 消息
	for i := len(messages) - 1; i >= 0; i-- {
		message, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		if role == "user" {
			content, _ := message["content"].(string)
			return content
		}
	}

	return ""
}

// ==================== Mock 上游 LLM 服务端 ====================
// 上游 LLM 是 Masker 脱敏后转发请求的目标。
// Mock 上游 LLM 接收脱敏后的请求，返回一个包含占位符的固定响应，
// 用于验证 Masker 反脱敏（将占位符还原为原始值）的逻辑是否正确。

// runMockUpstreamLLM 启动 Mock 上游 LLM 服务端。
// 该服务模拟接收脱敏请求的上游 LLM，返回包含占位符的固定回复。
func runMockUpstreamLLM() *http.Server {
	mux := http.NewServeMux()

	// 注册 /v1/chat/completions 路径，这是 OpenAI 协议的上游 LLM 接口路径
	mux.HandleFunc("/v1/chat/completions", handleMockUpstreamChatCompletions)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", mockUpstreamPort),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("❌ Mock 上游 LLM 服务启动失败: %v\n", err)
		}
	}()

	// 等待服务就绪
	waitForServerReady(mockUpstreamBaseURL)
	fmt.Printf("✅ Mock 上游 LLM 服务已启动，监听端口: %d\n", mockUpstreamPort)

	return server
}

// handleMockUpstreamChatCompletions 处理 Mock 上游 LLM 的 /v1/chat/completions 请求。
// 读取脱敏后的用户消息，返回一段包含占位符的固定回复文本，
// 用于验证 Masker 的反脱敏流程是否能正确将占位符还原为原始敏感值。
func handleMockUpstreamChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST 方法", http.StatusMethodNotAllowed)
		return
	}

	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "读取请求体失败", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var requestPayload map[string]any
	if err := json.Unmarshal(requestBody, &requestPayload); err != nil {
		http.Error(w, "请求体 JSON 解析失败", http.StatusBadRequest)
		return
	}

	// 从请求中提取用户的脱敏后消息，用于构建包含占位符的回复
	userContent := extractUserContentFromRequest(requestPayload)

	// 构造上游 LLM 的固定回复：提及所有可能出现的占位符
	// 如果脱敏成功，用户消息中的敏感信息已被替换为占位符
	// 上游 LLM 回复中也会使用这些占位符，Masker 需要在反脱敏阶段还原它们
	upstreamReply := buildUpstreamReply(userContent)

	openAIResponse := map[string]any{
		"id":      "mock-upstream-chatcmpl-001",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "mock-upstream-llm",
		"choices": []map[string]any{
			{
				"index":   0,
				"message": map[string]any{
					"role":    "assistant",
					"content": upstreamReply,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     50,
			"completion_tokens": 100,
			"total_tokens":      150,
		},
	}

	responseJSON, err := json.Marshal(openAIResponse)
	if err != nil {
		http.Error(w, "序列化响应失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseJSON)
}

// buildUpstreamReply 根据用户脱敏后的消息内容，构建上游 LLM 的回复。
// 回复中包含用户消息里出现的占位符，用于验证 Masker 反脱敏流程。
func buildUpstreamReply(userContent string) string {
	// 检测用户消息中包含哪些占位符，在回复中引用它们
	reply := "我已收到您的信息。"

	if strings.Contains(userContent, "${PHONE_1}") {
		reply += " 您提供的手机号 ${PHONE_1} 已记录。"
	}
	if strings.Contains(userContent, "${EMAIL_1}") {
		reply += " 您的邮箱 ${EMAIL_1} 已验证。"
	}
	if strings.Contains(userContent, "${ID_CARD_1}") {
		reply += " 您的身份证号 ${ID_CARD_1} 已核实。"
	}

	// 如果用户消息中没有占位符（可能不含敏感信息），返回简单回复
	if !strings.Contains(reply, "${") {
		reply += " 没有发现需要处理的敏感信息。"
	}

	return reply
}

// ==================== 辅助函数 ====================

// waitForServerReady 等待 HTTP 服务就绪，最多重试 10 次，每次间隔 100ms。
func waitForServerReady(baseURL string) {
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		resp, err := http.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("⚠️ 等待服务 %s 就绪超时\n", baseURL)
}

// createMaskerInstance 创建 Masker 实例，配置 Mock TrustedLLM 地址和内存存储。
// sessionStoreType 使用 "memory"，不依赖 Redis。
func createMaskerInstance() (*masker.Masker, error) {
	return masker.Builder().
		WithTimeout(testTimeout).
		WithSessionStoreType("memory").
		WithTrustedLLMBaseURL(mockLLMBaseURL).
		WithTrustedLLMAPIKey(mockLLMAPIKey).
		WithTrustedLLMModelName(mockLLMModelName).
		WithTrustedLLMTemperature(mockLLMTemperature).
		Build()
}

// buildTestChatRequest 构造一个 OpenAI 格式的聊天请求 HTTP Request 对象。
// requestURL: 请求发送的目标地址（上游 LLM 地址）
// userMessages: 用户消息列表，每条消息包含 role 和 content
func buildTestChatRequest(requestURL string, userMessages []map[string]string) (*http.Request, error) {
	// 构造 OpenAI 格式的请求体
	messages := make([]map[string]any, 0)
	for _, msg := range userMessages {
		messages = append(messages, map[string]any{
			"role":    msg["role"],
			"content": msg["content"],
		})
	}

	requestBody := map[string]any{
		"model":    "mock-upstream-llm",
		"messages": messages,
		"stream":   false,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer upstream-test-key")

	return req, nil
}

// parseChatResponse 解析 OpenAI 格式的聊天响应，提取 assistant 回复内容。
func parseChatResponse(resp *http.Response) (string, error) {
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

// testWithoutSessionID 测试不使用 sessionID 的单次请求脱敏与反脱敏流程。
// 验证要点：
// 1. Masker 能正确调用 TrustedLLM 检测敏感信息
// 2. 请求中的敏感值被正确替换为占位符（脱敏）
// 3. 上游 LLM 返回的占位符被正确还原为原始值（反脱敏）
// 4. 最终返回给调用者的响应中不包含占位符，而是包含原始敏感值
func testWithoutSessionID(m *masker.Masker) {
	fmt.Println("\n========== 测试场景1：不使用 sessionID（单次无状态请求）==========")

	// 构造包含敏感信息的用户消息
	userMessages := []map[string]string{
		{"role": "user", "content": "我的手机号是13812345678，邮箱是test@example.com，身份证号是320102199001011234"},
	}

	// 构造发送到上游 LLM 的请求（URL 包含 /v1/chat/completions 以匹配 OpenAI 协议判断）
	req, err := buildTestChatRequest(mockUpstreamBaseURL + "/v1/chat/completions", userMessages)
	if err != nil {
		fmt.Printf("❌ 构造请求失败: %v\n", err)
		return
	}

	// 调用 Masker.Process，不传入 sessionID
	resp, err := m.Process(req)
	if err != nil {
		fmt.Printf("❌ Masker.Process 执行失败: %v\n", err)
		return
	}

	// 解析最终响应内容
	finalContent, err := parseChatResponse(resp)
	if err != nil {
		fmt.Printf("❌ 解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("📋 最终响应内容: %s\n", finalContent)

	// 验证反脱敏结果：最终响应中应包含原始敏感值，不应包含占位符
	verifyDesensitizationResult(finalContent, "无sessionID")
}

// testWithSessionID 测试使用 sessionID 的多轮对话脱敏与反脱敏流程。
// 验证要点：
// 1. 第一轮请求：TrustedLLM 检测所有敏感信息，映射保存到 session 存储中
// 2. 第二轮请求：从 session 存储中加载已有映射，只检测新增敏感信息（增量模式）
// 3. 两轮请求的响应中，所有敏感值均被正确还原
func testWithSessionID(m *masker.Masker) {
	fmt.Println("\n========== 测试场景2：使用 sessionID（多轮有状态对话）==========")

	sessionID := "test-session-001"

	// ---- 第一轮对话：包含手机号和邮箱 ----
	fmt.Println("\n--- 第一轮对话 ---")
	firstRoundMessages := []map[string]string{
		{"role": "user", "content": "我的手机号是13812345678，邮箱是test@example.com"},
	}

	req, err := buildTestChatRequest(mockUpstreamBaseURL + "/v1/chat/completions", firstRoundMessages)
	if err != nil {
		fmt.Printf("❌ 第一轮构造请求失败: %v\n", err)
		return
	}

	resp, err := m.Process(req, sessionID)
	if err != nil {
		fmt.Printf("❌ 第一轮 Masker.Process 执行失败: %v\n", err)
		return
	}

	firstRoundContent, err := parseChatResponse(resp)
	if err != nil {
		fmt.Printf("❌ 第一轮解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("📋 第一轮响应内容: %s\n", firstRoundContent)
	verifyDesensitizationResult(firstRoundContent, "有sessionID-第一轮")

	// ---- 第二轮对话：包含身份证号（新增敏感信息），同时引用之前的手机号 ----
	fmt.Println("\n--- 第二轮对话 ---")
	secondRoundMessages := []map[string]string{
		{"role": "user", "content": "我的手机号是13812345678"},     // 已在第一轮映射中
		{"role": "assistant", "content": "已记录您的手机号"},        // 上一轮的回复（模拟）
		{"role": "user", "content": "我的身份证号是320102199001011234"}, // 新增敏感信息
	}

	req2, err := buildTestChatRequest(mockUpstreamBaseURL + "/v1/chat/completions", secondRoundMessages)
	if err != nil {
		fmt.Printf("❌ 第二轮构造请求失败: %v\n", err)
		return
	}

	resp2, err := m.Process(req2, sessionID)
	if err != nil {
		fmt.Printf("❌ 第二轮 Masker.Process 执行失败: %v\n", err)
		return
	}

	secondRoundContent, err := parseChatResponse(resp2)
	if err != nil {
		fmt.Printf("❌ 第二轮解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("📋 第二轮响应内容: %s\n", secondRoundContent)

	// 验证第二轮的反脱敏结果：应同时包含第一轮和第二轮的所有原始敏感值
	verifyDesensitizationResult(secondRoundContent, "有sessionID-第二轮")

	// 验证增量模式是否生效：第二轮应能检测到新的身份证号
	if strings.Contains(secondRoundContent, "320102199001011234") {
		fmt.Println("✅ 增量模式验证通过：第二轮新增的身份证号被正确检测并还原")
	} else {
		fmt.Println("❌ 增量模式验证失败：第二轮新增的身份证号未被还原")
	}
}

// testWithNoSensitiveInfo 测试不包含敏感信息的请求。
// 验证要点：当用户文本中无敏感信息时，TrustedLLM 返回空映射，
// Masker 不做任何替换，原始文本完整保留。
func testWithNoSensitiveInfo(m *masker.Masker) {
	fmt.Println("\n========== 测试场景3：不包含敏感信息的请求 ==========")

	userMessages := []map[string]string{
		{"role": "user", "content": "今天天气怎么样？我想去公园散步。"},
	}

	req, err := buildTestChatRequest(mockUpstreamBaseURL + "/v1/chat/completions", userMessages)
	if err != nil {
		fmt.Printf("❌ 构造请求失败: %v\n", err)
		return
	}

	resp, err := m.Process(req)
	if err != nil {
		fmt.Printf("❌ Masker.Process 执行失败: %v\n", err)
		return
	}

	finalContent, err := parseChatResponse(resp)
	if err != nil {
		fmt.Printf("❌ 解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("📋 最终响应内容: %s\n", finalContent)

	// 验证：不含敏感信息的请求，响应中不应出现任何占位符
	if !strings.Contains(finalContent, "${") {
		fmt.Println("✅ 无敏感信息验证通过：响应中无占位符残留")
	} else {
		fmt.Printf("❌ 无敏感信息验证失败：响应中出现了不应存在的占位符: %s\n", finalContent)
	}
}

// ==================== 结果校验 ====================

// verifyDesensitizationResult 校验反脱敏结果是否符合预期。
// 最终响应中应包含所有原始敏感值，且不应包含任何未还原的占位符。
func verifyDesensitizationResult(finalContent string, scenarioName string) {
	allPassed := true

	// 校验原始敏感值是否出现在最终响应中（说明反脱敏成功）
	expectedOriginalValues := []string{
		"13812345678",          // 手机号
		"test@example.com",     // 邮箱
		"320102199001011234",   // 身份证号
	}

	for _, originalValue := range expectedOriginalValues {
		if strings.Contains(finalContent, originalValue) {
			fmt.Printf("  ✅ 原始值 %s 已正确还原\n", originalValue)
		} else {
			// 该场景可能不包含所有敏感信息，仅在应出现时报告失败
			if strings.Contains(finalContent, strings.ReplaceAll(originalValue, "13812345678", "${PHONE_1}")) ||
				strings.Contains(finalContent, "${EMAIL_1}") ||
				strings.Contains(finalContent, "${ID_CARD_1}") {
				fmt.Printf("  ❌ 原始值 %s 未被还原，占位符仍残留\n", originalValue)
				allPassed = false
			}
		}
	}

	// 校验最终响应中不应存在未还原的占位符
	unresolvedPlaceholders := []string{"${PHONE_1}", "${EMAIL_1}", "${ID_CARD_1}"}
	for _, placeholder := range unresolvedPlaceholders {
		if strings.Contains(finalContent, placeholder) {
			fmt.Printf("  ❌ 占位符 %s 未被还原为原始值\n", placeholder)
			allPassed = false
		}
	}

	if allPassed {
		fmt.Printf("✅ 【%s】脱敏与反脱敏流程验证通过\n", scenarioName)
	} else {
		fmt.Printf("❌ 【%s】脱敏与反脱敏流程验证存在异常\n", scenarioName)
	}
}

// ==================== 主函数 ====================

func main() {
	fmt.Println("========================================")
	fmt.Println("  llm-privacy-masker 功能测试（Mock 模式）")
	fmt.Println("========================================")

	// 1. 启动 Mock TrustedLLM 服务端
	mockTrustedLLMServer := runMockLLM()

	// 2. 启动 Mock 上游 LLM 服务端
	mockUpstreamLLMServer := runMockUpstreamLLM()

	// 3. 创建 Masker 实例（使用内存存储，不依赖 Redis）
	maskerInstance, err := createMaskerInstance()
	if err != nil {
		fmt.Printf("❌ 创建 Masker 实例失败: %v\n", err)
		shutdownMockServers(mockTrustedLLMServer, mockUpstreamLLMServer)
		os.Exit(1)
	}
	fmt.Println("✅ Masker 实例创建成功（内存存储模式）")

	// 4. 执行测试场景
	testWithoutSessionID(maskerInstance)
	testWithSessionID(maskerInstance)
	testWithNoSensitiveInfo(maskerInstance)

	// 5. 关闭 Mock 服务
	shutdownMockServers(mockTrustedLLMServer, mockUpstreamLLMServer)

	fmt.Println("\n========================================")
	fmt.Println("  所有测试场景执行完毕")
	fmt.Println("========================================")
}

// shutdownMockServers 优雅关闭 Mock HTTP 服务端
func shutdownMockServers(servers ...*http.Server) {
	for _, server := range servers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := server.Shutdown(ctx); err != nil {
			fmt.Printf("⚠️ 关闭服务失败: %v\n", err)
		}
		cancel()
	}
}

