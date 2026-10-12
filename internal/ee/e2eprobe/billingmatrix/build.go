package billingmatrix

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// itemSpec is one price on the scenario's plan or on its own addon.
type itemSpec struct {
	key     string
	cad     cadence // period ONETIME for one-time items
	arrear  bool
	usage   bool
	pricing pricing
	qty     int64
}

func (it itemSpec) onetime() bool { return it.cad.period == periodOnetime }

func (it itemSpec) quantity() decimal.Decimal { return decimal.NewFromInt(max(it.qty, 1)) }

func (it itemSpec) cost() decimal.Decimal { return it.pricing.cost(it.quantity()) }

// priceBody is the POST /prices request. Prices start a day back so they are live before any
// subscription the scenario starts now; a later price start would delay the line item.
func (it itemSpec) priceBody(entityType, entityID, meterID string) map[string]any {
	body := map[string]any{
		"start_date":           time.Now().UTC().AddDate(0, 0, -1).Truncate(time.Second).Format(time.RFC3339),
		"currency":             "usd",
		"entity_type":          entityType,
		"entity_id":            entityID,
		"type":                 "FIXED",
		"price_unit_type":      "FIAT",
		"billing_period":       it.cad.period,
		"billing_period_count": it.cad.n(),
		"invoice_cadence":      "ADVANCE",
		"display_name":         it.key,
	}
	if it.arrear || it.usage {
		body["invoice_cadence"] = "ARREAR"
	}
	if it.usage {
		body["type"] = "USAGE"
		body["meter_id"] = meterID
		body["billing_model"] = modelFlatFee
		body["amount"] = "0.01"
		return body
	}
	p := it.pricing
	body["billing_model"] = p.model
	switch p.model {
	case modelPackage:
		body["amount"] = p.amount.String()
		body["transform_quantity"] = map[string]any{"divide_by": p.divideBy, "round": "up"}
	case modelTiered:
		body["tier_mode"] = p.tierMode
		tiers := make([]map[string]any, 0, len(p.tiers))
		for _, t := range p.tiers {
			tb := map[string]any{"unit_amount": t.unitAmount.String(), "flat_amount": t.flatAmount.String()}
			if t.upTo != nil {
				tb["up_to"] = *t.upTo
			}
			tiers = append(tiers, tb)
		}
		body["tiers"] = tiers
	default:
		body["amount"] = p.amount.String()
	}
	return body
}

// Anchor variants for anniversary subs.
const (
	anchorDefault  = ""         // anchor = start
	anchorNear     = "near"     // start + ~0.37 period
	anchorMonthEnd = "monthend" // next local month end at the start's clock
	anchorBefore   = "before"   // 10 days before the start
)

// subSpec describes a subscription to create and every item on it.
type subSpec struct {
	cad          cadence
	calendar     bool
	anchorMode   string
	prorate      bool
	tz           string
	items        []itemSpec // plan prices, all included
	addons       []itemSpec // each on its own addon, attached at creation
	creditGrants []map[string]any
	endInPeriod  int        // > 0: end_date falls 40% into that period (1-based)
	extraPrices  []itemSpec // plan prices left off the sub, for the line item API
}

// built is a created subscription with the oracle's view of its schedule.
type built struct {
	spec       subSpec
	sched      subSchedule
	sub        *subscriptionResp
	customerID string
	planID     string
	priceOf    map[string]string // item key → price id
	addonOf    map[string]string // addon item key → addon id
	items      map[string]itemSpec
}

func (b *built) price(key string) string { return b.priceOf[key] }

func (c client) createMeteredFeature(ctx context.Context, key string) (featureID, meterID string, err error) {
	var out struct {
		ID      string `json:"id"`
		MeterID string `json:"meter_id"`
	}
	err = c.post(ctx, "/features", map[string]any{
		"name": key, "lookup_key": key, "type": "metered",
		"meter": map[string]any{
			"name": key, "event_name": key, "reset_usage": "BILLING_PERIOD",
			"aggregation": map[string]any{"type": "SUM", "field": "units"},
		},
	}, &out)
	c.track.add(kindFeature, out.ID)
	return out.ID, out.MeterID, err
}

func (e *env) client() client { return client{api: e.api, track: e.track} }

// uniq is a short unique suffix for names and lookup keys within a run.
func (e *env) uniq() string {
	return fmt.Sprintf("%s-%d", strings.ReplaceAll(e.scenario, "/", "-"), time.Now().UnixNano())
}

// startNow is the scenario start: now, at whole seconds so oracle and server agree exactly.
func startNow() time.Time { return time.Now().UTC().Truncate(time.Second) }

