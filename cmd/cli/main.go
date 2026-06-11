// Command cli 是 P1 阶段的调试入口。
// 用法:
//
//	go run ./cmd/cli                                  # 默认 dev 环境,日志写 logs/agent.log
//	go run ./cmd/cli -log-file -                      # 日志改写 stderr(混屏)
//	go run ./cmd/cli -city 北京                       # 给 LLM 一个城市提示
//
// 退出:输入 :exit / :quit 或 Ctrl-D
//
// 设计:
//   - stdout 只输出"用户对话相关"内容(banner + 你/agent 行 + LLM 文本)
//   - 诊断输出(http req/resp、tool 入口/出口)统一走 logWriter,
//     -log-file 默认 logs/agent.log;传 - 则写 stderr。
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/zxq97/agent/internal/agent"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/llm"
	"github.com/zxq97/agent/internal/prompt"
	"github.com/zxq97/agent/internal/tools"
)

func main() {
	var (
		envName   = flag.String("env", "dev", "环境名,对应 conf/<env>.yaml")
		confDir   = flag.String("conf-dir", "conf", "conf 目录")
		logFile   = flag.String("log-file", "logs/agent.log", "诊断日志输出文件。传 - 则写 stderr(混屏)。默认写 logs/agent.log,stdout 只剩对话")
		cityHint  = flag.String("city", "", "给 LLM 的城市提示(可选)")
		assistant = flag.String("name", "小租", "客服昵称(写入 system prompt)")
	)
	flag.Parse()

	logW, closeLog := openLogWriter(*logFile)
	defer closeLog()
	logger := log.New(logW, "", log.LstdFlags|log.Lmicroseconds)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go handleSignal(cancel)

	confPath := filepath.Join(*confDir, *envName+".yaml")
	cfg, err := config.Load(confPath)
	exitOn(err, "load config")

	chat, err := llm.NewFactory(&cfg.LLM).Get(ctx, "shopping")
	exitOn(err, "build chat model")
	// 把 LLM 也套一层日志:每轮 Generate/Stream 的入参 messages、出参 content + tool_calls 全部落地。
	chat = llm.NewLoggingChatModel(chat, logW)

	deps := tools.NewDeps(cfg, logW)
	allTools, err := tools.All(ctx, deps)
	exitOn(err, "build tools (拉 tyche tools/list 失败:检查 tyche.endpoint 与白名单手机号)")
	// 给每个 tool 再套一层 InvokableRun 入口/出口日志(args 是 eino 累加后的完整 JSON)。
	allTools = tools.WrapWithLogging(allTools, logW)

	sa, err := agent.NewShoppingAgent(ctx, chat, allTools, prompt.ShoppingSystemVars{
		CityHint:      *cityHint,
		AssistantName: *assistant,
	})
	exitOn(err, "build shopping agent")

	printBanner(cfg, *logFile, sa, logger)

	reader := bufio.NewReader(os.Stdin)
	history := make([]*schema.Message, 0, 32)

	for {
		fmt.Print("\n你: ")
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			fmt.Println("\n(EOF) 再见")
			return
		}
		exitOn(err, "read stdin")

		userText := strings.TrimSpace(line)
		if userText == "" {
			continue
		}
		if userText == ":exit" || userText == ":quit" {
			fmt.Println("再见")
			return
		}
		if userText == ":reset" {
			history = history[:0]
			fmt.Println("(已清空历史)")
			logger.Println("=== history reset ===")
			continue
		}

		logger.Printf("=== user: %s", userText)
		history = append(history, schema.UserMessage(userText))

		fmt.Print(*assistant + ": ")
		assistantMsg, err := streamOnce(ctx, sa, history, logger)
		if err != nil {
			fmt.Printf("\n[错误] %v\n", err)
			logger.Printf("[error] streamOnce: %v", err)
			// 出错时撤回这一轮用户消息,避免下一轮污染
			history = history[:len(history)-1]
			continue
		}
		fmt.Println()
		history = append(history, assistantMsg)
	}
}

// openLogWriter 按 -log-file 选 writer。
//   - "" 或 "-" → stderr(老行为,混屏调试用),close 是 no-op
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
	// 文件被打开时先写一行分隔,便于多次启动定位。
	fmt.Fprintf(f, "\n========== rental-agent CLI session start %s ==========\n", time.Now().Format(time.RFC3339))
	return f, func() { _ = f.Close() }
}

// streamOnce 调一次 agent.Stream,把每个 chunk 的 content 打到 stdout。
//
// 流式片段日志(chunk / tool-call / tool-result / stream-end)默认全部关闭 ——
// 它们和 eino 累加之后的真实调用对不上号,只会扰乱视线。
// 真相由 internal/tools/logging.go 里的 [tool-in]/[tool-out] 与 httpclient 的 [http] 给出。
func streamOnce(
	ctx context.Context,
	sa *agent.ShoppingAgent,
	history []*schema.Message,
	logger *log.Logger,
) (*schema.Message, error) {
	sr, err := sa.Stream(ctx, history)
	if err != nil {
		return nil, fmt.Errorf("stream: %w", err)
	}
	defer sr.Close()

	var buf strings.Builder
	for {
		chunk, err := sr.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("recv chunk: %w", err)
		}
		if chunk == nil {
			continue
		}
		if chunk.Content != "" {
			fmt.Print(chunk.Content)
			buf.WriteString(chunk.Content)
		}
	}
	logger.Printf("=== assistant: %s", truncate(buf.String(), 500))
	return schema.AssistantMessage(buf.String(), nil), nil
}

func printBanner(cfg *config.Config, logFile string, sa *agent.ShoppingAgent, logger *log.Logger) {
	logTo := logFile
	if logFile == "" || logFile == "-" {
		logTo = "stderr"
	}
	fmt.Printf("==============================================\n")
	fmt.Printf(" 租车智能体 CLI\n")
	fmt.Printf("   env      = %s\n", cfg.Env)
	fmt.Printf("   tyche    = %s (timeout=%ds, phone=%s)\n", cfg.Tyche.Endpoint, cfg.Tyche.Timeout, maskPhone(cfg.Tyche.Phone))
	fmt.Printf("   log     -> %s\n", logTo)
	fmt.Printf(" 输入 :exit 退出,:reset 清空历史\n")
	fmt.Printf("==============================================\n")
	logger.Printf("[system prompt]\n%s", sa.SystemPrompt())
}

// maskPhone 仅在 banner 上做脱敏展示,避免明文打印手机号。
func maskPhone(p string) string {
	if len(p) < 7 {
		return p
	}
	return p[:3] + "****" + p[len(p)-4:]
}

func handleSignal(cancel context.CancelFunc) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	fmt.Println("\n(收到中断) 退出中...")
	cancel()
	os.Exit(0)
}

func exitOn(err error, what string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "fatal: %s: %v\n", what, err)
	os.Exit(1)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
