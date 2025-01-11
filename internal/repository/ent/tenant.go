package ent

import (
	domainTenant "github.com/flexprice/flexprice/internal/domain/tentant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type TenantRepository struct {
	client *postgres.IClient
	log    *logger.Logger
}

func NewTenantRepository(client *postgres.IClient, log *logger.Logger) domainTenant.Repository {
	return &TenantRepository{
		client: client,
		log:    log}
}
