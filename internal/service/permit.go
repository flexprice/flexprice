package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	permitConfig "github.com/permitio/permit-golang/pkg/config"
	"github.com/permitio/permit-golang/pkg/enforcement"
	"github.com/permitio/permit-golang/pkg/models"
	"github.com/permitio/permit-golang/pkg/permit"
	"go.uber.org/zap"
)

type PermitService struct {
	Client *permit.Client
	logger *logger.Logger
	config *config.PermitConfig
}

type PermitInterface interface {
	// User Management
	SyncUser(ctx context.Context, userID, email, tenantID string) error // Syncs a user to Permit.io
	AssignRole(ctx context.Context, userID, roleKey, tenantID string) error
	RemoveRole(ctx context.Context, userID, roleKey, tenantID string) error
	GetUserRoles(ctx context.Context, userID, tenantID string) ([]string, error)
	GetTenantRoles(ctx context.Context, tenantID string) ([]string, error)
	GetTenantResources(ctx context.Context, tenantID string) ([]string, error)

	// Permission Checking
	CheckPermission(ctx context.Context, userID, action, resource, tenantID string) (bool, error)
	CheckPermissionWithAttributes(ctx context.Context, userID, action, resource string, attributes map[string]interface{}) (bool, error)

	// Resource Management
	SyncResource(ctx context.Context, resourceType, resourceID, tenantID string, attributes map[string]interface{}) error
	DeleteResource(ctx context.Context, resourceType, resourceID, tenantID string) error

	// Policy Management
	CreateRole(ctx context.Context, roleKey, name, description, tenantID string, permissions []string) error
	UpdateRole(ctx context.Context, roleKey, tenantID string, permissions []string) error
	DeleteRole(ctx context.Context, roleKey, tenantID string) error

	// Environment Management
	CreateEnvironment(ctx context.Context, tenantID, envName string) error
	GetEnvironment(ctx context.Context, tenantID, envName string) (*models.EnvironmentRead, error)

	// Tenant Management
	CreateTenant(ctx context.Context, tenantID, name string) error
	GetTenant(ctx context.Context, tenantID string) (*models.TenantRead, error)
}

func NewPermitService(cfg *config.PermitConfig, logger *logger.Logger) (PermitInterface, error) {
	// Validate required configuration
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("permit API key is required")
	}
	if cfg.APIURL == "" {
		return nil, fmt.Errorf("permit API URL is required")
	}
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("permit project ID is required")
	}

	// Create a zap logger for permit.io SDK
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		return nil, fmt.Errorf("failed to create zap logger: %w", err)
	}

	permitConfig := permitConfig.NewPermitConfig(
		cfg.APIURL,
		cfg.APIKey,
		cfg.PDPURL,
		cfg.Debug,
		nil, // No context
		zapLogger,
	)

	client := permit.NewPermit(*permitConfig)

	// Set the project and environment context
	client.Api.SetContext(context.Background(), cfg.ProjectID, cfg.Environment)

	return &PermitService{
		Client: client,
		logger: logger,
		config: cfg,
	}, nil
}

// SyncUser creates a new user in Permit.io with proper tenant association
func (s *PermitService) SyncUser(ctx context.Context, userID, email, tenantID string) error {
	// Step 1: Create the user using the SyncUser method
	user := models.NewUserCreate(userID)

	// Set email if provided
	if email != "" {
		user.SetEmail(email)
	}

	// Set first name using userID
	user.SetFirstName(userID)

	// Add tenant information to user attributes
	attributes := map[string]interface{}{
		"tenant_id": tenantID,
		"email":     email,
	}
	user.SetAttributes(attributes)

	// Sync the user using the SyncUser method (recommended for initial user creation)
	_, err := s.Client.Api.Users.SyncUser(ctx, *user)
	if err != nil {
		s.logger.Errorw("failed to sync user", "error", err, "user_id", userID, "tenant_id", tenantID)
		return fmt.Errorf("failed to sync user: %w", err)
	}

	// Step 2: Assign user to tenant if tenantID is provided
	if tenantID != "" {
		// Try to assign user to tenant using the Users API
		// Note: This will fail if the role doesn't exist, but that's okay
		_, err = s.Client.Api.Users.AssignRole(ctx, userID, "test", tenantID)
		if err != nil {
			s.logger.Warnw("failed to assign member role to user", "error", err, "user_id", userID, "tenant_id", tenantID)
			s.logger.Infow("user synced but role assignment failed - user is still associated with tenant via attributes", "user_id", userID, "tenant_id", tenantID)
		} else {
			s.logger.Infow("assigned user to tenant with member role", "user_id", userID, "tenant_id", tenantID)
		}
	}

	s.logger.Infow("synced user to permit with tenant association", "user_id", userID, "tenant_id", tenantID)
	return nil
}

