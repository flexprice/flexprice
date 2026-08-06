package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
)

// EnvAccessService handles environment access control
type EnvAccessService interface {
	// HasEnvironmentAccess checks if a user has access to a specific environment
	HasEnvironmentAccess(ctx context.Context, userID, tenantID, environmentID string) bool

	// GetAllowedEnvironments returns the list of environment IDs a user can access
	GetAllowedEnvironments(ctx context.Context, userID, tenantID string) []string
}

type envAccessService struct {
	cfg *config.Configuration
}

// NewEnvAccessService creates a new environment access service
func NewEnvAccessService(cfg *config.Configuration) EnvAccessService {
	return &envAccessService{
		cfg: cfg,
	}
}

// HasEnvironmentAccess checks if a user has access to a specific environment
func (s *envAccessService) HasEnvironmentAccess(ctx context.Context, userID, tenantID, environmentID string) bool {
	// If no environment ID provided, allow access (backward compatibility)
	if environmentID == "" {
		return true
	}

	// If no user environment mapping configured, allow all access
	if s.cfg.EnvAccess.UserEnvMapping == nil {
		return true
	}

	// Check if tenant exists in mapping
	tenantMapping, tenantExists := s.cfg.EnvAccess.UserEnvMapping[tenantID]
	if !tenantExists {
		// Tenant not in mapping = super admin access
		return true
	}

	// Check if user exists in tenant mapping
	userEnvs, userExists := tenantMapping[userID]
	if !userExists {
		// User not in mapping = super admin access
		return true
	}

	// Check if environment is in user's allowed list
	for _, allowedEnv := range userEnvs {
		if allowedEnv == environmentID {
			return true
		}
	}

	return false
}

// GetAllowedEnvironments returns the list of environment IDs a user can access
func (s *envAccessService) GetAllowedEnvironments(ctx context.Context, userID, tenantID string) []string {
	// If no user environment mapping configured, return empty (means all access)
	if s.cfg.EnvAccess.UserEnvMapping == nil {
		return []string{}
	}

	// Check if tenant exists in mapping
	tenantMapping, tenantExists := s.cfg.EnvAccess.UserEnvMapping[tenantID]
	if !tenantExists {
		return []string{} // Tenant not in mapping = super admin (empty list means all)
	}

	// Check if user exists in tenant mapping
	userEnvs, userExists := tenantMapping[userID]
	if !userExists {
		return []string{} // User not in mapping = super admin (empty list means all)
	}

	return userEnvs
}
