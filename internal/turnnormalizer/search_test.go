package turnnormalizer

import "testing"

func TestNormalizeSearch(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		operation    string
		noPreference bool
	}{
		{name: "default", text: "帮我搜车", operation: "search_now"},
		{name: "previous", text: "返回上一批", operation: "previous_batch"},
		{name: "next", text: "还有其他的吗", operation: "next_batch"},
		{name: "refresh", text: "重新查一下", operation: "refresh"},
		{name: "no preference", text: "车型都可以，直接搜", operation: "search_now", noPreference: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeSearch(test.text)
			if got.Operation != test.operation || got.NoPreference != test.noPreference {
				t.Fatalf("NormalizeSearch(%q) = %#v", test.text, got)
			}
		})
	}
}