// AssignRole assigns a role to a user within a tenant
func (s *PermitService) AssignRole(ctx context.Context, userID, roleKey, tenantID string) error {
	// First, ensure the user exists
	err := s.SyncUser(ctx, userID, userID+"@example.com", tenantID)
	if err != nil {
		return fmt.Errorf("failed to ensure user exists: %w", err)
	}

	// Assign role using the Users API
	_, err = s.Client.Api.Users.AssignRole(ctx, userID, roleKey, tenantID)
	if err != nil {
		s.logger.Errorw("failed to assign role", "error", err, "user_id", userID, "role", roleKey, "tenant_id", tenantID)
		return fmt.Errorf("failed to assign role: %w", err)
	}

	s.logger.Infow("assigned role to user", "user_id", userID, "role", roleKey, "tenant_id", tenantID)
	return nil
}

// RemoveRole removes a role from a user within a tenant
func (s *PermitService) RemoveRole(ctx context.Context, userID, roleKey, tenantID string) error {
	// Unassign role using the Users API
	_, err := s.Client.Api.Users.UnassignRole(ctx, userID, roleKey, tenantID)
	if err != nil {
		s.logger.Errorw("failed to remove role", "error", err, "user_id", userID, "role", roleKey, "tenant_id", tenantID)
		return fmt.Errorf("failed to remove role: %w", err)
	}

	s.logger.Infow("removed role from user", "user_id", userID, "role", roleKey, "tenant_id", tenantID)
	return nil
}

// GetUserRoles retrieves all roles for a user within a tenant
func (s *PermitService) GetUserRoles(ctx context.Context, userID, tenantID string) ([]string, error) {
	assignments, err := s.Client.Api.Users.GetAssignedRoles(ctx, userID, tenantID, 1, 100)
	if err != nil {
		s.logger.Errorw("failed to get user roles", "error", err, "user_id", userID, "tenant_id", tenantID)
		return nil, fmt.Errorf("failed to get user roles: %w", err)
	}

	var roles []string
	for _, assignment := range assignments {
		roles = append(roles, assignment.Role)
	}

	s.logger.Infow("retrieved user roles", "user_id", userID, "tenant_id", tenantID, "roles", roles)
	return roles, nil
}

// GetTenantRoles retrieves all roles available in a tenant
func (s *PermitService) GetTenantRoles(ctx context.Context, tenantID string) ([]string, error) {
	roles, err := s.Client.Api.Roles.List(ctx, 1, 100)
	if err != nil {
		s.logger.Errorw("failed to get tenant roles", "error", err, "tenant_id", tenantID)
		return nil, fmt.Errorf("failed to get tenant roles: %w", err)
	}

	var roleKeys []string
	for _, role := range roles {
		roleKeys = append(roleKeys, role.Key)
	}

	s.logger.Infow("retrieved tenant roles", "tenant_id", tenantID, "roles", roleKeys)
	return roleKeys, nil
}

