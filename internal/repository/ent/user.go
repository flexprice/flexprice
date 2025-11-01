package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	entUser "github.com/flexprice/flexprice/ent/user"
	domainUser "github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type userRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewUserRepository creates a new user repository
func NewUserRepository(client postgres.IClient, logger *logger.Logger) domainUser.Repository {
	return &userRepository{
		client: client,
		logger: logger,
	}
}

// Create creates a new user
func (r *userRepository) Create(ctx context.Context, user *domainUser.User) error {
	r.logger.Debugw("creating user", "user_id", user.ID, "email", user.Email, "tenant_id", user.TenantID)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "user", "create", map[string]interface{}{
		"user_id":   user.ID,
		"email":     user.Email,
		"tenant_id": user.TenantID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	_, err := client.User.
		Create().
		SetID(user.ID).
		SetEmail(user.Email).
		SetType(user.Type).
		SetRoles(user.Roles).
		SetTenantID(user.TenantID).
		SetStatus(string(user.Status)).
		SetCreatedBy(user.CreatedBy).
		SetUpdatedBy(user.UpdatedBy).
		SetCreatedAt(user.CreatedAt).
		SetUpdatedAt(user.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create user").
			WithReportableDetails(map[string]interface{}{
				"user_id":   user.ID,
				"email":     user.Email,
				"tenant_id": user.TenantID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// GetByID retrieves a user by ID
func (r *userRepository) GetByID(ctx context.Context, id string) (*domainUser.User, error) {
	tenantID, ok := ctx.Value(types.CtxTenantID).(string)
	if !ok {
		return nil, ierr.NewError("tenant ID not found in context").
			WithHint("Tenant ID is required in the context").
			Mark(ierr.ErrValidation)
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "user", "get_by_id", map[string]interface{}{
		"user_id":   id,
		"tenant_id": tenantID,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	user, err := client.User.
		Query().
		Where(
			entUser.ID(id),
			entUser.TenantID(tenantID),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("User not found").
				WithReportableDetails(map[string]interface{}{
					"user_id":   id,
					"tenant_id": tenantID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve user").
			WithReportableDetails(map[string]interface{}{
				"user_id":   id,
				"tenant_id": tenantID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainUser.FromEnt(user), nil
}

// GetByEmail retrieves a user by email
func (r *userRepository) GetByEmail(ctx context.Context, email string) (*domainUser.User, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "user", "get_by_email", map[string]interface{}{
		"email": email,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	// For login, we don't have tenant ID in context, so we just search by email
	query := client.User.Query().Where(
		entUser.Email(email),
		entUser.Status(string(types.StatusPublished)),
	)

	tenantID := types.GetTenantID(ctx)
	// If tenant ID is in context, add it to the query
	if tenantID != "" {
		query = query.Where(entUser.TenantID(tenantID))
	}

	user, err := query.Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("User not found with the provided email").
				WithReportableDetails(map[string]interface{}{
					"email": email,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve user by email").
			WithReportableDetails(map[string]interface{}{
				"email": email,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainUser.FromEnt(user), nil
}

// ListByType retrieves all users by type
func (r *userRepository) ListByType(ctx context.Context, tenantID, userType string) ([]*domainUser.User, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "user", "list_by_type", map[string]interface{}{
		"tenant_id": tenantID,
		"user_type": userType,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	users, err := client.User.
		Query().
		Where(
			entUser.TenantID(tenantID),
			entUser.Type(userType),
			entUser.Status(string(types.StatusPublished)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list users by type").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": tenantID,
				"user_type": userType,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	domainUsers := make([]*domainUser.User, len(users))
	for i, u := range users {
		domainUsers[i] = domainUser.FromEnt(u)
	}

	return domainUsers, nil
}
