// Package webchat adapts the current Agent domains to a browser-oriented chat
// session without coupling domain handlers to HTTP or SSE.
package webchat

import "time"

type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionSummary struct {
	SessionID string    `json:"session_id"`
	Preview   string    `json:"preview"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SessionDetail struct {
	SessionID string    `json:"session_id"`
	ClientSeq int64     `json:"client_seq"`
	History   []Message `json:"history"`
	State     StateView `json:"state"`
}

type StateView struct {
	Location          *LocationView     `json:"location,omitempty"`
	PickupTime        *time.Time        `json:"pickup_time,omitempty"`
	ReturnTime        *time.Time        `json:"return_time,omitempty"`
	Requirements      []RequirementView `json:"requirements"`
	Pending           *PendingView      `json:"pending,omitempty"`
	ResultCount       int               `json:"result_count"`
	SearchDirtyReason string            `json:"search_dirty_reason,omitempty"`
}

type LocationView struct {
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
	CityID  string `json:"city_id,omitempty"`
}

type RequirementView struct {
	Type          string `json:"type"`
	Value         string `json:"value"`
	RawText       string `json:"raw_text,omitempty"`
	SemanticLabel string `json:"semantic_label,omitempty"`
	Category      string `json:"category,omitempty"`
	CanonicalType string `json:"canonical_type,omitempty"`
	Importance    string `json:"importance,omitempty"`
	Status        string `json:"status,omitempty"`
}

type PendingView struct {
	Type     string              `json:"type"`
	Question string              `json:"question"`
	Options  []PendingOptionView `json:"options"`
	ExpireAt time.Time           `json:"expire_at,omitempty"`
}

type PendingOptionView struct {
	Index int    `json:"index"`
	Label string `json:"label"`
	Value string `json:"value,omitempty"`
}

type VehicleView struct {
	Index           int      `json:"index"`
	Name            string   `json:"name"`
	Brand           string   `json:"brand,omitempty"`
	Seats           int      `json:"seats,omitempty"`
	Supplier        string   `json:"supplier,omitempty"`
	TotalAmount     *float64 `json:"total_amount,omitempty"`
	DeductionAmount *float64 `json:"deduction_amount,omitempty"`
}

type TurnResponse struct {
	Message                string                      `json:"message"`
	Pending                *PendingView                `json:"pending,omitempty"`
	Vehicles               []VehicleView               `json:"vehicles"`
	RequirementResolutions []RequirementResolutionView `json:"requirement_resolutions,omitempty"`
	State                  StateView                   `json:"state"`
}

type RequirementResolutionView struct {
	ID         string   `json:"id"`
	RawText    string   `json:"raw_text"`
	Status     string   `json:"status"`
	Executions []string `json:"executions,omitempty"`
	ReasonCode string   `json:"reason_code,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}
