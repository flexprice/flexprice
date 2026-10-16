package selfbilling

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummary_MapsSubscriptionAndStatus(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	end := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	fc.subs["us:t1"] = &remoteSubscription{id: "sub1", status: "trialing", trialEnd: &end}

	got, err := s.Summary(tenantCtx("t1"), "t1")
	require.NoError(t, err)
	assert.Equal(t, types.TenantInternalStatusTrialing, got.Status)
	assert.Equal(t, "trialing", got.SubscriptionStatus)
	assert.Equal(t, &end, got.TrialEndsAt)
	assert.NotNil(t, got.Invoices)
}

func TestPortalSession_UsesOwnCustomer(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusActive)
	got, err := s.PortalSession(tenantCtx("t1"), "t1")
	require.NoError(t, err)
	assert.Equal(t, "https://portal.test", got.URL)
	assert.Equal(t, []string{"us:t1"}, fc.portalExtIDs)
}
