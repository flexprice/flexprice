package models

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// APIKeyProvider provides headers for API key authentication
type APIKeyProvider struct {
	APIKey    string
	Namespace string
}

// GetHeaders implements client.HeadersProvider using existing constants
func (a *APIKeyProvider) GetHeaders(_ context.Context) (map[string]string, error) {
	return map[string]string{
		types.HeaderAuthorization: "Bearer " + a.APIKey,
		"temporal-namespace":      a.Namespace,
	}, nil
}
