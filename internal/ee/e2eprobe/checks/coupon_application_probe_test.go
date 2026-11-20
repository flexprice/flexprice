package checks

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	itypes "github.com/flexprice/flexprice/internal/types"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

func TestCouponApplicationProbe_HappyPath(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{
		PlanIDs:          []string{"plan_1"},
		SharedCouponID:   "coupon_1",
		SharedCouponCode: "E2EPROBE_COUPON_10PCT",
	})

	couponID := "coupon_1"
	discount := "0.10"
	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{
			CouponApplications: []sdktypes.CouponApplicationResponse{{
				CouponID:         &couponID,
				DiscountedAmount: &discount,
			}},
		},
	}

	p := NewCouponApplicationProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(fc.events.ingested) != 100 {
		t.Errorf("ingested = %d, want 100", len(fc.events.ingested))
	}
	// The sub-create request must carry SubscriptionCoupons with the seed code.
	if len(fc.subs.created) != 1 {
		t.Fatalf("subs created = %d, want 1", len(fc.subs.created))
	}
	req := fc.subs.created[0]
	if len(req.SubscriptionCoupons) != 1 || req.SubscriptionCoupons[0].CouponCode != "E2EPROBE_COUPON_10PCT" {
		t.Errorf("sub SubscriptionCoupons = %+v, want [{CouponCode: E2EPROBE_COUPON_10PCT}]", req.SubscriptionCoupons)
	}
}

func TestCouponApplicationProbe_MissingSeedSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_1"}}) // SharedCouponCode empty

	p := NewCouponApplicationProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("missing SharedCouponCode must soft-skip; got %v", err)
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("no customer should be created when coupon seed absent")
	}
}

func TestCouponApplicationProbe_EmptyApplicationsFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{
		PlanIDs:          []string{"plan_1"},
		SharedCouponID:   "coupon_1",
		SharedCouponCode: "E2EPROBE_COUPON_10PCT",
	})

	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{},
	}

	p := NewCouponApplicationProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected error when preview.CouponApplications empty, got nil")
	}
}

func TestCouponApplicationProbe_WrongCouponIDFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{
		PlanIDs:          []string{"plan_1"},
		SharedCouponID:   "coupon_1",
		SharedCouponCode: "E2EPROBE_COUPON_10PCT",
	})

	otherID := "coupon_999"
	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{
			CouponApplications: []sdktypes.CouponApplicationResponse{{CouponID: &otherID}},
		},
	}

	p := NewCouponApplicationProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected error when no coupon application matches SharedCouponID, got nil")
	}
}
