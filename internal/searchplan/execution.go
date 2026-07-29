package searchplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/capability"
)

type SearchExecutionPlan struct {
	RequirementVersion int64
	CapabilityVersion  string
	RuntimeFingerprint string

	RemoteFilters []capability.Execution
	RemoteSorts   []capability.Execution
	LocalFilters  []capability.Execution
	LocalRanks    []capability.Execution
	Resolutions   []capability.Resolution
	Unresolved    []capability.Resolution

	FilterPlan FilterPlan
	PlanHash   string
}

type ExecutionCompiler struct {
	legacy   *Compiler
	resolver capability.Resolver
}

func NewExecutionCompiler(legacy *Compiler, resolver capability.Resolver) *ExecutionCompiler {
	if legacy == nil {
		legacy = NewCompiler()
	}
	if resolver == nil {
		resolver = capability.NewResolver(capability.NewDefaultCatalog(), nil)
	}
	return &ExecutionCompiler{legacy: legacy, resolver: resolver}
}

func (c *ExecutionCompiler) CatalogVersion() string {
	if c == nil || c.resolver == nil {
		return ""
	}
	return c.resolver.CatalogVersion()
}

func (c *ExecutionCompiler) Compile(
	ctx context.Context,
	requirements []Requirement,
	menu []guide.MenuGroup,
	runtime capability.RuntimeContext,
	requirementVersion int64,
) SearchExecutionPlan {
	if runtime.CatalogVersion == "" {
		runtime.CatalogVersion = c.resolver.CatalogVersion()
	}
	var canonical []Requirement
	var open []Requirement
	for _, value := range requirements {
		if value.Facet == "" {
			open = append(open, value)
		} else {
			canonical = append(canonical, value)
		}
	}
	filterPlan := c.legacy.Compile(canonical, menu)
	result := SearchExecutionPlan{
		RequirementVersion: requirementVersion,
		CapabilityVersion:  c.resolver.CatalogVersion(),
		RuntimeFingerprint: RuntimeFingerprint(runtime),
		FilterPlan:         filterPlan,
	}

	for _, resolution := range filterPlan.Resolutions {
		value := resolutionFromLegacy(resolution)
		result.Resolutions = append(result.Resolutions, value)
		appendExecutions(&result, value.Executions)
		if value.Status != capability.ResolutionResolved {
			result.Unresolved = append(result.Unresolved, value)
		}
	}
	for _, value := range open {
		resolution := c.resolver.Resolve(ctx, capability.Requirement{
			ID:            value.ID,
			RawText:       value.RawText,
			SemanticLabel: value.SemanticLabel,
			Category:      value.Category,
			Value:         value.Value,
			Operator:      value.Operator,
			Importance:    value.Importance,
		}, runtime)
		result.Resolutions = append(result.Resolutions, resolution)
		appendExecutions(&result, resolution.Executions)
		appendFilterPlanExecutions(&result.FilterPlan, resolution.Executions)
		decorateExploratoryRanks(&result.FilterPlan, resolution)
		if resolution.Status != capability.ResolutionResolved {
			result.Unresolved = append(result.Unresolved, resolution)
		}
		result.FilterPlan.Resolutions = append(result.FilterPlan.Resolutions, legacyResolution(resolution))
	}
	result.FilterPlan.Disclosures = DisclosuresFromResolutions(result.FilterPlan.Resolutions)
	for _, disclosure := range ExploratoryDisclosures(result.FilterPlan.ExploratoryRanks) {
		result.FilterPlan.Disclosures = AddDisclosure(result.FilterPlan.Disclosures, disclosure)
	}
	appendRankOnlyHardDisclosures(&result.FilterPlan, result.Resolutions)
	result.FilterPlan.CapabilityVersion = result.CapabilityVersion
	result.FilterPlan.RuntimeFingerprint = result.RuntimeFingerprint
	result.PlanHash = executionPlanHash(result)
	result.FilterPlan.PlanHash = result.PlanHash
	return result
}

