package billingmatrix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/shopspring/decimal"
)

// changeBase is a subscription the change scenarios act on, created now.
type changeBase struct {
	name       string
	cad        cadence
	calendar   bool
	anchorMode string
	tz         string
}

var changeBases = []changeBase{
	{"monthly-anchor-near", cadMonthly, false, anchorNear, "America/New_York"},
	{"monthly-calendar", cadMonthly, true, "", "Asia/Kathmandu"},
	{"quarterly-calendar", cadQuarterly, true, "", "UTC"},
	{"annual-calendar", cadAnnual, true, "", "Asia/Kolkata"},
}

var (
	itemBase   = itemSpec{key: "base", pricing: flatPricing("31"), qty: 3}
	itemArrear = itemSpec{key: "arrear", arrear: true, pricing: flatPricing("1"), qty: 10}
	itemExtra  = itemSpec{key: "extra", pricing: flatPricing("20")}
)

func (cb changeBase) spec(prorate bool, addons []itemSpec) subSpec {
	base, arrear, extra := itemBase, itemArrear, itemExtra
	base.cad, arrear.cad, extra.cad = cb.cad, cb.cad, cb.cad
	items := []itemSpec{base, arrear}
	if cb.cad.months() >= 3 {
		items = append(items, itemSpec{key: "shorter", cad: cadMonthly, pricing: flatPricing("31")})
	}
	if cb.cad.months() < 12 {
		items = append(items, itemSpec{key: "longer", cad: cadAnnual, pricing: flatPricing("365")})
	}
	return subSpec{cad: cb.cad, calendar: cb.calendar, anchorMode: cb.anchorMode, prorate: prorate, tz: cb.tz,
		items: items, addons: addons, extraPrices: []itemSpec{extra}}
}

// addonCadences are the addon price cadences an add or remove exercises on a base: same, shorter, longer, one-time.
func (cb changeBase) addonCadences() map[string]cadence {
	out := map[string]cadence{"same": cb.cad, "onetime": cadOnetime}
	if cb.cad.months() >= 3 {
		out["shorter"] = cadMonthly
	}
	if cb.cad.months() < 12 {
		out["longer"] = cadAnnual
	}
	return out
}

// effectiveAt is a change date 30% into the current period, strictly inside it.
func effectiveAt(p span) time.Time {
	return p.start.Add(time.Duration(float64(p.end.Sub(p.start)) * 0.3)).Truncate(time.Second)
}

// windowAmounts prices an item from at onward within the current period: one entry per item
// window (same/shorter) or the item's own period (longer). Each entry is [charge, window].
func (b *built) remainingWindows(it itemSpec, from time.Time, itemStart time.Time) []struct {
	amount decimal.Decimal
	window span
} {
	s := b.sched
	cost := it.cost()
	var out []struct {
		amount decimal.Decimal
		window span
	}
	if it.cad.longerThan(s.cad) {
		g := newGrid(s.longerItemAnchor(itemStart, it.cad), it.cad, s.loc)
		full := g.full(from.Add(-time.Nanosecond))
		if from.Equal(itemStart) {
			full = g.full(from)
		}
		w := span{start: maxTime(full.start, itemStart), end: full.end}
		if s.end != nil && w.end.After(*s.end) {
			w.end = *s.end
		}
		used := span{start: from, end: w.end}
		out = append(out, struct {
			amount decimal.Decimal
			window span
		}{money(cost.Mul(g.fraction(used))), w})
		return out
	}
	g := newGrid(s.anchor, it.cad, s.loc)
	for _, w := range s.windows(s.firstPeriod(), it.cad) {
		if !w.end.After(from) {
			continue
		}
		used := span{start: maxTime(from, w.start), end: w.end}
		out = append(out, struct {
			amount decimal.Decimal
			window span
		}{money(cost.Mul(g.fraction(used))), w})
	}
	return out
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// billedFor is what the opening invoice charged an item for one of its windows (the oracle's view).
func (b *built) billedFor(it itemSpec, w span) decimal.Decimal {
	s := b.sched
	if it.cad.longerThan(s.cad) {
		g := newGrid(s.longerItemAnchor(s.start, it.cad), it.cad, s.loc)
		amt, _ := fixedCharge(it.cost(), g, w, s.subPeriodStartAt(w.start), s.prorate)
		return money(amt)
	}
	amt, _ := fixedCharge(it.cost(), newGrid(s.anchor, it.cad, s.loc), w, w.start, s.prorate)
	return money(amt)
}

// expectedCredit is the removal/cancel credit for an item from at: each window's unused share, capped at what it billed.
func (b *built) expectedCredit(it itemSpec, at time.Time) (decimal.Decimal, int) {
	if it.onetime() || it.arrear || it.usage || !b.sched.prorate {
		return decimal.Zero, 1
	}
	total, windows := decimal.Zero, b.remainingWindows(it, at, b.sched.start)
	for _, rw := range windows {
		billed := b.billedFor(it, rw.window)
		total = total.Add(decimal.Min(rw.amount, billed))
	}
	return total, len(windows)
}

type association struct {
	ID        string    `json:"id"`
	AddonID   string    `json:"addon_id"`
	StartDate time.Time `json:"start_date"`
}

func (c client) addonAssociations(ctx context.Context, subID string) ([]association, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/subscriptions/"+url.PathEscape(subID)+"/addons/associations", &raw); err != nil {
		return nil, err
	}
	var out []association
	return out, decodeItems(raw, &out)
}

