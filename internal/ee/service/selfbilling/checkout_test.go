package selfbilling

import (
	"context"
	"net/http"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var checkoutReq = dto.SelfBillingCheckoutRequest{Name: "Acme", Email: "a@acme.io", Line1: "1 Main", City: "SF", Country: "US", PostalCode: "94105"}

func TestStartCheckout(t *testing.T) {
	tests := []struct {
		name          string
		remoteStatus  string // "" = no subscription yet
		free          bool   // $0 first period: Mumbai ends the trial without a session
		wantStatus    string
		wantURL       string
		wantTenant    types.TenantInternalStatus
		wantCheckouts int
	}{
		{"trialing_opens_trial_end_checkout", "trialing", false, "pending", "https://checkout.stripe.test/sub_us:t1", types.TenantInternalStatusTrialing, 1},
		{"no_subscription_provisions_then_checks_out", "", false, "pending", "https://checkout.stripe.test/sub_us:t1", types.TenantInternalStatusTrialing, 1},
		{"active_activates_without_checkout", "active", false, "completed", "", types.TenantInternalStatusActive, 0},
		{"incomplete_sends_to_portal", "incomplete", false, "pay_in_portal", "https://portal.test", types.TenantInternalStatusTrialing, 0},
		{"free_first_period_activates_immediately", "trialing", true, "completed", "", types.TenantInternalStatusActive, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			fc.freeFirstPeriod = tt.free
			s, d := newTestService(t, fc)
			seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
			if tt.remoteStatus != "" {
				fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: tt.remoteStatus}
			}
			resp, err := s.StartCheckout(tenantCtx("t1"), "t1", checkoutReq)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.Status)
			assert.Equal(t, tt.wantURL, resp.URL)
			assert.Equal(t, tt.wantTenant, resp.Tenant)
			assert.Equal(t, tt.wantCheckouts, fc.trialCheckouts)

			got, err := d.tenants.GetByID(context.Background(), "t1")
			require.NoError(t, err)
			assert.Equal(t, tt.wantTenant, got.InternalStatus)
			n, err := d.envs.CountByType(tenantCtx("t1"), types.EnvironmentProduction)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTenant == types.TenantInternalStatusActive, n == 1)
		})
	}
}

func TestStartCheckout_InvalidRequest(t *testing.T) {
	s, d := newTestService(t, newFakeClient())
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	_, err := s.StartCheckout(tenantCtx("t1"), "t1", dto.SelfBillingCheckoutRequest{})
	require.Error(t, err)
}

func TestStartCheckout_CancelledSubscriptionRejected(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusSuspended)
	fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: "cancelled"}
	_, err := s.StartCheckout(tenantCtx("t1"), "t1", checkoutReq)
	require.Error(t, err)
	assert.True(t, ierr.IsInvalidOperation(err), "got %v", err)
	assert.Equal(t, 0, fc.trialCheckouts)
}

// conflictErr is how the SDK surfaces Mumbai's 409 for a pending trial-end checkout.
func conflictErr(body string) error {
	return sdkerrors.NewAPIError("API error occurred", http.StatusConflict, body, nil)
}

func TestStartCheckout_CheckoutAlreadyInProgress(t *testing.T) {
	const ownBody = `{"code":"already_exists","message":"Complete or cancel the existing checkout before ending the trial","http_status_code":409,"details":{"subscription_id":"sub_us:t1","checkout_session_id":"cs_open"}}`
	const foreignBody = `{"code":"already_exists","http_status_code":409,"details":{"checkout_session_id":"cs_other"}}`
	const bareBody = `{"code":"already_exists","http_status_code":409}`
	tests := []struct {
		name    string
		body    string
		wantURL string // "" = 409 with the in-progress hint
	}{
		{"returns_existing_session", ownBody, "https://checkout.stripe.test/open"},
		{"no_session_id_in_body", bareBody, ""},
		{"session_owned_by_another_tenant", foreignBody, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			fc.trialCheckoutErr = conflictErr(tt.body)
			fc.checkouts["cs_open"] = &remoteCheckout{id: "cs_open", status: "pending", url: "https://checkout.stripe.test/open", extID: "us:t1"}
			fc.checkouts["cs_other"] = &remoteCheckout{id: "cs_other", status: "pending", url: "https://checkout.stripe.test/other", extID: "us:t2"}
			s, d := newTestService(t, fc)
			seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
			fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: "trialing"}

			resp, err := s.StartCheckout(tenantCtx("t1"), "t1", checkoutReq)
			if tt.wantURL != "" {
				require.NoError(t, err)
				assert.Equal(t, "cs_open", resp.CheckoutID)
				assert.Equal(t, tt.wantURL, resp.URL)
				assert.Equal(t, "pending", resp.Status)
				assert.Equal(t, types.TenantInternalStatusTrialing, resp.Tenant)
				return
			}
			require.Error(t, err)
			assert.True(t, ierr.IsAlreadyExists(err), "got %v", err)
			status, _ := ierr.ResolveError(err)
			assert.Equal(t, http.StatusConflict, status)
			assert.Contains(t, errors.FlattenHints(err), "a checkout is already in progress; finish or cancel it")
		})
	}
}

