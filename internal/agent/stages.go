package agent

import "context"

// DecideStage 调 Decider 产 Decision(LLM #1,流式)。
type DecideStage struct {
	decider *Decider
}

func (s *DecideStage) Name() string { return "Decide" }

func (s *DecideStage) Handle(ctx context.Context, ac *AgentContext) (Signal, error) {
	if ac.Decision != nil {
		return SignalContinue, nil
	}
	emitEventPayload(ac.Emit, "thinking_tips", map[string]string{"status": "start", "type": "msg", "text": "小租正在思考"})
	dec, err := s.decider.Decide(ctx, ac.State, ac.UserInput, ac.Emit, ac.Logger)
	if err != nil {
		return SignalStop, err
	}
	ApplyProfilePatch(ac.State, dec.ProfilePatch)
	emitEventPayload(ac.Emit, "thinking_tips", map[string]string{"status": "done", "type": "msg", "text": ""})
	ac.Decision = dec
	return SignalContinue, nil
}

// CapabilityStage 按 Decision.Tool 认领分发到对应 Capability。
type CapabilityStage struct {
	orch *CapabilityOrchestrator
}

func (s *CapabilityStage) Name() string { return "Capability" }

func (s *CapabilityStage) Handle(ctx context.Context, ac *AgentContext) (Signal, error) {
	if ac.Result != nil {
		return SignalContinue, nil
	}
	if ac.Decision != nil && ac.Decision.Tool == ToolSearchVehicles {
		emitEventPayload(ac.Emit, "thinking_box", map[string]string{"box_type": "search", "step": "initialize", "words": "正在整理筛选条件"})
		emitEventPayload(ac.Emit, "thinking_box", map[string]string{"box_type": "search", "step": "thinking", "words": "正在查找可租车型"})
	}
	res, err := s.orch.Handle(ctx, ac)
	if err != nil {
		return SignalStop, err
	}
	if ac.Decision != nil && ac.Decision.Tool == ToolSearchVehicles {
		emitEventPayload(ac.Emit, "thinking_box", map[string]string{"box_type": "search", "step": "done", "words": "已找到候选车型"})
	}
	ac.Result = res
	return SignalContinue, nil
}

// FinalizeStage 落 state、写 history。
type FinalizeStage struct{}

func (s *FinalizeStage) Name() string { return "Finalize" }

func (s *FinalizeStage) Handle(ctx context.Context, ac *AgentContext) (Signal, error) {
	finalize(ac)
	return SignalContinue, nil
}
