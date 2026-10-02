package ent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/activitylog"
	domain "github.com/flexprice/flexprice/internal/domain/activitylog"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type activityLogRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewActivityLogRepository(client postgres.IClient, log *logger.Logger) domain.Repository {
	return &activityLogRepository{client: client, log: log}
}

func encodeActivityCursor(c types.ActivityCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeActivityCursor(s string) (*types.ActivityCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var c types.ActivityCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.ID == "" || c.OccurredAt.IsZero() {
		return nil, fmt.Errorf("invalid cursor")
	}
	return &c, nil
}

func (r *activityLogRepository) List(ctx context.Context, f *types.ActivityFilter) ([]*domain.ActivityLog, *types.ActivityCursor, error) {
	limit := f.GetLimit()
	q := r.client.Reader(ctx).ActivityLog.Query().
		Where(
			activitylog.TenantID(types.GetTenantID(ctx)),
			activitylog.EnvironmentID(types.GetEnvironmentID(ctx)),
		)
	if f.EntityType != nil {
		q = q.Where(activitylog.EntityType(*f.EntityType))
	}
	if f.EntityID != nil {
		q = q.Where(activitylog.EntityID(*f.EntityID))
	}
	if f.CustomerID != nil {
		q = q.Where(activitylog.CustomerID(*f.CustomerID))
	}
	if f.SubscriptionID != nil {
		q = q.Where(activitylog.SubscriptionID(*f.SubscriptionID))
	}
	if len(f.ExcludeActorTypes) > 0 {
		q = q.Where(activitylog.ActorTypeNotIn(f.ExcludeActorTypes...))
	}
	if f.ActorType != nil {
		q = q.Where(activitylog.ActorType(*f.ActorType))
	}
	if f.ActorID != nil {
		q = q.Where(activitylog.ActorID(*f.ActorID))
	}
	if len(f.Actions) > 0 {
		q = q.Where(activitylog.ActionIn(f.Actions...))
	}
	if f.RequestID != nil {
		q = q.Where(activitylog.RequestID(*f.RequestID))
	}
	start, end := timeBounds(f, time.Now().UTC())
	// A related-changes lookup passes the parent row's day as start/end so it prunes to one partition.
	q = q.Where(activitylog.OccurredAtGTE(start), activitylog.OccurredAtLT(end))
	if f.Cursor != nil && *f.Cursor != "" {
		c, err := decodeActivityCursor(*f.Cursor)
		if err != nil {
			return nil, nil, ierr.WithError(err).WithHint("invalid cursor").Mark(ierr.ErrValidation)
		}
		q = q.Where(func(s *entsql.Selector) {
			s.Where(entsql.P(func(b *entsql.Builder) {
				b.WriteString("(").Ident(activitylog.FieldOccurredAt).WriteString(", ").Ident(activitylog.FieldID).
					WriteString(") < (").Arg(c.OccurredAt).WriteString(", ").Arg(c.ID).WriteString(")")
			}))
		})
	}
	rows, err := q.Order(ent.Desc(activitylog.FieldOccurredAt), ent.Desc(activitylog.FieldID)).Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, nil, ierr.WithError(err).WithHint("listing activity failed").Mark(ierr.ErrDatabase)
	}
	var next *types.ActivityCursor
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = &types.ActivityCursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
	out := make([]*domain.ActivityLog, len(rows))
	for i, e := range rows {
		out[i] = domain.FromEnt(e)
	}
	return out, next, nil
}

// timeBounds defaults an unbounded query to the hot window so partition
// pruning always applies. The service sets f.StartTime from config.Activity.HotWindowDays
// before calling List when neither bound is given; the 90-day fallback here only
// guards a caller that bypasses that default.
func timeBounds(f *types.ActivityFilter, now time.Time) (time.Time, time.Time) {
	end := now.Add(time.Minute)
	if f.EndTime != nil {
		end = *f.EndTime
	}
	start := now.AddDate(0, 0, -90)
	if f.StartTime != nil {
		start = *f.StartTime
	}
	return start, end
}

func (r *activityLogRepository) Get(ctx context.Context, id string) (*domain.ActivityLog, error) {
	e, err := r.client.Reader(ctx).ActivityLog.Query().
		Where(
			activitylog.ID(id),
			activitylog.TenantID(types.GetTenantID(ctx)),
			activitylog.EnvironmentID(types.GetEnvironmentID(ctx)),
		).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).WithHint("activity entry not found").Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("fetching activity failed").Mark(ierr.ErrDatabase)
	}
	return domain.FromEnt(e), nil
}

// EncodeCursor is exported for the service's response building.
func EncodeCursor(c *types.ActivityCursor) *string {
	if c == nil {
		return nil
	}
	s := encodeActivityCursor(*c)
	return &s
}
