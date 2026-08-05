package capability

import (
	"context"
	"strings"

	"github.com/pkg/errors"
)

type DefaultResolver struct {
	catalog *Catalog
	matcher Matcher
}

func NewResolver(catalog *Catalog, matcher Matcher) (*DefaultResolver, error) {
	if catalog == nil {
		return nil, errors.New("capability resolver: catalog is required")
	}
	if matcher == nil {
		return nil, errors.New("capability resolver: matcher is required")
	}
	return &DefaultResolver{catalog: catalog, matcher: matcher}, nil
}

func (r *DefaultResolver) CatalogVersion() string {
	return r.catalog.Version()
}

func (r *DefaultResolver) Resolve(ctx context.Context, value Requirement, runtime RuntimeContext) Resolution {
	base := Resolution{
		RequirementID:  value.ID,
		RawText:        value.RawText,
		Importance:     value.Importance,
		UnresolvedPart: value.RawText,
	}
	if runtime.CatalogVersion != "" && runtime.CatalogVersion != r.CatalogVersion() {
		base.Status = ResolutionInsufficientData
		base.ReasonCode = "catalog_version_mismatch"
		base.Reason = "本轮运行时能力目录版本与解析器版本不一致"
		return base
	}
	if value.CanonicalType != "" {
		base.Status = ResolutionResolved
		base.MatchMethod = MatchCanonical
		base.ResolvedPart = value.RawText
		base.UnresolvedPart = ""
		base.ReasonCode = "canonical_requirement"
		base.Reason = "标准需求交由确定性执行计划编译"
		base.Confidence = 1
		return base
	}
	candidates := r.catalog.Candidates(value, 10)
	if len(candidates) == 0 {
		base.Status = ResolutionUnsupported
		base.ReasonCode = "capability_candidate_not_found"
		base.Reason = "能力目录中没有相关候选"
		return base
	}

	definition, method, relation, confidence, ok := r.selectCandidate(ctx, value, candidates)
	if !ok {
		base.Status = ResolutionAmbiguous
		base.ReasonCode = "capability_match_ambiguous"
		base.Reason = "存在多个相关能力，但无法确定唯一可执行语义"
		return base
	}
	base.MatchMethod = method
	base.CapabilityIDs = []string{definition.ID}
	base.Confidence = confidence
	if relation == "relevant" {
		base.Status = ResolutionInsufficientData
		base.ReasonCode = "semantic_relation_not_executable"
		base.Reason = "语义相关性不能证明车辆满足该诉求"
		return base
	}

	execution, status, reasonCode, reason := executionFor(definition, value, runtime)
	base.Status = status
	base.ReasonCode = reasonCode
	base.Reason = reason
	if execution != nil {
		execution.RequirementID = value.ID
		execution.CapabilityID = definition.ID
		execution.Confidence = confidence
		base.Executions = []Execution{*execution}
		base.ResolvedPart = value.RawText
		base.UnresolvedPart = ""
	}
	return base
}

func (r *DefaultResolver) selectCandidate(ctx context.Context, value Requirement, candidates []Definition) (Definition, MatchMethod, string, float64, bool) {
	var exact []Definition
	for _, definition := range candidates {
		if definitionMatches(definition, value) {
			exact = append(exact, definition)
		}
	}
	if len(exact) == 1 {
		return exact[0], MatchAlias, "exact", 1, true
	}
	if len(candidates) < 2 {
		return Definition{}, "", "", 0, false
	}
	request := &MatchRequest{Requirement: value, Candidates: make([]MatchCandidate, 0, len(candidates))}
	allowed := make(map[string]Definition, len(candidates))
	for _, candidate := range candidates {
		allowed[candidate.ID] = candidate
		request.Candidates = append(request.Candidates, MatchCandidate{
			ID:             candidate.ID,
			Name:           candidate.Name,
			Description:    candidate.Description,
			SupportedModes: supportedModes(candidate),
		})
	}
	matches, err := r.matcher.Match(ctx, request)
	if err != nil || len(matches) != 1 {
		return Definition{}, "", "", 0, false
	}
	match := matches[0]
	definition, exists := allowed[match.CapabilityID]
	if !exists || (match.Relation != "exact" && match.Relation != "relevant") ||
		match.Confidence < 0 || match.Confidence > 1 {
		return Definition{}, "", "", 0, false
	}
	return definition, MatchLLM, match.Relation, match.Confidence, true
}

