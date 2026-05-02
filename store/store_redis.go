package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore 使用 Redis 保存隐私映射的存储实现。
// 支持会话级别和请求级别的映射存储，并自动过期清理。
type RedisStore struct {
	client     *redis.Client   // Redis 客户端连接
	sessionTTL time.Duration   // 会话级映射的过期时间
	requestTTL time.Duration   // 请求级映射的过期时间
}

// NewRedisStore 创建一个 Redis 存储实例。
// 参数 redisURL: Redis 连接地址，格式如 redis://host:port
// 参数 sessionTTL: 会话级映射的存活时间
// 参数 requestTTL: 请求级映射的存活时间
func NewRedisStore(redisURL string, sessionTTL time.Duration, requestTTL time.Duration) (*RedisStore, error) {
	connectionOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	redisClient := redis.NewClient(connectionOptions)

	// 验证 Redis 连接是否正常
	pingContext, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelPing()

	if err := redisClient.Ping(pingContext).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &RedisStore{
		client:     redisClient,
		sessionTTL: sessionTTL,
		requestTTL: requestTTL,
	}, nil
}

// LoadSessionMappings 从 Redis 中读取会话级的映射数据。
// 返回两个映射表：原始值到占位符的映射、占位符到原始值的映射。
// 如果映射不存在，返回空映射表而不是错误。
func (s *RedisStore) LoadSessionMappings(sessionID string) (map[string]string, map[string]string, error) {
	originalToPlaceholderKey := buildSessionOriginalToPlaceholderKey(sessionID)
	placeholderToOriginalKey := buildSessionPlaceholderToOriginalKey(sessionID)
	return s.loadMappingPair(context.Background(), originalToPlaceholderKey, placeholderToOriginalKey)
}

// SaveSessionMappings 将会话级的映射数据保存到 Redis。
// 同时保存原始值到占位符的映射和占位符到原始值的反向映射。
func (s *RedisStore) SaveSessionMappings(sessionID string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string) error {
	originalToPlaceholderKey := buildSessionOriginalToPlaceholderKey(sessionID)
	placeholderToOriginalKey := buildSessionPlaceholderToOriginalKey(sessionID)
	return s.saveMappingPair(context.Background(), originalToPlaceholderKey, placeholderToOriginalKey, originalToPlaceholder, placeholderToOriginal, s.sessionTTL)
}

// LoadRequestMappings 从 Redis 中读取请求级的映射数据。
// 返回两个映射表：原始值到占位符的映射、占位符到原始值的映射。
// 如果映射不存在，返回空映射表而不是错误。
func (s *RedisStore) LoadRequestMappings(requestID string) (map[string]string, map[string]string, error) {
	originalToPlaceholderKey := buildRequestOriginalToPlaceholderKey(requestID)
	placeholderToOriginalKey := buildRequestPlaceholderToOriginalKey(requestID)
	return s.loadMappingPair(context.Background(), originalToPlaceholderKey, placeholderToOriginalKey)
}

// SaveRequestMappings 将请求级的映射数据保存到 Redis。
// 请求级映射会在指定的 TTL 后自动过期删除。
func (s *RedisStore) SaveRequestMappings(requestID string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string) error {
	originalToPlaceholderKey := buildRequestOriginalToPlaceholderKey(requestID)
	placeholderToOriginalKey := buildRequestPlaceholderToOriginalKey(requestID)
	return s.saveMappingPair(context.Background(), originalToPlaceholderKey, placeholderToOriginalKey, originalToPlaceholder, placeholderToOriginal, s.requestTTL)
}

// DeleteRequestMappings 删除指定请求的映射数据。
// 同时删除原始值到占位符和占位符到原始值两个方向的映射。
func (s *RedisStore) DeleteRequestMappings(requestID string) error {
	deleteContext := context.Background()
	originalToPlaceholderKey := buildRequestOriginalToPlaceholderKey(requestID)
	placeholderToOriginalKey := buildRequestPlaceholderToOriginalKey(requestID)
	return s.client.Del(deleteContext, originalToPlaceholderKey, placeholderToOriginalKey).Err()
}

