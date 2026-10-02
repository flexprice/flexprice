package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/activity"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/activitylog"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/rbac"
	entrepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type ActivityLogService interface {
	List(ctx context.Context, f *types.ActivityFilter) (*dto.ListActivityResponse, error)
	Get(ctx context.Context, id string) (*dto.ActivityResponse, error)
}

type activityLogService struct {
	ServiceParams
	repo        activitylog.Repository
	reg         *activity.Registry
	rbacService *rbac.RBACService
}

func NewActivityLogService(params ServiceParams, repo activitylog.Repository, reg *activity.Registry, rbacService *rbac.RBACService) ActivityLogService {
	return &activityLogService{ServiceParams: params, repo: repo, reg: reg, rbacService: rbacService}
}

func (s *activityLogService) List(ctx context.Context, f *types.ActivityFilter) (*dto.ListActivityResponse, error) {
	if f == nil {
		f = &types.ActivityFilter{}
	}
	if (f.EntityID != nil) != (f.EntityType != nil) {
		return nil, ierr.NewError("entity_type and entity_id must be given together").Mark(ierr.ErrValidation)
	}
	if f.EntityType != nil && !s.rbacService.HasPermission(types.GetRoles(ctx), *f.EntityType, string(types.ActionRead)) {
		return nil, ierr.NewError("insufficient permissions for entity type").
			WithHintf("caller cannot read entity type %q", *f.EntityType).
			Mark(ierr.ErrPermissionDenied)
	}
	if f.StartTime == nil && f.EndTime == nil {
		start := time.Now().UTC().AddDate(0, 0, -s.Config.Activity.HotWindowDays)
		f.StartTime = &start
	}
	rows, next, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, err
	}
	items := make([]*dto.ActivityResponse, len(rows))
	for i, r := range rows {
		items[i] = s.decorate(r)
	}
	return &dto.ListActivityResponse{Items: items, NextCursor: entrepo.EncodeCursor(next), HasMore: next != nil}, nil
}

func (s *activityLogService) Get(ctx context.Context, id string) (*dto.ActivityResponse, error) {
	r, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.decorate(r), nil
}

func (s *activityLogService) decorate(r *activitylog.ActivityLog) *dto.ActivityResponse {
	out := dto.NewActivityResponse(r)
	def, _ := s.reg.ByEntityType(types.SystemEntityType(r.EntityType))
	entityLabel := r.EntityLabel
	if entityLabel == "" {
		entityLabel = r.EntityID
	}
	actorLabel := r.ActorLabel
	if actorLabel == "" {
		actorLabel = r.ActorType
	}
	out.Changes = activity.AnnotateChanges(def, r.Changes)
	out.Metadata = activity.AnnotateMetadata(def, r.Metadata)
	in := activity.SummaryInput{ActorLabel: actorLabel, EntityLabel: entityLabel, Action: r.Action, Changes: r.Changes}
	parts := dto.ActivityDisplayParts{Actor: actorLabel, Verb: r.Action[strings.LastIndex(r.Action, ".")+1:], EntityType: r.EntityType, Entity: entityLabel, Count: len(r.Changes)}
	if len(r.Changes) == 1 {
		for field, raw := range r.Changes {
			ch, _ := raw.(map[string]any)
			label := activity.Humanize(field)
			if fd, ok := def.FieldLabels[field]; ok {
				label = fd.Label
			}
			parts.Field, parts.From, parts.To = &label, ch["from"], ch["to"]
		}
	}
	out.Display = dto.ActivityDisplay{Summary: activity.Summary(def, in), EntityLabel: entityLabel, Parts: parts}
	return out
}