func executionFor(definition Definition, value Requirement, runtime RuntimeContext) (*Execution, ResolutionStatus, string, string) {
	if value.Importance == "hard" {
		if executable(ExecutionRemoteFilter, definition.RemoteFilter) && menusAvailable(runtime.MenuCodes, definition.RemoteFilter.RequiredMenus) {
			return execution(ExecutionRemoteFilter, definition.RemoteFilter), ResolutionResolved, "remote_filter", "使用已验证的远程筛选能力"
		}
		if executable(ExecutionLocalFilter, definition.LocalFilter) && fieldsAvailable(runtime.ResultFields, definition.LocalFilter.RequiredFields) {
			return execution(ExecutionLocalFilter, definition.LocalFilter), ResolutionResolved, "local_filter", "使用真实返回字段执行本地严格过滤"
		}
		if executable(ExecutionLocalRank, definition.LocalRank) && fieldsAvailable(runtime.ResultFields, definition.LocalRank.RequiredFields) {
			return execution(ExecutionLocalRank, definition.LocalRank), ResolutionPartiallyResolved, "hard_requirement_rank_only", "该硬诉求不能被严格证明，仅使用可用事实对候选进行探索性排序"
		}
		if hasAnyExecution(definition) {
			return nil, ResolutionUnsupported, "hard_requirement_not_filterable", "硬需求只有排序或缺少严格过滤能力"
		}
		return nil, ResolutionInsufficientData, "hard_requirement_data_missing", "缺少验证该硬需求所需的车辆字段"
	}
	if executable(ExecutionRemoteSort, definition.RemoteSort) && menusAvailable(runtime.MenuCodes, definition.RemoteSort.RequiredMenus) {
		return execution(ExecutionRemoteSort, definition.RemoteSort), ResolutionResolved, "remote_sort", "使用已验证的远程排序能力"
	}
	if executable(ExecutionLocalRank, definition.LocalRank) && fieldsAvailable(runtime.ResultFields, definition.LocalRank.RequiredFields) {
		return execution(ExecutionLocalRank, definition.LocalRank), ResolutionResolved, "local_rank", "使用真实返回字段在当前候选集中排序"
	}
	if hasAnyExecution(definition) {
		return nil, ResolutionInsufficientData, "soft_requirement_data_missing", "当前结果字段不足以执行该偏好"
	}
	return nil, ResolutionInsufficientData, "scenario_model_unavailable", "当前没有足够事实判断该场景需求"
}

func execution(mode ExecutionMode, definition *ExecutionDefinition) *Execution {
	return &Execution{
		Mode: mode, RequiredFields: append([]string(nil), definition.RequiredFields...),
		RequiredMenus: append([]string(nil), definition.RequiredMenus...),
		Operation:     definition.Operation, Value: definition.Value,
	}
}

func executable(mode ExecutionMode, definition *ExecutionDefinition) bool {
	if definition == nil {
		return false
	}
	switch mode {
	case ExecutionLocalRank:
		if definition.Operation == "price_low" {
			return containsString(definition.RequiredFields, "total_charge.total_amount")
		}
		switch strings.TrimPrefix(definition.Operation, "scenario:") {
		case "elderly_friendly_v1", "family_trip_v1", "large_space_v1",
			"long_distance_v1", "large_luggage_v1":
			return strings.HasPrefix(definition.Operation, "scenario:") &&
				len(definition.RequiredFields) > 0
		}
		return false
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func fieldsAvailable(available map[string]struct{}, required []string) bool {
	for _, field := range required {
		if _, exists := available[field]; !exists {
			return false
		}
	}
	return true
}

func menusAvailable(available map[string]struct{}, required []string) bool {
	if len(required) == 0 {
		return false
	}
	for _, code := range required {
		if _, exists := available[code]; !exists {
			return false
		}
	}
	return true
}

func hasAnyExecution(definition Definition) bool {
	return definition.RemoteFilter != nil || definition.RemoteSort != nil ||
		definition.LocalFilter != nil || definition.LocalRank != nil
}

func supportedModes(definition Definition) []ExecutionMode {
	var result []ExecutionMode
	if definition.RemoteFilter != nil {
		result = append(result, ExecutionRemoteFilter)
	}
	if definition.RemoteSort != nil {
		result = append(result, ExecutionRemoteSort)
	}
	if definition.LocalFilter != nil {
		result = append(result, ExecutionLocalFilter)
	}
	if definition.LocalRank != nil {
		result = append(result, ExecutionLocalRank)
	}
	return result
}
