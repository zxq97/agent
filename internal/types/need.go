package types

// NeedState Need 的状态，从 Confidence 区间派生，不持久化。
type NeedState string

const (
	NeedActiveHard NeedState = "active_hard" // conf > 0.8
	NeedActiveSoft NeedState = "active_soft" // 0.5 < conf <= 0.8
	NeedDecaying   NeedState = "decaying"    // 0.3 < conf <= 0.5
	NeedDormant    NeedState = "dormant"     // conf <= 0.3
	NeedNegated    NeedState = "negated"     // negative=true
)

// UserNeed 用户单条需求。由 LLM 通过 need_delta 增量操作产出,Go 侧管理生命周期。
// 参考 tyche common/dto/agent/internal.go。
type UserNeed struct {
	Type           string      `json:"type"`                      // vehicle_type/energy_type/seat_num/brand/price_preference/transmission/scene/car_age/comfort_preference/luggage/vehicle_model/vehicle_series/service/license
	Value          interface{} `json:"value"`                     // 需求值,类型随 Type 变化
	Source         string      `json:"source"`                    // user_explicit / model_inferred
	Hardness       string      `json:"hardness"`                  // hard / soft
	Confidence     float64     `json:"confidence"`                // 0~1
	Negative       bool        `json:"negative,omitempty"`        // true 表示排除条件
	BornTurn       int         `json:"born_turn,omitempty"`       // 需求首次出现的轮次
	LastReinforced int         `json:"last_reinforced,omitempty"` // 最近一次被强化的轮次
}

// State 从 Confidence 区间派生当前状态,不存储。
func (n *UserNeed) State() NeedState {
	if n.Negative {
		return NeedNegated
	}
	if n.Confidence > 0.8 {
		return NeedActiveHard
	}
	if n.Confidence > 0.5 {
		return NeedActiveSoft
	}
	if n.Confidence > 0.3 {
		return NeedDecaying
	}
	return NeedDormant
}

// NeedDelta 单条需求变更操作(LLM 输出)。
type NeedDelta struct {
	Op         string      `json:"op"`                   // ADD / UPDATE / NEGATE / DECAY / DELETE / REINFORCE
	Type       string      `json:"type"`                 // 需求类型
	Value      interface{} `json:"value,omitempty"`      // 需求值
	Hardness   string      `json:"hardness,omitempty"`   // hard / soft
	Confidence float64     `json:"confidence,omitempty"` // 0~1
	Factor     float64     `json:"factor,omitempty"`     // DECAY 时的衰减因子
}

// SearchConstraints 会话级结构化用户约束。
type SearchConstraints struct {
	Hard     []UserNeed `json:"hard"`     // 用户明确表达的硬条件
	Soft     []UserNeed `json:"soft"`     // 偏好/推荐类软条件
	Negative []UserNeed `json:"negative"` // 排除条件(negative=true)
}

// LastSearchState 上次搜索的完整状态。
type LastSearchState struct {
	SearchMode     string   `json:"search_mode,omitempty"`
	FilterCodes    []string `json:"filter_codes"`
	Page           int      `json:"page"`
	PageSize       int      `json:"page_size"`
	HasMore        bool     `json:"has_more"`
	ShownRefs      []string `json:"shown_refs,omitempty"`
	ExcludedRefs   []string `json:"excluded_refs,omitempty"`
	ExcludedModels []string `json:"excluded_models,omitempty"`
	MinPrice       float64  `json:"min_price,omitempty"`
	MaxPrice       float64  `json:"max_price,omitempty"`
	RelaxLevel     int      `json:"relax_level,omitempty"`
}

// MenuGroupView 给内部消费的精简 menu_group 视图。
type MenuGroupView struct {
	GroupCode string         `json:"group_code"`
	GroupName string         `json:"group_name"`
	Items     []MenuItemView `json:"items"`
}

// MenuItemView 单个筛选项。
type MenuItemView struct {
	ItemCode string `json:"item_code"`
	Name     string `json:"name"`
}
