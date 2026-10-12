package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	cerrors "github.com/cockroachdb/errors"
	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/nedpals/supabase-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const supabaseUsersPath = "/auth/v1/admin/users"

// fakeSupabase serves the Supabase admin user endpoints the service calls.
type fakeSupabase struct {
	mu         sync.Mutex
	users      map[string]supabase.AdminUserParams
	created    int
	createCode int
	createMsg  string
}

func (f *fakeSupabase) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost && r.URL.Path == supabaseUsersPath {
		if f.createCode != 0 {
			w.WriteHeader(f.createCode)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": f.createCode, "msg": f.createMsg})
			return
		}
		var params supabase.AdminUserParams
		_ = json.NewDecoder(r.Body).Decode(&params)
		f.created++
		id := fmt.Sprintf("supabase_user_%d", f.created)
		f.users[id] = params
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "email": params.Email})
		return
	}

	id := strings.TrimPrefix(r.URL.Path, supabaseUsersPath+"/")
	if _, ok := f.users[id]; !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": http.StatusNotFound, "msg": "User not found"})
		return
	}
	if r.Method == http.MethodDelete {
		delete(f.users, id)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
}

func (f *fakeSupabase) user(id string) (supabase.AdminUserParams, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	return u, ok
}

func (f *fakeSupabase) counts() (created, remaining int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created, len(f.users)
}

// failingTenantStore fails every create, like a database error while saving the tenant.
type failingTenantStore struct {
	*testutil.InMemoryTenantStore
}

