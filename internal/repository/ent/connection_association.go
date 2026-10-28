package ent

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/connectionassociation"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/cache"
	domainConnectionAssociation "github.com/flexprice/flexprice/internal/domain/connectionassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type connectionAssociationRepository struct {
	client     postgres.IClient
	log        *logger.Logger
	queryOpts  ConnectionAssociationQueryOptions
	redisCache cache.RedisCache
}

func NewConnectionAssociationRepository(client postgres.IClient, log *logger.Logger, redisCache cache.RedisCache) domainConnectionAssociation.Repository {
	return &connectionAssociationRepository{
		client:     client,
		log:        log,
		queryOpts:  ConnectionAssociationQueryOptions{},
		redisCache: redisCache,
	}
}

var connectionAssociationConflictHints = map[string]string{
	schema.ConnectionAssociationOwnerConnectionUnique: "This connection already belongs to a billing entity",
	schema.ConnectionAssociationOwnerProviderUnique:   "This billing entity already has a connection for this provider",
	schema.ConnectionAssociationCustomerUnique:        "This customer already has a preferred connection",
}

func (r *connectionAssociationRepository) Create(ctx context.Context, a *domainConnectionAssociation.ConnectionAssociation) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "connection_association", "create", map[string]interface{}{
		"connection_association_id": a.ID,
		"entity_type":               a.EntityType,
		"entity_id":                 a.EntityID,
		"connection_id":             a.ConnectionID,
	})
	defer FinishSpan(span)

	if a.EnvironmentID == "" {
		a.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	created, err := client.ConnectionAssociation.Create().
		SetID(a.ID).
		SetTenantID(a.TenantID).
		SetEnvironmentID(a.EnvironmentID).
		SetEntityType(string(a.EntityType)).
		SetEntityID(a.EntityID).
		SetConnectionID(a.ConnectionID).
		SetProviderType(string(a.ProviderType)).
		SetStatus(string(a.Status)).
		SetCreatedAt(a.CreatedAt).
		SetUpdatedAt(a.UpdatedAt).
		SetCreatedBy(a.CreatedBy).
		SetUpdatedBy(a.UpdatedBy).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		details := map[string]any{
			"entity_type":   a.EntityType,
			"entity_id":     a.EntityID,
			"connection_id": a.ConnectionID,
			"provider_type": a.ProviderType,
		}

		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if hint, ok := connectionAssociationConflictHints[pqErr.Constraint]; ok {
					return ierr.WithError(err).
						WithHint(hint).
						WithReportableDetails(details).
						Mark(ierr.ErrAlreadyExists)
				}
			}
			return ierr.WithError(err).
				WithHint("Failed to create connection association").
				WithReportableDetails(details).
				Mark(ierr.ErrValidation)
		}
		return ierr.WithError(err).
			WithHint("Failed to create connection association").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	*a = *domainConnectionAssociation.FromEnt(created)
	r.deleteConnectionCaches(ctx, a.ConnectionID)
	return nil
}

func (r *connectionAssociationRepository) Get(ctx context.Context, id string) (*domainConnectionAssociation.ConnectionAssociation, error) {
	span := StartRepositorySpan(ctx, "connection_association", "get", map[string]interface{}{
		"connection_association_id": id,
	})
	defer FinishSpan(span)

	a, err := r.client.Reader(ctx).ConnectionAssociation.Query().
		Where(
			connectionassociation.ID(id),
			connectionassociation.TenantID(types.GetTenantID(ctx)),
			connectionassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Connection association with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"connection_association_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get connection association").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainConnectionAssociation.FromEnt(a), nil
}

