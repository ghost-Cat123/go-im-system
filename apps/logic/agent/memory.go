package agent

import (
	"context"
	"encoding/json"
	"github.com/cloudwego/eino/schema"
	"go-im-system/apps/pkg/cache"
	"time"
)

// RedisStore 单例store 负责创造session
type RedisStore struct {
	TTL time.Duration
}

func NewRedisStore(ttl time.Duration) *RedisStore {
	return &RedisStore{TTL: ttl}
}

func (s *RedisStore) GetOrCreate(sessionId string) *Session {
	return &Session{
		ID:    sessionId,
		Key:   "agent_session:" + sessionId,
		Store: s,
	}
}

// Session 存储某一用户的上下文
type Session struct {
	ID    string
	Key   string
	Store *RedisStore
}

// Append Append存入
func (s *Session) Append(ctx context.Context, msg *schema.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	pipe := cache.GetCache().Pipeline()
	pipe.RPush(ctx, s.Key, data)
	// 滑动窗口截断模式 保留最新的20条对话
	pipe.LTrim(ctx, s.Key, -20, -1)
	pipe.Expire(ctx, s.Key, s.Store.TTL)
	_, err = pipe.Exec(ctx)
	return err
}

// GetMessages 读取全部对话 + 反序列化
func (s *Session) GetMessages(ctx context.Context) ([]*schema.Message, error) {
	strs, err := cache.GetCache().LRange(ctx, s.Key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	// 反序列化为schema.Message列表
	var history []*schema.Message
	for _, str := range strs {
		var msg schema.Message
		if err := json.Unmarshal([]byte(str), &msg); err == nil {
			history = append(history, &msg)
		}
	}
	return history, nil
}
