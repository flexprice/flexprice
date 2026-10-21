package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service/selfbilling"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSelfBilling struct {
	selfbilling.Service

	gotTenant   string
	gotCheckout string
}

func (s *stubSelfBilling) Status(_ context.Context, tenantID string) (*dto.SelfBillingStatusResponse, error) {
	s.gotTenant = tenantID
	return &dto.SelfBillingStatusResponse{Enabled: true, Status: types.TenantInternalStatusTrialing}, nil
}

func (s *stubSelfBilling) PollCheckout(_ context.Context, tenantID, id string) (*dto.SelfBillingCheckoutResponse, error) {
	s.gotTenant = tenantID
	s.gotCheckout = id
	return &dto.SelfBillingCheckoutResponse{CheckoutID: id, Status: types.CheckoutStatusPending}, nil
}

func (s *stubSelfBilling) Summary(_ context.Context, tenantID string) (*dto.SelfBillingSummaryResponse, error) {
	s.gotTenant = tenantID
	return &dto.SelfBillingSummaryResponse{Status: types.TenantInternalStatusActive}, nil
}

func (s *stubSelfBilling) PortalSession(_ context.Context, tenantID string) (*dto.SelfBillingPortalResponse, error) {
	s.gotTenant = tenantID
	return &dto.SelfBillingPortalResponse{URL: "https://portal.test"}, nil
}

func TestSelfBillingHandler_UsesContextTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		method       string
		path         string
		wantCheckout string
	}{
		{"status_ignores_query_tenant", http.MethodGet, "/status?tenant_id=evil", ""},
		{"poll_passes_path_id", http.MethodGet, "/checkout/cs_1?tenant_id=evil", "cs_1"},
		{"summary_ignores_query_tenant", http.MethodGet, "/summary?tenant_id=evil", ""},
		{"tenant_billing_ignores_query_tenant", http.MethodGet, "/tenants/billing?tenant_id=evil", ""},
		{"portal_ignores_query_tenant", http.MethodPost, "/portal-session?tenant_id=evil", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubSelfBilling{}
			h := NewSelfBillingHandler(stub)

			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(types.SetTenantID(c.Request.Context(), "t1"))
			})
			r.GET("/status", h.Status)
			r.GET("/checkout/:id", h.PollCheckout)
			r.GET("/summary", h.Summary)
			r.GET("/tenants/billing", h.TenantBilling)
			r.POST("/portal-session", h.PortalSession)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "t1", stub.gotTenant)
			assert.Equal(t, tt.wantCheckout, stub.gotCheckout)
		})
	}
}
