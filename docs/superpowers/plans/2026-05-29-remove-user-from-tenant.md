# Remove User from Tenant Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `DELETE /users/:id` to soft-delete a user from a tenant and update `POST /users` to transparently re-invite "free" users (previously removed, no tenant in Supabase `app_metadata`).

**Architecture:** Extend the `Provider` interface with three new methods (`RemoveUserFromTenant`, `GetUserByEmail`, `ResetUserPassword`), implement them in both Supabase and Flexprice providers, add `Update` to the user repository, then wire up new service methods and a HTTP handler. The service `InviteUser` method gains a provider-level email check before calling `UserInvite` to detect and handle free users.

**Tech Stack:** Go 1.23, Gin, Uber FX, Ent ORM (PostgreSQL), Supabase Admin API (direct HTTP for `GetUserByEmail`), testify/suite for tests.

---

## File Map

| File | Change |
|------|--------|
| `internal/auth/provider.go` | Add `ProviderUser` type + 3 interface methods |
| `internal/auth/flexprice.go` | Implement 3 new methods (no-ops / nil) |
| `internal/auth/supabase.go` | Implement `RemoveUserFromTenant`, `GetUserByEmail`, `ResetUserPassword` |
| `internal/domain/user/repository.go` | Add `Update(ctx, *User) error` to `Repository` interface |
| `internal/repository/ent/user.go` | Implement `Update` |
| `internal/testutil/inmemory_user_store.go` | Add `Update` to satisfy the updated interface |
| `internal/testutil/mock_auth_provider.go` | **New** — configurable mock for `Provider` interface |
| `internal/service/user.go` | Add `provider` field + `getProvider()`, add `RemoveUser` to interface + impl, update `InviteUser` |
| `internal/service/user_test.go` | Tests for `RemoveUser` + updated `InviteUser` |
| `internal/api/v1/user.go` | Add `RemoveUser` handler |
| `internal/api/router.go` | Register `DELETE /users/:id` |

---

## Task 1: Extend the Provider interface

**Files:**
- Modify: `internal/auth/provider.go`

- [ ] **Step 1: Add `ProviderUser` type and 3 new method signatures**

Open `internal/auth/provider.go`. After the existing `UserInviteResponse` struct (line 48), add:

```go
// ProviderUser is a minimal user record returned by the auth provider.
// TenantID is empty when the user has no tenant association ("free" user).
type ProviderUser struct {
	ID       string
	TenantID string
}
```

Then add 3 new methods to the `Provider` interface (after `AssignUserToTenant`):

```go
// RemoveUserFromTenant clears the tenant association for a user in the auth provider.
RemoveUserFromTenant(ctx context.Context, userID string) error

// GetUserByEmail looks up a user in the auth provider by email.
// Returns nil, nil when the user does not exist — this is not an error.
GetUserByEmail(ctx context.Context, email string) (*ProviderUser, error)

// ResetUserPassword updates the user's password in the auth provider.
ResetUserPassword(ctx context.Context, userID string, newPassword string) error
```

- [ ] **Step 2: Verify the file compiles (will fail until both providers implement the interface)**

```bash
cd /path/to/repo && go build ./internal/auth/...
```

Expected: compile errors about missing methods on `flexpriceAuth` and `supabaseAuth`. That's expected — proceed to Task 2.

- [ ] **Step 3: Commit**

```bash
git add internal/auth/provider.go
git commit -m "feat(auth): add ProviderUser type and RemoveUserFromTenant/GetUserByEmail/ResetUserPassword to Provider interface"
```

---

## Task 2: Implement new Provider methods in Flexprice auth

**Files:**
- Modify: `internal/auth/flexprice.go`

- [ ] **Step 1: Add the three no-op / nil methods**

Append these three methods to `internal/auth/flexprice.go` (after the existing `UserInvite` method):

```go
// RemoveUserFromTenant is a no-op for Flexprice — JWTs are self-contained and expire after 30 days.
func (f *flexpriceAuth) RemoveUserFromTenant(ctx context.Context, userID string) error {
	return nil
}

// GetUserByEmail always returns nil for Flexprice — there is no global user store.
// Re-invite for Flexprice always follows the fresh-create path.
func (f *flexpriceAuth) GetUserByEmail(ctx context.Context, email string) (*ProviderUser, error) {
	return nil, nil
}

// ResetUserPassword is a no-op for Flexprice — GetUserByEmail always returns nil,
// so this method is never reached in the current feature.
func (f *flexpriceAuth) ResetUserPassword(ctx context.Context, userID string, newPassword string) error {
	return nil
}
```

- [ ] **Step 2: Verify the Flexprice provider now satisfies the interface**

```bash
go build ./internal/auth/...
```

Expected: only supabase compile errors remain.

- [ ] **Step 3: Commit**

