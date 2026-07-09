package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zxq97/rental-agent/internal/config"
	"github.com/zxq97/rental-agent/internal/orchestration"
)

type redisClient interface {
	Get(key string) (string, bool, error)
	SetEX(key, value string, ttl time.Duration) error
	Del(key string) error
	Keys(pattern string) ([]string, error)
}

// NewStore 按配置选择 RedisStore 或 MemoryStore。dev/未配 Redis 时自动降级内存。
func NewStore(cfg config.SessionConf) Store {
	return NewStoreWithLogger(cfg, nil)
}

func NewStoreWithLogger(cfg config.SessionConf, logger io.Writer) Store {
	if strings.TrimSpace(cfg.RedisAddr) == "" {
		return NewMemoryStore(cfg.TTLHours)
	}
	ttl := time.Duration(cfg.TTLHours) * time.Hour
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "agent:session"
	}
	return newRedisStoreWithClient(newRESPRedisClient(cfg.RedisAddr, cfg.Password, cfg.DB), prefix, ttl, logger)
}

type RedisStore struct {
	client   redisClient
	prefix   string
	ttl      time.Duration
	fallback *MemoryStore
	logger   io.Writer
}

func newRedisStoreWithClient(client redisClient, prefix string, ttl time.Duration, logger ...io.Writer) *RedisStore {
	if prefix == "" {
		prefix = "agent:session"
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	var out io.Writer
	if len(logger) > 0 {
		out = logger[0]
	}
	return &RedisStore{client: client, prefix: prefix, ttl: ttl, fallback: NewMemoryStore(int(ttl.Hours())), logger: out}
}

func (r *RedisStore) key(userID, sessionID string) string {
	return r.prefix + ":" + userID + ":" + sessionID
}

func (r *RedisStore) Get(userID, sessionID string) *orchestration.ConversationState {
	raw, ok, err := r.client.Get(r.key(userID, sessionID))
	if err != nil {
		r.logf("op=get status=redis_error user=%s session=%s err=%v action=fallback", userID, sessionID, err)
		return r.fallback.Get(userID, sessionID)
	}
	if !ok || raw == "" {
		if st := r.fallback.Get(userID, sessionID); st != nil {
			r.logf("op=get status=redis_miss user=%s session=%s action=fallback_hit", userID, sessionID)
			return st
		}
		return r.fallback.Get(userID, sessionID)
	}
	var st orchestration.ConversationState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		r.logf("op=get status=unmarshal_error user=%s session=%s err=%v action=fallback", userID, sessionID, err)
		return r.fallback.Get(userID, sessionID)
	}
	r.fallback.Put(userID, sessionID, &st)
	return &st
}

func (r *RedisStore) Put(userID, sessionID string, state *orchestration.ConversationState) {
	if state == nil {
		return
	}
	r.fallback.Put(userID, sessionID, state)
	b, err := json.Marshal(state)
	if err != nil {
		r.logf("op=put status=marshal_error user=%s session=%s err=%v", userID, sessionID, err)
		return
	}
	if err := r.client.SetEX(r.key(userID, sessionID), string(b), r.ttl); err != nil {
		r.logf("op=put status=redis_error user=%s session=%s err=%v action=fallback_only", userID, sessionID, err)
	}
}

func (r *RedisStore) Delete(userID, sessionID string) {
	r.fallback.Delete(userID, sessionID)
	if err := r.client.Del(r.key(userID, sessionID)); err != nil {
		r.logf("op=delete status=redis_error user=%s session=%s err=%v", userID, sessionID, err)
	}
}

func (r *RedisStore) List(userID string) []Summary {
	prefix := r.prefix + ":" + userID + ":"
	keys, err := r.client.Keys(prefix + "*")
	if err != nil {
		r.logf("op=list status=redis_error user=%s err=%v action=fallback", userID, err)
		return r.fallback.List(userID)
	}
	result := make([]Summary, 0, len(keys))
	for _, key := range keys {
		sessionID := strings.TrimPrefix(key, prefix)
		st := r.Get(userID, sessionID)
		if st == nil {
			continue
		}
		result = append(result, Summary{
			SessionID: sessionID,
			CreatedAt: st.CreatedAt,
			UpdatedAt: st.UpdatedAt,
			Preview:   extractPreview(st),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[j].UpdatedAt.Before(result[i].UpdatedAt)
	})
	return result
}

func (r *RedisStore) logf(format string, args ...any) {
	if r.logger == nil {
		return
	}
	fmt.Fprintf(r.logger, "[session] store=redis "+format+"\n", args...)
}

func matchRedisGlob(pattern, key string) bool {
	ok, err := path.Match(pattern, key)
	return err == nil && ok
}

type respRedisClient struct {
	addr     string
	password string
	db       int
	timeout  time.Duration
}

func newRESPRedisClient(addr, password string, db int) *respRedisClient {
	return &respRedisClient{addr: addr, password: password, db: db, timeout: 3 * time.Second}
}

func (c *respRedisClient) Get(key string) (string, bool, error) {
	v, err := c.command("GET", key)
	if err != nil {
		return "", false, err
	}
	if v == nil {
		return "", false, nil
	}
	s, ok := v.(string)
	return s, ok, nil
}

func (c *respRedisClient) SetEX(key, value string, ttl time.Duration) error {
	seconds := int(ttl.Seconds())
	if seconds <= 0 {
		seconds = int((24 * time.Hour).Seconds())
	}
	_, err := c.command("SETEX", key, strconv.Itoa(seconds), value)
	return err
}

func (c *respRedisClient) Del(key string) error {
	_, err := c.command("DEL", key)
	return err
}

func (c *respRedisClient) Keys(pattern string) ([]string, error) {
	v, err := c.command("KEYS", pattern)
	if err != nil {
		return nil, err
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (c *respRedisClient) command(args ...string) (any, error) {
	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	r := bufio.NewReader(conn)
	if c.password != "" {
		if err := writeRESPCommand(conn, "AUTH", c.password); err != nil {
			return nil, err
		}
		if _, err := readRESP(r); err != nil {
			return nil, err
		}
	}
	if c.db > 0 {
		if err := writeRESPCommand(conn, "SELECT", strconv.Itoa(c.db)); err != nil {
			return nil, err
		}
		if _, err := readRESP(r); err != nil {
			return nil, err
		}
	}
	if err := writeRESPCommand(conn, args...); err != nil {
		return nil, err
	}
	return readRESP(r)
}

func writeRESPCommand(conn net.Conn, args ...string) error {
	if _, err := fmt.Fprintf(conn, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return nil
}

func readRESP(r *bufio.Reader) (any, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, fmt.Errorf("redis: %s", line)
	case ':':
		return strconv.Atoi(line)
	case '$':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := r.Read(buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, err := readRESP(r)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("redis: unknown RESP prefix %q", prefix)
	}
}
