package agent

import (
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/types"
)

const (
	SearchModeInitial          = "initial"
	SearchModeRefine           = "refine"
	SearchModePage             = "page"
	SearchModeNegativeFeedback = "negative_feedback"
	SearchModeBudgetDown       = "budget_down"
	SearchModeBudgetUp         = "budget_up"
	SearchModeRelax            = "relax"
)

const (
	ConfidenceActionSearch        = "search"
	ConfidenceActionInterpret     = "interpret"
	ConfidenceActionAsk           = "ask"
	ConfidenceActionRelaxedSearch = "relaxed_search"
	ConfidenceActionFallback      = "fallback"
)

type ConfidenceDecision struct {
	Action          string
	Reason          string
	NormalizedDelta []types.NeedDelta
}

type IterationPlan struct {
	SearchMode   string
	FilterCodes  []string
	Page         int
	PageSize     int
	ExcludedRefs []string
	RelaxLevel   int
}

func normalizeSearchMode(mode string) string {
	switch mode {
	case SearchModeInitial, SearchModeRefine, SearchModePage, SearchModeNegativeFeedback, SearchModeBudgetDown, SearchModeBudgetUp, SearchModeRelax:
		return mode
	default:
		return SearchModeRefine
	}
}

// ApplyConfidenceGate turns confidence signals into a deterministic search action.
func ApplyConfidenceGate(dec *Decision, uncovered []types.UserNeed) ConfidenceDecision {
	if dec == nil {
		return ConfidenceDecision{Action: ConfidenceActionFallback, Reason: "missing decision"}
	}
	normalized := cloneNeedDelta(dec.NeedDelta)
	downgraded := false
	for i := range normalized {
		if normalized[i].Hardness == "hard" && normalized[i].Confidence > 0 && normalized[i].Confidence < 0.6 {
			normalized[i].Hardness = "soft"
			downgraded = true
		}
	}

	if dec.Understanding != nil {
		s := dec.Understanding.Sufficiency
		if s > 0 && s < 0.4 && !dec.StrongSearchIntent {
			return ConfidenceDecision{Action: ConfidenceActionAsk, Reason: "low sufficiency", NormalizedDelta: normalized}
		}
		if s >= 0.4 && s < 0.7 {
			return ConfidenceDecision{Action: ConfidenceActionInterpret, Reason: "gray sufficiency", NormalizedDelta: normalized}
		}
	}

	if len(uncovered) > 0 && !dec.StrongSearchIntent {
		return ConfidenceDecision{Action: ConfidenceActionRelaxedSearch, Reason: "uncovered needs", NormalizedDelta: normalized}
	}
	if downgraded {
		return ConfidenceDecision{Action: ConfidenceActionSearch, Reason: "downgraded low-confidence hard needs", NormalizedDelta: normalized}
	}
	return ConfidenceDecision{Action: ConfidenceActionSearch, Reason: "high confidence", NormalizedDelta: normalized}
}

func cloneNeedDelta(in []types.NeedDelta) []types.NeedDelta {
	if len(in) == 0 {
		return nil
	}
	out := make([]types.NeedDelta, len(in))
	copy(out, in)
	return out
}

func ApplyIterationPolicy(dec *Decision, st *orchestration.ConversationState, filterCodes []string) IterationPlan {
	mode := SearchModeRefine
	if dec != nil {
		mode = normalizeSearchMode(dec.SearchMode)
	}
	plan := IterationPlan{
		SearchMode:  mode,
		FilterCodes: cloneStrings(filterCodes),
		Page:        1,
		PageSize:    6,
	}
	if st != nil && st.LastSearch != nil && st.LastSearch.PageSize > 0 {
		plan.PageSize = st.LastSearch.PageSize
	}

	if mode == SearchModePage && st != nil && st.LastSearch != nil {
		plan.FilterCodes = cloneStrings(st.LastSearch.FilterCodes)
		plan.Page = st.LastSearch.Page + 1
		if plan.Page <= 1 {
			plan.Page = 2
		}
		plan.ExcludedRefs = appendUnique(cloneStrings(st.LastSearch.ExcludedRefs), st.LastSearch.ShownRefs...)
		plan.RelaxLevel = st.LastSearch.RelaxLevel
		return plan
	}

	if mode == SearchModeRelax && st != nil && st.LastSearch != nil {
		plan.RelaxLevel = st.LastSearch.RelaxLevel + 1
	}
	if mode == SearchModeNegativeFeedback && st != nil && dec != nil && dec.FeedbackRef != "" {
		if ref, clarify := ResolveQuoteRef(st, dec.FeedbackRef); ref != "" && clarify == nil {
			plan.ExcludedRefs = append(plan.ExcludedRefs, ref)
		}
		if st.LastSearch != nil {
			plan.ExcludedRefs = appendUnique(plan.ExcludedRefs, st.LastSearch.ExcludedRefs...)
		}
	}
	return plan
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func appendUnique(base []string, values ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(values))
	out := make([]string, 0, len(base)+len(values))
	for _, v := range append(base, values...) {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
