# RBAC & ABAC Implementation with Casbin - Product Requirements Document

## Table of Contents

1. [Overview](#overview)
2. [Architecture Design](#architecture-design)
3. [Two-Level Implementation Strategy](#two-level-implementation-strategy)
4. [Configuration](#configuration)
5. [Implementation Steps](#implementation-steps)
6. [Code Examples](#code-examples)
7. [Migration Strategy](#migration-strategy)
8. [Testing Strategy](#testing-strategy)

## Overview

This document outlines the implementation of Role-Based Access Control (RBAC) and Attribute-Based Access Control (ABAC) using Casbin in their Go backend application at both handler and service levels.

### Goals

- Implement fine-grained access control at two levels:
  - **Handler Level**: Coarse-grained API endpoint protection
  - **Service Level**: Fine-grained business logic protection
- Maintain existing clean architecture
- Support multi-tenancy
- Enable gradual migration without breaking changes

### Key Features

- **RBAC**: Role-based permissions (admin, manager, user)
- **ABAC**: Attribute-based permissions (resource ownership, department, status)
- **Multi-tenant**: Tenant-isolated access control
- **Hierarchical**: Nested roles and permissions
- **Dynamic**: Runtime policy updates

## Architecture Design

```
┌─────────────────────────────────────────────────────────────────┐
│                        Gin Router                                │
├─────────────────────────────────────────────────────────────────┤
│  Handler Level Authorization (Coarse-grained)                   │
│  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐   │
│  │   Auth Middleware│ │  RBAC Middleware│ │ Route Handlers   │   │
│  └─────────────────┘ └─────────────────┘ └─────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│                      Service Layer                              │
│  ├─────────────────────────────────────────────────────────────┤
│  │  Service Level Authorization (Fine-grained)                 │
│  │  ┌─────────────────┐ ┌─────────────────┐ ┌──────────────┐  │
│  │  │  Casbin Service │ │   ABAC Policies │ │ Business Logic│  │
│  │  └─────────────────┘ └─────────────────┘ └──────────────┘  │
├─────────────────────────────────────────────────────────────────┤
│                    Repository Layer                             │
│  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐   │
│  │   Ent Adapter   │ │   Policy Store  │ │   Data Access   │   │
│  └─────────────────┘ └─────────────────┘ └─────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Two-Level Implementation Strategy

### Level 1: Handler Level (Coarse-grained)

- **Purpose**: Protect API endpoints based on roles
- **Scope**: HTTP routes (`/users`, `/invoices`, etc.)
- **Performance**: Fast evaluation using simple RBAC
- **Example**: Only `admin` can access `POST /users`

### Level 2: Service Level (Fine-grained)

- **Purpose**: Protect business operations based on attributes
- **Scope**: Service methods and resource ownership
- **Performance**: More complex evaluation with ABAC
- **Example**: User can only edit their own profile or invoices they created

## Configuration

### Type Safety Enums

```go
// internal/authz/types.go
package authz

// Role represents user roles in the system
type Role string

const (
    RoleUser       Role = "user"
    RoleManager    Role = "manager"
    RoleAdmin      Role = "admin"
    RoleSuperAdmin Role = "superadmin"
)

// IsValid checks if the role is valid
func (r Role) IsValid() bool {
    switch r {
    case RoleUser, RoleManager, RoleAdmin, RoleSuperAdmin:
        return true
    default:
        return false
    }
}

// String returns the string representation
func (r Role) String() string {
    return string(r)
}

// GetHierarchyLevel returns the hierarchy level (higher = more permissions)
func (r Role) GetHierarchyLevel() int {
    switch r {
    case RoleUser:       return 1
    case RoleManager:    return 2
    case RoleAdmin:      return 3
    case RoleSuperAdmin: return 4
    default:            return 0
    }
}

// HasPermissionLevel checks if role has at least the given permission level
func (r Role) HasPermissionLevel(requiredRole Role) bool {
    return r.GetHierarchyLevel() >= requiredRole.GetHierarchyLevel()
}

// Action represents actions that can be performed
type Action string

const (
    ActionCreate Action = "create"
    ActionRead   Action = "read"
    ActionUpdate Action = "update"
    ActionDelete Action = "delete"
    ActionList   Action = "list"
    ActionLookup Action = "lookup"
    ActionExecute Action = "execute"
    ActionGenerate Action = "generate"
)

// IsValid checks if the action is valid
func (a Action) IsValid() bool {
    switch a {
    case ActionCreate, ActionRead, ActionUpdate, ActionDelete, ActionList, ActionLookup, ActionExecute, ActionGenerate:
        return true
    default:
        return false
    }
}

func (a Action) String() string {
    return string(a)
}

// Resource represents resources in the system
type Resource string

const (
    ResourceUser     Resource = "user"
    ResourceInvoice  Resource = "invoice"
    ResourceCustomer Resource = "customer"
    ResourcePlan     Resource = "plan"
    ResourcePrice    Resource = "price"
    ResourceWallet   Resource = "wallet"
    ResourcePayment  Resource = "payment"
    ResourceReport   Resource = "report"
    ResourceEvent    Resource = "event"
    ResourceFeature  Resource = "feature"
    ResourceMeter    Resource = "meter"
    ResourceTask     Resource = "task"
    ResourceTenant   Resource = "tenant"
    ResourceSecret   Resource = "secret"
    ResourceConnection Resource = "connection"
    ResourceSubscription Resource = "subscription"
    ResourceEntitlement Resource = "entitlement"
    ResourceEnvironment Resource = "environment"
)

func (r Resource) IsValid() bool {
    switch r {
    case ResourceUser, ResourceInvoice, ResourceCustomer, ResourcePlan, ResourcePrice,
         ResourceWallet, ResourcePayment, ResourceReport, ResourceEvent, ResourceFeature,
         ResourceMeter, ResourceTask, ResourceTenant, ResourceSecret, ResourceConnection,
         ResourceSubscription, ResourceEntitlement, ResourceEnvironment:
        return true
    default:
        return false
    }
}

func (r Resource) String() string {
    return string(r)
}

// HTTPMethod represents HTTP methods
type HTTPMethod string

const (
    MethodGET    HTTPMethod = "GET"
    MethodPOST   HTTPMethod = "POST"
    MethodPUT    HTTPMethod = "PUT"
    MethodPATCH  HTTPMethod = "PATCH"
    MethodDELETE HTTPMethod = "DELETE"
)

func (m HTTPMethod) String() string {
    return string(m)
}

// Endpoint represents API endpoints
type Endpoint string

const (
    // User endpoints
    EndpointUsers          Endpoint = "/users"
    EndpointUsersID        Endpoint = "/users/:id"
    EndpointProfile        Endpoint = "/profile"

    // Invoice endpoints
    EndpointInvoices       Endpoint = "/invoices"
    EndpointInvoicesID     Endpoint = "/invoices/:id"

    // Customer endpoints
    EndpointCustomers      Endpoint = "/customers"
    EndpointCustomersID    Endpoint = "/customers/:id"

    // Plan endpoints
    EndpointPlans          Endpoint = "/plans"
    EndpointPlansID        Endpoint = "/plans/:id"

    // Price endpoints
    EndpointPrices         Endpoint = "/prices"
    EndpointPricesID       Endpoint = "/prices/:id"

    // Wallet endpoints
    EndpointWallets        Endpoint = "/wallets"
    EndpointWalletsID      Endpoint = "/wallets/:id"

    // Payment endpoints
    EndpointPayments       Endpoint = "/payments"
    EndpointPaymentsID     Endpoint = "/payments/:id"

    // Report endpoints
    EndpointReports        Endpoint = "/reports"
    EndpointReportsGenerate Endpoint = "/reports/generate"

    // Admin endpoints
    EndpointAdminUsers     Endpoint = "/admin/users"
    EndpointAdminTenants   Endpoint = "/admin/tenants"
)

func (e Endpoint) String() string {
    return string(e)
}

// Permission represents a combination of role, resource, and action
type Permission struct {
    Role     Role
    Resource Resource
    Action   Action
}

// String returns a string representation of the permission
func (p Permission) String() string {
    return fmt.Sprintf("%s:%s:%s", p.Role, p.Resource, p.Action)
}

// PolicyRule represents a policy rule with optional conditions
type PolicyRule struct {
    Role      Role
    Resource  Resource
    Action    Action
    Condition string // Optional condition for ABAC
}

// EndpointPolicy represents a handler-level policy rule
type EndpointPolicy struct {
    Role     Role
    Endpoint Endpoint
    Method   HTTPMethod
}

// PolicySet represents a collection of policies
type PolicySet struct {
    HandlerPolicies []EndpointPolicy
    ServicePolicies []PolicyRule
}
```

### Casbin Model Configuration

#### Handler Level Model (`rbac_handler.conf`)

```ini
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _  # user, role

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && r.obj == p.obj && r.act == p.act
```

#### Service Level Model (`abac_service.conf`)

```ini
[request_definition]
r = sub, obj, act, attrs

[policy_definition]
p = sub, obj, act, condition

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && r.obj == p.obj && r.act == p.act && eval(p.condition)
```

### Environment Configuration

```go
// config/authz.go
type AuthzConfig struct {
    HandlerModelPath string `env:"AUTHZ_HANDLER_MODEL_PATH" default:"./config/rbac_handler.conf"`
    ServiceModelPath string `env:"AUTHZ_SERVICE_MODEL_PATH" default:"./config/abac_service.conf"`
    DatabaseURL      string `env:"DATABASE_URL"`
    EnableDebug      bool   `env:"AUTHZ_DEBUG" default:"false"`
    CacheEnabled     bool   `env:"AUTHZ_CACHE_ENABLED" default:"true"`
}
```

## Implementation Steps

### Step 1: Install Dependencies

```bash
go get github.com/casbin/casbin/v2
go get github.com/casbin/ent-adapter
```

### Step 2: Create Authorization Service

```go
// internal/ee/service/authz.go
package service

import (
    "context"
    "fmt"
    "github.com/casbin/casbin/v2"
    entadapter "github.com/casbin/ent-adapter"
)

type AuthzService struct {
    handlerEnforcer *casbin.Enforcer
    serviceEnforcer *casbin.Enforcer
    logger          *logger.Logger
}

type AuthzInterface interface {
    // Handler Level Authorization (Type-safe endpoint checking)
    CanAccessEndpoint(ctx context.Context, userID string, endpoint Endpoint, method HTTPMethod) (bool, error)

    // Service Level Authorization (Type-safe resource checking with dynamic tenant validation)
    CanAccessResource(ctx context.Context, userID string, resource Resource, action Action, attrs map[string]interface{}) (bool, error)

    // Direct Role Checking (Type-safe role validation)
    HasRole(userID string, role Role) (bool, error)
    HasAnyRole(userID string, roles []Role) (bool, error)
    HasMinimumRole(userID string, minimumRole Role) (bool, error)
    GetUserRoles(userID string) ([]Role, error)

    // Permission Checking
    HasPermission(userID string, permission Permission) (bool, error)
    CanPerform(userID string, resource Resource, action Action) (bool, error)

    // Policy Management (Type-safe policy operations)
    AddRole(userID string, role Role) error
    RemoveRole(userID string, role Role) error
    AddEndpointPolicy(policy EndpointPolicy) error
    AddResourcePolicy(policy PolicyRule) error
    SeedPolicies(policySet PolicySet) error

    // Validation
    ValidateRoleHierarchy(userRole Role, requiredRole Role) bool
}

func NewAuthzService(config *AuthzConfig, logger *logger.Logger) (AuthzInterface, error) {
    // Handler Level Enforcer (RBAC)
    handlerAdapter, err := entadapter.NewAdapter("postgres", config.DatabaseURL)
    if err != nil {
        return nil, fmt.Errorf("failed to create handler adapter: %w", err)
    }

    handlerEnforcer, err := casbin.NewEnforcer(config.HandlerModelPath, handlerAdapter)
    if err != nil {
        return nil, fmt.Errorf("failed to create handler enforcer: %w", err)
    }

    // Service Level Enforcer (ABAC)
    serviceAdapter, err := entadapter.NewAdapter("postgres", config.DatabaseURL+"_service")
    if err != nil {
        return nil, fmt.Errorf("failed to create service adapter: %w", err)
    }

    serviceEnforcer, err := casbin.NewEnforcer(config.ServiceModelPath, serviceAdapter)
    if err != nil {
        return nil, fmt.Errorf("failed to create service enforcer: %w", err)
    }

    // Enable auto-save for real-time policy updates
    handlerEnforcer.EnableAutoSave(true)
    serviceEnforcer.EnableAutoSave(true)

    return &AuthzService{
        handlerEnforcer: handlerEnforcer,
        serviceEnforcer: serviceEnforcer,
        logger:          logger,
    }, nil
}

func (s *AuthzService) CanAccessEndpoint(ctx context.Context, userID string, endpoint Endpoint, method HTTPMethod) (bool, error) {
    // Validate input types
    if endpoint == "" || method == "" {
        return false, fmt.Errorf("endpoint and method cannot be empty")
    }

    ok, err := s.handlerEnforcer.Enforce(userID, endpoint.String(), method.String())
    if err != nil {
        s.logger.Errorw("handler authorization failed", "error", err, "user", userID, "endpoint", endpoint, "method", method)
        return false, err
    }

    s.logger.Debugw("handler authorization result", "user", userID, "endpoint", endpoint, "method", method, "allowed", ok)
    return ok, nil
}

func (s *AuthzService) CanAccessResource(ctx context.Context, userID string, resource Resource, action Action, attrs map[string]interface{}) (bool, error) {
    // Validate input types
    if !resource.IsValid() {
        return false, fmt.Errorf("invalid resource: %s", resource)
    }
    if !action.IsValid() {
        return false, fmt.Errorf("invalid action: %s", action)
    }

    // Convert attrs to string for Casbin
    attrsJSON, _ := json.Marshal(attrs)

    ok, err := s.serviceEnforcer.Enforce(userID, resource.String(), action.String(), string(attrsJSON))
    if err != nil {
        s.logger.Errorw("service authorization failed", "error", err, "user", userID, "resource", resource, "action", action)
        return false, err
    }

    s.logger.Debugw("service authorization result", "user", userID, "resource", resource, "action", action, "allowed", ok)
    return ok, nil
}

// Direct Role Checking Methods
func (s *AuthzService) HasRole(userID string, role Role) (bool, error) {
    if !role.IsValid() {
        return false, fmt.Errorf("invalid role: %s", role)
    }
    return s.handlerEnforcer.HasGroupingPolicy(userID, role.String())
}

func (s *AuthzService) HasAnyRole(userID string, roles []Role) (bool, error) {
    for _, role := range roles {
        if has, _ := s.HasRole(userID, role); has {
            return true, nil
        }
    }
    return false, nil
}

func (s *AuthzService) HasMinimumRole(userID string, minimumRole Role) (bool, error) {
    userRoles, err := s.GetUserRoles(userID)
    if err != nil {
        return false, err
    }

    minimumLevel := minimumRole.GetHierarchyLevel()
    for _, userRole := range userRoles {
        if userRole.GetHierarchyLevel() >= minimumLevel {
            return true, nil
        }
    }

    return false, nil
}

func (s *AuthzService) GetUserRoles(userID string) ([]Role, error) {
    roleStrings, err := s.handlerEnforcer.GetRolesForUser(userID)
    if err != nil {
        return nil, err
    }

    roles := make([]Role, 0, len(roleStrings))
    for _, roleStr := range roleStrings {
        role := Role(roleStr)
        if role.IsValid() {
            roles = append(roles, role)
        }
    }

    return roles, nil
}

// Permission Checking Methods
func (s *AuthzService) HasPermission(userID string, permission Permission) (bool, error) {
    attrs := map[string]interface{}{
        "permission_check": true,
    }
    return s.CanAccessResource(context.Background(), userID, permission.Resource, permission.Action, attrs)
}

func (s *AuthzService) CanPerform(userID string, resource Resource, action Action) (bool, error) {
    attrs := map[string]interface{}{
        "operation_check": true,
    }
    return s.CanAccessResource(context.Background(), userID, resource, action, attrs)
}

// Policy Management Methods
func (s *AuthzService) AddRole(userID string, role Role) error {
    if !role.IsValid() {
        return fmt.Errorf("invalid role: %s", role)
    }
    _, err := s.handlerEnforcer.AddGroupingPolicy(userID, role.String())
    return err
}

func (s *AuthzService) RemoveRole(userID string, role Role) error {
    if !role.IsValid() {
        return fmt.Errorf("invalid role: %s", role)
    }
    _, err := s.handlerEnforcer.RemoveGroupingPolicy(userID, role.String())
    return err
}

func (s *AuthzService) AddEndpointPolicy(policy EndpointPolicy) error {
    if !policy.Role.IsValid() {
        return fmt.Errorf("invalid role in policy: %s", policy.Role)
    }
    _, err := s.handlerEnforcer.AddPolicy(policy.Role.String(), policy.Endpoint.String(), policy.Method.String())
    return err
}

func (s *AuthzService) AddResourcePolicy(policy PolicyRule) error {
    if !policy.Role.IsValid() {
        return fmt.Errorf("invalid role in policy: %s", policy.Role)
    }
    if !policy.Resource.IsValid() {
        return fmt.Errorf("invalid resource in policy: %s", policy.Resource)
    }
    if !policy.Action.IsValid() {
        return fmt.Errorf("invalid action in policy: %s", policy.Action)
    }

    _, err := s.serviceEnforcer.AddPolicy(policy.Role.String(), policy.Resource.String(), policy.Action.String(), policy.Condition)
    return err
}

func (s *AuthzService) ValidateRoleHierarchy(userRole Role, requiredRole Role) bool {
    return userRole.HasPermissionLevel(requiredRole)
}
```

### Step 3: Create Handler Level Middleware

```go
// internal/api/v1/middleware/authz.go
package middleware

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "github.com/flexprice/flexprice/internal/ee/service"
    "github.com/flexprice/flexprice/internal/types"
)

func HandlerAuthzMiddleware(authzService service.AuthzInterface) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Get user from context (set by auth middleware)
        user, exists := c.Get("user")
        if !exists {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
            return
        }

                currentUser := user.(*domainUser.User)
        endpoint := Endpoint(c.FullPath())
        method := HTTPMethod(c.Request.Method)

        // Check handler level authorization (type-safe, no tenant consideration)
        allowed, err := authzService.CanAccessEndpoint(c.Request.Context(), currentUser.ID, endpoint, method)
        if err != nil {
            c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "authorization check failed"})
            return
        }

        if !allowed {
            c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
                "error": "access denied",
                "details": map[string]interface{}{
                    "endpoint": endpoint,
                    "method":   method,
                    "required_permissions": "insufficient privileges",
                },
            })
            return
        }

        c.Next()
    }
}
```

### Step 4: Implement Service Level Authorization

```go
// internal/ee/service/invoice.go (example)
package service

import (
    "context"
    "fmt"
    "github.com/flexprice/flexprice/internal/domain/invoice"
)

func (s *invoiceService) GetByID(ctx context.Context, id string) (*invoice.Invoice, error) {
    // Get current user from context
    userID := types.GetUserID(ctx)
    tenantID := types.GetTenantID(ctx)

    // First, get the invoice to check attributes
    inv, err := s.repo.GetByID(ctx, id)
    if err != nil {
        return nil, err
    }

        // Prepare attributes for ABAC evaluation (includes tenant check)
    attrs := map[string]interface{}{
        "resource_owner_id": inv.CreatedBy,
        "resource_status":   string(inv.Status),
        "invoice_amount":    inv.Amount,
        "created_at":        inv.CreatedAt,
        "entity_tenant_id":  inv.TenantID,  // Entity's tenant ID
        "user_tenant_id":    tenantID,      // User's tenant ID
    }

    // Check service level authorization (type-safe, tenant checked via attributes)
    allowed, err := s.authzService.CanAccessResource(
        ctx,
        userID,
        ResourceInvoice,
        ActionRead,
        attrs,
    )
    if err != nil {
        return nil, fmt.Errorf("authorization check failed: %w", err)
    }

    if !allowed {
        return nil, ierr.NewError("access denied to invoice").
            WithHint("you don't have permission to access this invoice").
            Mark(ierr.ErrForbidden)
    }

    return inv, nil
}

func (s *invoiceService) Update(ctx context.Context, id string, updates *invoice.UpdateInvoice) error {
    userID := types.GetUserID(ctx)
    tenantID := types.GetTenantID(ctx)

    // Get existing invoice for attribute checks
    existing, err := s.repo.GetByID(ctx, id)
    if err != nil {
        return err
    }

        attrs := map[string]interface{}{
        "resource_owner_id": existing.CreatedBy,
        "resource_status":   string(existing.Status),
        "is_paid":          existing.Status == types.StatusPaid,
        "amount_change":    updates.Amount != nil && *updates.Amount != existing.Amount,
        "entity_tenant_id": existing.TenantID,  // Entity's tenant ID
        "user_tenant_id":   tenantID,           // User's tenant ID
    }

    allowed, err := s.authzService.CanAccessResource(
        ctx,
        userID,
        ResourceInvoice,
        ActionUpdate,
        attrs,
    )
    if err != nil {
        return fmt.Errorf("authorization check failed: %w", err)
    }

    if !allowed {
        return ierr.NewError("access denied").
            WithHint("you don't have permission to update this invoice").
            Mark(ierr.ErrForbidden)
    }

    return s.repo.Update(ctx, id, updates)
}
```

### Step 5: Policy Seeding and Management

```go
// internal/ee/service/policy_seeder.go
package service

func (s *AuthzService) SeedPolicies(policySet PolicySet) error {
    // Add handler-level policies
    for _, policy := range policySet.HandlerPolicies {
        if err := s.AddEndpointPolicy(policy); err != nil {
            return fmt.Errorf("failed to add handler policy %+v: %w", policy, err)
        }
    }

    // Add service-level policies
    for _, policy := range policySet.ServicePolicies {
        if err := s.AddResourcePolicy(policy); err != nil {
            return fmt.Errorf("failed to add service policy %+v: %w", policy, err)
        }
    }

    return nil
}

// GetDefaultPolicySet returns the default policy configuration using type-safe enums
func GetDefaultPolicySet() PolicySet {
    return PolicySet{
        HandlerPolicies: []EndpointPolicy{
            // SuperAdmin permissions (access to everything)
            {RoleSuperAdmin, EndpointUsers, MethodGET},
            {RoleSuperAdmin, EndpointUsers, MethodPOST},
            {RoleSuperAdmin, EndpointUsersID, MethodPUT},
            {RoleSuperAdmin, EndpointUsersID, MethodDELETE},
            {RoleSuperAdmin, EndpointInvoices, MethodGET},
            {RoleSuperAdmin, EndpointInvoices, MethodPOST},
            {RoleSuperAdmin, EndpointInvoicesID, MethodPUT},
            {RoleSuperAdmin, EndpointInvoicesID, MethodDELETE},
            {RoleSuperAdmin, EndpointCustomers, MethodGET},
            {RoleSuperAdmin, EndpointCustomers, MethodPOST},
            {RoleSuperAdmin, EndpointCustomersID, MethodPUT},
            {RoleSuperAdmin, EndpointCustomersID, MethodDELETE},
            {RoleSuperAdmin, EndpointReports, MethodGET},
            {RoleSuperAdmin, EndpointReportsGenerate, MethodPOST},

            // Admin permissions
            {RoleAdmin, EndpointUsers, MethodGET},
            {RoleAdmin, EndpointUsers, MethodPOST},
            {RoleAdmin, EndpointUsersID, MethodGET},
            {RoleAdmin, EndpointUsersID, MethodPUT},
            {RoleAdmin, EndpointUsersID, MethodDELETE},
            {RoleAdmin, EndpointInvoices, MethodGET},
            {RoleAdmin, EndpointInvoices, MethodPOST},
            {RoleAdmin, EndpointInvoicesID, MethodPUT},
            {RoleAdmin, EndpointCustomers, MethodGET},
            {RoleAdmin, EndpointCustomers, MethodPOST},
            {RoleAdmin, EndpointCustomersID, MethodPUT},
            {RoleAdmin, EndpointReports, MethodGET},

            // Manager permissions
            {RoleManager, EndpointUsers, MethodGET},
            {RoleManager, EndpointUsersID, MethodGET},
            {RoleManager, EndpointInvoices, MethodGET},
            {RoleManager, EndpointInvoices, MethodPOST},
            {RoleManager, EndpointInvoicesID, MethodGET},
            {RoleManager, EndpointCustomers, MethodGET},
            {RoleManager, EndpointCustomersID, MethodGET},

            // User permissions (basic access)
            {RoleUser, EndpointUsersID, MethodGET},      // Can view specific users (with ABAC checks)
            {RoleUser, EndpointUsersID, MethodPUT},      // Can update specific users (with ABAC checks)
            {RoleUser, EndpointProfile, MethodGET},      // Can view own profile
            {RoleUser, EndpointProfile, MethodPUT},      // Can update own profile
            {RoleUser, EndpointInvoices, MethodGET},     // Can view invoices (with ABAC checks)
            {RoleUser, EndpointInvoicesID, MethodGET},   // Can view specific invoices (with ABAC checks)
        },

        ServicePolicies: []PolicyRule{
            // User permissions - can only access own resources in same tenant
            {RoleUser, ResourceUser, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id && (r.attrs.target_user_id == r.sub || r.attrs.is_self_access == true)"},
            {RoleUser, ResourceUser, ActionUpdate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id && r.attrs.target_user_id == r.sub"},
            {RoleUser, ResourceUser, ActionDelete, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id && r.attrs.target_user_id == r.sub"},

            {RoleUser, ResourceInvoice, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id && r.attrs.resource_owner_id == r.sub"},
            {RoleUser, ResourceInvoice, ActionUpdate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id && r.attrs.resource_owner_id == r.sub && r.attrs.resource_status != 'paid'"},

            {RoleUser, ResourceCustomer, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id && r.attrs.resource_owner_id == r.sub"},

            // Manager permissions - broader access within tenant
            {RoleManager, ResourceUser, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleManager, ResourceUser, ActionLookup, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},

            {RoleManager, ResourceInvoice, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleManager, ResourceInvoice, ActionCreate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleManager, ResourceInvoice, ActionUpdate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id && r.attrs.invoice_amount < 10000"},

            {RoleManager, ResourceCustomer, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleManager, ResourceCustomer, ActionCreate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleManager, ResourceCustomer, ActionUpdate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},

            {RoleManager, ResourcePlan, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleManager, ResourcePrice, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},

            // Admin permissions - full access within tenant
            {RoleAdmin, ResourceUser, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceUser, ActionCreate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceUser, ActionUpdate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceUser, ActionDelete, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceUser, ActionLookup, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},

            {RoleAdmin, ResourceInvoice, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceInvoice, ActionCreate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceInvoice, ActionUpdate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceInvoice, ActionDelete, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},

            {RoleAdmin, ResourceCustomer, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceCustomer, ActionCreate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceCustomer, ActionUpdate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourceCustomer, ActionDelete, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},

            {RoleAdmin, ResourcePlan, ActionRead, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourcePlan, ActionCreate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourcePlan, ActionUpdate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},
            {RoleAdmin, ResourcePlan, ActionDelete, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},

            {RoleAdmin, ResourceReport, ActionGenerate, "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"},

            // SuperAdmin permissions - access everything across all tenants
            {RoleSuperAdmin, ResourceUser, ActionRead, "true"},
            {RoleSuperAdmin, ResourceUser, ActionCreate, "true"},
            {RoleSuperAdmin, ResourceUser, ActionUpdate, "true"},
            {RoleSuperAdmin, ResourceUser, ActionDelete, "true"},
            {RoleSuperAdmin, ResourceUser, ActionLookup, "true"},

            {RoleSuperAdmin, ResourceInvoice, ActionRead, "true"},
            {RoleSuperAdmin, ResourceInvoice, ActionCreate, "true"},
            {RoleSuperAdmin, ResourceInvoice, ActionUpdate, "true"},
            {RoleSuperAdmin, ResourceInvoice, ActionDelete, "true"},

            {RoleSuperAdmin, ResourceCustomer, ActionRead, "true"},
            {RoleSuperAdmin, ResourceCustomer, ActionCreate, "true"},
            {RoleSuperAdmin, ResourceCustomer, ActionUpdate, "true"},
            {RoleSuperAdmin, ResourceCustomer, ActionDelete, "true"},

            {RoleSuperAdmin, ResourceTenant, ActionRead, "true"},
            {RoleSuperAdmin, ResourceTenant, ActionCreate, "true"},
            {RoleSuperAdmin, ResourceTenant, ActionUpdate, "true"},
            {RoleSuperAdmin, ResourceTenant, ActionDelete, "true"},

            {RoleSuperAdmin, ResourceReport, ActionGenerate, "true"},
        },
    }
}
```

### Step 6: Integration with Existing Code

```go
// internal/api/v1/invoice.go (updated)
package v1

import (
    "github.com/gin-gonic/gin"
    "github.com/flexprice/flexprice/internal/ee/service"
)

func NewInvoiceHandler(
    invoiceService service.InvoiceInterface,
    authzService service.AuthzInterface,
) *InvoiceHandler {
    return &InvoiceHandler{
        invoiceService: invoiceService,
        authzService:   authzService,
    }
}

func (h *InvoiceHandler) RegisterRoutes(r *gin.RouterGroup, authzMiddleware gin.HandlerFunc) {
    invoices := r.Group("/invoices")
    invoices.Use(authzMiddleware) // Handler level authorization
    {
        invoices.GET("/:id", h.GetByID)
        invoices.POST("", h.Create)
        invoices.PUT("/:id", h.Update)
        invoices.DELETE("/:id", h.Delete)
    }
}
```

## Migration Strategy

### Phase 1: Setup and Basic RBAC (Week 1-2)

1. Install Casbin dependencies
2. Create model configurations
3. Implement AuthzService with basic RBAC
4. Add handler level middleware to critical endpoints
5. Seed basic policies

### Phase 2: Service Level Authorization (Week 3-4)

1. Implement service level checks in critical services
2. Add ABAC policies for resource ownership
3. Test with existing functionality

### Phase 3: Fine-grained ABAC (Week 5-6)

1. Implement complex attribute-based policies
2. Add dynamic policy management
3. Performance optimization and caching

### Phase 4: Full Migration (Week 7-8)

1. Cover all endpoints and services
2. Remove legacy authorization code
3. Performance tuning and monitoring

## Testing Strategy

### Unit Tests

```go
// internal/ee/service/authz_test.go
func TestAuthzService_CanAccessEndpoint(t *testing.T) {
    tests := []struct {
        name     string
        userID   string
        endpoint string
        method   string
        tenantID string
        want     bool
    }{
        {
            name:     "admin can access users endpoint",
            userID:   "admin_user",
            endpoint: "/users",
            method:   "GET",
            tenantID: "tenant1",
            want:     true,
        },
        {
            name:     "regular user cannot access users endpoint",
            userID:   "regular_user",
            endpoint: "/users",
            method:   "GET",
            tenantID: "tenant1",
            want:     false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Setup test enforcer with test policies
            // Test authorization logic
        })
    }
}
```

### Integration Tests

```go
func TestInvoiceService_GetByID_Authorization(t *testing.T) {
    // Test that users can only access their own invoices
    // Test that managers can access department invoices
    // Test that admins can access all invoices
}
```

### Performance Tests

- Benchmark authorization checks
- Test with large policy sets
- Measure impact on API response times

## Benefits of This Approach

1. **Minimal Architecture Change**: Fits into existing clean architecture
2. **Gradual Migration**: Can be implemented incrementally
3. **Fine-grained Control**: Two levels of authorization
4. **Scalable**: Casbin handles complex policy evaluation efficiently
5. **Maintainable**: Policies are stored in database and can be managed dynamically
6. **Testable**: Each level can be tested independently
7. **Multi-tenant**: Built-in support for tenant isolation

## Type Safety Benefits

### Compile-Time Validation

```go
// ✅ Type-safe - compile-time validation
allowed, err := authzService.CanAccessResource(ctx, userID, authz.ResourceUser, authz.ActionRead, attrs)

// ❌ Error-prone - runtime validation only
allowed, err := authzService.CanAccessResource(ctx, userID, "user", "read", attrs)
```

### IDE Support & Autocomplete

```go
// IDE will autocomplete available roles
userRole := authz.RoleAdmin

// IDE will show available actions
action := authz.ActionDelete

// IDE will show available resources
resource := authz.ResourceInvoice
```

### Validation Methods

```go
// Role hierarchy checking
if userRole.HasPermissionLevel(authz.RoleManager) {
    // User has manager-level permissions or higher
}

// Role validation
if role.IsValid() {
    // Role is a valid enum value
}

// Resource validation
if resource.IsValid() {
    // Resource is a valid enum value
}
```

### Common Usage Patterns

#### 1. Simple Role Checking

```go
func (s *service) AdminOnlyMethod(ctx context.Context) error {
    userID := types.GetUserID(ctx)

    // Type-safe role checking
    isAdmin, err := s.authzService.HasRole(userID, authz.RoleAdmin)
    if err != nil || !isAdmin {
        return ierr.NewError("admin access required").Mark(ierr.ErrForbidden)
    }

    return s.performAdminOperation(ctx)
}
```

#### 2. Multiple Role Support

```go
func (s *service) ManagerOrAdminMethod(ctx context.Context) error {
    userID := types.GetUserID(ctx)

    // Check multiple roles
    allowedRoles := []authz.Role{authz.RoleManager, authz.RoleAdmin, authz.RoleSuperAdmin}
    hasRole, err := s.authzService.HasAnyRole(userID, allowedRoles)
    if err != nil || !hasRole {
        return ierr.NewError("insufficient permissions").Mark(ierr.ErrForbidden)
    }

    return s.performOperation(ctx)
}
```

#### 3. Hierarchy-Based Checking

```go
func (s *service) RequireMinimumRole(ctx context.Context, minimumRole authz.Role) error {
    userID := types.GetUserID(ctx)

    // Check if user has minimum required role level
    hasLevel, err := s.authzService.HasMinimumRole(userID, minimumRole)
    if err != nil || !hasLevel {
        return ierr.NewError("insufficient role level").Mark(ierr.ErrForbidden)
    }

    return nil
}
```

#### 4. Resource-Based Access Control

```go
func (s *invoiceService) GetByID(ctx context.Context, id string) (*Invoice, error) {
    userID := types.GetUserID(ctx)
    tenantID := types.GetTenantID(ctx)

    // Get invoice to check attributes
    invoice, err := s.repo.GetByID(ctx, id)
    if err != nil {
        return nil, err
    }

    // Prepare ABAC attributes
    attrs := map[string]interface{}{
        "entity_tenant_id": invoice.TenantID,
        "user_tenant_id":   tenantID,
        "resource_owner_id": invoice.CreatedBy,
        "invoice_status":   string(invoice.Status),
    }

    // Type-safe resource access check
    allowed, err := s.authzService.CanAccessResource(
        ctx, userID, authz.ResourceInvoice, authz.ActionRead, attrs)
    if err != nil || !allowed {
        return nil, ierr.NewError("access denied").Mark(ierr.ErrForbidden)
    }

    return invoice, nil
}
```

#### 5. Policy Definition with Type Safety

```go
func setupPolicies(authzService authz.AuthzInterface) error {
    // Type-safe policy definition
    policies := []authz.EndpointPolicy{
        {authz.RoleAdmin, authz.EndpointUsers, authz.MethodPOST},
        {authz.RoleManager, authz.EndpointInvoices, authz.MethodGET},
        {authz.RoleUser, authz.EndpointProfile, authz.MethodPUT},
    }

    for _, policy := range policies {
        if err := authzService.AddEndpointPolicy(policy); err != nil {
            return fmt.Errorf("failed to add policy %+v: %w", policy, err)
        }
    }

    return nil
}
```

### Error Prevention

#### Before (String-based)

```go
// ❌ Typos cause runtime errors
authzService.CanAccessResource(ctx, userID, "invoic", "raed", attrs) // typos!
authzService.HasRole(userID, "admn") // typo!
```

#### After (Type-safe)

```go
// ✅ Compile-time error prevention
authzService.CanAccessResource(ctx, userID, authz.ResourceInvoice, authz.ActionRead, attrs)
authzService.HasRole(userID, authz.RoleAdmin)
```

## Key Changes From Standard Approach

### Handler Level Simplifications

- **Removed tenant parameter** from RBAC model
- **Static role-based permissions** only (user, manager, admin, superadmin)
- **No tenant consideration** at handler level for simpler policy management
- **Faster evaluation** since no tenant lookup required

### Service Level Enhancements

- **Dynamic tenant checking** via entity attributes
- **Mandatory tenant validation** using `entity_tenant_id == user_tenant_id`
- **Flexible attribute-based conditions** for ownership, status, etc.
- **Cross-tenant access** only for superadmin role

### Policy Structure

```
Handler Level: [role, endpoint, method]
Examples:
- {"admin", "/users", "POST"}
- {"manager", "/invoices", "GET"}
- {"user", "/users/:id", "PUT"}

Service Level: [role, resource, action, condition]
Examples:
- {"user", "user", "read", "r.attrs.entity_tenant_id == r.attrs.user_tenant_id && r.attrs.target_user_id == r.sub"}
- {"manager", "invoice", "update", "r.attrs.entity_tenant_id == r.attrs.user_tenant_id && r.attrs.invoice_amount < 10000"}
- {"admin", "customer", "delete", "r.attrs.entity_tenant_id == r.attrs.user_tenant_id"}
```

### Role Hierarchy

1. **user**: Own resources within tenant
2. **manager**: All resources within tenant (with some restrictions)
3. **admin**: All resources within tenant
4. **superadmin**: All resources across all tenants

## Conclusion

This implementation provides a robust, scalable authorization solution that integrates seamlessly with your existing Go backend architecture. The two-level approach ensures both performance and flexibility, while maintaining the clean architecture principles you're already following.

The simplified handler-level RBAC combined with dynamic service-level ABAC gives you the best of both worlds: fast endpoint protection and fine-grained business logic control with automatic tenant isolation.
