package turnnormalizer

import "strings"

type SearchSignals struct {
	Operation    string
	NoPreference bool
}

func NormalizeSearch(text string) SearchSignals {
	normalized := strings.ToLower(strings.TrimSpace(text))
	result := SearchSignals{Operation: "search_now"}
	result.NoPreference = containsAny(normalized, []string{
		"都行", "都可以", "没要求", "没有要求", "不限", "随便", "看着办",
	})
	switch {
	case containsAny(normalized, []string{"上一批", "上一页", "返回上一"}):
		result.Operation = "previous_batch"
	case containsAny(normalized, []string{"刷新", "重新搜", "重新查", "更新一下"}):
		result.Operation = "refresh"
	case containsAny(normalized, []string{"换一批", "还有别的", "还有其他", "下一批", "下一页", "继续看", "更多"}):
		result.Operation = "next_batch"
	}
	return result
}

func containsAny(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