func TestPollCheckout_PaidActivatesOnce(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	fc.checkouts["cs1"] = &remoteCheckout{id: "cs1", status: "completed", terminal: true, extID: "us:t1"}
	fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: "active"}
	for i := 0; i < 3; i++ {
		resp, err := s.PollCheckout(tenantCtx("t1"), "t1", "cs1")
		require.NoError(t, err)
		assert.Equal(t, types.TenantInternalStatusActive, resp.Tenant)
	}
	n, err := d.envs.CountByType(tenantCtx("t1"), types.EnvironmentProduction)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestPollCheckout_CompletedActivatesOnlyWhenSubscriptionActive(t *testing.T) {
	tests := []struct {
		name         string
		remoteStatus string // "" = no subscription
		wantTenant   types.TenantInternalStatus
	}{
		{"active_subscription_activates", "active", types.TenantInternalStatusActive},
		{"cancelled_subscription_stays_suspended", "cancelled", types.TenantInternalStatusSuspended},
		{"incomplete_subscription_stays_suspended", "incomplete", types.TenantInternalStatusSuspended},
		{"no_subscription_stays_suspended", "", types.TenantInternalStatusSuspended},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(t, fc)
			seedTenant(t, d, "t1", types.TenantInternalStatusSuspended)
			fc.checkouts["cs_old"] = &remoteCheckout{id: "cs_old", status: "completed", terminal: true, extID: "us:t1"}
			if tt.remoteStatus != "" {
				fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: tt.remoteStatus}
			}

			resp, err := s.PollCheckout(tenantCtx("t1"), "t1", "cs_old")
			require.NoError(t, err)
			assert.Equal(t, "completed", resp.Status)
			assert.Equal(t, tt.wantTenant, resp.Tenant)
			n, err := d.envs.CountByType(tenantCtx("t1"), types.EnvironmentProduction)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTenant == types.TenantInternalStatusActive, n == 1)
		})
	}
}

func TestStartCheckout_FreeFirstPeriodWaitsForActiveSubscription(t *testing.T) {
	fc := newFakeClient()
	fc.freeFirstPeriod, fc.staleAfterTrialEnd = true, true
	s, d := newTestService(t, fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: "trialing"}

	resp, err := s.StartCheckout(tenantCtx("t1"), "t1", checkoutReq)
	require.NoError(t, err)
	assert.Equal(t, "completed", resp.Status)
	assert.Equal(t, types.TenantInternalStatusTrialing, resp.Tenant)
	n, err := d.envs.CountByType(tenantCtx("t1"), types.EnvironmentProduction)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestPollCheckout_ForeignSession(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	fc.checkouts["cs_b"] = &remoteCheckout{id: "cs_b", status: "completed", terminal: true, extID: "us:t2"}
	_, err := s.PollCheckout(tenantCtx("t1"), "t1", "cs_b")
	require.Error(t, err)
	status, _ := ierr.ResolveError(err)
	assert.Equal(t, http.StatusNotFound, status)
	got, _ := d.tenants.GetByID(context.Background(), "t1")
	assert.Equal(t, types.TenantInternalStatusTrialing, got.InternalStatus)
	n, _ := d.envs.CountByType(tenantCtx("t1"), types.EnvironmentProduction)
	assert.Equal(t, 0, n)
}

func TestPollCheckout_NotCompletedChangesNothing(t *testing.T) {
	for _, st := range []string{"pending", "failed", "expired"} {
		t.Run(st, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(t, fc)
			seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
			fc.checkouts["cs1"] = &remoteCheckout{id: "cs1", status: st, terminal: st != "pending", extID: "us:t1"}
			resp, err := s.PollCheckout(tenantCtx("t1"), "t1", "cs1")
			require.NoError(t, err)
			assert.Equal(t, types.TenantInternalStatusTrialing, resp.Tenant)
		})
	}
}