```bash
git add internal/auth/flexprice.go
git commit -m "feat(auth): implement RemoveUserFromTenant/GetUserByEmail/ResetUserPassword in flexpriceAuth (no-ops)"
```

---

## Task 3: Implement new Provider methods in Supabase auth

**Files:**
- Modify: `internal/auth/supabase.go`

- [ ] **Step 1: Add imports**

The `GetUserByEmail` method needs `encoding/json`, `fmt`, `io`, `net/http`, and `net/url`. Add them to the import block in `internal/auth/supabase.go`. The file already imports `fmt` — add the rest:

```go
import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "net/url"
    "time"

    "github.com/flexprice/flexprice/internal/config"
    "github.com/flexprice/flexprice/internal/domain/auth"
    ierr "github.com/flexprice/flexprice/internal/errors"
    "github.com/flexprice/flexprice/internal/logger"
    "github.com/flexprice/flexprice/internal/types"
    "github.com/golang-jwt/jwt/v4"
    "github.com/nedpals/supabase-go"
    "github.com/sethvargo/go-password/password"
)
```

- [ ] **Step 2: Add a private response struct for the Supabase Admin list-users response**

Add this struct just above the `supabaseAuth` struct definition:

```go
// supabaseAdminUsersResponse matches the Supabase Auth Admin GET /auth/v1/admin/users response.
type supabaseAdminUsersResponse struct {
    Users []supabase.AdminUser `json:"users"`
}
```

- [ ] **Step 3: Implement `RemoveUserFromTenant`**

Append to `internal/auth/supabase.go` (after the existing `UserInvite` method):

```go
func (s *supabaseAuth) RemoveUserFromTenant(ctx context.Context, userID string) error {
    _, err := s.client.Admin.UpdateUser(ctx, userID, supabase.AdminUserParams{
        AppMetadata: map[string]interface{}{
            "tenant_id": "",
        },
    })
    if err != nil {
        return ierr.WithError(err).
            WithHint("Failed to remove user from tenant in Supabase").
            Mark(ierr.ErrSystem)
    }
    return nil
}
```

- [ ] **Step 4: Implement `GetUserByEmail`**

The `nedpals/supabase-go@v0.5.0` library does not expose a list-users-by-email admin method, so we make a direct HTTP call to the Supabase Auth Admin API.

Append to `internal/auth/supabase.go`:

```go
func (s *supabaseAuth) GetUserByEmail(ctx context.Context, email string) (*ProviderUser, error) {
    // Supabase Admin API: GET /auth/v1/admin/users?email=<email>&per_page=1
    reqURL := fmt.Sprintf("%s/auth/v1/admin/users?email=%s&per_page=1",
        s.AuthConfig.Supabase.BaseURL,
        url.QueryEscape(email),
    )

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
    if err != nil {
        return nil, ierr.WithError(err).
            WithHint("Failed to build Supabase admin list users request").
            Mark(ierr.ErrSystem)
    }
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.AuthConfig.Supabase.ServiceKey))
    req.Header.Set("apikey", s.AuthConfig.Supabase.ServiceKey)

    httpClient := &http.Client{}
    resp, err := httpClient.Do(req)
    if err != nil {
        return nil, ierr.WithError(err).
            WithHint("Failed to call Supabase admin list users").
            Mark(ierr.ErrSystem)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, ierr.NewError("Supabase admin list users returned non-200").
            WithHint(fmt.Sprintf("status %d: %s", resp.StatusCode, string(body))).
            Mark(ierr.ErrSystem)
    }

    var result supabaseAdminUsersResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, ierr.WithError(err).
            WithHint("Failed to decode Supabase admin list users response").
            Mark(ierr.ErrSystem)
    }

    // Filter for exact email match (Supabase may do substring matching)
    for _, u := range result.Users {
        if u.Email != email {
            continue
        }
        tenantID := ""
        if tid, ok := u.AppMetaData["tenant_id"].(string); ok {
            tenantID = tid
        }
        return &ProviderUser{
            ID:       u.ID,
            TenantID: tenantID,
        }, nil
    }

    return nil, nil // user does not exist
}
```

- [ ] **Step 5: Implement `ResetUserPassword`**

Append to `internal/auth/supabase.go`:

```go
func (s *supabaseAuth) ResetUserPassword(ctx context.Context, userID string, newPassword string) error {
    _, err := s.client.Admin.UpdateUser(ctx, userID, supabase.AdminUserParams{
        Password: &newPassword,
    })
    if err != nil {
        return ierr.WithError(err).
            WithHint("Failed to reset user password in Supabase").
            Mark(ierr.ErrSystem)
    }
    return nil
}
```

- [ ] **Step 6: Verify the full auth package compiles**

