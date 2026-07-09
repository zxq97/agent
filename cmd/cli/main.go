// Command cli 是本地调试入口:加载 config、装配 agent、起一个多轮 REPL。
//
// 用法:
//
//	DEEPSEEK_API_KEY=sk-xxx go run ./cmd/cli -env dev
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zxq97/rental-agent/internal/agent"
	"github.com/zxq97/rental-agent/internal/config"
	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/logsink"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
)

// cliEmitter 只把 agent 的话术文字打到 stdout(保持终端只有对话)。
// Event(thinking_tips/quick_action/card 等)不上终端——它们已通过 logW 进日志文件。
// -v 时才在终端显示 event(灰色),用于调试。
type cliEmitter struct {
	showEvents bool
}

func (e cliEmitter) Text(delta string) { fmt.Print(delta) }
func (e cliEmitter) Event(name, detail string) {
	if e.showEvents {
		fmt.Printf("\n  \033[90m[%s] %s\033[0m\n", name, detail)
	}
}

func main() {
	envName := flag.String("env", "dev", "环境名,对应 conf/<env>.yaml")
	confDir := flag.String("conf-dir", "conf", "conf 目录")
	verbose := flag.Bool("v", false, "把日志复制一份到 stderr(文件仍按 conf 落盘)")
	logDir := flag.String("log-dir", "", "日志目录覆盖(空=用 conf 中 log.dir);设为 - 表示禁用文件输出")
	flag.Parse()

	confPath := filepath.Join(*confDir, *envName+".yaml")
	cfg, err := config.Load(confPath)
	exitOn(err, "load config")

	// 日志:CLI 默认不往 stderr 打(保持终端干净,只显示对话);
	// -v 时才复制到 stderr 供调试;文件落盘照常。
	logOpts := logsink.Options{
		Dir:        cfg.Log.Dir,
		Stderr:     false, // CLI 默认关 stderr,不管 conf 怎么写
		FilePrefix: cfg.Log.FilePrefix,
	}
	if *logDir != "" {
		if *logDir == "-" {
			logOpts.Dir = ""
		} else {
			logOpts.Dir = *logDir
		}
	}
	if *verbose {
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

	fmt.Println("==============================================")
	fmt.Printf(" 租车智能体 v2 (CLI 调试) env=%s\n", cfg.Env)
	fmt.Printf("   tyche    = %s\n", cfg.Tyche.Endpoint)
	fmt.Printf("   决策模型  = binding[decide] (默认 provider: %s)\n", cfg.LLM.Default)
	fmt.Println("   输入消息开始对话,Ctrl-C 退出。")
	fmt.Println("==============================================")

	ag, err := agent.New(ctx, factory, deps, logW)
	exitOn(err, "build agent")

	state := orchestration.New("cli-session", "cli-user")
	emit := cliEmitter{showEvents: *verbose}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Print("\n你> ")
		if !sc.Scan() {
			break
		}
		line := sc.Text()
		if line == "" {
			continue
		}
		fmt.Print("小租> ")
		if _, err := ag.Run(ctx, state, line, emit); err != nil {
			fmt.Printf("\n\033[31m[error] %v\033[0m\n", err)
			continue
		}
		fmt.Println()
	}
}

func exitOn(err error, what string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "fatal: %s: %v\n", what, err)
	os.Exit(1)
}
