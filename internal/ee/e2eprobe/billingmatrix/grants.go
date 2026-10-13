package billingmatrix

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

const reasonCreditGrant = "SUBSCRIPTION_CREDIT_GRANT"

// grantBases are the schedules grant scenarios run on: calendar stubs and anchor-ahead stubs.
var grantBases = []changeBase{
	{"monthly-calendar", cadMonthly, true, "", "America/New_York"},
	{"monthly-anchor-near", cadMonthly, false, anchorNear, "UTC"},
	{"quarterly-calendar", cadQuarterly, true, "", "Asia/Kathmandu"},
	{"annual-anchor-near", cadAnnual, false, anchorNear, "Asia/Kolkata"},
}

func grantScenarios() []scenario {
	var out []scenario
	for _, gb := range grantBases {
		for _, prorate := range []bool{true, false} {
			kinds := []string{"same-cadence", "onetime"}
			if gb.cad.equal(cadMonthly) {
				kinds = append(kinds, "annual-on-monthly")
			}
			for _, kind := range kinds {
				out = append(out, creationGrantScenario(gb, prorate, kind))
			}
			out = append(out, addonCreditGrantScenario(gb, prorate), addonEntitlementGrantScenario(gb, prorate))
		}
	}
	return out
}

// pollCredits waits briefly for grant top-ups, which settle just after the create call commits.
func (e *env) pollCredits(ctx context.Context, customerID string, want int) ([]walletTxn, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		txns, err := e.client().walletCredits(ctx, customerID)
		var grants []walletTxn
		for _, t := range txns {
			if t.TransactionReason == reasonCreditGrant {
				grants = append(grants, t)
			}
		}
		if err == nil && len(grants) >= want {
			return grants, nil
		}
		if time.Now().After(deadline) {
			return grants, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func creditTotal(txns []walletTxn) decimal.Decimal {
	t := decimal.Zero
	for _, x := range txns {
		t = t.Add(x.CreditAmount)
	}
	return t
}

// creationGrantScenario: a subscription credit grant sent at creation (D4). A grant on the sub's
// cadence is prorated over the first period and renews on the billing date; others grant in full.
func creationGrantScenario(gb changeBase, prorate bool, kind string) scenario {
	name := fmt.Sprintf("grants/creation/%s/%s/%s", gb.name, kind, behaviorName(prorate))
	return scenario{family: FamilyGrants, name: name, run: func(ctx context.Context, e *env) error {
		credits := decimal.NewFromInt(1000)
		grant := map[string]any{
			"name": "e2eprobe-bm-cg", "scope": "SUBSCRIPTION", "credits": credits.String(),
			"cadence": "RECURRING", "period": gb.cad.period, "period_count": 1, "expiration_type": "NEVER",
		}
		grantCad := gb.cad
		switch kind {
		case "onetime":
			grant["cadence"] = "ONETIME"
			delete(grant, "period")
			delete(grant, "period_count")
		case "annual-on-monthly":
			grant["period"] = periodAnnual
			grantCad = cadAnnual
		}
		spec := gb.spec(prorate, nil)
		spec.creditGrants = []map[string]any{grant}
		b, err := e.build(ctx, spec)
		if err != nil {
			return err
		}
		txns, err := e.pollCredits(ctx, b.customerID, 1)
		if err != nil {
			return fmt.Errorf("wallet transactions: %w", err)
		}
		first := b.sched.firstPeriod()
		want := credits
		follows := kind == "same-cadence"
		if follows && prorate {
			want = credits.Mul(b.sched.grid().fraction(first))
		}
		e.money("first grant application", want, creditTotal(txns))

		if kind == "onetime" {
			return nil
		}
		upcoming, err := e.client().upcomingGrants(ctx, b.sub.ID)
		if err != nil {
			return fmt.Errorf("upcoming grants: %w", err)
		}
		if len(upcoming) == 0 {
			e.fail("no upcoming grant application")
			return nil
		}
		next := newGrid(b.sched.start, grantCad, b.sched.loc).at(1)
		if follows {
			next = first.end
		}
		e.instant("next grant application", next, earliest(upcoming))
		return nil
	}}
}

func earliest(apps []creditGrantApplication) time.Time {
	t := apps[0].ScheduledFor
	for _, a := range apps[1:] {
		if a.ScheduledFor.Before(t) {
			t = a.ScheduledFor
		}
	}
	return t
}

// attachAddonNow attaches addonID immediately and returns the association's start.
func (e *env) attachAddonNow(ctx context.Context, b *built, addonID string, prorate bool) (time.Time, *modifyResp, error) {
	resp, err := e.client().modify(ctx, b.sub.ID, map[string]any{
		"type": "addon",
		"addon_params": map[string]any{"action": "add", "add": map[string]any{
			"addon_id": addonID, "change_at": "immediate", "proration_behavior": behaviorName(prorate),
		}},
	}, false)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("attach addon: %w", err)
	}
	for _, a := range resp.ChangedResources.AddonAssociations {
		if a.AddonID == addonID {
			return a.StartDate, resp, nil
		}
	}
	return time.Time{}, resp, fmt.Errorf("attach addon: no association in response")
}

// addonCreditGrantScenario: an addon's recurring grant on the sub cadence, attached mid-period, gets the charge's fraction.
func addonCreditGrantScenario(gb changeBase, prorate bool) scenario {
	name := fmt.Sprintf("grants/addon-credit-grant/%s/%s", gb.name, behaviorName(prorate))
	return scenario{family: FamilyGrants, name: name, run: func(ctx context.Context, e *env) error {
		b, err := e.build(ctx, gb.spec(prorate, nil))
		if err != nil {
			return err
		}
		credits := decimal.NewFromInt(600)
		af, err := e.fx.addon(ctx, addonDef{
			item: itemSpec{key: "addon-cg", cad: gb.cad, pricing: flatPricing("10")},
			creditGrant: map[string]any{
				"name": "e2eprobe-bm-addon-cg", "credits": credits.String(),
				"cadence": "RECURRING", "period": gb.cad.period, "period_count": 1, "expiration_type": "NEVER",
			},
		})
		if err != nil {
			return fmt.Errorf("addon fixture: %w", err)
		}
		addonID := af.addonID
		at, _, err := e.attachAddonNow(ctx, b, addonID, prorate)
		if err != nil {
			return err
		}
		txns, err := e.pollCredits(ctx, b.customerID, 1)
		if err != nil {
			return fmt.Errorf("wallet transactions: %w", err)
		}
		want := credits
		if prorate {
			want = credits.Mul(b.sched.grid().fraction(span{start: at, end: b.sched.firstPeriod().end}))
		}
		e.money("addon grant first application", want, creditTotal(txns))
		return nil
	}}
}

// addonEntitlementGrantScenario: an additive subscription-period entitlement grant on an addon attached
// mid-period opens with quota × the charge's fraction (D4, full precision).
func addonEntitlementGrantScenario(gb changeBase, prorate bool) scenario {
	name := fmt.Sprintf("grants/addon-entitlement-grant/%s/%s", gb.name, behaviorName(prorate))
	return scenario{family: FamilyGrants, name: name, run: func(ctx context.Context, e *env) error {
		b, err := e.build(ctx, gb.spec(prorate, nil))
		if err != nil {
			return err
		}
		c := e.client()
		quota := decimal.NewFromInt(100)
		af, err := e.fx.addon(ctx, addonDef{
			item:       itemSpec{key: "addon-eg", cad: gb.cad, pricing: flatPricing("10")},
			grantQuota: quota.String(),
		})
		if err != nil {
			return fmt.Errorf("addon fixture: %w", err)
		}
		feature, err := e.fx.feature(ctx, fixtureKeyPrefix+"eg")
		if err != nil {
			return err
		}
		addonID, featureID, ent := af.addonID, feature.featureID, idOnly{ID: af.entitlementID}
		if _, _, err := e.attachAddonNow(ctx, b, addonID, prorate); err != nil {
			return err
		}
		ents, err := c.subscriptionEntitlements(ctx, b.sub.ID, featureID)
		if err != nil {
			return fmt.Errorf("subscription entitlements: %w", err)
		}
		var found *allowance
		for _, f := range ents.Features {
			if f.Entitlement.GrantState == nil {
				continue
			}
			for i, a := range f.Entitlement.GrantState.Allowances {
				if a.EntitlementID == ent.ID {
					found = &f.Entitlement.GrantState.Allowances[i]
				}
			}
		}
		if found == nil || found.ValidFrom == nil || found.ValidTo == nil {
			e.fail("no entitlement grant allowance for the addon entitlement")
			return nil
		}
		first := b.sched.firstPeriod()
		e.instant("grant window end", first.end, *found.ValidTo)
		want := quota
		if prorate {
			want = quota.Mul(b.sched.grid().fraction(span{start: *found.ValidFrom, end: *found.ValidTo}))
		}
		if want.Sub(found.Quota).Abs().GreaterThan(decimal.RequireFromString("0.001")) {
			e.fail("grant quota: want %s, got %s", want.Round(6), found.Quota)
		}
		return nil
	}}
}
