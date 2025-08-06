# RBAC Implementation with Casbin and Ent Adapter

## Overview

This document describes the Role-Based Access Control (RBAC) implementation for the Flexprice multi-tenant SaaS platform using Casbin with a custom Ent adapter for PostgreSQL storage.

## Architecture

### Components

1. **Casbin Enforcer**: Core authorization engine
2. **Ent Adapter**: Custom database adapter for PostgreSQL storage
3. **RBAC Service**: Business logic layer
4. **RBAC Repository**: Data access layer using Ent ORM
5. **API Handlers**: HTTP endpoints for role management
6. **Middleware**: Authentication and authorization middleware

### Database Schema

The RBAC system uses three main tables:

#### `user_roles` Table
```sql
CREATE TABLE user_roles (
    id VARCHAR(50) PRIMARY KEY,
    user_id VARCHAR(50) NOT NULL,
    role VARCHAR(50) NOT NULL,
    tenant_id VARCHAR(50) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(20) DEFAULT 'active'
);
```

#### `rbac_policies` Table
```sql
CREATE TABLE rbac_policies (
    id VARCHAR(50) PRIMARY KEY,
    role VARCHAR(50) NOT NULL,
    resource VARCHAR(50) NOT NULL,
    action VARCHAR(50) NOT NULL,
    tenant_id VARCHAR(50) NOT NULL,
    effect VARCHAR(10) DEFAULT 'allow',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(20) DEFAULT 'published'
);
```

#### `authorization_audit` Table
```sql
CREATE TABLE authorization_audit (
    id VARCHAR(50) PRIMARY KEY,
    user_id VARCHAR(50) NOT NULL,
    tenant_id VARCHAR(50) NOT NULL,
    resource VARCHAR(50) NOT NULL,
    action VARCHAR(50) NOT NULL,
    allowed BOOLEAN DEFAULT FALSE,
    reason TEXT,
    ip_address VARCHAR(45),
    user_agent TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(20) DEFAULT 'active'
);
```

## Implementation Details

### Ent Adapter

The custom Ent adapter (`internal/auth/rbac/ent_adapter.go`) implements Casbin's `persist.Adapter` interface:

```go
type EntAdapter struct {
    client *ent.Client
    logger *logger.Logger
}
```

#### Key Features:
- **Database Storage**: Policies stored in PostgreSQL via Ent ORM
- **Real-time Updates**: Policies can be modified without application restart
- **Multi-tenant Support**: All policies are tenant-scoped
- **Performance**: Caching with database persistence
- **ACID Compliance**: Database transactions ensure data consistency

#### Methods:
- `LoadPolicy()`: Load policies from database
- `SavePolicy()`: Save all policies to database
- `AddPolicy()`: Add single policy
- `RemovePolicy()`: Remove single policy
- `AddPolicies()`: Batch add policies
- `RemovePolicies()`: Batch remove policies
- `UpdatePolicy()`: Update existing policy
- `RemoveFilteredPolicy()`: Remove policies by filter
- `LoadFilteredPolicy()`: Load policies with filters

### RBAC Service

The service layer (`internal/auth/rbac/service.go`) provides:

#### Core Methods:
- `CheckPermission()`: Check if user has permission
- `GetUserRoles()`: Get user's roles
- `AssignRole()`: Assign role to user
- `RemoveRole()`: Remove role from user

#### Dynamic Role Management:
- `CreateRole()`: Create new role with permissions
- `UpdateRole()`: Update existing role
- `DeleteRole()`: Delete role and all its policies
- `ListRoles()`: List all roles with pagination

### Repository Layer

The repository (`internal/repository/rbac.go`) provides:

#### Interface:
```go
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
```

## API Endpoints

### Role Management
- `POST /v1/rbac/roles/create` - Create new role
- `PUT /v1/rbac/roles/:role_id` - Update role
- `DELETE /v1/rbac/roles/:role_id` - Delete role
- `GET /v1/rbac/roles` - List roles

### User Role Management
- `POST /v1/rbac/users/:user_id/roles` - Assign role to user
- `DELETE /v1/rbac/users/:user_id/roles/:role` - Remove role from user
- `GET /v1/rbac/users/:user_id/roles` - Get user's roles

### Authorization
- `POST /v1/rbac/check` - Check permission

## Configuration

### Model Configuration (`internal/auth/rbac/model.conf`)

```conf
[request_definition]
r = sub, obj, act, tenant

[policy_definition]
p = sub, obj, act, tenant, eft

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.tenant) && r.obj == p.obj && r.act == p.act && (r.tenant == p.tenant || p.tenant == "*")
```

