package scenarios

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/ledger"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// planSoftLimit is the allowance provisioning puts on every eligible meter.
const planSoftLimit = 100

// registerEntitlements adds the entitlement scenarios to cat.
func registerEntitlements(cat *scenario.Catalog) {
	for _, r := range []registration{
		entry("entitlement-usage", e2eprobe.KindProbe, 30*time.Minute, func(d scenario.Deps) scenario.Scenario { return newEntitlementUsage(d) }, e2eprobe.TraitSoftLimit, e2eprobe.TraitEvents),
		entry("entitlement-enforcement", e2eprobe.KindFlow, time.Hour, func(d scenario.Deps) scenario.Scenario { return newEntitlementEnforcement(d) }, e2eprobe.TraitSoftLimit).ingests(overLimitEvents),
		entry("grant-inheritance", e2eprobe.KindFlow, time.Hour, func(d scenario.Deps) scenario.Scenario { return newGrantInheritance(d) }, e2eprobe.TraitAdditiveGrant).ingests(grantEvents),
	} {
		cat.Register(r.info, r.build)
	}
}

// entitlementUsage reads the entitled persona's usage summary: the count
// feature must carry the plan's soft limit and at least the usage the ledger
// says was sent this billing period.
type entitlementUsage struct {
	// settle is how old a tick must be before its usage is expected to have landed.
	settle time.Duration
}

func newEntitlementUsage(d scenario.Deps) *entitlementUsage {
	return &entitlementUsage{settle: 2 * d.Tick}
}

func (*entitlementUsage) Name() string        { return "entitlement-usage" }
func (*entitlementUsage) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }
func (*entitlementUsage) Requires() []e2eprobe.Trait {
	return []e2eprobe.Trait{e2eprobe.TraitSoftLimit, e2eprobe.TraitEvents}
}

func (s *entitlementUsage) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	if gap := p.Gap(e2eprobe.TraitSoftLimit); gap != "" {
		return sc.Skip("persona", gap)
	}
	customerID := p.CustomerID()
	sc.Attr("customer_id", customerID)
	sc.Attr("subscription_id", p.SubscriptionID())
	featureID := sc.World().Fixtures().FeatureID("count")
	resp, err := sc.Client().Customers().GetUsageSummary(ctx, dtos.GetCustomerUsageSummaryRequest{CustomerID: &customerID, FeatureIds: []string{featureID}})
	if err != nil {
		return sc.Fail("usage_summary", err)
	}
	summary, ok := featureSummary(resp, featureID)
	if !ok {
		sc.Truth("count feature missing from usage summary", false)
		return sc.Result("assert_entitlement_usage")
	}
	sc.Truth("count entitlement is a soft limit", summary.IsSoftLimit != nil && *summary.IsSoftLimit)
	limit := 0
	if summary.TotalLimit != nil {
		limit = int(*summary.TotalLimit)
	}
	sc.Count("count entitlement limit", planSoftLimit, limit)

	got := usageOf(summary)
	if profile, ok := ledger.Lookup(p.Spec().Events); ok {
		// Only ticks wholly inside the period and old enough to have been
		// aggregated count, so the bound stays a bound.
		window := ledger.Window{Start: periodStart(resp, sc.Now()).Add(sc.Tick()), End: sc.Now().Add(-s.settle)}
		ticks := sc.Ledger().Ticks(p.Name(), window)
		count, _ := e2eprobe.MeterByKey("count")
		expected := ledger.NewOracle(profile, sc.Tick()).Expect(count, ticks)
		sc.Truth(fmt.Sprintf("current usage %s >= %s sent since period start", got, expected), got.GreaterThanOrEqual(expected))
	}
	return sc.Result("assert_entitlement_usage")
}

// periodStart is the summary's billing period start, else the start of the current UTC month.
func periodStart(resp *dtos.GetCustomerUsageSummaryResponse, now time.Time) time.Time {
	if p := resp.GetCustomerUsageSummaryResponse().GetPeriod(); p != nil && p.StartTime != nil {
		return p.StartTime.UTC()
	}
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// featureSummary finds the feature's entry in a usage summary.
func featureSummary(resp *dtos.GetCustomerUsageSummaryResponse, featureID string) (types.FeatureUsageSummary, bool) {
	for _, f := range resp.GetCustomerUsageSummaryResponse().GetFeatures() {
		if f.Feature != nil && f.Feature.ID != nil && *f.Feature.ID == featureID {
			return f, true
		}
	}
	return types.FeatureUsageSummary{}, false
}

// usageOf is the summary's current usage; a missing or unreadable value is zero.
func usageOf(f types.FeatureUsageSummary) decimal.Decimal {
	if f.CurrentUsage == nil {
		return decimal.Zero
	}
	v, err := decimal.NewFromString(*f.CurrentUsage)
	if err != nil {
		return decimal.Zero
	}
	return v
}

// awaitUsage polls the customer's usage summary until the feature reports want
// or the timeout passes, returning the last summary seen.
func awaitUsage(ctx context.Context, sc *scenario.Context, customerID, featureID string, want decimal.Decimal, every, timeout time.Duration) (types.FeatureUsageSummary, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var last types.FeatureUsageSummary
	for {
		resp, err := sc.Client().Customers().GetUsageSummary(ctx, dtos.GetCustomerUsageSummaryRequest{CustomerID: &customerID, FeatureIds: []string{featureID}})
		if err != nil {
			return last, err
		}
		if f, ok := featureSummary(resp, featureID); ok {
			last = f
			if usageOf(f).Equal(want) {
				return f, nil
			}
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-deadline.C:
			return last, nil
		case <-time.After(every):
		}
	}
}

// burst is n events of the meter for the clone, all stamped now; ids derive
// from the clone, the scenario and the second so a resend is idempotent.
func burst(sc *scenario.Context, eph *world.Ephemeral, m e2eprobe.Meter, n int, props map[string]string) []types.IngestEventRequest {
	now := sc.Now().UTC()
	ts := now.Format(time.RFC3339Nano)
	source := "e2eprobe"
	out := make([]types.IngestEventRequest, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-%s-%d-%d", eph.ExternalID(), sc.Scenario(), now.Unix(), i)
		p := map[string]string{"e2eprobe": "true", "e2eprobe_run_id": sc.RunID()}
		for k, v := range props {
			p[k] = v
		}
		out = append(out, types.IngestEventRequest{
			EventID:            &id,
			EventName:          m.EventName,
			ExternalCustomerID: eph.ExternalID(),
			Source:             &source,
			Timestamp:          &ts,
			Properties:         p,
		})
	}
	return out
}
