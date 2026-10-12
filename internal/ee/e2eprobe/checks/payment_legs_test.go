package checks

import (
	"errors"
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
)

func TestLegResults_GroupsLegsByError(t *testing.T) {
	status, msg := int64(500), "Unable to charge the stored token in Razorpay"
	apiErr := &sdkerrors.ErrorResponse{HTTPStatusCode: &status, Message: &msg}
	var legs legResults
	for _, leg := range []string{"wallet_topup", "pay_invoice"} {
		legs.run(leg, func() error {
			return e2eprobe.Errorf(map[string]string{"step": leg + "_start"}, "start %s: %w", leg, apiErr)
		})
	}
	legs.run("decline", func() error { return errors.New("declined card completed the session") })
	legs.runIf(false, "refund", func() error { return fmt.Errorf("must not run") })

	f := &paymentFlow{opts: PaymentProbeOpts{Provider: e2eprobe.PaymentProviderConfig{Provider: "razorpay"}}}
	err := legs.err(f)
	want := "wallet_topup, pay_invoice: 500 Unable to charge the stored token in Razorpay\ndecline: declined card completed the session"
	if err == nil || err.Error() != want {
		t.Fatalf("err =\n%v\nwant\n%s", err, want)
	}
	attrs := e2eprobe.AttributesFrom(err)
	if attrs["failed_legs"] != "wallet_topup,pay_invoice,decline" || attrs["leg.pay_invoice.step"] != "pay_invoice_start" {
		t.Errorf("attrs = %v", attrs)
	}
	if _, ok := attrs["skipped_legs"]; ok {
		t.Errorf("skipped legs must not be reported: %v", attrs)
	}
}
