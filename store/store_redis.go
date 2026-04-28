package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore 使用 Redis 保存映射。
type RedisStore struct {
	client     *redis.Client
	sessionTTL time.Duration
	requestTTL time.Duration
}

// NewRedisStore 创建 Redis store。
func NewRedisStore(redisURL string, sessionTTL time.Duration, requestTTL time.Duration) (*RedisStore, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &RedisStore{client: client, sessionTTL: sessionTTL, requestTTL: requestTTL}, nil
}

// LoadSessionMappings 读取 session 级映射。
func (s *RedisStore) LoadSessionMappings(sessionID string) (map[string]string, map[string]string, error) {
	return s.loadPair(context.Background(), sessionOriginalToPlaceholderKey(sessionID), sessionPlaceholderToOriginalKey(sessionID))
}

// SaveSessionMappings 保存 session 级映射。
func (s *RedisStore) SaveSessionMappings(sessionID string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string) error {
	return s.savePair(context.Background(), sessionOriginalToPlaceholderKey(sessionID), sessionPlaceholderToOriginalKey(sessionID), originalToPlaceholder, placeholderToOriginal, s.sessionTTL)
}

// LoadRequestMappings 读取 request 级映射。
func (s *RedisStore) LoadRequestMappings(requestID string) (map[string]string, map[string]string, error) {
	return s.loadPair(context.Background(), requestOriginalToPlaceholderKey(requestID), requestPlaceholderToOriginalKey(requestID))
}

// SaveRequestMappings 保存 request 级映射。
func (s *RedisStore) SaveRequestMappings(requestID string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string) error {
	return s.savePair(context.Background(), requestOriginalToPlaceholderKey(requestID), requestPlaceholderToOriginalKey(requestID), originalToPlaceholder, placeholderToOriginal, s.requestTTL)
}

// DeleteRequestMappings 删除 request 级映射。
func (s *RedisStore) DeleteRequestMappings(requestID string) error {
	ctx := context.Background()
	return s.client.Del(ctx, requestOriginalToPlaceholderKey(requestID), requestPlaceholderToOriginalKey(requestID)).Err()
}

func (s *RedisStore) loadPair(ctx context.Context, o2pKey string, p2oKey string) (map[string]string, map[string]string, error) {
	o2p, err := s.loadMap(ctx, o2pKey)
	if err != nil {
		return nil, nil, err
	}

	p2o, err := s.loadMap(ctx, p2oKey)
	if err != nil {
		return nil, nil, err
	}

	return o2p, p2o, nil
}

func (s *RedisStore) savePair(ctx context.Context, o2pKey string, p2oKey string, originalToPlaceholder map[string]string, placeholderToOriginal map[string]string, ttl time.Duration) error {
	o2pBytes, err := json.Marshal(cloneMap(originalToPlaceholder))
	if err != nil {
		return fmt.Errorf("marshal originalToPlaceholder: %w", err)
	}

	p2oBytes, err := json.Marshal(cloneMap(placeholderToOriginal))
	if err != nil {
		return fmt.Errorf("marshal placeholderToOriginal: %w", err)
	}

	pipe := s.client.TxPipeline()
	pipe.Set(ctx, o2pKey, o2pBytes, ttl)
	pipe.Set(ctx, p2oKey, p2oBytes, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("save redis mappings: %w", err)
	}

	return nil
}

func (s *RedisStore) loadMap(ctx context.Context, key string) (map[string]string, error) {
	value, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load redis key %s: %w", key, err)
	}

	output := map[string]string{}
	if err := json.Unmarshal([]byte(value), &output); err != nil {
		return nil, fmt.Errorf("unmarshal redis key %s: %w", key, err)
	}

	return output, nil
}

func sessionOriginalToPlaceholderKey(sessionID string) string {
	return fmt.Sprintf("privacyguard:session:%s:o2p", sessionID)
}

func sessionPlaceholderToOriginalKey(sessionID string) string {
	return fmt.Sprintf("privacyguard:session:%s:p2o", sessionID)
}

func requestOriginalToPlaceholderKey(requestID string) string {
	return fmt.Sprintf("privacyguard:req:%s:o2p", requestID)
}

func requestPlaceholderToOriginalKey(requestID string) string {
	return fmt.Sprintf("privacyguard:req:%s:p2o", requestID)
}
