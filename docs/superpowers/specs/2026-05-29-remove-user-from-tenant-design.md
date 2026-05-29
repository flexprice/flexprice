# Remove User from Tenant — Design Spec

**Date:** 2026-05-29  
**Status:** Approved

## Overview

Add support for removing a user from a tenant and update the invite flow to allow re-inviting "free" users (users with no tenant association in the auth provider). Two changes ship together:

1. `DELETE /users/:id` — soft-deletes a user from the tenant and clears their auth provider tenant association.
2. Updated `POST /users` — transparently detects if an invited email belongs to a "free" user in the auth provider and re-assigns them instead of creating a fresh account.

## Constraints & Decisions

- **Soft delete only**: the DB record is kept (`status = deleted`); `tenant_id` remains in the DB for audit purposes. The auth provider (`app_metadata.tenant_id` in Supabase) is cleared to `""`.
- **Last-user guard**: if the caller is the only active user in the tenant, they cannot remove themselves. No other role-based restrictions.
- **Any authenticated user** within the tenant may call the remove endpoint.
- **Re-invite generates a new password** even for existing auth provider users.
- **Transparent re-invite**: same `POST /users` endpoint; the caller does not need to know whether the user is new or being re-invited.
- **Atomicity ordering**: auth provider calls happen before DB writes. A provider failure leaves the DB untouched. A DB failure after a successful provider call results in drift (consistent with the existing `InviteUser` pattern).

## API Layer

### New endpoint

```
DELETE /users/:id
```

- Calls `userService.RemoveUser(ctx, userID)`
- `204 No Content` on success
- `400` if the caller is the only remaining user and targets themselves
- `404` if the user does not exist in this tenant

### Unchanged endpoint

```
POST /users
```

Re-invite detection is transparent. The request shape and response shape are identical to a fresh invite.

**File:** [`internal/api/v1/user.go`](../../../internal/api/v1/user.go)

## Service Layer

### New method: `RemoveUser`

Added to `UserService` interface and `userService` struct.

```go
RemoveUser(ctx context.Context, userID string) error
```

**Steps:**
1. Fetch user by ID via `userRepo.GetByID` — validates existence within the tenant.
2. Read caller ID from context. If `callerID == userID`, count active users via `userRepo.ListByFilter`; if count is 1, return `ErrValidation`: "cannot remove the only remaining user from a tenant".
3. Call `provider.RemoveUserFromTenant(ctx, userID)` — clears auth provider tenant association.
4. Fetch user, set `Status = types.StatusDeleted`, update `UpdatedBy` / `UpdatedAt`, call `userRepo.Update(ctx, user)`.

**File:** [`internal/service/user.go`](../../../internal/service/user.go)

### Updated method: `InviteUser`

Insert a provider-level email check at the top of `InviteUser`, before the existing DB email lookup:

1. Call `provider.GetUserByEmail(ctx, email)` → returns `*ProviderUser{ID, TenantID}` or `nil`.
2. **If found and `TenantID != ""`** → return `ErrValidation` (400): "user already belongs to another tenant".
3. **If found and `TenantID == ""`** (free user) → re-invite path:
   - Generate a new password.
   - Call `provider.ResetUserPassword(ctx, providerUser.ID, newPassword)`.
   - Call `provider.AssignUserToTenant(ctx, providerUser.ID, tenantID)`.
   - Create a new DB user record with `providerUser.ID` as the user ID and current tenant context.
   - Return the new password (same `CreateUserResponse` shape).
4. **If not found** → existing fresh-create path, no changes.

The existing DB-level email uniqueness check (`GetByEmail` within tenant) is retained after the provider check as a secondary guard.

**File:** [`internal/service/user.go`](../../../internal/service/user.go)

## Provider Interface

**File:** [`internal/auth/provider.go`](../../../internal/auth/provider.go)

### New type

```go
type ProviderUser struct {
    ID       string
    TenantID string // empty string means the user is "free" (no tenant association)
}
```

### New interface methods

```go
// RemoveUserFromTenant clears the tenant association for a user in the auth provider.
RemoveUserFromTenant(ctx context.Context, userID string) error

// GetUserByEmail looks up a user in the auth provider by email.
// Returns nil, nil if the user does not exist (not an error).
GetUserByEmail(ctx context.Context, email string) (*ProviderUser, error)

// ResetUserPassword updates the user's password in the auth provider.
ResetUserPassword(ctx context.Context, userID string, newPassword string) error
```

### Supabase implementation

**File:** [`internal/auth/supabase.go`](../../../internal/auth/supabase.go)

| Method | Implementation |
|--------|---------------|
| `RemoveUserFromTenant` | `Admin.UpdateUser` — sets `app_metadata.tenant_id = ""` |
| `GetUserByEmail` | Supabase Admin list users filtered by email; reads `app_metadata.tenant_id` from result |
| `ResetUserPassword` | `Admin.UpdateUser` — sets new password |

### Flexprice implementation

**File:** [`internal/auth/flexprice.go`](../../../internal/auth/flexprice.go)

| Method | Implementation |
|--------|---------------|
| `RemoveUserFromTenant` | No-op — JWTs are self-contained and expire after 30 days |
| `GetUserByEmail` | Returns `nil, nil` — no global user store; re-invite always takes the fresh-create path |
| `ResetUserPassword` | No-op — `GetUserByEmail` always returns `nil` for Flexprice, so this method is never reached in the current feature |

## Repository Layer

### New method: `Update`

Added to the `user.Repository` interface and ent implementation.

```go
Update(ctx context.Context, user *domainUser.User) error
```

- Updates mutable fields: `status`, `roles`, `updated_at`, `updated_by`.
- Query scoped to `userID + tenantID` (from context) for tenant isolation.
- Returns `ErrNotFound` if no matching record exists in this tenant.

**Files:**  
- Interface: [`internal/domain/user/model.go`](../../../internal/domain/user/model.go) (or repository interface file)  
- Implementation: [`internal/repository/ent/user.go`](../../../internal/repository/ent/user.go)

## Error Handling

### `RemoveUser`

| Condition | Error | HTTP |
|-----------|-------|------|
| User not found in tenant | `ErrNotFound` | 404 |
| Caller is the only user, removing themselves | `ErrValidation`: "cannot remove the only remaining user from a tenant" | 400 |
| Auth provider failure | `ErrSystem` | 500 |

### `InviteUser` (updated)

| Condition | Error | HTTP |
|-----------|-------|------|
| User found in provider with non-empty `tenant_id` | `ErrValidation`: "user already belongs to another tenant" | 400 |
| `GetUserByEmail` provider call fails | `ErrSystem` | 500 |
| `ResetUserPassword` or `AssignUserToTenant` fails during re-invite | `ErrSystem` | 500 |

## Files Changed

| File | Change |
|------|--------|
| `internal/auth/provider.go` | Add `ProviderUser` type + 3 new interface methods |
| `internal/auth/supabase.go` | Implement `RemoveUserFromTenant`, `GetUserByEmail`, `ResetUserPassword` |
| `internal/auth/flexprice.go` | Implement `RemoveUserFromTenant` (no-op), `GetUserByEmail` (nil), `ResetUserPassword` (bcrypt update) |
| `internal/domain/user/model.go` | Add `Update` to repository interface |
| `internal/repository/ent/user.go` | Implement `Update` |
| `internal/service/user.go` | Add `RemoveUser`, update `InviteUser` |
| `internal/api/v1/user.go` | Add `RemoveUser` handler |
| `internal/api/router.go` | Register `DELETE /users/:id` route |
