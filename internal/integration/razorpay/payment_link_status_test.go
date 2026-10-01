package razorpay

// Tests for PaymentService.GetPaymentLinkStatus — the payment-link fallback used
// by the FlexPrice sync path when the direct pay_xxx isn't known yet.

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/logger"
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

type cancelChargeClient struct {
	RazorpayClient
	invoices          map[string]map[string]interface{}
	links             map[string]map[string]interface{}
	cancelledInvoices []string
	cancelledLinks    []string
}

func (c *cancelChargeClient) GetInvoice(_ context.Context, id string) (map[string]interface{}, error) {
	return c.invoices[id], nil
}

func (c *cancelChargeClient) CancelInvoice(_ context.Context, id string) (map[string]interface{}, error) {
	c.cancelledInvoices = append(c.cancelledInvoices, id)
	return map[string]interface{}{"id": id, "status": "cancelled"}, nil
}

func (c *cancelChargeClient) FetchPaymentLink(_ context.Context, id string) (map[string]interface{}, error) {
	return c.links[id], nil
}

func (c *cancelChargeClient) CancelPaymentLink(_ context.Context, id string) (map[string]interface{}, error) {
	c.cancelledLinks = append(c.cancelledLinks, id)
	return map[string]interface{}{"id": id, "status": "cancelled"}, nil
}

func TestCancelOpenCharge(t *testing.T) {
	ctx := context.Background()

	t.Run("invoice cancels an unpaid invoice", func(t *testing.T) {
		client := &cancelChargeClient{invoices: map[string]map[string]interface{}{
			"inv_open": {"status": "issued"},
		}}
		adapter := &CheckoutAdapter{Svc: NewPaymentService(client, nil, nil, nil, logger.NewNoopLogger())}

		got, err := adapter.CancelOpenCharge(ctx, "inv_open")

		if err != nil {
			t.Fatal(err)
		}
		if got.Captured || len(client.cancelledInvoices) != 1 || client.cancelledInvoices[0] != "inv_open" {
			t.Fatalf("got captured=%v invoices=%v", got.Captured, client.cancelledInvoices)
		}
	})

	t.Run("payment link cancels an unpaid link", func(t *testing.T) {
		client := &cancelChargeClient{links: map[string]map[string]interface{}{
			"plink_open": {"status": "created"},
		}}
		adapter := &CheckoutAdapter{Svc: NewPaymentService(client, nil, nil, nil, logger.NewNoopLogger())}

		got, err := adapter.CancelOpenCharge(ctx, "plink_open")

		if err != nil {
			t.Fatal(err)
		}
		if got.Captured || len(client.cancelledLinks) != 1 || client.cancelledLinks[0] != "plink_open" {
			t.Fatalf("got captured=%v links=%v", got.Captured, client.cancelledLinks)
		}
	})

	t.Run("paid invoice reports the capture", func(t *testing.T) {
		client := &cancelChargeClient{invoices: map[string]map[string]interface{}{
			"inv_paid": {"status": "paid", "payment_id": "pay_rzp_1"},
		}}
		adapter := &CheckoutAdapter{Svc: NewPaymentService(client, nil, nil, nil, logger.NewNoopLogger())}

		got, err := adapter.CancelOpenCharge(ctx, "inv_paid")

		if err != nil {
			t.Fatal(err)
		}
		if !got.Captured || got.GatewayPaymentID != "pay_rzp_1" || len(client.cancelledInvoices) != 0 {
			t.Fatalf("got %+v cancelled=%v", got, client.cancelledInvoices)
		}
	})

	t.Run("unknown prefix fails closed", func(t *testing.T) {
		adapter := &CheckoutAdapter{Svc: NewPaymentService(&cancelChargeClient{}, nil, nil, nil, logger.NewNoopLogger())}
		if _, err := adapter.CancelOpenCharge(ctx, "order_unknown"); err == nil {
			t.Fatal("expected an error")
		}
	})
}
