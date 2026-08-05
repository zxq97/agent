package searchplan

import (
	"strings"
)

// RelaxRequirement applies a requirement relaxation only after an upstream
// user-confirmed alternative plan names the exact requirement to relax.
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
