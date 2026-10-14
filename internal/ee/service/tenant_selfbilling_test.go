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

type recordingHook struct{ calls []string }

func (h *recordingHook) OnTenantCreated(_ context.Context, tenantID, name, email string) {
	h.calls = append(h.calls, tenantID+"|"+name+"|"+email)
}

func TestCreateTenant_SelfBillingHook(t *testing.T) {
	tests := []struct {
		name  string
		hook  *recordingHook
		email string
		want  []string
	}{
		{"hook_called_once_with_email", &recordingHook{}, "a@acme.io", []string{"tenant_1|Acme|a@acme.io"}},
		{"nil_hook_is_fine", nil, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := ServiceParams{Logger: logger.NewNoopLogger(), Config: &config.Configuration{}, TenantRepo: testutil.NewInMemoryTenantStore()}
			if tt.hook != nil {
				params.SelfBilling = tt.hook
			}
			_, err := NewTenantService(params).CreateTenant(context.Background(), dto.CreateTenantRequest{
				ID: "tenant_1", Name: "Acme", BillingDetails: &dto.TenantBillingDetails{Email: tt.email},
			})
			require.NoError(t, err)
			if tt.hook != nil {
				assert.Equal(t, tt.want, tt.hook.calls)
			}
		})
	}
}