### Default Roles

The system provides three default roles:

1. **superadmin**: Full access across all tenants
2. **manager**: Administrative access within tenant
3. **user**: Basic user access within tenant

## Usage Examples

### Check Permission
```go
allowed, err := rbacService.CheckPermission(ctx, userID, "invoice", "read", tenantID)
if err != nil {
    return err
}
if !allowed {
    return errors.New("permission denied")
}
```

### Assign Role
```go
err := rbacService.AssignRole(ctx, userID, "manager", tenantID)
if err != nil {
    return err
}
```

### Create Custom Role
```go
permissions := []string{
    "invoice:read",
    "invoice:create",
    "customer:read",
}
err := rbacService.CreateRole(ctx, "billing_manager", "Billing Manager", permissions, tenantID)
```

### Dynamic Policy Updates
```go
// Add new policy
enforcer.AddPolicy("billing_manager", "invoice", "update", "tenant123", "allow")

// Remove policy
enforcer.RemovePolicy("user", "invoice", "delete", "tenant123", "deny")

// Save to database
enforcer.SavePolicy()
```

## Benefits of Ent Adapter

### vs File-based Storage

| **File Storage** | **Database Adapter** |
|------------------|---------------------|
| ❌ Policies lost on restart | ✅ Persistent storage |
| ❌ No real-time updates | ✅ Dynamic policy updates |
| ❌ Single instance only | ✅ Multi-instance support |
| ❌ No ACID compliance | ✅ Database transactions |
| ❌ Slower file I/O | ✅ Optimized database queries |
| ❌ No concurrent access | ✅ Concurrent read/write |

### Key Advantages

1. **Scalability**: Multiple application instances can share policies
2. **Reliability**: Policies survive application restarts
3. **Performance**: Fast policy lookups with caching
4. **Management**: Real-time policy updates without restart
5. **Security**: Database-level access control and audit trail

## Migration from File-based Storage

### 1. Update Service Constructor
```go
// Old: File-based
adapter := fileadapter.NewAdapter("policy.csv")

// New: Database-based
adapter := NewEntAdapter(entClient, logger)
```

### 2. Load Existing Policies
```go
// Load policies from CSV file into database
policies := loadPoliciesFromCSV("policy.csv")
for _, policy := range policies {
    enforcer.AddPolicy(policy...)
}
enforcer.SavePolicy()
```

### 3. Remove File Dependencies
- Delete `internal/auth/rbac/policy.csv`
- Remove file adapter imports
- Update documentation

## Testing

### Unit Tests
```bash
go test ./internal/auth/rbac -v
```

### Integration Tests
```bash
# Test with real database
go test ./internal/auth/rbac -tags=integration
```

## Monitoring and Logging

### Audit Logging
All authorization decisions are logged to the `authorization_audit` table with:
- User ID and tenant
- Resource and action
- Decision (allowed/denied)
- Reason and metadata
- IP address and user agent

### Metrics
- Policy lookup performance
- Authorization decision rates
- Role assignment frequency
- Policy update frequency

## Security Considerations

1. **Tenant Isolation**: All policies are tenant-scoped
2. **Audit Trail**: All authorization decisions are logged
3. **Input Validation**: All inputs are validated and sanitized
4. **SQL Injection Protection**: Using parameterized queries via Ent
5. **Permission Escalation**: Preventing privilege escalation attacks

## Performance Optimization

1. **Caching**: Casbin caches policies in memory
2. **Database Indexing**: Optimized indexes on policy lookups
3. **Batch Operations**: Support for batch policy updates
4. **Connection Pooling**: Efficient database connection management

## Troubleshooting

### Common Issues

1. **Policy Not Found**: Check if policy exists in database
2. **Permission Denied**: Verify user has required role
3. **Database Connection**: Ensure Ent client is properly configured
4. **Cache Issues**: Clear Casbin cache if policies are stale

### Debug Commands
```go
// Check user roles
roles, err := rbacService.GetUserRoles(ctx, userID, tenantID)

// List all policies
policies, err := rbacService.GetPolicies(ctx, tenantID)

// Check specific permission
allowed, err := rbacService.CheckPermission(ctx, userID, resource, action, tenantID)
```

## Future Enhancements

1. **Policy Versioning**: Track policy changes over time
2. **Role Inheritance**: Hierarchical role structure
3. **Conditional Policies**: Time-based or context-based policies
4. **Policy Templates**: Reusable policy patterns
5. **Advanced Filtering**: Complex policy filtering options
6. **Performance Monitoring**: Detailed performance metrics
7. **Policy Analytics**: Usage patterns and optimization suggestions 