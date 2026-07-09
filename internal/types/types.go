// Package types 持有跨模块共享的小类型,避免循环依赖。
package types

// Phase 会话阶段(预留,P3+ supervisor 不再用,这里仅作语义标记)。
type Phase string

const (
	PhaseShopping Phase = "shopping"
)

// QuoteSlot 导购阶段累计填充的关键槽位(过渡用,逐步被 RentalCtx 取代)。
type QuoteSlot struct {
	PickupText  string // 用户原始取车地点表述
	DropoffText string // 用户原始还车地点表述
	PickupTime  string // 取车时间(自然语言或已换算)
	DropoffTime string // 还车时间
	Seats       int    // 座位需求
	CarType     string // 车型偏好
}

// ToolCallSnapshot 一次工具调用的可回放快照。
// P1/B 用于 history 回放:把 assistant 轮还原成 (assistant{tool_calls}, tool{result})。
type ToolCallSnapshot struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串
	Result    string `json:"result"`    // 结果摘要(供下一轮模型读"上轮做了啥")
}
