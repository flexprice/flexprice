package ent

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/ent/user"
	domainUser "github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type userRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewUserRepository(client postgres.IClient, log *logger.Logger) domainUser.Repository {
	return &userRepository{
		client: client,
		log:    log,
	}
}

func (r *userRepository) Create(ctx context.Context, userEntity *domainUser.User) error {
	client := r.client.Querier(ctx)
	_, err := client.User.Create().
		SetID(userEntity.ID).
		SetEmail(userEntity.Email).
		SetTenantID(userEntity.TenantID).
		SetCreatedAt(userEntity.CreatedAt).
		SetUpdatedAt(userEntity.UpdatedAt).
		SetCreatedBy(userEntity.CreatedBy).
		SetUpdatedBy(userEntity.UpdatedBy).
		Save(ctx)

	if err != nil {
		r.log.Error("failed to create user", "error", err)
		return fmt.Errorf("creating user: %w", err)
	}

	return nil
}

func (r *userRepository) GetByID(ctx context.Context, id string) (*domainUser.User, error) {
	client := r.client.Querier(ctx)
	tenantID := types.GetTenantID(ctx)

	userEntity, err := client.User.Query().
		Where(
			user.ID(id),
			user.TenantID(tenantID),
		).
		Only(ctx)

	if err != nil {
		r.log.Error("failed to get user by ID", "error", err)
		return nil, fmt.Errorf("getting user by ID: %w", err)
	}

	return domainUser.FromEnt(userEntity), nil
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*domainUser.User, error) {
	client := r.client.Querier(ctx)

	userEntity, err := client.User.Query().
		Where(user.Email(email)).
		Only(ctx)

	if err != nil {
		r.log.Error("failed to get user by email", "error", err)
		return nil, fmt.Errorf("getting user by email: %w", err)
	}

	return domainUser.FromEnt(userEntity), nil
}