```bash
go build ./internal/auth/...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/auth/supabase.go
git commit -m "feat(auth): implement RemoveUserFromTenant, GetUserByEmail, ResetUserPassword in supabaseAuth"
```

---

## Task 4: Add `Update` to the user Repository

**Files:**
- Modify: `internal/domain/user/repository.go`
- Modify: `internal/repository/ent/user.go`
- Modify: `internal/testutil/inmemory_user_store.go`

- [ ] **Step 1: Write the failing test for `Update` in the in-memory store**

Open `internal/service/user_test.go`. In `UserServiceSuite`, add this test directly after `TestGetUserInfo`:

```go
func (s *UserServiceSuite) TestUserRepo_Update() {
    ctx := testutil.SetupContext()

    // seed a user
    u := &user.User{
        ID:        "u-update-1",
        Email:     "update@example.com",
        BaseModel: types.GetDefaultBaseModel(ctx),
    }
    s.Require().NoError(s.userRepo.Create(ctx, u))

    // change status to deleted
    u.Status = types.StatusDeleted
    s.Require().NoError(s.userRepo.Update(ctx, u))

    // the user must no longer appear in published list
    _, count, err := s.userRepo.ListByFilter(ctx, &types.UserFilter{
        QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(uint64(10)), Status: lo.ToPtr(types.StatusPublished)},
    })
    s.Require().NoError(err)
    s.Equal(int64(0), count)
}
```

Add `"github.com/samber/lo"` to the test file imports if missing.

- [ ] **Step 2: Run the test — verify it fails**

```bash
go test -v -run TestUserService/TestUserRepo_Update ./internal/service/...
```

Expected: compile error `userRepo.Update undefined`.

- [ ] **Step 3: Add `Update` to the Repository interface**

Edit `internal/domain/user/repository.go` to add `Update`:

```go
type Repository interface {
    Create(ctx context.Context, user *User) error
    GetByID(ctx context.Context, id string) (*User, error)
    GetByEmail(ctx context.Context, email string) (*User, error)
    ListByFilter(ctx context.Context, filter *types.UserFilter) ([]*User, int64, error)
    Update(ctx context.Context, user *User) error
}
```

- [ ] **Step 4: Implement `Update` in the ent repository**

Append to `internal/repository/ent/user.go`:

```go
// Update updates mutable fields on an existing user scoped to the tenant in context.
func (r *userRepository) Update(ctx context.Context, u *domainUser.User) error {
    tenantID, ok := ctx.Value(types.CtxTenantID).(string)
    if !ok {
        return ierr.NewError("tenant ID not found in context").
            WithHint("Tenant ID is required in the context").
            Mark(ierr.ErrValidation)
    }

    span := StartRepositorySpan(ctx, "user", "update", map[string]interface{}{
        "user_id":   u.ID,
        "tenant_id": tenantID,
    })
    defer FinishSpan(span)

    client := r.client.Writer(ctx)
    n, err := client.User.Update().
        Where(
            entUser.ID(u.ID),
            entUser.TenantID(tenantID),
        ).
        SetStatus(string(u.Status)).
        SetRoles(u.Roles).
        SetUpdatedBy(u.UpdatedBy).
        SetUpdatedAt(u.UpdatedAt).
        Save(ctx)

    if err != nil {
        SetSpanError(span, err)
        return ierr.WithError(err).
            WithHint("Failed to update user").
            WithReportableDetails(map[string]interface{}{
                "user_id":   u.ID,
                "tenant_id": tenantID,
            }).
            Mark(ierr.ErrDatabase)
    }

    if n == 0 {
        SetSpanError(span, err)
        return ierr.NewError("user not found").
            WithHint("No user matched the given ID and tenant").
            WithReportableDetails(map[string]interface{}{
                "user_id":   u.ID,
                "tenant_id": tenantID,
            }).
            Mark(ierr.ErrNotFound)
    }

    SetSpanSuccess(span)
    return nil
}
```

- [ ] **Step 5: Implement `Update` in the in-memory store**

Append to `internal/testutil/inmemory_user_store.go`:

```go
// Update updates mutable fields of an existing user in the in-memory store.
func (r *InMemoryUserStore) Update(ctx context.Context, u *user.User) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    for email, stored := range r.users {
        if stored.ID == u.ID {
            stored.Status = u.Status
            stored.Roles = u.Roles
            stored.UpdatedBy = u.UpdatedBy
            stored.UpdatedAt = u.UpdatedAt
            r.users[email] = stored
            return nil
        }
    }
    return ierr.NewError("user not found").Mark(ierr.ErrNotFound)
}
```

- [ ] **Step 6: Run the test — verify it passes**

```bash
go test -v -run TestUserService/TestUserRepo_Update ./internal/service/...
```

Expected: PASS.

