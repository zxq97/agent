package rentalcontext

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/maps"
	"github.com/zxq97/agent/internal/progress"
	"github.com/zxq97/agent/internal/session"
)

var (
	ErrSessionNil             = errors.New("modify rental context: session is required")
	ErrInputNil               = errors.New("modify rental context: input is required")
	ErrPickupTimeInPast       = errors.New("pickup time must be in the future")
	ErrReturnTimeInPast       = errors.New("return time must be in the future")
	ErrInvalidRentalTimeRange = errors.New("return time must be after pickup time")
)

type AmbiguityConfig struct {
	MinTopScore, MinScoreGap float64
	MaxOptions               int
	PendingTTL               time.Duration
}

func DefaultAmbiguityConfig() AmbiguityConfig {
	return AmbiguityConfig{0.90, 0.15, 5, 10 * time.Minute}
}

type IDGenerator interface{ NewID() string }

type ModifyRentalContextHandler struct {
	extractor CommandExtractor
	maps      maps.Client
	ids       IDGenerator
	now       func() time.Time
	timezone  *time.Location
	ambiguity AmbiguityConfig
}

func NewModifyRentalContextHandler(extractor CommandExtractor, mapsClient maps.Client, ids IDGenerator, now func() time.Time, timezone *time.Location, ambiguity AmbiguityConfig) (*ModifyRentalContextHandler, error) {
	if timezone == nil {
		return nil, errors.New("modify rental context: handler dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	if ambiguity.MaxOptions <= 0 {
		ambiguity = DefaultAmbiguityConfig()
	}
	return &ModifyRentalContextHandler{extractor: extractor, maps: mapsClient, ids: ids, now: now, timezone: timezone, ambiguity: ambiguity}, nil
}

func (h *ModifyRentalContextHandler) Handle(ctx context.Context, s *session.AgentSession, input *ModifyRentalContextInput) (result *ModifyRentalContextResult, err error) {
	if s == nil {
		return nil, ErrSessionNil
	}
	if input == nil {
		return nil, ErrInputNil
	}
	now := input.ReceivedAt
	if now.IsZero() {
		now = h.now()
	}
	s = session.Clone(s)
	defer func() {
		if err == nil && result != nil {
			result.Deltas = []session.StateDelta{session.RentalDeltaFrom(s, now)}
		}
	}()
	s.Pending.Expire(now)
	cmd := input.Command
	var ambiguous []AmbiguousField
	if cmd == nil {
		if h.extractor == nil {
			return nil, errors.New("modify rental context: extractor is required for source text")
		}
		extracted, err := h.extractor.Extract(ctx, h.buildExtractionInput(s, input.SourceText, now))
		if err != nil {
			return nil, err
		}
		cmd, ambiguous, err = mapExtractResult(extracted)
		if err != nil {
			return nil, err
		}
	}
	if err := validateCommand(cmd, len(ambiguous) > 0); err != nil {
		return nil, err
	}
	var resolved *maps.Candidate
	if cmd.LocationID != "" {
		var err error
		resolved, err = resolveSelectedLocation(s, cmd)
		if err != nil {
			return nil, err
		}
	} else if cmd.LocationQuery != "" {
		if option := matchPendingLocation(s.Pending.Active, cmd.LocationQuery); option != nil {
			resolved = candidateFromOption(*option)
			cmd.LocationID = option.ID
			cmd.InteractionID = s.Pending.Active.ID
		} else if s.Pending.Active != nil && s.Pending.Active.Type == session.PendingSelectLocation {
			s.Pending.Finish(session.PendingSuperseded, now)
		}
	}
	if resolved == nil && cmd.LocationQuery != "" {
		if h.maps == nil {
			return nil, errors.New("modify rental context: maps client is required")
		}
		progress.Emit(ctx, "location_search", "正在搜索并核对租车地点")
		response, err := h.maps.Search(ctx, &maps.SearchRequest{Keyword: cmd.LocationQuery, Limit: h.ambiguity.MaxOptions})
		if err != nil {
			return nil, err
		}
		var candidates []maps.Candidate
		if response != nil {
			candidates = response.Candidates
		}
		if len(candidates) == 0 {
			return &ModifyRentalContextResult{Status: ResultRejected, Message: "没有找到匹配的租车地点。"}, nil
		}
		if len(candidates) > 1 {
			preview := buildPreview(s.Search, cmd, nil)
			if err := validateTimes(preview, now); err != nil {
				return &ModifyRentalContextResult{Status: ResultRejected, Message: err.Error()}, nil
			}
			// Keep independent, resolved time changes even though the location
			// still needs the user to choose one of the map candidates.
			modified := apply(s, cmd, nil)
			result, err := h.createLocationPending(s, cmd, candidates, now)
			if result != nil {
				result.ModifiedFields = modified
			}
			if input.Command == nil {
				appendHistory(s, input.SourceText)
			}
			return result, err
		}
		resolved = &candidates[0]
		cmd.LocationID = resolved.ID
	}
	// Validate the prospective state before mutating the Session so an invalid
	// time range cannot be partially committed.
	preview := buildPreview(s.Search, cmd, resolved)
	if err := validateTimes(preview, now); err != nil {
		return &ModifyRentalContextResult{Status: ResultRejected, Message: err.Error()}, nil
	}
	// Apply every field that is already resolved. Ambiguous fields are handled
	// separately below and therefore do not discard these independent changes.
	modified := apply(s, cmd, resolved)
	// If the resolved time answers the currently active time clarification,
	// associate this command with that Pending so it can be completed below.
	associateTimePending(s.Pending.Active, cmd)
	if len(ambiguous) > 0 {
		// Present or defer only the first unresolved time field. PendingStore
		// enforces that the Session has at most one active user question.
		result, err := h.createTimePending(s, cmd, ambiguous[0], now)
		if result != nil {
			result.ModifiedFields = modified
		}
		if input.Command == nil {
			appendHistory(s, input.SourceText)
		}
		return result, err
	}
	// A Pending is resolved only by the command explicitly associated with the
	// same interaction; unrelated turns must not close the active question.
	if cmd.InteractionID != "" && s.Pending.Active != nil && s.Pending.Active.ID == cmd.InteractionID {
		s.Pending.Finish(session.PendingResolved, now)
	}
	if input.Command == nil {
		appendHistory(s, input.SourceText)
	}
	return success(s.Search, modified), nil
}

func validateCommand(command *ModifyRentalContextCommand, allowNoModification bool) error {
	if command == nil {
		return errors.New("modify rental context: command is required")
	}
	command.LocationQuery = strings.TrimSpace(command.LocationQuery)
	if !allowNoModification && !command.hasModification() {
		return errors.New("modify rental context: command has no modification")
	}
	if len(command.LocationQuery) > 120 {
		return errors.New("modify rental context: command field exceeds maximum length")
	}
	return nil
}

func resolveSelectedLocation(s *session.AgentSession, cmd *ModifyRentalContextCommand) (*maps.Candidate, error) {
	active := s.Pending.Active
	if active == nil || active.Type != session.PendingSelectLocation || active.ID != cmd.InteractionID {
		return nil, errors.New("modify rental context: location selection does not match active pending")
	}
	for _, option := range active.Options {
		if option.ID == cmd.LocationID {
			return candidateFromOption(option), nil
		}
	}
	return nil, errors.New("modify rental context: selected location is not an active pending option")
}

func matchPendingLocation(active *session.PendingInteraction, query string) *session.PendingOption {
	if active == nil || active.Type != session.PendingSelectLocation {
		return nil
	}
	query = normalizeLocationText(query)
	if query == "" {
		return nil
	}
	var matched *session.PendingOption
	for i := range active.Options {
		label := normalizeLocationText(active.Options[i].Label)
		address := normalizeLocationText(active.Options[i].Value)
		if label == "" || (!strings.Contains(label, query) && !strings.Contains(query, label) && (address == "" || !strings.Contains(address, query))) {
			continue
		}
		if matched != nil {
			return nil
		}
		matched = &active.Options[i]
	}
	return matched
}

func normalizeLocationText(value string) string {
	return strings.NewReplacer(" ", "", "，", "", ",", "", "。", "").Replace(strings.ToLower(strings.TrimSpace(value)))
}

func candidateFromOption(option session.PendingOption) *maps.Candidate {
	if option.Location == nil {
		return &maps.Candidate{ID: option.ID, Name: option.Label, Address: option.Value}
	}
	cityID, _ := strconv.Atoi(option.Location.CityID)
	return &maps.Candidate{ID: option.Location.ID, Name: option.Location.Name, Address: option.Location.Address, CityID: cityID, Latitude: option.Location.Latitude, Longitude: option.Location.Longitude}
}

// associateTimePending marks a resolved time command as the answer to the
// matching active clarification. The Pending is completed later, after all
// validation and ambiguity handling in Handle have succeeded.
func associateTimePending(active *session.PendingInteraction, cmd *ModifyRentalContextCommand) {
	if active == nil || cmd == nil {
		return
	}
	if active.Type == session.PendingClarifyPickupTime && cmd.PickupTime != nil {
		// The command supplied the pickup time requested by this Pending.
		cmd.InteractionID = active.ID
	}
	if active.Type == session.PendingClarifyReturnTime && cmd.ReturnTime != nil {
		// The command supplied the return time requested by this Pending.
		cmd.InteractionID = active.ID
	}
}

func (h *ModifyRentalContextHandler) buildExtractionInput(s *session.AgentSession, text string, now time.Time) *ExtractionInput {
	in := &ExtractionInput{SourceText: text, Now: now, Timezone: h.timezone.String(), CurrentState: CurrentRentalContext{PickupTime: s.Search.PickupTime, ReturnTime: s.Search.ReturnTime}}
	if s.Search.Location != nil {
		in.CurrentState.LocationName = s.Search.Location.Name
	}
	for _, text := range lastTwo(s.Memory.RecentRentalContextTexts) {
		in.RecentDomainHistory = append(in.RecentDomainHistory, DomainHistoryItem{UserText: text})
	}
	return in
}
func (h *ModifyRentalContextHandler) createLocationPending(s *session.AgentSession, cmd *ModifyRentalContextCommand, candidates []maps.Candidate, now time.Time) (*ModifyRentalContextResult, error) {
	if h.ids == nil {
		return nil, errors.New("modify rental context: interaction id generator is required")
	}
	if len(candidates) > h.ambiguity.MaxOptions {
		candidates = candidates[:h.ambiguity.MaxOptions]
	}
	options := make([]session.PendingOption, 0, len(candidates))
	for _, c := range candidates {
		location := locationRefFromCandidate(&c)
		options = append(options, session.PendingOption{ID: c.ID, Label: c.Name, Value: c.Address, Location: location})
	}
	p := &session.PendingInteraction{ID: h.ids.NewID(), Type: session.PendingSelectLocation, Status: session.PendingActive, Question: "找到多个相关地点，请确认具体地点。", Options: options, WorkflowName: string(session.ActionModifyRentalContext), Priority: 100, CreatedAt: now, ExpireAt: now.Add(h.ambiguity.PendingTTL), BaseVersion: s.Version, DependencyFingerprint: rentalFingerprint(s.Search), BlockingActions: []session.PendingAction{session.ActionExecuteVehicleSearch}, Context: session.PendingContext{LocationQuery: cmd.LocationQuery}}
	activated := s.Pending.Offer(p, &session.DeferredAction{ID: p.ID, Action: session.ActionModifyRentalContext, WorkflowName: p.WorkflowName, Reason: "location requires revalidation", EvidenceText: cmd.LocationQuery, BaseVersion: s.Version, DependencyFingerprint: p.DependencyFingerprint, CreatedAt: now})
	if !activated {
		return &ModifyRentalContextResult{Status: ResultDeferred, InteractionID: s.Pending.Active.ID}, nil
	}
	return &ModifyRentalContextResult{Status: ResultWaitingUser, InteractionID: p.ID, LocationOptions: candidates, Message: p.Question}, nil
}
func (h *ModifyRentalContextHandler) createTimePending(s *session.AgentSession, cmd *ModifyRentalContextCommand, field AmbiguousField, now time.Time) (*ModifyRentalContextResult, error) {
	if h.ids == nil {
		return nil, errors.New("modify rental context: interaction id generator is required")
	}
	typ := session.PendingClarifyPickupTime
	if field.Field == "return_time" {
		typ = session.PendingClarifyReturnTime
	}
	if s.Pending.Active != nil && s.Pending.Active.Type == typ {
		return &ModifyRentalContextResult{Status: ResultWaitingUser, InteractionID: s.Pending.Active.ID, Message: s.Pending.Active.Question}, nil
	}
	p := &session.PendingInteraction{ID: h.ids.NewID(), Type: typ, Status: session.PendingActive, Question: "请补充明确的" + field.Field, WorkflowName: string(session.ActionModifyRentalContext), Priority: 90, CreatedAt: now, ExpireAt: now.Add(h.ambiguity.PendingTTL), BaseVersion: s.Version, DependencyFingerprint: rentalFingerprint(s.Search), BlockingActions: []session.PendingAction{session.ActionExecuteVehicleSearch}, Context: session.PendingContext{AmbiguousField: field.Field, AmbiguousRaw: field.Raw}}
	activated := s.Pending.Offer(p, &session.DeferredAction{ID: p.ID, Action: session.ActionModifyRentalContext, WorkflowName: p.WorkflowName, Reason: "time requires revalidation", EvidenceText: field.Raw, BaseVersion: s.Version, DependencyFingerprint: p.DependencyFingerprint, CreatedAt: now})
	if !activated {
		return &ModifyRentalContextResult{Status: ResultDeferred, InteractionID: s.Pending.Active.ID}, nil
	}
	return &ModifyRentalContextResult{Status: ResultWaitingUser, InteractionID: p.ID, Message: p.Question}, nil
}

type preview struct {
	location    *session.LocationRef
	pickup, ret *time.Time
}

func buildPreview(state session.SearchState, cmd *ModifyRentalContextCommand, loc *maps.Candidate) preview {
	p := preview{state.Location, state.PickupTime, state.ReturnTime}
	if loc != nil {
		p.location = &session.LocationRef{ID: loc.ID, Name: loc.Name, Address: loc.Address, CityID: strconv.Itoa(loc.CityID), Latitude: loc.Latitude, Longitude: loc.Longitude}
	}
	if cmd.PickupTime != nil {
		p.pickup = cmd.PickupTime
	}
	if cmd.ReturnTime != nil {
		p.ret = cmd.ReturnTime
	}
	return p
}

func locationRefFromCandidate(candidate *maps.Candidate) *session.LocationRef {
	if candidate == nil {
		return nil
	}
	return &session.LocationRef{ID: candidate.ID, Name: candidate.Name, Address: candidate.Address, CityID: strconv.Itoa(candidate.CityID), Latitude: candidate.Latitude, Longitude: candidate.Longitude}
}

func rentalFingerprint(state session.SearchState) string {
	locationID := ""
	if state.Location != nil {
		locationID = state.Location.ID
	}
	pickup := ""
	if state.PickupTime != nil {
		pickup = state.PickupTime.Format(time.RFC3339Nano)
	}
	returnTime := ""
	if state.ReturnTime != nil {
		returnTime = state.ReturnTime.Format(time.RFC3339Nano)
	}
	return locationID + "|" + pickup + "|" + returnTime
}
func validateTimes(p preview, now time.Time) error {
	if p.pickup != nil && !p.pickup.After(now) {
		return ErrPickupTimeInPast
	}
	if p.ret != nil && !p.ret.After(now) {
		return ErrReturnTimeInPast
	}
	if p.pickup != nil && p.ret != nil && !p.ret.After(*p.pickup) {
		return ErrInvalidRentalTimeRange
	}
	return nil
}
func apply(s *session.AgentSession, cmd *ModifyRentalContextCommand, loc *maps.Candidate) []ModifiedField {
	var out []ModifiedField
	if loc != nil {
		v := &session.LocationRef{ID: loc.ID, Name: loc.Name, Address: loc.Address, CityID: strconv.Itoa(loc.CityID), Latitude: loc.Latitude, Longitude: loc.Longitude}
		if s.Search.Location == nil || s.Search.Location.ID != v.ID {
			s.Search.Location = v
			out = append(out, ModifiedLocation)
		}
	}
	if cmd.PickupTime != nil && (s.Search.PickupTime == nil || !s.Search.PickupTime.Equal(*cmd.PickupTime)) {
		s.Search.PickupTime = cmd.PickupTime
		out = append(out, ModifiedPickupTime)
	}
	if cmd.ReturnTime != nil && (s.Search.ReturnTime == nil || !s.Search.ReturnTime.Equal(*cmd.ReturnTime)) {
		s.Search.ReturnTime = cmd.ReturnTime
		out = append(out, ModifiedReturnTime)
	}
	return out
}
func success(state session.SearchState, modified []ModifiedField) *ModifyRentalContextResult {
	r := &ModifyRentalContextResult{Status: ResultSuccess, PickupTime: state.PickupTime, ReturnTime: state.ReturnTime, ModifiedFields: modified}
	if state.Location != nil {
		cityID, _ := strconv.Atoi(state.Location.CityID)
		r.Location = &maps.Candidate{ID: state.Location.ID, Name: state.Location.Name, Address: state.Location.Address, CityID: cityID, Latitude: state.Location.Latitude, Longitude: state.Location.Longitude}
	}
	return r
}
func appendHistory(s *session.AgentSession, text string) {
	if text == "" {
		return
	}
	s.Memory.RecentRentalContextTexts = append(s.Memory.RecentRentalContextTexts, text)
	if len(s.Memory.RecentRentalContextTexts) > 5 {
		s.Memory.RecentRentalContextTexts = s.Memory.RecentRentalContextTexts[len(s.Memory.RecentRentalContextTexts)-5:]
	}
}
func lastTwo(values []string) []string {
	if len(values) > 2 {
		return values[len(values)-2:]
	}
	return values
}
