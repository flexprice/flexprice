package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/integrationentity"
	domainIntegration "github.com/flexprice/flexprice/internal/domain/integration"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// integrationRepository is the implementation of domainIntegration.Repository using Ent.
type integrationRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewIntegrationRepository creates a new instance of integrationRepository.
func NewIntegrationRepository(client postgres.IClient, logger *logger.Logger) domainIntegration.Repository {
	return &integrationRepository{
		client: client,
		logger: logger,
	}
}

// Create creates a new IntegrationEntity.
func (r *integrationRepository) Create(ctx context.Context, ie *domainIntegration.IntegrationEntity) error {
	// Set tenant ID from context if not already set
	if ie.TenantID == "" {
		ie.TenantID = types.GetTenantID(ctx)
	}

	querier := r.client.Querier(ctx)

	builder := querier.IntegrationEntity.Create().
		SetID(ie.ID).
		SetTenantID(ie.TenantID).
		SetStatus(string(ie.Status)).
		SetEntityType(ie.EntityType).
		SetEntityID(ie.EntityID).
		SetProviderType(ie.ProviderType).
		SetSyncStatus(ie.SyncStatus).
		SetEnvironmentID(ie.EnvironmentID).
		SetMetadata(ie.Metadata)

	if ie.CreatedBy != "" {
		builder.SetCreatedBy(ie.CreatedBy)
	}

	if ie.UpdatedBy != "" {
		builder.SetUpdatedBy(ie.UpdatedBy)
	}

	if ie.ProviderID != "" {
		builder.SetProviderID(ie.ProviderID)
	}

	if ie.LastSyncedAt != nil {
		builder.SetLastSyncedAt(*ie.LastSyncedAt)
	}

	if ie.LastErrorMsg != nil {
		builder.SetLastErrorMsg(*ie.LastErrorMsg)
	}

	if len(ie.SyncHistory) > 0 {
		builder.SetSyncHistory(domainIntegration.ToEntSyncHistory(ie.SyncHistory))
	}

	_, err := builder.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return errors.WithError(err).
				WithHint("Entity ie already exists").
				Mark(errors.ErrAlreadyExists)
		}
		return errors.WithError(err).
			WithHint("Failed to create entity ie").
			Mark(errors.ErrDatabase)
	}

	return nil
}

// Get retrieves an IntegrationEntity by ID.
func (r *integrationRepository) Get(ctx context.Context, id string) (*domainIntegration.IntegrationEntity, error) {
	querier := r.client.Querier(ctx)

	ec, err := querier.IntegrationEntity.Query().
		Where(
			integrationentity.ID(id),
			integrationentity.Status(string(types.StatusPublished)),
			integrationentity.TenantID(types.GetTenantID(ctx)),
			integrationentity.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.WithError(err).
				WithHintf("Entity ie with ID %s not found", id).
				Mark(errors.ErrNotFound)
		}
		return nil, errors.WithError(err).
			WithHint("Failed to retrieve entity ie").
			Mark(errors.ErrDatabase)
	}

	return domainIntegration.FromEnt(ec), nil
}

// GetByEntityAndProvider retrieves an IntegrationEntity by entity type, entity ID, and provider type.
func (r *integrationRepository) GetByEntityAndProvider(ctx context.Context, entityType types.EntityType, entityID string, providerType types.SecretProvider) (*domainIntegration.IntegrationEntity, error) {
	querier := r.client.Querier(ctx)

	ec, err := querier.IntegrationEntity.Query().
		Where(
			integrationentity.EntityType(entityType),
			integrationentity.EntityID(entityID),
			integrationentity.ProviderType(providerType),
			integrationentity.StatusNEQ("deleted"),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.WithError(err).
				WithHintf("Entity ie for %s %s with provider %s not found", entityType, entityID, providerType).
				Mark(errors.ErrNotFound)
		}
		return nil, errors.WithError(err).
			WithHint("Failed to retrieve entity ie").
			Mark(errors.ErrDatabase)
	}

	return domainIntegration.FromEnt(ec), nil
}

// GetByProviderID retrieves an IntegrationEntity by provider ID and provider type.
func (r *integrationRepository) GetByProviderID(ctx context.Context, entityType types.EntityType, providerID string, providerType types.SecretProvider) (*domainIntegration.IntegrationEntity, error) {
	querier := r.client.Querier(ctx)

	query := querier.IntegrationEntity.Query().
		Where(
			integrationentity.ProviderID(providerID),
			integrationentity.ProviderType(providerType),
			integrationentity.StatusNEQ("deleted"),
		)

	if entityType != "" {
		query = query.Where(integrationentity.EntityType(entityType))
	}

	ec, err := query.Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.WithError(err).
				WithHintf("Entity ie with provider ID %s and type %s not found", providerID, providerType).
				Mark(errors.ErrNotFound)
		}
		return nil, errors.WithError(err).
			WithHint("Failed to retrieve entity ie").
			Mark(errors.ErrDatabase)
	}

	return domainIntegration.FromEnt(ec), nil
}

