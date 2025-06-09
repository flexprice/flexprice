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
		// currently structure of storing key in credentials is:
		// {
		// 	"api_key": "sk_test_1234567890"
		// }
		apiKey := ""
		if value, exists := credentials["api_key"]; exists && value != "" {
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
