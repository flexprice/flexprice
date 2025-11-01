# Flexprice RBAC System

## Document Information
- **Version**: 2.0
- **Last Updated**: November 1, 2025
- **Status**: Draft
- **Owner**: Engineering Team
- **Major Changes**: Simplified to explicit permission declarations with set-based lookups. Added name/description to roles for UI/UX.

---

## Table of Contents
1. [Executive Summary](#executive-summary)
2. [Goals and Objectives](#goals-and-objectives)
3. [System Overview](#system-overview)
4. [Data Models](#data-models)
5. [Role Definition System](#role-definition-system)
6. [Workflows](#workflows)
7. [Permission Enforcement](#permission-enforcement)
8. [API Specifications](#api-specifications)
9. [Migration Strategy](#migration-strategy)
10. [Security Considerations](#security-considerations)
11. [Future Enhancements: Permit.io Integration](#future-enhancements-permitio-integration)
12. [Open Questions](#open-questions)

---

## Executive Summary

Flexprice's RBAC (Role-Based Access Control) system introduces fine-grained access control to manage permissions for different types of users and service accounts. This system allows organizations to:

- Create service accounts with limited, role-based permissions
- Maintain backward compatibility with existing users (full access by default)
- Enforce permissions at the API key level
- Scale to support future permission requirements through static role definitions
- Integrate with external authorization systems (Permit.io) in the future

---

## Goals and Objectives

### Primary Goals
1. **Service Account Support**: Enable creation of service accounts with limited permissions for specific operations
2. **Security Enhancement**: Reduce security risk by implementing principle of least privilege
3. **Backward Compatibility**: Ensure existing users and workflows continue to function without changes
4. **Maintainability**: Design a system that is easy to extend with new roles and permissions
5. **Performance**: Minimize overhead in permission checking for high-throughput operations

### Success Metrics
- Zero breaking changes for existing users
- < 1ms latency added for permission checks (O(roles) with O(1) lookups)
- Ability to add new roles by editing roles.json (no code changes)
- Explicit permission declarations at route level

### Non-Goals (Phase 1)
- Dynamic role creation by end users
- Fine-grained object-level permissions
- Role hierarchy or inheritance
- UI for role management
- Audit logging for permission denials (future phase)
- Automatic endpoint-to-permission mapping

---

## System Overview

### Architecture Principles
1. **Static Role Definitions**: Roles are defined in `roles.json` managed by Flexprice engineering
2. **API Key-Based Enforcement**: Permissions are checked at the API key level, not user level
3. **Explicit Permission Declarations**: Each route explicitly declares required permissions
4. **Set-Based Lookups**: O(1) permission checks using nested maps
5. **Fail-Open for Regular Users**: Regular users without roles retain full access
6. **Fail-Closed for Service Accounts**: Service accounts must have explicit roles

### Key Components
```
┌─────────────────────────────────────────────────────────────┐
│                    API Request (with API Key)               │
└───────────────────────────┬─────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│              Authentication Middleware                      │
│              (Validates API Key)                            │
└───────────────────────────┬─────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│     Permission Middleware: RequirePermission(entity, action)│
│     1. Get roles from secret (in context)                   │
│     2. Check: permissions[role][entity][action] == true     │
│     3. Return 403 if denied, continue if allowed            │
└───────────────────────────┬─────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│              Route Handler (Business Logic)                 │
└─────────────────────────────────────────────────────────────┘
```

### Design Philosophy

**Simplicity Over Complexity**
- No automatic endpoint mapping
- No regex pattern matching
- No separate mapping configuration files
- Permissions declared explicitly in route definitions

**Performance**
- Set-based lookups: `permissions[role][entity][action]` = O(1)
- Complexity: O(roles) where roles typically = 1-3
- No JSON parsing on request path
- Minimal memory overhead (~16KB for typical setup)

**Role Metadata for UI/UX**
- Role definitions include `name` and `description` fields
- Used exclusively for UI dropdowns and documentation
- **Zero performance impact**: permission checks never touch name/description
- Stored separately in memory - only accessed for `GET /api/v1/rbac/roles` endpoint
- Enables dynamic role selection in frontend without hardcoding role lists

---

## Data Models

### User Schema Enhancements

#### New Fields for `users` Table

```go
type User struct {
    // ... existing fields ...
    
    // Type defines the category of user account
    // Enum: "user" | "service_account"
    // Default: "user"
    Type string `json:"type" db:"type"`
    
    // Roles is a JSON array of role identifiers assigned to this user
    // Example: ["event_ingestor", "metrics_reader"]
    // Default: [] (empty array)
    Roles []string `json:"roles" db:"roles"`
}
```

#### Field Specifications

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `type` | enum(string) | No | `"user"` | Must be "user" or "service_account" | Determines if account is human or automated |
| `roles` | array[string] | No | `[]` | Each role must exist in role definitions | List of roles assigned to user |

### Secrets Schema Enhancements

#### New Fields for `secrets` Table

```go
type Secret struct {
    // ... existing fields ...
    
    // Roles is copied from the user at API key creation time
    // This allows permission checks without joining to users table
    Roles []string `json:"roles" db:"roles"`
    
    // Optional: UserType to distinguish service account keys
    UserType string `json:"user_type" db:"user_type"`
}
```

#### Design Decision: Denormalization
**Why copy roles to secrets table?**
- **Performance**: Permission checks happen on every API request; avoiding JOIN with users table
- **Immutability**: API keys have fixed permissions at creation time (clearer security model)
- **Independence**: API key permissions don't change if user roles are modified later

**Trade-off**: 
- ✅ Faster permission checks
- ❌ Requires regenerating API keys to change permissions
- **Decision**: Accept this trade-off for better performance and clearer security semantics

---

## Role Definition System

### Role Definition File Structure

Roles are defined in a **simple JSON file**: `internal/config/rbac/roles.json`

**Design Principle**: Keep it simple - role definitions include name, description, and permissions. The name and description are used for UI/UX (dropdowns, role selection), while permissions are optimized for fast lookups.

```json
{
  "event_ingestor": {
    "name": "Event Ingestor",
    "description": "Limited to ingesting events and batch events. Use for services that only send events.",
    "permissions": {
      "event": ["create", "write"],
      "batch_event": ["create"]
    }
  },
  "customer_manager": {
    "name": "Customer Manager",
    "description": "Full access to customer data and read-only access to subscriptions and invoices.",
    "permissions": {
      "customer": ["create", "read", "update", "delete", "list"],
      "subscription": ["read", "list"],
      "invoice": ["read", "list"]
    }
  },
  "metrics_reader": {
    "name": "Metrics Reader",
    "description": "Read-only access to metrics, analytics, and dashboards for reporting.",
    "permissions": {
      "metrics": ["read", "list"],
      "analytics": ["read"],
      "dashboard": ["read"]
    }
  },
  "feature_manager": {
    "name": "Feature Manager",
    "description": "Full access to feature flags and configurations.",
    "permissions": {
      "feature": ["create", "read", "update", "delete", "list"],
      "feature_flag": ["create", "read", "update", "toggle", "delete"]
    }
  },
  "billing_admin": {
    "name": "Billing Administrator",
    "description": "Manage invoices, subscriptions, and payment operations.",
    "permissions": {
      "invoice": ["create", "read", "update", "delete", "list"],
      "subscription": ["create", "read", "update", "delete", "list"],
      "payment": ["create", "read", "list"]
    }
  },
  "pricing_admin": {
    "name": "Pricing Administrator",
    "description": "Manage pricing models, plans, and pricing calculations.",
    "permissions": {
      "pricing": ["create", "read", "update", "delete", "execute"],
      "pricing_model": ["create", "read", "update", "delete"],
      "plan": ["create", "read", "update", "delete", "list"]
    }
  },
  "admin": {
    "name": "Administrator",
    "description": "Full access to all resources. Use sparingly and only for trusted administrators.",
    "permissions": {
      "customer": ["create", "read", "update", "delete", "list"],
      "subscription": ["create", "read", "update", "delete", "list"],
      "invoice": ["create", "read", "update", "delete", "list"],
      "event": ["create", "read", "write", "delete", "list"],
      "metrics": ["read", "list"],
      "feature": ["create", "read", "update", "delete", "list"],
      "pricing": ["create", "read", "update", "delete", "execute"]
    }
  }
}
```

**Why include name and description?**
- **UI/UX**: When creating service accounts in the frontend, users see friendly names and descriptions in dropdowns
- **Single source of truth**: Backend owns all role definitions; frontend fetches via API
- **Maintainability**: Add new role = edit JSON only, no frontend code changes
- **Zero performance impact**: name/description are never used in permission checks (hot path)

### Permission Model

#### Entity Definitions
Entities represent resources in the Flexprice system:

| Entity | Description | Example Routes |
|--------|-------------|----------------|
| `event` | Individual events | POST /api/v1/events |
| `batch_event` | Batch event operations | POST /api/v1/events/batch |
| `metrics` | Usage metrics | GET /api/v1/metrics |
| `analytics` | Analytics data | GET /api/v1/analytics |
| `feature` | Feature configurations | /api/v1/features/* |
| `customer` | Customer resources | /api/v1/customers/* |
| `subscription` | Subscription management | /api/v1/subscriptions/* |
| `invoice` | Invoices | /api/v1/invoices/* |
| `pricing` | Pricing configurations | /api/v1/pricing/* |
| `plan` | Pricing plans | /api/v1/plans/* |
| `payment` | Payment operations | /api/v1/payments/* |
| `wallet` | Wallet management | /api/v1/wallets/* |

#### Action Definitions
Standard CRUD actions plus custom operations:

| Action | Description | Typical HTTP Methods |
|--------|-------------|---------------------|
| `create` | Create new resources | POST |
| `read` | Read single resource | GET /:id |
| `list` | List/query resources | GET / |
| `update` | Modify existing resources | PUT, PATCH |
| `delete` | Remove resources | DELETE |
| `write` | Combined create/update | POST, PUT |
| `execute` | Execute operations | POST |
| `toggle` | Toggle states | POST |
```

### RBAC Service Implementation

**Core Implementation**: Load roles.json at startup, store metadata separately, and convert permissions to set-based structure for O(1) lookups.

**Key Design**: Permission checks never touch name/description - they're stored separately and only used for API responses.

```go
// internal/rbac/service.go
package rbac

import (
    "encoding/json"
    "os"
)

// Service handles permission checks with set-based lookups
type Service struct {
    // Fast lookup for permission checks (hot path - O(1))
    permissions map[string]map[string]map[string]bool
    
    // Metadata for API responses (cold path - rarely accessed)
    roles map[string]*Role
}

// Role represents a role with metadata
type Role struct {
    ID          string              `json:"id"`
    Name        string              `json:"name"`
    Description string              `json:"description"`
    Permissions map[string][]string `json:"permissions"`
}

// NewService loads roles.json and optimizes for fast lookups
func NewService(configPath string) (*Service, error) {
    // Load JSON
    data, err := os.ReadFile(configPath)
    if err != nil {
        return nil, err
    }
    
    // Parse as: role_id -> role definition (with name, description, permissions)
    var rawConfig map[string]*Role
    if err := json.Unmarshal(data, &rawConfig); err != nil {
        return nil, err
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
    
    return &Service{
        permissions: permissions,
        roles:       rawConfig,
    }, nil
}

// HasPermission checks if any of the user's roles grant permission
// Complexity: O(roles) with O(1) lookups = ~3 operations for typical use
// NOTE: Never touches role.Name or role.Description - zero overhead
func (s *Service) HasPermission(roles []string, entity string, action string) bool {
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
func (s *Service) ValidateRole(roleName string) bool {
    _, exists := s.permissions[roleName]
    return exists
}

// GetAllRoles returns all roles with metadata (for API endpoint)
// This is called rarely (only when fetching available roles for UI)
func (s *Service) GetAllRoles() []*Role {
    result := make([]*Role, 0, len(s.roles))
    for _, role := range s.roles {
        result = append(result, role)
    }
    return result
}

// GetRole returns a specific role with metadata
func (s *Service) GetRole(roleID string) (*Role, bool) {
    role, exists := s.roles[roleID]
    return role, exists
}
```

**Performance Notes**:
- `HasPermission()`: O(roles) with O(1) lookups - **never** touches name/description
- `GetAllRoles()`: Called only when UI needs to show role dropdown - not performance critical
- Memory overhead: ~1-2 KB for metadata (7 roles × ~200 bytes each)

**Caching Strategy**:
- Load roles once at application startup
- Store in memory (roles.json rarely changes)
- Optional: Provide admin endpoint to reload: `POST /internal/rbac/reload`
- Optional: Hot-reload with file watcher in development mode

---

## Workflows

### Workflow 1: Create User with Type and Roles

#### 1A: Creating Regular User (Default)

```
User: POST /api/v1/users
Request Body:
{
    "email": "user@example.com",
    "name": "John Doe"
    // type not specified → defaults to "user"
    // roles not specified → defaults to []
}

System:
1. Create user with type="user", roles=[]
2. Return user object

Result: User has complete access to all resources
```

#### 1B: Creating Service Account with Roles

```
Admin: POST /api/v1/users
Request Body:
{
    "email": "event-service@example.com",
    "name": "Event Ingestion Service",
    "type": "service_account",
    "roles": ["event_ingestor"]
}

System:
1. Validate type is "service_account"
2. Validate roles is non-empty (required for service accounts)
3. Validate each role exists in role definitions
4. Create user with specified type and roles
5. Return user object

Result: Service account limited to event ingestion permissions
```

#### Validation Rules

| Rule | Condition | Error Message |
|------|-----------|---------------|
| R1 | `type == "service_account" && len(roles) == 0` | "Service accounts must have at least one role assigned" |
| R2 | `role not in role_definitions` | "Invalid role: {role}. Available roles: [list]" |
| R3 | `type not in ["user", "service_account"]` | "Invalid type. Must be 'user' or 'service_account'" |

### Workflow 2: Create API Key with Permission Inheritance

#### 2A: Create API Key for Current User (No user_id param)

```
User: POST /api/v1/secrets
Request Body:
{
    "name": "Production API Key"
    // user_id not specified
}

System:
1. Extract user_id from JWT/session context
2. Fetch user object from database
3. Create API key with:
   - user_id = context.user_id
   - roles = user.roles (copy from user)
   - user_type = user.type
4. Generate and return API key

Result: API key inherits permissions from the user who created it
```

#### 2B: Create API Key for Service Account (user_id param provided)

```
Admin: POST /api/v1/secrets
Request Body:
{
    "name": "Event Ingestion Key",
    "user_id": "service_account_123"
}

System:
1. Validate user_id parameter
2. Fetch user object by user_id
3. Validate user exists and type == "service_account"
4. Create API key with:
   - user_id = request.user_id
   - roles = user.roles (copy from service account)
   - user_type = "service_account"
5. Generate and return API key

Result: API key has limited permissions based on service account roles
```

#### Authorization Rules

| Scenario | Allowed? | Notes |
|----------|----------|-------|
| Regular user creates key for themselves | ✅ | Default behavior |
| Regular user creates key for another user | ❌ | Requires admin privileges |
| Admin creates key for any user/service account | ✅ | Admin bypass |
| Service account creates key for themselves | ⚠️ | Design decision needed |

**Recommendation**: Service accounts should NOT be able to create new API keys (prevents privilege escalation).

### Workflow 3: API Request with Permission Enforcement

```
Client: GET /api/v1/features
Headers:
  Authorization: Bearer {api_key}

System Flow:

1. Authentication Middleware:
   ├─ Extract API key from Authorization header
   ├─ Query secrets table for key
   ├─ If not found → 401 Unauthorized
   └─ If found → Load secret object (includes roles), add to context

2. Permission Middleware: RequirePermission("feature", "list")
   ├─ Get secret from context
   ├─ Check if roles array is empty
   │  ├─ If empty → ALLOW (full access - backward compatibility)
   │  └─ If not empty → Continue to permission check
   │
   ├─ Call rbacService.HasPermission(secret.Roles, "feature", "list")
   │  └─ O(1) lookup: permissions[role]["feature"]["list"]
   │
   ├─ If any role grants permission → ALLOW, continue
   └─ If no role grants permission → 403 Forbidden

3. Route Handler:
   └─ Execute business logic

Response:
- 200 OK (with data) if permission granted
- 403 Forbidden if permission denied
```

**Key Point**: Permission check is **explicit** in route definition, not automatic mapping.

### Workflow 4: Managing Service Account Lifecycle

#### Update Service Account Roles

```
Admin: PATCH /api/v1/users/{user_id}
Request Body:
{
    "roles": ["event_ingestor", "metrics_reader"]
}

System:
1. Validate user_id exists and type == "service_account"
2. Validate all roles exist in definitions
3. Update user.roles in database
4. Return updated user object

⚠️ Important: Existing API keys are NOT updated
Users must regenerate API keys to apply new permissions
```

#### Regenerate API Key with New Permissions

```
Workflow:
1. User updates service account roles
2. User deletes old API key(s): DELETE /api/v1/secrets/{key_id}
3. User creates new API key: POST /api/v1/secrets
4. New key inherits updated roles from service account
5. User updates API key in their service/application
```

---

## Permission Enforcement

### Middleware Implementation

**Design**: Explicit permission declarations at route level - no automatic mapping.

#### Permission Middleware Structure

```go
// internal/middleware/permission.go
package middleware

import (
    "fmt"
    "net/http"
    "github.com/gin-gonic/gin"
    "github.com/flexprice/flexprice/internal/rbac"
)

type PermissionMiddleware struct {
    rbacService *rbac.Service
}

func NewPermissionMiddleware(rbacService *rbac.Service) *PermissionMiddleware {
    return &PermissionMiddleware{rbacService: rbacService}
}

// RequirePermission returns a middleware that checks for specific entity.action
// This is called explicitly in route definitions
func (pm *PermissionMiddleware) RequirePermission(entity string, action string) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 1. Get secret from context (set by auth middleware)
        secretInterface, exists := c.Get("secret")
        if !exists {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
                "error": "Unauthorized",
            })
            return
        }
        
        secret := secretInterface.(*models.Secret)
        
        // 2. Check permission using set-based lookup
        if !pm.rbacService.HasPermission(secret.Roles, entity, action) {
            log.Info("Permission denied: user=%s, roles=%v, entity=%s, action=%s",
                secret.UserID, secret.Roles, entity, action)
            
            c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
                "error": "Forbidden",
                "message": fmt.Sprintf("Insufficient permissions to %s %s", action, entity),
            })
            return
        }
        
        // 3. Permission granted, continue to handler
        c.Next()
    }
}
```

**Simplicity**: ~40 lines total. No regex, no mapping files, no complexity.

#### Router Registration

```go
// internal/api/router.go
func SetupRoutes(
    router *gin.Engine, 
    handlers *Handlers, 
    permMW *middleware.PermissionMiddleware,
) {
    v1 := router.Group("/v1")
    v1.Use(middleware.AuthenticateMiddleware())
    
    // Customer routes - explicit permission declarations
    customer := v1.Group("/customers")
    {
        customer.POST("", 
            permMW.RequirePermission("customer", "create"), 
            handlers.Customer.CreateCustomer)
            
        customer.GET("", 
            permMW.RequirePermission("customer", "list"), 
            handlers.Customer.ListCustomers)
            
        customer.GET("/:id", 
            permMW.RequirePermission("customer", "read"), 
            handlers.Customer.GetCustomer)
            
        customer.PUT("/:id", 
            permMW.RequirePermission("customer", "update"), 
            handlers.Customer.UpdateCustomer)
            
        customer.DELETE("/:id", 
            permMW.RequirePermission("customer", "delete"), 
            handlers.Customer.DeleteCustomer)
    }
    
    // Event routes
    events := v1.Group("/events")
    {
        events.POST("", 
            permMW.RequirePermission("event", "create"), 
            handlers.Events.CreateEvent)
            
        events.POST("/batch", 
            permMW.RequirePermission("batch_event", "create"), 
            handlers.Events.CreateBatchEvents)
    }
    
    // Subscription routes
    subscription := v1.Group("/subscriptions")
    {
        subscription.POST("", 
            permMW.RequirePermission("subscription", "create"), 
            handlers.Subscription.Create)
            
        subscription.GET("", 
            permMW.RequirePermission("subscription", "list"), 
            handlers.Subscription.List)
            
        subscription.GET("/:id", 
            permMW.RequirePermission("subscription", "read"), 
            handlers.Subscription.Get)
            
        subscription.PUT("/:id", 
            permMW.RequirePermission("subscription", "update"), 
            handlers.Subscription.Update)
    }
}
```

**Benefits**:
- ✅ Self-documenting - see permission at route definition
- ✅ No configuration drift - route and permission always in sync
- ✅ Easy to review - see all permissions in router file
- ✅ Type-safe - compiler catches typos
- ✅ No regex overhead

### Error Responses

#### 403 Forbidden Response Format

```json
{
  "error": "Forbidden",
  "message": "Insufficient permissions to create feature",
  "details": {
    "required_permission": {
      "entity": "feature",
      "action": "create"
    },
    "user_roles": ["event_ingestor"]
  }
}
```

**Design Decision**: Include permission details in error response?
- ✅ **Recommended**: Include details (helps debugging for developers)
- Include `details` only in development/staging environments
- In production, provide generic "Forbidden" message to avoid information leakage

### Bypassing Permission Checks

#### Exempt Endpoints

Some endpoints don't require permission checks:
- Public health checks: `/health`, `/metrics`
- Public auth endpoints: `/auth/login`, `/auth/signup`
- Swagger documentation: `/swagger/*`

**Implementation**: Simply don't add `RequirePermission` middleware to these routes.

```go
// Public routes - no auth, no permissions
router.GET("/health", handlers.Health)
router.GET("/metrics", handlers.Metrics)

// Public auth routes - auth but no permissions
public := router.Group("/v1")
public.POST("/auth/login", handlers.Auth.Login)
public.POST("/auth/signup", handlers.Auth.Signup)

// Protected routes - add RequirePermission explicitly
v1 := router.Group("/v1")
v1.Use(middleware.AuthenticateMiddleware())
{
    // Only these have permission checks
    v1.POST("/customers", 
        permMW.RequirePermission("customer", "create"),
        handlers.Customer.Create)
}
```

**No global middleware approach** - permissions are opt-in per route.

---

## API Specifications

### User Management APIs

#### Create User

```http
POST /api/v1/users
Content-Type: application/json
Authorization: Bearer {admin_token}

Request Body:
{
  "email": "string",
  "name": "string",
  "type": "user | service_account",  // Optional, default: "user"
  "roles": ["string"]                 // Optional, default: []
}

Response: 201 Created
{
  "id": "user_123",
  "email": "event-service@example.com",
  "name": "Event Ingestion Service",
  "type": "service_account",
  "roles": ["event_ingestor"],
  "created_at": "2025-10-31T10:00:00Z"
}

Errors:
- 400 Bad Request: Invalid type or roles
- 422 Unprocessable Entity: Service account without roles
```

#### Update User Roles

```http
PATCH /api/v1/users/{user_id}
Content-Type: application/json
Authorization: Bearer {admin_token}

Request Body:
{
  "roles": ["event_ingestor", "metrics_reader"]
}

Response: 200 OK
{
  "id": "user_123",
  "email": "event-service@example.com",
  "name": "Event Ingestion Service",
  "type": "service_account",
  "roles": ["event_ingestor", "metrics_reader"],
  "updated_at": "2025-10-31T11:00:00Z"
}

Errors:
- 404 Not Found: User not found
- 400 Bad Request: Invalid roles
```

#### Get User

```http
GET /api/v1/users/{user_id}
Authorization: Bearer {token}

Response: 200 OK
{
  "id": "user_123",
  "email": "event-service@example.com",
  "name": "Event Ingestion Service",
  "type": "service_account",
  "roles": ["event_ingestor"],
  "created_at": "2025-10-31T10:00:00Z"
}
```

#### List Available Roles

```http
GET /api/v1/rbac/roles
Authorization: Bearer {admin_token}

Response: 200 OK
{
  "roles": [
    {
      "id": "event_ingestor",
      "name": "Event Ingestor",
      "description": "Limited to ingesting events and batch events. Use for services that only send events.",
      "permissions": {
        "event": ["create", "write"],
        "batch_event": ["create"]
      }
    },
    {
      "id": "customer_manager",
      "name": "Customer Manager",
      "description": "Full access to customer data and read-only access to subscriptions and invoices.",
      "permissions": {
        "customer": ["create", "read", "update", "delete", "list"],
        "subscription": ["read", "list"],
        "invoice": ["read", "list"]
      }
    },
    {
      "id": "metrics_reader",
      "name": "Metrics Reader",
      "description": "Read-only access to metrics, analytics, and dashboards for reporting.",
      "permissions": {
        "metrics": ["read", "list"],
        "analytics": ["read"],
        "dashboard": ["read"]
      }
    },
    {
      "id": "admin",
      "name": "Administrator",
      "description": "Full access to all resources. Use sparingly and only for trusted administrators.",
      "permissions": {
        "customer": ["create", "read", "update", "delete", "list"],
        "subscription": ["create", "read", "update", "delete", "list"],
        "invoice": ["create", "read", "update", "delete", "list"],
        "event": ["create", "read", "write", "delete", "list"],
        "metrics": ["read", "list"],
        "feature": ["create", "read", "update", "delete", "list"],
        "pricing": ["create", "read", "update", "delete", "execute"]
      }
    }
  ]
}
```

**Use Case**: Frontend calls this endpoint when displaying the "Create Service Account" form to populate the role dropdown with friendly names and descriptions.

### API Key Management

#### Create API Key

```http
POST /api/v1/secrets
Content-Type: application/json
Authorization: Bearer {token}

Request Body:
{
  "name": "string",
  "user_id": "string"  // Optional: if provided, creates key for that user
}

Response: 201 Created
{
  "id": "key_123",
  "name": "Production Key",
  "user_id": "user_123",
  "api_key": "fp_live_xxxxxxxxxxxxxx",  // Only returned once
  "roles": ["event_ingestor"],
  "created_at": "2025-10-31T10:00:00Z"
}

Note: api_key is only returned in creation response. Store securely.
```

#### List API Keys

```http
GET /api/v1/secrets
Authorization: Bearer {token}

Response: 200 OK
{
  "secrets": [
    {
      "id": "key_123",
      "name": "Production Key",
      "user_id": "user_123",
      "roles": ["event_ingestor"],
      "last_used": "2025-10-31T09:00:00Z",
      "created_at": "2025-10-31T08:00:00Z"
      // Note: api_key value is NOT included in list
    }
  ]
}
```

#### Delete API Key

```http
DELETE /api/v1/secrets/{key_id}
Authorization: Bearer {token}

Response: 204 No Content
```

### Internal/Admin APIs

#### Reload RBAC Configuration

```http
POST /internal/rbac/reload
Authorization: Bearer {internal_token}

Response: 200 OK
{
  "message": "RBAC configuration reloaded successfully",
  "roles_loaded": 7
}
```

**Note**: This endpoint reloads `roles.json` without restarting the application. Useful for hot-reloading role changes in development.

---

## Migration Strategy

### Phase 1: Database Schema Migrations

#### Migration 1: Add Fields to Users Table

```sql
-- Migration: 001_add_rbac_to_users.up.sql

-- Add type column
ALTER TABLE users ADD COLUMN type VARCHAR(50) DEFAULT 'user' NOT NULL;

-- Add roles column (JSON array)
ALTER TABLE users ADD COLUMN roles JSONB DEFAULT '[]'::jsonb NOT NULL;

-- Add indexes for performance
CREATE INDEX idx_users_type ON users(type);
CREATE INDEX idx_users_roles ON users USING GIN (roles);

-- Add check constraint
ALTER TABLE users ADD CONSTRAINT check_user_type 
    CHECK (type IN ('user', 'service_account'));

COMMENT ON COLUMN users.type IS 'User account type: user or service_account';
COMMENT ON COLUMN users.roles IS 'Array of role identifiers assigned to user';
```

```sql
-- Migration: 001_add_rbac_to_users.down.sql

DROP INDEX IF EXISTS idx_users_roles;
DROP INDEX IF EXISTS idx_users_type;

ALTER TABLE users DROP CONSTRAINT IF EXISTS check_user_type;
ALTER TABLE users DROP COLUMN IF EXISTS roles;
ALTER TABLE users DROP COLUMN IF EXISTS type;
```

#### Migration 2: Add Fields to Secrets Table

```sql
-- Migration: 002_add_rbac_to_secrets.up.sql

-- Add roles column (denormalized from users)
ALTER TABLE secrets ADD COLUMN roles JSONB DEFAULT '[]'::jsonb NOT NULL;

-- Add user_type column (optional, for faster lookups)
ALTER TABLE secrets ADD COLUMN user_type VARCHAR(50) DEFAULT 'user' NOT NULL;

-- Add index
CREATE INDEX idx_secrets_roles ON secrets USING GIN (roles);

COMMENT ON COLUMN secrets.roles IS 'Roles copied from user at key creation time';
COMMENT ON COLUMN secrets.user_type IS 'User type copied from user at key creation time';
```

```sql
-- Migration: 002_add_rbac_to_secrets.down.sql

DROP INDEX IF EXISTS idx_secrets_roles;
ALTER TABLE secrets DROP COLUMN IF EXISTS user_type;
ALTER TABLE secrets DROP COLUMN IF EXISTS roles;
```

### Phase 2: Backward Compatibility Data Migration

#### Backfill Existing Data

```sql
-- Backfill existing users with default values
UPDATE users 
SET type = 'user', 
    roles = '[]'::jsonb 
WHERE type IS NULL OR roles IS NULL;

-- Backfill existing secrets with empty roles
UPDATE secrets 
SET roles = '[]'::jsonb,
    user_type = 'user'
WHERE roles IS NULL;
```

**Verification Query**:
```sql
-- Check all users have valid types
SELECT COUNT(*) FROM users WHERE type NOT IN ('user', 'service_account');
-- Expected: 0

-- Check all secrets have roles field
SELECT COUNT(*) FROM secrets WHERE roles IS NULL;
-- Expected: 0
```

### Phase 3: Code Deployment

#### Deployment Steps

1. **Deploy Schema Changes**
   - Run migrations on staging
   - Verify all tables updated correctly
   - Run backfill scripts
   - Run migrations on production

2. **Deploy Application Code (Feature Flag OFF)**
   - Deploy new code with RBAC feature flag disabled
   - Update ORM models to include new fields
   - Verify no errors in logs
   - Monitor for 24 hours

3. **Enable Feature Flag Gradually**
   - Enable for internal testing (1%)
   - Enable for staging environment (100%)
   - Enable for production (10% → 50% → 100%)

4. **Monitoring**
   - Track permission check latency
   - Monitor 403 error rates
   - Alert on unexpected permission denials

### Rollback Plan

If issues are detected:

1. **Disable Feature Flag** (Immediate - 0 downtime)
   ```
   Set RBAC_ENABLED=false
   Restart services
   ```

2. **Rollback Code** (if necessary)
   ```
   Deploy previous version
   ```

3. **Rollback Database** (last resort - requires downtime)
   ```
   Run down migrations
   May require data recovery if service accounts were created
   ```

---

## Security Considerations

### Threat Model

#### Threat 1: Privilege Escalation via API Key Creation

**Scenario**: Service account creates API key for itself with broader permissions

**Mitigation**:
- Service accounts CANNOT create API keys (enforced by permission check)
- Only admins or regular users can create keys
- API keys inherit permissions from user at creation time (immutable)

#### Threat 2: Role Definition Tampering

**Scenario**: Attacker modifies role definitions JSON file

**Mitigation**:
- Role definition files stored in source control
- File permissions restricted (read-only for application process)
- Configuration reloading requires admin authentication
- Audit log for configuration changes

#### Threat 3: Permission Check Bypass

**Scenario**: Bug in middleware allows bypassing permission checks

**Mitigation**:
- Fail-closed by default (deny if error occurs)
- Comprehensive unit tests for permission logic
- Integration tests covering all endpoints
- Regular security audits of middleware code

#### Threat 4: Token Theft

**Scenario**: Service account API key is stolen

**Mitigation**:
- API keys have limited permissions (RBAC)
- Implement rate limiting per key
- Monitor for unusual activity patterns
- Ability to immediately revoke compromised keys
- Recommend key rotation policies

### Security Best Practices

#### For Developers

1. **Never skip permission checks** - All authenticated endpoints must have permission middleware
2. **Fail closed** - If permission check errors, deny access
3. **Log permission denials** - Log all 403 responses for audit
4. **Validate role definitions** - Unit tests for all role configurations
5. **Least privilege** - Create minimal roles with only required permissions

#### For Operations

1. **Rotate service account keys** - Recommended: 90 day rotation
2. **Monitor 403 errors** - Alert on spikes (potential attack or misconfiguration)
3. **Regular access reviews** - Quarterly review of service account permissions
4. **Secure role definitions** - Protect role JSON files with restrictive permissions
5. **Audit logging** - Log all permission changes and denials

### Compliance Considerations

#### Audit Requirements

For compliance (SOC2, ISO 27001, etc.), maintain:

1. **Access logs**: Who accessed what resources and when
2. **Change logs**: Who modified user roles and permissions
3. **Denial logs**: All 403 permission denials with context
4. **Key creation logs**: Track API key lifecycle

**Implementation**: Consider integrating with centralized logging (ELK, Splunk) for audit trail.

---

## Future Enhancements: Permit.io Integration

### Overview

Permit.io is a cloud-based authorization service that provides:
- Dynamic policy management UI
- Fine-grained attribute-based access control (ABAC)
- ReBAC (Relationship-Based Access Control)
- Audit logs and compliance features

### Integration Design

#### Configuration

```yaml
# config.yaml
rbac:
  provider: "static"  # Options: "static", "permit"
  
  static:
    roles_file: "internal/config/rbac/roles.json"
    
  permit:
    enabled: false
    api_key: "${PERMIT_API_KEY}"
    environment: "production"
    pdp_url: "https://cloudpdp.api.permit.io"
```

```bash
# Environment variable
export FLEXPRICE_PERMIT_INTEGRATION=true
export PERMIT_API_KEY="permit_key_xxxxx"
```

### Workflow Changes with Permit

#### Create Service Account with Permit Integration

```
Admin: POST /api/v1/users
Request Body:
{
    "email": "service@example.com",
    "name": "Service Account",
    "type": "service_account",
    "roles": ["event_ingestor"]
}

System Logic:
1. Check if Permit integration is enabled
   
   IF permit.enabled == false:
       - Use static role definitions
       - Validate roles against roles.json
       - Create user in local database
   
   IF permit.enabled == true:
       - Fetch available roles from Permit API
       - Validate roles against Permit roles
       - Create user in local database
       - Sync user to Permit (create user resource)
       - Assign roles in Permit

2. Return user object
```

#### Permission Check with Permit

```go
func (s *RBACService) CheckPermission(roles []string, entity string, action string) bool {
    if s.config.Permit.Enabled {
        // Use Permit for permission check
        return s.permitClient.Check(context.Background(), permit.CheckRequest{
            User: s.currentUser,
            Action: action,
            Resource: entity,
        })
    } else {
        // Use static role definitions
        return s.checkStaticPermissions(roles, entity, action)
    }
}
```

### Hybrid Mode (Recommended Approach)

**Fallback Strategy**: Use static roles as fallback if Permit is unavailable

```go
func (s *RBACService) CheckPermission(roles []string, entity string, action string) bool {
    if s.config.Permit.Enabled {
        // Try Permit first
        allowed, err := s.permitClient.Check(...)
        if err != nil {
            log.Error("Permit check failed, falling back to static: %v", err)
            return s.checkStaticPermissions(roles, entity, action)
        }
        return allowed
    }
    
    // Permit not enabled, use static
    return s.checkStaticPermissions(roles, entity, action)
}
```

### Migration Path to Permit

#### Phase 1: Dual Mode (Static + Permit)
- Run both systems in parallel
- Log differences between static and Permit decisions
- Analyze logs for inconsistencies
- Fix configuration mismatches

#### Phase 2: Permit Primary with Static Fallback
- Use Permit for permission checks
- Fallback to static on errors
- Monitor Permit SLA and latency

#### Phase 3: Permit Only (Optional)
- Remove static role definitions
- All role management through Permit UI
- Deprecate local role files

### Permit Integration Benefits

1. **Dynamic Policy Management**: Update permissions without code deployment
2. **UI for Role Management**: Non-technical users can manage roles
3. **Advanced Features**: Attribute-based policies, relationship-based access
4. **Compliance**: Built-in audit logs and approval workflows
5. **Multi-tenancy**: Easier to implement customer-specific permissions

### Permit Integration Risks

1. **External Dependency**: Service availability depends on Permit uptime
2. **Latency**: Additional network hop for every permission check
3. **Cost**: Permit charges per user/check (evaluate pricing)
4. **Complexity**: Another system to learn and maintain
5. **Data Residency**: User/permission data stored externally

**Recommendation**: Start with static roles, migrate to Permit only when:
- Need for dynamic role management becomes critical
- Support team needs self-service role assignment
- Require advanced features (ABAC, ReBAC)

---

## Implementation Checklist

### Database Changes
- [ ] Create migration for users table (type, roles)
- [ ] Create migration for secrets table (roles, user_type)
- [ ] Run migrations on staging
- [ ] Test backward compatibility
- [ ] Run migrations on production

### Role Definition System
- [ ] Create `internal/config/rbac/` directory
- [ ] Create `roles.json` with simplified format (role -> entity -> actions)
- [ ] Implement RBAC service with set-based lookups
- [ ] Add unit tests for HasPermission() function
- [ ] Test role validation

### Permission Middleware
- [ ] Implement `PermissionMiddleware` struct with `RequirePermission(entity, action)`
- [ ] Add middleware instance to router initialization
- [ ] No endpoint mapping needed - explicit declarations only
- [ ] Test middleware with various permission scenarios

### Router Updates
- [ ] Add explicit `RequirePermission` calls to all protected routes
- [ ] Identify public routes (no auth/permissions needed)
- [ ] Review each endpoint and assign entity/action
- [ ] Document entity and action names used

### API Endpoints
- [ ] Update `POST /api/v1/users` to accept type and roles
- [ ] Update `PATCH /api/v1/users/{id}` to update roles
- [ ] Update `POST /api/v1/secrets` to copy roles from user
- [ ] Create `GET /api/v1/rbac/roles` endpoint
- [ ] Optional: Create `POST /internal/rbac/reload` endpoint

### Testing
- [ ] Unit tests for HasPermission() with set-based lookups
- [ ] Unit tests for role loading and validation
- [ ] Integration tests for protected endpoints
- [ ] Test service account creation
- [ ] Test API key inheritance
- [ ] Test permission denial (403)
- [ ] Test backward compatibility (empty roles = full access)
- [ ] Load test permission middleware (target < 1ms latency)

### Documentation
- [ ] Update API documentation (OpenAPI/Swagger)
- [ ] Developer guide: "How to add a new protected route"
- [ ] Developer guide: "How to add a new role"
- [ ] Runbook for permission issues
- [ ] Security documentation

### Monitoring & Observability
- [ ] Add metrics: permission_check_duration_ms
- [ ] Add metrics: permission_denied_total (by entity, action, role)
- [ ] Add logs for permission denials
- [ ] Add alerts for unusual 403 rates
- [ ] Dashboard for RBAC metrics

### Deployment
- [ ] Feature flag for RBAC system (optional)
- [ ] Gradual rollout plan
- [ ] Rollback procedures documented
- [ ] Smoke tests for production
- [ ] Communication plan for users

**Estimated Implementation Time**: 
- Database migrations: 1 day
- RBAC service + middleware: 2 days  
- Router updates: 3-5 days (depends on number of routes)
- Testing: 2-3 days
- Total: ~1.5-2 weeks

---

## Open Questions

### Technical Decisions

1. **Role Hierarchy**: Should we support role inheritance (e.g., "admin" includes "metrics_reader")?
   - **Status**: Deferred to Phase 2
   - **Recommendation**: Start simple without hierarchy

2. **Wildcard Permissions**: Should we support wildcards (e.g., `entity: "*"` for admin)?
   - **Status**: Under consideration
   - **Proposal**: Add "admin" role with all permissions

3. **Per-Organization Roles**: Can roles differ per organization/tenant?
   - **Status**: Deferred to multi-tenancy implementation
   - **Current**: Global roles across all organizations

4. **API Key Expiration**: Should service account keys auto-expire?
   - **Status**: Deferred to Phase 2
   - **Recommendation**: Add expiration_date field later

### Process & Workflow

5. **Service Account Key Rotation**: What's the recommended rotation policy?
   - **Proposal**: 90 days, with warnings at 60 days
   - **Need**: Automated rotation mechanism?

6. **Permission Denial Communication**: How do we inform users about missing permissions?
   - **Current**: 403 error with message
   - **Future**: Suggest required role in error message

7. **Admin Management**: Who can create/modify service accounts?
   - **Proposal**: Organization admins + Flexprice superadmins
   - **Need**: Define admin role/permissions

### Permit Integration

8. **Permit Timing**: When should we integrate Permit?
   - **Recommendation**: After 3-6 months of static RBAC
   - **Trigger**: When manual role management becomes bottleneck

9. **Permit Features**: Which Permit features do we need?
   - **Phase 1**: Basic RBAC
   - **Phase 2**: ABAC (attribute-based)
   - **Phase 3**: ReBAC (relationship-based)

10. **Permit Cost**: What's the expected cost impact?
    - **Need**: Estimate based on user count and API call volume
    - **Action**: Get quote from Permit sales

---

## Success Criteria

### Phase 1 Launch (Static RBAC)

#### Functionality
- ✅ Service accounts can be created with roles
- ✅ API keys inherit permissions from users
- ✅ Permission middleware enforces access control
- ✅ Backward compatibility maintained (existing users unaffected)
- ✅ At least 3 roles defined and working

#### Performance
- ✅ Permission check latency < 5ms (p99)
- ✅ No increase in overall API latency > 10ms
- ✅ System handles 10,000 permission checks/second

#### Security
- ✅ No bypasses found in security audit
- ✅ Service accounts properly restricted
- ✅ All endpoints have permission checks

#### Quality
- ✅ Test coverage > 85% for RBAC code
- ✅ Zero critical bugs in first 2 weeks
- ✅ Documentation complete and accurate

### Phase 2 (Permit Integration - Optional)

- ✅ Permit integration working with fallback to static
- ✅ All roles migrated to Permit
- ✅ Permission check latency still < 10ms (p99)
- ✅ 99.9% uptime with Permit dependency

---

## Appendix

### Example Role Definitions

#### Comprehensive roles.json

```json
{
  "event_ingestor": {
    "name": "Event Ingestor",
    "description": "Limited to ingesting events and batch events. Use for services that only send events.",
    "permissions": {
      "event": ["create", "write"],
      "batch_event": ["create"]
    }
  },
  "metrics_reader": {
    "name": "Metrics Reader",
    "description": "Read-only access to metrics, analytics, and dashboards for reporting.",
    "permissions": {
      "metrics": ["read", "list"],
      "analytics": ["read"],
      "dashboard": ["read"]
    }
  },
  "feature_manager": {
    "name": "Feature Manager",
    "description": "Full access to feature flags and configurations.",
    "permissions": {
      "feature": ["create", "read", "update", "delete", "list"],
      "feature_flag": ["create", "read", "update", "toggle", "delete"]
    }
  },
  "billing_reader": {
    "name": "Billing Reader",
    "description": "Read-only access to invoices, subscriptions, and payment information.",
    "permissions": {
      "invoice": ["read", "list"],
      "subscription": ["read", "list"],
      "payment": ["read", "list"]
    }
  },
  "billing_admin": {
    "name": "Billing Administrator",
    "description": "Manage invoices, subscriptions, and payment operations.",
    "permissions": {
      "invoice": ["create", "read", "update", "delete", "list"],
      "subscription": ["create", "read", "update", "delete", "list"],
      "payment": ["create", "read", "list"]
    }
  },
  "pricing_admin": {
    "name": "Pricing Administrator",
    "description": "Manage pricing models, plans, and pricing calculations.",
    "permissions": {
      "pricing": ["create", "read", "update", "delete", "execute"],
      "pricing_model": ["create", "read", "update", "delete"],
      "pricing_calculation": ["execute", "read"],
      "plan": ["create", "read", "update", "delete", "list"]
    }
  },
  "customer_support": {
    "name": "Customer Support",
    "description": "Read customer data with limited modification rights. Can update customer info and create support tickets.",
    "permissions": {
      "customer": ["read", "list", "update"],
      "subscription": ["read", "list", "update"],
      "invoice": ["read", "list"],
      "support_ticket": ["create", "read", "update", "list"]
    }
  },
  "customer_manager": {
    "name": "Customer Manager",
    "description": "Full access to customer data and read-only access to subscriptions and invoices.",
    "permissions": {
      "customer": ["create", "read", "update", "delete", "list"],
      "subscription": ["read", "list"],
      "invoice": ["read", "list"]
    }
  },
  "api_key_manager": {
    "name": "API Key Manager",
    "description": "Manage API keys and secrets for service accounts.",
    "permissions": {
      "api_key": ["create", "read", "delete", "list"],
      "secret": ["create", "read", "delete", "list"]
    }
  },
  "admin": {
    "name": "Administrator",
    "description": "Full access to all resources. Use sparingly and only for trusted administrators.",
    "permissions": {
      "customer": ["create", "read", "update", "delete", "list"],
      "subscription": ["create", "read", "update", "delete", "list"],
      "invoice": ["create", "read", "update", "delete", "list"],
      "event": ["create", "read", "write", "delete", "list"],
      "metrics": ["read", "list"],
      "feature": ["create", "read", "update", "delete", "list"],
      "pricing": ["create", "read", "update", "delete", "execute"],
      "api_key": ["create", "read", "delete", "list"],
      "wallet": ["create", "read", "update", "delete", "list"]
    }
  }
}
```

**Notes**:
- **Name**: User-friendly name displayed in UI dropdowns
- **Description**: Helpful text shown when selecting roles in the frontend
- **Permissions**: Entity-action mappings converted to set-based structure at startup
- **Performance**: Name/description are never used in permission checks (hot path)
- **UI/UX**: Frontend fetches this via `GET /api/v1/rbac/roles` to populate role selection

### Code Examples

#### Complete RBAC Service

```go
// internal/rbac/service.go
package rbac

import (
    "encoding/json"
    "fmt"
    "os"
)

type Service struct {
    // Fast lookup for permission checks (hot path - never touches metadata)
    permissions map[string]map[string]map[string]bool
    
    // Full role definitions with metadata (for API responses)
    roles map[string]*Role
}

type Role struct {
    ID          string              `json:"id"`
    Name        string              `json:"name"`
    Description string              `json:"description"`
    Permissions map[string][]string `json:"permissions"`
}

func NewService(configPath string) (*Service, error) {
    data, err := os.ReadFile(configPath)
    if err != nil {
        return nil, fmt.Errorf("failed to read config: %w", err)
    }
    
    var rawConfig map[string]*Role
    if err := json.Unmarshal(data, &rawConfig); err != nil {
        return nil, fmt.Errorf("failed to parse config: %w", err)
    }
    
    permissions := make(map[string]map[string]map[string]bool)
    
    for roleID, role := range rawConfig {
        role.ID = roleID
        permissions[roleID] = make(map[string]map[string]bool)
        
        for entity, actions := range role.Permissions {
            permissions[roleID][entity] = make(map[string]bool)
            for _, action := range actions {
                permissions[roleID][entity][action] = true
            }
        }
    }
    
    return &Service{
        permissions: permissions,
        roles:       rawConfig,
    }, nil
}

func (s *Service) HasPermission(roles []string, entity string, action string) bool {
    if len(roles) == 0 {
        return true // Backward compatibility
    }
    
    for _, role := range roles {
        if s.permissions[role] != nil && 
           s.permissions[role][entity] != nil && 
           s.permissions[role][entity][action] {
            return true
        }
    }
    
    return false
}

func (s *Service) ValidateRole(roleName string) bool {
    _, exists := s.permissions[roleName]
    return exists
}

func (s *Service) GetAllRoles() []*Role {
    result := make([]*Role, 0, len(s.roles))
    for _, role := range s.roles {
        result = append(result, role)
    }
    return result
}

func (s *Service) GetRole(roleID string) (*Role, bool) {
    role, exists := s.roles[roleID]
    return role, exists
}
```

#### Complete Permission Middleware

```go
// internal/middleware/permission.go
package middleware

import (
    "fmt"
    "net/http"
    
    "github.com/gin-gonic/gin"
    "github.com/flexprice/flexprice/internal/rbac"
    "github.com/flexprice/flexprice/internal/logger"
)

type PermissionMiddleware struct {
    rbacService *rbac.Service
    logger      *logger.Logger
}

func NewPermissionMiddleware(rbacService *rbac.Service, logger *logger.Logger) *PermissionMiddleware {
    return &PermissionMiddleware{
        rbacService: rbacService,
        logger:      logger,
    }
}

func (pm *PermissionMiddleware) RequirePermission(entity string, action string) gin.HandlerFunc {
    return func(c *gin.Context) {
        secretInterface, exists := c.Get("secret")
        if !exists {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
                "error": "Unauthorized",
            })
            return
        }
        
        secret, ok := secretInterface.(*models.Secret)
        if !ok {
            c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
                "error": "Internal server error",
            })
            return
        }
        
        if !pm.rbacService.HasPermission(secret.Roles, entity, action) {
            pm.logger.Info("Permission denied",
                "user_id", secret.UserID,
                "roles", secret.Roles,
                "entity", entity,
                "action", action,
                "path", c.Request.URL.Path,
            )
            
            c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
                "error": "Forbidden",
                "message": fmt.Sprintf("Insufficient permissions to %s %s", action, entity),
            })
            return
        }
        
        c.Next()
    }
}
```

#### Frontend UI Example

**Use Case**: Create Service Account form with role selection dropdown

```typescript
// Frontend: Create Service Account Component
import { useQuery } from '@tanstack/react-query';
import { Select, SelectItem } from '@/components/ui/select';

function CreateServiceAccountForm() {
  // Fetch available roles from backend
  const { data: rolesData } = useQuery({
    queryKey: ['rbac-roles'],
    queryFn: () => fetch('/api/v1/rbac/roles').then(res => res.json())
  });

  return (
    <form>
      <Input label="Name" name="name" />
      <Input label="Email" name="email" />
      
      <Select label="Account Type" name="type">
        <SelectItem value="user">User</SelectItem>
        <SelectItem value="service_account">Service Account</SelectItem>
      </Select>
      
      {/* Dynamic role dropdown - fetched from backend */}
      <Select label="Role" name="roles">
        {rolesData?.roles.map(role => (
          <SelectItem key={role.id} value={role.id}>
            <div className="flex flex-col">
              <span className="font-medium">{role.name}</span>
              <span className="text-sm text-gray-500">{role.description}</span>
            </div>
          </SelectItem>
        ))}
      </Select>
      
      <Button type="submit">Create Service Account</Button>
    </form>
  );
}
```

**Result**: When adding a new role to `roles.json`, frontend automatically shows it in the dropdown - no frontend code changes needed!

---

## Glossary

| Term | Definition |
|------|------------|
| **RBAC** | Role-Based Access Control - permission model based on user roles |
| **Service Account** | Non-human user account used by services/applications |
| **Role** | Named collection of permissions |
| **Permission** | Ability to perform an action on an entity |
| **Entity** | Resource in the system (e.g., event, feature, customer) |
| **Action** | Operation on an entity (e.g., create, read, update, delete) |
| **API Key** | Authentication token used to access Flexprice APIs |
| **Static Role** | Role defined in configuration file (not in database) |
| **Dynamic Role** | Role defined through UI or API (stored in database/Permit) |
| **Fail-Open** | Default to allow access on error |
| **Fail-Closed** | Default to deny access on error |
| **Denormalization** | Copying data to avoid JOIN queries (performance optimization) |

---

## Document Change Log

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2025-10-31 | Engineering Team | Initial draft based on design specifications |
| 2.0 | 2025-11-01 | Engineering Team | Major update: Simplified to explicit permission declarations with set-based lookups. Removed automatic endpoint mapping system. |
| 2.1 | 2025-11-01 | Engineering Team | Added name and description fields to role definitions for UI/UX. Updated RBAC service to store metadata separately. Clarified zero performance impact on permission checks. |

---

## Feedback and Iterations

This is a living document. Please provide feedback on:
- Missing requirements or edge cases
- Security concerns
- Implementation complexity
- Timeline estimates
- Integration challenges

**Next Review Date**: 2025-11-07

---

## References

- [NIST RBAC Model](https://csrc.nist.gov/projects/role-based-access-control)
- [OWASP Access Control Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Access_Control_Cheat_Sheet.html)
- [Permit.io Documentation](https://docs.permit.io/)
- [The Principle of Least Privilege](https://en.wikipedia.org/wiki/Principle_of_least_privilege)

---

## Visual Diagrams & Flowcharts

This section provides visual representations of the RBAC system architecture, workflows, and data models to aid understanding and implementation.

### Diagram 1: System Architecture Overview

**Description**: This diagram shows the high-level architecture of the Flexprice RBAC system, including how API requests flow through authentication, permission checks, and into business logic. It illustrates the relationship between users, API keys, role definitions, and the permission enforcement layer.

```mermaid
graph TB
    subgraph "External"
        Client[Client Application]
        ServiceAccount[Service Account]
    end
    
    subgraph "API Gateway"
        APIRequest[API Request + API Key]
    end
    
    subgraph "Middleware Layer"
        AuthMiddleware[Authentication Middleware]
        PermissionMiddleware[Permission Middleware]
    end
    
    subgraph "Data Layer"
        SecretsTable[(Secrets Table<br/>API Keys + Roles)]
        UsersTable[(Users Table<br/>Type + Roles)]
        RoleDefinitions[Role Definitions JSON<br/>roles.json]
    end
    
    subgraph "Business Logic"
        RouteHandler[Route Handler]
        BusinessLogic[Business Logic]
    end
    
    Client -->|HTTP Request| APIRequest
    ServiceAccount -->|HTTP Request| APIRequest
    APIRequest --> AuthMiddleware
    AuthMiddleware -->|Validate API Key| SecretsTable
    SecretsTable -->|API Key Valid + Roles| AuthMiddleware
    AuthMiddleware --> PermissionMiddleware
    PermissionMiddleware -->|Load Roles| SecretsTable
    PermissionMiddleware -->|Load Role Permissions| RoleDefinitions
    PermissionMiddleware -->|Check Permission| PermissionMiddleware
    PermissionMiddleware -->|403 Forbidden| APIRequest
    PermissionMiddleware -->|Allowed| RouteHandler
    RouteHandler --> BusinessLogic
    BusinessLogic -->|Response| APIRequest
    
    UsersTable -.->|Roles Copied at Creation| SecretsTable
    
    style PermissionMiddleware fill:#f9f,stroke:#333,stroke-width:4px
    style RoleDefinitions fill:#bbf,stroke:#333,stroke-width:2px
    style SecretsTable fill:#bfb,stroke:#333,stroke-width:2px
```

---

### Diagram 2: Data Model & Relationships

**Description**: Entity-Relationship diagram showing the database schema changes for RBAC, including the new fields in Users and Secrets tables, and their relationships. This illustrates how roles are denormalized from Users to Secrets for performance.

```mermaid
erDiagram
    USERS ||--o{ SECRETS : "has many"
    USERS {
        uuid id PK
        string email
        string name
        string type "user|service_account"
        jsonb roles "array of role names"
        timestamp created_at
        timestamp updated_at
    }
    
    SECRETS {
        uuid id PK
        uuid user_id FK
        string api_key "encrypted"
        string name
        jsonb roles "copied from users"
        string user_type "copied from users"
        timestamp last_used
        timestamp created_at
    }
    
    ROLE_DEFINITIONS {
        string role_name PK
        string display_name
        string description
        json permissions "entity+actions"
    }
    
    USERS ||--o{ ROLE_DEFINITIONS : "references"
    SECRETS ||--o{ ROLE_DEFINITIONS : "references"
```

---

### Diagram 3: User Creation Workflow

**Description**: Complete workflow for creating users, showing the decision tree based on user type and the validation rules applied. This covers both regular users and service accounts with their different role requirements.

```mermaid
flowchart TD
    Start([POST /api/v1/users]) --> CheckType{Type Provided?}
    
    CheckType -->|No| SetDefaultUser[Set type = 'user']
    CheckType -->|Yes| ValidateType{Type Valid?}
    
    ValidateType -->|Invalid| Error400[Return 400<br/>Invalid Type]
    ValidateType -->|Valid| CheckUserType{Type = user OR<br/>service_account?}
    
    SetDefaultUser --> CheckRoles{Roles Provided?}
    
    CheckUserType -->|type = user| CheckRoles
    CheckUserType -->|type = service_account| ValidateRolesRequired{Roles Array<br/>Not Empty?}
    
    CheckRoles -->|No| SetEmptyRoles[Set roles = empty array]
    CheckRoles -->|Yes| ValidateRoleNames[Validate Role Names<br/>Against Definitions]
    
    ValidateRolesRequired -->|Empty| Error422[Return 422<br/>Service account<br/>requires roles]
    ValidateRolesRequired -->|Not Empty| ValidateRoleNames
    
    ValidateRoleNames -->|Invalid Role| Error400b[Return 400<br/>Unknown Role]
    ValidateRoleNames -->|Valid| CreateUser[Create User in DB]
    
    SetEmptyRoles --> CreateUser
    
    CreateUser --> Return201[Return 201 Created<br/>With User Object]
    
    Return201 --> End([End])
    Error400 --> End
    Error400b --> End
    Error422 --> End
    
    style Start fill:#90EE90
    style End fill:#FFB6C1
    style Error400 fill:#FF6B6B
    style Error400b fill:#FF6B6B
    style Error422 fill:#FF6B6B
    style ValidateRolesRequired fill:#FFD700
    style CreateUser fill:#87CEEB
```

---

### Diagram 4: API Key Creation Workflow

**Description**: Shows how API keys are created with permission inheritance from users. This diagram illustrates the different paths when creating keys for the current user vs. creating keys for service accounts, and how roles are copied to the Secrets table.

```mermaid
flowchart TD
    Start([POST /api/v1/secrets]) --> CheckUserID{user_id<br/>Parameter<br/>Provided?}
    
    CheckUserID -->|No| GetContextUser[Get user_id from<br/>JWT/Session Context]
    CheckUserID -->|Yes| CheckAdmin{Current User<br/>is Admin?}
    
    CheckAdmin -->|No| Error403[Return 403<br/>Forbidden]
    CheckAdmin -->|Yes| GetParamUser[Get user_id from<br/>Request Parameter]
    
    GetContextUser --> FetchUser1[Fetch User from DB]
    GetParamUser --> FetchUser2[Fetch User from DB]
    
    FetchUser1 --> UserExists1{User Exists?}
    FetchUser2 --> UserExists2{User Exists?}
    
    UserExists1 -->|No| Error404a[Return 404<br/>User Not Found]
    UserExists2 -->|No| Error404b[Return 404<br/>User Not Found]
    
    UserExists1 -->|Yes| CopyRoles1[Copy user.roles<br/>to secret.roles]
    UserExists2 -->|Yes| ValidateSA{user.type =<br/>service_account?}
    
    ValidateSA -->|No| Error400[Return 400<br/>Can only create keys<br/>for service accounts]
    ValidateSA -->|Yes| CopyRoles2[Copy user.roles<br/>to secret.roles]
    
    CopyRoles1 --> CopyType1[Copy user.type<br/>to secret.user_type]
    CopyRoles2 --> CopyType2[Copy user.type<br/>to secret.user_type]
    
    CopyType1 --> GenerateKey[Generate API Key]
    CopyType2 --> GenerateKey
    
    GenerateKey --> SaveSecret[Save to Secrets Table]
    SaveSecret --> Return201[Return 201 Created<br/>with API Key]
    
    Return201 --> End([End])
    Error403 --> End
    Error404a --> End
    Error404b --> End
    Error400 --> End
    
    style Start fill:#90EE90
    style End fill:#FFB6C1
    style Error403 fill:#FF6B6B
    style Error404a fill:#FF6B6B
    style Error404b fill:#FF6B6B
    style Error400 fill:#FF6B6B
    style GenerateKey fill:#87CEEB
    style CopyRoles1 fill:#FFD700
    style CopyRoles2 fill:#FFD700
```

---

### Diagram 5: Permission Check Flow (Runtime)

**Description**: Detailed flow of how permission checks are performed during API request processing. This is the core RBAC enforcement logic that runs on every authenticated API request, showing the decision tree for allowing or denying access.

```mermaid
flowchart TD
    Start([Incoming API Request]) --> AuthMW[Authentication Middleware]
    
    AuthMW --> ValidateKey{API Key Valid?}
    ValidateKey -->|No| Return401[Return 401<br/>Unauthorized]
    ValidateKey -->|Yes| LoadSecret[Load Secret + Roles<br/>from Database]
    
    LoadSecret --> PermMW[Permission Middleware]
    PermMW --> CheckExempt{Endpoint<br/>Exempt?}
    
    CheckExempt -->|Yes /health, /metrics| AllowRequest[Allow Request]
    CheckExempt -->|No| CheckRoles{Roles Array<br/>Empty?}
    
    CheckRoles -->|Yes Empty| AllowRequest
    CheckRoles -->|No Not Empty| MapEndpoint[Map HTTP Method + Path<br/>to Entity + Action]
    
    MapEndpoint --> MappingExists{Mapping<br/>Exists?}
    MappingExists -->|No| Return403a[Return 403<br/>Unmapped Endpoint]
    MappingExists -->|Yes| LoadRoleDefs[Load Role Definitions<br/>from JSON Cache]
    
    LoadRoleDefs --> CheckPermLoop{For Each Role<br/>in user.roles}
    
    CheckPermLoop --> LoadRole[Load Role Definition]
    LoadRole --> RoleExists{Role Exists<br/>in Definitions?}
    
    RoleExists -->|No| NextRole[Try Next Role]
    RoleExists -->|Yes| CheckPerms[Check Role Permissions<br/>for Entity + Action]
    
    CheckPerms --> HasPermission{Permission<br/>Granted?}
    HasPermission -->|Yes| LogSuccess[Log Permission Grant]
    HasPermission -->|No| NextRole
    
    NextRole --> MoreRoles{More Roles<br/>to Check?}
    MoreRoles -->|Yes| CheckPermLoop
    MoreRoles -->|No| LogDenial[Log Permission Denial]
    
    LogDenial --> Return403b[Return 403<br/>Forbidden]
    LogSuccess --> AllowRequest
    
    AllowRequest --> RouteHandler[Route Handler]
    RouteHandler --> BusinessLogic[Execute Business Logic]
    BusinessLogic --> Return200[Return 200 OK<br/>with Response]
    
    Return200 --> End([End])
    Return401 --> End
    Return403a --> End
    Return403b --> End
    
    style Start fill:#90EE90
    style End fill:#FFB6C1
    style Return401 fill:#FF6B6B
    style Return403a fill:#FF6B6B
    style Return403b fill:#FF6B6B
    style AllowRequest fill:#87CEEB
    style CheckRoles fill:#FFD700
    style HasPermission fill:#FFD700
```

---

### Diagram 6: Role Permission Evaluation

**Description**: This diagram shows how the system evaluates whether a set of roles has permission for a specific entity and action. It illustrates the logic of checking multiple roles and the "any role grants access" principle.

```mermaid
flowchart LR
    subgraph Input
        Roles[User Roles:<br/>event_ingestor,<br/>metrics_reader]
        Entity[Entity: event]
        Action[Action: write]
    end
    
    subgraph "Role Definitions Cache"
        RoleDef1[event_ingestor:<br/>- event: create, write<br/>- batch_event: create]
        RoleDef2[metrics_reader:<br/>- metrics: read, list<br/>- analytics: read]
    end
    
    subgraph "Permission Check Logic"
        CheckRole1{Check event_ingestor<br/>for event.write}
        CheckRole2{Check metrics_reader<br/>for event.write}
    end
    
    subgraph Output
        Result{Any Role<br/>Grants Access?}
        Allow[✅ ALLOW<br/>Continue to Handler]
        Deny[❌ DENY<br/>Return 403]
    end
    
    Roles --> CheckRole1
    Roles --> CheckRole2
    Entity --> CheckRole1
    Entity --> CheckRole2
    Action --> CheckRole1
    Action --> CheckRole2
    
    RoleDef1 --> CheckRole1
    RoleDef2 --> CheckRole2
    
    CheckRole1 -->|Match Found| Result
    CheckRole2 -->|No Match| Result
    
    Result -->|Yes| Allow
    Result -->|No| Deny
    
    style CheckRole1 fill:#90EE90
    style CheckRole2 fill:#FFB6C1
    style Allow fill:#90EE90
    style Deny fill:#FF6B6B
```

---

### Diagram 7: Permit.io Integration Architecture

**Description**: Shows how the Flexprice RBAC system integrates with Permit.io for enhanced authorization capabilities. This diagram illustrates the hybrid approach with fallback to static roles when Permit is unavailable.

```mermaid
graph TB
    subgraph "Flexprice Application"
        PermMW[Permission Middleware]
        RBACService[RBAC Service]
        StaticRoles[Static Role Definitions<br/>roles.json]
    end
    
    subgraph "Configuration"
        Config[Config:<br/>permit.enabled = true/false]
        EnvVar[ENV:<br/>FLEXPRICE_PERMIT_INTEGRATION]
    end
    
    subgraph "External Services"
        PermitPDP[Permit.io PDP<br/>Policy Decision Point]
        PermitAPI[Permit.io API<br/>Management]
    end
    
    subgraph "Database"
        UsersDB[(Users Table)]
        SecretsDB[(Secrets Table)]
    end
    
    PermMW --> RBACService
    Config --> RBACService
    EnvVar --> Config
    
    RBACService --> CheckEnabled{Permit<br/>Enabled?}
    
    CheckEnabled -->|No| StaticRoles
    CheckEnabled -->|Yes| CallPermit[Call Permit PDP<br/>Check Permission]
    
    CallPermit --> PermitSuccess{Permit<br/>Available?}
    
    PermitSuccess -->|Yes| PermitPDP
    PermitPDP --> ReturnPermit[Return Decision<br/>from Permit]
    
    PermitSuccess -->|No/Error| Fallback[Fallback to<br/>Static Roles]
    Fallback --> StaticRoles
    
    StaticRoles --> ReturnStatic[Return Decision<br/>from Static]
    
    ReturnPermit --> Decision{Allow or<br/>Deny?}
    ReturnStatic --> Decision
    
    Decision -->|Allow| Continue[Continue Request]
    Decision -->|Deny| Return403[Return 403]
    
    UsersDB -.->|Sync on Create/Update| PermitAPI
    PermitAPI -.->|User/Role Sync| PermitPDP
    
    style CheckEnabled fill:#FFD700
    style PermitPDP fill:#87CEEB
    style StaticRoles fill:#90EE90
    style Fallback fill:#FFA500
    style Return403 fill:#FF6B6B
    style Continue fill:#90EE90
```

---

### Diagram 8: User & Role Sync with Permit.io

**Description**: Workflow showing how users and their roles are synchronized with Permit.io when the integration is enabled. This ensures Permit has the latest user and role information for authorization decisions.

```mermaid
sequenceDiagram
    participant Admin
    participant API as Flexprice API
    participant DB as Database
    participant Config as Config Manager
    participant Permit as Permit.io API
    
    Admin->>API: POST /api/v1/users<br/>{type: service_account, roles: [event_ingestor]}
    
    API->>Config: Check permit.enabled?
    Config-->>API: enabled = true
    
    API->>Permit: GET /roles<br/>Fetch available roles
    Permit-->>API: [event_ingestor, metrics_reader, ...]
    
    API->>API: Validate roles exist in Permit
    
    API->>DB: INSERT INTO users<br/>(type, roles)
    DB-->>API: User created
    
    API->>Permit: POST /users<br/>Create user in Permit
    Permit-->>API: User created in Permit
    
    API->>Permit: POST /role-assignments<br/>Assign roles to user
    Permit-->>API: Roles assigned
    
    API-->>Admin: 201 Created<br/>User object
    
    Note over API,Permit: If Permit call fails,<br/>log error but continue<br/>(graceful degradation)
```

---

### Diagram 9: Migration Phases Timeline

**Description**: Visual timeline showing the three phases of RBAC implementation and deployment, from database migrations through feature flag rollout to full production release.

```mermaid
gantt
    title RBAC Implementation Timeline
    dateFormat YYYY-MM-DD
    section Phase 1: Database
    Schema Design & Review           :p1_1, 2025-11-01, 3d
    Create Migrations               :p1_2, after p1_1, 2d
    Deploy to Staging               :p1_3, after p1_2, 1d
    Verify & Test                   :p1_4, after p1_3, 2d
    Deploy to Production            :p1_5, after p1_4, 1d
    Backfill Data                   :p1_6, after p1_5, 1d
    
    section Phase 2: Code Deployment
    Implement RBAC Service          :p2_1, 2025-11-01, 5d
    Implement Middleware            :p2_2, after p2_1, 3d
    Unit Tests                      :p2_3, after p2_1, 5d
    Integration Tests               :p2_4, after p2_2, 3d
    Code Review                     :p2_5, after p2_4, 2d
    Deploy with Flag OFF            :p2_6, after p2_5, 1d
    Monitor for 24h                 :p2_7, after p2_6, 1d
    
    section Phase 3: Feature Rollout
    Enable Internal (1%)            :p3_1, after p2_7, 1d
    Monitor & Fix Issues            :p3_2, after p3_1, 2d
    Enable Staging (100%)           :p3_3, after p3_2, 1d
    Load Testing                    :p3_4, after p3_3, 2d
    Production 10%                  :p3_5, after p3_4, 1d
    Production 50%                  :p3_6, after p3_5, 2d
    Production 100%                 :p3_7, after p3_6, 2d
    Post-Launch Monitor             :p3_8, after p3_7, 7d
```

---

### Diagram 10: Permission Denial Scenarios

**Description**: Decision tree showing all possible scenarios that lead to permission denial (403 Forbidden), helping developers understand when and why access is blocked.

```mermaid
flowchart TD
    Request[API Request] --> Auth{Authenticated?}
    
    Auth -->|No| E401[❌ 401 Unauthorized<br/>Invalid/Missing API Key]
    Auth -->|Yes| HasRoles{User has<br/>roles assigned?}
    
    HasRoles -->|No Empty Array| Allow[✅ 200 OK<br/>Full Access<br/>Backward Compatibility]
    HasRoles -->|Yes Not Empty| Mapped{Endpoint<br/>Mapped?}
    
    Mapped -->|No| E403_Unmapped[❌ 403 Forbidden<br/>Endpoint not in mapping<br/>Access denied by default]
    Mapped -->|Yes| HasMatchingRole{Any role has<br/>permission?}
    
    HasMatchingRole -->|No| E403_NoPermission[❌ 403 Forbidden<br/>Insufficient permissions<br/>for entity.action]
    HasMatchingRole -->|Yes| RateLimited{Rate Limit<br/>Exceeded?}
    
    RateLimited -->|Yes| E429[❌ 429 Too Many Requests<br/>Rate limit exceeded]
    RateLimited -->|No| Allow2[✅ 200 OK<br/>Request Allowed]
    
    Allow --> Success[Process Request]
    Allow2 --> Success
    Success --> Response[Return Response]
    
    style E401 fill:#FF6B6B
    style E403_Unmapped fill:#FF6B6B
    style E403_NoPermission fill:#FF6B6B
    style E429 fill:#FFA500
    style Allow fill:#90EE90
    style Allow2 fill:#90EE90
    style Success fill:#87CEEB
    style HasRoles fill:#FFD700
    style HasMatchingRole fill:#FFD700
```

---

### Diagram 11: Role Definition Structure

**Description**: Visual representation of how roles are structured in the JSON configuration file, showing the hierarchy from role to permissions to entities and actions.

```mermaid
graph TD
    subgraph "roles.json File"
        Root[Role Definitions]
    end
    
    Root --> Role1[event_ingestor]
    Root --> Role2[metrics_reader]
    Root --> Role3[feature_manager]
    
    Role1 --> R1Meta[Metadata:<br/>name, description]
    Role1 --> R1Perms[Permissions Array]
    
    R1Perms --> R1P1[Permission 1]
    R1Perms --> R1P2[Permission 2]
    
    R1P1 --> R1P1E[Entity: event]
    R1P1 --> R1P1A[Actions: create, write]
    
    R1P2 --> R1P2E[Entity: batch_event]
    R1P2 --> R1P2A[Actions: create]
    
    Role2 --> R2Meta[Metadata:<br/>name, description]
    Role2 --> R2Perms[Permissions Array]
    
    R2Perms --> R2P1[Permission 1]
    R2P1 --> R2P1E[Entity: metrics]
    R2P1 --> R2P1A[Actions: read, list]
    
    Role3 --> R3Meta[Metadata:<br/>name, description]
    Role3 --> R3Perms[Permissions Array]
    
    R3Perms --> R3P1[Permission 1]
    R3P1 --> R3P1E[Entity: feature]
    R3P1 --> R3P1A[Actions: create, read,<br/>update, delete, list]
    
    style Root fill:#87CEEB
    style Role1 fill:#90EE90
    style Role2 fill:#FFD700
    style Role3 fill:#FFA07A
    style R1P1E fill:#E6E6FA
    style R2P1E fill:#E6E6FA
    style R3P1E fill:#E6E6FA
```

---

### Diagram 12: Complete System State Diagram

**Description**: State machine showing the lifecycle of a service account from creation through API key generation to request processing and eventual deactivation. This provides a holistic view of how service accounts move through the system.

```mermaid
stateDiagram-v2
    [*] --> UserCreated: Admin creates<br/>service account<br/>with roles
    
    UserCreated --> APIKeyCreated: Admin generates<br/>API key<br/>(roles copied)
    
    APIKeyCreated --> Active: Key ready<br/>for use
    
    Active --> RequestProcessing: API request<br/>received
    
    RequestProcessing --> PermissionCheck: Check roles<br/>and permissions
    
    PermissionCheck --> Allowed: Permission<br/>granted
    PermissionCheck --> Denied: Permission<br/>denied
    
    Allowed --> Active: Continue<br/>processing
    Denied --> Active: Return 403
    
    Active --> RolesUpdated: Admin updates<br/>user roles
    
    RolesUpdated --> APIKeyCreated: Regenerate<br/>API key<br/>(new roles)
    
    Active --> KeyRevoked: Admin deletes<br/>API key
    
    KeyRevoked --> APIKeyCreated: Create new<br/>API key
    
    Active --> Deactivated: Admin deletes<br/>service account
    
    Deactivated --> [*]: All keys<br/>invalidated
    
    note right of UserCreated
        Service account must
        have at least one role
    end note
    
    note right of APIKeyCreated
        Roles are immutable
        in API key
    end note
    
    note right of PermissionCheck
        Check against
        role definitions
    end note
```

---

### Diagram 13: Backward Compatibility Flow

**Description**: Demonstrates how the RBAC system maintains backward compatibility with existing users who don't have roles assigned, ensuring zero breaking changes during rollout.

```mermaid
flowchart LR
    subgraph "Existing Users (Pre-RBAC)"
        OldUser1[Regular User<br/>roles = NULL/empty<br/>type = NULL/user]
        OldAPIKey1[Old API Keys<br/>roles = NULL/empty]
    end
    
    subgraph "Migration"
        Migration[Schema Migration<br/>+ Backfill]
    end
    
    subgraph "Post-Migration State"
        NewUser1[Regular User<br/>roles = empty array<br/>type = 'user']
        NewAPIKey1[Old API Keys<br/>roles = empty array<br/>type = 'user']
    end
    
    subgraph "Runtime Behavior"
        CheckRoles{Roles<br/>Empty?}
        FullAccess[✅ Full Access<br/>All Endpoints<br/>No Restrictions]
        NoBreaking[❌ No Breaking Changes<br/>Existing workflows work]
    end
    
    OldUser1 --> Migration
    OldAPIKey1 --> Migration
    
    Migration --> NewUser1
    Migration --> NewAPIKey1
    
    NewUser1 --> CheckRoles
    NewAPIKey1 --> CheckRoles
    
    CheckRoles -->|Yes| FullAccess
    FullAccess --> NoBreaking
    
    style OldUser1 fill:#FFE4B5
    style OldAPIKey1 fill:#FFE4B5
    style Migration fill:#87CEEB
    style FullAccess fill:#90EE90
    style NoBreaking fill:#90EE90
```

---

*End of Document*

