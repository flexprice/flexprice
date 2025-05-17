package stripe

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integrations"
	"github.com/flexprice/flexprice/internal/logger"
)

// NewStripeGatewayFactory creates a factory function for Stripe gateways
func NewStripeGatewayFactory() integrations.GatewayFactory {
	return func(credentials map[string]string, logger *logger.Logger) (integrations.IntegrationGateway, error) {
		// Extract API key from credentials
		apiKey := ""

		// the key is the default name for the API key when installing the Stripe integration
		if value, exists := credentials["key"]; exists && value != "" {
			apiKey = value
		}

		if apiKey == "" {
			return nil, ierr.NewError("Stripe API key is required").
				WithHint("Please provide a valid Stripe API key").
				Mark(ierr.ErrValidation)
		}

		return NewStripeGateway(apiKey, logger)
	}
}