func changeScenarios() []scenario {
	var out []scenario
	for _, cb := range changeBases {
		for _, prorate := range []bool{true, false} {
			for label, cad := range cb.addonCadences() {
				out = append(out, addonAddScenario(cb, prorate, label, cad), addonRemoveScenario(cb, prorate, label, cad))
			}
			out = append(out, lineItemAPIScenario(cb, prorate), cancelScenario(cb, prorate))
		}
		for _, change := range []string{"qty-up", "qty-down", "price-up"} {
			for _, target := range []string{"base", "shorter", "longer"} {
				if (target == "shorter" && cb.cad.months() < 3) || (target == "longer" && cb.cad.months() >= 12) {
					continue
				}
				out = append(out, lineItemChangeScenario(cb, change, target))
			}
		}
		out = append(out, arrearChangeScenario(cb, true), arrearChangeScenario(cb, false))
	}
	return out
}

func addonAddScenario(cb changeBase, prorate bool, label string, cad cadence) scenario {
	name := fmt.Sprintf("change/addon-add/%s/%s/%s", cb.name, label, behaviorName(prorate))
	sc := scenario{family: FamilyChange, name: name}
	if cad.period == periodOnetime && !prorate {
		sc.knownIssue = "one-time advance item attached mid-period with none is never billed (design follow-up 7)"
	}
	sc.run = func(ctx context.Context, e *env) error {
		b, err := e.build(ctx, cb.spec(prorate, nil))
		if err != nil {
			return err
		}
		c := e.client()
		it := itemSpec{key: "addon-" + label, cad: cad, pricing: flatPricing("40")}
		af, err := e.fx.addon(ctx, addonDef{item: it})
		if err != nil {
			return fmt.Errorf("addon fixture: %w", err)
		}
		addonID := af.addonID
		at := effectiveAt(b.sched.firstPeriod())
		resp, err := c.modify(ctx, b.sub.ID, map[string]any{
			"type": "addon",
			"addon_params": map[string]any{"action": "add", "add": map[string]any{
				"addon_id": addonID, "start_date": at.Format(time.RFC3339), "proration_behavior": behaviorName(prorate),
			}},
		}, false)
		if err != nil {
			return fmt.Errorf("attach addon: %w", err)
		}
		want, lines := decimal.Zero, 1
		switch {
		case it.onetime():
			want = it.cost()
		case prorate:
			windows := b.remainingWindows(it, at, at)
			for _, rw := range windows {
				want = want.Add(rw.amount)
			}
			lines = len(windows)
		}
		got, _ := resp.charges()
		e.moneyLines("attach charge", want, got, lines)
		if !prorate && !it.onetime() {
			e.truth("none: no charge invoice at attach", len(resp.ChangedResources.Invoices) == 0)
		}
		return nil
	}
	return sc
}

func addonRemoveScenario(cb changeBase, prorate bool, label string, cad cadence) scenario {
	name := fmt.Sprintf("change/addon-remove/%s/%s/%s", cb.name, label, behaviorName(prorate))
	return scenario{family: FamilyChange, name: name, run: func(ctx context.Context, e *env) error {
		it := itemSpec{key: "addon-" + label, cad: cad, pricing: flatPricing("40")}
		b, err := e.build(ctx, cb.spec(prorate, []itemSpec{it}))
		if err != nil {
			return err
		}
		c := e.client()
		assocs, err := c.addonAssociations(ctx, b.sub.ID)
		if err != nil || len(assocs) == 0 {
			return fmt.Errorf("addon association: %v (found %d)", err, len(assocs))
		}
		at := effectiveAt(b.sched.firstPeriod())
		resp, err := c.modify(ctx, b.sub.ID, map[string]any{
			"type": "addon",
			"addon_params": map[string]any{"action": "remove", "remove": map[string]any{
				"addon_association_id": assocs[0].ID, "effective_date": at.Format(time.RFC3339),
				"proration_behavior": behaviorName(prorate),
			}},
		}, false)
		if err != nil {
			return fmt.Errorf("remove addon: %w", err)
		}
		want, lines := b.expectedCredit(it, at)
		e.moneyLines("removal credit", want, resp.credits(), lines)
		return nil
	}}
}