- [ ] **Step 7: Verify full build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/domain/user/repository.go internal/repository/ent/user.go internal/testutil/inmemory_user_store.go internal/service/user_test.go
git commit -m "feat(user): add Update method to user Repository interface and implementations"
```

---

## Task 5: Create MockAuthProvider for service tests

**Files:**
- Create: `internal/testutil/mock_auth_provider.go`

- [ ] **Step 1: Create the mock**

Create `internal/testutil/mock_auth_provider.go`:

```go
package testutil

import (
    "context"
    "time"

    authProvider "github.com/flexprice/flexprice/internal/auth"
    domainAuth "github.com/flexprice/flexprice/internal/domain/auth"
    "github.com/flexprice/flexprice/internal/types"
)

// MockAuthProvider is a configurable test double for the auth.Provider interface.
// Set the *Fn fields to control behaviour; unset fields return zero values / nil errors.
type MockAuthProvider struct {
    GetUserByEmailFn       func(ctx context.Context, email string) (*authProvider.ProviderUser, error)
    RemoveUserFromTenantFn func(ctx context.Context, userID string) error
    ResetUserPasswordFn    func(ctx context.Context, userID string, newPassword string) error
    AssignUserToTenantFn   func(ctx context.Context, userID string, tenantID string) error
    UserInviteFn           func(ctx context.Context, req authProvider.UserInviteRequest) (*authProvider.UserInviteResponse, error)
}

func (m *MockAuthProvider) GetProvider() types.AuthProvider { return types.AuthProviderFlexprice }

func (m *MockAuthProvider) GetUserByEmail(ctx context.Context, email string) (*authProvider.ProviderUser, error) {
    if m.GetUserByEmailFn != nil {
        return m.GetUserByEmailFn(ctx, email)
    }
    return nil, nil
}

func (m *MockAuthProvider) RemoveUserFromTenant(ctx context.Context, userID string) error {
    if m.RemoveUserFromTenantFn != nil {
        return m.RemoveUserFromTenantFn(ctx, userID)
    }
    return nil
}

func (m *MockAuthProvider) ResetUserPassword(ctx context.Context, userID string, newPassword string) error {
    if m.ResetUserPasswordFn != nil {
        return m.ResetUserPasswordFn(ctx, userID, newPassword)
    }
    return nil
}

func (m *MockAuthProvider) AssignUserToTenant(ctx context.Context, userID string, tenantID string) error {
    if m.AssignUserToTenantFn != nil {
        return m.AssignUserToTenantFn(ctx, userID, tenantID)
    }
    return nil
}

func (m *MockAuthProvider) UserInvite(ctx context.Context, req authProvider.UserInviteRequest) (*authProvider.UserInviteResponse, error) {
    if m.UserInviteFn != nil {
        return m.UserInviteFn(ctx, req)
    }
    return &authProvider.UserInviteResponse{
        ID:         "mock-user-id",
        Password:   "mock-password",
        AuthRecord: nil,
    }, nil
}

// --- Stub implementations for methods not under test ---

func (m *MockAuthProvider) SignUp(ctx context.Context, req authProvider.AuthRequest) (*authProvider.AuthResponse, error) {
    return &authProvider.AuthResponse{}, nil
}

func (m *MockAuthProvider) Login(ctx context.Context, req authProvider.AuthRequest, userAuthInfo *domainAuth.Auth) (*authProvider.AuthResponse, error) {
    return &authProvider.AuthResponse{}, nil
}

func (m *MockAuthProvider) ValidateToken(ctx context.Context, token string) (*domainAuth.Claims, error) {
    return &domainAuth.Claims{}, nil
}

func (m *MockAuthProvider) GenerateSessionToken(customerID, externalCustomerID, tenantID, environmentID string, timeoutHours int) (string, time.Time, error) {
    return "", time.Time{}, nil
}