// GetTenantResources retrieves all resources available in a tenant
func (s *PermitService) GetTenantResources(ctx context.Context, tenantID string) ([]string, error) {
	resources, err := s.Client.Api.Resources.List(ctx, 1, 100)
	if err != nil {
		s.logger.Errorw("failed to get tenant resources", "error", err, "tenant_id", tenantID)
		return nil, fmt.Errorf("failed to get tenant resources: %w", err)
	}

	var resourceKeys []string
	for _, resource := range resources {
		resourceKeys = append(resourceKeys, resource.Key)
	}

	s.logger.Infow("retrieved tenant resources", "tenant_id", tenantID, "resources", resourceKeys)
	return resourceKeys, nil
}

// CheckPermission checks if a user has permission to perform an action on a resource
func (s *PermitService) CheckPermission(ctx context.Context, userID, action, resource, tenantID string) (bool, error) {
	// Create user for permission check with tenant context
	user := enforcement.UserBuilder(userID).Build()

	// Create action
	actionObj := enforcement.Action(action)

	// Create resource with tenant context
	resourceObj := enforcement.ResourceBuilder(resource).WithTenant(tenantID).Build()

	// Check permission
	allowed, err := s.Client.Check(user, actionObj, resourceObj)
	if err != nil {
		s.logger.Errorw("failed to check permission", "error", err, "user_id", userID, "action", action, "resource", resource, "tenant_id", tenantID)
		return false, fmt.Errorf("failed to check permission: %w", err)
	}

	s.logger.Infow("permission check result", "user_id", userID, "action", action, "resource", resource, "tenant_id", tenantID, "allowed", allowed)
	return allowed, nil
}

// CheckPermissionWithAttributes checks permission with additional attributes
func (s *PermitService) CheckPermissionWithAttributes(ctx context.Context, userID, action, resource string, attributes map[string]interface{}) (bool, error) {
	// Create user for permission check with attributes
	user := enforcement.UserBuilder(userID).WithAttributes(attributes).Build()

	// Create action
	actionObj := enforcement.Action(action)

	// Create resource
	resourceObj := enforcement.ResourceBuilder(resource).Build()

	// Check permission
	allowed, err := s.Client.Check(user, actionObj, resourceObj)
	if err != nil {
		s.logger.Errorw("failed to check permission with attributes", "error", err, "user_id", userID, "action", action, "resource", resource)
		return false, fmt.Errorf("failed to check permission with attributes: %w", err)
	}

	s.logger.Infow("permission check with attributes result", "user_id", userID, "action", action, "resource", resource, "allowed", allowed)
	return allowed, nil
}

// SyncResource creates or updates a resource in Permit.io
func (s *PermitService) SyncResource(ctx context.Context, resourceType, resourceID, tenantID string, attributes map[string]interface{}) error {
	// Create resource
	resource := models.ResourceCreate{
		Key:  resourceID,
		Name: resourceID,
		Actions: map[string]models.ActionBlockEditable{
			"read":   {},
			"create": {},
			"update": {},
			"delete": {},
		},
	}

	_, err := s.Client.Api.Resources.Create(ctx, resource)
	if err != nil {
		s.logger.Errorw("failed to sync resource", "error", err, "resource_type", resourceType, "resource_id", resourceID, "tenant_id", tenantID)
		return fmt.Errorf("failed to sync resource: %w", err)
	}

	s.logger.Infow("synced resource to permit", "resource_type", resourceType, "resource_id", resourceID, "tenant_id", tenantID)
	return nil
}

