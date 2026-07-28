package pendingresolver

import (
	"testing"

	"github.com/zxq97/agent/internal/session"
)

func TestResolveRequiresExplicitCancellationPrefix(t *testing.T) {
	resolver := New()
	active := &session.PendingInteraction{ID: "pending"}
	result := resolver.Resolve(active, "我不是要取消，只是问一下规则")
	if result.Event != EventNotAddressed || result.ResidualText == "" {
		t.Fatalf("result=%#v", result)
	}
	result = resolver.Resolve(active, "算了，改成杭州东站")
	if result.Event != EventCancelled || result.ResidualText != "，改成杭州东站" {
		t.Fatalf("result=%#v", result)
	}
}

func TestResolveSelectsOptionAndKeepsResidualText(t *testing.T) {
	resolver := New()
	active := &session.PendingInteraction{ID: "pending", Options: []session.PendingOption{
		{ID: "airport", Label: "虹桥机场"},
		{ID: "station", Label: "虹桥火车站"},
	}}
	result := resolver.Resolve(active, "虹桥机场，每天300")
	if result.Event != EventSelected || result.SelectedOption == nil ||
		result.SelectedOption.ID != "airport" || result.ResidualText != "，每天300" {
		t.Fatalf("result=%#v", result)
	}
	result = resolver.Resolve(active, "第2个，改成明天")
	if result.Event != EventSelected || result.SelectedOption == nil ||
		result.SelectedOption.ID != "station" || result.ResidualText != "改成明天" {
		t.Fatalf("result=%#v", result)
	}
}
