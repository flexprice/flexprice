package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/activity"
	"github.com/flexprice/flexprice/internal/api/dto"
	domainActivity "github.com/flexprice/flexprice/internal/domain/activity"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/oklog/ulid/v2"
	"github.com/samber/lo"
)

const (
	activityDefaultWindow = 30 * 24 * time.Hour
	activityMaxWindow     = 90 * 24 * time.Hour
	activityDefaultLimit  = 50
	activityMaxLimit      = 100
	activityMaxStateLimit = 50
	activityIDPrefix      = "act_"
)

var activityActorTypes = []string{
	string(types.ActorTypeUser), string(types.ActorTypeAPIKey), string(types.ActorTypeCustomer), string(types.ActorTypeSystem),
}

// ActivityService reads the activity log.
type ActivityService interface {
	List(ctx context.Context, req *dto.ListActivityRequest) (*dto.ListActivityResponse, error)
	Get(ctx context.Context, id string) (*dto.ActivityResponse, error)
}

type activityService struct {
	ServiceParams
	registry *activity.Registry
}

func NewActivityService(params ServiceParams) ActivityService {
	return &activityService{ServiceParams: params, registry: activity.Registrations()}
}

func activityValidation(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return ierr.NewError(msg).WithHint(msg).Mark(ierr.ErrValidation)
}

func (s *activityService) List(ctx context.Context, req *dto.ListActivityRequest) (*dto.ListActivityResponse, error) {
	now := time.Now().UTC()
	query, limit, err := s.buildQuery(req, now)
	if err != nil {
		return nil, err
	}
	withState := slices.Contains(req.Expand, "state")

	rows, err := s.ActivityRepo.List(ctx, query)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	var states map[string]string
	if withState && len(rows) > 0 {
		ids := make([]string, len(rows))
		minAt, maxAt := rows[0].OccurredAt, rows[0].OccurredAt
		for i, row := range rows {
			ids[i] = row.ID
			if row.OccurredAt.Before(minAt) {
				minAt = row.OccurredAt
			}
			if row.OccurredAt.After(maxAt) {
				maxAt = row.OccurredAt
			}
		}
		if states, err = s.ActivityRepo.States(ctx, ids, minAt, maxAt); err != nil {
			return nil, err
		}
	}

	resp := &dto.ListActivityResponse{Items: make([]*dto.ActivityResponse, 0, len(rows)), Pagination: dto.ActivityPagination{Limit: limit, HasMore: hasMore}}
	for _, row := range rows {
		item := toActivityResponse(row)
		if withState {
			item.State = rawState(states[row.ID])
		}
		resp.Items = append(resp.Items, item)
	}
	if hasMore {
		last := rows[len(rows)-1]
		next := encodeCursor(domainActivity.Cursor{OccurredAt: last.OccurredAt, ID: last.ID})
		resp.Pagination.NextCursor = &next
	}
	return resp, nil
}

