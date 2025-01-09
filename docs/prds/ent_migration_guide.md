# Ent Migration Guide

This document outlines the process of migrating existing PostgreSQL-based repositories to use the Ent ORM framework.

## Overview

We are migrating from raw SQL queries to Ent ORM to:
1. Improve type safety and code maintainability
2. Reduce boilerplate code and potential SQL injection vulnerabilities
3. Leverage Ent's powerful graph-based relationships and querying capabilities
4. Standardize our database access patterns across the codebase

## Migration Steps

### 1. Define Ent Schema

Create a new schema file in `ent/schema/` directory:

1. Define the entity structure using Ent's schema API
2. Add fields matching the database table schema
3. Define indexes and edges (relationships)
4. Use appropriate field types and constraints
5. Implement Mixin for common fields (BaseModel)

Example:
```go
// ent/schema/customer.go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
    baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

type Customer struct {
    ent.Schema
}

func (Customer) Mixin() []ent.Mixin {
    return []ent.Mixin{
        baseMixin.BaseMixin{},
    }
}

func (Customer) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").
            SchemaType(map[string]string{
                "postgres": "varchar(50)",
            }).
            Unique().
            Immutable(),
        // ... other fields
    }
}

func (Customer) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("tenant_id", "external_id").
            Unique(),
    }
}
```

### 2. Update Domain Model

1. Ensure domain model matches Ent schema
2. Add FromEnt conversion method
3. Update any related interfaces or types

Example:
```go
// internal/domain/customer/model.go
func FromEnt(e *ent.Customer) *Customer {
    if e == nil {
        return nil
    }
    return &Customer{
        ID:         e.ID,
        ExternalID: e.ExternalID,
        Name:       e.Name,
        Email:      e.Email,
        BaseModel: types.BaseModel{
            TenantID:  e.TenantID,
            Status:    types.Status(e.Status),
            CreatedAt: e.CreatedAt,
            UpdatedAt: e.UpdatedAt,
            CreatedBy: e.CreatedBy,
            UpdatedBy: e.UpdatedBy,
        },
    }
}
```

### 3. Implement Ent Repository

Create a new repository implementation in `internal/repository/ent/`:

1. Use postgres.IClient interface for Ent client
2. Implement all repository interface methods
3. Use transactions where necessary
4. Add proper error handling and logging
5. Implement filter parsing if needed

Example Structure:
```go
type customerRepository struct {
    client postgres.IClient
    log    *logger.Logger
}

func NewCustomerRepository(client postgres.IClient, log *logger.Logger) customer.Repository {
    return &customerRepository{
        client: client,
        log:    log,
    }
}

// Implement all interface methods...
```

### 4. Update Repository Factory

Update `internal/repository/factory.go` to use the new Ent repository:

1. Add new repository constructor
2. Update factory method to return Ent implementation
3. Remove old implementation

### 5. Testing

1. Write unit tests for new repository implementation
2. Add integration tests
3. Test all edge cases and error scenarios
4. Verify performance characteristics

## Best Practices

1. **Error Handling**
   - Use meaningful error messages
   - Wrap errors with context
   - Log appropriate error details

2. **Transactions**
   - Use transactions for multi-operation changes
   - Properly handle rollbacks
   - Consider using context for transaction management

3. **Querying**
   - Use Ent's query builder
   - Implement proper filtering
   - Consider pagination for large result sets

4. **Logging**
   - Log important operations
   - Include relevant context (tenant_id, user_id)
   - Use appropriate log levels

## Migration Checklist

- [ ] Create Ent schema
- [ ] Update domain model
- [ ] Implement Ent repository
- [ ] Update factory
- [ ] Add tests
- [ ] Review and update documentation
- [ ] Deploy and monitor

## Example Migration

See the following examples for reference:
- Subscription: `ent/schema/subscription.go`
- Invoice: `internal/repository/ent/invoice.go`

## Notes

- Always run tests before and after migration
- Consider data migration if schema changes are needed
- Monitor performance after migration
- Update related services and handlers if needed
