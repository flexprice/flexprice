package service

import (
	"github.com/flexprice/flexprice/ent"
	stripe "github.com/stripe/stripe-go/v82" // want `CL002: gateway SDK types stay inside internal/integration`
	"internal/repository"
)

type Service struct {
	store   *repository.Store
	client  *ent.Client
	invoice *stripe.Invoice
}
