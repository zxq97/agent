package webchat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/domain"
	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/rentalrules"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclecompare"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/router"
	"github.com/zxq97/agent/internal/searchpolicy"
	"github.com/zxq97/agent/internal/session"
)

type staticIntentRouter struct {
	result *router.RouteResult
}

type failingIntentRouter struct{}

func (failingIntentRouter) Route(context.Context, *router.Input) (*router.RouteResult, error) {
	return nil, errors.New("router unavailable")
}

func (r staticIntentRouter) Route(context.Context, *router.Input) (*router.RouteResult, error) {
	return r.result, nil
}

type controlledSaveStore struct {
	Store
	failures int
	err      error
	saves    int
}

func (s *controlledSaveStore) Save(ctx context.Context, value *SessionEnvelope, expectedVersion int64) error {
	s.saves++
	if s.failures > 0 {
		s.failures--
		return s.err
	}
	return s.Store.Save(ctx, value, expectedVersion)
}

type mismatchRentalHandler struct{}

func (mismatchRentalHandler) Handle(context.Context, *session.AgentSession, *rentalcontext.Input) (*rentalcontext.Result, error) {
	return nil, domain.ErrDomainMismatch
}

type mismatchRequirementHandler struct{}

func (mismatchRequirementHandler) Handle(context.Context, *session.AgentSession, *vehiclerequirement.Input) (*vehiclerequirement.Result, error) {
	return nil, domain.ErrDomainMismatch
}

type noOpSearchHandler struct{}

func (noOpSearchHandler) Handle(context.Context, *session.AgentSession, *searchcar.Input) (*searchcar.Result, error) {
	return &searchcar.Result{Status: searchcar.ResultNeedsContext}, nil
}

type staticGeneralReplyHandler struct{}

func (staticGeneralReplyHandler) Handle(_ context.Context, _ *session.AgentSession, input *generalreply.Input) (*generalreply.Result, error) {
	return &generalreply.Result{Message: "通用回复：" + input.SourceText}, nil
}

type failingGeneralReplyHandler struct{}

func (failingGeneralReplyHandler) Handle(context.Context, *session.AgentSession, *generalreply.Input) (*generalreply.Result, error) {
	return nil, errors.New("general reply unavailable")
}

func newTestRentalRulesHandler() rentalrules.Handler {
	handler, err := rentalrules.NewHandler(rentalrules.NewDefaultCatalog())
	if err != nil {
		panic(err)
	}
	return handler
}

func newTestOrchestrator(
	rental rentalcontext.Handler,
	requirement vehiclerequirement.Handler,
	search searchcar.Handler,
	policy orchestrator.SearchPolicy,
	now func() time.Time,
	general ...generalreply.Handler,
) *orchestrator.Orchestrator {
	if rental == nil {
		rental = mismatchRentalHandler{}
	}
	if requirement == nil {
		requirement = mismatchRequirementHandler{}
	}
	if search == nil {
		search = noOpSearchHandler{}
	}
	var generalHandler generalreply.Handler = staticGeneralReplyHandler{}
	if len(general) > 0 && general[0] != nil {
		generalHandler = general[0]
	}
	value, err := orchestrator.NewWithExtensions(
		rental,
		requirement,
		search,
		policy,
		now,
		generalHandler,
		vehiclecompare.NewHandler(),
		newTestRentalRulesHandler(),
	)
	if err != nil {
		panic(err)
	}
	return value
}

func TestServiceChatIsIdempotentAndRestoresClientSequence(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })
	intentRouter := staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{Action: router.ActionGeneralReply, EvidenceText: "你好", Confidence: 1}}}}
	service, err := NewService(newTestOrchestrator(mismatchRentalHandler{}, nil, nil, searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now }), intentRouter, store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession(context.Background(), "user")
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
	if _, _, err := service.Chat(context.Background(), "user", detail.SessionID, "request-1", 1, "不同内容"); err != ErrRequestIdentityConflict {
		t.Fatalf("request identity error=%v", err)
	}
	loaded, err := service.GetSession(context.Background(), "user", detail.SessionID)
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
		newTestOrchestrator(nil, nil, nil, searchpolicy.New(1, time.Now), time.Now),
		staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{
			Action: router.ActionGeneralReply, EvidenceText: "你好", Confidence: 1,
		}}}},
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession(context.Background(), "user")
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
		newTestOrchestrator(nil, nil, nil, searchpolicy.New(1, time.Now), time.Now, staticGeneralReplyHandler{}),
		staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{
			Action: router.ActionGeneralReply, EvidenceText: "SUV和MPV有什么区别", Confidence: 1,
		}}}},
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession(context.Background(), "user")
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

