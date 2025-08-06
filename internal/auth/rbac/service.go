package rbac

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/repository"
)

// Service provides RBAC authorization functionality
type Service struct {
	enforcer *casbin.Enforcer
	logger   *logger.Logger
	repo     repository.RBACRepositoryInterface
}

// NewService creates a new RBAC service
func NewService(logger *logger.Logger, repo repository.RBACRepositoryInterface, client *ent.Client) (*Service, error) {
	// Try to find the model file relative to the current directory
	modelPath := "internal/auth/rbac/model.conf"
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		// If not found, try from the project root
		modelPath = "model.conf"
	}

	// Load model from file
	m, err := model.NewModelFromFile(modelPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load RBAC model: %w", err)
	}

	// Create Ent adapter for database storage
	adapter := NewEntAdapter(client, logger)

	// Create enforcer with database adapter
	enforcer, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create RBAC enforcer: %w", err)
	}

	// Enable logging
	enforcer.EnableLog(true)

	// Load policies from database
	err = enforcer.LoadPolicy()
	if err != nil {
		return nil, fmt.Errorf("failed to load policies from database: %w", err)
	}

	return &Service{
		enforcer: enforcer,
		logger:   logger,
		repo:     repo,
	}, nil
}

// CheckPermission checks if a user has permission to perform an action on a resource
func (s *Service) CheckPermission(ctx context.Context, userID, resource, action, tenantID string) (bool, error) {
	// Get user roles for the tenant
	roles, err := s.GetUserRoles(ctx, userID, tenantID)
	if err != nil {
		return false, fmt.Errorf("failed to get user roles: %w", err)
	}

	// Check each role for permission
	for _, role := range roles {
		// For superadmin, use wildcard tenant
		checkTenant := tenantID
		if role == "superadmin" {
			checkTenant = "*"
		}

		// Try with actual tenant ID first
		allowed, err := s.enforcer.Enforce(role, resource, action, checkTenant)
		if err != nil {
			s.logger.Errorw("failed to enforce policy", "error", err, "role", role, "resource", resource, "action", action, "tenant", checkTenant)
			continue
		}

		if allowed {
			s.logger.Debugw("permission granted", "user_id", userID, "role", role, "resource", resource, "action", action, "tenant", tenantID)
			// Log audit entry
			s.repo.LogAuthorizationAudit(ctx, userID, tenantID, resource, action, true, "Permission granted", "", "")
			return true, nil
		}

		// If not allowed with actual tenant, try with "tenant" placeholder for regular roles
		if role != "superadmin" {
			allowed, err = s.enforcer.Enforce(role, resource, action, "tenant")
			if err != nil {
				s.logger.Errorw("failed to enforce policy with placeholder", "error", err, "role", role, "resource", resource, "action", action)
				continue
			}

			if allowed {
				s.logger.Debugw("permission granted with placeholder", "user_id", userID, "role", role, "resource", resource, "action", action, "tenant", tenantID)
				// Log audit entry
				s.repo.LogAuthorizationAudit(ctx, userID, tenantID, resource, action, true, "Permission granted with placeholder", "", "")
				return true, nil
			}
		}
	}

	s.logger.Debugw("permission denied", "user_id", userID, "resource", resource, "action", action, "tenant", tenantID)
	// Log audit entry
	s.repo.LogAuthorizationAudit(ctx, userID, tenantID, resource, action, false, "Permission denied", "", "")
	return false, nil
}

// GetUserRoles returns all roles for a user in a specific tenant
func (s *Service) GetUserRoles(ctx context.Context, userID, tenantID string) ([]string, error) {
	// Get roles from database
	roles, err := s.repo.GetUserRoles(ctx, userID, tenantID)
	if err != nil {
		s.logger.Errorw("failed to get user roles from database", "error", err, "user_id", userID, "tenant", tenantID)
		// Fallback to default role assignment logic
		return s.getDefaultRoles(userID), nil
	}

	if len(roles) > 0 {
		return roles, nil
	}

	// Default role assignment logic for new users
	return s.getDefaultRoles(userID), nil
}

// getDefaultRoles returns default roles based on user ID
func (s *Service) getDefaultRoles(userID string) []string {
	if strings.Contains(userID, "admin") || strings.Contains(userID, "super") {
		return []string{"superadmin"}
	}

	if strings.Contains(userID, "manager") {
		return []string{"manager"}
	}

	// Default to user role
	return []string{"user"}
}

