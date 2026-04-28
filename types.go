package masker

// HTTPRequest 表示调用者传入的完整 HTTP 请求。
type HTTPRequest struct {
	Method    string            // HTTP 方法，如 GET、POST
	URL       string            // 请求目标 URL
	Headers   map[string]string // HTTP 请求头
	Body      []byte            // HTTP 请求体
	SessionID string            // 会话 ID 非必须
}

// HTTPResponse 表示返回给调用者的完整 HTTP 响应。
type HTTPResponse struct {
	StatusCode int                 // HTTP 状态码
	Headers    map[string][]string // HTTP 响应头
	Body       []byte              // HTTP 响应体
}

// MappingEntry 表示可信 LLM 返回的一条脱敏映射记录。
type MappingEntry struct {
	Original    string `json:"original"`    // 原始敏感值
	Placeholder string `json:"placeholder"` // 占位符
	Type        string `json:"type"`        // 数据类型标识
}

// TrustedLLMMappingResponse 表示可信 LLM 返回的映射集合。
type TrustedLLMMappingResponse struct {
	Entries []MappingEntry `json:"entries"` // 映射条目列表
}
