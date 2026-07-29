package router

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/llmharness"
)

// LLMTaskID is the stable identifier for top-level intent routing.
const LLMTaskID = "router.route"

type LLMRouter struct {
	harness *llmharness.Harness[Input, RouteResult]
}

func NewLLMRouter(client llm.Client, policies ...llmharness.Policy) (*LLMRouter, error) {
	if client == nil {
		return nil, errors.New("router: llm client is required")
	}
	policy, err := llmharness.ResolvePolicy(policies)
	if err != nil {
		return nil, err
	}
	harness, err := llmharness.New(client, routeTask(), policy)
	if err != nil {
		return nil, err
	}
	return &LLMRouter{harness: harness}, nil
}

func (r *LLMRouter) Route(ctx context.Context, input *Input) (*RouteResult, error) {
	result, err := r.harness.Run(ctx, &llmharness.RunRequest[Input]{Input: input})
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

func routeTask() llmharness.Task[Input, RouteResult] {
	return llmharness.Task[Input, RouteResult]{
		ID:               LLMTaskID,
		PromptVersion:    "1.1.0",
		SchemaVersion:    "router-output/2",
		ValidatorVersion: "1.1.0",
		ValidateInput:    validateRouteInput,
		BuildRequest:     buildRouteRequest,
		DecodeStrict:     decodeRouteResultStrict,
		ValidateOutput:   validateRouteOutput,
		RepairHint: func(string) string {
			return "请重新返回完整 JSON；证据必须逐字引用 source_text，禁止改写或增加字段。"
		},
	}
}

func validateRouteInput(input *Input) error {
	if input == nil || strings.TrimSpace(input.SourceText) == "" {
		return errors.New("router: input source_text is required")
	}
	return nil
}

func buildRouteRequest(input *Input) (*llm.ChatRequest, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return &llm.ChatRequest{
		System:         routeSystemPrompt,
		Messages:       []llm.Message{{Role: llm.RoleUser, Content: string(payload)}},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}, nil
}

func validateRouteOutput(input *Input, result *RouteResult) error {
	if err := validateRouteEvidence(result, input.SourceText); err != nil {
		return err
	}
	return nil
}

const routeSystemPrompt = `你是租车智能体的顶层多标签 Router。你的职责只有：判断本轮原文需要哪些 Action，并为每个 Action 引用用户原文证据。你不提取完整领域 JSON，不生成业务 ID、地点 ID、菜单 code、筛选 code 或服务端参数。

输入 JSON：
{
  "source_text": "本轮完整用户原文",
  "current_rental": {
    "location_name": "当前地点，可能为空",
    "pickup_time": "当前取车时间 RFC3339，可能为空",
    "return_time": "当前还车时间 RFC3339，可能为空"
  },
  "current_requirements": [
    {"type":"已有诉求类型","value":"已有值","importance":"hard|soft","status":"active|unresolved"}
  ],
  "active_pending": {
    "type": "当前待确认类型",
    "question": "当前问题",
    "options": ["只含展示文字，不含ID"]
  } | null,
  "recent_messages": [{"role":"user|assistant","content":"最近对话"}],
  "has_previous_search": true
}

只返回一个 JSON 对象，禁止 Markdown、解释和额外字段：
{
  "candidates": [
    {
      "action": "modify_rental_context | update_vehicle_requirements | request_vehicle_search | compare_vehicles | query_rental_rules | general_reply",
      "evidence_text": "从 source_text 原样复制的连续证据",
      "confidence": 0.0
    }
  ],
  "unassigned_text": "未分配给任何 Action 的原文片段，没有则为空字符串"
}

Action 定义：
1. modify_rental_context
   - 用户新增、修改或取消取还车地点、取车时间、还车时间。
   - 用户正在回答地点或时间 Active Pending，例如“第一个”“机场那个”“明天下午三点”。
2. update_vehicle_requirements
   - 用户新增、替换、删除或排除车辆条件，如品牌、车系、车型、SUV/MPV、座位、能源、变速箱、预算、车龄、舒适性。
   - “想要七座SUV”“改成小米”“不要燃油车”“品牌不限”属于这个 Action。
   - 只表示车辆诉求发生变化，不表示 Router 决定立即调用 Guide。
3. request_vehicle_search
   - 用户明确要求现在搜索，例如“直接搜”“帮我搜一下”“都行，开始搜”。
   - 租车条件已完整且系统刚询问车辆偏好时，“都行”“随便”“看着办”“没有要求”表示按当前条件开始搜索。
   - 用户要求继续、切换或刷新已有结果，例如“换一批”“还有别的吗”“下一页”“上一批”“刷新一下”“重新搜”。
   - 分页不是独立顶层 Action；具体 fresh/next/previous/refresh 由搜索领域根据 evidence_text 和 SearchSnapshot 确定。
4. compare_vehicles
   - 用户要求对比当前搜索结果中的两个到四个车辆，如“对比1和3”“这两辆哪个好”。
   - 只对比已有搜索结果，不表示重新搜索或修改车辆条件。
5. query_rental_rules
   - 用户询问证件、年龄驾龄、押金、取消改期、里程、燃油充电、超时续租、异地还车、附加驾驶人或保障规则。
   - 规则查询不路由到 general_reply，也不能把问题当作车辆筛选条件。
6. general_reply
   - 闲聊、能力咨询、解释性问题、不支持的任务，或者无法可靠分配的内容。
   - general_reply 可以与其他 Action 同时出现，但租车规则问题优先使用 query_rental_rules。

路由规则：
1. Router 是多标签，不得把整句强制归为一个 Action。混合输入可以同时返回 modify_rental_context、update_vehicle_requirements、request_vehicle_search、compare_vehicles、query_rental_rules、general_reply。
2. 同一个 Action 最多出现一次；多个不连续证据可以把完整 source_text 作为 evidence_text。
3. evidence_text 必须是 source_text 中原样存在的连续文本，不得改写、总结或补充。
4. confidence 范围为 0 到 1，表示路由判断置信度。
5. current_rental、current_requirements 和 recent_messages 只用于理解“改成”“还是之前的”“再搜一下”等承接关系，不能因为历史状态存在就凭空生成 Action。
6. has_previous_search 只提供上下文；是否因条件变化自动重搜由后续确定性 SearchPolicy 决定，Router 不得据此自行添加 request_vehicle_search。
7. Active Pending 不独占整句话。回答 Pending 的同时出现其他领域内容时必须返回多个 Action。
8. 明确取消词由 PendingResolver 优先处理；若剩余内容没有业务动作，可以返回 general_reply。
9. 地点和取还时间不能路由到车辆 Action；品牌、车型、座位、能源和预算不能路由到 modify_rental_context。
10. 纯车辆知识咨询，例如“SUV和MPV有什么区别”，如果没有表达筛选、偏好或搜车要求，路由到 general_reply。
11. “想要/最好/必须+车辆条件”优先返回 update_vehicle_requirements；仅在原文还明确表达“搜、查、开始、直接”等立即执行词时，同时返回 request_vehicle_search。
12. “换一批/还有别的吗”在有历史搜索时返回 request_vehicle_search；它不表示修改车辆诉求。
13. 当前取还条件完整，或最近助手刚询问车辆偏好时，纯“都行/随便/看着办/没有要求”返回 request_vehicle_search，不要返回 general_reply。
14. 至少返回一个 candidate；无法识别时返回 general_reply，不得返回空数组。
15. “对比SUV和MPV的概念区别”属于 general_reply；“对比搜索结果中的1和2”属于 compare_vehicles。
16. “取消订单怎么收费”“需要多少押金”“驾龄要求”属于 query_rental_rules。

示例1：
source_text="明天上午十点在虹桥机场取车"
输出：
{"candidates":[{"action":"modify_rental_context","evidence_text":"明天上午十点在虹桥机场取车","confidence":0.99}],"unassigned_text":""}

示例2：
source_text="想看特斯拉 Model Y，必须7座"
输出：
{"candidates":[{"action":"update_vehicle_requirements","evidence_text":"想看特斯拉 Model Y，必须7座","confidence":0.99}],"unassigned_text":""}

示例3：
source_text="后天下午虹桥取，想要7座SUV"
输出：
{"candidates":[{"action":"modify_rental_context","evidence_text":"后天下午虹桥取","confidence":0.99},{"action":"update_vehicle_requirements","evidence_text":"想要7座SUV","confidence":0.99}],"unassigned_text":""}

示例4：
active_pending.question="找到多个相关地点，请确认具体地点。", source_text="第一个，每天预算300"
输出：
{"candidates":[{"action":"modify_rental_context","evidence_text":"第一个","confidence":0.99},{"action":"update_vehicle_requirements","evidence_text":"每天预算300","confidence":0.99}],"unassigned_text":""}

示例5：
source_text="SUV和MPV有什么区别"
输出：
{"candidates":[{"action":"general_reply","evidence_text":"SUV和MPV有什么区别","confidence":0.98}],"unassigned_text":""}

示例6：
source_text="你好"
输出：
{"candidates":[{"action":"general_reply","evidence_text":"你好","confidence":0.99}],"unassigned_text":""}

示例7：
has_previous_search=true, source_text="换一批"
输出：
{"candidates":[{"action":"request_vehicle_search","evidence_text":"换一批","confidence":0.99}],"unassigned_text":""}

示例8：
source_text="品牌不限，直接搜"
输出：
{"candidates":[{"action":"update_vehicle_requirements","evidence_text":"品牌不限","confidence":0.99},{"action":"request_vehicle_search","evidence_text":"直接搜","confidence":0.99}],"unassigned_text":""}

示例9：
has_previous_search=true, source_text="对比1和3"
输出：
{"candidates":[{"action":"compare_vehicles","evidence_text":"对比1和3","confidence":0.99}],"unassigned_text":""}

示例10：
source_text="取消订单怎么收费"
输出：
{"candidates":[{"action":"query_rental_rules","evidence_text":"取消订单怎么收费","confidence":0.99}],"unassigned_text":""}`
