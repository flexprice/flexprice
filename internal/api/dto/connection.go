package dto

import (
	"context"
	"time"

	domainConnection "github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CreateConnectionRequest represents a request to create a new connection
type CreateConnectionRequest struct {
	Name           string                 `json:"name" validate:"required"`
	ProviderType   types.SecretProvider   `json:"provider_type" validate:"required"`
	ConnectionCode string                 `json:"connection_code" validate:"required"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	Credentials    map[string]string      `json:"credentials" validate:"required"`
}

func (r *CreateConnectionRequest) ToConnection(ctx context.Context, secretID string) *domainConnection.Connection {
	return &domainConnection.Connection{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONNECTION),
		Name:           r.Name,
		ProviderType:   r.ProviderType,
		ConnectionCode: r.ConnectionCode,
		Metadata:       r.Metadata,
		SecretID:       secretID,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
}

// Validate validates the create connection request
func (r *CreateConnectionRequest) Validate() error {
	if r.Name == "" {
		return ierr.NewError("name is required").
			WithHint("Name is required").
			Mark(ierr.ErrValidation)
	}
	if r.ProviderType == "" {
		return ierr.NewError("provider_type is required").
			WithHint("Provider type is required").
			Mark(ierr.ErrValidation)
	}
	if r.ConnectionCode == "" {
		return ierr.NewError("connection_code is required").
			WithHint("Connection code is required").
			Mark(ierr.ErrValidation)
	}
	if len(r.Credentials) == 0 {
		return ierr.NewError("credentials are required").
			WithHint("Credentials are required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// UpdateConnectionRequest represents a request to update an existing connection
type UpdateConnectionRequest struct {
	Name           string                 `json:"name,omitempty"`
	ConnectionCode string                 `json:"connection_code,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// Validate validates the update connection request
func (r *UpdateConnectionRequest) Validate() error {
	// Nothing to validate for update since all fields are optional
	return nil
}

// ConnectionResponse represents a connection response
type ConnectionResponse struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	ProviderType   types.SecretProvider   `json:"provider_type"`
	ConnectionCode string                 `json:"connection_code"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	Status         types.Status           `json:"status"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
}

// ToConnectionResponse converts a domain connection to a response
func ToConnectionResponse(c *domainConnection.Connection) *ConnectionResponse {
	if c == nil {
		return nil
	}

	return &ConnectionResponse{
		ID:             c.ID,
		Name:           c.Name,
		ProviderType:   c.ProviderType,
		ConnectionCode: c.ConnectionCode,
		Metadata:       c.Metadata,
		Status:         c.Status,
		CreatedAt:      c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      c.UpdatedAt.Format(time.RFC3339),
	}
}

// ToConnectionResponseList converts a list of domain connections to responses
func ToConnectionResponseList(connections []*domainConnection.Connection) []*ConnectionResponse {
	if connections == nil {
		return nil
	}

	result := make([]*ConnectionResponse, len(connections))
	for i, c := range connections {
		result[i] = ToConnectionResponse(c)
	}
	return result
}

// ListConnectionsResponse represents a paginated list of connections
type ListConnectionsResponse struct {
	Items      []*ConnectionResponse     `json:"items"`
	Pagination *types.PaginationResponse `json:"pagination"`
}

// TestConnectionRequest represents a request to test connection credentials
type TestConnectionRequest struct {
	ProviderType types.SecretProvider `json:"provider_type" validate:"required"`
	Credentials  map[string]string    `json:"credentials" validate:"required"`
}

// Validate validates the test connection request
func (r *TestConnectionRequest) Validate() error {
	if r.ProviderType == "" {
		return ierr.NewError("provider_type is required").
			WithHint("Provider type is required").
			Mark(ierr.ErrValidation)
	}
	if len(r.Credentials) == 0 {
		return ierr.NewError("credentials are required").
			WithHint("Credentials are required").
			Mark(ierr.ErrValidation)
	}
	return nil
}
