package agent

import (
	"strings"
	"testing"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/types"
)

func TestBuildMessagesReplaysToolCallsAndAddsStatePrefix(t *testing.T) {
	st := orchestration.New("s1", "u1")
	st.AppendMessage(&llm.Message{Role: llm.RoleUser, Content: "明天北京 SUV"}, nil)
	st.AppendMessage(&llm.Message{Role: llm.RoleAssistant, Content: "我帮你找几辆。"}, &types.ToolCallSnapshot{
		Name:      ToolSearchVehicles,
		Arguments: `{"need_delta":[]}`,
		Result:    "已展示3辆车",
	})
	decider := NewDecider(nil, "")

	msgs := decider.buildMessages(st, "换一批")

	if len(msgs) != 4 {
		t.Fatalf("len(messages)=%d, want 4: %#v", len(msgs), msgs)
	}
	if msgs[1].Role != llm.RoleAssistant || len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("assistant replay = %#v", msgs[1])
	}
	if msgs[1].ToolCalls[0].Function.Name != ToolSearchVehicles {
		t.Fatalf("tool name = %q", msgs[1].ToolCalls[0].Function.Name)
	}
	if msgs[2].Role != llm.RoleTool || msgs[2].Content != "已展示3辆车" || msgs[2].ToolCallID == "" {
		t.Fatalf("tool replay = %#v", msgs[2])
	}
	if msgs[3].Role != llm.RoleUser || !strings.Contains(msgs[3].Content, "## 当前会话状态") || !strings.Contains(msgs[3].Content, "换一批") {
		t.Fatalf("last user message = %#v", msgs[3])
	}
}