func (h recordingRentalHandler) Handle(context.Context, *session.AgentSession, *rentalcontext.Input) (*rentalcontext.Result, error) {
	*h.calls = append(*h.calls, "rental")
	return &rentalcontext.Result{Status: rentalcontext.ResultSuccess}, nil
}

type recordingSearchHandler struct {
	calls *[]string
}

func (h recordingSearchHandler) Handle(context.Context, *session.AgentSession, *searchcar.Input) (*searchcar.Result, error) {
	*h.calls = append(*h.calls, "search")
	return &searchcar.Result{Status: searchcar.ResultNeedsContext}, nil
}

type failingSearchHandler struct{}

func (failingSearchHandler) Handle(context.Context, *session.AgentSession, *searchcar.Input) (*searchcar.Result, error) {
	return nil, errors.New("search failed")
}

type recordingRequirementHandler struct {
	calls *[]string
}

func (h recordingRequirementHandler) Handle(_ context.Context, agentSession *session.AgentSession, _ *vehiclerequirement.Input) (*vehiclerequirement.Result, error) {
	*h.calls = append(*h.calls, "requirement")
	requirements := []session.SearchRequirementStateItem{{ID: "seat", Facet: "seat_num", CanonicalValue: "7", Operator: "eq", Importance: "hard", Status: "active"}}
	return &vehiclerequirement.Result{
		Changed: true, Requirements: requirements,
		Deltas: []session.StateDelta{&session.RequirementDelta{
			Requirements: requirements, IncrementVersion: true, ActivateGoal: true,
		}},
	}, nil
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
				newTestOrchestrator(
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
			detail, err := service.CreateSession(context.Background(), "user")
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

func TestServiceCommitsConfirmedRequirementWhenSearchFails(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })
	var calls []string
	service, err := NewService(
		newTestOrchestrator(
			nil,
			recordingRequirementHandler{calls: &calls},
			failingSearchHandler{},
			searchpolicy.New(1, func() time.Time { return now }),
			func() time.Time { return now },
		),
		staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{
			Action: router.ActionUpdateVehicleRequirements, EvidenceText: "想要7座", Confidence: 1,
		}}}},
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	response, replayed, err := service.Chat(context.Background(), "user", detail.SessionID, "failed-turn", 1, "想要7座")
	if err != nil {
		t.Fatal(err)
	}
	if replayed || response == nil || !strings.Contains(response.Message, "搜车服务暂时不可用") {
		t.Fatalf("response=%#v replayed=%v", response, replayed)
	}
	loaded, err := service.GetSession(context.Background(), "user", detail.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientSeq != 1 || len(loaded.History) != 2 || len(loaded.State.Requirements) != 1 {
		t.Fatalf("confirmed requirement was not committed: %#v", loaded)
	}
}

