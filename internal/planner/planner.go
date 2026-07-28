// Package planner builds the small deterministic action plan for one turn.
// It does not perform language understanding or external calls.
package planner

import (
	"fmt"
	"strings"
)

type ActionType string

const (
	ActionModifyRentalContext       ActionType = "modify_rental_context"
	ActionUpdateVehicleRequirements ActionType = "update_vehicle_requirements"
	ActionExecuteVehicleSearch      ActionType = "execute_vehicle_search"
	ActionGeneralReply              ActionType = "general_reply"
)

type Candidate struct {
	Type         ActionType
	EvidenceText string
	SourceID     string
	BaseVersion  int64
	BlockedBy    string
}

type PlannedAction struct {
	ID           string
	Type         ActionType
	EvidenceText string
	DependsOn    []string
	DeferredID   string
	BaseVersion  int64
	BlockedBy    string
}

type ActionPlan struct {
	Actions []PlannedAction
}

type Planner struct{}

func New() *Planner {
	return &Planner{}
}

func (p *Planner) Build(candidates []Candidate) ActionPlan {
	byType := make(map[ActionType]PlannedAction)
	for _, candidate := range candidates {
		if !validAction(candidate.Type) {
			continue
		}
		action := byType[candidate.Type]
		if action.ID == "" {
			action = PlannedAction{
				ID:   fmt.Sprintf("action-%s", candidate.Type),
				Type: candidate.Type,
			}
		}
		action.EvidenceText = appendUniqueEvidence(action.EvidenceText, candidate.EvidenceText)
		if candidate.SourceID != "" {
			action.DeferredID = candidate.SourceID
		}
		if candidate.BaseVersion != 0 {
			action.BaseVersion = candidate.BaseVersion
		}
		if candidate.BlockedBy != "" {
			action.BlockedBy = candidate.BlockedBy
		}
		byType[candidate.Type] = action
	}

	order := []ActionType{
		ActionModifyRentalContext,
		ActionUpdateVehicleRequirements,
		ActionExecuteVehicleSearch,
		ActionGeneralReply,
	}
	result := ActionPlan{}
	var previous string
	for _, actionType := range order {
		action, exists := byType[actionType]
		if !exists {
			continue
		}
		if previous != "" {
			action.DependsOn = []string{previous}
		}
		result.Actions = append(result.Actions, action)
		previous = action.ID
	}
	return result
}

func (p *Planner) Merge(base ActionPlan, candidates []Candidate) ActionPlan {
	all := make([]Candidate, 0, len(base.Actions)+len(candidates))
	for _, action := range base.Actions {
		all = append(all, Candidate{
			Type: action.Type, EvidenceText: action.EvidenceText, SourceID: action.DeferredID,
			BaseVersion: action.BaseVersion, BlockedBy: action.BlockedBy,
		})
	}
	all = append(all, candidates...)
	return p.Build(all)
}

func (p *ActionPlan) BindBaseVersion(version int64) {
	if p == nil {
		return
	}
	for index := range p.Actions {
		p.Actions[index].BaseVersion = version
	}
}

func (p ActionPlan) Action(actionType ActionType) *PlannedAction {
	for index := range p.Actions {
		if p.Actions[index].Type == actionType {
			return &p.Actions[index]
		}
	}
	return nil
}

func validAction(value ActionType) bool {
	switch value {
	case ActionModifyRentalContext, ActionUpdateVehicleRequirements,
		ActionExecuteVehicleSearch, ActionGeneralReply:
		return true
	default:
		return false
	}
}

func appendUniqueEvidence(current, addition string) string {
	addition = strings.TrimSpace(addition)
	if addition == "" {
		return strings.TrimSpace(current)
	}
	for _, existing := range strings.Split(current, "\n") {
		if strings.TrimSpace(existing) == addition {
			return strings.TrimSpace(current)
		}
	}
	if strings.TrimSpace(current) == "" {
		return addition
	}
	return strings.TrimSpace(current) + "\n" + addition
}
