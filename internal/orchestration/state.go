// Package orchestration 持有 ConversationState 的唯一定义。
// 任何模块要读写会话状态都 import 这里,禁止重新定义同名结构。
package orchestration

import (
	"fmt"
	"sync"
	"time"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/types"
)

// NeedsStatePrefix 构造当前需求状态的文本前缀(注入给 LLM 看)。
func NeedsStatePrefix(constraints types.SearchConstraints) string {
	all := make([]types.UserNeed, 0, len(constraints.Hard)+len(constraints.Soft)+len(constraints.Negative))
	all = append(all, constraints.Hard...)
	all = append(all, constraints.Soft...)
	all = append(all, constraints.Negative...)
	if len(all) == 0 {
		return "【当前需求状态】(空)"
	}
	var b []byte
	b = append(b, "【当前需求状态】\n"...)
	for _, n := range all {
		prefix := ""
		if n.Negative {
			prefix = "排除:"
		}
		b = append(b, fmt.Sprintf("- %s%s=%v (hardness=%s, conf=%.2f)\n", prefix, n.Type, n.Value, n.Hardness, n.Confidence)...)
	}
	return string(b)
}

// QuoteRef 一条报价的最小指代信息。
// LLM 不直接读 ReferenceID/Supplier,只在被指代时由 ResolveQuoteRef 翻译。
type QuoteRef struct {
	ReferenceID string
	Supplier    string
	CarName     string
	BrandName   string
	DailyPrice  float64
	TotalPrice  float64
	Index       int // 1-based 序号,匹配"第一辆/第 2 个"
}

// RentalPOI 是真实取/还车点位。CityID/Name 是对旧字段的冗余快照,
// Latitude/Longitude 是 rental-guide 搜车真正需要的点位坐标。
type RentalPOI struct {
	LocationID string
	CityID     int
	Name       string
	Latitude   float64
	Longitude  float64
}

func (p RentalPOI) Valid() bool {
	return p.CityID > 0 && p.Latitude != 0 && p.Longitude != 0
}

// RentalCtx 一对取还车 + 它的 context_id。
// context_id 由 rental_search_quotes 返回后由 Go 写入,不依赖 LLM。
type RentalCtx struct {
	PickupCityID  int
	PickupName    string
	PickupPOI     RentalPOI
	PickupTime    time.Time
	DropoffCityID int
	DropoffName   string
	DropoffPOI    RentalPOI
	DropoffTime   time.Time
	ContextID     string // ← Go 注入到下游工具入参
}

// Profile 是跨轮沉淀的轻量用户画像。
type Profile struct {
	TripScene        string `json:"trip_scene,omitempty"`
	Companions       string `json:"companions,omitempty"`
	PriceSensitivity string `json:"price_sensitivity,omitempty"`
	StylePreference  string `json:"style_preference,omitempty"`
}

// HistoryEntry 一条历史(带工具调用快照,供 history 回放)。
type HistoryEntry struct {
	Msg      *llm.Message            // 基础消息
	ToolCall *types.ToolCallSnapshot // 可空:本轮 assistant 发起过的工具调用快照
}

// ConversationState 一次会话的全部上下文。多组件共享同一份,mutex 保护并发。
type ConversationState struct {
	mu sync.Mutex

	SessionID string
	UserID    string
	CreatedAt time.Time
	UpdatedAt time.Time

	Phase types.Phase
	Slot  types.QuoteSlot // 过渡用,逐步被 Rental 取代

	// 取还车 + context_id
	Rental RentalCtx

	// 上一轮报价
	LastQuotes  []QuoteRef
	QuoteAt     time.Time // 15 分钟时效判定
	SelectedRef string    // 用户已锁定的报价(说"看第一辆明细"后 Go 解析填)

	// 结构化需求约束(跨轮累积)
	Constraints types.SearchConstraints
	TurnCount   int

	// 上次搜索状态
	LastSearch *types.LastSearchState

	// 轻量画像(P7):由 decide profile_patch 写入,下一轮 state prefix 注入。
	Profile Profile

	// 菜单缓存(从 guide storelist 返回,跟 context_id 绑定)
	CachedMenu []types.MenuGroupView

	// 完整消息历史(含工具调用快照)
	History []HistoryEntry

	// 长对话摘要(P6):压缩早期 history 后注入 state prefix。
	Summary string
}

// New 创建全新会话状态。
func New(sessionID, userID string) *ConversationState {
	now := time.Now()
	return &ConversationState{
		SessionID: sessionID,
		UserID:    userID,
		CreatedAt: now,
		UpdatedAt: now,
		Phase:     types.PhaseShopping,
		History:   make([]HistoryEntry, 0, 16),
	}
}

// AppendMessage 线程安全追加一条消息(可带工具调用快照)。
func (s *ConversationState) AppendMessage(msg *llm.Message, tc *types.ToolCallSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = append(s.History, HistoryEntry{Msg: msg, ToolCall: tc})
	s.UpdatedAt = time.Now()
}

// SnapshotHistory 返回历史拷贝,供外部只读避免并发改写。
func (s *ConversationState) SnapshotHistory() []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HistoryEntry, len(s.History))
	copy(out, s.History)
	return out
}

// SetQuotes 写入本轮报价 + context_id,刷新时效戳。
func (s *ConversationState) SetQuotes(ctxID string, quotes []QuoteRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Rental.ContextID = ctxID
	s.LastQuotes = quotes
	s.QuoteAt = time.Now()
	s.SelectedRef = "" // 新一批报价,清掉旧选定
	s.UpdatedAt = s.QuoteAt
}

// SnapshotQuotes 返回 context_id + 报价拷贝 + 距上次报价的时长。
func (s *ConversationState) SnapshotQuotes() (ctxID string, quotes []QuoteRef, age time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]QuoteRef, len(s.LastQuotes))
	copy(out, s.LastQuotes)
	age = time.Duration(1<<62 - 1) // 无报价时视为极久远
	if !s.QuoteAt.IsZero() {
		age = time.Since(s.QuoteAt)
	}
	return s.Rental.ContextID, out, age
}

// SelectQuote 锁定某条报价(reference_id)。
func (s *ConversationState) SelectQuote(ref string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SelectedRef = ref
	s.UpdatedAt = time.Now()
}

// SelectedQuoteRef 返回当前已锁定的报价 reference_id。
func (s *ConversationState) SelectedQuoteRef() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.SelectedRef
}

// IsQuoteStale 报价是否过期。无报价视为过期。
func (s *ConversationState) IsQuoteStale(ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.QuoteAt.IsZero() {
		return true
	}
	return time.Since(s.QuoteAt) > ttl
}

// SupplierOf 查 reference_id 对应的 supplier(Go 注入下游工具用)。
func (s *ConversationState) SupplierOf(ref string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.LastQuotes {
		if q.ReferenceID == ref {
			return q.Supplier
		}
	}
	return ""
}

// ResetForRentalChange 清除因地点/时间变更而失效的关联数据。
// 调用时机:UpdateRentalCapability 写完新 rental 字段后立即调用。
// 原因:context_id / 报价 / 搜索状态 / 菜单都绑定在旧的取还车参数上,改参数后必须让下一轮 search 重拉。
func (s *ConversationState) ResetForRentalChange() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Rental.ContextID = ""
	s.LastQuotes = nil
	s.QuoteAt = time.Time{}
	s.SelectedRef = ""
	s.LastSearch = nil
	s.CachedMenu = nil
}
