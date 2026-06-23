// Command http 是 P4 阶段的 HTTP+SSE 服务入口。
//
// 用法:
//
//	go run ./cmd/http -env pre               # 默认日志写 logs/agent.log
//	go run ./cmd/http -env prod -log-file -  # 日志改写 stderr
//
// 设计:
//   - 复用 cmd/cli 的 agent 装配代码(internal/agent.NewSupervisorSystem)
//   - SSE 流式输出,每 event 含 agent 名 / 类型(message / event / done / error)
//   - history 落 Redis,跨请求 / 跨 pod 续聊;dev 环境 redis_addr 留空时降级 MemoryStore
//   - 优雅停机:SIGTERM/SIGINT 后等正在执行的 SSE 跑完(由 srv.Shutdown 控制)
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zxq97/agent/internal/agent"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/http"
	"github.com/zxq97/agent/internal/llm"
	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/tools"
)

func main() {
	var (
		envName   = flag.String("env", "pre", "环境名,对应 conf/<env>.yaml")
		confDir   = flag.String("conf-dir", "conf", "conf 目录")
		logFile   = flag.String("log-file", "logs/agent-http.log", "诊断日志输出文件;传 - 写 stderr")
		assistant = flag.String("name", "小租", "客服昵称(写入 system prompt)")
	)
	flag.Parse()

	logW, closeLog := openLogWriter(*logFile)
	defer closeLog()
	logger := log.New(logW, "", log.LstdFlags|log.Lmicroseconds)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	confPath := filepath.Join(*confDir, *envName+".yaml")
	cfg, err := config.Load(confPath)
	exitOn(err, "load config")

	// LLM 工厂(自动套日志层)
	factory := llm.NewFactory(&cfg.LLM)
	factory.SetLogger(logW)

	// Tools 从 tyche MCP 拉取 + 套日志层
	deps := tools.NewDeps(cfg, logW)
	allTools, err := tools.All(rootCtx, deps)
	exitOn(err, "build tools (拉 tyche tools/list 失败:检查 tyche.endpoint 与白名单手机号)")
	allTools = tools.WrapWithLogging(allTools, logW)

	// 组装 supervisor + ShoppingAgent + InsuranceAgent
	runner, err := agent.NewSupervisorSystem(rootCtx, agent.SystemDeps{
		ChatModelFactory: factory,
		AllTools:         allTools,
		MaxIterations:    cfg.Agent.MaxStep,
		AssistantName:    *assistant,
	})
	exitOn(err, "build supervisor system")

	// Session Store: 配置了 redis 走 Redis,否则降级内存(dev 友好)
	var store session.Store
	if cfg.Session.RedisAddr != "" {
		rs := session.NewRedisStore(cfg.Session)
		defer rs.Close()
		store = rs
		logger.Printf("[boot] session=redis addr=%s db=%d ttl=%dh",
			cfg.Session.RedisAddr, cfg.Session.DB, cfg.Session.TTLHours)
	} else {
		store = session.NewMemoryStore(cfg.Session.TTLHours)
		logger.Printf("[boot] session=memory ttl=%dh (no redis_addr configured)", cfg.Session.TTLHours)
	}

	// HTTP Server
	srv := http.NewServer(cfg.HTTP, cfg.RateLimit, store, runner, logger)
	printBanner(cfg, *logFile)

	// 启动监听 + 优雅停机
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "\n(收到信号 %s) 优雅停机中...\n", sig)
		logger.Printf("[shutdown] signal=%s", sig)
		// 给正在执行的 SSE 流最长 30s 跑完
		shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			logger.Printf("[shutdown] err=%v", err)
		}
		rootCancel()
	case err := <-errCh:
		if err != nil {
			logger.Printf("[fatal] http server: %v", err)
			fmt.Fprintf(os.Stderr, "fatal: http server: %v\n", err)
			os.Exit(1)
		}
	}
}

// openLogWriter 按 -log-file 选 writer。
//   - "" 或 "-" → stderr(混屏调试用),close 是 no-op
//   - 其他路径 → 创建父目录、以追加模式打开;返回 close 函数
func openLogWriter(path string) (io.Writer, func()) {
	if path == "" || path == "-" {
		return os.Stderr, func() {}
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: open log file %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Fprintf(f, "\n========== rental-agent HTTP server start %s ==========\n", time.Now().Format(time.RFC3339))
	return f, func() { _ = f.Close() }
}

func printBanner(cfg *config.Config, logFile string) {
	logTo := logFile
	if logFile == "" || logFile == "-" {
		logTo = "stderr"
	}
	fmt.Printf("==============================================\n")
	fmt.Printf(" 租车智能体 HTTP 服务 (P4)\n")
	fmt.Printf("   env       = %s\n", cfg.Env)
	fmt.Printf("   addr      = %s\n", cfg.HTTP.Addr)
	fmt.Printf("   tyche     = %s (timeout=%ds)\n", cfg.Tyche.Endpoint, cfg.Tyche.Timeout)
	if cfg.Session.RedisAddr != "" {
		fmt.Printf("   session   = redis %s db=%d ttl=%dh\n",
			cfg.Session.RedisAddr, cfg.Session.DB, cfg.Session.TTLHours)
	} else {
		fmt.Printf("   session   = memory ttl=%dh\n", cfg.Session.TTLHours)
	}
	fmt.Printf("   ratelimit = %d/min  %d/day\n", cfg.RateLimit.PerMinute, cfg.RateLimit.PerDay)
	fmt.Printf("   log      -> %s\n", logTo)
	fmt.Printf("==============================================\n")
}

func exitOn(err error, what string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "fatal: %s: %v\n", what, err)
	os.Exit(1)
}
