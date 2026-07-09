package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zxq97/rental-agent/internal/llm"
)

// decide 层 7 个工具名。
const (
	ToolSearchVehicles = "search_vehicles"
	ToolAsk            = "ask"
	ToolGetPriceDetail = "get_price_detail"
	ToolInsurance      = "insurance"
	ToolCompare        = "compare_vehicles"
	ToolInterpretRules = "interpret_rules"
	ToolUpdateRental   = "update_rental"
)

// decideTools 返回提供给模型的 6 个工具 schema。
// 关键:**均不含 context_id / reference_id / supplier 等 ID 字段** —— ID 由 Go 从 state 注入。
func decideTools() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        ToolSearchVehicles,
				Description: "用户在挑车/找车/报价/换筛选条件且信息足够时调用,检索车型报价。你只产出需求(need_delta),不输出 filter_codes——筛选码由后续步骤据 needs 自动生成。",
				Parameters: json.RawMessage(`{
  "type":"object",
  "properties":{
    "search_mode":{
      "type":"string",
      "enum":["initial","refine","page","negative_feedback","budget_down","budget_up","relax"],
      "description":"本轮搜车迭代模式: 首搜/条件细化/换一批/不喜欢或排除/预算下调/预算上调/放宽条件"
    },
    "pickup_text":{
      "type":"string",
      "description":"用户本轮或历史里说过的取车地点自然语言(如 '首都机场T3'/'北京西站'/'朝阳区望京')。**只有当用户明确给出取车地点时才填**,否则一定要留空并改调 ask 追问取车地点;禁止把用户的车型/需求/闲聊文字塞到这里。"
    },
	    "dropoff_text":{
	      "type":"string",
	      "description":"用户明确给出的还车地点自然语言;未说则留空,与取车相同也可留空(系统默认同点还车)。"
	    },
	    "pickup_time":{
	      "type":"string",
	      "description":"用户明确给出的取车时间,必须是 YYYY-MM-DD HH:MM:SS 或 YYYY-MM-DD HH:MM;没说就留空,禁止编默认时间"
	    },
	    "dropoff_time":{
	      "type":"string",
	      "description":"用户明确给出的还车时间,必须是 YYYY-MM-DD HH:MM:SS 或 YYYY-MM-DD HH:MM;没说就留空,禁止编默认时间"
	    },
	    "feedback_ref":{
      "type":"string",
      "description":"用户反馈指向的自然语言对象,如 第一辆/比亚迪/SUV。禁止输出任何 context_id/reference_id/supplier"
    },
    "need_delta":{
      "type":"array",
      "items":{
        "type":"object",
        "properties":{
          "op":{"type":"string","enum":["ADD","UPDATE","DELETE","NEGATE","DECAY","REINFORCE"],"description":"操作类型"},
          "type":{"type":"string","description":"需求维度: vehicle_type/energy_type/seat_num/brand/price_preference/transmission/scene/car_age/comfort_preference/vehicle_model/vehicle_series/luggage/license/service"},
          "value":{"description":"需求值,自然语言原文(如 SUV/纯电/7座/200左右)"},
          "hardness":{"type":"string","enum":["hard","soft"],"description":"hard=用户明确表达 soft=场景推理"},
          "confidence":{"type":"number","description":"0~1 置信度"}
        },
        "required":["op","type"]
      },
      "description":"需求变更增量。多维输入(如'豪华型电车')必须为每个维度各产一条。翻页时留空。乘车人数归 seat_num。"
    },
    "strong_search_intent":{
      "type":"boolean",
      "description":"用户'直接推/别问了/有啥推啥/不挑都行'或要直接结论时 true,跳过自评直接推车"
    },
	    "understanding":{
	      "type":"object",
	      "properties":{
	        "sufficiency":{"type":"number","description":"0~1 自评信息够不够推好车。>=0.6 直接推,<0.6 应改调 ask"},
	        "covered_dims":{"type":"array","items":{"type":"string"},"description":"已掌握的维度"},
	        "rationale":{"type":"string","description":"一句话自评依据"}
	      }
	    },
	    "profile_patch":{
	      "type":"object",
	      "properties":{
	        "trip_scene":{"type":"string","description":"用户出行场景,如 家庭出游/商务接送/情侣自驾"},
	        "companions":{"type":"string","description":"同行人群,如 老人小孩/客户/朋友"},
	        "price_sensitivity":{"type":"string","description":"价格敏感度: high/medium/low"},
	        "style_preference":{"type":"string","description":"风格偏好,如 空间舒适/经济实惠/车新"}
	      },
	      "description":"从用户明确表达或高置信场景中提取的轻量画像补丁;不要编造"
	    }
	  }
	}`),
			},
		},
		{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        ToolAsk,
				Description: "信息明显不足、直接推会跑偏时调用,追问一个关键维度,必须带 2-4 个引导选项。",
				Parameters: json.RawMessage(`{
  "type":"object",
  "properties":{
    "question":{"type":"string","description":"一句话追问,只问一个维度,结合用户场景自然地问"},
    "options":{"type":"array","items":{"type":"string"},"description":"2-4 个真实槽位值选项"},
    "slot":{"type":"string","description":"追问的维度,如 seat_num/vehicle_type/price_preference"}
  },
  "required":["question","options"]
}`),
			},
		},
		{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        ToolGetPriceDetail,
				Description: "用户要看某辆车的费用明细/价格构成时调用。",
				Parameters: json.RawMessage(`{
  "type":"object",
  "properties":{
    "vehicle_ref":{"type":"string","description":"用户怎么指代这辆车的原话,如 第一辆/朗逸/那辆SUV"}
  },
  "required":["vehicle_ref"]
}`),
			},
		},
		{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        ToolInsurance,
				Description: "用户问保险/保障/全险/要不要加保时调用。",
				Parameters: json.RawMessage(`{
  "type":"object",
	"properties":{
	"vehicle_ref":{"type":"string","description":"用户指代哪辆车的原话,可空(空则用已选定的)"},
	"question":{"type":"string","description":"用户的保险问题原文"},
	"driver_age":{"type":"integer","description":"用户明确说出的驾龄年数;未知则不要填,Capability 会追问"}
  }
}`),
			},
		},
		{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        ToolCompare,
				Description: "用户纠结选哪辆、发起车型对比时调用(如 A和B哪个好/选朗逸还是轩逸)。",
				Parameters: json.RawMessage(`{
  "type":"object",
  "properties":{
    "vehicle_refs":{"type":"array","items":{"type":"string"},"description":"2-3 个用户原话指代,如 [\"朗逸\",\"轩逸\"]"}
  },
  "required":["vehicle_refs"]
}`),
			},
		},
		{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        ToolInterpretRules,
				Description: "用户问租车规则/政策/流程(异地还车费、怎么取车、驾照证件要求、违章车损、免押条件等)时调用。",
				Parameters: json.RawMessage(`{
  "type":"object",
  "properties":{
    "rule_query":{"type":"string","description":"用户的规则问题原文"}
  },
  "required":["rule_query"]
}`),
			},
		},
		{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        ToolUpdateRental,
				Description: "用户明确要修改取/还车地点或时间时调用(如'改成机场取车'/'还车推到后天'/'我想异地还车到浦东')。不用于首次确认地点(首次走 search_vehicles 的 pickup_text 或 ask)。",
				Parameters: json.RawMessage(`{
  "type":"object",
  "properties":{
    "pickup_text":{"type":"string","description":"新取车地点自然语言,如'首都机场T3';不改则留空"},
    "dropoff_text":{"type":"string","description":"新还车地点自然语言;不改则留空;与取车同点也留空"},
    "pickup_time":{"type":"string","description":"新取车时间 YYYY-MM-DD HH:MM:SS(秒可省);不改留空"},
    "dropoff_time":{"type":"string","description":"新还车时间;不改留空"},
    "clear_dropoff":{"type":"boolean","description":"用户明说要恢复同点还车时设 true"}
  }
}`),
			},
		},
	}
}

