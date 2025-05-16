package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/connection"
	domainConnection "github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type connectionRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts ConnectionQueryOptions
}

type ConnectionQueryOptions struct{}

func NewConnectionRepository(client postgres.IClient, log *logger.Logger) domainConnection.Repository {
	return &connectionRepository{
		client:    client,
		log:       log,
		queryOpts: ConnectionQueryOptions{},
	}
}

func (r *connectionRepository) Create(ctx context.Context, c *domainConnection.Connection) error {

	r.log.Debugw("creating connection",
		"connection_id", c.ID,
		"tenant_id", c.TenantID,
		"connection_code", c.ConnectionCode,
		"provider_type", c.ProviderType,
	)

	if c.EnvironmentID == "" {
		c.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	client := r.client.Querier(ctx)

	_, err := client.Connection.Create().
		SetName(c.Name).
		SetProviderType(string(c.ProviderType)).
		SetConnectionCode(c.ConnectionCode).
		SetMetadata(c.Metadata).
		SetStatus(string(c.Status)).
		SetCreatedAt(c.CreatedAt).
		SetUpdatedAt(c.UpdatedAt).
		SetCreatedBy(c.CreatedBy).
		SetUpdatedBy(c.UpdatedBy).
		SetTenantID(c.TenantID).
		SetSecretID(c.SecretID).
		Save(ctx)

	if err != nil {
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("An connection with this code and provider type already exists").
				WithReportableDetails(map[string]interface{}{
					"connection_code": c.ConnectionCode,
					"provider_type":   c.ProviderType,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create connection").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *connectionRepository) Get(ctx context.Context, id string) (*domainConnection.Connection, error) {
	r.log.Debugw("getting connection", "connection_id", id)
	client := r.client.Querier(ctx)

	conn, err := client.Connection.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Connection not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get connection").
			Mark(ierr.ErrDatabase)
	}

	return domainConnection.FromEnt(conn), nil
}

func (r *connectionRepository) GetByConnectionCode(ctx context.Context, connectionCode string) (*domainConnection.Connection, error) {
	client := r.client.Querier(ctx)

	conn, err := client.Connection.Query().
		Where(
			connection.TenantID(types.GetTenantID(ctx)),
			connection.ConnectionCode(connectionCode),
			connection.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		First(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Connection not found for connection code").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get connection by connection code").
			Mark(ierr.ErrDatabase)
	}

	return domainConnection.FromEnt(conn), nil
}

func (r *connectionRepository) GetByProviderType(ctx context.Context, providerType types.SecretProvider) (*domainConnection.Connection, error) {
	client := r.client.Querier(ctx)

	conn, err := client.Connection.Query().
		Where(
			connection.TenantID(types.GetTenantID(ctx)),
			connection.ProviderType(string(providerType)),
			connection.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		First(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Connection not found for provider type").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get connection by provider type").
			Mark(ierr.ErrDatabase)
	}

	return domainConnection.FromEnt(conn), nil
}

func (r *connectionRepository) List(ctx context.Context, filter *types.ConnectionFilter) ([]*domainConnection.Connection, error) {
	client := r.client.Querier(ctx)

	query := client.Connection.Query()

	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply sorting
	if filter != nil {
		order := filter.GetOrder()
		sortField := filter.GetSort()

		if order == "desc" {
			query = query.Order(ent.Desc(sortField))
		} else {
			query = query.Order(ent.Asc(sortField))
		}
	}

	// Apply pagination
	if filter != nil && !filter.IsUnlimited() {
		limit := filter.GetLimit()
		offset := filter.GetOffset()

		query = query.Limit(limit).Offset(offset)
	}

	connections, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list connections").
			Mark(ierr.ErrDatabase)
	}

	return domainConnection.FromEntList(connections), nil
}

func (r *connectionRepository) Count(ctx context.Context, filter *types.ConnectionFilter) (int, error) {
	client := r.client.Querier(ctx)

	query := client.Connection.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count connections").
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *connectionRepository) Update(ctx context.Context, c *domainConnection.Connection) error {
	client := r.client.Querier(ctx)

	_, err := client.Connection.UpdateOneID(c.ID).
		Where(
			connection.ID(c.ID),
			connection.TenantID(c.TenantID),
			connection.EnvironmentID(c.EnvironmentID),
		).
		SetName(c.Name).
		SetConnectionCode(c.ConnectionCode).
		SetMetadata(c.Metadata).
		SetStatus(string(c.Status)).
		SetUpdatedAt(c.UpdatedAt).
		SetUpdatedBy(c.UpdatedBy).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Connection not found").
				Mark(ierr.ErrNotFound)
		}
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("An connection with this code and provider type already exists").
				WithReportableDetails(map[string]interface{}{
					"connection_code": c.ConnectionCode,
					"provider_type":   c.ProviderType,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to update connection").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *connectionRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	err := client.Connection.DeleteOneID(id).Exec(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete connection").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (o ConnectionQueryOptions) ApplyTenantFilter(ctx context.Context, query *ent.ConnectionQuery) *ent.ConnectionQuery {
	return query.Where(connection.TenantIDEQ(types.GetTenantID(ctx)))
}

func (o ConnectionQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query *ent.ConnectionQuery) *ent.ConnectionQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(connection.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o ConnectionQueryOptions) ApplyStatusFilter(query *ent.ConnectionQuery, status string) *ent.ConnectionQuery {
	if status == "" {
		return query.Where(connection.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(connection.Status(status))
}

func (o ConnectionQueryOptions) ApplySortFilter(query *ent.ConnectionQuery, field string, order string) *ent.ConnectionQuery {
	if field != "" {
		if order == types.OrderDesc {
			query = query.Order(ent.Desc(o.GetFieldName(field)))
		} else {
			query = query.Order(ent.Asc(o.GetFieldName(field)))
		}
	}
	return query
}

func (o ConnectionQueryOptions) ApplyPaginationFilter(query *ent.ConnectionQuery, limit int, offset int) *ent.ConnectionQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o ConnectionQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return connection.FieldCreatedAt
	case "updated_at":
		return connection.FieldUpdatedAt
	case "name":
		return connection.FieldName
	case "connection_code":
		return connection.FieldConnectionCode
	case "provider_type":
		return connection.FieldProviderType
	case "status":
		return connection.FieldStatus
	default:
		return connection.FieldCreatedAt
	}
}

func (o ConnectionQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.ConnectionFilter, query *ent.ConnectionQuery) *ent.ConnectionQuery {
	if f == nil {
		return query
	}

	if f.ConnectionCode != "" {
		query = query.Where(connection.ConnectionCode(f.ConnectionCode))
	}

	if f.ProviderType != "" {
		query = query.Where(connection.ProviderType(string(f.ProviderType)))
	}

	if f.Status != nil {
		query = query.Where(connection.StatusIn(lo.Map(f.Status, func(status types.Status, _ int) string {
			return string(status)
		})...))
	}

	return query
}
