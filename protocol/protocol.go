package protocol

// TextNode 表示协议 JSON 中可被提取和回写的文本节点。
type TextNode struct {
	Path string // 节点路径，如 "messages.0.content"
	Role string // 角色标识，如 "user"、"assistant"
	Text string // 文本内容
}

// ProtocolType 表示协议类型标识。
type ProtocolType string

const (
	ProtocolTypeOpenAI    ProtocolType = "openai"
	ProtocolTypeAnthropic ProtocolType = "anthropic"
	ProtocolTypeGemini    ProtocolType = "gemini"
)

// Protocol 定义 LLM 协议处理接口。
type Protocol interface {
	// ExtractRequestTextNodes 提取请求中的文本节点。
	ExtractRequestTextNodes(body []byte) ([]TextNode, error)
	// LatestUserText 返回最后一个用户消息的文本。
	LatestUserText(body []byte) (string, bool, error)
	// RewriteRequest 使用映射改写请求文本。
	RewriteRequest(body []byte, originalToPlaceholder map[string]string) ([]byte, error)
	// ExtractResponseTextNodes 提取响应中的文本节点。
	ExtractResponseTextNodes(body []byte) ([]TextNode, error)
	// RewriteResponse 使用映射改写响应文本。
	RewriteResponse(body []byte, placeholderToOriginal map[string]string) ([]byte, error)
	// ForceNonStream 强制关闭流式传输，返回处理后的 URL 和请求体。
	// Gemini 协议处理 URL，Anthropic/OpenAI 协议处理请求体中的 stream 字段。
	ForceNonStream(url string, body []byte) (string, []byte, error)
}