package admin

import (
	"context"
	"net/http"
	"testing"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
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