// DeleteResource removes a resource from Permit.io
func (s *PermitService) DeleteResource(ctx context.Context, resourceType, resourceID, tenantID string) error {
	err := s.Client.Api.Resources.Delete(ctx, resourceID)
	if err != nil {
		s.logger.Errorw("failed to delete resource", "error", err, "resource_type", resourceType, "resource_id", resourceID, "tenant_id", tenantID)
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	s.logger.Infow("deleted resource from permit", "resource_type", resourceType, "resource_id", resourceID, "tenant_id", tenantID)
	return nil
}

// CreateRole creates a new role in Permit.io
func (s *PermitService) CreateRole(ctx context.Context, roleKey, name, description, tenantID string, permissions []string) error {
	// First check if the role already exists
	_, err := s.Client.Api.Roles.Get(ctx, roleKey)
	if err == nil {
		// Role already exists, just log and return success
		s.logger.Infow("role already exists, skipping creation", "role_key", roleKey, "name", name, "tenant_id", tenantID)
		return nil
	}

	// Role doesn't exist, create it
	role := models.RoleCreate{
		Key:  roleKey,
		Name: name,
	}

	if description != "" {
		role.SetDescription(description)
	}

	_, err = s.Client.Api.Roles.Create(ctx, role)
	if err != nil {
		s.logger.Errorw("failed to create role", "error", err, "role_key", roleKey, "name", name, "tenant_id", tenantID)
		return fmt.Errorf("failed to create role: %w", err)
	}

	s.logger.Infow("created role in permit", "role_key", roleKey, "name", name, "tenant_id", tenantID)
	return nil
}

// UpdateRole updates an existing role in Permit.io
func (s *PermitService) UpdateRole(ctx context.Context, roleKey, tenantID string, permissions []string) error {
	role := models.RoleUpdate{
		Name: &roleKey,
	}

	_, err := s.Client.Api.Roles.Update(ctx, roleKey, role)
	if err != nil {
		s.logger.Errorw("failed to update role", "error", err, "role_key", roleKey, "tenant_id", tenantID)
		return fmt.Errorf("failed to update role: %w", err)
	}

	s.logger.Infow("updated role", "role_key", roleKey, "tenant_id", tenantID)
	return nil
}

// DeleteRole removes a role from Permit.io
func (s *PermitService) DeleteRole(ctx context.Context, roleKey, tenantID string) error {
	err := s.Client.Api.Roles.Delete(ctx, roleKey)
	if err != nil {
		s.logger.Errorw("failed to delete role", "error", err, "role_key", roleKey, "tenant_id", tenantID)
		return fmt.Errorf("failed to delete role: %w", err)
	}

	s.logger.Infow("deleted role", "role_key", roleKey, "tenant_id", tenantID)
	return nil
}

// CreateEnvironment creates a new environment in Permit.io
func (s *PermitService) CreateEnvironment(ctx context.Context, tenantID, envName string) error {
	environment := models.EnvironmentCreate{
		Key:  envName,
		Name: envName,
	}

	_, err := s.Client.Api.Environments.Create(ctx, environment)
	if err != nil {
		s.logger.Errorw("failed to create environment", "error", err, "env_name", envName, "tenant_id", tenantID)
		return fmt.Errorf("failed to create environment: %w", err)
	}

	s.logger.Infow("created environment", "env_name", envName, "tenant_id", tenantID)
	return nil
}

// GetEnvironment retrieves an environment from Permit.io
func (s *PermitService) GetEnvironment(ctx context.Context, tenantID, envName string) (*models.EnvironmentRead, error) {
	environment, err := s.Client.Api.Environments.Get(ctx, envName)
	if err != nil {
		s.logger.Errorw("failed to get environment", "error", err, "env_name", envName, "tenant_id", tenantID)
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}

	return environment, nil
}

// CreateTenant creates a new tenant in Permit.io
func (s *PermitService) CreateTenant(ctx context.Context, tenantID, name string) error {
	tenant := models.TenantCreate{
		Key:  tenantID,
		Name: name,
	}

	_, err := s.Client.Api.Tenants.Create(ctx, tenant)
	if err != nil {
		s.logger.Errorw("failed to create tenant", "error", err, "tenant_id", tenantID, "name", name)
		return fmt.Errorf("failed to create tenant: %w", err)
	}

	s.logger.Infow("created tenant in permit", "tenant_id", tenantID, "name", name)
	return nil
}

// GetTenant retrieves a tenant from Permit.io
func (s *PermitService) GetTenant(ctx context.Context, tenantID string) (*models.TenantRead, error) {
	tenant, err := s.Client.Api.Tenants.Get(ctx, fmt.Sprintf("tenant-%s", tenantID))
	if err != nil {
		s.logger.Errorw("failed to get tenant", "error", err, "tenant_id", tenantID)
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	return tenant, nil
}
