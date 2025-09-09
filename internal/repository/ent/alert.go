package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	entAlert "github.com/flexprice/flexprice/ent/alert"
	domainAlert "github.com/flexprice/flexprice/internal/domain/alert"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type alertRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewAlertRepository creates a new alert repository
func NewAlertRepository(client postgres.IClient, logger *logger.Logger) domainAlert.Repository {
	return &alertRepository{
		client: client,
		logger: logger,
	}
}

// Create creates a new alert
func (r *alertRepository) Create(ctx context.Context, alert *domainAlert.Alert) error {
	r.logger.Debugw("creating alert", "entity_type", alert.EntityType, "entity_id", alert.EntityID)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alert", "create", map[string]interface{}{
		"entity_type": alert.EntityType,
		"entity_id":   alert.EntityID,
		"tenant_id":   alert.TenantID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	_, err := client.Alert.
		Create().
		SetID(alert.ID).
		SetEntityType(alert.EntityType).
		SetNillableEntityID(&alert.EntityID).
		SetAlertMetric(alert.AlertMetric).
		SetAlertState(string(alert.AlertState)).
		SetAlertInfo(alert.AlertInfo).
		SetTenantID(alert.TenantID).
		SetEnvironmentID(alert.EnvironmentID).
		SetStatus(string(alert.Status)).
		SetCreatedBy(alert.CreatedBy).
		SetUpdatedBy(alert.UpdatedBy).
		SetCreatedAt(alert.CreatedAt).
		SetUpdatedAt(alert.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("An alert with these parameters already exists").
				WithReportableDetails(map[string]interface{}{
					"entity_type": alert.EntityType,
					"entity_id":   alert.EntityID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create alert").
			WithReportableDetails(map[string]interface{}{
				"entity_type": alert.EntityType,
				"entity_id":   alert.EntityID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// GetLatestByEntity retrieves the latest alert for a given entity and metric
func (r *alertRepository) GetLatestByEntity(ctx context.Context, tenantID, environmentID, entityType, entityID, alertMetric string) (*domainAlert.Alert, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alert", "get_latest_by_entity", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
		"entity_type":    entityType,
		"entity_id":      entityID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	result, err := client.Alert.Query().
		Where(
			entAlert.TenantID(tenantID),
			entAlert.EnvironmentID(environmentID),
			entAlert.EntityType(entityType),
			entAlert.EntityID(entityID),
			entAlert.AlertMetric(types.AlertMetric(alertMetric)),
		).
		Order(ent.Desc(entAlert.FieldCreatedAt)).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, nil // No alert found is not an error in this case
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get latest alert").
			WithReportableDetails(map[string]interface{}{
				"tenant_id":      tenantID,
				"environment_id": environmentID,
				"entity_type":    entityType,
				"entity_id":      entityID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlert.FromEnt(result), nil
}
