package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cerrors "github.com/cockroachdb/errors"
	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/nedpals/supabase-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingUserStore fails every create, like a database error while saving the user.
type failingUserStore struct {
	*testutil.InMemoryUserStore
}

func (failingUserStore) Create(context.Context, *user.User) error {
	return ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

// failingArchiveStore fails every archive, like a database error while removing the user.
type failingArchiveStore struct {
	*testutil.InMemoryUserStore
}

func (failingArchiveStore) Delete(context.Context, string) error {
	return ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

func TestAddUser(t *testing.T) {
	ctx := context.Background()
	const tenantID = "tenant_acme"
	teammate := admindto.AddUserRequest{TenantID: tenantID, Email: "teammate@acme.com", Password: "chosen-by-operator"}

	withTenant := func(t *testing.T) *tenantTestDeps {
		t.Helper()
		d := newTenantTestDeps(t)
		require.NoError(t, d.tenants.Create(ctx, &tenant.Tenant{ID: tenantID, Name: "Acme"}))
		return d
	}

	t.Run("adds the user as all_reader when no role is given", func(t *testing.T) {
		d := withTenant(t)

		resp, err := NewUserService(d.params).AddUser(ctx, teammate)
		require.NoError(t, err)

		stored, err := d.users.GetByEmail(ctx, "teammate@acme.com")
		require.NoError(t, err)
		assert.Equal(t, resp.UserID, stored.ID)
		assert.Equal(t, tenantID, stored.TenantID)
		assert.Equal(t, types.UserTypeUser, stored.Type)
		assert.Equal(t, []string{"all_reader"}, stored.Roles)
		assert.Equal(t, types.StatusPublished, stored.Status)

		assert.Equal(t, tenantID, resp.TenantID)
		assert.Equal(t, "teammate@acme.com", resp.Email)
		assert.Equal(t, []string{"all_reader"}, resp.Roles)
	})

	t.Run("gives the user the role asked for", func(t *testing.T) {
		d := withTenant(t)

		req := teammate
		req.Role = types.RoleAllWriter
		resp, err := NewUserService(d.params).AddUser(ctx, req)
		require.NoError(t, err)

		stored, err := d.users.GetByEmail(ctx, "teammate@acme.com")
		require.NoError(t, err)
		assert.Equal(t, []string{"all_writer"}, stored.Roles)
		assert.Equal(t, []string{"all_writer"}, resp.Roles)
	})

	t.Run("creates a confirmed supabase user for the tenant with the given password", func(t *testing.T) {
		d := withTenant(t)

		resp, err := NewUserService(d.params).AddUser(ctx, teammate)
		require.NoError(t, err)

		login, ok := d.supabase.user(resp.UserID)
		require.True(t, ok, "the user id must be the supabase user id")
		assert.Equal(t, "teammate@acme.com", login.Email)
		require.NotNil(t, login.Password)
		assert.Equal(t, "chosen-by-operator", *login.Password)
		assert.True(t, login.EmailConfirm)
		assert.Equal(t, tenantID, login.AppMetadata["tenant_id"])
	})

	t.Run("refuses a tenant that does not exist", func(t *testing.T) {
		d := newTenantTestDeps(t)

		_, err := NewUserService(d.params).AddUser(ctx, teammate)
		require.Error(t, err)

		created, _ := d.supabase.counts()
		assert.Zero(t, created, "no supabase user may be created for an unknown tenant")
		_, err = d.users.GetByEmail(ctx, "teammate@acme.com")
		assert.True(t, ierr.IsNotFound(err))
	})

	t.Run("refuses an email that already has a user", func(t *testing.T) {
		d := withTenant(t)
		require.NoError(t, d.users.Create(ctx, user.NewUser("teammate@acme.com", "tenant_other")))

		_, err := NewUserService(d.params).AddUser(ctx, teammate)
		require.Error(t, err)
		assert.True(t, ierr.IsAlreadyExists(err))

		created, _ := d.supabase.counts()
		assert.Zero(t, created, "no supabase user may be created for a taken email")
	})

	t.Run("refuses an email supabase already has", func(t *testing.T) {
		d := withTenant(t)
		d.supabase.createCode = http.StatusUnprocessableEntity
		d.supabase.createMsg = "A user with this email address has already been registered"

		_, err := NewUserService(d.params).AddUser(ctx, teammate)
		require.Error(t, err)
		assert.True(t, ierr.IsAlreadyExists(err))

		_, err = d.users.GetByEmail(ctx, "teammate@acme.com")
		assert.True(t, ierr.IsNotFound(err))
	})

	t.Run("reports a password supabase rejects as bad input, not a duplicate", func(t *testing.T) {
		d := withTenant(t)
		d.supabase.createCode = http.StatusUnprocessableEntity
		d.supabase.createMsg = "Password should contain at least one character of each: abcdefghijklmnopqrstuvwxyz, ABCDEFGHIJKLMNOPQRSTUVWXYZ, 0123456789"

		_, err := NewUserService(d.params).AddUser(ctx, teammate)
		require.Error(t, err)
		assert.True(t, ierr.IsValidation(err))
		assert.False(t, ierr.IsAlreadyExists(err))
		assert.Contains(t, cerrors.FlattenHints(err), "Password should contain", "the operator must see why Supabase refused")

		_, err = d.users.GetByEmail(ctx, "teammate@acme.com")
		assert.True(t, ierr.IsNotFound(err))
	})

	t.Run("removes the supabase user when saving fails", func(t *testing.T) {
		d := withTenant(t)
		d.params.UserRepo = failingUserStore{d.users}

		_, err := NewUserService(d.params).AddUser(ctx, teammate)
		require.Error(t, err)

		created, remaining := d.supabase.counts()
		assert.Equal(t, 1, created)
		assert.Zero(t, remaining, "a supabase user without a row would block retrying this email")
	})

	t.Run("refuses to run without supabase auth", func(t *testing.T) {
		d := withTenant(t)
		d.params.Config.Auth.Provider = types.AuthProviderFlexprice

		_, err := NewUserService(d.params).AddUser(ctx, teammate)
		require.Error(t, err)
		assert.True(t, ierr.IsInvalidOperation(err))

		created, _ := d.supabase.counts()
		assert.Zero(t, created)
	})
}

func TestAddUserValidation(t *testing.T) {
	tests := []struct {
		name string
		req  admindto.AddUserRequest
	}{
		{name: "missing tenant id", req: admindto.AddUserRequest{Email: "teammate@acme.com", Password: "chosen-by-operator"}},
		{name: "blank tenant id", req: admindto.AddUserRequest{TenantID: "   ", Email: "teammate@acme.com", Password: "chosen-by-operator"}},
		{name: "missing email", req: admindto.AddUserRequest{TenantID: "tenant_acme", Password: "chosen-by-operator"}},
		{name: "malformed email", req: admindto.AddUserRequest{TenantID: "tenant_acme", Email: "teammate-at-acme", Password: "chosen-by-operator"}},
		{name: "password under 8 characters", req: admindto.AddUserRequest{TenantID: "tenant_acme", Email: "teammate@acme.com", Password: "1234567"}},
		{name: "unknown role", req: admindto.AddUserRequest{TenantID: "tenant_acme", Email: "teammate@acme.com", Password: "chosen-by-operator", Role: "owner"}},
		{name: "role only service accounts can hold", req: admindto.AddUserRequest{TenantID: "tenant_acme", Email: "teammate@acme.com", Password: "chosen-by-operator", Role: "event_ingestor"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTenantTestDeps(t)

			resp, err := NewUserService(d.params).AddUser(context.Background(), tt.req)
			require.Error(t, err)
			assert.True(t, ierr.IsValidation(err))
			assert.Nil(t, resp)

			created, _ := d.supabase.counts()
			assert.Zero(t, created)
		})
	}
}

// seedLogin gives the fake Supabase a login for an existing user.
func seedLogin(f *fakeSupabase, userID, email string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[userID] = supabase.AdminUserParams{Email: email}
}

// failingDeleteSupabase finds every user but fails every delete, like a Supabase outage mid-removal.
func failingDeleteSupabase(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": http.StatusInternalServerError, "msg": "boom"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": strings.TrimPrefix(r.URL.Path, supabaseUsersPath+"/")})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestRemoveUser(t *testing.T) {
	ctx := context.Background()
	const tenantID = "tenant_acme"
	removeTeammate := admindto.RemoveUserRequest{TenantID: tenantID, Email: "teammate@acme.com"}

	// withMembers gives the tenant a user with a Supabase login for each email.
	withMembers := func(t *testing.T, emails ...string) *tenantTestDeps {
		t.Helper()
		d := newTenantTestDeps(t)
		for _, email := range emails {
			u := user.NewUser(email, tenantID)
			require.NoError(t, d.users.Create(ctx, u))
			seedLogin(d.supabase, u.ID, email)
		}
		return d
	}

	// state reports a user's row status and whether Supabase still has their login.
	state := func(t *testing.T, d *tenantTestDeps, email string) (types.Status, bool) {
		t.Helper()
		u, err := d.users.GetByEmail(ctx, email)
		require.NoError(t, err)
		_, hasLogin := d.supabase.user(u.ID)
		return u.Status, hasLogin
	}

	t.Run("archives the user and deletes their supabase login", func(t *testing.T) {
		d := withMembers(t, "owner@acme.com", "teammate@acme.com")
		teammate, err := d.users.GetByEmail(ctx, "teammate@acme.com")
		require.NoError(t, err)

		resp, err := NewUserService(d.params).RemoveUser(ctx, removeTeammate)
		require.NoError(t, err)
		assert.Equal(t, &admindto.RemoveUserResponse{
			UserID:   teammate.ID,
			Email:    "teammate@acme.com",
			TenantID: tenantID,
			Status:   types.StatusArchived,
		}, resp)

		status, hasLogin := state(t, d, "teammate@acme.com")
		assert.Equal(t, types.StatusArchived, status)
		assert.False(t, hasLogin)

		status, hasLogin = state(t, d, "owner@acme.com")
		assert.Equal(t, types.StatusPublished, status, "other users must stay")
		assert.True(t, hasLogin)
	})

	t.Run("refuses an email with no user", func(t *testing.T) {
		d := withMembers(t, "owner@acme.com", "teammate@acme.com")

		_, err := NewUserService(d.params).RemoveUser(ctx, admindto.RemoveUserRequest{TenantID: tenantID, Email: "ghost@acme.com"})
		require.Error(t, err)
		assert.True(t, ierr.IsNotFound(err))

		status, hasLogin := state(t, d, "teammate@acme.com")
		assert.Equal(t, types.StatusPublished, status)
		assert.True(t, hasLogin)
	})

	t.Run("refuses a user of another tenant", func(t *testing.T) {
		d := withMembers(t, "owner@acme.com", "teammate@acme.com")
		outsider := user.NewUser("outsider@other.com", "tenant_other")
		require.NoError(t, d.users.Create(ctx, outsider))
		seedLogin(d.supabase, outsider.ID, "outsider@other.com")

		_, err := NewUserService(d.params).RemoveUser(ctx, admindto.RemoveUserRequest{TenantID: tenantID, Email: "outsider@other.com"})
		require.Error(t, err)
		assert.True(t, ierr.IsNotFound(err))

		status, hasLogin := state(t, d, "outsider@other.com")
		assert.Equal(t, types.StatusPublished, status, "a user of another tenant must not be touched")
		assert.True(t, hasLogin)
	})

	t.Run("refuses a service account", func(t *testing.T) {
		d := withMembers(t, "owner@acme.com", "teammate@acme.com")
		bot := &user.User{
			ID:        "user_bot",
			Email:     "bot@acme.com",
			Type:      types.UserTypeServiceAccount,
			BaseModel: types.BaseModel{TenantID: tenantID, Status: types.StatusPublished},
		}
		require.NoError(t, d.users.Create(ctx, bot))

		_, err := NewUserService(d.params).RemoveUser(ctx, admindto.RemoveUserRequest{TenantID: tenantID, Email: "bot@acme.com"})
		require.Error(t, err)
		assert.True(t, ierr.IsValidation(err))

		stored, err := d.users.GetByEmail(ctx, "bot@acme.com")
		require.NoError(t, err)
		assert.Equal(t, types.StatusPublished, stored.Status, "a service account must not be archived here")
	})

	t.Run("refuses to remove the tenant's last user", func(t *testing.T) {
		d := withMembers(t, "teammate@acme.com")

		_, err := NewUserService(d.params).RemoveUser(ctx, removeTeammate)
		require.Error(t, err)
		assert.True(t, ierr.IsValidation(err))

		status, hasLogin := state(t, d, "teammate@acme.com")
		assert.Equal(t, types.StatusPublished, status)
		assert.True(t, hasLogin)
	})

	t.Run("keeps the supabase login when archiving fails", func(t *testing.T) {
		d := withMembers(t, "owner@acme.com", "teammate@acme.com")
		d.params.UserRepo = failingArchiveStore{d.users}

		_, err := NewUserService(d.params).RemoveUser(ctx, removeTeammate)
		require.Error(t, err)

		status, hasLogin := state(t, d, "teammate@acme.com")
		assert.Equal(t, types.StatusPublished, status)
		assert.True(t, hasLogin, "a login deleted before the archive cannot be restored")
	})

	t.Run("fails the removal when supabase cannot delete the login", func(t *testing.T) {
		d := withMembers(t, "owner@acme.com", "teammate@acme.com")
		d.params.Config.Auth.Supabase.BaseURL = failingDeleteSupabase(t)

		// The error makes the real transaction roll back the archive; the test database does not roll back.
		_, err := NewUserService(d.params).RemoveUser(ctx, removeTeammate)
		require.Error(t, err)
	})

	t.Run("refuses to run without supabase auth", func(t *testing.T) {
		d := withMembers(t, "owner@acme.com", "teammate@acme.com")
		d.params.Config.Auth.Provider = types.AuthProviderFlexprice

		_, err := NewUserService(d.params).RemoveUser(ctx, removeTeammate)
		require.Error(t, err)
		assert.True(t, ierr.IsInvalidOperation(err))

		status, hasLogin := state(t, d, "teammate@acme.com")
		assert.Equal(t, types.StatusPublished, status)
		assert.True(t, hasLogin)
	})
}

func TestRemoveUserValidation(t *testing.T) {
	tests := []struct {
		name string
		req  admindto.RemoveUserRequest
	}{
		{name: "missing tenant id", req: admindto.RemoveUserRequest{Email: "teammate@acme.com"}},
		{name: "missing email", req: admindto.RemoveUserRequest{TenantID: "tenant_acme"}},
		{name: "malformed email", req: admindto.RemoveUserRequest{TenantID: "tenant_acme", Email: "teammate-at-acme"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTenantTestDeps(t)

			resp, err := NewUserService(d.params).RemoveUser(context.Background(), tt.req)
			require.Error(t, err)
			assert.True(t, ierr.IsValidation(err))
			assert.Nil(t, resp)
		})
	}
}
