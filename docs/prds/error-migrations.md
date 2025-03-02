# Error Handling Migration Plan

## Overview

This document outlines the plan for migrating all entities in the FlexPrice system to use the centralized error handling system (`ierr`). The goal is to ensure consistent error handling across the codebase, providing better user experience, improved debugging, and standardized error responses.

## Background

The FlexPrice system is transitioning from domain-specific error handling to a centralized error system. This migration involves:

1. Removing domain-specific error files (e.g., `internal/domain/feature/errors.go`)
2. Updating repository, service, and API layers to use the central error builder pattern
3. Ensuring test utilities (in-memory stores) match the error handling of their production counterparts

## Migration Process for Each Entity

For each entity in the system, follow these steps:

### 1. Remove Domain-Specific Errors

- Delete the domain-specific errors file (if it exists): `internal/domain/{entity}/errors.go`
- Ensure no imports reference the deleted file

### 2. Update Repository Layer

- Modify the repository implementation to use the central error system:
  - Replace domain-specific error returns with `ierr.WithError()`, `ierr.NewError()`, etc.
  - Add appropriate hints using `WithHint()` or `WithHintf()`
  - Include reportable details with `WithReportableDetails()`
  - Mark errors with appropriate error codes using `Mark()`
- Common error scenarios to handle:
  - Not found errors: `Mark(ierr.ErrNotFound)`
  - Validation errors: `Mark(ierr.ErrValidation)`
  - Database errors: `Mark(ierr.ErrDatabase)`
  - Already exists errors: `Mark(ierr.ErrAlreadyExists)`

### 3. Update Service Layer

- Update service methods to:
  - Use `ierr.NewError()` for validation errors
  - Preserve error context from repository layer
  - Add appropriate hints for user-friendly messages
  - Include structured details for debugging

### 4. Update API Handler

- Ensure API handlers use `c.Error()` to pass errors to middleware
- Update validation error handling to use the structured format
- Standardize HTTP status codes based on error types

### 5. Update In-Memory Store for Tests

- Modify the in-memory store implementation to match the repository error handling:
  - Use the same error builder pattern
  - Return identical error structures for the same scenarios
  - Ensure error messages and hints match production code

### 6. Verify Migration

- Run all tests to ensure they pass with the new error handling
- Verify API responses contain the expected error structure
- Search for any remaining references to domain-specific errors

## Error Handling Patterns

### Repository Layer

```go
// Not found error
if ent.IsNotFound(err) {
    return nil, ierr.WithError(err).
        WithHintf("%s with ID %s was not found", entityName, id).
        WithReportableDetails(map[string]any{
            "entity_id": id,
        }).
        Mark(ierr.ErrNotFound)
}

// Already exists error
if ent.IsConstraintError(err) {
    return ierr.WithError(err).
        WithHint("A %s with this identifier already exists").
        WithReportableDetails(map[string]any{
            "identifier": value,
        }).
        Mark(ierr.ErrAlreadyExists)
}

// Database error
return ierr.WithError(err).
    WithHintf("Failed to %s %s", operation, entityName).
    Mark(ierr.ErrDatabase)
```

### Service Layer

```go
// Validation error
if id == "" {
    return nil, ierr.NewError("%s ID is required", entityName).
        WithHint("%s ID is required", entityName).
        Mark(ierr.ErrValidation)
}

// Business rule error
return nil, ierr.NewError("invalid operation").
    WithHint("Cannot perform this operation due to business rules").
    WithReportableDetails(map[string]any{
        "reason": "specific reason",
    }).
    Mark(ierr.ErrValidation)
```

### API Handler

```go
// Binding error
if err := c.ShouldBindJSON(&req); err != nil {
    c.Error(ierr.WithError(err).
        WithHint("Invalid request format").
        Mark(ierr.ErrValidation))
    return
}

// Missing parameter
if id == "" {
    c.Error(ierr.NewError("%s ID is required", entityName).
        WithHint("%s ID is required", entityName).
        Mark(ierr.ErrValidation))
    return
}
```

### In-Memory Store

Also need to make sure the in-memory store matches the error handling of the repository layer.

### Router
Add the error handler middleware to the router for the entity.

```go
feature := v1Private.Group("/features")
feature.Use(middleware.ErrorHandler())
{
    feature.POST("", handlers.Feature.CreateFeature)
}
```

## Entity Migration Checklist

- [x] Feature
- [x] Invoice
- [x] Customer
- [x] Subscription
- [x] Plan
- [x] Price
- [x] Meter
- [x] Entitlement
- [x] Payment
- [x] Wallet
- [ ] User
- [ ] Tenant
- [ ] Environment
- [ ] Task
- [ ] Secret
- [ ] Auth

## Benefits of Migration

1. **Better User Experience**: Consistent, user-friendly error messages
2. **Improved Debugging**: Structured error details for easier troubleshooting
3. **Consistent Error Handling**: Uniform approach across all entities
4. **Type Safety**: Error codes provide type-safe error checking
5. **Simplified Testing**: In-memory stores that match production behavior

## Verification Strategy

After migrating each entity, verify the changes by:

1. Running entity-specific tests
2. Running integration tests
3. Searching for any remaining domain-specific error references
4. Checking API responses for proper error structure

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

## Conclusion

This migration plan provides a systematic approach to updating all entities in the FlexPrice system to use the centralized error handling system. Following this plan will ensure consistency across the codebase and improve the overall quality of error handling. 