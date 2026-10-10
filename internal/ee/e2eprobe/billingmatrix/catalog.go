package billingmatrix

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
)

var (
	cadMonthly   = cadence{period: periodMonthly, count: 1}
	cadMonthly2  = cadence{period: periodMonthly, count: 2}
	cadMonthly6  = cadence{period: periodMonthly, count: 6}
	cadQuarterly = cadence{period: periodQuarterly, count: 1}
	cadHalfYear  = cadence{period: periodHalfYear, count: 1}
	cadAnnual    = cadence{period: periodAnnual, count: 1}
	cadWeekly    = cadence{period: periodWeekly, count: 1}
	cadDaily     = cadence{period: periodDaily, count: 1}
	cadWeekly2   = cadence{period: periodWeekly, count: 2}
	cadDaily3    = cadence{period: periodDaily, count: 3}
	cadOnetime   = cadence{period: periodOnetime, count: 1}

	subCadences = []cadence{cadMonthly, cadQuarterly, cadHalfYear, cadAnnual, cadMonthly2, cadWeekly, cadDaily, cadWeekly2, cadDaily3}

	// timezones covers UTC, a DST zone, a +5:45 zone, a half-hour zone and a southern-hemisphere DST zone.
	timezones = []string{"UTC", "America/New_York", "Asia/Kathmandu", "Asia/Kolkata", "Australia/Sydney"}
)

func i64(v int64) *int64 { return &v }

// pricingModels are the fixed-charge money models the base item rotates through, with the
// quantity each is billed at.
var pricingModels = []struct {
	name string
	p    pricing
	qty  int64
}{
	{"flat", flatPricing("31"), 3},
	{"package", pricing{model: modelPackage, amount: decimal.NewFromInt(10), divideBy: 5}, 12},
	{"volume", pricing{model: modelTiered, tierMode: tierVolume, tiers: []tier{
		{upTo: i64(10), unitAmount: decimal.NewFromInt(10)},
		{unitAmount: decimal.NewFromInt(8), flatAmount: decimal.NewFromInt(5)},
	}}, 12},
	{"slab", pricing{model: modelTiered, tierMode: tierSlab, tiers: []tier{
		{upTo: i64(10), unitAmount: decimal.NewFromInt(10)},
		{unitAmount: decimal.NewFromInt(8)},
	}}, 12},
}

// itemsFor builds the item mix for a sub cadence: same-cadence advance (model rotated) and
// arrear fixed, usage, one-time, shorter and longer items where D12 allows them, and one
// creation-time addon.
func itemsFor(sub cadence, idx int) (items, addons []itemSpec) {
	m := pricingModels[idx%len(pricingModels)]
	items = []itemSpec{
		{key: "base-" + m.name, cad: sub, pricing: m.p, qty: m.qty},
		{key: "arrear", cad: sub, arrear: true, pricing: flatPricing("7"), qty: 2},
		{key: "usage", cad: sub, usage: true},
		{key: "onetime", cad: cadOnetime, pricing: flatPricing("5")},
	}
	if sub.months() == 0 {
		return items, []itemSpec{{key: "addon-same", cad: sub, pricing: flatPricing("11")}}
	}
	if sub.months() >= 2 {
		items = append(items, itemSpec{key: "shorter-monthly", cad: cadMonthly, pricing: flatPricing("31")})
	}
	if sub.months() == 12 {
		items = append(items, itemSpec{key: "shorter-quarterly-arrear", cad: cadQuarterly, arrear: true, pricing: flatPricing("90")})
	}
	if sub.months() < 12 {
		items = append(items,
			itemSpec{key: "longer-annual", cad: cadAnnual, pricing: flatPricing("365")},
			itemSpec{key: "longer-annual-usage", cad: cadAnnual, usage: true},
		)
	}
	if sub.equal(cadMonthly) {
		items = append(items, itemSpec{key: "longer-monthlyx6-arrear", cad: cadMonthly6, arrear: true, pricing: flatPricing("60")})
	}
	if sub.equal(cadQuarterly) {
		items = append(items, itemSpec{key: "longer-halfyear", cad: cadHalfYear, pricing: flatPricing("180")})
	}
	addonCad := sub
	if sub.months() >= 3 {
		addonCad = cadMonthly
	}
	return items, []itemSpec{{key: "addon-" + addonCad.period, cad: addonCad, pricing: flatPricing("11")}}
}

