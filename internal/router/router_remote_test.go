package router

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/config"
)

const routerTestConfigPath = "../../conf/dev.yaml"

func TestLLMRouterWithRemoteService(t *testing.T) {
	cfg, err := config.Load(routerTestConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		t.Skip("DEEPSEEK_API_KEY is required for remote router tests")
	}
	client, err := llm.NewHTTPClient(&llm.HTTPConfig{Endpoint: cfg.LLM.Endpoint, APIKey: cfg.LLM.APIKey, TimeoutSec: cfg.LLM.TimeoutSec})
	if err != nil {
		t.Fatal(err)
	}
	intentRouter, err := NewLLMRouter(client)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		input   *Input
		want    []ActionType
		notWant []ActionType
	}{
		{
			name:  "rental only",
			input: &Input{SourceText: "明天下午三点在虹桥机场取车"},
			want:  []ActionType{ActionModifyRentalContext},
			notWant: []ActionType{
				ActionUpdateVehicleRequirements,
				ActionRequestVehicleSearch,
			},
		},
		{
			name:  "search only",
			input: &Input{SourceText: "想看特斯拉 Model Y"},
			want:  []ActionType{ActionUpdateVehicleRequirements},
			notWant: []ActionType{
				ActionModifyRentalContext,
				ActionRequestVehicleSearch,
			},
		},
		{
			name:  "mixed intent",
			input: &Input{SourceText: "后天下午虹桥取，想要7座SUV"},
			want:  []ActionType{ActionModifyRentalContext, ActionUpdateVehicleRequirements},
		},
		{
			name: "pending answer with requirement",
			input: &Input{
				SourceText: "第一个，每天预算300",
				ActivePending: &PendingView{
					Type:     "select_location",
					Question: "找到多个相关地点，请确认具体地点。",
					Options:  []string{"虹桥机场 上海", "虹桥火车站 上海"},
				},
			},
			want: []ActionType{ActionModifyRentalContext, ActionUpdateVehicleRequirements},
		},
		{
			name:  "general reply",
			input: &Input{SourceText: "你好"},
			want:  []ActionType{ActionGeneralReply},
			notWant: []ActionType{
				ActionModifyRentalContext,
				ActionUpdateVehicleRequirements,
				ActionRequestVehicleSearch,
			},
		},
		{
			name:  "continue previous search",
			input: &Input{SourceText: "换一批", HasPreviousSearch: true},
			want:  []ActionType{ActionRequestVehicleSearch},
			notWant: []ActionType{
				ActionUpdateVehicleRequirements,
			},
		},
		{
			name: "no preference answer starts search",
			input: &Input{
				SourceText: "看着办，都可以",
				CurrentRental: RentalContextView{
					LocationName: "虹桥机场",
					PickupTime:   "2026-07-24T10:00:00+08:00",
					ReturnTime:   "2026-07-25T10:00:00+08:00",
				},
				RecentMessages: []ConversationMessage{{
					Role: "assistant", Content: "对品牌、车型、座位数、能源类型或预算有要求吗？",
				}},
			},
			want: []ActionType{ActionRequestVehicleSearch},
			notWant: []ActionType{
				ActionUpdateVehicleRequirements,
				ActionGeneralReply,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := intentRouter.Route(ctx, test.input)
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range test.want {
				if result.Candidate(action) == nil {
					t.Fatalf("missing action %q in %#v", action, result)
				}
			}
			for _, action := range test.notWant {
				if result.Candidate(action) != nil {
					t.Fatalf("unexpected action %q in %#v", action, result)
				}
			}
		})
	}
}