// anchorFor resolves the anchor the request sends (nil for calendar and default anniversary).
func anchorFor(mode string, start time.Time, cad cadence, loc *time.Location) *time.Time {
	var a time.Time
	switch mode {
	case anchorNear:
		full := newGrid(start, cad, loc).at(1).Sub(start)
		a = start.Add(time.Duration(float64(full) * 0.37)).Truncate(time.Second)
	case anchorMonthEnd:
		l := start.In(loc)
		h, mi, s := l.Clock()
		a = time.Date(l.Year(), l.Month()+1, 0, h, mi, s, 0, loc)
		if !a.After(start) {
			a = time.Date(l.Year(), l.Month()+2, 0, h, mi, s, 0, loc)
		}
	case anchorBefore:
		a = start.AddDate(0, 0, -10)
	default:
		return nil
	}
	a = a.UTC()
	return &a
}

// build provisions plan, prices, addons and customer, then creates the subscription.
func (e *env) build(ctx context.Context, spec subSpec) (*built, error) {
	c := e.client()
	loc, err := time.LoadLocation(spec.tz)
	if err != nil {
		return nil, err
	}
	u := e.uniq()
	b := &built{spec: spec, priceOf: map[string]string{}, addonOf: map[string]string{}, items: map[string]itemSpec{}}

	_, meterID, err := e.fx.usageMeter(ctx)
	if err != nil {
		return nil, fmt.Errorf("usage meter: %w", err)
	}
	if b.planID, err = c.createPlan(ctx, "e2eprobe-bm "+u); err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}
	var include []string
	for _, it := range append(append([]itemSpec{}, spec.items...), spec.extraPrices...) {
		id, err := c.createPrice(ctx, it.priceBody("PLAN", b.planID, meterID))
		if err != nil {
			return nil, fmt.Errorf("create price %s: %w", it.key, err)
		}
		b.priceOf[it.key], b.items[it.key] = id, it
	}
	for _, it := range spec.items {
		include = append(include, b.priceOf[it.key])
	}

	start := startNow()
	var addons []map[string]any
	for _, it := range spec.addons {
		af, err := e.fx.addon(ctx, addonDef{item: it})
		if err != nil {
			return nil, fmt.Errorf("addon fixture %s: %w", it.key, err)
		}
		b.items[it.key], b.addonOf[it.key] = it, af.addonID
		addons = append(addons, map[string]any{"addon_id": af.addonID})
	}

	ext := "e2eprobe-cust-eph-bm-" + u
	if b.customerID, err = c.createCustomer(ctx, ext, spec.tz, e.runID, "ephemeral-billing-matrix"); err != nil {
		return nil, fmt.Errorf("create customer: %w", err)
	}

	sched := subSchedule{start: start, cad: spec.cad, loc: loc, calendar: spec.calendar, prorate: spec.prorate}
	body := map[string]any{
		"customer_id":          b.customerID,
		"plan_id":              b.planID,
		"currency":             "usd",
		"start_date":           start.Format(time.RFC3339),
		"billing_period":       spec.cad.period,
		"billing_period_count": spec.cad.n(),
		"billing_cycle":        "anniversary",
		"proration_behavior":   "none",
		"include_price_ids":    include,
		"trial_period_days":    0,
	}
	if spec.prorate {
		body["proration_behavior"] = "create_prorations"
	}
	if spec.calendar {
		body["billing_cycle"] = "calendar"
		sched.anchor = calendarAnchor(start, spec.cad.period, loc)
	} else if a := anchorFor(spec.anchorMode, start, spec.cad, loc); a != nil {
		body["billing_anchor"] = a.Format(time.RFC3339)
		sched.anchor = *a
	} else {
		sched.anchor = start
	}
	if spec.endInPeriod > 0 {
		ps := sched.periods(spec.endInPeriod)
		last := ps[len(ps)-1]
		end := last.start.Add(time.Duration(float64(last.end.Sub(last.start)) * 0.4)).Truncate(time.Second).UTC()
		body["end_date"] = end.Format(time.RFC3339)
		sched.end = &end
	}
	var overrides []map[string]any
	for _, it := range spec.items {
		if !it.usage && it.qty > 1 {
			overrides = append(overrides, map[string]any{"price_id": b.priceOf[it.key], "quantity": it.qty})
		}
	}
	if len(overrides) > 0 {
		body["override_line_items"] = overrides
	}
	if len(addons) > 0 {
		body["addons"] = addons
	}
	if len(spec.creditGrants) > 0 {
		body["credit_grants"] = spec.creditGrants
	}

	if b.sub, err = c.createSubscription(ctx, body); err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}
	// A quantity override bills against a new subscription-scoped price; follow it by display name.
	for _, li := range b.sub.LineItems {
		if _, ok := b.items[li.DisplayName]; ok {
			b.priceOf[li.DisplayName] = li.PriceID
		}
	}
	b.sched = sched
	return b, nil
}

// checkSchedule compares the created sub's anchor, first period and stored line item cadences to the oracle.
func (e *env) checkSchedule(b *built) {
	e.instant("billing_anchor", b.sched.anchor, b.sub.BillingAnchor)
	e.instant("current_period_end", b.sched.firstPeriod().end, b.sub.CurrentPeriodEnd)
	if b.sub.Timezone != b.spec.tz {
		e.fail("timezone: want %s, got %s", b.spec.tz, b.sub.Timezone)
	}
	for key, it := range b.items {
		li := b.sub.lineItemFor(b.priceOf[key])
		if li == nil {
			if !containsKey(b.spec.extraPrices, key) {
				e.fail("line item for %s missing on subscription", key)
			}
			continue
		}
		e.count("billing_period_count of "+key, it.cad.n(), li.BillingPeriodCount)
	}
}

