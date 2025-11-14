package secret

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// Secret represents a credential in the system
type Secret struct {
	ID            string
	Name          string
	Type          types.SecretType
	Provider      types.SecretProvider
	Value         string
	DisplayID     string
	EnvironmentID string
	ExpiresAt     *time.Time
	LastUsedAt    *time.Time
	ProviderData  map[string]string
	Roles         []string // RBAC roles
	UserType      string   // "user" or "service_account"
	types.BaseModel
}

// FromEnt converts an ent.Secret to a domain Secret
func FromEnt(e *ent.Secret) *Secret {
	if e == nil {
		return nil
	}

	return &Secret{
		ID:            e.ID,
		Name:          e.Name,
		Type:          types.SecretType(e.Type),
		Provider:      types.SecretProvider(e.Provider),
		Value:         e.Value,
		DisplayID:     e.DisplayID,
		EnvironmentID: e.EnvironmentID,
		ExpiresAt:     e.ExpiresAt,
		LastUsedAt:    e.LastUsedAt,
		ProviderData:  e.ProviderData,
		Roles:         e.Roles,
		UserType:      e.UserType,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// FromEntList converts a list of ent.Secret to domain Secrets
func FromEntList(list []*ent.Secret) []*Secret {
	if list == nil {
		return nil
	}

	secrets := make([]*Secret, len(list))
	for i, e := range list {
		secrets[i] = FromEnt(e)
	}

	return secrets
}

// ToEnt is not needed as the repository will handle the conversion
// This method is removed as it's not consistent with the repository pattern used in the codebase

// IsAPIKey returns true if the secret is an API key (private or publishable)
func (s *Secret) IsAPIKey() bool {
	return s.Type == types.SecretTypePrivateKey || s.Type == types.SecretTypePublishableKey
}

// IsIntegration returns true if the secret is an integration credential
func (s *Secret) IsIntegration() bool {
	return s.Type == types.SecretTypeIntegration
}

// IsActive returns true if the secret is active (published status)
func (s *Secret) IsActive() bool {
	return s.Status == types.StatusPublished
}

// IsExpired checks if the secret has expired
func (s *Secret) IsExpired() bool {
	if s.ExpiresAt == nil {
		return false
	}
	return s.ExpiresAt.Before(time.Now())
}
