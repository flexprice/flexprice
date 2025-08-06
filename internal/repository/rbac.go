package repository

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/authorizationaudit"
	"github.com/flexprice/flexprice/ent/rbacpolicy"
	"github.com/flexprice/flexprice/ent/userrole"
	"github.com/flexprice/flexprice/internal/logger"
)

// RBACRepositoryInterface defines the interface for RBAC repository operations
type RBACRepositoryInterface interface {
	AssignRole(ctx context.Context, userID, role, tenantID string) error
	RemoveRole(ctx context.Context, userID, role, tenantID string) error
	GetUserRoles(ctx context.Context, userID, tenantID string) ([]string, error)
	AddPolicy(ctx context.Context, role, resource, action, tenantID string) error
	RemovePolicy(ctx context.Context, role, resource, action, tenantID string) error
	GetPolicies(ctx context.Context, tenantID string) ([][]string, error)
	LogAuthorizationAudit(ctx context.Context, userID, tenantID, resource, action string, allowed bool, reason, ipAddress, userAgent string) error
	GetAuthorizationAuditLogs(ctx context.Context, userID, tenantID string, limit, offset int) ([]*ent.AuthorizationAudit, error)
}

// RBACRepository provides database operations for RBAC
type RBACRepository struct {
	client *ent.Client
	logger *logger.Logger
}

// Ensure RBACRepository implements RBACRepositoryInterface
var _ RBACRepositoryInterface = (*RBACRepository)(nil)

// NewRBACRepository creates a new RBAC repository
func NewRBACRepository(client *ent.Client, logger *logger.Logger) *RBACRepository {
	return &RBACRepository{
		client: client,
		logger: logger,
	}
}

// AssignRole assigns a role to a user
func (r *RBACRepository) AssignRole(ctx context.Context, userID, role, tenantID string) error {
	_, err := r.client.UserRole.Create().
		SetUserID(userID).
		SetRole(role).
		SetTenantID(tenantID).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to assign role: %w", err)
	}

	r.logger.Infow("role assigned", "user_id", userID, "role", role, "tenant", tenantID)
	return nil
}

// RemoveRole removes a role from a user
func (r *RBACRepository) RemoveRole(ctx context.Context, userID, role, tenantID string) error {
	_, err := r.client.UserRole.Delete().
		Where(
			userrole.UserID(userID),
			userrole.Role(role),
			userrole.TenantID(tenantID),
		).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("failed to remove role: %w", err)
	}

	r.logger.Infow("role removed", "user_id", userID, "role", role, "tenant", tenantID)
	return nil
}

// GetUserRoles returns all roles for a user in a specific tenant
func (r *RBACRepository) GetUserRoles(ctx context.Context, userID, tenantID string) ([]string, error) {
	userRoles, err := r.client.UserRole.Query().
		Where(
			userrole.UserID(userID),
			userrole.TenantID(tenantID),
			userrole.StatusEQ("published"),
		).
		All(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to get user roles: %w", err)
	}

	roles := make([]string, len(userRoles))
	for i, ur := range userRoles {
		roles[i] = ur.Role
	}

	return roles, nil
}

// AddPolicy adds a new RBAC policy
func (r *RBACRepository) AddPolicy(ctx context.Context, role, resource, action, tenantID string) error {
	_, err := r.client.RBACPolicy.Create().
		SetRole(role).
		SetResource(resource).
		SetAction(action).
		SetTenantID(tenantID).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to add policy: %w", err)
	}

	r.logger.Infow("policy added", "role", role, "resource", resource, "action", action, "tenant", tenantID)
	return nil
}

// RemovePolicy removes an RBAC policy
func (r *RBACRepository) RemovePolicy(ctx context.Context, role, resource, action, tenantID string) error {
	_, err := r.client.RBACPolicy.Delete().
		Where(
			rbacpolicy.Role(role),
			rbacpolicy.Resource(resource),
			rbacpolicy.Action(action),
			rbacpolicy.TenantID(tenantID),
		).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("failed to remove policy: %w", err)
	}

	r.logger.Infow("policy removed", "role", role, "resource", resource, "action", action, "tenant", tenantID)
	return nil
}

// GetPolicies returns all policies for a specific tenant
func (r *RBACRepository) GetPolicies(ctx context.Context, tenantID string) ([][]string, error) {
	policies, err := r.client.RBACPolicy.Query().
		Where(
			rbacpolicy.TenantID(tenantID),
			rbacpolicy.StatusEQ("published"),
		).
		All(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to get policies: %w", err)
	}

	result := make([][]string, len(policies))
	for i, policy := range policies {
		result[i] = []string{policy.Role, policy.Resource, policy.Action, policy.TenantID, policy.Effect}
	}

	return result, nil
}

// LogAuthorizationAudit logs an authorization decision
func (r *RBACRepository) LogAuthorizationAudit(ctx context.Context, userID, tenantID, resource, action string, allowed bool, reason, ipAddress, userAgent string) error {
	_, err := r.client.AuthorizationAudit.Create().
		SetUserID(userID).
		SetTenantID(tenantID).
		SetResource(resource).
		SetAction(action).
		SetAllowed(allowed).
		SetReason(reason).
		SetIPAddress(ipAddress).
		SetUserAgent(userAgent).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to log authorization audit: %w", err)
	}

	return nil
}

// GetAuthorizationAuditLogs returns authorization audit logs
func (r *RBACRepository) GetAuthorizationAuditLogs(ctx context.Context, userID, tenantID string, limit, offset int) ([]*ent.AuthorizationAudit, error) {
	return r.client.AuthorizationAudit.Query().
		Where(
			authorizationaudit.UserID(userID),
			authorizationaudit.TenantID(tenantID),
		).
		Limit(limit).
		Offset(offset).
		Order(ent.Desc(authorizationaudit.FieldCreatedAt)).
		All(ctx)
}