func lineItemChangeScenario(cb changeBase, change, target string) scenario {
	name := fmt.Sprintf("change/line-item-change/%s/%s/%s", cb.name, target, change)
	return scenario{family: FamilyChange, name: name, run: func(ctx context.Context, e *env) error {
		b, err := e.build(ctx, cb.spec(true, nil))
		if err != nil {
			return err
		}
		it := b.items[target]
		li := b.sub.lineItemFor(b.price(target))
		if li == nil {
			return fmt.Errorf("line item for %s not on subscription", target)
		}
		at := effectiveAt(b.sched.firstPeriod())
		entry := map[string]any{"id": li.ID, "effective_date": at.Format(time.RFC3339)}
		next := it
		switch change {
		case "qty-up":
			next.qty = it.qty + 2
			entry["quantity"] = next.qty
		case "qty-down":
			next.qty = 1
			entry["quantity"] = next.qty
		case "price-up":
			next.pricing = flatPricing("50")
			entry["amount"] = "50"
		}
		resp, err := e.client().modify(ctx, b.sub.ID, map[string]any{
			"type": "line_item_change", "line_item_change_params": map[string]any{"line_items": []any{entry}},
		}, false)
		if err != nil {
			return fmt.Errorf("line item change: %w", err)
		}
		want := decimal.Zero
		newWindows, oldWindows := b.remainingWindows(next, at, b.sched.start), b.remainingWindows(it, at, b.sched.start)
		for _, rw := range newWindows {
			want = want.Add(rw.amount)
		}
		for _, rw := range oldWindows {
			want = want.Sub(rw.amount)
		}
		charges, _ := resp.charges()
		e.moneyLines("net change", want, charges.Sub(resp.credits()), len(newWindows)+len(oldWindows))
		return nil
	}}
}

// arrearChangeScenario changes an arrear item's quantity mid-period and previews the period:
// each version should bill for the time it was live.
func arrearChangeScenario(cb changeBase, prorate bool) scenario {
	name := fmt.Sprintf("change/line-item-change/%s/arrear-qty-up/%s", cb.name, behaviorName(prorate))
	sc := scenario{family: FamilyChange, name: name}
	if !prorate {
		sc.knownIssue = "mid-period arrear line_item_change on a none sub bills the old version in full and the new one at $0 (prorateFixedCharge uses the sub's behavior)"
	}
	sc.run = func(ctx context.Context, e *env) error {
		b, err := e.build(ctx, cb.spec(prorate, nil))
		if err != nil {
			return err
		}
		li := b.sub.lineItemFor(b.price("arrear"))
		if li == nil {
			return fmt.Errorf("arrear line item missing")
		}
		first := b.sched.firstPeriod()
		at := effectiveAt(first)
		if _, err := e.client().modify(ctx, b.sub.ID, map[string]any{
			"type": "line_item_change", "line_item_change_params": map[string]any{"line_items": []any{
				map[string]any{"id": li.ID, "quantity": 20, "effective_date": at.Format(time.RFC3339)},
			}},
		}, false); err != nil {
			return fmt.Errorf("line item change: %w", err)
		}
		inv, err := e.client().preview(ctx, b.sub.ID, &first)
		if err != nil {
			return fmt.Errorf("preview: %w", err)
		}
		g := b.sched.grid()
		unit := decimal.NewFromInt(1)
		want := money(unit.Mul(decimal.NewFromInt(10)).Mul(g.fraction(span{start: first.start, end: at}))).
			Add(money(unit.Mul(decimal.NewFromInt(20)).Mul(g.fraction(span{start: at, end: first.end}))))
		got, _ := inv.byPrice()
		e.moneyLines("arrear period total across versions", want, got[b.price("arrear")], 2)
		return nil
	}
	return sc
}

