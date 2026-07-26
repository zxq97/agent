package session

type AgentSession struct {
	SessionID    string
	Version      int64
	Search       SearchState
	Pending      PendingStore
	StateChanges []StateChange
	Memory       ConversationMemory
}

type ConversationMemory struct {
	RecentRentalContextTexts []string
	RecentSearchCarTexts     []string
}
