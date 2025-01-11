package ent

import (
	"context"
	"errors"
	"fmt"

	"github.com/flexprice/flexprice/ent/auth"
	domainAuth "github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type authRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewAuthRepository(client postgres.IClient, log *logger.Logger) *authRepository {
	return &authRepository{
		client: client,
		log:    log,
	}
}

func (r *authRepository) CreateAuth(ctx context.Context, authEntity *domainAuth.Auth) error {
	if !r.ValidateProvider(authEntity.Provider) {
		return fmt.Errorf("invalid provider")
	}

	client := r.client.Querier(ctx)
	_, err := client.Auth.Create().
		SetUserID(authEntity.UserID).
		SetProvider(string(authEntity.Provider)).
		SetToken(authEntity.Token).
		SetStatus(string(authEntity.Status)).
		SetCreatedAt(authEntity.CreatedAt).
		SetUpdatedAt(authEntity.UpdatedAt).
		Save(ctx)

	if err != nil {
		r.log.Error("failed to create auth", "error", err)
		return fmt.Errorf("creating auth: %w", err)
	}

	return nil
}

func (r *authRepository) GetAuthByUserID(ctx context.Context, userID string) (*domainAuth.Auth, error) {
	client := r.client.Querier(ctx)
	authEntity, err := client.Auth.Query().
		Where(auth.UserID(userID)).
		Only(ctx)

	if err != nil {
		r.log.Error("failed to get auth", "error", err)
		return nil, fmt.Errorf("failed to get auth: %w", err)
	}

	return domainAuth.FromEnt(authEntity), nil
}

func (r *authRepository) UpdateAuth(ctx context.Context, authEntity *domainAuth.Auth) error {
	if !r.ValidateProvider(authEntity.Provider) {
		return fmt.Errorf("invalid provider")
	}

	client := r.client.Querier(ctx)
	updated, err := client.Auth.Update().
		Where(auth.UserID(authEntity.UserID)).
		SetProvider(string(authEntity.Provider)).
		SetToken(authEntity.Token).
		SetStatus(string(authEntity.Status)).
		SetUpdatedAt(authEntity.UpdatedAt).
		Save(ctx)

	if err != nil {
		r.log.Error("failed to update auth", "error", err)
		return fmt.Errorf("updating auth: %w", err)
	}

	if updated == 0 {
		return errors.New("no matching auth found to update")
	}

	return nil
}

func (r *authRepository) DeleteAuth(ctx context.Context, userID string) error {
	client := r.client.Querier(ctx)
	_, err := client.Auth.Delete().
		Where(auth.UserID(userID)).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete auth", "error", err)
		return fmt.Errorf("deleting auth: %w", err)
	}

	return nil
}

func (r *authRepository) ValidateProvider(provider types.AuthProvider) bool {
	return provider == types.AuthProviderFlexprice
}
