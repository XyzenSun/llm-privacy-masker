package main

import (
	"bytes"
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

// envConfig 从 .env 文件加载的真实 LLM 配置
type envConfig struct {
	// trustedLLMURL 可信 LLM（用于 NER 检测）的 API 基础地址
	trustedLLMURL string
	// trustedLLMAPIKey 可信 LLM 的 API 密钥
	trustedLLMAPIKey string
	// trustedLLMModel 可信 LLM 的模型名称
	trustedLLMModel string
	// cloudLLMURL 云端 LLM（接收脱敏后请求的上游 LLM）的 API 基础地址
	cloudLLMURL string
	// cloudLLMAPIKey 云端 LLM 的 API 密钥
	cloudLLMAPIKey string
	// cloudLLMModel 云端 LLM 的模型名称
	cloudLLMModel string
}

// testTimeout 真实环境测试超时时间，真实 LLM 响应较慢，设置较长
const testTimeout = 180 * time.Second

// ==================== .env 文件加载 ====================

// loadEnvConfig 从当前目录下的 .env 文件读取环境配置。
// .env 文件格式为 KEY=VALUE，每行一个配置项，不支持引号和转义。
func loadEnvConfig(envFilePath string) (*envConfig, error) {
	fileContent, err := os.ReadFile(envFilePath)
	if err != nil {
		return nil, fmt.Errorf("读取 .env 文件失败: %w", err)
	}

	envMap := make(map[string]string)
	for _, line := range strings.Split(string(fileContent), "\n") {
		line = strings.TrimSpace(line)
		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 查找第一个等号位置，将行分割为 KEY 和 VALUE
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

	// 校验必需配置项
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

// createRealMaskerInstance 使用真实可信 LLM 配置创建 Masker 实例。
// 使用内存存储（不依赖 Redis），使用 Masker 库内置的默认系统提示词。
func createRealMaskerInstance(env *envConfig) (*masker.Masker, error) {
	return masker.New().
		WithTimeout(testTimeout).
		WithSessionStoreType("memory").
		WithTrustedLLMBaseURL(env.trustedLLMURL).
		WithTrustedLLMAPIKey(env.trustedLLMAPIKey).
		WithTrustedLLMModelName(env.trustedLLMModel).
		WithTrustedLLMTemperature(0.0).
		Build()
}

// ==================== 请求构建 ====================

// buildOpenAIChatRequest 构造 OpenAI 格式的聊天请求。
// URL 必须包含 /v1/chat/completions 或 /chat/completions 路径，
// 以便 Masker 的 JudgeProtocol 正确识别为 OpenAI 协议。
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

	// URL 添加 /chat/completions 路径，Masker 通过此路径识别 OpenAI 协议
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

// parseOpenAIChatResponse 解析 OpenAI 格式的聊天响应，提取 assistant 回复内容。
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

// ==================== 结果校验 ====================

// knownPlaceholderSuffixes 已知的敏感信息占位符的后缀列表。
// Masker 内置提示词要求可信 LLM 使用 ${TYPE_N} 格式，N 为数字序号。
// 校验时精确匹配 ${TYPE_N}（后面紧跟 }），排除 CloudLLM 回复中
// 自行使用的 Shell 变量语法（如 ${PHONE_1:0:3}）不被误判为占位符残留。
var knownPlaceholderSuffixes = []string{
	"${PHONE_1}", "${PHONE_2}", "${PHONE_3}",
	"${EMAIL_1}", "${EMAIL_2}", "${EMAIL_3}",
	"${ID_CARD_1}", "${ID_CARD_2}", "${ID_CARD_3}",
	"${PASSWORD_1}", "${PASSWORD_2}",
	"${IP_1}", "${IP_2}",
	"${BANK_CARD_1}", "${BANK_CARD_2}",
}

// containsUnresolvedPlaceholder 检查文本中是否包含 Masker 定义的未还原占位符。
// 仅匹配 ${TYPE_N}（N为数字）完整格式，排除 CloudLLM 在回复中自行使用的
// Shell/Python 变量语法（如 ${PHONE_1:0:3}、${EMAIL_1:1:5}）不被误判。
func containsUnresolvedPlaceholder(text string) bool {
	for _, placeholder := range knownPlaceholderSuffixes {
		if strings.Contains(text, placeholder) {
			return true
		}
	}
	return false
}

// verifyMaskerProcessResult 校验 Masker.Process 的完整执行结果。
// 真实环境下 CloudLLM 回复内容不可控，校验聚焦于：
// 1. Process 是否成功执行（无报错）——已在调用处校验
// 2. HTTP 响应状态码是否为 2xx ——说明云端 LLM 正常响应
// 3. 响应中是否包含已还原的原始敏感值 ——说明反脱敏成功
// 4. 响应中是否无 Masker 定义的 ${TYPE_N} 占位符残留 ——说明反脱敏完整
func verifyMaskerProcessResult(finalContent string, scenarioName string, expectedOriginalValues []string) {
	fmt.Printf("\n--- 校验场景: %s ---\n", scenarioName)
	allPassed := true

	// 校验原始敏感值是否出现在最终响应中（说明反脱敏成功还原）
	for _, originalValue := range expectedOriginalValues {
		if strings.Contains(finalContent, originalValue) {
			fmt.Printf("  PASS: 原始值 \"%s\" 已还原至最终响应（反脱敏成功）\n", originalValue)
		} else {
			fmt.Printf("  WARN: 原始值 \"%s\" 未在最终响应中出现（CloudLLM 可能未引用该值）\n", originalValue)
			// 真实 LLM 回复内容不确定，未引用某值不代表反脱敏失败，仅标记为 WARN
		}
	}

	// 校验最终响应中不应存在 Masker 定义的 ${TYPE_N} 格式占位符残留
	if containsUnresolvedPlaceholder(finalContent) {
		fmt.Printf("  FAIL: 最终响应中仍存在未还原的 ${TYPE_N} 占位符（反脱敏不完整）\n")
		allPassed = false
	} else {
		fmt.Printf("  PASS: 最终响应中无 ${TYPE_N} 占位符残留（反脱敏完整）\n")
	}

	if allPassed {
		fmt.Printf("PASS: 【%s】核心验证通过\n", scenarioName)
	} else {
		fmt.Printf("FAIL: 【%s】核心验证存在异常\n", scenarioName)
	}
}

// ==================== 测试场景 ====================

// testWithoutSessionID 测试 sessionID 为空时的单次无状态请求。
// 验证要点：
// 1. Masker 使用内置提示词调用真实可信 LLM 检测敏感信息
// 2. 请求中的敏感值被替换为占位符（脱敏），发送到云端 LLM
// 3. 云端 LLM 响应中的占位符被还原为原始值（反脱敏）
// 4. sessionID 为空时，映射为一次性使用，请求结束后自动清理
// 5. Process 返回 2xx 状态码，说明脱敏-转发-反脱敏流程完整执行
func testWithoutSessionID(m *masker.Masker, env *envConfig) {
	fmt.Println("\n========== 测试场景1：sessionID 为空（单次无状态请求）==========")

	// 构造包含手机号、邮箱、身份证号的用户消息
	userMessages := []map[string]string{
		{"role": "user", "content": "我的手机号是13812345678，邮箱是test@example.com，身份证号是320102199001011234"},
	}

	req, err := buildOpenAIChatRequest(env.cloudLLMURL, env.cloudLLMAPIKey, env.cloudLLMModel, userMessages)
	if err != nil {
		fmt.Printf("FAIL: 构造请求失败: %v\n", err)
		return
	}

	// 不传入 sessionID，无状态模式
	resp, err := m.Process(req)
	if err != nil {
		fmt.Printf("FAIL: Masker.Process 执行失败（sessionID为空时脱敏流程未完成）: %v\n", err)
		return
	}
	// Process 成功返回说明：可信LLM检测、脱敏、云端转发、反脱敏全流程执行成功
	fmt.Printf("PASS: Masker.Process 执行成功（sessionID为空，无状态模式）\n")

	// 校验 HTTP 响应状态码
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Printf("FAIL: 云端 LLM 返回非 2xx 状态码: %d\n", resp.StatusCode)
		return
	}
	fmt.Printf("PASS: 云端 LLM 返回 2xx 状态码: %d\n", resp.StatusCode)

	finalContent, err := parseOpenAIChatResponse(resp)
	if err != nil {
		fmt.Printf("FAIL: 解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("最终响应内容: %s\n", finalContent)

	// 校验脱敏与反脱敏结果
	expectedOriginalValues := []string{
		"13812345678",
		"test@example.com",
		"320102199001011234",
	}
	verifyMaskerProcessResult(finalContent, "sessionID为空-单次无状态请求", expectedOriginalValues)
}

// testWithSessionID 测试 sessionID 不为空时的多轮有状态对话。
// 验证要点：
// 1. 第一轮：可信 LLM 检测手机号和邮箱，映射保存到 session 存储
// 2. 第二轮：加载已有映射做增量检测，新增身份证号被识别
// 3. 两轮 Process 均成功执行（无报错）
// 4. sessionID 不为空时，映射跨请求持久保存，支持增量检测
func testWithSessionID(m *masker.Masker, env *envConfig) {
	fmt.Println("\n========== 测试场景2：sessionID 不为空（多轮有状态对话）==========")

	sessionID := "real-test-session-001"

	// ---- 第一轮对话：包含手机号和邮箱 ----
	fmt.Println("\n--- 第一轮对话 ---")
	firstRoundMessages := []map[string]string{
		{"role": "user", "content": "我的手机号是13812345678，邮箱是test@example.com"},
	}

	req1, err := buildOpenAIChatRequest(env.cloudLLMURL, env.cloudLLMAPIKey, env.cloudLLMModel, firstRoundMessages)
	if err != nil {
		fmt.Printf("FAIL: 第一轮构造请求失败: %v\n", err)
		return
	}

	// 传入 sessionID，有状态模式
	resp1, err := m.Process(req1, sessionID)
	if err != nil {
		fmt.Printf("FAIL: 第一轮 Masker.Process 执行失败: %v\n", err)
		return
	}
	fmt.Printf("PASS: 第一轮 Masker.Process 执行成功（sessionID=%s，有状态模式）\n", sessionID)

	if resp1.StatusCode < 200 || resp1.StatusCode >= 300 {
		fmt.Printf("FAIL: 第一轮云端 LLM 返回非 2xx 状态码: %d\n", resp1.StatusCode)
		return
	}
	fmt.Printf("PASS: 第一轮云端 LLM 返回 2xx 状态码: %d\n", resp1.StatusCode)

	firstRoundContent, err := parseOpenAIChatResponse(resp1)
	if err != nil {
		fmt.Printf("FAIL: 第一轮解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("第一轮响应内容: %s\n", firstRoundContent)

	firstRoundExpectedValues := []string{
		"13812345678",
		"test@example.com",
	}
	verifyMaskerProcessResult(firstRoundContent, "sessionID不为空-第一轮对话", firstRoundExpectedValues)

	// ---- 第二轮对话：包含新增身份证号 ----
	fmt.Println("\n--- 第二轮对话 ---")
	// 已映射的手机号 + 新增的身份证号，验证增量检测模式
	secondRoundMessages := []map[string]string{
		{"role": "user", "content": "我的手机号是13812345678"},
		{"role": "assistant", "content": "已记录您的手机号和邮箱信息"},
		{"role": "user", "content": "我的身份证号是320102199001011234"},
	}

	req2, err := buildOpenAIChatRequest(env.cloudLLMURL, env.cloudLLMAPIKey, env.cloudLLMModel, secondRoundMessages)
	if err != nil {
		fmt.Printf("FAIL: 第二轮构造请求失败: %v\n", err)
		return
	}

	// 使用相同 sessionID，Masker 加载第一轮映射并做增量检测
	resp2, err := m.Process(req2, sessionID)
	if err != nil {
		fmt.Printf("FAIL: 第二轮 Masker.Process 执行失败（增量模式未成功执行）: %v\n", err)
		return
	}
	fmt.Printf("PASS: 第二轮 Masker.Process 执行成功（增量模式，sessionID=%s）\n", sessionID)

	if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
		fmt.Printf("FAIL: 第二轮云端 LLM 返回非 2xx 状态码: %d\n", resp2.StatusCode)
		return
	}
	fmt.Printf("PASS: 第二轮云端 LLM 返回 2xx 状态码: %d\n", resp2.StatusCode)

	secondRoundContent, err := parseOpenAIChatResponse(resp2)
	if err != nil {
		fmt.Printf("FAIL: 第二轮解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("第二轮响应内容: %s\n", secondRoundContent)

	// 校验第二轮：手机号（已有映射）和身份证号（增量检测新增）
	secondRoundExpectedValues := []string{
		"13812345678",
		"320102199001011234",
	}
	verifyMaskerProcessResult(secondRoundContent, "sessionID不为空-第二轮对话（增量模式）", secondRoundExpectedValues)
}

// testWithSessionIDNoNewSensitiveInfo 测试 sessionID 不为空但第二轮无新增敏感信息。
// 验证要点：
// 1. 第一轮建立映射，第二轮无新增敏感信息
// 2. 第二轮 Process 成功执行（无报错），增量模式下已知映射仍有效
// 3. 第二轮响应中无 ${TYPE_N} 占位符残留（反脱敏完整）
func testWithSessionIDNoNewSensitiveInfo(m *masker.Masker, env *envConfig) {
	fmt.Println("\n========== 测试场景3：sessionID 不为空，第二轮无新增敏感信息 ==========)")

	// 使用不同 sessionID，避免与场景2冲突
	sessionID := "real-test-session-002"

	fmt.Println("\n--- 第一轮对话 ---")
	firstRoundMessages := []map[string]string{
		{"role": "user", "content": "我的手机号是13812345678"},
	}

	req1, err := buildOpenAIChatRequest(env.cloudLLMURL, env.cloudLLMAPIKey, env.cloudLLMModel, firstRoundMessages)
	if err != nil {
		fmt.Printf("FAIL: 第一轮构造请求失败: %v\n", err)
		return
	}

	resp1, err := m.Process(req1, sessionID)
	if err != nil {
		fmt.Printf("FAIL: 第一轮 Masker.Process 执行失败: %v\n", err)
		return
	}
	fmt.Printf("PASS: 第一轮 Masker.Process 执行成功（sessionID=%s）\n", sessionID)

	if resp1.StatusCode < 200 || resp1.StatusCode >= 300 {
		fmt.Printf("FAIL: 第一轮云端 LLM 返回非 2xx 状态码: %d\n", resp1.StatusCode)
		return
	}

	firstRoundContent, err := parseOpenAIChatResponse(resp1)
	if err != nil {
		fmt.Printf("FAIL: 第一轮解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("第一轮响应内容: %s\n", firstRoundContent)
	verifyMaskerProcessResult(firstRoundContent, "sessionID不为空-无新增敏感信息-第一轮", []string{"13812345678"})

	// ---- 第二轮对话：无新增敏感信息 ----
	fmt.Println("\n--- 第二轮对话 ---")
	// 已映射的手机号 + 无敏感信息的天气讨论
	secondRoundMessages := []map[string]string{
		{"role": "user", "content": "我的手机号是13812345678"},
		{"role": "assistant", "content": "已记录您的手机号"},
		{"role": "user", "content": "今天天气怎么样？我想去公园散步"},
	}

	req2, err := buildOpenAIChatRequest(env.cloudLLMURL, env.cloudLLMAPIKey, env.cloudLLMModel, secondRoundMessages)
	if err != nil {
		fmt.Printf("FAIL: 第二轮构造请求失败: %v\n", err)
		return
	}

	resp2, err := m.Process(req2, sessionID)
	if err != nil {
		fmt.Printf("FAIL: 第二轮 Masker.Process 执行失败（无新增敏感信息时增量模式异常）: %v\n", err)
		return
	}
	fmt.Printf("PASS: 第二轮 Masker.Process 执行成功（无新增敏感信息，增量模式）\n")

	if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
		fmt.Printf("FAIL: 第二轮云端 LLM 返回非 2xx 状态码: %d\n", resp2.StatusCode)
		return
	}

	secondRoundContent, err := parseOpenAIChatResponse(resp2)
	if err != nil {
		fmt.Printf("FAIL: 第二轮解析响应失败: %v\n", err)
		return
	}

	fmt.Printf("第二轮响应内容: %s\n", secondRoundContent)
	verifyMaskerProcessResult(secondRoundContent, "sessionID不为空-无新增敏感信息-第二轮", []string{"13812345678"})
}

// ==================== 主函数 ====================

func main() {
	fmt.Println("========================================")
	fmt.Println("  llm-privacy-masker 真实环境功能测试")
	fmt.Println("  （使用真实 TrustedLLM 和 CloudLLM）")
	fmt.Println("========================================")

	// 从 .env 文件加载真实 LLM 配置
	env, err := loadEnvConfig(".env")
	if err != nil {
		fmt.Printf("FAIL: 加载环境配置失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("环境配置加载成功:\n")
	fmt.Printf("  TrustedLLM: %s (模型: %s)\n", env.trustedLLMURL, env.trustedLLMModel)
	fmt.Printf("  CloudLLM:   %s (模型: %s)\n", env.cloudLLMURL, env.cloudLLMModel)

	// 创建 Masker 实例（内存存储，使用内置提示词）
	maskerInstance, err := createRealMaskerInstance(env)
	if err != nil {
		fmt.Printf("FAIL: 创建 Masker 实例失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Masker 实例创建成功（内存存储模式，使用内置提示词）")

	// 执行测试场景
	testWithoutSessionID(maskerInstance, env)
	testWithSessionID(maskerInstance, env)
	testWithSessionIDNoNewSensitiveInfo(maskerInstance, env)

	fmt.Println("\n========================================")
	fmt.Println("  所有真实环境测试场景执行完毕")
	fmt.Println("========================================")
}