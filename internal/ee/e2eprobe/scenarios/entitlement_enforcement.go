package scenarios

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// overLimitEvents puts a clone half again past the plan's soft limit.
const overLimitEvents = 150

// entitlementEnforcement pushes a clone past the plan's soft limit and checks
// the usage summary keeps counting: a soft limit never blocks ingest.
type entitlementEnforcement struct {
	every, timeout time.Duration
}

func newEntitlementEnforcement(scenario.Deps) *entitlementEnforcement {
	return &entitlementEnforcement{every: 10 * time.Second, timeout: 3 * time.Minute}
}

func (*entitlementEnforcement) Name() string        { return "entitlement-enforcement" }
func (*entitlementEnforcement) Kind() e2eprobe.Kind { return e2eprobe.KindFlow }
func (*entitlementEnforcement) Requires() []e2eprobe.Trait {
	return []e2eprobe.Trait{e2eprobe.TraitSoftLimit}
}

func (s *entitlementEnforcement) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	if gap := sc.Persona().Gap(e2eprobe.TraitSoftLimit); gap != "" {
		return sc.Skip("persona", gap)
	}
	eph, err := sc.Clone(ctx)
	if err != nil {
		return sc.Fail("clone", err)
	}
	sc.Attr("ephemeral_external_id", eph.ExternalID())
	sc.Attr("customer_id", eph.CustomerID())
	sc.Attr("subscription_id", eph.SubscriptionID())

	count, _ := e2eprobe.MeterByKey("count")
	if err := sc.Client().Events().IngestBulk(ctx, types.BulkIngestEventRequest{Events: burst(sc, eph, count, overLimitEvents, nil)}); err != nil {
		return sc.Fail("ingest_bulk", err)
	}
	want := decimal.NewFromInt(overLimitEvents)
	summary, err := awaitUsage(ctx, sc, eph.CustomerID(), sc.World().Fixtures().FeatureID("count"), want, s.every, s.timeout)
	if err != nil {
		return sc.Fail("usage_summary", err)
	}
	got := usageOf(summary)
	sc.Quantity("current usage", want, got)
	sc.Truth("usage exceeds the soft limit", got.GreaterThan(decimal.NewFromInt(planSoftLimit)))
	sc.Truth("limit is soft", summary.IsSoftLimit != nil && *summary.IsSoftLimit)
	return sc.Result("assert_enforcement")
}
