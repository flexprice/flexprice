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
	gotTenant, gotCheckout string
}

func (s *stubSelfBilling) Status(_ context.Context, tenantID string) (*dto.SelfBillingStatusResponse, error) {
	s.gotTenant = tenantID
	return &dto.SelfBillingStatusResponse{Enabled: true, Status: types.TenantInternalStatusTrialing}, nil
}

func (s *stubSelfBilling) PollCheckout(_ context.Context, tenantID, id string) (*dto.SelfBillingCheckoutResponse, error) {
	s.gotTenant, s.gotCheckout = tenantID, id
	return &dto.SelfBillingCheckoutResponse{CheckoutID: id, Status: "pending"}, nil
}

func TestSelfBillingHandler_UsesContextTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name, path, wantCheckout string
	}{
		{"status ignores query tenant", "/status?tenant_id=evil", ""},
		{"poll passes path id", "/checkout/cs_1?tenant_id=evil", "cs_1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubSelfBilling{}
			h := NewSelfBillingHandler(stub)
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(types.SetTenantID(c.Request.Context(), "t1")) })
			r.GET("/status", h.Status)
			r.GET("/checkout/:id", h.PollCheckout)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "t1", stub.gotTenant)
			assert.Equal(t, tt.wantCheckout, stub.gotCheckout)
		})
	}
}
