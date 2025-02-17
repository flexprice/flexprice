# Entity Development Guide

## Overview
This document outlines the standardized approach for developing new entities in the FlexPrice system. It provides a step-by-step guide for implementing new domain entities, ensuring consistency and maintainability across the codebase.

## Development Flow

### 1. Schema Definition
- Create a new schema file in `ent/schema/{entity}.go`
- Define the entity structure using `ent` fields
- Implement required mixins (especially `BaseMixin`)
- Define edges/relationships with other entities
- Create appropriate indexes for performance

Example schema structure:
```go
package schema

type Entity struct {
    ent.Schema
}

func (Entity) Mixin() []ent.Mixin {
    return []ent.Mixin{
        mixin.BaseMixin{},
        // Add other mixins as needed
    }
}

func (Entity) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
        field.String("entity_status"). // ex invoice_status
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default("some_default_value"), // ex finalised
            // note that this is different from the status field which is part of the BaseMixin and it alreayd had published/archived/deleted as possible status
        // Add entity-specific fields
    }
}

func (Entity) Edges() []ent.Edge {
    return []ent.Edge{
        // Define relationships
        edge.To("related_entities", RelatedEntity.Type),
    }
}

func (Entity) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("tenant_id", "status"),
        // Add other indexes
    }
}
```

### 2. Generate Ent Code
Run the ent code generation command:
```bash
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/execquery ./ent/schema
```

### 3. Domain Layer Implementation
Create the following files in `internal/domain/{entity}/`:

#### a. Model Definition (`model.go`)
- Define the domain model struct
- Include `types.BaseModel`
- Implement `FromEnt` and `FromEntList` conversion methods
- Add domain-specific validations

Example:
```go
type Entity struct {
    ID          string
    // Entity-specific fields
    types.BaseModel
}

func FromEnt(e *ent.Entity) *Entity {
    if e == nil {
        return nil
    }
    return &Entity{
        ID: e.ID,
        BaseModel: types.BaseModel{
            TenantID:  e.TenantID,
            Status:    types.Status(e.Status),
            CreatedBy: e.CreatedBy,
            UpdatedBy: e.UpdatedBy,
            CreatedAt: e.CreatedAt,
            UpdatedAt: e.UpdatedAt,
        },
    }
}
```

#### b. Repository Interface (`repository.go`)
Define standard CRUD operations:
```go
type Repository interface {
    Create(ctx context.Context, entity *Entity) error
    Get(ctx context.Context, id string) (*Entity, error)
    List(ctx context.Context, filter *types.EntityFilter) ([]*Entity, error)
    Count(ctx context.Context, filter *types.EntityFilter) (int, error)
    Update(ctx context.Context, entity *Entity) error
    Delete(ctx context.Context, id string) error
}
```

### 4. Repository Implementation
Create implementation in `internal/repository/ent/{entity}.go`:
- Implement all repository interface methods
- Use ent client for database operations
- Handle proper error mapping
- Implement filter logic for List operations

### 5. In-Memory Store Implementation
Create `internal/testutil/inmemory_{entity}_store.go` for testing:
- Implement repository interface with in-memory storage
- Mirror all repository operations
- Include proper filtering and sorting logic

### 6. Factory Integration
Add repository to `internal/repository/factory.go`:
```go
func NewEntityRepository(p RepositoryParams) entity.Repository {
    return entRepo.NewEntityRepository(p.EntClient, p.Logger)
}
```

### 7. Service Layer Implementation
Create service in `internal/service/{entity}.go`:
- Define service interface
- Implement business logic
- Handle proper error handling and validation
- Implement transaction management if needed

### 8. DTO Layer Implementation
Create in `internal/api/dto/{entity}.go`:
- Define request/response structs
- Implement validation methods
- Add conversion helpers (ToEntity, ToEntityResponse)
- Use proper validation tags

Example:
```go
type CreateEntityRequest struct {
    Name string `json:"name" binding:"required"`
}

func (r *CreateEntityRequest) ToEntity() *entity.Entity {
    return &entity.Entity{
        ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY),
        Name: r.Name,
    }
}
```

### 9. API Handler Implementation
Create in `internal/api/v1/{entity}.go`:
- Implement REST endpoints
- Add proper documentation
- Handle request validation
- Implement proper error responses

### 10. Router Integration
Update `internal/api/router.go`:
- Add new handler to Handlers struct
- Define API routes
- Group related endpoints
- Add middleware if needed

## Best Practices

### 1. ID Generation
- Use `types.GenerateUUIDWithPrefix()` with entity-specific prefix
- Add prefix constant in `internal/types/uuid.go`

### 2. Error Handling
FlexPrice uses a centralized error handling approach with proper error wrapping and consistent error responses.

#### a. Centralized Error Definitions
All error codes and common errors are defined in `internal/errors/errors.go`:

```go
// Common error codes
const (
    ErrCodeNotFound         = "not_found"
    ErrCodeValidation       = "validation_error"
    ErrCodeInvalidOperation = "invalid_operation"
    ErrCodePermissionDenied = "permission_denied"
    ErrCodeSystemError      = "system_error"
)

// Common errors that can be reused across the application
var (
    ErrNotFound         = New(ErrCodeNotFound, "resource not found")
    ErrValidation       = New(ErrCodeValidation, "validation error")
    ErrInvalidOperation = New(ErrCodeInvalidOperation, "invalid operation")
    ErrPermissionDenied = New(ErrCodePermissionDenied, "permission denied")
)

// Error checking helpers
func IsNotFound(err error) bool {
    return errors.Is(err, ErrNotFound)
}

func IsValidation(err error) bool {
    return errors.Is(err, ErrValidation)
}
```