// AssignRole assigns a role to a user in a specific tenant
func (s *Service) AssignRole(ctx context.Context, userID, role, tenantID string) error {
	// Add role assignment to the enforcer
	_, err := s.enforcer.AddGroupingPolicy(userID, role, tenantID)
	if err != nil {
		return fmt.Errorf("failed to assign role to enforcer: %w", err)
	}

	// Store role assignment in database
	err = s.repo.AssignRole(ctx, userID, role, tenantID)
	if err != nil {
		return fmt.Errorf("failed to assign role to database: %w", err)
	}

	s.logger.Infow("role assigned", "user_id", userID, "role", role, "tenant", tenantID)
	return nil
}

// RemoveRole removes a role from a user in a specific tenant
func (s *Service) RemoveRole(ctx context.Context, userID, role, tenantID string) error {
	// Remove role assignment from the enforcer
	_, err := s.enforcer.RemoveGroupingPolicy(userID, role, tenantID)
	if err != nil {
		return fmt.Errorf("failed to remove role from enforcer: %w", err)
	}

	// Remove role assignment from database
	err = s.repo.RemoveRole(ctx, userID, role, tenantID)
	if err != nil {
		return fmt.Errorf("failed to remove role from database: %w", err)
	}

	s.logger.Infow("role removed", "user_id", userID, "role", role, "tenant", tenantID)
	return nil
}

// AddPolicy adds a new policy to the RBAC system
func (s *Service) AddPolicy(ctx context.Context, role, resource, action, tenantID string) error {
	_, err := s.enforcer.AddPolicy(role, resource, action, tenantID, "allow")
	if err != nil {
		return fmt.Errorf("failed to add policy: %w", err)
	}

	s.logger.Infow("policy added", "role", role, "resource", resource, "action", action, "tenant", tenantID)
	return nil
}

// RemovePolicy removes a policy from the RBAC system
func (s *Service) RemovePolicy(ctx context.Context, role, resource, action, tenantID string) error {
	_, err := s.enforcer.RemovePolicy(role, resource, action, tenantID, "allow")
	if err != nil {
		return fmt.Errorf("failed to remove policy: %w", err)
	}

	s.logger.Infow("policy removed", "role", role, "resource", resource, "action", action, "tenant", tenantID)
	return nil
}

// GetPolicies returns all policies for a specific tenant
func (s *Service) GetPolicies(ctx context.Context, tenantID string) ([][]string, error) {
	// Get policies from database
	policies, err := s.repo.GetPolicies(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get policies from database: %w", err)
	}

	// Also get policies from enforcer for backward compatibility
	enforcerPolicies, err := s.enforcer.GetFilteredPolicy(3, tenantID)
	if err != nil {
		s.logger.Errorw("failed to get policies from enforcer", "error", err, "tenant", tenantID)
	}

	// Combine policies from both sources
	allPolicies := make([][]string, 0, len(policies)+len(enforcerPolicies))
	allPolicies = append(allPolicies, policies...)
	allPolicies = append(allPolicies, enforcerPolicies...)

	return allPolicies, nil
}

// SavePolicies saves the current policies to the storage
func (s *Service) SavePolicies(ctx context.Context) error {
	return s.enforcer.SavePolicy()
}

// ReloadPolicies reloads policies from storage
func (s *Service) ReloadPolicies(ctx context.Context) error {
	return s.enforcer.LoadPolicy()
}

// CheckTenantAccess checks if a user has access to a specific tenant
func (s *Service) CheckTenantAccess(ctx context.Context, userID, targetTenantID string) (bool, error) {
	// Superadmin can access any tenant
	roles, err := s.GetUserRoles(ctx, userID, targetTenantID)
	if err != nil {
		return false, err
	}

	for _, role := range roles {
		if role == "superadmin" {
			return true, nil
		}
	}

	// For other roles, check if they have any policies for the target tenant
	// Since we're using placeholder "tenant" in policies, any user with a role has access
	// In a real implementation, you'd check actual tenant-specific policies
	return len(roles) > 0, nil
}

// ValidateResourceOwnership checks if a user owns a specific resource
func (s *Service) ValidateResourceOwnership(ctx context.Context, userID, resourceID, resourceType, tenantID string) (bool, error) {
	// This is a simplified implementation
	// In a real system, you would check the resource ownership in the database

	// For now, we'll assume users can only access their own resources
	// unless they have admin or manager roles
	roles, err := s.GetUserRoles(ctx, userID, tenantID)
	if err != nil {
		return false, err
	}

	for _, role := range roles {
		if role == "admin" || role == "manager" || role == "superadmin" {
			return true, nil
		}
	}

	// For regular users, check if they own the resource
	// This would typically involve a database lookup
	// For now, we'll use a simple heuristic
	if strings.Contains(resourceID, userID) {
		return true, nil
	}

	return false, nil
}

