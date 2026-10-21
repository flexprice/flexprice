package selfbilling

import (
	"context"
	"net/http"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var checkoutReq = dto.SelfBillingCheckoutRequest{
	Name:       "Acme",
	Email:      "a@acme.io",
	Line1:      "1 Main",
	City:       "SF",
	Country:    "US",
	PostalCode: "94105",
}

func productionEnvs(t *testing.T, d *testDeps, tenantID string) int {
	t.Helper()
	n, err := d.envs.CountByType(tenantCtx(tenantID), types.EnvironmentProduction)
	require.NoError(t, err)
	return n
}

func TestStartCheckout(t *testing.T) {
	tests := []struct {
		name          string
		remoteStatus  types.SubscriptionStatus // "" = no subscription yet
		free          bool                     // $0 first period: the trial ends without a session
		wantStatus    types.CheckoutStatus
		wantURL       string
		wantTenant    types.TenantInternalStatus
		wantCheckouts int
	}{
		{
			name:          "trialing_opens_trial_end_checkout",
			remoteStatus:  types.SubscriptionStatusTrialing,
			wantStatus:    types.CheckoutStatusPending,
			wantURL:       "https://checkout.test/sub_us:t1",
			wantTenant:    types.TenantInternalStatusTrialing,
			wantCheckouts: 1,
		},
		{
			name:          "no_subscription_provisions_then_checks_out",
			wantStatus:    types.CheckoutStatusPending,
			wantURL:       "https://checkout.test/sub_us:t1",
			wantTenant:    types.TenantInternalStatusTrialing,
			wantCheckouts: 1,
		},
		{
			name:         "active_activates_without_checkout",
			remoteStatus: types.SubscriptionStatusActive,
			wantStatus:   types.CheckoutStatusCompleted,
			wantTenant:   types.TenantInternalStatusActive,
		},
		{
			name:         "incomplete_sends_to_portal",
			remoteStatus: types.SubscriptionStatusIncomplete,
			wantStatus:   types.CheckoutStatusPending,
			wantURL:      "https://portal.test",
			wantTenant:   types.TenantInternalStatusTrialing,
		},
		{
			name:          "free_first_period_activates_once_subscription_active",
			remoteStatus:  types.SubscriptionStatusTrialing,
			free:          true,
			wantStatus:    types.CheckoutStatusCompleted,
			wantTenant:    types.TenantInternalStatusActive,
			wantCheckouts: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			fc.freeFirstPeriod = tt.free
			s, d := newTestService(fc)
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
			assert.Equal(t, tt.wantTenant == types.TenantInternalStatusActive, productionEnvs(t, d, "t1") == 1)
		})
	}
}

func TestStartCheckout_Rejected(t *testing.T) {
	tests := []struct {
		name         string
		req          dto.SelfBillingCheckoutRequest
		remoteStatus types.SubscriptionStatus
		wantKind     func(error) bool
	}{
		{"invalid_request", dto.SelfBillingCheckoutRequest{}, types.SubscriptionStatusTrialing, ierr.IsValidation},
		{"cancelled_subscription", checkoutReq, types.SubscriptionStatusCancelled, ierr.IsInvalidOperation},
		{"paused_subscription", checkoutReq, types.SubscriptionStatusPaused, ierr.IsInvalidOperation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			seedTenant(t, d, "t1", types.TenantInternalStatusSuspended)
			fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: tt.remoteStatus}

			_, err := s.StartCheckout(tenantCtx("t1"), "t1", tt.req)
			require.Error(t, err)
			assert.True(t, tt.wantKind(err), "got %v", err)
			assert.Equal(t, 0, fc.trialCheckouts)
		})
	}
}

func TestStartCheckout_FreeFirstPeriodWaitsForActiveSubscription(t *testing.T) {
	fc := newFakeClient()
	fc.freeFirstPeriod = true
	fc.staleAfterTrialEnd = true
	s, d := newTestService(fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: types.SubscriptionStatusTrialing}

	resp, err := s.StartCheckout(tenantCtx("t1"), "t1", checkoutReq)
	require.NoError(t, err)

	assert.Equal(t, types.CheckoutStatusCompleted, resp.Status)
	assert.Equal(t, types.TenantInternalStatusTrialing, resp.Tenant)
	assert.Equal(t, 0, productionEnvs(t, d, "t1"))
}

