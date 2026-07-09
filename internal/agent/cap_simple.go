package agent

import "context"

// PureReplyCapability 闲聊/越界:Decision.Reply 已由 Decider 流式吐过,直接收尾。
type PureReplyCapability struct{}

func (c *PureReplyCapability) Name() string { return "pure_reply" }

func (c *PureReplyCapability) Run(ctx context.Context, in CapabilityInput) (*CapabilityResult, error) {
	return &CapabilityResult{Text: in.Decision.Reply}, nil
}

// AskCapability 追问:渲染 Decision.Args 里的 question + options。
// 话术(前置铺垫)已由 Decider 流式吐过,这里把反问作为 Clarification 交给下游渲染。
type AskCapability struct{}

func (c *AskCapability) Name() string { return "ask" }

func (c *AskCapability) Run(ctx context.Context, in CapabilityInput) (*CapabilityResult, error) {
	args := in.Decision.Args
	q, _ := args["question"].(string)
	slot, _ := args["slot"].(string)

	var opts []string
	if raw, ok := args["options"].([]any); ok {
		for _, o := range raw {
			if s, ok := o.(string); ok {
				opts = append(opts, s)
			}
		}
	}

	clar := &Clarification{Question: q, Options: opts, Slot: slot}
	return &CapabilityResult{
		Text:          in.Decision.Reply,
		Clarification: clar,
	}, nil
}
