package rentalcontext

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/llmharness"
)

var ErrDomainMismatch = errors.New("input does not belong to modify rental context domain")

// LLMTaskID is the stable identifier for rental-context extraction.
const LLMTaskID = "rental_context.extract"

type CommandExtractor interface {
	Extract(context.Context, *ExtractionInput) (*RentalContextExtractResult, error)
}

type LLMCommandExtractor struct {
	harness *llmharness.Harness[ExtractionInput, RentalContextExtractResult]
}

func NewLLMCommandExtractor(client llm.Client, policies ...llmharness.Policy) (*LLMCommandExtractor, error) {
	if client == nil {
		return nil, errors.New("modify rental context: llm client is required")
	}
	policy, err := llmharness.ResolvePolicy(policies)
	if err != nil {
		return nil, err
	}
	harness, err := llmharness.New(client, rentalExtractionTask(), policy)
	if err != nil {
		return nil, err
	}
	return &LLMCommandExtractor{harness: harness}, nil
}

func (e *LLMCommandExtractor) Extract(ctx context.Context, input *ExtractionInput) (*RentalContextExtractResult, error) {
	result, err := e.harness.Run(ctx, &llmharness.RunRequest[ExtractionInput]{Input: input})
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

func rentalExtractionTask() llmharness.Task[ExtractionInput, RentalContextExtractResult] {
	return llmharness.Task[ExtractionInput, RentalContextExtractResult]{
		ID:               LLMTaskID,
		PromptVersion:    "1.0.0",
		SchemaVersion:    "rental-context-output/1",
		ValidatorVersion: "1.0.0",
		ValidateInput:    validateRentalExtractionInput,
		BuildRequest:     buildRentalExtractionRequest,
		DecodeStrict:     decodeExtractResultStrict,
		ValidateOutput: func(_ *ExtractionInput, result *RentalContextExtractResult) error {
			return validateExtractResult(result)
		},
		RepairHint: func(string) string {
			return "请重新返回全部固定字段；未修改的时间必须是 absent、raw 为空且 value 为 null。"
		},
	}
}

func validateRentalExtractionInput(input *ExtractionInput) error {
	if input == nil || strings.TrimSpace(input.SourceText) == "" {
		return errors.New("modify rental context: extraction input is required")
	}
	return nil
}

func buildRentalExtractionRequest(input *ExtractionInput) (*llm.ChatRequest, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return &llm.ChatRequest{
		System:         extractSystemPrompt,
		Messages:       []llm.Message{{Role: llm.RoleUser, Content: string(payload)}},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}, nil
}

const extractSystemPrompt = `你是租车取还条件提取器，只负责识别用户本轮是否修改租车地点、取车时间或还车时间。

输入 JSON 结构固定为：
{
  "source_text": "本轮用户原文",
  "current_state": {
    "location_name": "当前已确认地点，可能为空",
    "pickup_time": "当前取车时间 RFC3339，可能为 null",
    "return_time": "当前还车时间 RFC3339，可能为 null"
  },
  "recent_domain_history": [{"user_text": "最近同领域原文"}],
  "now": "当前时间 RFC3339",
  "timezone": "IANA 时区名称"
}

只返回一个 JSON 对象，禁止 Markdown、解释文字和额外字段。所有以下 key 都必须出现：
{
  "location_query": "string",
  "pickup_time": {
    "status": "absent | resolved | ambiguous",
    "raw": "string",
    "value": "RFC3339 string | null"
  },
  "return_time": {
    "status": "absent | resolved | ambiguous",
    "raw": "string",
    "value": "RFC3339 string | null"
  },
  "domain_matched": true
}

字段规则：
1. location_query 只放用户本轮明确表达的地点文字，例如“虹桥机场”“南京路”。不要生成 location_id、city_id、经纬度、地址对象；没有修改地点时必须为 ""。
2. pickup_time 和 return_time 的三个子 key 必须始终出现。
3. status=absent：本轮没有修改该字段，raw="" 且 value=null。不要因为 current_state 已有值就重复输出。
4. status=resolved：时间含义足够明确，raw 保留对应原文，value 必须按输入 now 和 timezone 换算为带时区的 RFC3339 字符串。
5. status=ambiguous：时间不能唯一确定，raw 保留模糊原文，value=null。例如只有“晚上”，但没有能够从上下文唯一确定的日期或具体时间。
6. domain_matched=true：本轮至少修改了地点、取车时间、还车时间之一，即使其中某个时间仍 ambiguous。
7. domain_matched=false：本轮完全没有地点或取还时间修改。此时 location_query=""，两个时间都必须是 absent。
8. 车型、品牌、座位、能源、价格、保险、供应商、闲聊不属于本领域；混合输入只提取地点和时间部分。
9. recent_domain_history 只用于理解“提前一天”“还是晚上”等承接表达，不能把历史中未被本轮提及的字段重新输出。
10. 不得猜测服务端 ID，不得把字符串字段输出成对象或数组。

示例1：
输入 source_text="换到南京路"
输出：
{"location_query":"南京路","pickup_time":{"status":"absent","raw":"","value":null},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":true}

示例2：
输入 now="2026-07-23T10:00:00+08:00", timezone="Asia/Shanghai", source_text="明天下午3点取车"
输出：
{"location_query":"","pickup_time":{"status":"resolved","raw":"明天下午3点","value":"2026-07-24T15:00:00+08:00"},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":true}

示例3：
输入 source_text="晚上取车"
输出：
{"location_query":"","pickup_time":{"status":"ambiguous","raw":"晚上","value":null},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":true}

示例4：
输入 source_text="明天虹桥机场取车，想要7座SUV"
输出中只提取“明天”和“虹桥机场”，忽略“7座SUV”。

示例5：
输入 source_text="找300元以内的SUV"
输出：
{"location_query":"","pickup_time":{"status":"absent","raw":"","value":null},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":false}`
