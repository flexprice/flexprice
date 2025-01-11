package ent

import (
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type AuthRepository struct {
	client *postgres.IClient
	log    *logger.Logger
}

func NewAuthRepository(client *postgres.IClient, log *logger.Logger) *AuthRepository {
	return &AuthRepository{
		client: client,
		log:    log}
}