// lineItemAPIScenario adds a price through the line item API mid-period, then deletes the base item.
func lineItemAPIScenario(cb changeBase, prorate bool) scenario {
	name := fmt.Sprintf("change/line-item-api/%s/%s", cb.name, behaviorName(prorate))
	return scenario{family: FamilyChange, name: name, run: func(ctx context.Context, e *env) error {
		b, err := e.build(ctx, cb.spec(prorate, nil))
		if err != nil {
			return err
		}
		c := e.client()
		at := effectiveAt(b.sched.firstPeriod())
		before, err := c.subscriptionInvoices(ctx, b.sub.ID)
		if err != nil {
			return fmt.Errorf("list invoices: %w", err)
		}
		var added lineItem
		if err := c.post(ctx, "/subscriptions/"+url.PathEscape(b.sub.ID)+"/lineitems", map[string]any{
			"price_id": b.price("extra"), "quantity": 1, "start_date": at.Format(time.RFC3339),
			"proration_behavior": behaviorName(prorate),
		}, &added); err != nil {
			return fmt.Errorf("add line item: %w", err)
		}
		after, err := c.subscriptionInvoices(ctx, b.sub.ID)
		if err != nil {
			return fmt.Errorf("list invoices: %w", err)
		}
		want, lines := decimal.Zero, 1
		if prorate {
			windows := b.remainingWindows(b.items["extra"], at, at)
			for _, rw := range windows {
				want = want.Add(rw.amount)
			}
			lines = len(windows)
		}
		e.moneyLines("line item API add charge", want, newInvoiceTotal(before, after), lines)

		li := b.sub.lineItemFor(b.price("base"))
		if li == nil {
			return fmt.Errorf("base line item missing")
		}
		txBefore, _ := c.walletCredits(ctx, b.customerID)
		if err := c.api.Do(ctx, http.MethodDelete, "/subscriptions/lineitems/"+url.PathEscape(li.ID), map[string]any{
			"effective_from": at.Format(time.RFC3339), "proration_behavior": behaviorName(prorate),
		}, nil); err != nil {
			return fmt.Errorf("delete line item: %w", err)
		}
		txAfter, err := c.walletCredits(ctx, b.customerID)
		if err != nil {
			return fmt.Errorf("wallet transactions: %w", err)
		}
		wantCredit, lines := b.expectedCredit(b.items["base"], at)
		e.moneyLines("line item API delete credit", wantCredit, newCredits(txBefore, txAfter), lines)
		return nil
	}}
}

func newInvoiceTotal(before, after []invoice) decimal.Decimal {
	seen := map[string]bool{}
	for _, inv := range before {
		seen[inv.ID] = true
	}
	total := decimal.Zero
	for _, inv := range after {
		if !seen[inv.ID] && inv.InvoiceStatus != "VOIDED" {
			total = total.Add(inv.Subtotal)
		}
	}
	return total
}

// newCredits sums wallet credits that appeared between two reads. Proration credits share the
// credit-grant transaction reason, so the reason cannot tell them apart.
func newCredits(before, after []walletTxn) decimal.Decimal {
	seen := map[string]bool{}
	for _, t := range before {
		seen[t.ID] = true
	}
	total := decimal.Zero
	for _, t := range after {
		if !seen[t.ID] && t.Type == "credit" {
			total = total.Add(t.Amount)
		}
	}
	return total
}

// cancelScenario cancels immediately: every advance fixed item is credited per window (D1, 3.3).
func cancelScenario(cb changeBase, prorate bool) scenario {
	name := fmt.Sprintf("change/cancel-immediate/%s/%s", cb.name, behaviorName(prorate))
	return scenario{family: FamilyChange, name: name, run: func(ctx context.Context, e *env) error {
		addon := itemSpec{key: "addon", cad: cb.cad, pricing: flatPricing("11")}
		b, err := e.build(ctx, cb.spec(prorate, []itemSpec{addon}))
		if err != nil {
			return err
		}
		resp, err := e.client().cancel(ctx, b.sub.ID, map[string]any{
			"cancellation_type": "immediate", "proration_behavior": behaviorName(prorate), "reason": "e2eprobe billing matrix",
		})
		if err != nil {
			return fmt.Errorf("cancel: %w", err)
		}
		gotByPrice := map[string]decimal.Decimal{}
		for _, d := range resp.ProrationDetails {
			gotByPrice[d.PriceID] = gotByPrice[d.PriceID].Add(d.CreditAmount)
		}
		total, totalLines := decimal.Zero, 0
		for _, it := range b.subItems() {
			want, lines := b.expectedCredit(it, resp.EffectiveDate)
			total, totalLines = total.Add(want), totalLines+lines
			e.moneyLines("cancel credit "+it.key, want, gotByPrice[b.price(it.key)], lines)
		}
		e.moneyLines("cancel total_credit_amount", total, resp.TotalCreditAmount, totalLines)
		if !prorate {
			e.count("none: proration_details rows", 0, len(resp.ProrationDetails))
		}
		return nil
	}}
}
