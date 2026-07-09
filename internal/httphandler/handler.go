package httphandler

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/zxq97/rental-agent/internal/agent"
	"github.com/zxq97/rental-agent/internal/metric"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/session"
	"github.com/zxq97/rental-agent/internal/trace"
)

// Handler HTTP 服务处理器。
type Handler struct {
	agent  *agent.RentalAgent
	store  session.Store
	logger io.Writer
}

// New 构造 Handler。
func New(ag *agent.RentalAgent, store session.Store, logger io.Writer) *Handler {
	return &Handler{agent: ag, store: store, logger: logger}
}

// Mux 返回带中间件的路由。webDir 是前端静态文件目录。
func (h *Handler) Mux(webDir string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/chat", h.handleChat)
	mux.HandleFunc("GET /api/sessions", h.handleListSessions)
	mux.HandleFunc("POST /api/sessions", h.handleCreateSession)
	mux.HandleFunc("GET /api/sessions/{id}", h.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", h.handleDeleteSession)
	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.HandleFunc("GET /debug/metrics", h.handleMetrics)
	mux.Handle("GET /", http.FileServer(http.Dir(webDir)))

	return chain(mux,
		recovery(h.logger),
		traceMiddleware(),
		cors(),
		accessLog(h.logger),
	)
}

// --- chat ---

type chatRequest struct {
	UserID    string              `json:"user_id"`
	SessionID string              `json:"session_id"`
	Message   string              `json:"message"`
	EventType string              `json:"event_type"`
	Action    *agent.ClientAction `json:"action"`
}

func (h *Handler) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.UserID == "" || req.SessionID == "" || (req.Message == "" && req.EventType == "") {
		jsonError(w, "user_id, session_id and message/event_type are required", http.StatusBadRequest)
		return
	}

	// trace_id 由 middleware 注入,handler 只负责使用。
	traceID := trace.FromCtx(r.Context())
	ctx := r.Context()
	// 底层 writer 用 handler 装配时注入的 logger(logsink 多路 writer),
	// 保证 per-request 日志既进按天切分的文件也可选进 stderr。
	sink := h.logger
	if sink == nil {
		sink = os.Stderr
	}
	tlog := trace.NewLog(traceID, sink)
	// 打全:message + event_type + action(前端胶囊点击时 message 为空,action 才是真身)。
	// event_type=action_click 时,action.type = compare / slot_patch / feedback_positive / feedback_negative 等。
	actionSummary := "(none)"
	if req.Action != nil {
		payload, _ := json.Marshal(req.Action.Payload)
		actionSummary = fmt.Sprintf("{type=%s label=%q payload=%s}", req.Action.Type, req.Action.Label, string(payload))
	}
	fmt.Fprintf(tlog, "[chat] user=%s session=%s event_type=%q message=%q action=%s\n",
		req.UserID, req.SessionID, req.EventType, req.Message, actionSummary)

	state := h.store.Get(req.UserID, req.SessionID)
	if state == nil {
		fmt.Fprintf(tlog, "[session] op=get status=miss user=%s session=%s action=new_state\n", req.UserID, req.SessionID)
		state = orchestration.New(req.SessionID, req.UserID)
	}

	emitter := newSSEEmitterWithVersion(w, r.Header.Get("Accept") == "application/x-sse-v1")
	if emitter == nil {
		jsonError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// 先发 session_id + trace_id 让前端知道
	emitter.send("session", map[string]string{"session_id": req.SessionID, "trace_id": traceID})

	_, err := h.agent.RunWithEvent(ctx, state, req.Message, req.EventType, req.Action, emitter, tlog)
	if err != nil {
		fmt.Fprintf(tlog, "[chat] error: %v\n", err)
		emitter.Error("抱歉,处理出了点问题,请稍后再试")
		return
	}

	h.store.Put(req.UserID, req.SessionID, state)
	fmt.Fprintf(tlog, "[session] op=put status=attempted user=%s session=%s\n", req.UserID, req.SessionID)

	// done 之前发 trace 日志给前端
	emitter.send("trace", map[string]any{"trace_id": traceID, "logs": tlog.Entries()})
	emitter.Done()
}

// --- sessions ---

type createSessionRequest struct {
	UserID string `json:"user_id"`
}

type createSessionResponse struct {
	SessionID string `json:"session_id"`
}

func (h *Handler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.UserID == "" {
		jsonError(w, "user_id is required", http.StatusBadRequest)
		return
	}

	sid := newSessionID()
	state := orchestration.New(sid, req.UserID)
	h.store.Put(req.UserID, sid, state)

	jsonOK(w, createSessionResponse{SessionID: sid})
}

func (h *Handler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		jsonError(w, "user_id query param is required", http.StatusBadRequest)
		return
	}
	list := h.store.List(userID)
	if list == nil {
		list = []session.Summary{}
	}
	jsonOK(w, list)
}

type displayMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sessionDetailResponse struct {
	SessionID string           `json:"session_id"`
	History   []displayMessage `json:"history"`
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	userID := r.URL.Query().Get("user_id")
	if userID == "" || sessionID == "" {
		jsonError(w, "user_id and session id are required", http.StatusBadRequest)
		return
	}

	state := h.store.Get(userID, sessionID)
	if state == nil {
		jsonError(w, "session not found", http.StatusNotFound)
		return
	}

	history := state.SnapshotHistory()
	msgs := make([]displayMessage, 0, len(history))
	for _, h := range history {
		if h.Msg == nil || h.Msg.Content == "" {
			continue
		}
		role := h.Msg.Role
		if role != "user" && role != "assistant" {
			continue
		}
		msgs = append(msgs, displayMessage{Role: role, Content: h.Msg.Content})
	}

	jsonOK(w, sessionDetailResponse{SessionID: sessionID, History: msgs})
}

func (h *Handler) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	userID := r.URL.Query().Get("user_id")
	if userID == "" || sessionID == "" {
		jsonError(w, "user_id and session id are required", http.StatusBadRequest)
		return
	}
	h.store.Delete(userID, sessionID)
	jsonOK(w, map[string]string{"status": "ok"})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]string{"status": "ok"})
}

func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(metric.Render()))
}

// --- helpers ---

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	// UUID v4 格式
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
