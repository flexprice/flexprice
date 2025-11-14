package rbac

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/flexprice/flexprice/internal/config"
)

// Service handles permission checks with set-based lookups
type RBACService struct {
	// Fast lookup for permission checks (hot path - O(1))
	permissions map[string]map[string]map[string]bool

	// Full role definitions with metadata (for API responses)
	roles map[string]*Role
}

// Role represents a role with metadata
type Role struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Permissions map[string][]string `json:"permissions"`
}

// NewRBACService loads roles.json from config and optimizes for fast lookups
func NewRBACService(cfg *config.Configuration) (*RBACService, error) {
	// Get roles path from config or use default
	configPath := cfg.RBAC.RolesConfigPath
	if configPath == "" {
		configPath = "./config/rbac/roles.json"
	}

	// Load JSON
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Parse as: role_id -> role definition (with name, description, permissions)
	var rawConfig map[string]*Role
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Convert to optimized set-based structure for permission checks
	permissions := make(map[string]map[string]map[string]bool)

	for roleID, role := range rawConfig {
		role.ID = roleID // Set ID from map key
		permissions[roleID] = make(map[string]map[string]bool)

		for entity, actions := range role.Permissions {
			permissions[roleID][entity] = make(map[string]bool)

			// Convert array to set for O(1) lookup
			for _, action := range actions {
				permissions[roleID][entity][action] = true
			}
		}
	}

	return &RBACService{
		permissions: permissions,
		roles:       rawConfig,
	}, nil
}

// HasPermission checks if any of the user's roles grant permission
// Complexity: O(roles) with O(1) lookups = ~3 operations for typical use
// NOTE: Never touches role.Name or role.Description - zero overhead
func (s *RBACService) HasPermission(roles []string, entity string, action string) bool {
	// Empty roles = full access (backward compatibility)
	if len(roles) == 0 {
		return true
	}

	// Check each role - if ANY role grants permission, allow
	for _, role := range roles {
		if s.permissions[role] != nil &&
			s.permissions[role][entity] != nil &&
			s.permissions[role][entity][action] {
			return true
		}
	}

	return false
}

// ValidateRole checks if role exists in definitions
func (s *RBACService) ValidateRole(roleName string) bool {
	_, exists := s.permissions[roleName]
	return exists
}

// GetAllRoles returns all roles with metadata (for API endpoint)
// This is called rarely (only when fetching available roles for UI)
func (s *RBACService) ListRoles() []*Role {
	result := make([]*Role, 0, len(s.roles))
	for _, role := range s.roles {
		result = append(result, role)
	}
	return result
}

// GetRole returns a specific role with metadata
func (s *RBACService) GetRole(roleID string) (*Role, bool) {
	role, exists := s.roles[roleID]
	return role, exists
}