func appendRankOnlyHardDisclosures(plan *FilterPlan, values []capability.Resolution) {
	exploratory := make(map[string]struct{}, len(plan.ExploratoryRanks))
	for _, rank := range plan.ExploratoryRanks {
		exploratory[rank.RequirementID] = struct{}{}
	}
	for _, value := range values {
		if value.Importance != "hard" || value.Status != capability.ResolutionPartiallyResolved {
			continue
		}
		if _, exists := exploratory[value.RequirementID]; exists {
			continue
		}
		plan.Disclosures = AddDisclosure(plan.Disclosures, Disclosure{
			RequirementID: value.RequirementID,
			RawText:       value.RawText,
			Kind:          DisclosureHardUnmapped,
			Message:       "“" + strings.TrimSpace(value.RawText) + "”不能作为严格筛选条件，本次仅用于候选排序，不代表该诉求已满足。",
			MustMention:   true,
		})
	}
}

func decorateExploratoryRanks(plan *FilterPlan, resolution capability.Resolution) {
	for index := range plan.ExploratoryRanks {
		if plan.ExploratoryRanks[index].RequirementID != resolution.RequirementID {
			continue
		}
		plan.ExploratoryRanks[index].RawText = resolution.RawText
		plan.ExploratoryRanks[index].Importance = resolution.Importance
	}
}

func appendFilterPlanExecutions(plan *FilterPlan, values []capability.Execution) {
	for _, value := range values {
		switch {
		case value.Mode == capability.ExecutionLocalRank && value.Operation == "price_low":
			plan.RankFactors = append(plan.RankFactors, RankFactor{
				RequirementID: value.RequirementID,
				Type:          RankPriceLow,
				Weight:        1,
				DataField:     "total_charge.total_amount",
			})
		case value.Mode == capability.ExecutionLocalRank && strings.HasPrefix(value.Operation, "scenario:"):
			plan.ExploratoryRanks = append(plan.ExploratoryRanks, ExploratoryRank{
				RequirementID: value.RequirementID,
				ScenarioID:    strings.TrimPrefix(value.Operation, "scenario:"),
				ModelVersion:  strings.TrimPrefix(value.Operation, "scenario:"),
				Weight:        1,
			})
		}
	}
}

func (p SearchExecutionPlan) FirstBlockingResolution() *capability.Resolution {
	// Capability limitations are reply obligations, not search blockers.
	return nil
}

func resolutionFromLegacy(value Resolution) capability.Resolution {
	result := capability.Resolution{
		RequirementID: value.RequirementID,
		RawText:       value.RawText,
		Importance:    value.Importance,
		MatchMethod:   capability.MatchCanonical,
		ResolvedPart:  value.RawText,
		ReasonCode:    value.ReasonCode,
		Reason:        value.Reason,
		Confidence:    1,
	}
	var execution *capability.Execution
	switch value.Capability {
	case CapabilityFilterable:
		result.Status = capability.ResolutionResolved
		execution = &capability.Execution{RequirementID: value.RequirementID, CapabilityID: value.ReasonCode, Mode: capability.ExecutionRemoteFilter, Confidence: 1, Reason: value.Reason}
	case CapabilityVerifiable:
		result.Status = capability.ResolutionResolved
		execution = &capability.Execution{RequirementID: value.RequirementID, CapabilityID: value.ReasonCode, Mode: capability.ExecutionLocalFilter, Confidence: 1, Reason: value.Reason}
	case CapabilityRankable:
		result.Status = capability.ResolutionResolved
		mode := capability.ExecutionLocalRank
		if value.ReasonCode == "server_sort" {
			mode = capability.ExecutionRemoteSort
		}
		execution = &capability.Execution{RequirementID: value.RequirementID, CapabilityID: value.ReasonCode, Mode: mode, Confidence: 1, Reason: value.Reason}
	case CapabilityAdvisory:
		result.Status = capability.ResolutionPartiallyResolved
	case CapabilityAmbiguous:
		result.Status = capability.ResolutionAmbiguous
	case CapabilityUnverifiable:
		result.Status = capability.ResolutionInsufficientData
	default:
		result.Status = capability.ResolutionUnsupported
	}
	if execution != nil {
		result.Executions = []capability.Execution{*execution}
		result.CapabilityIDs = []string{execution.CapabilityID}
	} else {
		result.ResolvedPart = ""
		result.UnresolvedPart = value.RawText
	}
	return result
}