func TestActivate(t *testing.T) {
	tests := []struct {
		name           string
		from           types.TenantInternalStatus
		prodLimit      int // 0 = leave the default
		wantProdLimit  int
		wantProduction int
	}{
		{"trialing_to_active", types.TenantInternalStatusTrialing, 0, 1, 1},
		{"suspended_to_active", types.TenantInternalStatusSuspended, 0, 1, 1},
		{"already_active_is_repaired", types.TenantInternalStatusActive, 0, 1, 1},
		{"higher_limit_kept", types.TenantInternalStatusTrialing, 3, 3, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, d := newTestService(t, newFakeClient())
			seedTenant(t, d, "t1", tt.from)
			ctx := tenantCtx("t1")
			if tt.prodLimit > 0 {
				_, err := s.settings.UpdateSettingByKey(ctx, types.SettingKeyTenantConfig, &dto.UpdateSettingRequest{Value: map[string]interface{}{"production": tt.prodLimit}})
				require.NoError(t, err)
			}
			require.NoError(t, s.activate(ctx, "t1"))
			require.NoError(t, s.activate(ctx, "t1"))

			got, err := d.tenants.GetByID(ctx, "t1")
			require.NoError(t, err)
			assert.Equal(t, types.TenantInternalStatusActive, got.InternalStatus)
			setting, err := s.settings.GetSettingByKeyUnchecked(ctx, types.SettingKeyTenantConfig)
			require.NoError(t, err)
			assert.EqualValues(t, tt.wantProdLimit, setting.Value["production"])
			n, err := d.envs.CountByType(ctx, types.EnvironmentProduction)
			require.NoError(t, err)
			assert.Equal(t, tt.wantProduction, n)
		})
	}
}

// racingEnvStore inserts a production environment right after the first count, as a concurrent activation would.
type racingEnvStore struct {
	*testutil.InMemoryEnvironmentStore
	counts int
}

func (r *racingEnvStore) CountByType(ctx context.Context, envType types.EnvironmentType) (int, error) {
	r.counts++
	n, err := r.InMemoryEnvironmentStore.CountByType(ctx, envType)
	if r.counts == 1 {
		winner := &environment.Environment{ID: "env_winner", Name: "Production", Type: types.EnvironmentProduction, BaseModel: types.GetDefaultBaseModel(ctx)}
		if cerr := r.InMemoryEnvironmentStore.Create(ctx, winner); cerr != nil {
			return 0, cerr
		}
	}
	return n, err
}

func TestActivate_LimitRaceTreatedAsDone(t *testing.T) {
	s, d := newTestService(t, newFakeClient())
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	racing := &racingEnvStore{InMemoryEnvironmentStore: d.envs}
	s.EnvironmentRepo = racing
	s.envs = service.NewEnvironmentService(racing, service.NewEnvAccessService(s.Config), s.settings, s.ServiceParams)

	require.NoError(t, s.activate(tenantCtx("t1"), "t1"))

	// First read saw 0, CreateEnvironment's limit check refused ours, the recount found the winner.
	assert.Equal(t, 3, racing.counts)
	n, err := d.envs.CountByType(tenantCtx("t1"), types.EnvironmentProduction)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	got, _ := d.tenants.GetByID(context.Background(), "t1")
	assert.Equal(t, types.TenantInternalStatusActive, got.InternalStatus)
}

func TestCancelCheckout_ForeignSessionRejected(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	fc.checkouts["cs_b"] = &remoteCheckout{id: "cs_b", status: "pending", extID: "us:t2"}
	require.Error(t, s.CancelCheckout(tenantCtx("t1"), "t1", "cs_b"))
	assert.Equal(t, "pending", fc.checkouts["cs_b"].status)
}

func TestCancelCheckout_OwnSession(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	fc.checkouts["cs1"] = &remoteCheckout{id: "cs1", status: "pending", extID: "us:t1"}
	require.NoError(t, s.CancelCheckout(tenantCtx("t1"), "t1", "cs1"))
	assert.Equal(t, "failed", fc.checkouts["cs1"].status)
}
