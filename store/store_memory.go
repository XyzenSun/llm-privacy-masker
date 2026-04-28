package store

import "sync"

type mappingPair struct {
	originalToPlaceholder map[string]string
	placeholderToOriginal map[string]string
}

// MemoryStore 使用内存保存映射。
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]mappingPair
	requests map[string]mappingPair
}

// NewMemoryStore 创建内存 store。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]mappingPair),
		requests: make(map[string]mappingPair),
	}
}

// LoadSessionMappings 读取 session 级映射。
func (s *MemoryStore) LoadSessionMappings(sessionID string) (map[string]string, map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pair, ok := s.sessions[sessionID]
	if !ok {
		return map[string]string{}, map[string]string{}, nil
	}

	return cloneMap(pair.originalToPlaceholder), cloneMap(pair.placeholderToOriginal), nil
}

// SaveSessionMappings 保存 session 级映射。
func (s *MemoryStore) SaveSessionMappings(sessionID string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[sessionID] = mappingPair{
		originalToPlaceholder: cloneMap(originalToPlaceholder),
		placeholderToOriginal: cloneMap(placeholderToOriginal),
	}

	return nil
}

// LoadRequestMappings 读取 request 级映射。
func (s *MemoryStore) LoadRequestMappings(requestID string) (map[string]string, map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pair, ok := s.requests[requestID]
	if !ok {
		return map[string]string{}, map[string]string{}, nil
	}

	return cloneMap(pair.originalToPlaceholder), cloneMap(pair.placeholderToOriginal), nil
}

// SaveRequestMappings 保存 request 级映射。
func (s *MemoryStore) SaveRequestMappings(requestID string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests[requestID] = mappingPair{
		originalToPlaceholder: cloneMap(originalToPlaceholder),
		placeholderToOriginal: cloneMap(placeholderToOriginal),
	}

	return nil
}

// DeleteRequestMappings 删除 request 级映射。
func (s *MemoryStore) DeleteRequestMappings(requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.requests, requestID)
	return nil
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}

	output := make(map[string]string, len(input))
	for k, v := range input {
		output[k] = v
	}

	return output
}