func (r *connectionAssociationRepository) List(ctx context.Context, filter *types.ConnectionAssociationFilter) ([]*domainConnectionAssociation.ConnectionAssociation, error) {
	span := StartRepositorySpan(ctx, "connection_association", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := r.client.Reader(ctx).ConnectionAssociation.Query()
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(filter, query)

	associations, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list connection associations").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainConnectionAssociation.FromEntList(associations), nil
}

func (r *connectionAssociationRepository) Delete(ctx context.Context, a *domainConnectionAssociation.ConnectionAssociation) error {
	span := StartRepositorySpan(ctx, "connection_association", "delete", map[string]interface{}{
		"connection_association_id": a.ID,
	})
	defer FinishSpan(span)

	_, err := r.client.Writer(ctx).ConnectionAssociation.UpdateOneID(a.ID).
		Where(
			connectionassociation.TenantID(types.GetTenantID(ctx)),
			connectionassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(a.UpdatedAt).
		SetUpdatedBy(a.UpdatedBy).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Connection association with ID %s was not found", a.ID).
				WithReportableDetails(map[string]any{
					"connection_association_id": a.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete connection association").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.deleteConnectionCaches(ctx, a.ConnectionID)
	return nil
}

// deleteConnectionCaches drops the cached connection reads that carry the owning billing entity.
func (r *connectionAssociationRepository) deleteConnectionCaches(ctx context.Context, connectionID string) {
	afterCommit(ctx, func() {
		r.redisCache.Delete(ctx, cache.GenerateKey(ctx, cache.PrefixPublishedConnections))
		r.redisCache.Delete(ctx, cache.GenerateKey(ctx, cache.PrefixConnection, connectionID))
	})
}

type ConnectionAssociationQuery = *ent.ConnectionAssociationQuery

type ConnectionAssociationQueryOptions struct{}

func (o ConnectionAssociationQueryOptions) ApplyTenantFilter(ctx context.Context, query ConnectionAssociationQuery) ConnectionAssociationQuery {
	return query.Where(connectionassociation.TenantID(types.GetTenantID(ctx)))
}

func (o ConnectionAssociationQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query ConnectionAssociationQuery) ConnectionAssociationQuery {
	envID := types.GetEnvironmentID(ctx)
	if envID != "" {
		return query.Where(connectionassociation.EnvironmentID(envID))
	}
	return query
}

func (o ConnectionAssociationQueryOptions) ApplyStatusFilter(query ConnectionAssociationQuery, status string) ConnectionAssociationQuery {
	if status == "" {
		return query.Where(connectionassociation.StatusEQ(string(types.StatusPublished)))
	}
	return query.Where(connectionassociation.Status(status))
}

func (o ConnectionAssociationQueryOptions) ApplySortFilter(query ConnectionAssociationQuery, field, order string) ConnectionAssociationQuery {
	if field != "" {
		if order == types.OrderDesc {
			query = query.Order(ent.Desc(o.GetFieldName(field)))
		} else {
			query = query.Order(ent.Asc(o.GetFieldName(field)))
		}
	}
	return query
}

func (o ConnectionAssociationQueryOptions) ApplyPaginationFilter(query ConnectionAssociationQuery, limit, offset int) ConnectionAssociationQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o ConnectionAssociationQueryOptions) GetFieldName(field string) string {
	if connectionassociation.ValidColumn(field) {
		return field
	}
	return ""
}

func (o ConnectionAssociationQueryOptions) applyEntityQueryOptions(f *types.ConnectionAssociationFilter, query ConnectionAssociationQuery) ConnectionAssociationQuery {
	if f == nil {
		return query
	}
	if f.EntityType != "" {
		query = query.Where(connectionassociation.EntityType(string(f.EntityType)))
	}
	if len(f.EntityIDs) > 0 {
		query = query.Where(connectionassociation.EntityIDIn(f.EntityIDs...))
	}
	if len(f.ConnectionIDs) > 0 {
		query = query.Where(connectionassociation.ConnectionIDIn(f.ConnectionIDs...))
	}
	if f.ProviderType != "" {
		query = query.Where(connectionassociation.ProviderType(string(f.ProviderType)))
	}
	return query
}
