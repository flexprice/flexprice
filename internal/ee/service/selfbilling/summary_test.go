package selfbilling

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummary_MapsSubscriptionAndInvoices(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)

	// A future trial end, relative so the test never expires with the calendar.
	end := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, 7)
	fc.summaries["us:t1"] = &remoteSummary{
		subscription: &remoteSubscription{
			id:       "sub1",
			status:   types.SubscriptionStatusTrialing,
			trialEnd: &end,
		},
		plan: &dto.SelfBillingPlanResponse{
			ID:   "plan_1",
			Name: "Self Serve",
			Charges: []*dto.SelfBillingChargeResponse{{
				PriceID: "price_override",
				Amount:  decimal.RequireFromString("79.99"),
			}},
		},
		invoices: []remoteInvoice{{
			id:            "inv_1",
			number:        "INV-1",
			status:        types.InvoiceStatusFinalized,
			paymentStatus: types.PaymentStatusSucceeded,
			total:         decimal.RequireFromString("12.5"),
			currency:      "usd",
		}},
	}

	got, err := s.Summary(tenantCtx("t1"), "t1")
	require.NoError(t, err)

	assert.Equal(t, types.TenantInternalStatusTrialing, got.Status)
	assert.Equal(t, types.SubscriptionStatusTrialing, got.SubscriptionStatus)
	assert.Equal(t, &end, got.TrialEndsAt)
	require.NotNil(t, got.Plan)
	assert.Equal(t, "plan_1", got.Plan.ID)
	require.Len(t, got.Plan.Charges, 1)
	assert.Equal(t, "79.99", got.Plan.Charges[0].Amount.String())
	require.Len(t, got.Invoices, 1)
	assert.Equal(t, "INV-1", got.Invoices[0].Number)
	assert.Equal(t, types.InvoiceStatusFinalized, got.Invoices[0].Status)
	assert.Equal(t, types.PaymentStatusSucceeded, got.Invoices[0].PaymentStatus)
	assert.Equal(t, "12.5", got.Invoices[0].Total.String())
}

func TestSummary_ReconcilesTenantStatus(t *testing.T) {
	tests := []struct {
		name       string
		current    types.TenantInternalStatus
		subStatus  types.SubscriptionStatus
		wantStatus types.TenantInternalStatus
		wantProd   bool
	}{
		{"paid_while_away_activates", types.TenantInternalStatusTrialing, types.SubscriptionStatusActive, types.TenantInternalStatusActive, true},
		{"paid_in_portal_restores_suspended", types.TenantInternalStatusSuspended, types.SubscriptionStatusActive, types.TenantInternalStatusActive, true},
		{"unpaid_trial_end_suspends", types.TenantInternalStatusTrialing, types.SubscriptionStatusIncomplete, types.TenantInternalStatusSuspended, false},
		{"trialing_unchanged", types.TenantInternalStatusTrialing, types.SubscriptionStatusTrialing, types.TenantInternalStatusTrialing, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			seedTenant(t, d, "t1", tt.current)
			fc.summaries["us:t1"] = &remoteSummary{
				subscription: &remoteSubscription{
					id:     "sub1",
					status: tt.subStatus,
				},
			}

			got, err := s.Summary(tenantCtx("t1"), "t1")
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, got.Status)

			stored, err := d.tenants.GetByID(tenantCtx("t1"), "t1")
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, stored.InternalStatus)
			assert.Equal(t, tt.wantProd, productionEnvs(t, d, "t1") == 1)
		})
	}
}

func TestSummary_NoSubscriptionHasEmptyInvoices(t *testing.T) {
	s, d := newTestService(newFakeClient())
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)

	got, err := s.Summary(tenantCtx("t1"), "t1")
	require.NoError(t, err)
	assert.Empty(t, got.SubscriptionStatus)
	assert.NotNil(t, got.Invoices)
}

func TestPortalSession_UsesOwnCustomer(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusActive)

	got, err := s.PortalSession(tenantCtx("t1"), "t1")
	require.NoError(t, err)
	assert.Equal(t, "https://portal.test", got.URL)
	assert.Equal(t, []string{"us:t1"}, fc.portalExtIDs)
}
