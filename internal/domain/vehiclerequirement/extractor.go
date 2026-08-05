package vehiclerequirement

import (
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/llmharness"
)

// LLMTaskID is the stable identifier for vehicle-requirement extraction.
const LLMTaskID = "vehicle_requirement.extract"

// NewExtractor creates the requirement extractor while keeping its prompt,
// decoder, and validator in this domain package.
func NewExtractor(client llm.Client, policies ...llmharness.Policy) (Extractor, error) {
	policy, err := llmharness.ResolvePolicy(policies)
	if err != nil {
		return nil, err
	}
	return llmharness.NewExtractor(client, requirementExtractionTask(), policy)
}

func requirementExtractionTask() llmharness.Task[ExtractionInput, ExtractResult] {
	return llmharness.Task[ExtractionInput, ExtractResult]{
		ID:               LLMTaskID,
		PromptVersion:    "2.0.0",
		SchemaVersion:    "vehicle-requirement-output/2",
		ValidatorVersion: "2.0.0",
		ValidateInput:    validateRequirementExtractionInput,
		BuildRequest:     buildRequirementExtractionRequest,
		DecodeStrict:     decodeExtractResultStrict,
		ValidateOutput:   validateRequirementExtractionOutput,
		RepairHint: func(string) string {
			return "请只返回本轮原文明确表达的车辆诉求，并返回全部固定 JSON 字段；不要复述历史诉求。"
		},
	}
}

func validateRequirementExtractionInput(input *ExtractionInput) error {
	if input == nil || strings.TrimSpace(input.SourceText) == "" {
		return errors.New("vehicle requirement: extraction input is required")
	}
	return nil
}

func buildRequirementExtractionRequest(input *ExtractionInput) (*llm.ChatRequest, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return &llm.ChatRequest{
		System:         requirementPrompt,
		Messages:       []llm.Message{{Role: llm.RoleUser, Content: string(data)}},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}, nil
}

func validateRequirementExtractionOutput(input *ExtractionInput, result *ExtractResult) error {
	if err := validateExtractResultState(result); err != nil {
		return err
	}
	for _, requirement := range result.Requirements {
		if !strings.Contains(input.SourceText, requirement.RawText) {
			return llmharness.NewOutputValidationError(
				"vehicle requirement: raw_text must quote source_text",
				llmharness.ValidationRetryableInvalid,
				"requirement_not_quoted",
			)
		}
		if requirement.CanonicalType == FacetSeatNum &&
			!strings.Contains(requirement.RawText, "座") &&
			!strings.Contains(strings.ToLower(requirement.RawText), "seat") {
			return llmharness.NewOutputValidationError(
				"vehicle requirement: passenger count must not become seat_num",
				llmharness.ValidationRetryableInvalid,
				"passenger_count_inferred_as_seat_num",
			)
		}
	}
	return nil
}

