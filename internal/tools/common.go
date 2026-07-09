// Package tools 提供 Capability 内部调用的 tyche MCP 工具封装。
//
// 与旧 scaffold 的区别:这些工具**不再注册到 LLM 可见 toolset**。
// LLM 看到的是 decide 层声明的 schema(internal/agent/decide.go);
// 真正的 tyche tool 只被 Capability 内部 Go 代码调用,关键 ID 由 Go 从 state 注入。
package tools

import (
	"io"
	"time"

	"github.com/zxq97/rental-agent/internal/agenthub"
	"github.com/zxq97/rental-agent/internal/config"
	"github.com/zxq97/rental-agent/internal/tyche"
)

// 只读工具名(沿用 tyche 命名)。
//
// ToolSearchQuotes 已下线,报价一律走 rental-guide 的 /car/rental/guide/store/list/agent。
// 该常量仅保留作为"这一轮是搜车"的日志/引导胶囊 tag(GuideActionStage 用它区分要不要
// emit 搜车快捷按钮),**不在白名单里**,任何真调用都会被 Deps.Call 硬拒。
const (
	ToolSearchLocations = "rental_search_locations"
	ToolResolvePOI      = "rental_resolve_poi"
	ToolSearchQuotes    = "rental_search_quotes" // deprecated: 仅作 tag 用,禁止真调
	ToolGetOrderDetails = "rental_get_order_details"
	ToolGetReservation  = "rental_get_reservation"
	ToolGetDriverList   = "rental_get_driver_list"
)

// Deps tool 层共享依赖。
type Deps struct {
	Cfg      *config.Config
	Tyche    *tyche.Client      // MCP JSON-RPC(取还车 POI 等)
	Guide    *tyche.GuideClient // rental-guide 集群(报价+菜单)
	AgentHub agenthub.Client    // AgentHub 规则知识检索
	Logger   io.Writer          // 可空;非空时落 tool 调用日志
}

// NewDeps 用 cfg 构造默认依赖。logOut 非 nil 时,tyche RPC 与 tool 调用日志写它。
func NewDeps(cfg *config.Config, logOut io.Writer) *Deps {
	var guide *tyche.GuideClient
	if cfg.Guide.Endpoint != "" {
		guide = tyche.NewGuideClient(cfg.Guide.Endpoint, cfg.Guide.Phone, cfg.Guide.Timeout, logOut)
	}
	return &Deps{
		Cfg:   cfg,
		Tyche: tyche.New(cfg.Tyche.Endpoint, cfg.Tyche.Phone, cfg.Tyche.Timeout, logOut),
		Guide: guide,
		AgentHub: agenthub.New(agenthub.Config{
			Host:            cfg.AgentHub.Host,
			RetrievalAPIKey: cfg.AgentHub.RetrievalAPIKey,
			Timeout:         cfg.AgentHub.Timeout,
		}),
		Logger: logOut,
	}
}

// isAllowedTool LLM/工具白名单 —— 只放只读/查询工具。
// 写操作(rental_create_order / pay / refund / modify_order)严禁出现。
// rental_search_quotes 已下线(报价走 rental-guide),故不在白名单。
func isAllowedTool(name string) bool {
	switch name {
	case ToolSearchLocations, ToolResolvePOI,
		ToolGetOrderDetails, ToolGetReservation, ToolGetDriverList:
		return true
	}
	return false
}

// IsAllowedTool 导出供启动时自检/CI 用。
func IsAllowedTool(name string) bool { return isAllowedTool(name) }

// QuoteTTL 报价时效:超过即视为过期,看明细前需重搜。
const QuoteTTL = 15 * time.Minute