func TestMemoryStoreListsNewestSessionFirst(t *testing.T) {
	current := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return current })
	first, err := store.Create(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(time.Minute)
	second, err := store.Create(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.List(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].SessionID != second.SessionID || list[1].SessionID != first.SessionID {
		t.Fatalf("list=%#v", list)
	}
}

func TestMemoryStoreSaveUsesExpectedVersion(t *testing.T) {
	store := NewMemoryStore(time.Now)
	created, err := store.Create(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	first := cloneEnvelope(created)
	first.State.Version = 1
	if err := store.Save(context.Background(), first, 0); err != nil {
		t.Fatal(err)
	}
	stale := cloneEnvelope(created)
	stale.State.Version = 1
	if err := store.Save(context.Background(), stale, 0); err != ErrVersionConflict {
		t.Fatalf("error=%v", err)
	}
	loaded, err := store.Load(context.Background(), "user", created.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Version != 1 {
		t.Fatalf("version=%d", loaded.State.Version)
	}
}

func TestServiceDoesNotExposeDraftWhenStoreSaveFails(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	base := NewMemoryStore(func() time.Time { return now })
	store := &controlledSaveStore{Store: base, failures: 1, err: errors.New("store unavailable")}
	service, err := NewService(
		newTestOrchestrator(nil, nil, nil, searchpolicy.New(1, time.Now), time.Now, staticGeneralReplyHandler{}),
		staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{
			Action: router.ActionGeneralReply, EvidenceText: "你好", Confidence: 1,
		}}}},
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Chat(context.Background(), "user", detail.SessionID, "request", 1, "你好"); err == nil {
		t.Fatal("expected save failure")
	}
	loaded, err := base.Load(context.Background(), "user", detail.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Version != 0 || loaded.LatestSeq != 0 || len(loaded.History) != 0 || len(loaded.Completed) != 0 {
		t.Fatalf("uncommitted draft leaked into store: %#v", loaded)
	}
}

func TestServiceReplansOnceAfterVersionConflict(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	base := NewMemoryStore(func() time.Time { return now })
	store := &controlledSaveStore{Store: base, failures: 1, err: ErrVersionConflict}
	service, err := NewService(
		newTestOrchestrator(nil, nil, nil, searchpolicy.New(1, time.Now), time.Now, staticGeneralReplyHandler{}),
		staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{
			Action: router.ActionGeneralReply, EvidenceText: "你好", Confidence: 1,
		}}}},
		store,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, replayed, err := service.Chat(context.Background(), "user", detail.SessionID, "request", 1, "你好"); err != nil || replayed {
		t.Fatalf("chat err=%v replayed=%v", err, replayed)
	}
	if store.saves != 2 {
		t.Fatalf("save attempts=%d want=2", store.saves)
	}
	loaded, err := base.Load(context.Background(), "user", detail.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Version != 1 || loaded.LatestSeq != 1 || len(loaded.History) != 2 || len(loaded.Completed) != 1 {
		t.Fatalf("unexpected committed state: %#v", loaded)
	}
}

func TestServiceRouterFailureDoesNotCommitTurn(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })
	service, err := NewService(
		newTestOrchestrator(nil, nil, nil, searchpolicy.New(1, time.Now), time.Now),
		failingIntentRouter{}, store, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Chat(context.Background(), "user", detail.SessionID, "request", 1, "你好"); err == nil {
		t.Fatal("expected router failure")
	}
	loaded, err := store.Load(context.Background(), "user", detail.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Version != 0 || loaded.LatestSeq != 0 || len(loaded.History) != 0 || len(loaded.Completed) != 0 {
		t.Fatalf("router failure committed a turn: %#v", loaded)
	}
}

func TestServiceGeneralReplyFailureUsesDeterministicFallback(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore(func() time.Time { return now })
	service, err := NewService(
		newTestOrchestrator(nil, nil, nil, searchpolicy.New(1, time.Now), time.Now, failingGeneralReplyHandler{}),
		staticIntentRouter{result: &router.RouteResult{Candidates: []router.RouteCandidate{{
			Action: router.ActionGeneralReply, EvidenceText: "解释一下", Confidence: 1,
		}}}},
		store, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.CreateSession(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	response, _, err := service.Chat(context.Background(), "user", detail.SessionID, "request", 1, "解释一下")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Message, "暂时无法回答") {
		t.Fatalf("missing deterministic fallback: %#v", response)
	}
	loaded, err := store.Load(context.Background(), "user", detail.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.Version != 1 || loaded.LatestSeq != 1 || len(loaded.History) != 2 || len(loaded.Completed) != 1 {
		t.Fatalf("fallback turn was not atomically committed: %#v", loaded)
	}
}