func TestPollCheckout_PaidActivatesOnce(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	fc.checkouts["cs1"] = &remoteCheckout{id: "cs1", status: types.CheckoutStatusCompleted, terminal: true, extID: "us:t1"}
	fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: types.SubscriptionStatusActive}

	for i := 0; i < 3; i++ {
		resp, err := s.PollCheckout(tenantCtx("t1"), "t1", "cs1")
		require.NoError(t, err)
		assert.Equal(t, types.TenantInternalStatusActive, resp.Tenant)
	}
	assert.Equal(t, 1, productionEnvs(t, d, "t1"))
}

func TestPollCheckout_CompletedActivatesOnlyWhenSubscriptionActive(t *testing.T) {
	tests := []struct {
		name         string
		remoteStatus types.SubscriptionStatus // "" = no subscription
		wantTenant   types.TenantInternalStatus
	}{
		{"active_subscription_activates", types.SubscriptionStatusActive, types.TenantInternalStatusActive},
		{"cancelled_subscription_stays_suspended", types.SubscriptionStatusCancelled, types.TenantInternalStatusSuspended},
		{"incomplete_subscription_stays_suspended", types.SubscriptionStatusIncomplete, types.TenantInternalStatusSuspended},
		{"no_subscription_stays_suspended", "", types.TenantInternalStatusSuspended},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			seedTenant(t, d, "t1", types.TenantInternalStatusSuspended)
			fc.checkouts["cs_old"] = &remoteCheckout{id: "cs_old", status: types.CheckoutStatusCompleted, terminal: true, extID: "us:t1"}
			if tt.remoteStatus != "" {
				fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: tt.remoteStatus}
			}

			resp, err := s.PollCheckout(tenantCtx("t1"), "t1", "cs_old")
			require.NoError(t, err)

			assert.Equal(t, types.CheckoutStatusCompleted, resp.Status)
			assert.Equal(t, tt.wantTenant, resp.Tenant)
			assert.Equal(t, tt.wantTenant == types.TenantInternalStatusActive, productionEnvs(t, d, "t1") == 1)
		})
	}
}

func TestPollCheckout_ForeignSession(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	fc.checkouts["cs_b"] = &remoteCheckout{id: "cs_b", status: types.CheckoutStatusCompleted, terminal: true, extID: "us:t2"}
	fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: types.SubscriptionStatusActive}

	_, err := s.PollCheckout(tenantCtx("t1"), "t1", "cs_b")
	require.Error(t, err)
	status, _ := ierr.ResolveError(err)
	assert.Equal(t, http.StatusNotFound, status)

	got, err := d.tenants.GetByID(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, types.TenantInternalStatusTrialing, got.InternalStatus)
	assert.Equal(t, 0, productionEnvs(t, d, "t1"))
}

func TestPollCheckout_NotCompletedChangesNothing(t *testing.T) {
	for _, st := range []types.CheckoutStatus{types.CheckoutStatusPending, types.CheckoutStatusFailed, types.CheckoutStatusExpired} {
		t.Run(string(st), func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
			fc.checkouts["cs1"] = &remoteCheckout{id: "cs1", status: st, terminal: st.IsTerminal(), extID: "us:t1"}
			fc.subs["us:t1"] = &remoteSubscription{id: "sub_us:t1", status: types.SubscriptionStatusActive}

			resp, err := s.PollCheckout(tenantCtx("t1"), "t1", "cs1")
			require.NoError(t, err)
			assert.Equal(t, types.TenantInternalStatusTrialing, resp.Tenant)
		})
	}
}

func TestCancelCheckout(t *testing.T) {
	tests := []struct {
		name       string
		extID      string
		wantErr    bool
		wantStatus types.CheckoutStatus
	}{
		{"own_session_cancelled", "us:t1", false, types.CheckoutStatusFailed},
		{"foreign_session_rejected", "us:t2", true, types.CheckoutStatusPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
			fc.checkouts["cs1"] = &remoteCheckout{id: "cs1", status: types.CheckoutStatusPending, extID: tt.extID}

			err := s.CancelCheckout(tenantCtx("t1"), "t1", "cs1")
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, ierr.IsNotFound(err), "got %v", err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantStatus, fc.checkouts["cs1"].status)
		})
	}
}
