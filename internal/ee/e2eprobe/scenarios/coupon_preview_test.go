package scenarios

import (
	"context"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func couponedPreview(couponID, discount string) *dtos.GetInvoicePreviewResponse {
	return &dtos.GetInvoicePreviewResponse{InvoiceResponse: &types.InvoiceResponse{
		CouponApplications: []types.CouponApplicationResponse{{CouponID: probetest.Ptr(couponID), DiscountedAmount: probetest.Ptr(discount)}},
	}}
}

func TestCouponPreview_Pass(t *testing.T) {
	d := newTestDeps(t)
	servePreview(d, couponedPreview(d.World.Fixtures().CouponID(), "2.00"))
	r := run(t, d, newCouponPreview(d.Deps), "couponed")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	subID := r.Attrs["ephemeral_subscription_id"]
	if len(d.fc.Invoice.PreviewCalls) != 1 || d.fc.Invoice.PreviewCalls[0].SubscriptionID != subID {
		t.Errorf("preview calls = %+v", d.fc.Invoice.PreviewCalls)
	}
	attached := false
	for _, ca := range d.fc.CouponAssoc.Items {
		attached = attached || (ca.SubscriptionID != nil && *ca.SubscriptionID == subID)
	}
	if !attached {
		t.Error("the clone must carry the shared coupon")
	}
	if len(d.fc.Event.Batches) != 0 {
		t.Error("the coupon preview needs no usage")
	}
}

func TestCouponPreview_MismatchWhenCouponAbsent(t *testing.T) {
	d := newTestDeps(t)
	servePreview(d, couponedPreview("coupon_other", "2.00"))
	r := run(t, d, newCouponPreview(d.Deps), "couponed")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_coupon_preview" || r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if len(r.Mismatches) != 1 || r.Mismatches[0] != "preview applies the shared coupon" {
		t.Errorf("mismatches = %v", r.Mismatches)
	}
}

func TestCouponPreview_MismatchWhenDiscountNotPositive(t *testing.T) {
	d := newTestDeps(t)
	servePreview(d, couponedPreview(d.World.Fixtures().CouponID(), "0.00"))
	r := run(t, d, newCouponPreview(d.Deps), "couponed")
	if r.Status != e2eprobe.StatusFail || len(r.Mismatches) != 1 || !strings.Contains(r.Mismatches[0], "discount") {
		t.Fatalf("result = %+v", r)
	}
}

func TestCouponPreview_RunsDespitePersonaGap(t *testing.T) {
	d := newTestDeps(t)
	d.fc.CouponAssoc.Items = nil
	if err := d.World.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	created := len(d.fc.Customer.Created)
	r := run(t, d, newCouponPreview(d.Deps), "couponed")
	// The clone is stamped fresh with the coupon, so the persona's adopted subscription does not matter.
	if r.Status == e2eprobe.StatusSkip {
		t.Fatalf("a clone flow must not skip on the persona's gap: %+v", r)
	}
	if len(d.fc.Customer.Created) != created+1 || len(d.fc.Invoice.PreviewCalls) != 1 {
		t.Error("the flow must clone and preview")
	}
}

func TestCouponPreview_PreviewErrorIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Invoice.Preview = func(types.GetPreviewInvoiceRequest) (*dtos.GetInvoicePreviewResponse, error) {
		return nil, probetest.ServerError(503)
	}
	r := run(t, d, newCouponPreview(d.Deps), "couponed")
	if r.Status != e2eprobe.StatusFail || r.Step != "preview" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
}
