// Command http 是 HTTP + SSE 服务入口:加载 config、装配 agent、起 HTTP server。
//
// 用法:
//
//	DEEPSEEK_API_KEY=sk-xxx go run ./cmd/http -env dev
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zxq97/rental-agent/internal/agent"
	"github.com/zxq97/rental-agent/internal/config"
	"github.com/zxq97/rental-agent/internal/httphandler"
	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/logsink"
	"github.com/zxq97/rental-agent/internal/session"
	"github.com/zxq97/rental-agent/internal/tools"
)

func main() {
	envName := flag.String("env", "dev", "环境名,对应 conf/<env>.yaml")
	confDir := flag.String("conf-dir", "conf", "conf 目录")
	webDir := flag.String("web-dir", "web", "前端静态文件目录")
	quiet := flag.Bool("q", false, "静默模式,不把日志复制到 stderr(文件仍写)")
	logDir := flag.String("log-dir", "", "日志目录覆盖(空=用 conf 中 log.dir);设为 - 表示禁用文件输出")
	flag.Parse()

	confPath := filepath.Join(*confDir, *envName+".yaml")
	cfg, err := config.Load(confPath)
	exitOn(err, "load config")

	// 日志:命令行 -log-dir 覆盖 conf,"-" 表示禁用文件;-q 覆盖 conf 里的 stderr。
	logOpts := logsink.Options{
		Dir:        cfg.Log.Dir,
		Stderr:     cfg.Log.Stderr,
		FilePrefix: cfg.Log.FilePrefix,
	}
	if *logDir != "" {
		if *logDir == "-" {
			logOpts.Dir = ""
		} else {
			logOpts.Dir = *logDir
		}
	}
	if *quiet {
		logOpts.Stderr = false
	} else {
		// HTTP 默认开 stderr 复制,除非用户显式 -q
		logOpts.Stderr = true
	}
	logW, logCloser, err := logsink.New(logOpts)
	exitOn(err, "init log")
	defer logCloser.Close()

	factory := llm.NewFactory(&cfg.LLM)
	if logW != nil {
		factory.SetLogger(logW)
	}
	deps := tools.NewDeps(cfg, logW)

	ctx := context.Background()
	ag, err := agent.New(ctx, factory, deps, logW)
	exitOn(err, "build agent")

	store := session.NewStoreWithLogger(cfg.Session, logW)
	h := httphandler.New(ag, store, logW)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      h.Mux(*webDir),
		ReadTimeout:  time.Duration(cfg.HTTP.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.HTTP.WriteTimeout) * time.Second,
	}

	fmt.Println("==============================================")
	fmt.Printf(" 租车智能体 v2 (HTTP) env=%s\n", cfg.Env)
	fmt.Printf("   addr     = %s\n", cfg.HTTP.Addr)
	fmt.Printf("   tyche    = %s\n", cfg.Tyche.Endpoint)
	fmt.Printf("   决策模型  = binding[decide] (默认 provider: %s)\n", cfg.LLM.Default)
	fmt.Printf("   前端目录  = %s\n", *webDir)
	fmt.Println("   浏览器打开 http://localhost" + cfg.HTTP.Addr + " 开始对话。")
	fmt.Println("==============================================")

	// 优雅关机
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "fatal: http listen: %v\n", err)
			os.Exit(1)
		}
	}()

	<-sigCtx.Done()
	fmt.Println("\nshutting down...")

	drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
	}
	fmt.Println("bye.")
}

func exitOn(err error, what string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "fatal: %s: %v\n", what, err)
	os.Exit(1)
}