#### b. Error Handling in Repository Layer
Map database errors to appropriate error types:

```go
func (r *entityRepository) Get(ctx context.Context, id string) (*Entity, error) {
    e, err := r.client.Entity.Query().
        Where(entity.ID(id)).
        Only(ctx)
    
    if err != nil {
        if ent.IsNotFound(err) {
            return nil, errors.New(errors.ErrCodeNotFound, "entity not found")
        }
        return nil, fmt.Errorf("failed to get entity: %w", err)
    }
    
    return FromEnt(e), nil
}
```

#### c. Error Handling in Service Layer
Add context and operation information to errors:

```go
func (s *entityService) ProcessEntity(ctx context.Context, id string) error {
    // Input validation
    if id == "" {
        return errors.New(errors.ErrCodeValidation, "id is required")
    }
    
    // Get entity with proper error handling
    entity, err := s.entityRepo.Get(ctx, id)
    if err != nil {
        return errors.WithOp(err, "getting entity")
    }
    
    // Business logic validation with specific error message
    if entity.Status != types.StatusActive {
        return errors.New(errors.ErrCodeInvalidOperation, 
            "cannot process inactive entity")
    }
    
    // Process with transaction and wrap errors with context
    err = s.db.WithTx(ctx, func(tx *ent.Tx) error {
        if err := s.processEntityTx(ctx, tx, entity); err != nil {
            return errors.Wrap(err, errors.ErrCodeSystemError, 
                "failed to process entity")
        }
        return nil
    })
    
    return err
}
```

#### d. Common API Error Handling
Create a common error handling middleware/helper in `internal/api/errors.go`:

```go
// ErrorResponse represents a standardized error response
type ErrorResponse struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

// Common HTTP status code mapping
var errorStatusCodes = map[string]int{
    errors.ErrCodeNotFound:         http.StatusNotFound,
    errors.ErrCodeValidation:       http.StatusBadRequest,
    errors.ErrCodeInvalidOperation: http.StatusBadRequest,
    errors.ErrCodePermissionDenied: http.StatusForbidden,
}

// HandleError handles common error cases and returns appropriate responses
func HandleError(c *gin.Context, err error, logger *logger.Logger) {
    // Extract the internal error if wrapped
    var ierr *errors.InternalError
    if !errors.As(err, &ierr) {
        // Unknown error type, return 500
        c.JSON(http.StatusInternalServerError, ErrorResponse{
            Code:    "internal_error",
            Message: "an unexpected error occurred",
        })
        return
    }
    
    // Log error with context
    logger.Errorw("operation failed",
        "error", err,
        "code", ierr.Code,
        "operation", ierr.Op,
    )
    
    // Get status code from mapping or default to 500
    status := errorStatusCodes[ierr.Code]
    if status == 0 {
        status = http.StatusInternalServerError
    }
    
    c.JSON(status, ErrorResponse{
        Code:    ierr.Code,
        Message: ierr.Message,
    })
}
```

#### e. Using Common Error Handling in API Handlers

```go
func (h *EntityHandler) ProcessEntity(c *gin.Context) {
    id := c.Param("id")
    
    err := h.service.ProcessEntity(c.Request.Context(), id)
    if err != nil {
        HandleError(c, err, h.logger)
        return
    }
    
    c.JSON(http.StatusOK, gin.H{"message": "entity processed successfully"})
}
```

#### f. Error Handling Best Practices

1. **Use Centralized Error Definitions**
   - Use error codes from `internal/errors/errors.go`
   - Create new error codes only when truly needed
   - Keep error messages consistent and user-friendly

2. **Error Context**
   - Use `errors.Wrap` to add context to errors
   - Use `errors.WithOp` to track operation flow
   - Include relevant details in error messages

3. **Logging**
   - Log errors at the API layer
   - Include operation context and error details
   - Don't log sensitive information
   - Use appropriate log levels (error, warn, info)

4. **API Responses**
   - Use the common error handling middleware
   - Return consistent error structures
   - Map internal errors to appropriate HTTP status codes
   - Provide clear, actionable error messages

5. **Validation**
   - Return validation errors early
   - Use clear validation messages
   - Include field-specific error details when possible

Example Error Flow:
```
API Layer (HandleError)
    ↓
Service Layer (errors.Wrap/WithOp)
    ↓
Repository Layer (errors.New)
    ↓
Database Error
```

### 3. Validation
- Implement domain-level validation
- Use struct tags for DTO validation
- Add custom validators when needed

### 4. Testing
- Create unit tests for all layers
- Use in-memory store for service tests
- Implement integration tests for critical paths

## Implementation Checklist

- [ ] Schema definition
- [ ] Ent code generation
- [ ] Domain model and repository interface
- [ ] Repository implementation
- [ ] In-memory store
- [ ] Factory integration
- [ ] Service layer
- [ ] DTO layer
- [ ] API handler
- [ ] Router integration
- [ ] Tests
- [ ] Documentation

## Notes
- Always follow the existing patterns in the codebase
- Maintain consistency in naming and structure
- Consider performance implications when defining indexes
- Document all public interfaces and methods
- Add proper logging at service and repository layers 