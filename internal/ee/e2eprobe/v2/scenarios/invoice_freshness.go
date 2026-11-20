package scenarios

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// registerInvoices adds the invoice scenarios. The suite's "invoice" entry
// expands its families to the invoice-<family> names registered here.
func registerInvoices(cat *scenario.Catalog) {
	all := []registration{
		entry("invoice-freshness", e2eprobe.KindProbe, time.Hour, func(d scenario.Deps) scenario.Scenario { return newInvoiceFreshness(d) }, e2eprobe.TraitSubscription),
		entry("invoice-tax", e2eprobe.KindProbe, time.Hour, func(d scenario.Deps) scenario.Scenario { return newInvoiceTax(d) }, e2eprobe.TraitTax),
		entry("invoice-coupon", e2eprobe.KindProbe, time.Hour, func(d scenario.Deps) scenario.Scenario { return newInvoiceCoupon(d) }, e2eprobe.TraitCoupon),
		entry("invoice-commitment", e2eprobe.KindProbe, time.Hour, func(d scenario.Deps) scenario.Scenario { return newInvoiceCommitment(d) }, e2eprobe.TraitCommitment),
		entry("invoice-multi-cadence", e2eprobe.KindProbe, time.Hour, func(d scenario.Deps) scenario.Scenario { return newInvoiceMultiCadence(d) }, e2eprobe.TraitMultiCadence),
	}
	for _, r := range all {
		cat.Register(r.info, r.build)
	}
}

// freshnessGrace is how long after a cycle closes its invoice may still be on
// its way before the cycle counts as stalled.
const freshnessGrace = 3 * 24 * time.Hour

// invoiceFreshness notices a subscription whose cycle invoicing stalled: the
// latest invoice's period end may lag now by at most one billing cycle plus
// the grace.
type invoiceFreshness struct{}

func newInvoiceFreshness(scenario.Deps) *invoiceFreshness { return &invoiceFreshness{} }

func (*invoiceFreshness) Name() string        { return "invoice-freshness" }
func (*invoiceFreshness) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }
func (*invoiceFreshness) Requires() []e2eprobe.Trait {
	return []e2eprobe.Trait{e2eprobe.TraitSubscription}
}

func (*invoiceFreshness) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	subID := sc.Persona().SubscriptionID()
	sc.Attr("subscription_id", subID)
	sub, err := subscriptionOf(ctx, sc, subID)
	switch {
	case world.IsNotFound(err):
		return sc.Skip("subscription", "subscription not found yet")
	case err != nil:
		return sc.Fail("subscription", err)
	}
	cycle := cycleLength(sub)
	invoices, err := subscriptionInvoices(ctx, sc, subID)
	if err != nil {
		return sc.Fail("invoices", err)
	}
	latest := latestInvoice(invoices)
	if latest == nil {
		age := subscriptionAge(sc.Now(), sub)
		if age <= cycle+freshnessGrace {
			return sc.Skip("young", fmt.Sprintf("subscription is %s old; its first invoice is due within one cycle of %s plus %s", age.Round(time.Hour), cycle, freshnessGrace))
		}
		sc.Truth(fmt.Sprintf("no invoice after one cycle (subscription is %s old, cycle %s)", age.Round(time.Hour), cycle), false)
		return sc.Result("freshness")
	}
	end := invoiceTime(*latest).UTC()
	lag := sc.Now().Sub(end)
	sc.Attr("latest_period_end", end.Format(time.RFC3339))
	sc.Attr("lag", lag.Round(time.Minute).String())
	sc.Truth(fmt.Sprintf("invoice freshness: latest period end %s lags %s, over one cycle of %s plus %s", end.Format(time.RFC3339), lag.Round(time.Minute), cycle, freshnessGrace), lag <= cycle+freshnessGrace)
	return sc.Result("freshness")
}

// cycleLength is one billing cycle; an unknown period reads as 30 days.
func cycleLength(sub types.SubscriptionResponse) time.Duration {
	unit := 30 * 24 * time.Hour
	if sub.BillingPeriod != nil {
		switch *sub.BillingPeriod {
		case types.BillingPeriodDaily:
			unit = 24 * time.Hour
		case types.BillingPeriodWeekly:
			unit = 7 * 24 * time.Hour
		case types.BillingPeriodQuarterly:
			unit = 91 * 24 * time.Hour
		case types.BillingPeriodHalfYearly:
			unit = 182 * 24 * time.Hour
		case types.BillingPeriodAnnual:
			unit = 365 * 24 * time.Hour
		}
	}
	return unit * time.Duration(periodCount(sub))
}

// periodCount is the billing period multiplier, at least one.
func periodCount(sub types.SubscriptionResponse) int64 {
	if sub.BillingPeriodCount != nil && *sub.BillingPeriodCount > 0 {
		return *sub.BillingPeriodCount
	}
	return 1
}

// subscriptionAge is how long ago the subscription was created; unknown reads as zero.
func subscriptionAge(now time.Time, sub types.SubscriptionResponse) time.Duration {
	if sub.CreatedAt == nil || sub.CreatedAt.After(now) {
		return 0
	}
	return now.Sub(*sub.CreatedAt)
}