// loadMappingPair 从 Redis 中加载一对映射表。
// 参数 originalToPlaceholderKey: 原始值到占位符映射的 Redis 键
// 参数 placeholderToOriginalKey: 占位符到原始值映射的 Redis 键
func (s *RedisStore) loadMappingPair(ctx context.Context, originalToPlaceholderKey string, placeholderToOriginalKey string) (map[string]string, map[string]string, error) {
	originalToPlaceholder, err := s.loadMapFromRedis(ctx, originalToPlaceholderKey)
	if err != nil {
		return nil, nil, err
	}

	placeholderToOriginal, err := s.loadMapFromRedis(ctx, placeholderToOriginalKey)
	if err != nil {
		return nil, nil, err
	}

	return originalToPlaceholder, placeholderToOriginal, nil
}

// saveMappingPair 将一对映射表保存到 Redis。
// 使用 Redis 事务管道确保两个映射表原子性地同时保存。
func (s *RedisStore) saveMappingPair(ctx context.Context, originalToPlaceholderKey string, placeholderToOriginalKey string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string, ttl time.Duration) error {
	// 序列化原始值到占位符的映射
	originalToPlaceholderBytes, err := json.Marshal(cloneMap(originalToPlaceholder))
	if err != nil {
		return fmt.Errorf("marshal originalToPlaceholder: %w", err)
	}

	// 序列化占位符到原始值的映射
	placeholderToOriginalBytes, err := json.Marshal(cloneMap(placeholderToOriginal))
	if err != nil {
		return fmt.Errorf("marshal placeholderToOriginal: %w", err)
	}

	// 使用事务管道确保原子性写入
	transactionPipeline := s.client.TxPipeline()
	transactionPipeline.Set(ctx, originalToPlaceholderKey, originalToPlaceholderBytes, ttl)
	transactionPipeline.Set(ctx, placeholderToOriginalKey, placeholderToOriginalBytes, ttl)

	if _, err := transactionPipeline.Exec(ctx); err != nil {
		return fmt.Errorf("save redis mappings: %w", err)
	}

	return nil
}

// loadMapFromRedis 从 Redis 中加载单个映射表。
// 如果键不存在，返回空映射表而不是错误。
func (s *RedisStore) loadMapFromRedis(ctx context.Context, key string) (map[string]string, error) {
	storedValue, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		// 键不存在时返回空映射
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load redis key %s: %w", key, err)
	}

	// 反序列化 JSON 数据为映射表
	loadedMap := map[string]string{}
	if err := json.Unmarshal([]byte(storedValue), &loadedMap); err != nil {
		return nil, fmt.Errorf("unmarshal redis key %s: %w", key, err)
	}

	return loadedMap, nil
}

// buildSessionOriginalToPlaceholderKey 构建会话级"原始值到占位符"映射的 Redis 键。
// 键格式: privacyguard:session:{sessionID}:o2p
func buildSessionOriginalToPlaceholderKey(sessionID string) string {
	return fmt.Sprintf("privacyguard:session:%s:o2p", sessionID)
}

// buildSessionPlaceholderToOriginalKey 构建会话级"占位符到原始值"映射的 Redis 键。
// 键格式: privacyguard:session:{sessionID}:p2o
func buildSessionPlaceholderToOriginalKey(sessionID string) string {
	return fmt.Sprintf("privacyguard:session:%s:p2o", sessionID)
}

// buildRequestOriginalToPlaceholderKey 构建请求级"原始值到占位符"映射的 Redis 键。
// 键格式: privacyguard:req:{requestID}:o2p
func buildRequestOriginalToPlaceholderKey(requestID string) string {
	return fmt.Sprintf("privacyguard:req:%s:o2p", requestID)
}

// buildRequestPlaceholderToOriginalKey 构建请求级"占位符到原始值"映射的 Redis 键。
// 键格式: privacyguard:req:{requestID}:p2o
func buildRequestPlaceholderToOriginalKey(requestID string) string {
	return fmt.Sprintf("privacyguard:req:%s:p2o", requestID)
}