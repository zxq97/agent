// Package router classifies one user turn into one or more domain actions.
// It identifies intent and supporting text only; domain handlers remain
// responsible for extracting and validating business values.
package router

import "context"

type ActionType string

const (
	ActionModifyRentalContext       ActionType = "modify_rental_context"
	ActionUpdateVehicleRequirements ActionType = "update_vehicle_requirements"
	ActionRequestVehicleSearch      ActionType = "request_vehicle_search"
	ActionGeneralReply              ActionType = "general_reply"
)

type RouteCandidate struct {
	Action       ActionType `json:"action"`
	EvidenceText string     `json:"evidence_text"`
	Confidence   float64    `json:"confidence"`
}

type RouteResult struct {
	Candidates     []RouteCandidate `json:"candidates"`
	UnassignedText string           `json:"unassigned_text"`
}

func (r *RouteResult) Candidate(action ActionType) *RouteCandidate {
	if r == nil {
		return nil
	}
	for index := range r.Candidates {
		if r.Candidates[index].Action == action {
			return &r.Candidates[index]
		}
	}
	return nil
}

type Input struct {
	SourceText          string                `json:"source_text"`
	CurrentRental       RentalContextView     `json:"current_rental"`
	CurrentRequirements []RequirementView     `json:"current_requirements"`
	ActivePending       *PendingView          `json:"active_pending"`
	RecentMessages      []ConversationMessage `json:"recent_messages"`
	HasPreviousSearch   bool                  `json:"has_previous_search"`
}

type RentalContextView struct {
	LocationName string `json:"location_name"`
	PickupTime   string `json:"pickup_time"`
	ReturnTime   string `json:"return_time"`
}

type RequirementView struct {
	Type       string `json:"type"`
	Value      string `json:"value"`
	Importance string `json:"importance"`
	Status     string `json:"status"`
}

type PendingView struct {
	Type     string   `json:"type"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

type ConversationMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Router interface {
	Route(context.Context, *Input) (*RouteResult, error)
}
