package events

import (
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
)

// IntegrationEventService is the facade used by main.go to register the consumer.
type IntegrationEventService struct {
	handler Handler
}

// NewIntegrationEventService constructs the service. Called by the FX module.
func NewIntegrationEventService(handler Handler) *IntegrationEventService {
	return &IntegrationEventService{handler: handler}
}

// RegisterHandler wires the integration events consumer into the Watermill router.
func (s *IntegrationEventService) RegisterHandler(router *pubsubRouter.Router) {
	s.handler.RegisterHandler(router)
}
