package searchplan

import (
	"sort"
	"strings"
)

type relaxationCandidate struct {
	requirementID string
	cost          int
}

// FirstRelaxedAlternative removes one deterministic hard constraint after a
// strict search returns no usable quote. The removed constraint remains a
// mandatory disclosure and is never presented as satisfied.
func FirstRelaxedAlternative(plan FilterPlan) (FilterPlan, bool) {
	hard := make(map[string]struct{})
	for _, resolution := range plan.Resolutions {
		if resolution.Importance == "hard" && resolution.Capability == CapabilityFilterable {
			hard[resolution.RequirementID] = struct{}{}
		}
	}
	var candidates []relaxationCandidate
	seen := make(map[string]struct{})
	for _, filter := range plan.MenuFilters {
		if _, exists := hard[filter.RequirementID]; !exists {
			continue
		}
		if _, exists := seen[filter.RequirementID]; exists {
			continue
		}
		seen[filter.RequirementID] = struct{}{}
		candidates = append(candidates, relaxationCandidate{
			requirementID: filter.RequirementID,
			cost:          relaxationCost(filter.Code),
		})
	}
	if len(candidates) == 0 {
		return plan, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].cost == candidates[j].cost {
			return candidates[i].requirementID < candidates[j].requirementID
		}
		return candidates[i].cost < candidates[j].cost
	})
	return RelaxRequirement(plan, candidates[0].requirementID)
}

// RelaxRequirement reconstructs a previously selected alternative for
// continuation requests.
func RelaxRequirement(plan FilterPlan, requirementID string) (FilterPlan, bool) {
	if strings.TrimSpace(requirementID) == "" {
		return plan, false
	}
	changed := false
	menuFilters := make([]MenuFilter, 0, len(plan.MenuFilters))
	for _, filter := range plan.MenuFilters {
		if filter.RequirementID == requirementID {
			changed = true
			continue
		}
		menuFilters = append(menuFilters, filter)
	}
	if !changed {
		return plan, false
	}
	plan.MenuFilters = menuFilters
	verifiers := make([]LocalVerifier, 0, len(plan.LocalVerifiers))
	for _, verifier := range plan.LocalVerifiers {
		if verifier.RequirementID != requirementID {
			verifiers = append(verifiers, verifier)
		}
	}
	plan.LocalVerifiers = verifiers
	for index := range plan.Resolutions {
		if plan.Resolutions[index].RequirementID != requirementID {
			continue
		}
		plan.Resolutions[index].Capability = CapabilityAdvisory
		plan.Resolutions[index].Status = "relaxed"
		plan.Resolutions[index].ReasonCode = "hard_requirement_relaxed_after_no_results"
		plan.Resolutions[index].Reason = "严格搜索无结果后，仅为提供替代候选而移除此条件"
		disclosure := Disclosure{
			RequirementID: requirementID,
			RawText:       plan.Resolutions[index].RawText,
			Kind:          DisclosureHardRelaxed,
			Message:       "严格按“" + strings.TrimSpace(plan.Resolutions[index].RawText) + "”搜索没有可用结果；以下是移除该条件后的替代候选，不代表该条件已满足。",
			MustMention:   true,
		}
		plan.Disclosures = AddDisclosure(plan.Disclosures, disclosure)
		break
	}
	plan.RelaxedRequirementIDs = append(plan.RelaxedRequirementIDs, requirementID)
	plan.PlanHash = planHash(plan)
	return plan, true
}

func relaxationCost(code string) int {
	switch {
	case strings.HasPrefix(code, "filter/total_fee/"), strings.HasPrefix(code, "filter/price/"):
		return 10
	case strings.HasPrefix(code, "filter/seat_num/"):
		return 20
	case strings.HasPrefix(code, "filter/car_age/"):
		return 30
	case strings.HasPrefix(code, "filter/vehcle_choice/"):
		return 40
	case strings.HasPrefix(code, "filter/brand/"), strings.HasPrefix(code, "filter/vehicle_name/"), strings.HasPrefix(code, "filter/model/"):
		return 50
	default:
		return 60
	}
}
