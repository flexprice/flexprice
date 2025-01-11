package ent

import (
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type UserRepository struct {
	client *postgres.IClient
	log    *logger.Logger
}

func NewUserRepository(client *postgres.IClient, log *logger.Logger) *UserRepository {
	return &UserRepository{
		client: client,
		log:    log}
}
