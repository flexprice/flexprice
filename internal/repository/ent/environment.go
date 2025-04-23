package ent

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/ent"
	entEnvironment "github.com/flexprice/flexprice/ent/environment"
	domainEnvironment "github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type environmentRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewEnvironmentRepository creates a new environment repository
func NewEnvironmentRepository(client postgres.IClient, logger *logger.Logger) domainEnvironment.Repository {
	return &environmentRepository{
		client: client,
		logger: logger,
	}
}

// Create creates a new environment
func (r *environmentRepository) Create(ctx context.Context, env *domainEnvironment.Environment) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "environment", "create", map[string]interface{}{
		"environment_id": env.ID,
		"tenant_id":      env.TenantID,
	})
	defer FinishSpan(span)

	r.logger.Debugw("creating environment", "environment_id", env.ID, "tenant_id", env.TenantID)

	client := r.client.Querier(ctx)
	_, err := client.Environment.
		Create().
		SetID(env.ID).
		SetTenantID(env.TenantID).
		SetName(env.Name).
		SetType(string(env.Type)).
		SetStatus(string(env.Status)).
		SetCreatedBy(env.CreatedBy).
		SetUpdatedBy(env.UpdatedBy).
		SetCreatedAt(env.CreatedAt).
		SetUpdatedAt(env.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create environment").
			WithReportableDetails(map[string]interface{}{
				"environment_id": env.ID,
				"tenant_id":      env.TenantID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// Get retrieves an environment by ID
func (r *environmentRepository) Get(ctx context.Context, id string) (*domainEnvironment.Environment, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "environment", "get", map[string]interface{}{
		"environment_id": id,
	})
	defer FinishSpan(span)

	tenantID, ok := ctx.Value(types.CtxTenantID).(string)
	if !ok {
		validationErr := fmt.Errorf("tenant ID not found in context")
		SetSpanError(span, validationErr)
		return nil, ierr.NewError("tenant ID not found in context").
			WithHint("Tenant ID is required in the context").
			Mark(ierr.ErrValidation)
	}

	client := r.client.Querier(ctx)
	e, err := client.Environment.
		Query().
		Where(
			entEnvironment.ID(id),
			entEnvironment.TenantID(tenantID),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Environment not found").
				WithReportableDetails(map[string]interface{}{
					"environment_id": id,
					"tenant_id":      tenantID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve environment").
			WithReportableDetails(map[string]interface{}{
				"environment_id": id,
				"tenant_id":      tenantID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainEnvironment.FromEnt(e), nil
}

// List retrieves environments based on filter
func (r *environmentRepository) List(ctx context.Context, filter types.Filter) ([]*domainEnvironment.Environment, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "environment", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	tenantID, ok := ctx.Value(types.CtxTenantID).(string)
	if !ok {
		validationErr := fmt.Errorf("tenant ID not found in context")
		SetSpanError(span, validationErr)
		return nil, ierr.NewError("tenant ID not found in context").
			WithHint("Tenant ID is required in the context").
			Mark(ierr.ErrValidation)
	}

	client := r.client.Querier(ctx)
	query := client.Environment.
		Query().
		Where(
			entEnvironment.TenantID(tenantID),
		).
		Order(ent.Desc(entEnvironment.FieldCreatedAt)).
		Limit(filter.Limit).
		Offset(filter.Offset)

	environments, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list environments").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": tenantID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainEnvironment.FromEntList(environments), nil
}

// Update updates an environment
func (r *environmentRepository) Update(ctx context.Context, env *domainEnvironment.Environment) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "environment", "update", map[string]interface{}{
		"environment_id": env.ID,
		"tenant_id":      env.TenantID,
	})
	defer FinishSpan(span)

	r.logger.Debugw("updating environment", "environment_id", env.ID, "tenant_id", env.TenantID)

	client := r.client.Querier(ctx)
	_, err := client.Environment.
		UpdateOneID(env.ID).
		SetName(env.Name).
		SetType(string(env.Type)).
		SetUpdatedBy(env.UpdatedBy).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Environment not found").
				WithReportableDetails(map[string]interface{}{
					"environment_id": env.ID,
					"tenant_id":      env.TenantID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update environment").
			WithReportableDetails(map[string]interface{}{
				"environment_id": env.ID,
				"tenant_id":      env.TenantID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}
