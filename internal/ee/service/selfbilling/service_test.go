package selfbilling

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExternalID(t *testing.T) {
	assert.Equal(t, "us:tenant_1", externalID("us", "tenant_1"))
}

func TestNewService_DisabledIsNoop(t *testing.T) {
	svc := NewService(service.ServiceParams{Logger: logger.NewNoopLogger(), Config: &config.Configuration{}}, nil, nil, nil, nil)
	assert.False(t, svc.Enabled())
	st, err := svc.Status(context.Background(), "tenant_1")
	require.NoError(t, err)
	assert.False(t, st.Enabled)
	svc.OnTenantCreated(context.Background(), "tenant_1", "Acme", "a@acme.io")
	require.NoError(t, svc.RunHourly(context.Background()))
	_, err = svc.StartCheckout(context.Background(), "tenant_1", dto.SelfBillingCheckoutRequest{})
	assert.ErrorIs(t, err, errDisabled)
}

func TestOnTenantCreated_Provisions(t *testing.T) {
	tests := []struct {
		name          string
		tenantID      string
		billingTenant string
		existingSub   bool
		wantCustomer  bool
		wantTrial     bool
		wantLegacyID  string
	}{
		{"new_tenant_gets_customer_and_trial", "t1", "", false, true, true, ""},
		{"existing_subscription_no_second_trial", "t1", "", true, true, false, ""},
		{"billing_tenant_skipped", "t_billing", "t_billing", false, false, false, ""},
		{"billing_tenant_region_looks_up_legacy_bare_id", "t1", "t_billing", false, true, true, "t1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, _ := newTestService(t, fc)
			s.Config.Billing.TenantID = tt.billingTenant
			if tt.existingSub {
				fc.subs["us:"+tt.tenantID] = &remoteSubscription{id: "old", status: "active"}
			}
			s.OnTenantCreated(context.Background(), tt.tenantID, "Acme", "a@acme.io")
			assert.Equal(t, tt.wantCustomer, fc.customers["us:"+tt.tenantID])
			assert.Equal(t, tt.wantTrial, fc.trialsStarted == 1)
			assert.Equal(t, tt.wantLegacyID, fc.legacyExtIDs["us:"+tt.tenantID])
		})
	}
}

func TestFakeClient_GetCheckoutUnknownIsNotFound(t *testing.T) {
	_, err := newFakeClient().GetCheckout(context.Background(), "cs_missing")
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
	assert.NotErrorIs(t, err, errDisabled)
}

// recordingClient records the context state provisioning ran with and can panic on the first Mumbai call.
type recordingClient struct {
	*fakeClient
	ctxErr      error
	hasDeadline bool
	panic       bool
}

func (r *recordingClient) EnsureCustomer(ctx context.Context, in customerInput) error {
	r.ctxErr = ctx.Err()
	_, r.hasDeadline = ctx.Deadline()
	if r.panic {
		panic("mumbai client blew up")
	}
	return r.fakeClient.EnsureCustomer(ctx, in)
}

func TestOnTenantCreated_RunsDetached(t *testing.T) {
	rc := &recordingClient{fakeClient: newFakeClient()}
	s, _ := newTestService(t, rc.fakeClient)
	s.client = rc
	var pending func()
	s.spawn = func(run func()) { pending = run }

	ctx, cancel := context.WithCancel(context.Background())
	s.OnTenantCreated(ctx, "t1", "Acme", "a@acme.io")
	require.NotNil(t, pending, "provisioning is handed off, not run inline")
	assert.False(t, rc.customers["us:t1"], "the caller is not blocked on Mumbai")

	cancel() // the request finishing must not abort provisioning
	pending()
	assert.True(t, rc.customers["us:t1"])
	assert.Equal(t, 1, rc.trialsStarted)
	require.NoError(t, rc.ctxErr)
	assert.True(t, rc.hasDeadline, "provisioning runs under its own timeout")
}

func TestOnTenantCreated_PanicIsRecovered(t *testing.T) {
	rc := &recordingClient{fakeClient: newFakeClient(), panic: true}
	s, _ := newTestService(t, rc.fakeClient)
	s.client = rc
	assert.NotPanics(t, func() { s.OnTenantCreated(context.Background(), "t1", "Acme", "a@acme.io") })
	assert.Equal(t, 0, rc.trialsStarted)
}
