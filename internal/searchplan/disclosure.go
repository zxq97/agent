package searchplan

import "strings"

// DisclosuresFromResolutions converts execution limitations into deterministic
// reply obligations. These records are data, not free-form model suggestions.
func DisclosuresFromResolutions(values []Resolution) []Disclosure {
	var result []Disclosure
	seen := make(map[string]struct{})
	for _, value := range values {
		if value.Importance != "hard" {
			continue
		}
		switch value.Capability {
		case CapabilityAmbiguous, CapabilityUnverifiable, CapabilityUnsupported:
		default:
			continue
		}
		message := "“" + strings.TrimSpace(value.RawText) + "”当前无法作为可靠筛选条件，已继续按其他可执行条件搜索，不代表该诉求已满足。"
		if strings.TrimSpace(value.Reason) != "" {
			message += "原因：" + strings.TrimSpace(value.Reason) + "。"
		}
		key := value.RequirementID + "|" + string(DisclosureHardUnmapped)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, Disclosure{
			RequirementID: value.RequirementID,
			RawText:       value.RawText,
			Kind:          DisclosureHardUnmapped,
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
