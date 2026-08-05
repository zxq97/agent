package searchplan

import "strings"

// DisclosuresFromResolutions converts execution limitations into deterministic
// reply obligations. These records are data, not free-form model suggestions.
func DisclosuresFromResolutions(values []Resolution) []Disclosure {
	var result []Disclosure
	seen := make(map[string]struct{})
	for _, value := range values {
		if value.ReasonCode == "local_vehicle_any_of" {
			key := value.RequirementID + "|local_vehicle_any_of"
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, Disclosure{
				RequirementID: value.RequirementID,
				RawText:       value.RawText,
				Kind:          DisclosureLocallyExcluded,
				Message:       "“" + strings.TrimSpace(value.RawText) + "”已在当前收集的 Guide 候选中按 OR 关系严格校验；该结论只覆盖本次已扫描的候选范围。",
				MustMention:   true,
			})
			continue
		}
		if value.ReasonCode == "local_negative_filter" {
			key := value.RequirementID + "|" + string(DisclosureLocallyExcluded)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, Disclosure{
				RequirementID: value.RequirementID,
				RawText:       value.RawText,
				Kind:          DisclosureLocallyExcluded,
				Message:       "已在 Guide 返回的候选中严格执行“" + strings.TrimSpace(value.RawText) + "”；明确违反或字段未知的报价不会展示。",
				MustMention:   true,
			})
			continue
		}
		switch value.Capability {
		case CapabilityAmbiguous, CapabilityUnverifiable, CapabilityUnsupported:
		default:
			continue
		}
		kind := DisclosureHardUnmapped
		message := "“" + strings.TrimSpace(value.RawText) + "”当前无法作为可靠筛选条件，已继续按其他可执行条件搜索，不代表该诉求已满足。"
		if value.Importance == "soft" {
			kind = DisclosureSoftUnmapped
			message = "“" + strings.TrimSpace(value.RawText) + "”当前缺少可靠筛选或排序能力，本次未应用该偏好。"
		}
		if strings.TrimSpace(value.Reason) != "" {
			message += "原因：" + strings.TrimSpace(value.Reason) + "。"
		}
		key := value.RequirementID + "|" + string(kind)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, Disclosure{
			RequirementID: value.RequirementID,
			RawText:       value.RawText,
			Kind:          kind,
			Message:       message,
			MustMention:   true,
		})
	}
	return result
}

// ExploratoryDisclosures makes it explicit that a score is recommendation
// evidence rather than a verified constraint.
func ExploratoryDisclosures(values []ExploratoryRank) []Disclosure {
	var result []Disclosure
	for _, value := range values {
		message := "“" + strings.TrimSpace(value.RawText) + "”无法被 Guide 筛选条件严格证明；仅根据返回的车辆事实对本次候选进行探索性排序，不代表该诉求已确定满足。"
		result = AddDisclosure(result, Disclosure{
			RequirementID: value.RequirementID,
			RawText:       value.RawText,
			Kind:          DisclosureExploratoryRanked,
			Message:       message,
			MustMention:   true,
		})
	}
	return result
}

// AddDisclosure appends a reply obligation once per requirement and kind.
func AddDisclosure(values []Disclosure, disclosure Disclosure) []Disclosure {
	for index := range values {
		if values[index].RequirementID == disclosure.RequirementID &&
			values[index].Kind == disclosure.Kind {
			return values
		}
	}
	return append(values, disclosure)
}
