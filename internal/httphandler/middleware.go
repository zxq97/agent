package httphandler

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/zxq97/agent/pkg/log"
)

func chain(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for index := len(middleware) - 1; index >= 0; index-- {
		handler = middleware[index](handler)
	}
	return handler
}

func requestContext(logger log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ctx := log.WithLogger(request.Context(), logger)
			ctx = log.WithTraceID(ctx, requestID())
			writer.Header().Set("X-Trace-Id", log.TraceID(ctx))
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

func accessLog() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			started := time.Now()
			next.ServeHTTP(writer, request)
			log.Write(request.Context(), log.Entry{Component: "web", Operation: request.Method + " " + request.URL.Path, DurationMS: time.Since(started).Milliseconds()})
		})
	}
}

func recoverPanic() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			defer func() {
				if value := recover(); value != nil {
					log.Write(request.Context(), log.Entry{Component: "web", Operation: "recover", Error: fmt.Sprintf("%v", value), Response: string(debug.Stack())})
					jsonError(writer, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(writer, request)
		})
	}
}

func cors() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-Id")
			if request.Method == http.MethodOptions {
				writer.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func requestID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", value)
}
