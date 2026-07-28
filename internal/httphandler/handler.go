// Package httphandler exposes the local browser API and serves the static UI.
package httphandler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/progress"
	"github.com/zxq97/agent/internal/webchat"
	"github.com/zxq97/agent/pkg/log"
)

type Handler struct {
	service *webchat.Service
	logger  log.Logger
}

func New(service *webchat.Service, logger log.Logger) (*Handler, error) {
	if service == nil {
		return nil, errors.New("web handler: service is required")
	}
	return &Handler{service: service, logger: logger}, nil
}

func (h *Handler) Mux(webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/chat", h.handleChat)
	mux.HandleFunc("GET /api/sessions", h.handleListSessions)
	mux.HandleFunc("POST /api/sessions", h.handleCreateSession)
	mux.HandleFunc("GET /api/sessions/{id}", h.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", h.handleDeleteSession)
	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.Handle("GET /", http.FileServer(http.Dir(webDir)))
	return chain(mux, requestContext(h.logger), recoverPanic(), cors(), accessLog())
}

type chatRequest struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
	ClientSeq int64  `json:"client_seq"`
	Message   string `json:"message"`
}

func (h *Handler) handleChat(writer http.ResponseWriter, request *http.Request) {
	var input chatRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		jsonError(writer, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.SessionID) == "" ||
		strings.TrimSpace(input.RequestID) == "" || input.ClientSeq <= 0 || strings.TrimSpace(input.Message) == "" {
		jsonError(writer, "user_id, session_id, request_id, positive client_seq and message are required", http.StatusBadRequest)
		return
	}
	emitter := newSSEEmitter(writer)
	if emitter == nil {
		jsonError(writer, "streaming not supported", http.StatusInternalServerError)
		return
	}
	traceID := log.TraceID(request.Context())
	emitter.send("accepted", map[string]any{"trace_id": traceID, "session_id": input.SessionID, "request_id": input.RequestID})
	processingContext := progress.WithReporter(request.Context(), func(event progress.Event) {
		emitter.send("progress", map[string]string{"code": event.Code, "text": event.Text})
	})
	progress.Emit(processingContext, "understanding", "正在理解你的租车需求")
	result, replayed, err := h.service.Chat(processingContext, input.UserID, input.SessionID, input.RequestID, input.ClientSeq, input.Message)
	if err != nil {
		code := "internal"
		message := "处理失败，请稍后重试"
		if errors.Is(err, webchat.ErrSessionNotFound) {
			code, message = "session_not_found", "会话不存在，请新建会话后重试"
		}
		if errors.Is(err, webchat.ErrStaleClientSeq) {
			code, message = "stale_client_seq", "这条消息已经过期，请刷新会话后重试"
		}
		log.Write(request.Context(), log.Entry{Component: "web", Operation: "chat", Error: err.Error()})
		emitter.send("error", map[string]string{"code": code, "message": message, "trace_id": traceID})
		return
	}
	emitter.send("progress", map[string]string{"code": "result_preparing", "text": "正在整理结果"})
	emitter.send("result", map[string]any{"response": result, "replayed": replayed, "trace_id": traceID})
	emitter.send("done", map[string]string{"trace_id": traceID})
}

type createSessionRequest struct {
	UserID string `json:"user_id"`
}

func (h *Handler) handleCreateSession(writer http.ResponseWriter, request *http.Request) {
	var input createSessionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		jsonError(writer, "invalid request body", http.StatusBadRequest)
		return
	}
	detail, err := h.service.CreateSession(request.Context(), input.UserID)
	if err != nil {
		jsonError(writer, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(writer, detail)
}

func (h *Handler) handleListSessions(writer http.ResponseWriter, request *http.Request) {
	userID := strings.TrimSpace(request.URL.Query().Get("user_id"))
	if userID == "" {
		jsonError(writer, "user_id query param is required", http.StatusBadRequest)
		return
	}
	values, err := h.service.ListSessions(request.Context(), userID)
	if err != nil {
		jsonError(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(writer, values)
}

func (h *Handler) handleGetSession(writer http.ResponseWriter, request *http.Request) {
	detail, err := h.service.GetSession(request.Context(), request.URL.Query().Get("user_id"), request.PathValue("id"))
	if err != nil {
		jsonError(writer, "session not found", http.StatusNotFound)
		return
	}
	jsonOK(writer, detail)
}

func (h *Handler) handleDeleteSession(writer http.ResponseWriter, request *http.Request) {
	err := h.service.DeleteSession(request.Context(), request.URL.Query().Get("user_id"), request.PathValue("id"))
	if err != nil {
		jsonError(writer, "session not found", http.StatusNotFound)
		return
	}
	jsonOK(writer, map[string]string{"status": "ok"})
}

func (h *Handler) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	jsonOK(writer, map[string]string{"status": "ok"})
}

func jsonOK(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(writer).Encode(value)
}

func jsonError(writer http.ResponseWriter, message string, status int) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": message})
}
