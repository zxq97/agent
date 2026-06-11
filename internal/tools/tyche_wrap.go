package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	"github.com/zxq97/agent/internal/tyche"
)

// tycheInvokableTool 把一个 tyche MCP tool 元数据(name/description/inputSchema)
// 包装成 eino tool.InvokableTool。
//
// 区别于 utils.InferTool(从 Go struct + jsonschema tag 反推),
// 这里 schema 直接来自 tyche 服务端,我们不重写,确保和 server 端契约一致。
type tycheInvokableTool struct {
	cli  *tyche.Client
	info *schema.ToolInfo
}

// newTycheInvokableTool 通过 tyche tools/list 返回的元数据构造一个 InvokableTool。
//
// inputSchema 是 tyche 端给的 JSON Schema(JSON 原文),用 schema.NewParamsOneOfByJSONSchema
// 喂给 eino,让 LLM 拿到原汁原味的字段定义。
func newTycheInvokableTool(d *Deps, item tyche.ToolListItem) (tool.InvokableTool, error) {
	info := &schema.ToolInfo{
		Name: item.Name,
		Desc: item.Description,
	}
	// inputSchema 不为空才尝试解析 —— 没有也能返回(LLM 视为无参 tool)。
	if len(item.InputSchema) > 0 && string(item.InputSchema) != "null" {
		// 解析 JSON Schema:这里用 json 反序列化到 any,再交给 ParamsOneOf。
		// eino 的 NewParamsOneOfByJSONSchema 期望具体 jsonschema 类型,
		// 我们简化用 NewParamsOneOfByOpenAPIV3 走 OpenAPI v3 schema 文档(更宽容)。
		// 如果解析失败,退化为"无显式参数",LLM 仍可凭 description 调用。
		params, err := buildParamsOneOf(item.InputSchema)
		if err == nil {
			info.ParamsOneOf = params
		} else if d.Logger != nil {
			fmt.Fprintf(d.Logger, "[tyche-wrap] %s schema parse warn: %v\n", item.Name, err)
		}
	}
	return &tycheInvokableTool{cli: d.cliOrPanic(), info: info}, nil
}

func (d *Deps) cliOrPanic() *tyche.Client {
	if d.Tyche == nil {
		panic("tyche client nil; check NewDeps")
	}
	return d.Tyche
}

func (t *tycheInvokableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

// toolResult 是统一的 tool 返回结构。
// LLM 只应读 user_msg + data;debug 字段用于日志排障,不应对用户展示。
type toolResult struct {
	IsError bool            `json:"is_error"`
	UserMsg string          `json:"user_msg,omitempty"` // 出错时 LLM 对用户说的话
	Debug   string          `json:"debug,omitempty"`    // 技术细节,仅日志使用
	Data    json.RawMessage `json:"data,omitempty"`     // 正常结果直接透传
}

func (r toolResult) JSON() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// InvokableRun 把 LLM 给的 args 转发 tyche,统一用 toolResult 结构返回。
//
// 设计原则:永远不返回 Go error —— 一旦上层拿到 error,eino 会把错误文本
// 直接塞进 tool message 喂给 LLM,LLM 很可能原样复述给用户。
// 改为:任何错误都包成 {is_error:true, user_msg:"友好提示", debug:"技术细节"},
// prompt 里告诉 LLM 看到 is_error:true 时只对用户说 user_msg。
func (t *tycheInvokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	res, err := t.cli.CallTool(ctx, t.info.Name, argumentsInJSON)
	if err != nil {
		return toolResult{
			IsError: true,
			UserMsg: "查询服务暂时不可用,请稍后再试或联系人工客服。",
			Debug:   fmt.Sprintf("tyche RPC err tool=%s: %v", t.info.Name, err),
		}.JSON(), nil
	}

	// 拼接所有 text content(99% 情况只有一段)。
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

	// tyche 业务错误:IsError=true 或 errno!=0
	if res.IsError {
		return toolResult{
			IsError: true,
			UserMsg: "抱歉,暂时未能获取到相关数据,请稍后再试或换个地点/时间重新查询。",
			Debug:   fmt.Sprintf("tyche tool=%s isError body=%s", t.info.Name, truncate(combined, 400)),
		}.JSON(), nil
	}

	// 正常:tyche 的 content[0].text 是 standardResp{errno,errmsg,data,trace_id}
	// 把整段 JSON 透传给 LLM(LLM 只读 data,errno==0 代表成功)。
	return combined, nil
}

// buildParamsOneOf 解析 tyche tools/list 给的 inputSchema(JSON Schema 文档),
// 返回 eino schema.ParamsOneOf。
//
// 走法和 cloudwego/eino-ext mcp 一致:把 inputSchema JSON 直接 unmarshal
// 到 jsonschema.Schema,再 NewParamsOneOfByJSONSchema 包装。
func buildParamsOneOf(raw json.RawMessage) (*schema.ParamsOneOf, error) {
	js := &jsonschema.Schema{}
	if err := json.Unmarshal(raw, js); err != nil {
		return nil, fmt.Errorf("unmarshal input schema: %w", err)
	}
	return schema.NewParamsOneOfByJSONSchema(js), nil
}