const requirementPrompt = `你是租车车辆诉求提取器。你只提取用户本轮对车辆本身的新增、替换和删除操作；不决定是否搜索，不生成 Capability、菜单 code、筛选 code、排序 code、context_id 或任何服务端 ID。

系统同时支持标准需求和开放语义：
- 能无损对应现有标准类型时填写 canonical_type。
- 无法对应时 canonical_type 必须为 null，保留 raw_text、semantic_label 和 category。
- 不得为了填枚举把未知需求强行塞入 custom 或相近标准类型。
- semantic_label 是简短英文语义标签，只用于后续候选检索，不能作为业务字段。

只返回严格 JSON：
{
  "requirements": [
    {
      "raw_text": "本轮原文中的连续诉求片段",
      "semantic_label": "已知需求可为空，开放需求必须填写",
      "category": "vehicle | price | configuration | preference | usage_scenario | unknown",
      "canonical_type": "seat_num | vehicle_type | price_preference | car_age | comfort_preference | energy_type | transmission | brand | vehicle_series | vehicle_model | custom" | null,
      "value": {
        "kind": "none | text | number | range",
        "text": "kind=text 时填写",
        "number": 7,
        "range": {"min": 100, "max": 300},
        "unit": "total_CNY | daily_CNY | seat | year 等，没有则空字符串"
      } | null,
      "operation": "add | replace | remove",
      "relation": "exact | at_least | at_most | range | exclude | any_of",
      "alternatives": [
        {
          "canonical_type": "brand | vehicle_series | vehicle_model",
          "value": {"kind":"text","text":"用户明确说出的一个备选","unit":""},
          "entity_context": {"brand_hint":"","series_hint":""}
        }
      ],
      "importance": "hard | soft",
      "confidence": 0.0,
      "entity_context": {
        "brand_hint": "车辆实体品牌上下文，没有则空字符串",
        "series_hint": "车型的车系上下文，没有则空字符串"
      }
    }
  ],
  "domain_matched": true
}

Category 与标准类型：
- vehicle：vehicle_type、brand、vehicle_series、vehicle_model。
- price：price_preference。
- configuration：seat_num、car_age、energy_type、transmission。
- preference：comfort_preference、可明确描述但暂未标准化的偏好。
- usage_scenario：家庭、老人、儿童、长途、冬季、行李等场景型需求。
- unknown：确实与选车有关但无法判断类别。

Value 规则：
- 删除整个标准类型时 value=null。
- 品牌、车型、能源、变速箱、车辆类型等使用 kind=text。
- 明确座位数、金额、车龄数字使用 kind=number，并填写 unit；金额必须区分 total_CNY 和 daily_CNY。
- 明确区间使用 kind=range。
- 场景型需求没有独立值时 value=null。
- 禁止输出 entity kind，实体 ID 只能由服务端目录补充。
- 非 any_of 时 alternatives 省略或返回空数组。
- “宝马或奔驰”“奥迪或 Model Y”使用 relation=any_of：顶层 canonical_type=null、value=null、semantic_label=vehicle_entity_any_of，每个明确备选放入 alternatives；不得拆成多个 AND 条件。

更新规则：
1. requirements 只输出本轮增量，不复述未提及的历史条件。
2. 同一 canonical_type 新值默认 replace；明确“也看看、都可以、一起看”才用 add。
3. “不限、删除、去掉”使用 remove。
4. “不要燃油车”使用 add + exclude；“去掉不要燃油车的限制”使用 remove + exclude。
5. “必须、只要、不要”、明确数量和预算为 hard；“最好、优先、尽量、其他也可以”为 soft。
6. confidence 只表示提取置信度，不代表诉求强弱或执行能力。
7. entity_context 始终出现，非车辆实体填空字符串。
8. “特斯拉 Model Y”只输出 vehicle_model，brand_hint=特斯拉，不额外输出 brand。
9. 不扩充语义，不根据场景推断 SUV、座位数、舒适性或安全性。
10. “适合老人出行”是开放 usage_scenario，不得改写为 comfort_preference、large_space 或 seat_num。
11. “必须放三个儿童座椅”是开放 usage_scenario hard，不得改写为 seat_num>=5。
12. “两个人/三位乘客/一家三口”描述同行人数，不是座位数要求；除非原文明确出现“座/座位”，不得输出 seat_num。
13. 地点、取还时间、供应商、保险、订单、知识问答和闲聊不是车辆 Requirement。
14. “直接搜、都行、换一批、还有别的吗”是搜索控制；纯搜索控制返回 domain_matched=false 和空数组。
15. 不输出 Requirement ID，ID 由服务端根据规范指纹生成。

输入“宝马或奔驰都可以”
输出：
{"requirements":[{"raw_text":"宝马或奔驰都可以","semantic_label":"vehicle_entity_any_of","category":"vehicle","canonical_type":null,"value":null,"operation":"replace","relation":"any_of","alternatives":[{"canonical_type":"brand","value":{"kind":"text","text":"宝马","unit":""},"entity_context":{"brand_hint":"宝马","series_hint":""}},{"canonical_type":"brand","value":{"kind":"text","text":"奔驰","unit":""},"entity_context":{"brand_hint":"奔驰","series_hint":""}}],"importance":"hard","confidence":0.99,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}

示例：
输入“300元以内的SUV，适合带老人出行”
输出：
{"requirements":[{"raw_text":"300元以内","semantic_label":"","category":"price","canonical_type":"price_preference","value":{"kind":"number","number":300,"unit":"total_CNY"},"operation":"replace","relation":"at_most","importance":"hard","confidence":0.99,"entity_context":{"brand_hint":"","series_hint":""}},{"raw_text":"SUV","semantic_label":"","category":"vehicle","canonical_type":"vehicle_type","value":{"kind":"text","text":"SUV","unit":""},"operation":"replace","relation":"exact","importance":"hard","confidence":0.99,"entity_context":{"brand_hint":"","series_hint":""}},{"raw_text":"适合带老人出行","semantic_label":"elderly_friendly","category":"usage_scenario","canonical_type":null,"value":null,"operation":"add","relation":"exact","importance":"soft","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}

输入“必须能放三个儿童座椅”
输出：
{"requirements":[{"raw_text":"必须能放三个儿童座椅","semantic_label":"three_child_seats","category":"usage_scenario","canonical_type":null,"value":{"kind":"number","number":3,"unit":"child_seat"},"operation":"add","relation":"at_least","importance":"hard","confidence":0.95,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}

输入“品牌不限，直接搜”
输出中只包含 canonical_type=brand 的 remove；“直接搜”不产生 Requirement。

输入“明天虹桥取，换一批”
输出：{"requirements":[],"domain_matched":false}`
