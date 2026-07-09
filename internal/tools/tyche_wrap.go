package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Result 统一的 tool 返回结构。
// LLM/Capability 只应读 user_msg + data;debug 仅日志排障,不对用户展示。
type Result struct {
	IsError bool            `json:"is_error"`
	UserMsg string          `json:"user_msg,omitempty"`
	Debug   string          `json:"debug,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// JSON 序列化为字符串(给 LLM 当 tool message)。
func (r Result) JSON() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// Call 调一个 tyche 工具,统一包成 Result。
//
// 设计原则:永远不向上层返回 Go error —— 任何错误都包成
// {is_error:true, user_msg:"友好提示", debug:"技术细节"},
// Capability 据 IsError 决定走兜底,且只把 user_msg 暴露给用户。
//
// argsJSON 是已经由 Go 注入好 ID 的完整入参 JSON。
func (d *Deps) Call(ctx context.Context, toolName, argsJSON string) Result {
	if !isAllowedTool(toolName) {
		return Result{
			IsError: true,
			UserMsg: "暂不支持该操作。",
			Debug:   fmt.Sprintf("tool %q not in allowlist", toolName),
		}
	}
	if d.Tyche == nil {
		return Result{IsError: true, UserMsg: "查询服务暂时不可用,请稍后再试。", Debug: "tyche client nil"}
	}

	start := time.Now()
	res, err := d.Tyche.CallTool(ctx, toolName, argsJSON)
	dur := time.Since(start).Milliseconds()
	if d.Logger != nil {
		fmt.Fprintf(d.Logger, "[tool] stage=capability tool=%s dur_ms=%d err=%v args=%s\n",
			toolName, dur, err, truncate(argsJSON, 4096))
	}
	if err != nil {
		return Result{
			IsError: true,
			UserMsg: "查询服务暂时不可用,请稍后再试或联系人工客服。",
			Debug:   fmt.Sprintf("tyche RPC err tool=%s: %v", toolName, err),
		}
	}

	// 拼接所有 text content(99% 只有一段)
	combined := ""
	for _, c := range res.Content {
		if c.Type == "text" {
			combined += c.Text
		}
	}
	if combined == "" {
		buf, _ := json.Marshal(res)
		combined = string(buf)
	}

	if res.IsError {
		return Result{
			IsError: true,
			UserMsg: "抱歉,暂时未能获取到相关数据,请稍后再试或换个地点/时间重新查询。",
			Debug:   fmt.Sprintf("tyche tool=%s isError body=%s", toolName, truncate(combined, 400)),
		}
	}

	// 正常:tyche content[0].text 是 {errno,errmsg,data,trace_id},整段作为 data 透传
	return Result{Data: json.RawMessage(combined)}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
