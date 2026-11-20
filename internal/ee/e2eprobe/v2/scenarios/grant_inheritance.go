package scenarios

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// grantEvents is the burst sent on the granted meter; usage multiplies it by the meter's factor.
const grantEvents = 200

// grantInheritance checks a clone inherits the plan's additive grant and that
// usage on the granted feature rolls up through the meter's multiplier.
type grantInheritance struct {
	every, timeout time.Duration
}

func newGrantInheritance(scenario.Deps) *grantInheritance {
	return &grantInheritance{every: 10 * time.Second, timeout: 3 * time.Minute}
}

func (*grantInheritance) Name() string        { return "grant-inheritance" }
func (*grantInheritance) Kind() e2eprobe.Kind { return e2eprobe.KindFlow }
func (*grantInheritance) Requires() []e2eprobe.Trait {
	return []e2eprobe.Trait{e2eprobe.TraitAdditiveGrant}
}

func (s *grantInheritance) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	if gap := sc.Persona().Gap(e2eprobe.TraitAdditiveGrant); gap != "" {
		return sc.Skip("persona", gap)
	}
	fx := sc.World().Fixtures()
	grantID := fx.GrantEntitlementID()
	if grantID == "" {
		return sc.Skip("grant", "plan has no additive grant entitlement")
	}
	sc.Attr("entitlement_id", grantID)
	raw, err := sc.Client().Entitlements().GetRaw(ctx, grantID)
	if err != nil {
		return sc.Fail("get_grant", err)
	}
	sc.Truth("grant echoes additive quantity grant", raw != nil && raw.GrantMeasure == "quantity" && raw.AggregationMode == "additive" && raw.IsEnabled)

	eph, err := sc.Clone(ctx)
	if err != nil {
		return sc.Fail("clone", err)
	}
	sc.Attr("ephemeral_external_id", eph.ExternalID())
	sc.Attr("customer_id", eph.CustomerID())
	sc.Attr("subscription_id", eph.SubscriptionID())

	meter, _ := e2eprobe.MeterByKey("sum_multiplier")
	featureID := fx.FeatureID("sum_multiplier")
	ents, err := sc.Client().Subscriptions().GetEntitlements(ctx, eph.SubscriptionID(), []string{featureID})
	if err != nil {
		return sc.Fail("subscription_entitlements", err)
	}
	sc.Truth("clone inherits the grant feature", listsFeature(ents, featureID))

	events := burst(sc, eph, meter, grantEvents, map[string]string{"amount": "1"})
	if err := sc.Client().Events().IngestBulk(ctx, types.BulkIngestEventRequest{Events: events}); err != nil {
		return sc.Fail("ingest_bulk", err)
	}
	want := decimal.NewFromInt(grantEvents).Mul(decimal.RequireFromString(meter.Multiplier))
	summary, err := awaitUsage(ctx, sc, eph.CustomerID(), featureID, want, s.every, s.timeout)
	if err != nil {
		return sc.Fail("usage_summary", err)
	}
	sc.Quantity("current usage", want, usageOf(summary))
	return sc.Result("assert_grant")
}

// listsFeature reports whether a subscription's entitlements include the feature.
func listsFeature(resp *dtos.GetSubscriptionEntitlementsResponse, featureID string) bool {
	for _, f := range resp.GetSubscriptionEntitlementsResponse().GetFeatures() {
		if f.Feature != nil && f.Feature.ID != nil && *f.Feature.ID == featureID {
			return true
		}
	}
	return false
}
