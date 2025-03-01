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
- Note use lo.FromPtr() and lo.ToPtr() to convert between pointers to values for nillable schema fields
- Note that we will use types.Metadata for metadata fields which will always be map[string]string

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
// Common error types that can be used across the application
var (
    ErrNotFound         = New(ErrCodeNotFound, "resource not found")
    ErrAlreadyExists    = New(ErrCodeAlreadyExists, "resource already exists")
    ErrVersionConflict  = New(ErrCodeVersionConflict, "version conflict")
    ErrValidation       = New(ErrCodeValidation, "validation error")
    ErrInvalidOperation = New(ErrCodeInvalidOperation, "invalid operation")
    ErrPermissionDenied = New(ErrCodePermissionDenied, "permission denied")
    ErrHTTPClient       = New(ErrCodeHTTPClient, "http client error")
    ErrDatabase         = New(ErrCodeDatabase, "database error")
    ErrSystem           = New(ErrCodeSystemError, "system error")
)

// Common error codes
const (
    ErrCodeHTTPClient       = "http_client_error"
    ErrCodeSystemError      = "system_error"
    ErrCodeNotFound         = "not_found"
    ErrCodeAlreadyExists    = "already_exists"
    ErrCodeVersionConflict  = "version_conflict"
    ErrCodeValidation       = "validation_error"
    ErrCodeInvalidOperation = "invalid_operation"
    ErrCodePermissionDenied = "permission_denied"
    ErrCodeDatabase         = "database_error"
)

// Error checking helpers
func IsNotFound(err error) bool {
    return errors.Is(err, ErrNotFound)
}

func IsValidation(err error) bool {
    return errors.Is(err, ErrValidation)
}
```

#### b. Error Builder Pattern
FlexPrice uses a fluent error builder pattern for creating and enriching errors. The builder is defined in `internal/errors/builder.go`:

```go
// Create a new error
ierr.NewError("entity not found").
    WithHint("The requested entity could not be found").
    Mark(ierr.ErrNotFound)

// here WithHint message will be shown directly to the user
// vs the NewError message which is more for developers and wont
// be shown on the UI and will only be part of the API response

// Wrap an existing error
ierr.WithError(err).
    WithHint("Failed to create entity").
    Mark(ierr.ErrDatabase)

// Add formatted hint
ierr.WithError(err).
    WithHintf("Entity with ID %s not found", id).
    Mark(ierr.ErrNotFound)

// notice how Mark here is used to categorize the error
// and we can use the category of error to compare everywhere
// in the code and handle them accordingly rather than using
// the error message which is more vague and not actionable

// Add reportable details for structured error information
// this we will use rarely and focus more with direct errors
ierr.NewError("invalid status transition").
    WithHint("The requested status transition is not allowed").
    WithReportableDetails(map[string]any{
        "current_status": currentStatus,
        "requested_status": requestedStatus,
        "allowed_transitions": allowedTransitions,
    }).
    Mark(ierr.ErrValidation)
```

**Important**: The `Mark()` method should always be the last call in the builder chain as it returns the final error.

#### c. Error Handling in Repository Layer
Map database errors to appropriate error types:

```go
func (r *entityRepository) Get(ctx context.Context, id string) (*Entity, error) {
    e, err := r.client.Entity.Query().
        Where(entity.ID(id)).
        Only(ctx)
    
    if err != nil {
        if ent.IsNotFound(err) {
            return nil, ierr.WithError(err).
                WithHintf("entity %s not found", id).
                Mark(ierr.ErrNotFound)
        }
        return nil, ierr.WithError(err).
            WithHint("database query failed").
            Mark(ierr.ErrDatabase)
    }
    
    return FromEnt(e), nil
}
```

#### d. Error Handling in Service Layer
Add context and operation information to errors:

```go
func (s *entityService) ProcessEntity(ctx context.Context, id string) error {
    // Input validation
    if id == "" {
        return ierr.NewError("id is required").
            WithHint("The entity ID is required").
            Mark(ierr.ErrValidation)
    }
    
    // Get entity with proper error handling
    entity, err := s.entityRepo.Get(ctx, id)
    if err != nil {
        // Repository errors are already properly formatted, just return them
        return err
    }
    
    // Business logic validation with specific error message
    if entity.Status != types.StatusActive {
        return ierr.NewError("entity must be active").
            WithHintf("entity with status %s cannot be processed", entity.Status).
            // These re helpful when we want to report the value of multiple fields and it doesn't make sense to have a single string with all the details
            WithReportableDetails(map[string]any{
                "current_status": entity.Status,
                "required_status": types.StatusActive,
            }).Mark(ierr.ErrInvalidOperation)
    }
    
    // Process with transaction and wrap errors with context
    err = s.db.WithTx(ctx, func(tx *ent.Tx) error {
        if err := s.processEntityTx(ctx, tx, entity); err != nil {
            return ierr.WithError(err).
                WithHint("entity processing failed").
                Mark(ierr.ErrSystem)
        }
        return nil
    })
    
    return err
}
```

#### e. Error Handling in API Layer
Handle errors in API handlers:

```go   
func (h *entityHandler) GetEntity(c *gin.Context) {
    id := c.Param("id")
    entity, err := h.entityService.GetEntity(c.Request.Context(), id)
    if err != nil {
        c.Error(err)
        return
    }
}
```
These errors will be handled by the error handling middleware
and will return a proper error response to the client  refer `internal/rest/middleware/errhandler.go`

#### g. Error Handling Best Practices

1. **Use the Error Builder Pattern**
   - Use `ierr.NewError()` to create new errors
   - Use `ierr.WithError()` to wrap existing errors
   - Always end builder chains with `Mark()` to categorize the error
   - Use `WithHint()` for user-friendly messages which can be shown to the user
   - Use `WithReportableDetails()` for highlingting any specific parameters or values which are relevant to the error and we rarely need to use this only when super important

2. **Error Context and Details**
   - Use `WithHintf()` to add formatted context to errors
   - Include relevant IDs and parameters in error messages
   - Use `WithReportableDetails()` to add structured data for API responses
   - Keep error messages clear and actionable

3. **Error Categorization**
   - Use appropriate error markers from `internal/errors/errors.go`
   - Be consistent with error codes across similar operations

4. **Logging**
   - Log errors at the API layer
   - Include operation context and error details
   - Don't log sensitive information like email_id, phone_number, etc
   - Use appropriate log levels (error, warn, info)

5. **API Responses**
   - Use the common error handling middleware
   - Return consistent error structures
   - Include structured details when available
   - Provide clear, actionable error messages

Example Error Flow:
```
API Layer (HandleError)
    ↓
Service Layer (ierr.NewError/WithError → WithHint → Mark)
    ↓
Repository Layer (ierr.WithError → WithHint → Mark)
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