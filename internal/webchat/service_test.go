package webchat

import (
	"context"
	"testing"
	"time"

	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/router"
	"github.com/zxq97/agent/internal/searchpolicy"
	"github.com/zxq97/agent/internal/session"
)

type staticIntentRouter struct {
	result *router.RouteResult
}

func (r staticIntentRouter) Route(context.Context, *router.Input) (*router.RouteResult, error) {
	return r.result, nil
}

type mismatchRentalHandler struct{}

func (mismatchRentalHandler) Handle(context.Context, *session.AgentSession, *rentalcontext.ModifyRentalContextInput) (*rentalcontext.ModifyRentalContextResult, error) {
	return nil, rentalcontext.ErrDomainMismatch
}

type staticGeneralReplyHandler struct{}

func (staticGeneralReplyHandler) Handle(_ context.Context, _ *session.AgentSession, input *generalreply.Input) (*generalreply.Result, error) {
	return &generalreply.Result{Message: "通用回复：" + input.SourceText}, nil
}

func TestServiceChatIsIdempotentAndRestoresClientSequence(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })
	intentRouter := staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{Action: router.ActionGeneralReply, EvidenceText: "你好", Confidence: 1}}}}
	service, err := NewService(orchestrator.New(mismatchRentalHandler{}, nil, nil, searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now }), intentRouter, store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession("user")
	if err != nil {
		t.Fatal(err)
	}
	first, replayed, err := service.Chat(context.Background(), "user", detail.SessionID, "request-1", 1, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if replayed || first.Message == "" {
		t.Fatalf("first=%#v replayed=%v", first, replayed)
	}
	second, replayed, err := service.Chat(context.Background(), "user", detail.SessionID, "request-1", 1, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || second.Message != first.Message {
		t.Fatalf("second=%#v replayed=%v", second, replayed)
	}
	loaded, err := service.GetSession("user", detail.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientSeq != 1 || len(loaded.History) != 2 {
		t.Fatalf("detail=%#v", loaded)
	}
	if _, _, err := service.Chat(context.Background(), "user", detail.SessionID, "request-2", 1, "旧消息"); err != ErrStaleClientSeq {
		t.Fatalf("stale error=%v", err)
	}
}

func TestServiceRequiresRequestIdentityAndSequence(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })
	service, err := NewService(
		orchestrator.New(nil, nil, nil, searchpolicy.New(1, time.Now), time.Now),
		staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{
			Action: router.ActionGeneralReply, EvidenceText: "你好", Confidence: 1,
		}}}},
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession("user")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Chat(context.Background(), "user", detail.SessionID, "", 1, "你好"); err != ErrRequestIDMissing {
		t.Fatalf("missing request id error=%v", err)
	}
	if _, _, err := service.Chat(context.Background(), "user", detail.SessionID, "request", 0, "你好"); err != ErrClientSeqInvalid {
		t.Fatalf("invalid client seq error=%v", err)
	}
}

func TestServiceExecutesGeneralReplyAction(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })
	service, err := NewService(
		orchestrator.New(nil, nil, nil, searchpolicy.New(1, time.Now), time.Now, staticGeneralReplyHandler{}),
		staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{
			Action: router.ActionGeneralReply, EvidenceText: "SUV和MPV有什么区别", Confidence: 1,
		}}}},
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession("user")
	if err != nil {
		t.Fatal(err)
	}
	response, _, err := service.Chat(context.Background(), "user", detail.SessionID, "general-1", 1, "SUV和MPV有什么区别")
	if err != nil {
		t.Fatal(err)
	}
	if response.Message != "通用回复：SUV和MPV有什么区别" {
		t.Fatalf("response=%#v", response)
	}
}

type recordingRentalHandler struct {
	calls *[]string
}

func (h recordingRentalHandler) Handle(context.Context, *session.AgentSession, *rentalcontext.ModifyRentalContextInput) (*rentalcontext.ModifyRentalContextResult, error) {
	*h.calls = append(*h.calls, "rental")
	return &rentalcontext.ModifyRentalContextResult{Status: rentalcontext.ResultSuccess}, nil
}

type recordingSearchHandler struct {
	calls *[]string
}

func (h recordingSearchHandler) Handle(context.Context, *session.AgentSession, *searchcar.SearchCarInput) (*searchcar.SearchCarResult, error) {
	*h.calls = append(*h.calls, "search")
	return &searchcar.SearchCarResult{Status: searchcar.ResultNeedsContext}, nil
}

type recordingRequirementHandler struct {
	calls *[]string
}

func (h recordingRequirementHandler) Handle(_ context.Context, agentSession *session.AgentSession, _ *vehiclerequirement.UpdateInput) (*vehiclerequirement.UpdateResult, error) {
	*h.calls = append(*h.calls, "requirement")
	agentSession.Search.Requirements = []session.SearchRequirementStateItem{{ID: "seat", Facet: "seat_num", CanonicalValue: "7", Operator: "eq", Importance: "hard", Status: "active"}}
	agentSession.Search.RequirementVersion++
	return &vehiclerequirement.UpdateResult{Changed: true, Requirements: agentSession.Search.Requirements}, nil
}

func TestServiceRoutesOnlySelectedDomainsInSerialOrder(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		text       string
		candidates []router.RouteCandidate
		want       []string
	}{
		{
			name:       "requirement update triggers search policy",
			text:       "想要7座SUV",
			candidates: []router.RouteCandidate{{Action: router.ActionUpdateVehicleRequirements, EvidenceText: "想要7座SUV", Confidence: 1}},
			want:       []string{"requirement", "search"},
		},
		{
			name: "mixed intent",
			text: "明天虹桥取，想要7座SUV",
			candidates: []router.RouteCandidate{
				{Action: router.ActionModifyRentalContext, EvidenceText: "明天虹桥取", Confidence: 1},
				{Action: router.ActionUpdateVehicleRequirements, EvidenceText: "想要7座SUV", Confidence: 1},
			},
			want: []string{"rental", "requirement", "search"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			store := NewMemoryStore(func() time.Time { return now })
			service, err := NewService(
				orchestrator.New(
					recordingRentalHandler{calls: &calls},
					recordingRequirementHandler{calls: &calls},
					recordingSearchHandler{calls: &calls},
					searchpolicy.New(1, func() time.Time { return now }),
					func() time.Time { return now },
				),
				staticIntentRouter{result: &router.RouteResult{Candidates: test.candidates}},
				store,
				func() time.Time { return now },
			)
			if err != nil {
				t.Fatal(err)
			}
			detail, err := service.CreateSession("user")
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := service.Chat(context.Background(), "user", detail.SessionID, test.name, 1, test.text); err != nil {
				t.Fatal(err)
			}
			if len(calls) != len(test.want) {
				t.Fatalf("calls=%v want=%v", calls, test.want)
			}
			for index := range calls {
				if calls[index] != test.want[index] {
					t.Fatalf("calls=%v want=%v", calls, test.want)
				}
			}
		})
	}
}

func TestMemoryStoreListsNewestSessionFirst(t *testing.T) {
	current := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return current })
	first := store.Create("user")
	current = current.Add(time.Minute)
	second := store.Create("user")
	list := store.List("user")
	if len(list) != 2 || list[0].SessionID != second.sessionID || list[1].SessionID != first.sessionID {
		t.Fatalf("list=%#v", list)
	}
}
