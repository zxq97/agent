// Package tools 提供 agent 可用的全部 InvokableTool。
//
// P1 起,所有租车业务工具都来自 tyche MCP server(/car/rental/inner/mcp,
// JSON-RPC 2.0 over HTTP)。tyche 那边已经把 7 个 C 端工具维护得很完善:
//
//	rental_search_locations / rental_resolve_poi /
//	rental_search_quotes    / rental_get_order_details /
//	rental_create_order     / rental_get_reservation /
//	rental_get_driver_list
//
// 我们这边不再造直连 saas-api 的轮子,本包只负责:
//  1. 拉 tyche tools/list,把每个 tool 包装成 eino InvokableTool;
//  2. 套一层 logging,落 InvokableRun 入口/出口日志(便于诊断);
//  3. 保留必要的安全过滤(后续可在这里黑名单写操作 tool)。
package tools

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/tyche"
)

// Deps tool 工厂的共享依赖。
type Deps struct {
	Cfg     *config.Config
	Tyche   *tyche.Client
	Logger  io.Writer // 可空;非空时给 tool 入口/出口落日志
}

// NewDeps 用 cfg 构造默认依赖。logOut 非 nil 时,所有 tyche RPC 与 tool 调用日志都写它。
func NewDeps(cfg *config.Config, logOut io.Writer) *Deps {
	return &Deps{
		Cfg:    cfg,
		Tyche:  tyche.New(cfg.Tyche.Endpoint, cfg.Tyche.Phone, cfg.Tyche.Timeout, logOut),
		Logger: logOut,
	}
}

// All 拉取 tyche 全部工具并包装成 eino InvokableTool 列表。
//
// 安全:写操作类 tool(rental_create_order)按业务原则不暴露给 LLM。
// 这里通过 includeWriteTools=false 默认过滤掉它;P5 真要做"下单 deeplink/确认环节"
// 再单独打开,且必须配合人工确认环节。
func All(ctx context.Context, d *Deps) ([]tool.BaseTool, error) {
	if d.Tyche == nil {
		return nil, fmt.Errorf("tyche client not initialized")
	}
	items, err := d.Tyche.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("tyche tools/list: %w", err)
	}
	out := make([]tool.BaseTool, 0, len(items))
	for _, it := range items {
		if !isAllowedTool(it.Name) {
			continue
		}
		t, err := newTycheInvokableTool(d, it)
		if err != nil {
			return nil, fmt.Errorf("wrap tyche tool %s: %w", it.Name, err)
		}
		out = append(out, t)
	}

	// P5: 本地 tool(纯计算/规则,不走 tyche,不查后端)。
	local, err := localTools()
	if err != nil {
		return nil, fmt.Errorf("build local tools: %w", err)
	}
	out = append(out, local...)
	return out, nil
}

// localTools 返回 P5 引入的本地 InvokableTool:
//   - check_qualification:驾龄 → 可租车型(本地规则表)
//   - estimate_trip_cost:行程总费拆项估算(经验公式)
//   - optimize_pickup_time:取还时间方案对比(本地计费估算)
//
// 这些 tool 没有后端数据源,在 agent 进程内完成,因此不受 isAllowedTool
// (那是给 tyche tool 的写操作白名单)约束。
func localTools() ([]tool.BaseTool, error) {
	q, err := NewCheckQualificationTool()
	if err != nil {
		return nil, fmt.Errorf("check_qualification: %w", err)
	}
	c, err := NewEstimateTripCostTool()
	if err != nil {
		return nil, fmt.Errorf("estimate_trip_cost: %w", err)
	}
	t, err := NewOptimizePickupTimeTool()
	if err != nil {
		return nil, fmt.Errorf("optimize_pickup_time: %w", err)
	}
	return []tool.BaseTool{q, c, t}, nil
}

// isAllowedTool 是 LLM 可见 tool 的白名单。
//
// 沿用旧 spec 审查结论:写操作严禁注册到 LLM 可见 ToolSet。
// 当前 P1 只放出 6 个只读/查询类 tool;rental_create_order 暂不放出,
// 走"用户在 App 内完成下单"路径,P5 再讨论是否带强确认地放开。
func isAllowedTool(name string) bool {
	switch name {
	case "rental_search_locations",
		"rental_resolve_poi",
		"rental_search_quotes",
		"rental_get_order_details",
		"rental_get_reservation",
		"rental_get_driver_list":
		return true
	}
	// 默认拒绝,白名单驱动。
	return false
}

// timeLayout 留作其他模块解析时间用(P1 自带 tool 已下线,但保留对外能力)。
const timeLayout = "2006-01-02 15:04:05"

// parseTime 容忍 RFC3339 / "YYYY-MM-DD HH:MM" / "YYYY-MM-DD HH:MM:SS"。
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	for _, layout := range []string{
		timeLayout,
		"2006-01-02 15:04",
		time.RFC3339,
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q (use YYYY-MM-DD HH:MM:SS)", s)
}

// wrapInfer 留给后续可能的本地(非 MCP)tool 用。
func wrapInfer[I, O any](name, desc string, fn func(context.Context, I) (O, error)) (tool.InvokableTool, error) {
	return utils.InferTool(name, desc, fn)
}

// truncate 给包内日志/错误信息使用,统一截断。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
