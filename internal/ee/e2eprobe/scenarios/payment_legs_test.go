package scenarios

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
)

func TestPaymentLegs_GroupsFailuresByMessage(t *testing.T) {
	d := newTestDeps(t)
	p, _ := d.World.Persona("payer-stripe")
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
	legs.runKnown("a known issue", true, "known", func() error { return fmt.Errorf("must not run") })

	r := legs.result(scenario.NewContext(d.Deps, p, "auto-charge"), "auto-charge")
	if r.Status != e2eprobe.StatusFail || r.Step != "auto-charge" {
		t.Fatalf("result = %+v", r)
	}
	want := []string{
		"wallet_topup, pay_invoice: 500 Unable to charge the stored token in Razorpay",
		"decline: declined card completed the session",
	}
	if strings.Join(r.Mismatches, "\n") != strings.Join(want, "\n") {
		t.Fatalf("mismatches =\n%s\nwant\n%s", strings.Join(r.Mismatches, "\n"), strings.Join(want, "\n"))
	}
	if r.Attrs["failed_legs"] != "wallet_topup,pay_invoice,decline" || r.Attrs["leg.pay_invoice.step"] != "pay_invoice_start" {
		t.Errorf("attrs = %v", r.Attrs)
	}
	if r.Attrs["step"] != "wallet_topup_start" || !strings.Contains(r.Attrs["error_body"], "500") {
		t.Errorf("the first failure's attributes must lead: %v", r.Attrs)
	}
	if r.Attrs["persona"] != "payer-stripe" {
		t.Errorf("automatic attrs must survive: %v", r.Attrs)
	}
	if _, ok := r.Attrs["skipped_legs"]; ok {
		t.Errorf("skipped legs must not be reported: %v", r.Attrs)
	}
}

func TestPaymentLegs_PassWhenEveryLegPasses(t *testing.T) {
	d := newTestDeps(t)
	p, _ := d.World.Persona("payer-stripe")
	var legs legResults
	legs.run("links", func() error { return nil })
	legs.runIf(false, "skipped", func() error { return fmt.Errorf("must not run") })
	r := legs.result(scenario.NewContext(d.Deps, p, "checkout-link"), "checkout-link")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if _, ok := r.Attrs["failed_legs"]; ok {
		t.Errorf("a passing run reports no failed legs: %v", r.Attrs)
	}
}
