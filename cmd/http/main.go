// Command http starts the local browser UI and its JSON/SSE API.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zxq97/agent/internal/config"
	agentlog "github.com/zxq97/agent/pkg/log"
)

func main() {
	configPath := flag.String("config", "conf/dev.yaml", "configuration file")
	address := flag.String("addr", ":8080", "HTTP listen address")
	webDir := flag.String("web-dir", "web", "static frontend directory")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	logger := agentlog.NewJSONLogger(os.Stderr)
	handler, err := initializeHTTPHandler(cfg, logger)
	if err != nil {
		log.Fatalf("initialize HTTP service: %v", err)
	}

	server := &http.Server{Addr: *address, Handler: handler.Mux(*webDir), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 120 * time.Second}
	fmt.Printf("租车智能体页面已启动：http://localhost%s\n", *address)
	fmt.Printf("配置：%s，前端目录：%s\n", *configPath, *webDir)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server: %v", err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
