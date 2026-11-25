package wallet

import (
	"context"

	"internal/ee/service" // want `CL002: domain packages depend on nothing above them`
)

type Wallet struct {
	ID string
}

type Repository interface {
	Get(ctx context.Context, id string) (*Wallet, error)
}

var _ *service.Service