func containsKey(items []itemSpec, key string) bool {
	for _, it := range items {
		if it.key == key {
			return true
		}
	}
	return false
}

// subItems lists the items on the subscription from its start: plan items and creation-time addons.
func (b *built) subItems() []itemSpec {
	return append(append([]itemSpec{}, b.spec.items...), b.spec.addons...)
}

// subPeriodStartAt is the start of the sub period containing t, never before the sub start.
func (s subSchedule) subPeriodStartAt(t time.Time) time.Time {
	p := s.grid().full(t).start
	if p.Before(s.start) {
		return s.start
	}
	return p
}

// expectedLines prices an item over a sub invoice. advance lines bill the period that starts
// in [from, to); arrear lines bill periods ending in (from, to]. Items start with the sub.
func (b *built) expectedLines(it itemSpec, inv span, arrearSide bool) []decimal.Decimal {
	s := b.sched
	cost := it.cost()
	if it.onetime() {
		if !arrearSide && !inv.start.After(s.start) && inv.end.After(s.start) {
			return []decimal.Decimal{cost}
		}
		return nil
	}
	var out []decimal.Decimal
	if it.cad.longerThan(s.cad) {
		g := newGrid(s.longerItemAnchor(s.start, it.cad), it.cad, s.loc)
		for _, ip := range s.longerItemPeriods(s.start, it.cad, 64) {
			edge := ip.start
			in := !edge.Before(inv.start) && edge.Before(inv.end)
			if arrearSide {
				edge = ip.end
				in = edge.After(inv.start) && !edge.After(inv.end)
			}
			if !in {
				continue
			}
			if amt, ok := fixedCharge(cost, g, ip, s.subPeriodStartAt(ip.start), s.prorate); ok {
				out = append(out, money(amt))
			}
		}
		return out
	}
	g := newGrid(s.anchor, it.cad, s.loc)
	for _, w := range s.windows(inv, it.cad) {
		if amt, ok := fixedCharge(cost, g, w, w.start, s.prorate); ok {
			out = append(out, money(amt))
		}
	}
	return out
}

func sum(ds []decimal.Decimal) decimal.Decimal {
	t := decimal.Zero
	for _, d := range ds {
		t = t.Add(d)
	}
	return t
}

// checkOpening compares the opening invoice to the oracle: advance fixed items for the first period.
func (e *env) checkOpening(b *built) {
	got, _ := b.sub.LatestInvoice.byPrice()
	first := b.sched.firstPeriod()
	for _, it := range b.subItems() {
		if it.usage || it.arrear {
			if amt, ok := got[b.price(it.key)]; ok && !amt.IsZero() {
				e.fail("opening invoice: %s (arrear/usage) billed %s up front", it.key, money(amt).StringFixed(2))
			}
			continue
		}
		lines := b.expectedLines(it, first, false)
		e.moneyLines("opening "+it.key, sum(lines), got[b.price(it.key)], len(lines))
	}
}

// checkPreview compares the preview for sub period p: arrear over p, advance for the period after p.
func (e *env) checkPreview(ctx context.Context, b *built, p span, label string) {
	inv, err := e.client().preview(ctx, b.sub.ID, &p)
	if err != nil {
		e.fail("%s: preview failed: %v", label, err)
		return
	}
	got, _ := inv.byPrice()
	next := b.sched.periodAt(p.end)
	for _, it := range b.subItems() {
		if it.usage {
			continue
		}
		var lines []decimal.Decimal
		if it.arrear {
			lines = b.expectedLines(it, p, true)
		} else if !next.empty() {
			lines = b.expectedLines(it, next, false)
		}
		e.moneyLines(fmt.Sprintf("%s %s", label, it.key), sum(lines), got[b.price(it.key)], len(lines))
	}
}

// previewPeriods picks the sub periods worth previewing: the first two, plus those where a
// longer item renews (advance) or closes (arrear), plus one where it must not bill.
func (b *built) previewPeriods() []int {
	ps := b.sched.periods(40)
	pick := map[int]bool{0: true, 1: true}
	for _, it := range b.subItems() {
		if it.usage || !it.cad.longerThan(b.sched.cad) {
			continue
		}
		ips := b.sched.longerItemPeriods(b.sched.start, it.cad, 3)
		for k := 0; k+1 < len(ps); k++ {
			if len(ips) > 1 && ps[k+1].start.Equal(ips[1].start) {
				pick[k] = true
				if k > 2 {
					pick[k-1] = true
				}
			}
			if it.arrear && ps[k].end.Equal(ips[0].end) {
				pick[k] = true
			}
		}
	}
	out := []int{}
	for k := 0; k < len(ps) && len(out) < 6; k++ {
		if pick[k] {
			out = append(out, k)
		}
	}
	return out
}
