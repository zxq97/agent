package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"

	"github.com/zxq97/agent/internal/config"
)

// RedisStore 把会话 history 序列化为 JSON 存到 Redis。
//
// Key 格式: <prefix>:<userID>:<sessionID>
//   - userID 前缀做天然隔离,即使 sessionID 重复也不会串号
//   - prefix 来自配置 (默认 "agent:session"),便于 Redis 上做扫描/清理
//
// 序列化用 JSON: history 量小(典型 < 50 条),JSON 比 gob/protobuf 更便于
// 在 Redis 端用 redis-cli 直接 GET 出来排查问题。
type RedisStore struct {
	cli       *redis.Client
	ttl       time.Duration
	keyPrefix string
}

// NewRedisStore 用配置构造一个 RedisStore。
//
// 调用方应在程序退出前调 Close()。
func NewRedisStore(c config.SessionConf) *RedisStore {
	cli := redis.NewClient(&redis.Options{
		Addr:     c.RedisAddr,
		Password: c.Password,
		DB:       c.DB,
	})
	return &RedisStore{
		cli:       cli,
		ttl:       time.Duration(c.TTLHours) * time.Hour,
		keyPrefix: c.KeyPrefix,
	}
}

// Close 关闭底层 Redis 连接池。
func (r *RedisStore) Close() error {
	return r.cli.Close()
}

func (r *RedisStore) key(userID, sessionID string) string {
	return fmt.Sprintf("%s:%s:%s", r.keyPrefix, userID, sessionID)
}

func (r *RedisStore) Get(ctx context.Context, userID, sessionID string) ([]*schema.Message, error) {
	raw, err := r.cli.Get(ctx, r.key(userID, sessionID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var msgs []*schema.Message
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return msgs, nil
}

func (r *RedisStore) Save(ctx context.Context, userID, sessionID string, history []*schema.Message) error {
	buf, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	if err := r.cli.Set(ctx, r.key(userID, sessionID), buf, r.ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

func (r *RedisStore) Touch(ctx context.Context, userID, sessionID string) error {
	if r.ttl <= 0 {
		return nil
	}
	if err := r.cli.Expire(ctx, r.key(userID, sessionID), r.ttl).Err(); err != nil {
		return fmt.Errorf("redis expire: %w", err)
	}
	return nil
}

func (r *RedisStore) Delete(ctx context.Context, userID, sessionID string) error {
	if err := r.cli.Del(ctx, r.key(userID, sessionID)).Err(); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	return nil
}
