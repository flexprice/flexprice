package ent

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/ent/environment"
	domainEnv "github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type environmentRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewEnvironmentRepository(client postgres.IClient, log *logger.Logger) domainEnv.Repository {
	return &environmentRepository{
		client: client,
		log:    log}
}

func (r *environmentRepository) Create(ctx context.Context, env *domainEnv.Environment) error {
	client := r.client.Querier(ctx)
	environment, err := client.Environment.Create().
		SetID(env.ID).
		SetTenantID(env.TenantID).
		SetName(env.Name).
		SetType(string(env.Type)).
		SetSlug(env.Slug).
		SetStatus(string(env.Status)).
		SetCreatedAt(env.CreatedAt).
		SetUpdatedAt(env.UpdatedAt).
		SetCreatedBy(env.CreatedBy).
		SetUpdatedBy(env.UpdatedBy).
		Save(ctx)

	if err != nil {
		r.log.Error("failed to create environment", "error", err)
		return fmt.Errorf("creating environment: %w", err)
	}

	*env = *domainEnv.FromEnt(environment)
	return nil

}

// Get retrieves an environment by ID and tenant_id
func (r *environmentRepository) Get(ctx context.Context, id string) (*domainEnv.Environment, error) {
	client := r.client.Querier(ctx)

	environment, err := client.Environment.Query().
		Where(
			environment.TenantID(types.GetTenantID(ctx)),
			environment.ID(id),
		).
		Only(ctx)

	if err != nil {
		r.log.Error("failed to get environment", "error", err)
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}

	return domainEnv.FromEnt(environment), nil
}

func (r *environmentRepository) Update(ctx context.Context, env *domainEnv.Environment) error {
	client := r.client.Querier(ctx)
	_, err := client.Environment.Update().
		Where(
			environment.ID(env.ID),
			environment.TenantID(env.TenantID),
		).
		SetName(env.Name).
		SetType(string(env.Type)).
		SetSlug(env.Slug).
		SetStatus(string(env.Status)).
		SetUpdatedAt(env.UpdatedAt).
		SetUpdatedBy(env.UpdatedBy).
		Save(ctx)

	if err != nil {
		r.log.Error("failed to update environment", "error", err)
		return fmt.Errorf("updating environment: %w", err)
	}

	return nil
}

func (r *environmentRepository) List(ctx context.Context, filter types.Filter) ([]*domainEnv.Environment, error) {
	client := r.client.Querier(ctx)
	environments, err := client.Environment.Query().
		Where(environment.TenantID(types.GetTenantID(ctx))).
		All(ctx)

	if err != nil {
		r.log.Error("failed to list environments", "error", err)
		return nil, fmt.Errorf("listing environments: %w", err)
	}

	var envs []*domainEnv.Environment
	for _, e := range environments {
		envs = append(envs, domainEnv.FromEnt(e))
	}

	return envs, nil
}