func (failingTenantStore) Create(context.Context, *tenant.Tenant) error {
	return ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

// failingEnvironmentStore fails every create, like a database error partway through the transaction.
type failingEnvironmentStore struct {
	*testutil.InMemoryEnvironmentStore
}

func (failingEnvironmentStore) Create(context.Context, *environment.Environment) error {
	return ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

type tenantTestDeps struct {
	supabase     *fakeSupabase
	users        *testutil.InMemoryUserStore
	tenants      *testutil.InMemoryTenantStore
	environments *testutil.InMemoryEnvironmentStore
	params       service.ServiceParams
}

func newTenantTestDeps(t *testing.T) *tenantTestDeps {
	t.Helper()
	fake := &fakeSupabase{users: map[string]supabase.AdminUserParams{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	cfg := &config.Configuration{}
	cfg.Auth.Provider = types.AuthProviderSupabase
	cfg.Auth.Supabase.BaseURL = srv.URL
	cfg.Auth.Supabase.ServiceKey = "service-key"

	log := logger.NewNoopLogger()
	d := &tenantTestDeps{
		supabase:     fake,
		users:        testutil.NewInMemoryUserStore(),
		tenants:      testutil.NewInMemoryTenantStore(),
		environments: testutil.NewInMemoryEnvironmentStore(),
	}
	d.params = service.ServiceParams{
		Config:          cfg,
		Logger:          log,
		DB:              testutil.NewMockPostgresClient(log),
		UserRepo:        d.users,
		TenantRepo:      d.tenants,
		EnvironmentRepo: d.environments,
	}
	return d
}

func (d *tenantTestDeps) storedEnvironments(t *testing.T) []*environment.Environment {
	t.Helper()
	envs, err := d.environments.List(context.Background(), types.Filter{Limit: 100})
	require.NoError(t, err)
	return envs
}

func (d *tenantTestDeps) storedTenantCount(t *testing.T) int {
	t.Helper()
	tenants, err := d.tenants.List(context.Background())
	require.NoError(t, err)
	return len(tenants)
}

const (
	billingTenantID      = "tenant_billing"
	billingEnvironmentID = "env_billing"
)

// tenantBilling holds the billing tenant's stores, where every new tenant becomes a customer.
type tenantBilling struct {
	customers *testutil.InMemoryCustomerStore
	webhooks  *testutil.InMemoryWebhookPublisher
}

// withBilling configures a billing tenant, as the hosted cloud does, so new tenants become billing customers.
func (d *tenantTestDeps) withBilling() *tenantBilling {
	b := &tenantBilling{
		customers: testutil.NewInMemoryCustomerStore(),
		webhooks:  testutil.NewInMemoryWebhookPublisher(),
	}
	d.params.Config.Billing.TenantID = billingTenantID
	d.params.Config.Billing.EnvironmentID = billingEnvironmentID
	d.params.CustomerRepo = b.customers
	d.params.WebhookPublisher = b.webhooks
	d.params.TaxAssociationRepo = testutil.NewInMemoryTaxAssociationStore()
	d.params.SettingsRepo = testutil.NewInMemorySettingsStore()
	d.params.PlanRepo = testutil.NewInMemoryPlanStore()
	return b
}

// failingCustomerStore fails every create, like a database error while making the billing customer.
type failingCustomerStore struct {
	*testutil.InMemoryCustomerStore
}

func (failingCustomerStore) Create(context.Context, *customer.Customer) error {
	return ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

func TestCreateTenant(t *testing.T) {
	ctx := context.Background()
	acme := admindto.CreateTenantRequest{TenantName: "Acme", Email: "owner@acme.com", Password: "chosen-by-operator"}

	// saveFailures each break one of the saves made inside the create transaction.
	saveFailures := []struct {
		name string
		fail func(d *tenantTestDeps)
	}{
		{name: "tenant", fail: func(d *tenantTestDeps) { d.params.TenantRepo = failingTenantStore{d.tenants} }},
		{name: "owner", fail: func(d *tenantTestDeps) { d.params.UserRepo = failingUserStore{d.users} }},
		{name: "environment", fail: func(d *tenantTestDeps) { d.params.EnvironmentRepo = failingEnvironmentStore{d.environments} }},
	}

	t.Run("creates the tenant with a super_admin owner and a sandbox", func(t *testing.T) {
		d := newTenantTestDeps(t)

		resp, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
		require.NoError(t, err)

		stored, err := d.tenants.GetByID(ctx, resp.TenantID)
		require.NoError(t, err)
		assert.Equal(t, "Acme", stored.Name)
		assert.Equal(t, types.StatusPublished, stored.Status)
		assert.Equal(t, types.TenantInternalStatusTrialing, stored.InternalStatus)

		owner, err := d.users.GetByEmail(ctx, "owner@acme.com")
		require.NoError(t, err)
		assert.Equal(t, resp.UserID, owner.ID)
		assert.Equal(t, resp.TenantID, owner.TenantID)
		assert.Equal(t, types.UserTypeUser, owner.Type)
		assert.Equal(t, []string{"super_admin"}, owner.Roles)
		assert.Equal(t, types.StatusPublished, owner.Status)

		envs := d.storedEnvironments(t)
		require.Len(t, envs, 1)
		assert.Equal(t, "Sandbox", envs[0].Name)
		assert.Equal(t, types.EnvironmentDevelopment, envs[0].Type)
		assert.Equal(t, resp.TenantID, envs[0].TenantID)

		require.Len(t, resp.Environments, 1)
		assert.Equal(t, envs[0].ID, resp.Environments[0].ID)
		assert.Equal(t, "Acme", resp.TenantName)
		assert.Equal(t, "owner@acme.com", resp.Email)
	})

	t.Run("creates a confirmed supabase user with the given password", func(t *testing.T) {
		d := newTenantTestDeps(t)

		resp, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
		require.NoError(t, err)

		login, ok := d.supabase.user(resp.UserID)
		require.True(t, ok, "the user id must be the supabase user id")
		assert.Equal(t, "owner@acme.com", login.Email)
		require.NotNil(t, login.Password)
		assert.Equal(t, "chosen-by-operator", *login.Password)
		assert.True(t, login.EmailConfirm)
		assert.Equal(t, resp.TenantID, login.AppMetadata["tenant_id"])
	})

	t.Run("adds production when asked", func(t *testing.T) {
		d := newTenantTestDeps(t)

		req := acme
		req.CreateProduction = true
		resp, err := newTestTenantService(d.params).CreateTenant(ctx, req)
		require.NoError(t, err)

		byType := map[types.EnvironmentType]*environment.Environment{}
		for _, env := range d.storedEnvironments(t) {
			byType[env.Type] = env
		}
		require.Len(t, byType, 2)
		require.NotNil(t, byType[types.EnvironmentDevelopment])
		require.NotNil(t, byType[types.EnvironmentProduction])
		assert.Equal(t, "Sandbox", byType[types.EnvironmentDevelopment].Name)
		assert.Equal(t, "Production", byType[types.EnvironmentProduction].Name)
		assert.Equal(t, resp.TenantID, byType[types.EnvironmentProduction].TenantID)
		assert.Len(t, resp.Environments, 2)
	})

	t.Run("accepts an 8 character password", func(t *testing.T) {
		d := newTenantTestDeps(t)

		req := acme
		req.Password = "12345678"
		_, err := newTestTenantService(d.params).CreateTenant(ctx, req)
		require.NoError(t, err)
	})

	t.Run("stores the email in lowercase", func(t *testing.T) {
		d := newTenantTestDeps(t)

		req := acme
		req.Email = "  Owner@ACME.com "
		resp, err := newTestTenantService(d.params).CreateTenant(ctx, req)
		require.NoError(t, err)

		assert.Equal(t, "owner@acme.com", resp.Email)
		_, err = d.users.GetByEmail(ctx, "owner@acme.com")
		require.NoError(t, err)
		login, ok := d.supabase.user(resp.UserID)
		require.True(t, ok)
		assert.Equal(t, "owner@acme.com", login.Email)
	})

	t.Run("catches an existing email typed in another casing", func(t *testing.T) {
		d := newTenantTestDeps(t)
		require.NoError(t, d.users.Create(ctx, user.NewUser("owner@acme.com", "tenant_existing")))

		req := acme
		req.Email = "OWNER@acme.com"
		_, err := newTestTenantService(d.params).CreateTenant(ctx, req)
		require.Error(t, err)
		assert.True(t, ierr.IsAlreadyExists(err))

		created, _ := d.supabase.counts()
		assert.Zero(t, created, "the duplicate must be caught before Supabase is called")
	})

	t.Run("refuses an email that already has a user", func(t *testing.T) {
		d := newTenantTestDeps(t)
		require.NoError(t, d.users.Create(ctx, user.NewUser("owner@acme.com", "tenant_existing")))

		resp, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
		require.Error(t, err)
		assert.True(t, ierr.IsAlreadyExists(err))
		assert.Nil(t, resp)

		created, _ := d.supabase.counts()
		assert.Zero(t, created, "no supabase user may be created for a taken email")
		assert.Zero(t, d.storedTenantCount(t))
	})

	t.Run("refuses an email supabase already has", func(t *testing.T) {
		d := newTenantTestDeps(t)
		d.supabase.createCode = http.StatusUnprocessableEntity
		d.supabase.createMsg = "A user with this email address has already been registered"

		_, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
		require.Error(t, err)
		assert.True(t, ierr.IsAlreadyExists(err))

		assert.Zero(t, d.storedTenantCount(t))
		_, err = d.users.GetByEmail(ctx, "owner@acme.com")
		assert.True(t, ierr.IsNotFound(err))
		assert.Empty(t, d.storedEnvironments(t))
	})

	t.Run("reports a password supabase rejects as bad input, not a duplicate", func(t *testing.T) {
		d := newTenantTestDeps(t)
		d.supabase.createCode = http.StatusUnprocessableEntity
		d.supabase.createMsg = "Password should contain at least one character of each: abcdefghijklmnopqrstuvwxyz, ABCDEFGHIJKLMNOPQRSTUVWXYZ, 0123456789"

		_, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
		require.Error(t, err)
		assert.True(t, ierr.IsValidation(err))
		assert.False(t, ierr.IsAlreadyExists(err))
		assert.Contains(t, cerrors.FlattenHints(err), "Password should contain", "the operator must see why Supabase refused")

		assert.Zero(t, d.storedTenantCount(t))
	})

	t.Run("removes the supabase user when saving fails", func(t *testing.T) {
		for _, f := range saveFailures {
			t.Run(f.name, func(t *testing.T) {
				d := newTenantTestDeps(t)
				f.fail(d)

				_, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
				require.Error(t, err)
				assert.True(t, ierr.IsDatabase(err))

				created, remaining := d.supabase.counts()
				assert.Equal(t, 1, created)
				assert.Zero(t, remaining, "a supabase user without a tenant would block retrying this email")
			})
		}
	})

	t.Run("sends no billing events when saving fails", func(t *testing.T) {
		for _, f := range saveFailures {
			t.Run(f.name, func(t *testing.T) {
				d := newTenantTestDeps(t)
				billing := d.withBilling()
				f.fail(d)

				_, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
				require.Error(t, err)
				assert.Empty(t, billing.webhooks.Events(), "an event sent before a rollback names a customer that was never saved")
			})
		}
	})

	t.Run("makes the tenant a billing customer", func(t *testing.T) {
		d := newTenantTestDeps(t)
		billing := d.withBilling()

		resp, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
		require.NoError(t, err)

		billingCtx := types.SetEnvironmentID(types.SetTenantID(ctx, billingTenantID), billingEnvironmentID)
		cust, err := billing.customers.GetByLookupKey(billingCtx, resp.TenantID)
		require.NoError(t, err)
		assert.Equal(t, "Acme", cust.Name)

		events := billing.webhooks.Events()
		require.Len(t, events, 1)
		assert.Equal(t, types.WebhookEventCustomerCreated, events[0].EventName)
		assert.Equal(t, cust.ID, events[0].EntityID)
	})

	t.Run("keeps the tenant when the billing customer cannot be made", func(t *testing.T) {
		d := newTenantTestDeps(t)
		billing := d.withBilling()
		d.params.CustomerRepo = failingCustomerStore{billing.customers}

		_, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
		require.NoError(t, err, "the tenant is already saved, so a billing failure must not fail the request")

		assert.Equal(t, 1, d.storedTenantCount(t))
		_, remaining := d.supabase.counts()
		assert.Equal(t, 1, remaining, "the login of a saved tenant must stay")
		assert.Empty(t, billing.webhooks.Events())
	})

	t.Run("returns the save error when the cleanup also fails", func(t *testing.T) {
		d := newTenantTestDeps(t)
		d.params.EnvironmentRepo = failingEnvironmentStore{d.environments}
		d.params.Config.Auth.Supabase.BaseURL = cleanupFailingSupabase(t)

		_, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
		require.Error(t, err)
		assert.True(t, ierr.IsDatabase(err), "a failed cleanup must not replace the original error")
	})

	t.Run("refuses to run without supabase auth", func(t *testing.T) {
		d := newTenantTestDeps(t)
		d.params.Config.Auth.Provider = types.AuthProviderFlexprice

		_, err := newTestTenantService(d.params).CreateTenant(ctx, acme)
		require.Error(t, err)
		assert.True(t, ierr.IsInvalidOperation(err))

		created, _ := d.supabase.counts()
		assert.Zero(t, created)
		assert.Zero(t, d.storedTenantCount(t))
	})
}

func TestCreateTenantValidation(t *testing.T) {
	tests := []struct {
		name string
		req  admindto.CreateTenantRequest
	}{
		{name: "missing tenant name", req: admindto.CreateTenantRequest{Email: "owner@acme.com", Password: "chosen-by-operator"}},
		{name: "blank tenant name", req: admindto.CreateTenantRequest{TenantName: "   ", Email: "owner@acme.com", Password: "chosen-by-operator"}},
		{name: "missing email", req: admindto.CreateTenantRequest{TenantName: "Acme", Password: "chosen-by-operator"}},
		{name: "malformed email", req: admindto.CreateTenantRequest{TenantName: "Acme", Email: "owner-at-acme", Password: "chosen-by-operator"}},
		{name: "missing password", req: admindto.CreateTenantRequest{TenantName: "Acme", Email: "owner@acme.com"}},
		{name: "password under 8 characters", req: admindto.CreateTenantRequest{TenantName: "Acme", Email: "owner@acme.com", Password: "1234567"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newTenantTestDeps(t)

			resp, err := newTestTenantService(d.params).CreateTenant(context.Background(), tt.req)
			require.Error(t, err)
			assert.True(t, ierr.IsValidation(err))
			assert.Nil(t, resp)

			created, _ := d.supabase.counts()
			assert.Zero(t, created)
		})
	}
}

// newTestTenantService wires the admin tenant service to the real shared tenant service, as fx does.
func newTestTenantService(params service.ServiceParams) TenantService {
	return NewTenantService(params, service.NewTenantService(params))
}

// cleanupFailingSupabase creates users but cannot delete them, like an outage right after the login is made.
func cleanupFailingSupabase(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": http.StatusInternalServerError, "msg": "boom"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "supabase_user_1"})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
