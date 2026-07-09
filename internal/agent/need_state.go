package agent

import "github.com/zxq97/rental-agent/internal/types"

// 需求生命周期常量,对齐 tyche logic/agent/search/need_state.go。
const (
	DecayPerTurn     = 0.95 // 每轮自然衰减因子
	ConflictDecay    = 0.3  // 冲突衰减因子(保证一次衰减后 ≤ DormantThreshold)
	ReinforceRestore = 0.85 // REINFORCE 恢复的 confidence
	DormantThreshold = 0.3  // ≤ 此阈值视为 Dormant
	ActiveThreshold  = 0.5  // > 此阈值视为 Active
)

// Delta 操作常量。
const (
	DeltaAdd       = "ADD"
	DeltaUpdate    = "UPDATE"
	DeltaNegate    = "NEGATE"
	DeltaDecay     = "DECAY"
	DeltaDelete    = "DELETE"
	DeltaReinforce = "REINFORCE"
)

// ConflictTypes 定义冲突对:ADD/UPDATE 一个类型时,对立类型应衰减。
// 参考 tyche logic/agent/search/need_state.go。
var ConflictTypes = map[string][]string{
	"price_preference": {"brand", "comfort_preference"},
	"brand":            {"price_preference", "seat_num"},
	"vehicle_model":    {"seat_num"},
	"vehicle_series":   {"seat_num"},
	"vehicle_type":     {"vehicle_model", "vehicle_series"},
}

// TickNeeds 每轮自然衰减:所有非 Negative 的 need confidence *= DecayPerTurn。
// Dormant 且超过 5 轮未访问的移除。
func TickNeeds(needs []types.UserNeed, currentTurn int) []types.UserNeed {
	result := make([]types.UserNeed, 0, len(needs))
	for _, n := range needs {
		if n.Negative {
			result = append(result, n)
			continue
		}
		n.Confidence *= DecayPerTurn
		if n.Confidence <= DormantThreshold && (currentTurn-n.LastReinforced) > 5 {
			continue // dead, 移除
		}
		result = append(result, n)
	}
	return result
}

// ApplyDelta 将 delta 操作应用到 needs 列表,返回新列表。
func ApplyDelta(needs []types.UserNeed, deltas []types.NeedDelta, currentTurn int) []types.UserNeed {
	for _, d := range deltas {
		switch normalizeDeltaOp(d.Op) {
		case DeltaAdd:
			needs = applyAdd(needs, d, currentTurn)
		case DeltaUpdate:
			needs = applyUpdate(needs, d, currentTurn)
		case DeltaNegate:
			needs = applyNegate(needs, d)
		case DeltaDecay:
			needs = applyDecayOp(needs, d)
		case DeltaDelete:
			needs = applyDelete(needs, d)
		case DeltaReinforce:
			needs = applyReinforce(needs, d, currentTurn)
		}
	}
	return needs
}

// ApplyConflictDecay 根据新增的 delta,自动衰减冲突需求。
// 本轮 ADD/UPDATE/REINFORCE 明确表达过的 type 不衰减(豁免守卫)。
func ApplyConflictDecay(needs []types.UserNeed, deltas []types.NeedDelta) []types.UserNeed {
	touched := make(map[string]bool, len(deltas))
	for _, d := range deltas {
		switch normalizeDeltaOp(d.Op) {
		case DeltaAdd, DeltaUpdate, DeltaReinforce:
			touched[d.Type] = true
		}
	}
	for _, d := range deltas {
		op := normalizeDeltaOp(d.Op)
		if op != DeltaAdd && op != DeltaUpdate {
			continue
		}
		conflicts, ok := ConflictTypes[d.Type]
		if !ok {
			continue
		}
		for _, ct := range conflicts {
			if touched[ct] {
				continue
			}
			needs = applyDecayOp(needs, types.NeedDelta{Type: ct, Factor: ConflictDecay})
		}
	}
	return needs
}

// FilterActiveNeeds 返回活跃需求(State 非 Dormant)。
func FilterActiveNeeds(needs []types.UserNeed) []types.UserNeed {
	result := make([]types.UserNeed, 0, len(needs))
	for _, n := range needs {
		if n.State() == types.NeedDormant {
			continue
		}
		result = append(result, n)
	}
	return result
}