// List retrieves a list of integrations based on filter criteria.
func (r *integrationRepository) List(ctx context.Context, filter *domainIntegration.IntegrationEntityFilter) ([]*domainIntegration.IntegrationEntity, error) {
	querier := r.client.Querier(ctx)

	query := querier.IntegrationEntity.Query()
	query = applyIntegrationEntityFilter(query, filter)

	// Apply tenant filter from context
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" {
		query = query.Where(integrationentity.TenantID(tenantID))
	}

	// Apply pagination if limit is specified
	if filter != nil && filter.GetLimit() > 0 {
		query = query.Limit(filter.GetLimit())

		if filter.GetOffset() > 0 {
			query = query.Offset(filter.GetOffset())
		}
	}

	ies, err := query.All(ctx)
	if err != nil {
		return nil, errors.WithError(err).
			WithHint("Failed to list entity integrations").
			Mark(errors.ErrDatabase)
	}

	return domainIntegration.FromEntList(ies), nil
}

// Count counts integrations based on filter criteria.
func (r *integrationRepository) Count(ctx context.Context, filter *domainIntegration.IntegrationEntityFilter) (int, error) {
	querier := r.client.Querier(ctx)

	query := querier.IntegrationEntity.Query()
	query = applyIntegrationEntityFilter(query, filter)

	// Apply tenant filter from context
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" {
		query = query.Where(integrationentity.TenantID(tenantID))
	}

	count, err := query.Count(ctx)
	if err != nil {
		return 0, errors.WithError(err).
			WithHint("Failed to count entity integrations").
			Mark(errors.ErrDatabase)
	}

	return count, nil
}

// Update updates an existing IntegrationEntity.
func (r *integrationRepository) Update(ctx context.Context, ie *domainIntegration.IntegrationEntity) error {
	querier := r.client.Querier(ctx)

	builder := querier.IntegrationEntity.UpdateOneID(ie.ID).
		SetStatus(string(ie.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetSyncStatus(ie.SyncStatus).
		SetMetadata(ie.Metadata).
		SetSyncHistory(domainIntegration.ToEntSyncHistory(ie.SyncHistory))

	if ie.UpdatedBy != "" {
		builder.SetUpdatedBy(ie.UpdatedBy)
	}

	if ie.ProviderID != "" {
		builder.SetProviderID(ie.ProviderID)
	}

	if ie.LastSyncedAt != nil {
		builder.SetLastSyncedAt(*ie.LastSyncedAt)
	}

	if ie.LastErrorMsg != nil {
		builder.SetLastErrorMsg(*ie.LastErrorMsg)
	}

	_, err := builder.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return errors.WithError(err).
				WithHintf("Entity ie with ID %s not found", ie.ID).
				Mark(errors.ErrNotFound)
		}
		return errors.WithError(err).
			WithHint("Failed to update entity ie").
			Mark(errors.ErrDatabase)
	}

	return nil
}

// Delete deletes an IntegrationEntity by ID.
func (r *integrationRepository) Delete(ctx context.Context, id string) error {
	querier := r.client.Querier(ctx)

	err := querier.IntegrationEntity.UpdateOneID(id).
		SetStatus("deleted").
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return errors.WithError(err).
				WithHintf("Entity ie with ID %s not found", id).
				Mark(errors.ErrNotFound)
		}
		return errors.WithError(err).
			WithHint("Failed to delete entity ie").
			Mark(errors.ErrDatabase)
	}

	return nil
}

// applyIntegrationEntityFilter applies filter criteria to the query.
func applyIntegrationEntityFilter(query *ent.IntegrationEntityQuery, filter *domainIntegration.IntegrationEntityFilter) *ent.IntegrationEntityQuery {
	if filter == nil {
		return query.Where(integrationentity.StatusNEQ("deleted"))
	}

	// Always exclude deleted integration entities
	query = query.Where(integrationentity.StatusNEQ("deleted"))

	// Apply status filter if specified
	if filter.GetStatus() != "" {
		query = query.Where(integrationentity.Status(filter.GetStatus()))
	}

	// Apply entity type filter if specified
	if filter.EntityType != nil {
		query = query.Where(integrationentity.EntityType(*filter.EntityType))
	}

	// Apply entity ID filter if specified
	if filter.EntityID != nil {
		query = query.Where(integrationentity.EntityID(*filter.EntityID))
	}

	// Apply provider type filter if specified
	if filter.ProviderType != nil {
		query = query.Where(integrationentity.ProviderType(*filter.ProviderType))
	}

	// Apply provider ID filter if specified
	if filter.ProviderID != nil {
		query = query.Where(integrationentity.ProviderID(*filter.ProviderID))
	}

	// Apply sync status filter if specified
	if filter.SyncStatus != nil {
		query = query.Where(integrationentity.SyncStatus(*filter.SyncStatus))
	}

	return query
}
