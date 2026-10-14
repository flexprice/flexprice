package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingHook records each tenant handed to self-billing as "tenant|name|email".
type recordingHook struct{ calls []string }

func (h *recordingHook) OnTenantCreated(_ context.Context, tenantID, name, email string) {
	h.calls = append(h.calls, tenantID+"|"+name+"|"+email)
}

// CreateTenant can run inside a caller's transaction, so provisioning is left to the caller after commit.
func TestCreateTenant_DoesNotCallSelfBillingHook(t *testing.T) {
	hook := &recordingHook{}
	params := ServiceParams{
		Logger: logger.NewNoopLogger(), Config: &config.Configuration{},
		TenantRepo: testutil.NewInMemoryTenantStore(), SelfBilling: hook,
	}

	_, err := NewTenantService(params).CreateTenant(context.Background(), dto.CreateTenantRequest{
		ID: "tenant_1", Name: "Acme", BillingDetails: &dto.TenantBillingDetails{Email: "a@acme.io"},
	})
	require.NoError(t, err)
	assert.Empty(t, hook.calls)
}
