package store

// Store 定义有状态与请求级临时映射存储能力。
type Store interface {
	//加载会话级语义变量与原始值的映射表
	LoadSessionMappings(sessionID string) (map[string]string, map[string]string, error)
	//保存会话级语义变量与原始值的映射表
	SaveSessionMappings(sessionID string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string) error
	//保存请求级语义变量与原始值的映射表，内存存储与Reids存储都将在请求结束后清除
	LoadRequestMappings(requestID string) (map[string]string, map[string]string, error)
	//临时请求级语义变量与原始值的映射表，内存存储与Reids存储都将在请求结束后清除
	SaveRequestMappings(requestID string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string) error
	//删除临时请求级语义变量与原始值的映射表
	DeleteRequestMappings(requestID string) error
}