type cycleVariant struct {
	name     string
	calendar bool
	anchor   string
}

var cycleVariants = []cycleVariant{
	{"calendar", true, ""},
	{"anniversary", false, anchorDefault},
	{"anchor-near", false, anchorNear},
	{"anchor-monthend", false, anchorMonthEnd},
	{"anchor-before-start", false, anchorBefore},
}

func behaviorName(prorate bool) string {
	if prorate {
		return "create_prorations"
	}
	return "none"
}

// knownIssueFor names a product deviation from the spec an opening row would hit. Daily and
// weekly NextBillingDate still steps from the start instead of using the anchor grid: with
// count > 1 and an anchor off the start, or a daily anchor later on the start's local day, the
// first period ends one step past the anchor and create_prorations over-charges.
func knownIssueFor(spec subSpec) string {
	if spec.cad.days() == 0 {
		return ""
	}
	if (spec.cad.n() > 1 && (spec.calendar || spec.anchorMode != anchorDefault)) ||
		(spec.cad.period == periodDaily && spec.anchorMode == anchorNear) {
		return "daily/weekly first period ends one step past the anchor (NextBillingDate ignores the anchor grid)"
	}
	return ""
}

// lifecycleSpecs is the creation matrix: sub cadence × cycle/anchor × proration, with
// timezone and pricing model rotated across rows.
func lifecycleSpecs() []struct {
	name string
	spec subSpec
} {
	var out []struct {
		name string
		spec subSpec
	}
	idx := 0
	for _, sc := range subCadences {
		for _, cv := range cycleVariants {
			if cv.anchor == anchorMonthEnd && sc.months() == 0 {
				continue
			}
			for _, prorate := range []bool{true, false} {
				items, addons := itemsFor(sc, idx)
				tz := timezones[idx%len(timezones)]
				name := fmt.Sprintf("%s×%d/%s/%s/%s", sc.period, sc.n(), cv.name, behaviorName(prorate), tz)
				out = append(out, struct {
					name string
					spec subSpec
				}{name, subSpec{
					cad: sc, calendar: cv.calendar, anchorMode: cv.anchor, prorate: prorate, tz: tz,
					items: items, addons: addons,
				}})
				idx++
			}
		}
	}
	return out
}

func buildCatalog() []scenario {
	var all []scenario
	for _, row := range lifecycleSpecs() {
		spec := row.spec
		all = append(all,
			scenario{family: FamilyOpening, name: "opening/" + row.name, knownIssue: knownIssueFor(spec), run: func(ctx context.Context, e *env) error {
				b, err := e.build(ctx, spec)
				if err != nil {
					return err
				}
				e.checkSchedule(b)
				e.checkOpening(b)
				return nil
			}},
			scenario{family: FamilyRenewal, name: "renewal/" + row.name, run: func(ctx context.Context, e *env) error {
				b, err := e.build(ctx, spec)
				if err != nil {
					return err
				}
				ps := b.sched.periods(40)
				for _, k := range b.previewPeriods() {
					e.checkPreview(ctx, b, ps[k], fmt.Sprintf("period %d", k))
				}
				return nil
			}},
		)
	}
	all = append(all, endDateScenarios()...)
	all = append(all, changeScenarios()...)
	all = append(all, grantScenarios()...)
	all = append(all, validationScenarios()...)
	return all
}

// endDateScenarios cover a short last period: the sub ends 40% into its third period.
func endDateScenarios() []scenario {
	var out []scenario
	for i, sc := range []cadence{cadMonthly, cadQuarterly, cadAnnual} {
		for _, prorate := range []bool{true, false} {
			items, _ := itemsFor(sc, i)
			spec := subSpec{cad: sc, calendar: i%2 == 0, prorate: prorate, tz: timezones[(i+1)%len(timezones)], items: items, endInPeriod: 3}
			out = append(out, scenario{
				family: FamilyRenewal,
				name:   fmt.Sprintf("renewal/end-date/%s/%s", sc.period, behaviorName(prorate)),
				run: func(ctx context.Context, e *env) error {
					b, err := e.build(ctx, spec)
					if err != nil {
						return err
					}
					ps := b.sched.periods(3)
					for k := range ps {
						e.checkPreview(ctx, b, ps[k], fmt.Sprintf("period %d", k))
					}
					return nil
				},
			})
		}
	}
	return out
}
