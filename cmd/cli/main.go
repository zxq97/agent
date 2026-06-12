// Command cli 是 P1-P3 阶段的调试入口。
// 用法:
//
//	go run ./cmd/cli                                  # 默认 dev 环境,日志写 logs/agent.log
//	go run ./cmd/cli -log-file -                      # 日志改写 stderr(混屏)
//	go run ./cmd/cli -city 北京                       # 给 LLM 一个城市提示
//	go run ./cmd/cli -driver-age 3                    # 已知用户驾龄,保险推荐更精准
//
// 退出:输入 :exit / :quit 或 Ctrl-D
//
// 设计:
//   - stdout 只输出"用户对话相关"内容(banner + 你/agent 行 + LLM 文本)
//   - 诊断输出(http req/resp、tool 入口/出口、ADK agent transfer 事件)统一走 logWriter,
//     -log-file 默认 logs/agent.log;传 - 则写 stderr。
//   - P3 起底层是 eino ADK supervisor 多 agent;CLI 只见 Runner 暴露的 AgentEvent stream。
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

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/zxq97/agent/internal/agent"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/llm"
	"github.com/zxq97/agent/internal/tools"
)

func main() {
	var (
		envName   = flag.String("env", "dev", "环境名,对应 conf/<env>.yaml")
		confDir   = flag.String("conf-dir", "conf", "conf 目录")
		logFile   = flag.String("log-file", "logs/agent.log", "诊断日志输出文件。传 - 则写 stderr(混屏)。默认写 logs/agent.log,stdout 只剩对话")
		cityHint  = flag.String("city", "", "给 LLM 的城市提示(可选)")
		assistant = flag.String("name", "小租", "客服昵称(写入 system prompt)")
		driverAge = flag.Int("driver-age", 0, "用户驾龄(年),0 表示未知,LLM 会在推荐保险时主动追问")
	)
	flag.Parse()
	_ = cityHint // P3 起 CityHint 暂未传给各 agent,留作 P4 扩展(可塞进 session values)

	logW, closeLog := openLogWriter(*logFile)
	defer closeLog()
	logger := log.New(logW, "", log.LstdFlags|log.Lmicroseconds)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go handleSignal(cancel)

	confPath := filepath.Join(*confDir, *envName+".yaml")
	cfg, err := config.Load(confPath)
	exitOn(err, "load config")

	// LLM 工厂:不同 agent 通过 agent_bindings 选 provider/model;SetLogger 后每次 Get 自动套日志层。
	factory := llm.NewFactory(&cfg.LLM)
	factory.SetLogger(logW)

	// Tools:从 tyche MCP 拉到的全部白名单工具,再套 InvokableRun 入口/出口日志。
	deps := tools.NewDeps(cfg, logW)
	allTools, err := tools.All(ctx, deps)
	exitOn(err, "build tools (拉 tyche tools/list 失败:检查 tyche.endpoint 与白名单手机号)")
	allTools = tools.WrapWithLogging(allTools, logW)

	// 组装 supervisor + ShoppingAgent + InsuranceAgent
	runner, err := agent.NewSupervisorSystem(ctx, agent.SystemDeps{
		ChatModelFactory: factory,
		AllTools:         allTools,
		MaxIterations:    cfg.Agent.MaxStep,
		AssistantName:    *assistant,
		DriverAge:        *driverAge,
	})
	exitOn(err, "build supervisor system")

	printBanner(cfg, *logFile, logger)

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
		newMsgs, err := runOnce(ctx, runner, history, logger)
		if err != nil {
			fmt.Printf("\n[错误] %v\n", err)
			logger.Printf("[error] runOnce: %v", err)
			// 出错时撤回这一轮用户消息,避免下一轮污染
			history = history[:len(history)-1]
			continue
		}
		history = append(history, newMsgs...)
	}
}