// parseDecisionArgs 把工具调用的 arguments JSON 解析成 map。失败返回空 map。
func parseDecisionArgs(argsJSON string) map[string]any {
	m, _ := parseDecisionArgsWithDiagnostics(argsJSON)
	return m
}

func parseDecisionArgsWithDiagnostics(argsJSON string) (map[string]any, *ToolArgsDiagnostics) {
	m := map[string]any{}
	diag := &ToolArgsDiagnostics{Raw: argsJSON}
	if argsJSON == "" {
		return m, diag
	}
	if err := json.Unmarshal([]byte(argsJSON), &m); err == nil {
		return m, diag
	} else {
		diag.ParseError = err.Error()
	}
	if repaired := firstJSONObject(argsJSON); repaired != "" {
		if err := json.Unmarshal([]byte(repaired), &m); err == nil {
			diag.Repaired = true
			diag.RepairedArgs = repaired
			return m, diag
		}
	}
	return m, diag
}

func firstJSONObject(s string) string {
	start := -1
	depth := 0
	inString := false
	escaped := false
	for i, r := range s {
		if start < 0 {
			if r == '{' {
				start = i
				depth = 1
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inString {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func validateToolArgs(tool string, args map[string]any, diag *ToolArgsDiagnostics) {
	if diag == nil {
		return
	}
	switch tool {
	case ToolSearchVehicles:
		diag.ValidationErrors = append(diag.ValidationErrors, validateSearchVehicleArgs(args)...)
	case ToolUpdateRental:
		diag.ValidationErrors = append(diag.ValidationErrors, validateUpdateRentalArgs(args)...)
	}
}

func validateSearchVehicleArgs(args map[string]any) []string {
	var errs []string
	if v, ok := args["search_mode"]; ok {
		s, ok := v.(string)
		if !ok {
			errs = append(errs, "search_mode must be string")
		} else if !isValidSearchMode(s) {
			errs = append(errs, "search_mode invalid")
		}
	}
	errs = append(errs, validateStringField(args, "pickup_text")...)
	errs = append(errs, validateStringField(args, "dropoff_text")...)
	errs = append(errs, validateStringField(args, "pickup_time")...)
	errs = append(errs, validateStringField(args, "dropoff_time")...)
	errs = append(errs, validateStringField(args, "feedback_ref")...)
	if v, ok := args["need_delta"]; ok {
		if _, ok := v.([]any); !ok {
			errs = append(errs, "need_delta must be array")
		}
	}
	if v, ok := args["strong_search_intent"]; ok {
		if _, ok := v.(bool); !ok {
			errs = append(errs, "strong_search_intent must be bool")
		}
	}
	if s, _ := args["pickup_text"].(string); looksLikeVehicleOrFilterNeed(s) {
		errs = append(errs, "pickup_text looks like vehicle/filter need")
	}
	if s, _ := args["dropoff_text"].(string); looksLikeVehicleOrFilterNeed(s) {
		errs = append(errs, "dropoff_text looks like vehicle/filter need")
	}
	if s, _ := args["pickup_time"].(string); strings.TrimSpace(s) != "" {
		if _, err := parseRentalTime(s); err != nil {
			errs = append(errs, "pickup_time invalid")
		}
	}
	if s, _ := args["dropoff_time"].(string); strings.TrimSpace(s) != "" {
		if _, err := parseRentalTime(s); err != nil {
			errs = append(errs, "dropoff_time invalid")
		}
	}
	return errs
}

func validateUpdateRentalArgs(args map[string]any) []string {
	var errs []string
	errs = append(errs, validateStringField(args, "pickup_text")...)
	errs = append(errs, validateStringField(args, "dropoff_text")...)
	errs = append(errs, validateStringField(args, "pickup_time")...)
	errs = append(errs, validateStringField(args, "dropoff_time")...)
	if v, ok := args["clear_dropoff"]; ok {
		if _, ok := v.(bool); !ok {
			errs = append(errs, "clear_dropoff must be bool")
		}
	}
	if s, _ := args["pickup_text"].(string); looksLikeVehicleOrFilterNeed(s) {
		errs = append(errs, "pickup_text looks like vehicle/filter need")
	}
	if s, _ := args["dropoff_text"].(string); looksLikeVehicleOrFilterNeed(s) {
		errs = append(errs, "dropoff_text looks like vehicle/filter need")
	}
	if s, _ := args["pickup_time"].(string); strings.TrimSpace(s) != "" {
		if _, err := parseRentalTime(s); err != nil {
			errs = append(errs, "pickup_time invalid")
		}
	}
	if s, _ := args["dropoff_time"].(string); strings.TrimSpace(s) != "" {
		if _, err := parseRentalTime(s); err != nil {
			errs = append(errs, "dropoff_time invalid")
		}
	}
	return errs
}

func validateStringField(args map[string]any, field string) []string {
	if v, ok := args[field]; ok {
		if _, ok := v.(string); !ok {
			return []string{fmt.Sprintf("%s must be string", field)}
		}
	}
	return nil
}

func isValidSearchMode(mode string) bool {
	switch normalizeSearchMode(mode) {
	case SearchModeInitial, SearchModeRefine, SearchModePage, SearchModeNegativeFeedback, SearchModeBudgetDown, SearchModeBudgetUp, SearchModeRelax:
		return mode == normalizeSearchMode(mode)
	default:
		return false
	}
}

func looksLikeVehicleOrFilterNeed(s string) bool {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return false
	}
	needWords := []string{
		"suv", "mpv", "轿车", "商务车", "经济型", "舒适型", "豪华型", "跑车",
		"电车", "新能源", "纯电", "油车", "汽油", "混动", "自动挡", "手动挡",
		"便宜", "贵点", "预算", "空间", "车新", "几座", "座",
	}
	for _, word := range needWords {
		if v == strings.ToLower(word) {
			return true
		}
	}
	return false
}
