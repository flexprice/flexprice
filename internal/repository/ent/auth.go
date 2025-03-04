package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	entAuth "github.com/flexprice/flexprice/ent/auth"
	domainAuth "github.com/flexprice/flexprice/internal/domain/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type authRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewAuthRepository creates a new auth repository
func NewAuthRepository(client postgres.IClient, logger *logger.Logger) domainAuth.Repository {
	return &authRepository{
		client: client,
		logger: logger,
	}
}

// CreateAuth creates a new auth record
func (r *authRepository) CreateAuth(ctx context.Context, auth *domainAuth.Auth) error {
	if !r.ValidateProvider(auth.Provider) {
		return ierr.NewError("invalid provider").
			WithHint("Only supported authentication providers are allowed").
			WithReportableDetails(map[string]interface{}{
				"provider": string(auth.Provider),
			}).
			Mark(ierr.ErrValidation)
	}

	r.logger.Debugw("creating auth", "user_id", auth.UserID, "provider", auth.Provider)

	client := r.client.Querier(ctx)
	_, err := client.Auth.
		Create().
		SetUserID(auth.UserID).
		SetProvider(string(auth.Provider)).
		SetToken(auth.Token).
		SetStatus(string(auth.Status)).
		SetCreatedAt(auth.CreatedAt).
		SetUpdatedAt(auth.UpdatedAt).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create authentication record").
			WithReportableDetails(map[string]interface{}{
				"user_id":  auth.UserID,
				"provider": auth.Provider,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// GetAuthByUserID retrieves an auth record by user ID
func (r *authRepository) GetAuthByUserID(ctx context.Context, userID string) (*domainAuth.Auth, error) {
	client := r.client.Querier(ctx)
	auth, err := client.Auth.
		Query().
		Where(
			entAuth.UserID(userID),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Authentication record not found").
				WithReportableDetails(map[string]interface{}{
					"user_id": userID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve authentication record").
			WithReportableDetails(map[string]interface{}{
				"user_id": userID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainAuth.FromEnt(auth), nil
}

// UpdateAuth updates an auth record
func (r *authRepository) UpdateAuth(ctx context.Context, auth *domainAuth.Auth) error {
	if !r.ValidateProvider(auth.Provider) {
		return ierr.NewError("invalid provider").
			WithHint("Only supported authentication providers are allowed").
			WithReportableDetails(map[string]interface{}{
				"provider": auth.Provider,
			}).
			Mark(ierr.ErrValidation)
	}

	r.logger.Debugw("updating auth", "user_id", auth.UserID, "provider", auth.Provider)

	client := r.client.Querier(ctx)
	_, err := client.Auth.
		Update().
		Where(entAuth.UserID(auth.UserID)).
		SetProvider(string(auth.Provider)).
		SetToken(auth.Token).
		SetStatus(string(auth.Status)).
		SetUpdatedAt(auth.UpdatedAt).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Authentication record not found").
				WithReportableDetails(map[string]interface{}{
					"user_id": auth.UserID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update authentication record").
			WithReportableDetails(map[string]interface{}{
				"user_id":  auth.UserID,
				"provider": auth.Provider,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// DeleteAuth deletes an auth record
func (r *authRepository) DeleteAuth(ctx context.Context, userID string) error {
	r.logger.Debugw("deleting auth", "user_id", userID)

	client := r.client.Querier(ctx)
	_, err := client.Auth.
		Delete().
		Where(entAuth.UserID(userID)).
		Exec(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete authentication record").
			WithReportableDetails(map[string]interface{}{
				"user_id": userID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// Auth provider to be used only for Flexprice or other self implemented providers
func (r *authRepository) ValidateProvider(provider types.AuthProvider) bool {
	return provider == types.AuthProviderFlexprice
}
