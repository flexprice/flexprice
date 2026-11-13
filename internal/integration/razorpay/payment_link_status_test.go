package razorpay

// Tests for PaymentService.GetPaymentLinkStatus — the payment-link fallback used
// by the FlexPrice sync path when the direct pay_xxx isn't known yet.

import (
	"context"
	"errors"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	rzperrors "github.com/razorpay/razorpay-go/errors"
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

func TestPaymentLinkCreateError(t *testing.T) {
	tests := []struct {
		name   string
		status int
		err    error
		is     func(error) bool
	}{
		{"rejection is invalid operation", 400, &rzperrors.BadRequestError{Message: "amount too small"}, ierr.IsInvalidOperation},
		{"auth failure is invalid operation", 401, &rzperrors.BadRequestError{Message: "Authentication failed"}, ierr.IsInvalidOperation},
		{"rate limit is too many requests", 429, &rzperrors.BadRequestError{}, ierr.IsTooManyRequests},
		{"proxy 502 is internal", 502, &rzperrors.BadRequestError{}, ierr.IsInternal},
		{"server error is internal", 500, &rzperrors.ServerError{Message: "down"}, ierr.IsInternal},
		{"no response is internal", 0, errors.New("dial tcp: timeout"), ierr.IsInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paymentLinkCreateError(tt.status, tt.err); !tt.is(got) {
				t.Fatalf("got %v", got)
			}
		})
	}
}
