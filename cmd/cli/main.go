package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"

	"github.com/zxq97/agent/internal/agent"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/llm"
	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/skill"
	"github.com/zxq97/agent/internal/skill/billing"
	"github.com/zxq97/agent/internal/skill/fulfillment"
	"github.com/zxq97/agent/internal/skill/insurance"
	"github.com/zxq97/agent/internal/skill/vehicle"
	"github.com/zxq97/agent/internal/tool"
	"github.com/zxq97/agent/internal/tool/knowledge"
	"github.com/zxq97/agent/internal/tool/mcp"
)

func main() {
	// 命令行参数
	configPath := flag.String("config", "configs/config.yaml", "配置文件路径")
	flag.Parse()

	// 初始化日志
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

	ctx := context.Background()

	// 1. 加载配置
	cfg := config.Load(*configPath)
	if err := cfg.Validate(); err != nil {
		fmt.Printf("❌ 配置校验失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("🚗 在线租车智能助手启动中...")

	// 2. 初始化 LLM
	chatModel, err := llm.NewChatModel(ctx, cfg)
	if err != nil {
		fmt.Printf("❌ LLM 初始化失败: %v\n", err)
		os.Exit(1)
	}

	// 3. 初始化 Tool Registry + 注册 MCP ToolProvider
	toolRegistry := tool.NewRegistry()
	mcpProvider := mcp.NewMCPToolProvider(cfg.MCP)
	if err := toolRegistry.RegisterProvider(ctx, mcpProvider); err != nil {
		fmt.Printf("❌ MCP 工具加载失败: %v\n", err)
		fmt.Println("提示: 请检查 MCP_BASE_URL、MCP_TOKEN、MCP_PHONE 环境变量")
		os.Exit(1)
	}

	// 4. 注册知识库 ToolProvider（车辆域）
	vehicleKnowledgeProvider := knowledge.NewProvider("knowledge", "vehicle")
	if err := toolRegistry.RegisterProvider(ctx, vehicleKnowledgeProvider); err != nil {
		fmt.Printf("⚠️ 车辆知识库加载失败（不影响核心功能）: %v\n", err)
	}

	// 5. 初始化 Session Manager
	sessionMgr := session.NewManager(cfg.Agent.MaxHistoryTurns)

	// 6. 创建 Skill Agents
	// Vehicle Agent: 语义化 MCP Tool + 知识库检索 Tool
	vehicleTools, err := mcp.NewVehicleTools(mcpProvider.Client())
	if err != nil {
		fmt.Printf("❌ 创建车辆语义化工具失败: %v\n", err)
		os.Exit(1)
	}
	vehicleTools = append(vehicleTools, toolRegistry.Select("search_vehicle_knowledge")...)
	vehicleAgent := vehicle.NewAgent(chatModel, vehicleTools)

	insuranceAgent := insurance.NewAgent(chatModel, toolRegistry.Select("rental_search_"))
	billingAgent := billing.NewAgent(chatModel, toolRegistry.Select("rental_get_order_", "rental_get_reservation"))
	fulfillmentAgent := fulfillment.NewAgent(chatModel, toolRegistry.Select("rental_get_order_", "rental_get_reservation"))

	// 7. 注册到 Skill Registry
	skillRegistry := skill.NewSkillRegistry()
	skillRegistry.Register(vehicleAgent)
	skillRegistry.Register(insuranceAgent)
	skillRegistry.Register(billingAgent)
	skillRegistry.Register(fulfillmentAgent)

	// 7. 创建 Orchestrator
	orchestrator := agent.NewOrchestrator(chatModel, skillRegistry, toolRegistry, sessionMgr)

	// 8. 创建会话
	sess := sessionMgr.Create()

	// 9. 交互循环
	fmt.Println("✅ 助手已就绪！输入问题开始对话，输入 exit 退出。")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("🚗 你: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue
		}
		if input == "exit" || input == "退出" {
			fmt.Println("👋 再见！")
			break
		}

		result, err := orchestrator.Run(ctx, sess.ID, input)
		if err != nil {
			fmt.Printf("❌ 错误: %v\n", err)
			continue
		}

		fmt.Printf("🤖 助手: %s\n\n", result)
	}
}
