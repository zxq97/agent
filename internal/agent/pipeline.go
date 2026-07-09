package agent

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/zxq97/rental-agent/internal/metric"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
)

// Signal Stage 处理后的流转信号。
type Signal int

const (
	SignalContinue Signal = iota // 交给下一个 Stage
	SignalStop                   // 本轮结束,短路
)

func (s Signal) String() string {
	switch s {
	case SignalContinue:
		return "continue"
	case SignalStop:
		return "stop"
	default:
		return "unknown"
	}
}

// AgentContext 一轮处理在 Pipeline 各 Stage 间传递的上下文。
type AgentContext struct {
	State     *orchestration.ConversationState
	UserInput string
	Deps      *tools.Deps
	Factory   ModelGetter
	Emit      Emitter
	EventType string
	Action    *ClientAction

	// Stage 间产物
	Decision *Decision
	Result   *CapabilityResult
	Feedback FeedbackStore

	// 日志
	Logger io.Writer
}

// Stage 责任链单阶段,职责单一。
type Stage interface {
	Name() string
	Handle(ctx context.Context, ac *AgentContext) (Signal, error)
}

// ChatPipeline 按顺序编排 Stage,遇 Stop 或 error 终止。
type ChatPipeline struct {
	stages []Stage
}

// NewChatPipeline 组装流水线。
func NewChatPipeline(stages ...Stage) *ChatPipeline {
	return &ChatPipeline{stages: stages}
}

// Run 逐 Stage 执行,每步打 start/done 日志(带耗时),Stop 短路、error 中断。
func (p *ChatPipeline) Run(ctx context.Context, ac *AgentContext) error {
	sid := ""
	if ac.State != nil {
		sid = ac.State.SessionID
	}
	// 每轮开头拍一张 state 快照,便于 grep '\[state\] .*session=xxx' 追跨轮画像演化。
	if ac.Logger != nil && ac.State != nil {
		fmt.Fprintln(ac.Logger, orchestration.SummarizeForLog(ac.State, "pipeline_start"))
	}
	for _, s := range p.stages {
		start := time.Now()
		sig, err := s.Handle(ctx, ac)
		dur := time.Since(start).Milliseconds()
		metric.Observe("stage_duration_ms", metric.Labels{"stage": s.Name()}, float64(dur))
		if ac.Logger != nil {
			if err != nil {
				fmt.Fprintf(ac.Logger, "[pipeline] session=%s stage=%s status=error dur_ms=%d err=%v\n", sid, s.Name(), dur, err)
			} else {
				fmt.Fprintf(ac.Logger, "[pipeline] session=%s stage=%s signal=%s dur_ms=%d\n", sid, s.Name(), sig.String(), dur)
			}
		}
		if err != nil {
			return err
		}
		if sig == SignalStop {
			break
		}
	}
	return nil
}
