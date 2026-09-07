package rbac

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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
	data, err := os.ReadFile(configPath) // #nosec G304 -- config path, not user input
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
	// Check each role - if ANY role grants permission, allow
	for _, role := range roles {
		permissions := s.permissions[role]
		if permissions == nil {
			continue
		}
		// Wildcard entity ("*") grants access to all entities
		if permissions["*"] != nil && (permissions["*"]["*"] || permissions["*"][action]) {
			return true
		}
		// Check specific entity with wildcard action or exact action
		if permissions[entity] != nil && (permissions[entity]["*"] || permissions[entity][action]) {
			return true
		}
	}

	return false
}

// ValidateRoles checks that every role exists, that it is assignable to the
// given user type, and that super_admin, which already grants everything, is
// not combined with other roles.
func (s *RBACService) ValidateRoles(userType types.UserType, roles []string) error {
	allowed := userType.AllowedRoles()

	for _, role := range roles {
		if _, exists := s.permissions[role]; !exists {
			return ierr.NewError("invalid role").
				WithHintf("Role '%s' does not exist", role).
				WithReportableDetails(map[string]interface{}{"role": role}).
				Mark(ierr.ErrValidation)
		}

		if !lo.Contains(allowed, types.Role(role)) {
			return ierr.NewError("role is not assignable to this user type").
				WithHintf("Role '%s' cannot be assigned to a '%s' account", role, userType).
				WithReportableDetails(map[string]interface{}{
					"role":          role,
					"user_type":     userType,
					"allowed_roles": lo.Map(allowed, func(r types.Role, _ int) string { return r.String() }),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	if lo.Contains(roles, types.RoleSuperAdmin.String()) && len(roles) > 1 {
		return ierr.NewError("super admin role need not be combined with other roles").
			WithHint("When super_admin is selected, no other roles need to be assigned").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// CanGrantRoles reports whether a caller holding callerRoles may hand out
// requestedRoles. A caller may only grant access it already holds, so a reader
// cannot mint a service account that writes, and only a super_admin can create
// another super_admin. Containment is checked permission by permission rather
// than by comparing role names, so a writer can still grant a narrower
// write-scoped role it fully covers.
func (s *RBACService) CanGrantRoles(callerRoles []string, requestedRoles []string) error {
	for _, role := range requestedRoles {
		permissions := s.permissions[role]
		if permissions == nil {
			// Unknown roles are rejected by ValidateRoles; nothing to compare here.
			continue
		}

		for entity, actions := range permissions {
			for action := range actions {
				if !s.HasPermission(callerRoles, entity, action) {
					return ierr.NewError("cannot grant a role that exceeds your own access").
						WithHintf("Role '%s' grants '%s' on '%s', which you do not have", role, action, entity).
						WithReportableDetails(map[string]interface{}{
							"role":         role,
							"entity":       entity,
							"action":       action,
							"caller_roles": callerRoles,
						}).
						Mark(ierr.ErrPermissionDenied)
				}
			}
		}
	}

	return nil
}

// ListRoles returns roles matching filter, for API responses (role pickers,
// permission UIs). A nil filter, or one with no fields set, returns every
// role. Add new fields to types.RoleFilter and check them here as filtering
// needs grow (e.g. by name) — this stays the one method callers use.
func (s *RBACService) ListRoles(filter *types.RoleFilter) []*Role {
	all := make([]*Role, 0, len(s.roles))
	for _, role := range s.roles {
		all = append(all, role)
	}

	if filter == nil || filter.UserType == nil {
		return all
	}

	allowed := filter.UserType.AllowedRoles()
	return lo.Filter(all, func(role *Role, _ int) bool {
		return lo.Contains(allowed, types.Role(role.ID))
	})
}

// GetRole returns a specific role with metadata
func (s *RBACService) GetRole(roleID string) (*Role, bool) {
	role, exists := s.roles[roleID]
	return role, exists
}
