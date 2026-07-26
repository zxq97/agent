package progress

import (
	"context"
	"testing"
)

func TestEmitUsesContextReporter(t *testing.T) {
	var got Event
	ctx := WithReporter(context.Background(), func(event Event) { got = event })
	Emit(ctx, "vehicle_search", "正在搜索车辆")
	if got.Code != "vehicle_search" || got.Text == "" {
		t.Fatalf("event=%#v", got)
	}
}
