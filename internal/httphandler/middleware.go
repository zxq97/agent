package httphandler

import (
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/zxq97/rental-agent/internal/session"
	"github.com/zxq97/rental-agent/internal/trace"
)

// recovery 捕获 panic,返回 500 并记日志。
func recovery(logger io.Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					if logger != nil {
						fmt.Fprintf(logger, "[recovery] panic: %v\n%s\n", err, debug.Stack())
					}
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// traceMiddleware generates one trace_id per HTTP request and injects it into context.
func traceMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID := trace.NewID()
			w.Header().Set("X-Trace-Id", traceID)
			next.ServeHTTP(w, r.WithContext(trace.WithTrace(r.Context(), traceID)))
		})
	}
}

// cors 开发环境宽松 CORS,处理 OPTIONS 预检。
func cors() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// accessLog 请求日志(受 -v 控制)。
func accessLog(logger io.Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if logger == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			fmt.Fprintf(logger, "[trace=%s] [http] %s %s dur_ms=%d\n", trace.FromCtx(r.Context()), r.Method, r.URL.Path, time.Since(start).Milliseconds())
		})
	}
}

// accessLock serializes requests for the same user/session when a lock is configured.
func accessLock(lock *session.AccessLock) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if lock == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := r.URL.Query().Get("user_id")
			sessionID := r.URL.Query().Get("session_id")
			if userID == "" || sessionID == "" {
				next.ServeHTTP(w, r)
				return
			}
			release, ok := lock.TryAcquire(userID, sessionID)
			if !ok {
				http.Error(w, `{"error":"request already running"}`, http.StatusTooManyRequests)
				return
			}
			defer release()
			next.ServeHTTP(w, r)
		})
	}
}

// chain 按顺序套用中间件(最先的最外层)。
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