func (m *MockAuthProvider) ValidateSessionToken(ctx context.Context, token string) (*domainAuth.SessionClaims, error) {
    return &domainAuth.SessionClaims{}, nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/testutil/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/testutil/mock_auth_provider.go
git commit -m "test(testutil): add MockAuthProvider for service-level auth provider testing"
```

---

## Task 6: Implement `RemoveUser` service method

**Files:**
- Modify: `internal/service/user.go`
- Modify: `internal/service/user_test.go`

- [ ] **Step 1: Write the failing tests**

Add the following test suite method to `UserServiceSuite` in `internal/service/user_test.go`:

```go
func (s *UserServiceSuite) TestRemoveUser() {
    tests := []struct {
        name        string
        setup       func(ctx context.Context) *userService
        targetID    string
        callerID    string
        wantErr     bool
        errContains string
    }{
        {
            name: "user_not_found",
            setup: func(ctx context.Context) *userService {
                return &userService{
                    userRepo:   s.userRepo,
                    tenantRepo: s.tenantRepo,
                    provider:   &testutil.MockAuthProvider{},
                }
            },
            targetID:    "nonexistent",
            callerID:    "nonexistent",
            wantErr:     true,
            errContains: "not found",
        },
        {
            name: "last_user_cannot_remove_themselves",
            setup: func(ctx context.Context) *userService {
                _ = s.userRepo.Create(ctx, &user.User{
                    ID:        "only-user",
                    Email:     "only@example.com",
                    BaseModel: types.GetDefaultBaseModel(ctx),
                })
                return &userService{
                    userRepo:   s.userRepo,
                    tenantRepo: s.tenantRepo,
                    provider:   &testutil.MockAuthProvider{},
                }
            },
            targetID:    "only-user",
            callerID:    "only-user",
            wantErr:     true,
            errContains: "cannot remove the only remaining user",
        },
        {
            name: "success_removes_user",
            setup: func(ctx context.Context) *userService {
                _ = s.userRepo.Create(ctx, &user.User{
                    ID:        "user-a",
                    Email:     "a@example.com",
                    BaseModel: types.GetDefaultBaseModel(ctx),
                })
                _ = s.userRepo.Create(ctx, &user.User{
                    ID:        "user-b",
                    Email:     "b@example.com",
                    BaseModel: types.GetDefaultBaseModel(ctx),
                })
                return &userService{
                    userRepo:   s.userRepo,
                    tenantRepo: s.tenantRepo,
                    provider:   &testutil.MockAuthProvider{},
                }
            },
            targetID: "user-a",
            callerID: "user-b",
            wantErr:  false,
        },
    }

    for _, tt := range tests {
        s.Run(tt.name, func() {
            s.userRepo = testutil.NewInMemoryUserStore()
            s.tenantRepo = testutil.NewInMemoryTenantStore()
            ctx := testutil.SetupContext()
            ctx = context.WithValue(ctx, types.CtxUserID, tt.callerID)
            _ = s.tenantRepo.Create(ctx, &tenant.Tenant{ID: types.DefaultTenantID, Name: "T"})
            svc := tt.setup(ctx)

            err := svc.RemoveUser(ctx, tt.targetID)

            if tt.wantErr {
                s.Error(err)
                if tt.errContains != "" {
                    s.Contains(err.Error(), tt.errContains)
                }
            } else {
                s.NoError(err)
                // verify user is soft-deleted (not visible in published list)
                _, count, listErr := s.userRepo.ListByFilter(ctx, &types.UserFilter{
                    QueryFilter: &types.QueryFilter{
                        Limit:  lo.ToPtr(uint64(10)),
                        Status: lo.ToPtr(types.StatusPublished),
                    },
                })
                s.NoError(listErr)
                // only user-b remains published
                s.Equal(int64(1), count)
            }
        })
    }
}
```

Add missing imports to `user_test.go` if needed: `"github.com/flexprice/flexprice/internal/testutil"`.

- [ ] **Step 2: Run the tests — verify they fail**

```bash
go test -v -run TestUserService/TestRemoveUser ./internal/service/...
```

Expected: compile error — `RemoveUser` undefined, `provider` field undefined.

- [ ] **Step 3: Add `provider` field to `userService` and `getProvider()` helper**

In `internal/service/user.go`, add the `provider` field to the struct:

```go
type userService struct {
    userRepo        user.Repository
    tenantRepo      tenant.Repository
    authRepo        domainAuth.Repository
    cfg             *config.Configuration
    rbacService     *rbac.RBACService
    supabaseAuth    *supabase.Client
    settingsService SettingsService
    logger          *logger.Logger
    // provider is set only in tests; production code calls NewProvider(cfg) via getProvider().
    provider authProvider.Provider
}
```

Add a `getProvider()` helper right after the struct, before any method:

```go
func (s *userService) getProvider() authProvider.Provider {
    if s.provider != nil {
        return s.provider
    }
    return authProvider.NewProvider(s.cfg)
}
```

- [ ] **Step 4: Add `RemoveUser` to the `UserService` interface**

In `internal/service/user.go`, extend the interface:

```go
type UserService interface {
    GetUserInfo(ctx context.Context) (*dto.UserResponse, error)
    CreateUser(ctx context.Context, req *dto.CreateUserRequest) (*dto.CreateUserResponse, error)
    ListUsersByFilter(ctx context.Context, filter *types.UserFilter) (*dto.ListUsersResponse, error)
    RemoveUser(ctx context.Context, userID string) error
}
```

- [ ] **Step 5: Implement `RemoveUser`**

Append to `internal/service/user.go`:

```go
// RemoveUser soft-deletes a user from the tenant.
// The caller (identified by context user ID) cannot remove themselves if they are the only
// remaining active user in the tenant.
func (s *userService) RemoveUser(ctx context.Context, userID string) error {
    // Verify the user exists in this tenant.
    u, err := s.userRepo.GetByID(ctx, userID)
    if err != nil {
        return err
    }

    // Guard: prevent removing the last user.
    callerID := types.GetUserID(ctx)
    if callerID == userID {
        _, total, err := s.userRepo.ListByFilter(ctx, &types.UserFilter{
            QueryFilter: &types.QueryFilter{
                Limit:  lo.ToPtr(uint64(1)),
                Offset: lo.ToPtr(uint64(0)),
                Status: lo.ToPtr(types.StatusPublished),
            },
        })
        if err != nil {
            return err
        }
        if total <= 1 {
            return ierr.NewError("cannot remove the only remaining user from a tenant").
                WithHint("At least one active user must remain in the tenant.").
                Mark(ierr.ErrValidation)
        }
    }

    // Clear tenant association in the auth provider first.
    provider := s.getProvider()
    if err := provider.RemoveUserFromTenant(ctx, userID); err != nil {
        return err
    }

    // Soft-delete in the database.
    u.Status = types.StatusDeleted
    u.UpdatedBy = callerID
    u.UpdatedAt = time.Now()
    return s.userRepo.Update(ctx, u)
}
```

Add `"time"` to the imports in `user.go` if not already present.

- [ ] **Step 6: Run the tests — verify they pass**

```bash
go test -v -run TestUserService/TestRemoveUser ./internal/service/...
```

Expected: all 3 sub-tests PASS.

- [ ] **Step 7: Full build check**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/service/user.go internal/service/user_test.go
git commit -m "feat(service): add RemoveUser method with last-user guard and auth provider cleanup"
```

---

## Task 7: Update `InviteUser` with provider-level email check

**Files:**
- Modify: `internal/service/user.go`
- Modify: `internal/service/user_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `UserServiceSuite` in `internal/service/user_test.go`:

```go
func (s *UserServiceSuite) TestInviteUser_ProviderEmailCheck() {
    ctx := testutil.SetupContext()
    ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
    ctx = context.WithValue(ctx, types.CtxUserID, "actor")

    tests := []struct {
        name             string
        providerUser     *authProvider.ProviderUser
        providerErr      error
        wantErr          bool
        errContains      string
        wantPasswordBack bool
    }{
        {
            name:        "user_belongs_to_another_tenant",
            providerUser: &authProvider.ProviderUser{ID: "ext-user-1", TenantID: "other-tenant"},
            wantErr:     true,
            errContains: "already belongs to another tenant",
        },
        {
            name:             "free_user_reinvite_succeeds",
            providerUser:     &authProvider.ProviderUser{ID: "free-user-1", TenantID: ""},
            wantErr:          false,
            wantPasswordBack: true,
        },
        {
            name:             "provider_returns_nil_fresh_create",
            providerUser:     nil,
            wantErr:          true, // still fails at settings service check (nil settingsService)
            errContains:      "settings service not configured",
        },
        {
            name:        "provider_error_propagates",
            providerErr: errors.New("supabase timeout"),
            wantErr:     true,
            errContains: "supabase timeout",
        },
    }

    for _, tt := range tests {
        s.Run(tt.name, func() {
            s.userRepo = testutil.NewInMemoryUserStore()
            s.tenantRepo = testutil.NewInMemoryTenantStore()
            _ = s.tenantRepo.Create(ctx, &tenant.Tenant{ID: types.DefaultTenantID, Name: "T"})

            mock := &testutil.MockAuthProvider{
                GetUserByEmailFn: func(_ context.Context, _ string) (*authProvider.ProviderUser, error) {
                    return tt.providerUser, tt.providerErr
                },
            }
            svc := &userService{
                userRepo:        s.userRepo,
                tenantRepo:      s.tenantRepo,
                settingsService: nil,
                provider:        mock,
            }

            result, err := svc.CreateUser(ctx, &dto.CreateUserRequest{
                Type:  types.UserTypeUser,
                Email: "invite@example.com",
            })

            if tt.wantErr {
                s.Error(err)
                if tt.errContains != "" {
                    s.Contains(err.Error(), tt.errContains)
                }
            } else {
                s.NoError(err)
                s.NotNil(result)
                if tt.wantPasswordBack {
                    s.NotEmpty(result.Password)
                }
            }
        })
    }
}
```

Add import `"errors"` to the test file imports.
Add import `authProvider "github.com/flexprice/flexprice/internal/auth"` to test file imports.

- [ ] **Step 2: Run tests — verify they fail**

```bash
go test -v -run TestUserService/TestInviteUser_ProviderEmailCheck ./internal/service/...
```

Expected: most cases pass except `free_user_reinvite_succeeds` (not yet implemented) and any compile issues.

- [ ] **Step 3: Update `InviteUser` in `internal/service/user.go`**

Replace the body of `InviteUser` with the following. The existing check for duplicate email within the tenant is kept. The provider check is inserted **after** the user-limit check (so limit applies to both fresh and re-invite paths) and **before** `provider.UserInvite`:

```go
func (s *userService) InviteUser(ctx context.Context, req *dto.CreateUserRequest, currentUserID string) (*user.User, *string, error) {
    var userID string

    // Reject if email already active in this tenant.
    existingUser, err := s.userRepo.GetByEmail(ctx, req.Email)
    if err != nil && !ierr.IsNotFound(err) {
        return nil, nil, err
    }
    if existingUser != nil {
        return nil, nil, ierr.NewError("email already in use").
            WithHint("A user with this email already exists in this tenant").
            WithReportableDetails(map[string]interface{}{"email": req.Email}).
            Mark(ierr.ErrAlreadyExists)
    }

    // Enforce per-tenant user limit.
    svc, ok := s.settingsService.(*settingsService)
    if !ok || svc == nil {
        return nil, nil, ierr.NewError("settings service not configured").
            WithHint("User creation requires settings service for add_user_config.").
            Mark(ierr.ErrValidation)
    }
    addUserConfig, err := GetSetting[types.TenantConfig](svc, ctx, types.SettingKeyTenantConfig)
    if err != nil {
        return nil, nil, err
    }
    _, totalActiveUsers, err := s.userRepo.ListByFilter(ctx, &types.UserFilter{
        QueryFilter: &types.QueryFilter{
            Limit:  lo.ToPtr(uint64(1)),
            Offset: lo.ToPtr(uint64(0)),
            Status: lo.ToPtr(types.StatusPublished),
        },
    })
    if err != nil {
        return nil, nil, err
    }
    if totalActiveUsers >= int64(addUserConfig.MaxUsers) {
        return nil, nil, ierr.NewError("user limit reached: you cannot add any more users").
            WithHintf("Maximum %d user(s) allowed for this tenant. Limit reached.", addUserConfig.MaxUsers).
            WithReportableDetails(map[string]interface{}{"max_users": addUserConfig.MaxUsers, "current_active_users": totalActiveUsers}).
            Mark(ierr.ErrValidation)
    }

    if s.cfg == nil && s.provider == nil {
        return nil, nil, ierr.NewError("auth configuration missing").
            WithHint("User creation requires auth provider configuration").
            Mark(ierr.ErrValidation)
    }

    provider := s.getProvider()

    // Check if the user already exists in the auth provider.
    providerUser, err := provider.GetUserByEmail(ctx, req.Email)
    if err != nil {
        return nil, nil, err
    }

    if providerUser != nil {
        if providerUser.TenantID != "" {
            return nil, nil, ierr.NewError("user already belongs to another tenant").
                WithHint("This email address is associated with a user in a different tenant.").
                WithReportableDetails(map[string]interface{}{"email": req.Email}).
                Mark(ierr.ErrValidation)
        }

        // Free user: re-invite path — reset password, assign to this tenant, create a new DB record.
        tenantID := types.GetTenantID(ctx)
        newPassword, err := generatePassword()
        if err != nil {
            return nil, nil, err
        }
        if err := provider.ResetUserPassword(ctx, providerUser.ID, newPassword); err != nil {
            return nil, nil, err
        }
        if err := provider.AssignUserToTenant(ctx, providerUser.ID, tenantID); err != nil {
            return nil, nil, err
        }
        newUser := &user.User{
            ID:    providerUser.ID,
            Email: req.Email,
            Type:  types.UserTypeUser,
            Roles: []string{},
        }
        newUser.BaseModel = types.GetDefaultBaseModel(ctx)
        newUser.BaseModel.CreatedBy = currentUserID
        newUser.BaseModel.UpdatedBy = currentUserID
        if err := newUser.Validate(); err != nil {
            return nil, nil, err
        }
        if err := s.userRepo.Create(ctx, newUser); err != nil {
            return nil, nil, err
        }
        return newUser, &newPassword, nil
    }

    // Fresh-create path (user does not exist in auth provider).
    inviteResp, err := provider.UserInvite(ctx, authProvider.UserInviteRequest{
        Email: req.Email,
    })
    if err != nil {
        return nil, nil, err
    }
    userID = inviteResp.ID
    password := inviteResp.Password

    if inviteResp.AuthRecord != nil {
        if s.authRepo == nil {
            return nil, nil, ierr.NewError("auth repository not configured").
                WithHint("Auth provider returned an auth record but auth repository is nil").
                Mark(ierr.ErrValidation)
        }
        if err := s.authRepo.CreateAuth(ctx, inviteResp.AuthRecord); err != nil {
            return nil, nil, err
        }
    }

    newUser := &user.User{
        ID:    userID,
        Email: req.Email,
        Type:  types.UserTypeUser,
        Roles: []string{},
    }
    newUser.BaseModel = types.GetDefaultBaseModel(ctx)
    newUser.BaseModel.CreatedBy = currentUserID
    newUser.BaseModel.UpdatedBy = currentUserID

    if err := newUser.Validate(); err != nil {
        return nil, nil, err
    }
    if err := s.userRepo.Create(ctx, newUser); err != nil {
        return nil, nil, err
    }
    return newUser, &password, nil
}
```

Add a private helper to avoid importing the password package directly in user.go (it's already used in auth providers, but service layer should not import it directly — use the provider's `UserInvite` for fresh creates):

Actually, the re-invite path needs to generate a password to pass to `ResetUserPassword`. Add this helper at the bottom of `internal/service/user.go`:

```go
func generatePassword() (string, error) {
    p, err := gopassword.Generate(16, 4, 2, false, false)
    if err != nil {
        return "", ierr.WithError(err).
            WithHint("Failed to generate password").
            Mark(ierr.ErrSystem)
    }
    return p, nil
}
```

And add the import alias at the top of user.go:

```go
gopassword "github.com/sethvargo/go-password/password"
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
go test -v -run TestUserService/TestInviteUser_ProviderEmailCheck ./internal/service/...
```

Expected: all sub-tests PASS (including `free_user_reinvite_succeeds`).

- [ ] **Step 5: Run all user service tests to check no regressions**

```bash
go test -v ./internal/service/ -run TestUserService
```

Expected: all tests PASS.

- [ ] **Step 6: Full build check**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/service/user.go internal/service/user_test.go
git commit -m "feat(service): update InviteUser with provider email check and re-invite path for free users"
```

---

## Task 8: Add HTTP handler and register route

**Files:**
- Modify: `internal/api/v1/user.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add the `RemoveUser` handler to `internal/api/v1/user.go`**

Append to `internal/api/v1/user.go`:

```go
// @Summary Remove a user from the tenant
// @ID removeUser
// @Description Soft-deletes a user from the current tenant. The caller cannot remove themselves if they are the only remaining active user.
// @Tags Users
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Success 204 "No Content"
// @Failure 400 {object} ierr.ErrorResponse "Cannot remove last user"
// @Failure 404 {object} ierr.ErrorResponse "User not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /users/{id} [delete]
func (h *UserHandler) RemoveUser(c *gin.Context) {
    userID := c.Param("id")
    if userID == "" {
        c.Error(ierr.NewError("user ID is required").
            WithHint("Provide the user ID in the URL path").
            Mark(ierr.ErrValidation))
        return
    }

    if err := h.userService.RemoveUser(c.Request.Context(), userID); err != nil {
        h.logger.Errorw("failed to remove user", "user_id", userID, "error", err)
        c.Error(err)
        return
    }

    c.Status(http.StatusNoContent)
}
```

- [ ] **Step 2: Register the route in `internal/api/router.go`**

Find the user routes block (around line 137–139):

```go
user.GET("/me", handlers.User.GetUserInfo)
user.POST("", write("user", types.ActionWrite), handlers.User.CreateUser)
user.POST("/search", handlers.User.QueryUsers)
```

Add the DELETE route after the existing lines:

```go
user.GET("/me", handlers.User.GetUserInfo)
user.POST("", write("user", types.ActionWrite), handlers.User.CreateUser)
user.POST("/search", handlers.User.QueryUsers)
user.DELETE("/:id", write("user", types.ActionWrite), handlers.User.RemoveUser)
```

- [ ] **Step 3: Full build check**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Run all tests**

```bash
go test ./internal/... -count=1
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/v1/user.go internal/api/router.go
git commit -m "feat(api): add DELETE /users/:id endpoint for removing users from tenant"
```

---

## Self-Review Checklist (completed)

**Spec coverage:**
- [x] `RemoveUserFromTenant`, `GetUserByEmail`, `ResetUserPassword` added to Provider — Tasks 1–3
- [x] Flexprice: all no-ops — Task 2
- [x] Supabase: direct HTTP for `GetUserByEmail`, `UpdateUser` for others — Task 3
- [x] `Update` in user repository + in-memory store — Task 4
- [x] `RemoveUser` service with last-user guard — Task 6
- [x] `InviteUser` updated with provider check + re-invite path — Task 7
- [x] `DELETE /users/:id` handler + route — Task 8
- [x] Error cases: not found (404), last user (400), user in another tenant (400), provider error (500) — Tasks 6–8

**Type consistency:** `ProviderUser` defined in Task 1, used in Tasks 3, 5, 6, 7 — all consistent. `Update(ctx, *User) error` defined in Task 4, used in Task 6 — consistent.

**No placeholders:** All steps contain concrete code or commands.
