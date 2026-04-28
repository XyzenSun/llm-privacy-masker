package masker

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
