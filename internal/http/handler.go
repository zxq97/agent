package http

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/session"
)

// Server 把 adk.Runner + Redis Session + 中间件 + 路由组装到一起。
//
// 单 Server 实例不持有 ctx 也不阻塞 —— 创建后调 Run() 进入监听循环,
// 收到信号时调 Shutdown(ctx) 优雅退出。
type Server struct {
	cfg      config.HTTPConf
	store    session.Store
	runner   *adk.Runner
	logger   *log.Logger
	srv      *http.Server
}

// NewServer 创建一个 Server。
//   - cfg:HTTP 监听 / 超时配置
//   - rateLimit:限流配置(per_minute / per_day 都为 0 时不限流)
//   - store:Session 持久化(Redis 或 Memory)
//   - runner:已组装好的 ADK Supervisor Runner(由调用方 NewSupervisorSystem 构造)
//   - logger:诊断日志输出,所有 SSE 内部错误 / agent 事件都写这里
func NewServer(
	cfg config.HTTPConf,
	rateLimit config.RateLimitConf,
	store session.Store,
	runner *adk.Runner,
	logger *log.Logger,
) *Server {
	s := &Server{
		cfg:    cfg,
		store:  store,
		runner: runner,
		logger: logger,
	}
	mux := http.NewServeMux()
	chatHandler := http.HandlerFunc(s.handleChat)

	// 中间件链:recover → trace → auth → ratelimit → handler
	mws := []func(http.Handler) http.Handler{
		recoverMiddleware,
		traceMiddleware,
		authMiddleware,
	}
	if rateLimit.PerMinute > 0 || rateLimit.PerDay > 0 {
		mws = append(mws, rateLimitMiddleware(newRateLimiter(rateLimit.PerMinute, rateLimit.PerDay)))
	}
	mux.Handle("/agent/chat", chain(chatHandler, mws...))

	mux.Handle("/agent/session/", chain(http.HandlerFunc(s.handleSession),
		recoverMiddleware, traceMiddleware, authMiddleware,
	))

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	s.srv = &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
	}
	return s
}

// Run 阻塞监听。返回的 error 是 ListenAndServe 的真实错误(不含 Shutdown 触发的 ErrServerClosed)。
func (s *Server) Run() error {
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown 优雅停机:等正在执行的 SSE 流跑完(直到 ctx 取消)。
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// chatRequest 是 POST /agent/chat 的 body。
type chatRequest struct {
	SessionID string         `json:"session_id"` // 必填
	Message   string         `json:"message"`    // 必填
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// handleChat 主接口。
//
// 流程:
//  1. 解析 body 校验必填
//  2. 从 Redis 拿历史 history
//  3. 打开 SSE
//  4. 调 adk.Runner.Run(ctx, history+user) → 边收 AgentEvent 边推 SSE
//  5. 流结束后把 history+本轮新消息写回 Redis
//
// 任何错误一律写 SSE error 事件 + 日志,**不暴露技术细节给前端**。
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	uid := UserIDFromCtx(ctx)
	tid := TraceIDFromCtx(ctx)

	req := &chatRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || strings.TrimSpace(req.Message) == "" {
		http.Error(w, "session_id and message are required", http.StatusBadRequest)
		return
	}
	s.logger.Printf("[chat] trace=%s user=%s session=%s msg=%s",
		tid, uid, req.SessionID, truncate(req.Message, 200))

	// 取 history
	history, err := s.store.Get(ctx, uid, req.SessionID)
	if err != nil {
		s.logger.Printf("[chat] trace=%s session.Get err=%v", tid, err)
		// 不打断:取不到就当新会话开始
		history = nil
	}
	history = append(history, schema.UserMessage(req.Message))

	// 开 SSE
	sw, err := newSSEWriter(w)
	if err != nil {
		s.logger.Printf("[chat] trace=%s sse init err=%v", tid, err)
		http.Error(w, "sse not supported", http.StatusInternalServerError)
		return
	}

	// 跑 agent
	iter := s.runner.Run(ctx, history)
	collected, finalText := s.consumeIter(ctx, iter, sw)

	// 写回 Redis(整体覆盖 history + 本轮新消息)
	newHistory := append(history, collected...)
	if err := s.store.Save(ctx, uid, req.SessionID, newHistory); err != nil {
		s.logger.Printf("[chat] trace=%s session.Save err=%v", tid, err)
	}

	// 收尾
	if err := sw.sendDone(); err != nil {
		s.logger.Printf("[chat] trace=%s sse done err=%v", tid, err)
	}
	s.logger.Printf("[chat] trace=%s done finalLen=%d collected=%d", tid, len(finalText), len(collected))
}

// consumeIter 从 ADK 事件流里:
//   - 把 assistant 文字答复以 message 类型推到 SSE
//   - 把 transfer / tool_call / tool_result 以 event 类型推出去(前端可选展示)
//   - 出错时只推 user_msg,debug 写日志
//   - 同时收集所有 message,返回给上层写回 history
func (s *Server) consumeIter(
	ctx context.Context,
	iter *adk.AsyncIterator[*adk.AgentEvent],
	sw *sseWriter,
) (collected []*schema.Message, finalText string) {
	tid := TraceIDFromCtx(ctx)
	var fb strings.Builder

	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			s.logger.Printf("[chat] trace=%s agent err=%v", tid, ev.Err)
			_ = sw.sendError("抱歉,系统繁忙,请稍后再试或联系人工客服。")
			continue
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput
		msg, err := mv.GetMessage()
		if err != nil || msg == nil {
			s.logger.Printf("[chat] trace=%s GetMessage err=%v", tid, err)
			continue
		}

		// 收集进 history
		collected = append(collected, msg)

		switch {
		case msg.Role == schema.Tool:
			_ = sw.sendEvent(ev.AgentName, "tool_result:"+msg.Name, map[string]any{
				"summary": truncate(msg.Content, 120),
			})
		case len(msg.ToolCalls) > 0:
			names := make([]string, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				names = append(names, tc.Function.Name)
			}
			detail := "tool_call:" + strings.Join(names, ",")
			if ev.Action != nil && ev.Action.TransferToAgent != nil {
				detail = "transfer:" + ev.Action.TransferToAgent.DestAgentName
			}
			_ = sw.sendEvent(ev.AgentName, detail, nil)
		case msg.Content != "":
			// 最终文字答复(纯文字,无 tool_calls)
			_ = sw.sendMessage(msg.Content)
			fb.WriteString(msg.Content)
		}
	}
	return collected, fb.String()
}

// handleSession 处理 GET / DELETE /agent/session/:id
//   - GET    返回最近 N 条消息摘要(脱敏)
//   - DELETE 清除会话
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := UserIDFromCtx(ctx)

	// path: /agent/session/<id>
	const prefix = "/agent/session/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	sid := strings.TrimPrefix(r.URL.Path, prefix)
	if sid == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		hist, err := s.store.Get(ctx, uid, sid)
		if err != nil {
			http.Error(w, "session error", http.StatusInternalServerError)
			return
		}
		// 仅返回最近 20 条做摘要
		const tail = 20
		if len(hist) > tail {
			hist = hist[len(hist)-tail:]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id": sid,
			"messages":   hist,
		})
	case http.MethodDelete:
		if err := s.store.Delete(ctx, uid, sid); err != nil {
			http.Error(w, "delete error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// truncate 截断长字符串用于日志。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