// BuildNeedsFromConstraints 从 SearchConstraints 还原为 UserNeed 列表。
func BuildNeedsFromConstraints(c types.SearchConstraints) []types.UserNeed {
	needs := make([]types.UserNeed, 0, len(c.Hard)+len(c.Soft)+len(c.Negative))
	needs = append(needs, c.Hard...)
	needs = append(needs, c.Soft...)
	needs = append(needs, c.Negative...)
	return needs
}

// UpdateConstraints 将 needs 按 hardness/negative 分桶写入 SearchConstraints。
func UpdateConstraints(needs []types.UserNeed) types.SearchConstraints {
	var c types.SearchConstraints
	for _, n := range needs {
		switch {
		case n.Negative:
			c.Negative = append(c.Negative, n)
		case n.Hardness == "hard" || n.Confidence > 0.8:
			c.Hard = append(c.Hard, n)
		default:
			c.Soft = append(c.Soft, n)
		}
	}
	return c
}

// --- internal helpers ---

func normalizeDeltaOp(op string) string {
	switch op {
	case "add":
		return DeltaAdd
	case "update":
		return DeltaUpdate
	case "negate":
		return DeltaNegate
	case "decay":
		return DeltaDecay
	case "delete":
		return DeltaDelete
	case "reinforce":
		return DeltaReinforce
	default:
		return op
	}
}

func applyAdd(needs []types.UserNeed, d types.NeedDelta, turn int) []types.UserNeed {
	conf := d.Confidence
	if conf == 0 {
		conf = 0.9
	}
	hardness := d.Hardness
	if hardness == "" {
		hardness = "hard"
	}
	// 已存在同类型 → 覆盖
	for i, n := range needs {
		if n.Type == d.Type && !n.Negative {
			needs[i].Value = d.Value
			needs[i].Confidence = conf
			needs[i].Hardness = hardness
			needs[i].LastReinforced = turn
			needs[i].Negative = false
			return needs
		}
	}
	return append(needs, types.UserNeed{
		Type:           d.Type,
		Value:          d.Value,
		Source:         "user_explicit",
		Hardness:       hardness,
		Confidence:     conf,
		BornTurn:       turn,
		LastReinforced: turn,
	})
}

func applyUpdate(needs []types.UserNeed, d types.NeedDelta, turn int) []types.UserNeed {
	for i, n := range needs {
		if n.Type == d.Type && !n.Negative {
			needs[i].Value = d.Value
			if d.Confidence > 0 {
				needs[i].Confidence = d.Confidence
			}
			if d.Hardness != "" {
				needs[i].Hardness = d.Hardness
			}
			needs[i].LastReinforced = turn
			return needs
		}
	}
	return applyAdd(needs, d, turn)
}

func applyNegate(needs []types.UserNeed, d types.NeedDelta) []types.UserNeed {
	for i, n := range needs {
		if n.Type == d.Type {
			needs[i].Negative = true
			needs[i].Value = d.Value
			return needs
		}
	}
	return append(needs, types.UserNeed{
		Type:     d.Type,
		Value:    d.Value,
		Source:   "user_explicit",
		Hardness: "hard",
		Negative: true,
	})
}

func applyDecayOp(needs []types.UserNeed, d types.NeedDelta) []types.UserNeed {
	factor := d.Factor
	if factor == 0 {
		factor = ConflictDecay
	}
	for i, n := range needs {
		if n.Type == d.Type && !n.Negative {
			needs[i].Confidence *= factor
			if needs[i].Confidence > ActiveThreshold {
				needs[i].Hardness = "soft"
			}
			return needs
		}
	}
	return needs
}

func applyDelete(needs []types.UserNeed, d types.NeedDelta) []types.UserNeed {
	result := needs[:0]
	for _, n := range needs {
		if n.Type != d.Type {
			result = append(result, n)
		}
	}
	return result
}

func applyReinforce(needs []types.UserNeed, d types.NeedDelta, turn int) []types.UserNeed {
	for i, n := range needs {
		if n.Type == d.Type && !n.Negative {
			needs[i].Confidence = ReinforceRestore
			needs[i].Hardness = "hard"
			needs[i].LastReinforced = turn
			return needs
		}
	}
	return needs
}
