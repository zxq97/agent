// Package orchestration 持有 ConversationState 的唯一定义。
// 旧规划审查发现 ConversationState 多处定义会冲突,因此这里是单源。
package orchestration

import (
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/zxq97/agent/internal/types"
)

// ConversationState 一次会话的全部上下文。
// 多 agent 共享同一份 state(单进程内),通过 mutex 保护并发。
type ConversationState struct {
	mu sync.Mutex

	SessionID string
	UserID    string
	CreatedAt time.Time
	UpdatedAt time.Time

	// Phase: P1 单 agent 不用,P3 supervisor 起用。
	Phase types.Phase

	// 关键槽位 —— 导购阶段累计填充。
	Slot types.QuoteSlot

	// LastQuoteIDs 上一轮 list_quotes 返回的报价 id,
	// 用户说 "第一个" / "那辆朗逸" 时由 agent 自行映射。
	LastQuoteIDs []string
	// SelectedQuoteID 用户已锁定的报价,用于后续 get_price_detail / list_insurances。
	SelectedQuoteID string

	// History 完整消息历史(eino schema)。
	History []*schema.Message
}

// New 创建一个全新的会话状态。
func New(sessionID, userID string) *ConversationState {
	now := time.Now()
	return &ConversationState{
		SessionID: sessionID,
		UserID:    userID,
		CreatedAt: now,
		UpdatedAt: now,
		Phase:     types.PhaseShopping,
		History:   make([]*schema.Message, 0, 16),
	}
}

// AppendMessage 线程安全地追加消息并刷新 UpdatedAt。
func (s *ConversationState) AppendMessage(msg *schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = append(s.History, msg)
	s.UpdatedAt = time.Now()
}

// SnapshotHistory 返回历史消息的拷贝,供外部读取避免并发改写。
func (s *ConversationState) SnapshotHistory() []*schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*schema.Message, len(s.History))
	copy(out, s.History)
	return out
}

// MutateSlot 提供回调式槽位修改,保证持锁。
func (s *ConversationState) MutateSlot(fn func(*types.QuoteSlot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.Slot)
	s.UpdatedAt = time.Now()
}
