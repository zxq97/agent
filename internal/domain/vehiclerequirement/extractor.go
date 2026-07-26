package vehiclerequirement

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/llm"
)

var ErrDomainMismatch = errors.New("input does not belong to vehicle requirement domain")

type LLMExtractor struct {
	client llm.Client
}

func NewLLMExtractor(client llm.Client) (*LLMExtractor, error) {
	if client == nil {
		return nil, errors.New("vehicle requirement: llm client is required")
	}
	return &LLMExtractor{client: client}, nil
}

func (e *LLMExtractor) Extract(ctx context.Context, input *ExtractionInput) (*ExtractResult, error) {
	if input == nil || strings.TrimSpace(input.SourceText) == "" {
		return nil, errors.New("vehicle requirement: extraction input is required")
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	response, err := e.client.Chat(ctx, &llm.ChatRequest{
		Model:          llm.ModelConversation,
		System:         requirementPrompt,
		Messages:       []llm.Message{{Role: llm.RoleUser, Content: string(data)}},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, err
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return nil, errors.New("vehicle requirement: extractor returned empty content")
	}
	result, err := decodeExtractResult(response.Content)
	if err != nil {
		return nil, err
	}
	return result, nil
}

const requirementPrompt = `你是租车车辆诉求提取器。你只提取用户本轮对车辆本身的新增、替换和删除操作；不负责判断是否搜车，不生成菜单或服务端参数。

输入 JSON：
{
  "source_text": "本轮车辆领域原文",
  "current_requirements": [
    {
      "facet": "稳定语义类型",
      "raw_value": "用户原值",
      "canonical_value": "服务端已有标准值",
      "operator": "比较方式",
      "importance": "hard | soft",
      "status": "active | ambiguous | unverifiable | unsupported"
    }
  ],
  "recent_domain_history": ["最近车辆诉求原文"]
}

只返回一个 JSON 对象，禁止 Markdown、解释和额外字段：
{
  "requirements": [
    {
      "facet": "seat_num | vehicle_type | price_preference | car_age | comfort_preference | energy_type | transmission | brand | vehicle_series | vehicle_model | custom",
      "raw_text": "从本轮原文摘取的诉求片段",
      "raw_value": "该诉求的值",
      "operation": "add | replace | remove",
      "operator": "eq | not_eq | gt | gte | lt | lte | in | not_in | contains",
      "importance": "hard | soft",
      "confidence": 0.0,
      "entity_context": {
        "brand_hint": "车辆实体的品牌上下文，没有则空字符串",
        "series_hint": "具体车型的车系上下文，没有则空字符串"
      }
    }
  ],
  "domain_matched": true
}

Facet 定义：
1. seat_num：座位数。“7座”raw_value="7", operator="eq"；“至少7座”operator="gte"。
2. vehicle_type：SUV、MPV、轿车、经济型、舒适型等明确车辆类别。不能放品牌或产品名。
3. price_preference：价格或预算。尽量规范为 daily<=300CNY、total<=500CNY；“便宜点”保留为 lower。
4. car_age：车龄。“一年以内”raw_value="1", operator="lte"；“尽量新”raw_value="newer", importance="soft"。
5. comfort_preference：用户明确说的乘坐、座椅、静谧、悬架、长途舒适偏好。“舒适型”是 vehicle_type，不是本 Facet。
6. energy_type：纯电、燃油、油电混合、插混、增程等。
7. transmission：自动挡、手动挡等。
8. brand：品牌，例如特斯拉、丰田、小米。
9. vehicle_series：用户明确表达的车系层级。
10. vehicle_model：用户希望搜索的最具体车辆产品名或车型，例如 Model Y、小米SU7。
11. custom：确实影响选车但其他 Facet 无法无损表达的诉求。禁止把地点、时间、知识问答或普通闲聊放入 custom。

更新规则：
1. requirements 只输出本轮相对历史的增量，不能复述未提及的历史条件。
2. 同 Facet 新值默认 operation="replace"；只有“也看看、都可以、一起看”等明确并集信号使用 add。
3. “不限、删除、去掉”使用 remove；删除整个 Facet 时 raw_value=""。
4. “不要燃油车”是 operation="add", operator="not_eq"，不是 exclude。
5. hard 表示必须、不要、明确数量/预算或没有弱化词的直接条件；soft 只用于最好、优先、尽量、其他也可以。
6. confidence 是提取置信度，不是用户诉求强弱。
7. entity_context 对所有 Requirement 都必须出现；非车辆实体时两个值均为空字符串。
8. “特斯拉 Model Y”只输出 vehicle_model=Model Y，brand_hint=特斯拉；不能同时输出 brand=特斯拉，以免父品牌扩大结果。
9. “特斯拉的SUV都可以”输出 brand=特斯拉和 vehicle_type=SUV，因为它们是独立维度。
10. 只做不扩充语义的文字整理。车辆名称最终由服务端实体目录归一，不能编造标准名。
11. “带老人和孩子”不是车辆条件；不能推断 SUV、七座或舒适性。
12. 地点、取还时间、供应商、保险、订单操作不属于车辆 Requirement。
13. “直接搜、都行、换一批、还有别的吗”是搜索控制，不是车辆 Requirement。纯搜索控制必须 domain_matched=false 且 requirements=[]。
14. 禁止输出 id、filter_code、sort_code、group_code、context_id、supplier_code 或其他服务端标识。

示例：
输入“特斯拉 Model Y，必须7座”
输出：
{"requirements":[{"facet":"vehicle_model","raw_text":"特斯拉 Model Y","raw_value":"Model Y","operation":"replace","operator":"eq","importance":"hard","confidence":0.99,"entity_context":{"brand_hint":"特斯拉","series_hint":""}},{"facet":"seat_num","raw_text":"必须7座","raw_value":"7","operation":"replace","operator":"eq","importance":"hard","confidence":0.99,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}

输入“最好自动挡，价格便宜点”
输出：
{"requirements":[{"facet":"transmission","raw_text":"最好自动挡","raw_value":"自动挡","operation":"replace","operator":"eq","importance":"soft","confidence":0.99,"entity_context":{"brand_hint":"","series_hint":""}},{"facet":"price_preference","raw_text":"价格便宜点","raw_value":"lower","operation":"replace","operator":"eq","importance":"soft","confidence":0.98,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}

输入“品牌不限，直接搜”
输出中只包含 brand 的 remove 操作；“直接搜”不产生 Requirement。

输入“明天虹桥取，换一批”
输出：
{"requirements":[],"domain_matched":false}`
