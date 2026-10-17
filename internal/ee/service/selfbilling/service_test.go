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
