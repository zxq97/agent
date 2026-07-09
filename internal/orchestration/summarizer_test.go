package orchestration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zxq97/rental-agent/internal/llm"
)

func TestMaybeSummarizeKeepsRecentWindowAndWritesSummary(t *testing.T) {
	st := New("s1", "u1")
	for i := 1; i <= 8; i++ {
		st.AppendMessage(&llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("用户第%d轮", i)}, nil)
		st.AppendMessage(&llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("助手第%d轮", i)}, nil)
	}

	MaybeSummarize(st)

	if !strings.Contains(st.Summary, "第 1-2 轮") {
		t.Fatalf("Summary = %q, want compressed early rounds", st.Summary)
	}
	hist := st.SnapshotHistory()
	if len(hist) != 12 {
		t.Fatalf("history len = %d, want 12", len(hist))
	}
	if strings.Contains(hist[0].Msg.Content, "第1轮") {
		t.Fatalf("oldest raw message still present: %#v", hist[0].Msg)
	}
}