// CreateRole creates a new role with the specified permissions
func (s *Service) CreateRole(ctx context.Context, name, description string, permissions []string, tenantID string) error {
	// Create policies for the new role
	for _, permission := range permissions {
		parts := strings.Split(permission, ":")
		if len(parts) != 2 {
			s.logger.Warnw("invalid permission format", "permission", permission)
			continue
		}

		resource := parts[0]
		action := parts[1]

		// Add policy to enforcer
		_, err := s.enforcer.AddPolicy(name, resource, action, tenantID, "allow")
		if err != nil {
			s.logger.Errorw("failed to add policy to enforcer", "error", err, "role", name, "resource", resource, "action", action)
			continue
		}

		// Add policy to database
		err = s.repo.AddPolicy(ctx, name, resource, action, tenantID)
		if err != nil {
			s.logger.Errorw("failed to add policy to database", "error", err, "role", name, "resource", resource, "action", action)
		}
	}

	s.logger.Infow("role created", "role", name, "tenant", tenantID)
	return nil
}

// UpdateRole updates an existing role with new permissions
func (s *Service) UpdateRole(ctx context.Context, roleID, name, description string, permissions []string, tenantID string) error {
	// Remove existing policies for this role
	existingPolicies, err := s.enforcer.GetFilteredPolicy(0, roleID)
	if err != nil {
		s.logger.Errorw("failed to get existing policies", "error", err, "role_id", roleID)
	}

	for _, policy := range existingPolicies {
		if len(policy) >= 4 {
			_, err := s.enforcer.RemovePolicy(policy[0], policy[1], policy[2], policy[3], policy[4])
			if err != nil {
				s.logger.Errorw("failed to remove existing policy", "error", err, "policy", policy)
			}
		}
	}

	// Add new policies
	for _, permission := range permissions {
		parts := strings.Split(permission, ":")
		if len(parts) != 2 {
			s.logger.Warnw("invalid permission format", "permission", permission)
			continue
		}

		resource := parts[0]
		action := parts[1]

		// Add policy to enforcer
		_, err := s.enforcer.AddPolicy(roleID, resource, action, tenantID, "allow")
		if err != nil {
			s.logger.Errorw("failed to add policy to enforcer", "error", err, "role", roleID, "resource", resource, "action", action)
			continue
		}

		// Add policy to database
		err = s.repo.AddPolicy(ctx, roleID, resource, action, tenantID)
		if err != nil {
			s.logger.Errorw("failed to add policy to database", "error", err, "role", roleID, "resource", resource, "action", action)
		}
	}

	s.logger.Infow("role updated", "role_id", roleID, "tenant", tenantID)
	return nil
}

// DeleteRole deletes a role and all its policies
func (s *Service) DeleteRole(ctx context.Context, roleID, tenantID string) error {
	// Remove all policies for this role
	existingPolicies, err := s.enforcer.GetFilteredPolicy(0, roleID)
	if err != nil {
		s.logger.Errorw("failed to get existing policies", "error", err, "role_id", roleID)
	}

	for _, policy := range existingPolicies {
		if len(policy) >= 4 {
			_, err := s.enforcer.RemovePolicy(policy[0], policy[1], policy[2], policy[3], policy[4])
			if err != nil {
				s.logger.Errorw("failed to remove policy", "error", err, "policy", policy)
			}
		}
	}

	// Remove role assignments
	_, err = s.enforcer.RemoveFilteredGroupingPolicy(1, roleID)
	if err != nil {
		s.logger.Errorw("failed to remove role assignments", "error", err, "role_id", roleID)
	}

	s.logger.Infow("role deleted", "role_id", roleID, "tenant", tenantID)
	return nil
}

// ListRoles returns all roles for a tenant with pagination
func (s *Service) ListRoles(ctx context.Context, tenantID string, limit, offset int) ([]dto.RoleResponse, int, error) {
	// Get all policies for the tenant
	policies, err := s.repo.GetPolicies(ctx, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get policies: %w", err)
	}

	// Group policies by role
	roleMap := make(map[string][]string)
	for _, policy := range policies {
		if len(policy) >= 3 {
			role := policy[0]
			resource := policy[1]
			action := policy[2]
			permission := resource + ":" + action

			if _, exists := roleMap[role]; !exists {
				roleMap[role] = []string{}
			}
			roleMap[role] = append(roleMap[role], permission)
		}
	}

	// Convert to response format
	roles := make([]dto.RoleResponse, 0, len(roleMap))
	for roleName, permissions := range roleMap {
		role := dto.RoleResponse{
			ID:          roleName,
			Name:        roleName,
			Description: "Role: " + roleName,
			Permissions: permissions,
			TenantID:    tenantID,
		}
		roles = append(roles, role)
	}

	total := len(roles)

	// Apply pagination
	if offset >= total {
		return []dto.RoleResponse{}, total, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return roles[offset:end], total, nil
}