// runOnce 调一次 adk.Runner,把完整对话 history 喂进去,
// 流出所有 AgentEvent,实时打印最终 assistant 文字到 stdout,
// 同时把中间产生的所有消息(各 agent 的 assistant 思考、tool call/result、
// transfer 操作)收集起来追加到外部 history,确保下一轮 LLM 能看到完整链路。
func runOnce(
	ctx context.Context,
	runner *adk.Runner,
	history []*schema.Message,
	logger *log.Logger,
) ([]*schema.Message, error) {
	iter := runner.Run(ctx, history)

	var (
		collected   []*schema.Message // 本轮新产生的所有 message
		finalText   strings.Builder   // 最终展示给用户的文本(supervisor 的收尾答复)
		lastEvent   *adk.AgentEvent   // 跟踪最后一个事件,便于日志
		lastAgent   string            // 最后一个事件的 agent 名(用来判定"是不是最终输出")
	)

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		lastEvent = event
		if event.Err != nil {
			return collected, fmt.Errorf("agent event err: %w", event.Err)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		mv := event.Output.MessageOutput
		msg, err := mv.GetMessage()
		if err != nil {
			logger.Printf("[event-err] agent=%s err=%v", event.AgentName, err)
			continue
		}
		if msg == nil {
			continue
		}

		// 日志:记录每个事件的 agent + role + content/tool_calls 摘要
		logEvent(logger, event, msg)

		// 收集所有 message 进 history。
		// 注意:supervisor 的 transfer 动作也会作为 tool_call/tool_result 出现 ——
		// 把它们一并保留,LLM 后续轮次能感知到"上次转给了 InsuranceAgent"。
		collected = append(collected, msg)

		// 判定"实时打印给用户":
		// - 只打 assistant 角色 + 没有 tool_calls(纯文字答复)
		// - supervisor 完成最后 transfer 后,子 agent 的回答会接管输出;
		//   但子 agent 答完会再 transfer 回 supervisor,supervisor 通常直接回话作为最终输出。
		// - 因此每个"纯文字 assistant"我们都打 —— 多个子 agent 答完串起来正好是用户看到的全部内容。
		if msg.Role == schema.Assistant && len(msg.ToolCalls) == 0 && msg.Content != "" {
			fmt.Print(msg.Content)
			finalText.WriteString(msg.Content)
		}
		lastAgent = event.AgentName
	}
	fmt.Println()

	_ = lastEvent
	logger.Printf("=== assistant(by %s): %s (中间消息 %d 条)", lastAgent, truncate(finalText.String(), 400), len(collected)-1)
	return collected, nil
}

// logEvent 把 AgentEvent 摘要写到日志(stdout 不污染)。
func logEvent(logger *log.Logger, event *adk.AgentEvent, msg *schema.Message) {
	role := string(msg.Role)
	if msg.Role == schema.Tool {
		logger.Printf("[event] agent=%s role=%s tool=%s content=%s",
			event.AgentName, role, msg.Name, truncate(msg.Content, 300))
		return
	}
	if len(msg.ToolCalls) > 0 {
		names := make([]string, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			names = append(names, tc.Function.Name)
		}
		logger.Printf("[event] agent=%s role=%s tool_calls=%v content=%s",
			event.AgentName, role, names, truncate(msg.Content, 300))
		if event.Action != nil && event.Action.TransferToAgent != nil {
			logger.Printf("[event] agent=%s TRANSFER → %s",
				event.AgentName, event.Action.TransferToAgent.DestAgentName)
		}
		return
	}
	logger.Printf("[event] agent=%s role=%s content=%s",
		event.AgentName, role, truncate(msg.Content, 300))
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
	fmt.Fprintf(f, "\n========== rental-agent CLI session start %s ==========\n", time.Now().Format(time.RFC3339))
	return f, func() { _ = f.Close() }
}

func printBanner(cfg *config.Config, logFile string, logger *log.Logger) {
	logTo := logFile
	if logFile == "" || logFile == "-" {
		logTo = "stderr"
	}
	fmt.Printf("==============================================\n")
	fmt.Printf(" 租车智能体 CLI (P3: supervisor 多 agent)\n")
	fmt.Printf("   env      = %s\n", cfg.Env)
	fmt.Printf("   tyche    = %s (timeout=%ds, phone=%s)\n", cfg.Tyche.Endpoint, cfg.Tyche.Timeout, maskPhone(cfg.Tyche.Phone))
	fmt.Printf("   log     -> %s\n", logTo)
	fmt.Printf("   agents  = Supervisor → {ShoppingAgent, InsuranceAgent}\n")
	fmt.Printf(" 输入 :exit 退出,:reset 清空历史\n")
	fmt.Printf("==============================================\n")
	logger.Printf("[boot] env=%s tyche=%s", cfg.Env, cfg.Tyche.Endpoint)
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
