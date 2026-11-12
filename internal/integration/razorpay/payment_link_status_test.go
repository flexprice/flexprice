package razorpay

// Tests for PaymentService.GetPaymentLinkStatus — the payment-link fallback used
// by the FlexPrice sync path when the direct pay_xxx isn't known yet.

import (
	"context"
	"errors"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePaymentLinkClient stubs only FetchPaymentLink; other RazorpayClient methods
// are inherited from the embedded (nil) interface and will panic if called.
type fakePaymentLinkClient struct {
	RazorpayClient
	resp map[string]interface{}
	err  error
}

func (c *fakePaymentLinkClient) FetchPaymentLink(_ context.Context, _ string) (map[string]interface{}, error) {
	return c.resp, c.err
}

func TestGetPaymentLinkStatus(t *testing.T) {
	tests := []struct {
		name        string
		resp        map[string]interface{}
		wantStatus  string
		wantPayment string
	}{
		{
			name: "paid link exposes captured pay_xxx for backfill",
			resp: map[string]interface{}{
				"status": "paid",
				"payments": []interface{}{
					map[string]interface{}{"payment_id": "pay_cap001", "status": "captured"},
				},
			},
			wantStatus:  "paid",
			wantPayment: "pay_cap001",
		},
		{
			name: "paid link skips non-captured attempts, picks first captured",
			resp: map[string]interface{}{
				"status": "paid",
				"payments": []interface{}{
					map[string]interface{}{"payment_id": "pay_fail001", "status": "failed"},
					map[string]interface{}{"payment_id": "pay_cap002", "status": "captured"},
				},
			},
			wantStatus:  "paid",
			wantPayment: "pay_cap002",
		},
		{
			name:        "created link has no payments — nothing to backfill",
			resp:        map[string]interface{}{"status": "created", "payments": nil},
			wantStatus:  "created",
			wantPayment: "",
		},
		{
			name:        "expired link — no captured payment expected",
			resp:        map[string]interface{}{"status": "expired"},
			wantStatus:  "expired",
			wantPayment: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &PaymentService{
				client: &fakePaymentLinkClient{resp: tt.resp},
				logger: logger.NewNoopLogger(),
			}
			got, err := svc.GetPaymentLinkStatus(context.Background(), "plink_test")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Status != tt.wantStatus || got.RazorpayPaymentID != tt.wantPayment {
				t.Fatalf("got %+v, want status=%s payment_id=%s", got, tt.wantStatus, tt.wantPayment)
			}
		})
	}
}

type expireClient struct {
	RazorpayClient
	cancelLinkIDs    []string
	cancelInvoiceIDs []string
	cancelErr        error
	fetched          map[string]interface{}
}

func (c *expireClient) CancelPaymentLink(_ context.Context, id string) (map[string]interface{}, error) {
	c.cancelLinkIDs = append(c.cancelLinkIDs, id)
	return map[string]interface{}{"status": "cancelled"}, c.cancelErr
}

func (c *expireClient) CancelInvoice(_ context.Context, id string) (map[string]interface{}, error) {
	c.cancelInvoiceIDs = append(c.cancelInvoiceIDs, id)
	return map[string]interface{}{"status": "cancelled"}, c.cancelErr
}

func (c *expireClient) FetchPaymentLink(context.Context, string) (map[string]interface{}, error) {
	return c.fetched, nil
}

func (c *expireClient) GetInvoice(context.Context, string) (map[string]interface{}, error) {
	return c.fetched, nil
}

func newExpireAdapter(c RazorpayClient) *CheckoutAdapter {
	return &CheckoutAdapter{Svc: &PaymentService{client: c, logger: logger.NewNoopLogger()}}
}

func TestExpirePaymentLink_RoutesPlinkToPaymentLinkCancel(t *testing.T) {
	c := &expireClient{}
	_, err := newExpireAdapter(c).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: "plink_x"})
	require.NoError(t, err)
	assert.Equal(t, []string{"plink_x"}, c.cancelLinkIDs)
	assert.Empty(t, c.cancelInvoiceIDs)
}

func TestExpirePaymentLink_RoutesInvoiceToInvoiceCancel(t *testing.T) {
	c := &expireClient{}
	_, err := newExpireAdapter(c).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: "inv_x"})
	require.NoError(t, err)
	assert.Equal(t, []string{"inv_x"}, c.cancelInvoiceIDs)
	assert.Empty(t, c.cancelLinkIDs)
}

func TestExpirePaymentLink_AlreadyClosedIsNil(t *testing.T) {
	for _, handle := range []string{"plink_x", "inv_x"} {
		for _, status := range []string{"cancelled", "expired"} {
			t.Run(handle+"/"+status, func(t *testing.T) {
				c := &expireClient{cancelErr: errors.New("boom"), fetched: map[string]interface{}{"status": status}}
				resp, err := newExpireAdapter(c).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: handle})
				require.NoError(t, err)
				assert.False(t, resp.AlreadyPaid)
			})
		}
	}
}

func TestExpirePaymentLink_PaidReportsCapturedPayment(t *testing.T) {
	tests := map[string]map[string]interface{}{
		"plink_x": {"status": "paid", "payments": []interface{}{
			map[string]interface{}{"payment_id": "pay_cap", "status": "captured"},
		}},
		"inv_x": {"status": "paid", "payment_id": "pay_cap"},
	}
	for handle, fetched := range tests {
		t.Run(handle, func(t *testing.T) {
			c := &expireClient{cancelErr: errors.New("boom"), fetched: fetched}
			resp, err := newExpireAdapter(c).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: handle})
			require.NoError(t, err)
			assert.True(t, resp.AlreadyPaid)
			assert.Equal(t, "pay_cap", resp.GatewayPaymentID)
		})
	}
}

func TestExpirePaymentLink_StillOpenReturnsCancelError(t *testing.T) {
	c := &expireClient{cancelErr: errors.New("boom"), fetched: map[string]interface{}{"status": "created"}}
	_, err := newExpireAdapter(c).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: "plink_x"})
	assert.Error(t, err)
}

func TestExpirePaymentLink_UnknownHandleNotImplemented(t *testing.T) {
	for _, handle := range []string{"order_x", "pay_x", ""} {
		_, err := newExpireAdapter(&expireClient{}).ExpirePaymentLink(context.Background(), interfaces.ExpirePaymentLinkRequest{GatewayTrackingID: handle})
		assert.True(t, ierr.IsNotImplemented(err), handle)
	}
}
