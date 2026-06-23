package http

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ctxKey 是 ctx 自定义键的 unexported 类型,避免和其他包冲突。
type ctxKey int

const (
	ctxKeyUserID ctxKey = iota
	ctxKeyTraceID
)

// UserIDFromCtx 从 ctx 取鉴权后注入的 user_id,无则空串。
func UserIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUserID).(string)
	return v
}

// TraceIDFromCtx 从 ctx 取本次请求的 trace_id。
func TraceIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyTraceID).(string)
	return v
}

// authMiddleware 简化版鉴权:期望前端在 Header 里带 X-User-Id。
//
// P4 范围内不实现完整签名/token 校验 —— 这块需要和公司 gateway 对齐
// (TODO 见 docs/specs/phase4-http-session-mcp.md)。这里只做"必须有 user_id"的最小校验,
// 让后续业务 / 限流 / session 隔离能基于一个稳定的 user 维度运行。
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("X-User-Id")
		if uid == "" {
			http.Error(w, "missing X-User-Id header", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyUserID, uid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// traceMiddleware 透传 X-Trace-Id;若前端没传,生成一个时间戳级别的 fallback。
//
// trace_id 会被注入 ctx,后续所有日志(LLM 调用 / tyche RPC / tool)都带上,
// 便于在公司 trace 系统里串联整条调用链。
func traceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := r.Header.Get("X-Trace-Id")
		if tid == "" {
			tid = strconv.FormatInt(time.Now().UnixNano(), 36)
		}
		ctx := context.WithValue(r.Context(), ctxKeyTraceID, tid)
		w.Header().Set("X-Trace-Id", tid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rateLimiter 是按 user_id 维度的最简 token bucket。
//
// 实现说明:
//   - 当前用进程内 map,**不**跨 pod 共享
//   - 真上线时应改成 Redis 实现(Redis INCR + EXPIRE),与公司 Redis Cluster 对齐
//   - 这里先把接口和位置占住,真实流量上来前自然会替换
type rateLimiter struct {
	mu        sync.Mutex
	perMinute int
	perDay    int
	minute    map[string]*windowCounter
	day       map[string]*windowCounter
}

type windowCounter struct {
	count       int
	windowStart time.Time
}

func newRateLimiter(perMinute, perDay int) *rateLimiter {
	return &rateLimiter{
		perMinute: perMinute,
		perDay:    perDay,
		minute:    map[string]*windowCounter{},
		day:       map[string]*windowCounter{},
	}
}

// allow 返回 (是否放行, 错误描述)。
func (r *rateLimiter) allow(userID string) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()

	if r.perMinute > 0 {
		c, ok := r.minute[userID]
		if !ok || now.Sub(c.windowStart) > time.Minute {
			r.minute[userID] = &windowCounter{count: 1, windowStart: now}
		} else {
			c.count++
			if c.count > r.perMinute {
				return false, fmt.Sprintf("rate exceeded: %d/min", r.perMinute)
			}
		}
	}
	if r.perDay > 0 {
		c, ok := r.day[userID]
		if !ok || now.Sub(c.windowStart) > 24*time.Hour {
			r.day[userID] = &windowCounter{count: 1, windowStart: now}
		} else {
			c.count++
			if c.count > r.perDay {
				return false, fmt.Sprintf("rate exceeded: %d/day", r.perDay)
			}
		}
	}
	return true, ""
}

// rateLimitMiddleware 包一层限流。
func rateLimitMiddleware(rl *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid := UserIDFromCtx(r.Context())
			if uid == "" {
				next.ServeHTTP(w, r)
				return
			}
			ok, reason := rl.allow(uid)
			if !ok {
				http.Error(w, reason, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// recoverMiddleware 兜底 panic,转 500,避免单次请求崩溃整个服务。
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				http.Error(w, fmt.Sprintf("internal error: %v", rv), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// chain 组合多个中间件,从右向左执行(最右边的最先包裹 handler)。
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
