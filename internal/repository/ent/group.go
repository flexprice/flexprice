package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/group"
	"github.com/flexprice/flexprice/internal/cache"
	domainGroup "github.com/flexprice/flexprice/internal/domain/group"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type groupRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	cache     cache.Cache
	queryOpts GroupQueryOptions
}

func NewGroupRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainGroup.Repository {
	return &groupRepository{
		client:    client,
		log:       log,
		cache:     cache,
		queryOpts: GroupQueryOptions{},
	}
}

func (r *groupRepository) Create(ctx context.Context, grp *domainGroup.Group) error {
	client := r.client.Writer(ctx)
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	_, err := client.Group.Create().
		SetID(grp.ID).
		SetName(grp.Name).
		SetEntityType(string(grp.EntityType)).
		SetLookupKey(grp.LookupKey).
		SetTenantID(tenantID).
		SetEnvironmentID(environmentID).
		SetStatus(string(grp.Status)).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		r.log.Error("Failed to create group", "error", err, "group_id", grp.ID)
		return ierr.WithError(err).
			WithHint("Failed to create group").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *groupRepository) Get(ctx context.Context, id string) (*domainGroup.Group, error) {
	client := r.client.Reader(ctx)
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	entGroup, err := client.Group.Query().
		Where(
			group.IDEQ(id),
			group.TenantIDEQ(tenantID),
			group.EnvironmentIDEQ(environmentID),
			group.StatusEQ(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Group not found").
				Mark(ierr.ErrNotFound)
		}
		r.log.Error("Failed to get group", "error", err, "group_id", id)
		return nil, ierr.WithError(err).
			WithHint("Failed to get group").
			Mark(ierr.ErrDatabase)
	}

	return r.toDomainGroup(entGroup), nil
}

func (r *groupRepository) GetByLookupKey(ctx context.Context, lookupKey string) (*domainGroup.Group, error) {
	client := r.client.Reader(ctx)
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	entGroup, err := client.Group.Query().
		Where(
			group.LookupKeyEQ(lookupKey),
			group.TenantIDEQ(tenantID),
			group.EnvironmentIDEQ(environmentID),
			group.StatusEQ(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Group not found").
				Mark(ierr.ErrNotFound)
		}
		r.log.Error("Failed to get group by lookup key", "error", err, "lookup_key", lookupKey)
		return nil, ierr.WithError(err).
			WithHint("Failed to get group by lookup key").
			Mark(ierr.ErrDatabase)
	}

	return r.toDomainGroup(entGroup), nil
}

func (r *groupRepository) List(ctx context.Context, filter *types.GroupFilter) ([]*domainGroup.Group, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "group", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Group.Query()
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list groups").
			Mark(ierr.ErrDatabase)
	}
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	entGroups, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		r.log.Error("Failed to list groups", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list groups").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	groups := make([]*domainGroup.Group, len(entGroups))
	for i, entGroup := range entGroups {
		groups[i] = r.toDomainGroup(entGroup)
	}

	return groups, nil
}

func (r *groupRepository) Count(ctx context.Context, filter *types.GroupFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "group", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Group.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count groups").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *groupRepository) Update(ctx context.Context, grp *domainGroup.Group) error {
	client := r.client.Writer(ctx)
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	_, err := client.Group.UpdateOneID(grp.ID).
		Where(
			group.TenantIDEQ(tenantID),
			group.EnvironmentIDEQ(environmentID),
		).
		SetName(grp.Name).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		r.log.Error("Failed to update group", "error", err, "group_id", grp.ID)
		return ierr.WithError(err).
			WithHint("Failed to update group").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *groupRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Soft delete by updating status
	_, err := client.Group.UpdateOneID(id).
		Where(
			group.TenantIDEQ(tenantID),
			group.EnvironmentIDEQ(environmentID),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		r.log.Error("Failed to delete group", "error", err, "group_id", id)
		return ierr.WithError(err).
			WithHint("Failed to delete group").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *groupRepository) toDomainGroup(entGroup *ent.Group) *domainGroup.Group {
	return &domainGroup.Group{
		ID:            entGroup.ID,
		Name:          entGroup.Name,
		EntityType:    types.GroupEntityType(entGroup.EntityType),
		EnvironmentID: entGroup.EnvironmentID,
		LookupKey:     entGroup.LookupKey,
		BaseModel: types.BaseModel{
			TenantID:  entGroup.TenantID,
			Status:    types.Status(entGroup.Status),
			CreatedAt: entGroup.CreatedAt,
			UpdatedAt: entGroup.UpdatedAt,
			CreatedBy: entGroup.CreatedBy,
			UpdatedBy: entGroup.UpdatedBy,
		},
	}
}

// GroupQuery type alias for better readability
type GroupQuery = *ent.GroupQuery

// GroupQueryOptions implements BaseQueryOptions for group queries
type GroupQueryOptions struct{}

func (o GroupQueryOptions) ApplyTenantFilter(ctx context.Context, query GroupQuery) GroupQuery {
	return query.Where(group.TenantIDEQ(types.GetTenantID(ctx)))
}

func (o GroupQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query GroupQuery) GroupQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(group.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o GroupQueryOptions) ApplyStatusFilter(query GroupQuery, status string) GroupQuery {
	if status == "" {
		return query.Where(group.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(group.Status(status))
}

func (o GroupQueryOptions) ApplySortFilter(query GroupQuery, field string, order string) GroupQuery {
	if field != "" {
		if order == types.OrderDesc {
			query = query.Order(ent.Desc(o.GetFieldName(field)))
		} else {
			query = query.Order(ent.Asc(o.GetFieldName(field)))
		}
	}
	return query
}

func (o GroupQueryOptions) ApplyPaginationFilter(query GroupQuery, limit int, offset int) GroupQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o GroupQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return group.FieldCreatedAt
	case "updated_at":
		return group.FieldUpdatedAt
	case "name":
		return group.FieldName
	case "entity_type":
		return group.FieldEntityType
	case "lookup_key":
		return group.FieldLookupKey
	case "status":
		return group.FieldStatus
	default:
		//unknown field
		return ""
	}
}

func (o GroupQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in group query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o GroupQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.GroupFilter, query GroupQuery) (GroupQuery, error) {
	if f == nil {
		return query.Where(
			group.TenantIDEQ(types.GetTenantID(ctx)),
			group.EnvironmentIDEQ(types.GetEnvironmentID(ctx)),
			group.StatusEQ(string(types.StatusPublished)),
		), nil
	}

	// Apply base filters
	query = query.Where(
		group.TenantIDEQ(types.GetTenantID(ctx)),
		group.EnvironmentIDEQ(types.GetEnvironmentID(ctx)),
		group.StatusEQ(string(types.StatusPublished)),
	)

	// Apply entity-specific filters
	if f.EntityType != "" {
		query = query.Where(group.EntityTypeEQ(f.EntityType))
	}
	if f.Name != "" {
		query = query.Where(group.NameContains(f.Name))
	}
	if f.LookupKey != "" {
		query = query.Where(group.LookupKeyEQ(f.LookupKey))
	}
	if len(f.GroupIDs) > 0 {
		query = query.Where(group.IDIn(f.GroupIDs...))
	}

	return query, nil
}