func legacyResolution(value capability.Resolution) Resolution {
	resolutionCapability := CapabilityUnsupported
	for _, execution := range value.Executions {
		if execution.Mode == capability.ExecutionLocalRank {
			resolutionCapability = CapabilityRankable
		}
	}
	switch value.Status {
	case capability.ResolutionAmbiguous:
		resolutionCapability = CapabilityAmbiguous
	case capability.ResolutionInsufficientData:
		resolutionCapability = CapabilityUnverifiable
	case capability.ResolutionPartiallyResolved:
		if resolutionCapability != CapabilityRankable {
			resolutionCapability = CapabilityAdvisory
		}
	case capability.ResolutionResolved:
		for _, execution := range value.Executions {
			switch execution.Mode {
			case capability.ExecutionRemoteFilter:
				resolutionCapability = CapabilityFilterable
			case capability.ExecutionLocalFilter:
				resolutionCapability = CapabilityVerifiable
			case capability.ExecutionRemoteSort, capability.ExecutionLocalRank:
				resolutionCapability = CapabilityRankable
			}
		}
	}
	return Resolution{
		RequirementID: value.RequirementID,
		RawText:       value.RawText,
		Importance:    value.Importance,
		Capability:    resolutionCapability,
		Status:        string(value.Status),
		ReasonCode:    value.ReasonCode,
		Reason:        value.Reason,
	}
}

func appendExecutions(plan *SearchExecutionPlan, values []capability.Execution) {
	for _, value := range values {
		switch value.Mode {
		case capability.ExecutionRemoteFilter:
			plan.RemoteFilters = append(plan.RemoteFilters, value)
		case capability.ExecutionRemoteSort:
			plan.RemoteSorts = append(plan.RemoteSorts, value)
		case capability.ExecutionLocalFilter:
			plan.LocalFilters = append(plan.LocalFilters, value)
		case capability.ExecutionLocalRank:
			plan.LocalRanks = append(plan.LocalRanks, value)
		}
	}
}

func RuntimeFingerprint(runtime capability.RuntimeContext) string {
	fields := make([]string, 0, len(runtime.ResultFields))
	for field := range runtime.ResultFields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	data, _ := json.Marshal(struct {
		Menu    string
		Fields  []string
		Catalog string
		Rental  string
	}{
		Menu:    runtime.MenuFingerprint,
		Fields:  fields,
		Catalog: runtime.CatalogVersion,
		Rental:  runtime.RentalFingerprint,
	})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func executionPlanHash(value SearchExecutionPlan) string {
	data, _ := json.Marshal(struct {
		RequirementVersion int64
		CapabilityVersion  string
		RuntimeFingerprint string
		RemoteFilters      []capability.Execution
		RemoteSorts        []capability.Execution
		LocalFilters       []capability.Execution
		LocalRanks         []capability.Execution
		Resolutions        []capability.Resolution
		FilterPlan         FilterPlan
	}{
		RequirementVersion: value.RequirementVersion,
		CapabilityVersion:  value.CapabilityVersion,
		RuntimeFingerprint: value.RuntimeFingerprint,
		RemoteFilters:      value.RemoteFilters,
		RemoteSorts:        value.RemoteSorts,
		LocalFilters:       value.LocalFilters,
		LocalRanks:         value.LocalRanks,
		Resolutions:        value.Resolutions,
		FilterPlan:         value.FilterPlan,
	})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