func (s *activityService) Get(ctx context.Context, id string) (*dto.ActivityResponse, error) {
	parsed, err := ulid.ParseStrict(strings.TrimPrefix(id, activityIDPrefix))
	if !strings.HasPrefix(id, activityIDPrefix) || err != nil {
		return nil, activityValidation("%q is not a valid activity id.", id)
	}
	from := ulid.Time(parsed.Time()).UTC()
	row, err := s.ActivityRepo.Get(ctx, id, from, from.Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	if row == nil {
		msg := fmt.Sprintf("Activity %s was not found.", id)
		return nil, ierr.NewError(msg).WithHint(msg).Mark(ierr.ErrNotFound)
	}
	item := toActivityResponse(row)
	item.State = rawState(row.State)
	return item, nil
}

// buildQuery validates req and returns the repository query and the page size.
func (s *activityService) buildQuery(req *dto.ListActivityRequest, now time.Time) (*domainActivity.ListQuery, int, error) {
	from, to, err := resolveWindow(now, req.StartTime, req.EndTime)
	if err != nil {
		return nil, 0, err
	}
	if err := s.validateFilters(req); err != nil {
		return nil, 0, err
	}
	limit, err := resolveLimit(req.Limit, slices.Contains(req.Expand, "state"))
	if err != nil {
		return nil, 0, err
	}
	var after *domainActivity.Cursor
	if req.Cursor != "" {
		if after, err = decodeCursor(req.Cursor); err != nil {
			return nil, 0, err
		}
	}
	return &domainActivity.ListQuery{
		From: from, To: to, EntityType: req.EntityType, EntityID: req.EntityID, CustomerID: req.CustomerID,
		ActorID: req.ActorID, RequestID: req.RequestID, ActorTypes: req.ActorTypes, Actions: req.Actions,
		After: after, Limit: limit + 1,
	}, limit, nil
}

func (s *activityService) validateFilters(req *dto.ListActivityRequest) error {
	if req.EntityID != "" && req.EntityType == "" {
		return activityValidation("entity_type is required when entity_id is set.")
	}
	if req.EntityType != "" {
		if _, ok := s.registry.ByType(types.SystemEntityType(req.EntityType)); !ok {
			return activityValidation("Unknown entity_type %q.", req.EntityType)
		}
	}
	for _, actorType := range req.ActorTypes {
		if !slices.Contains(activityActorTypes, actorType) {
			return activityValidation("Unknown actor type %q. Allowed actor types: %s.", actorType, strings.Join(activityActorTypes, ", "))
		}
	}
	for _, action := range req.Actions {
		if s.registry.IsAction(activity.Action(action)) {
			continue
		}
		entity, _, _ := strings.Cut(action, ".")
		if allowed := s.registry.AllowedActions(types.SystemEntityType(entity)); len(allowed) > 0 {
			names := lo.Map(allowed, func(a activity.Action, _ int) string { return string(a) })
			return activityValidation("Unknown action %q. Allowed %s actions: %s.", action, entity, strings.Join(names, ", "))
		}
		return activityValidation("Unknown action %q. Actions are named <entity>.<verb>, for example invoice.voided.", action)
	}
	for _, expand := range req.Expand {
		if expand != "state" {
			return activityValidation("Unknown expand %q. Allowed values: state.", expand)
		}
	}
	return nil
}

// resolveWindow returns the [from, to) range for the optional start and end times.
func resolveWindow(now time.Time, start, end *time.Time) (from, to time.Time, err error) {
	switch {
	case start == nil && end == nil:
		from, to = now.Add(-activityDefaultWindow), now
	case end == nil:
		from, to = start.UTC(), now
	case start == nil:
		from, to = end.UTC().Add(-activityDefaultWindow), end.UTC()
	default:
		from, to = start.UTC(), end.UTC()
		if !to.After(from) {
			return from, to, activityValidation("end_time must be after start_time.")
		}
	}
	if oldest := now.AddDate(-1, 0, 0); from.Before(oldest) {
		return from, to, activityValidation("Activity is available for the last year. Set start_time on or after %s.", oldest.Format("2006-01-02"))
	}
	if to.Sub(from) > activityMaxWindow {
		return from, to, activityValidation("The time range can be at most 90 days. Narrow start_time and end_time.")
	}
	return from, to, nil
}

func resolveLimit(limit int, withState bool) (int, error) {
	maxLimit := activityMaxLimit
	if withState {
		maxLimit = activityMaxStateLimit
	}
	if limit == 0 {
		return activityDefaultLimit, nil
	}
	if limit < 0 || limit > maxLimit {
		return 0, activityValidation("limit can be at most 100 (50 with expand state).")
	}
	return limit, nil
}

func encodeCursor(c domainActivity.Cursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s string) (*domainActivity.Cursor, error) {
	invalid := activityValidation("The cursor is invalid. Start again without a cursor.")
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, invalid
	}
	var c domainActivity.Cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.ID == "" || c.OccurredAt.IsZero() {
		return nil, invalid
	}
	return &c, nil
}

func rawState(state string) *json.RawMessage {
	raw := json.RawMessage("null")
	if state != "" {
		raw = json.RawMessage(state)
	}
	return &raw
}

func toActivityResponse(l *domainActivity.Log) *dto.ActivityResponse {
	return &dto.ActivityResponse{
		ID: l.ID, OccurredAt: l.OccurredAt, Action: l.Action,
		Entity:     dto.ActivityEntity{Type: l.Entity.Type, ID: l.Entity.ID, Label: l.Entity.Label},
		CustomerID: l.CustomerID,
		Actor:      dto.ActivityActor{Type: l.Actor.Type, ID: l.Actor.ID, Label: l.Actor.Label},
		Source:     l.Request.Source, RequestID: l.Request.ID, IP: l.Request.IP, UserAgent: l.Request.UserAgent,
	}
}
