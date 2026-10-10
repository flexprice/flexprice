package billingmatrix

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// expectRejected fails the scenario unless err is a 4xx.
func (e *env) expectRejected(label string, err error) {
	if err == nil {
		e.fail("%s: accepted, want a 4xx", label)
		return
	}
	if !isClientError(err) {
		e.fail("%s: want a 4xx, got %v", label, err)
	}
}

func (e *env) expectAccepted(label string, err error) bool {
	if err != nil {
		e.fail("%s: rejected: %v", label, err)
		return false
	}
	return true
}

// rawSub provisions a customer and a plan with the given prices, and returns a create body
// the scenario then edits.
func (e *env) rawSub(ctx context.Context, tz string, sub cadence, prices []itemSpec) (map[string]any, map[string]string, error) {
	c := e.client()
	u := e.uniq()
	planID, err := c.createPlan(ctx, "e2eprobe-bm "+u)
	if err != nil {
		return nil, nil, fmt.Errorf("create plan: %w", err)
	}
	ids := map[string]string{}
	var include []string
	for _, it := range prices {
		id, err := c.createPrice(ctx, it.priceBody("PLAN", planID, ""))
		if err != nil {
			return nil, nil, fmt.Errorf("create price %s: %w", it.key, err)
		}
		ids[it.key] = id
		include = append(include, id)
	}
	custID, err := c.createCustomer(ctx, "e2eprobe-cust-eph-bm-"+u, tz, e.runID, "ephemeral-billing-matrix")
	if err != nil {
		return nil, nil, fmt.Errorf("create customer: %w", err)
	}
	return map[string]any{
		"customer_id": custID, "plan_id": planID, "currency": "usd",
		"start_date":     startNow().Format(time.RFC3339),
		"billing_period": sub.period, "billing_period_count": sub.n(), "billing_cycle": "anniversary",
		"include_price_ids": include, "trial_period_days": 0,
	}, ids, nil
}

func validationScenarios() []scenario {
	monthlyFlat := itemSpec{key: "monthly", cad: cadMonthly, pricing: flatPricing("31")}
	quarterlyFlat := itemSpec{key: "quarterly", cad: cadQuarterly, pricing: flatPricing("90")}
	v := func(name string, run func(ctx context.Context, e *env) error) scenario {
		return scenario{family: FamilyValidation, name: "validation/" + name, run: run}
	}
	return []scenario{
		v("anchor-past-one-period-rejected", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "UTC", cadMonthly, []itemSpec{monthlyFlat})
			if err != nil {
				return err
			}
			start, _ := time.Parse(time.RFC3339, body["start_date"].(string))
			body["billing_anchor"] = addMonthsClamped(start, 1).Add(24 * time.Hour).Format(time.RFC3339)
			_, err = e.client().createSubscription(ctx, body)
			e.expectRejected("anchor one period + 1 day after start (D6)", err)
			return nil
		}),
		v("anchor-exactly-one-period-accepted", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "America/New_York", cadMonthly, []itemSpec{monthlyFlat})
			if err != nil {
				return err
			}
			start, _ := time.Parse(time.RFC3339, body["start_date"].(string))
			ny, _ := time.LoadLocation("America/New_York")
			anchor := addMonthsClamped(start.In(ny), 1).UTC()
			body["billing_anchor"] = anchor.Format(time.RFC3339)
			sub, err := e.client().createSubscription(ctx, body)
			if e.expectAccepted("anchor exactly one period after start", err) {
				e.instant("current_period_end", anchor, sub.CurrentPeriodEnd)
			}
			return nil
		}),
		v("anchor-with-calendar-rejected", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "UTC", cadMonthly, []itemSpec{monthlyFlat})
			if err != nil {
				return err
			}
			body["billing_cycle"] = "calendar"
			body["billing_anchor"] = time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
			_, err = e.client().createSubscription(ctx, body)
			e.expectRejected("billing_anchor with a calendar cycle", err)
			return nil
		}),
		v("include-2-month-on-quarterly-rejected", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "UTC", cadQuarterly, []itemSpec{quarterlyFlat, {key: "two-month", cad: cadMonthly2, pricing: flatPricing("60")}})
			if err != nil {
				return err
			}
			_, err = e.client().createSubscription(ctx, body)
			e.expectRejected("MONTHLY×2 price on a quarterly sub (D12)", err)
			return nil
		}),
		v("include-weekly-on-monthly-rejected", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "UTC", cadMonthly, []itemSpec{monthlyFlat, {key: "weekly", cad: cadWeekly, pricing: flatPricing("7")}})
			if err != nil {
				return err
			}
			_, err = e.client().createSubscription(ctx, body)
			e.expectRejected("weekly price on a monthly sub (D12)", err)
			return nil
		}),
		v("line-item-api-2-month-on-quarterly-rejected", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "UTC", cadQuarterly, []itemSpec{quarterlyFlat})
			if err != nil {
				return err
			}
			sub, err := e.client().createSubscription(ctx, body)
			if !e.expectAccepted("create quarterly sub", err) {
				return nil
			}
			twoMonth := itemSpec{key: "two-month", cad: cadMonthly2, pricing: flatPricing("60")}
			priceID, err := e.client().createPrice(ctx, twoMonth.priceBody("PLAN", body["plan_id"].(string), ""))
			if err != nil {
				return fmt.Errorf("create price: %w", err)
			}
			err = e.client().post(ctx, "/subscriptions/"+url.PathEscape(sub.ID)+"/lineitems", map[string]any{"price_id": priceID, "quantity": 1}, nil)
			e.expectRejected("line item API: MONTHLY×2 price on a quarterly sub", err)
			return nil
		}),
		v("addon-incompatible-cadence-rejected", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "UTC", cadQuarterly, []itemSpec{quarterlyFlat})
			if err != nil {
				return err
			}
			sub, err := e.client().createSubscription(ctx, body)
			if !e.expectAccepted("create quarterly sub", err) {
				return nil
			}
			c := e.client()
			af, err := e.fx.addon(ctx, addonDef{item: itemSpec{key: "two-month", cad: cadMonthly2, pricing: flatPricing("60")}})
			if err != nil {
				return fmt.Errorf("addon fixture: %w", err)
			}
			addonID := af.addonID
			_, err = c.modify(ctx, sub.ID, map[string]any{"type": "addon", "addon_params": map[string]any{
				"action": "add", "add": map[string]any{"addon_id": addonID, "change_at": "immediate"},
			}}, false)
			e.expectRejected("addon whose only price is MONTHLY×2 on a quarterly sub", err)
			return nil
		}),
		v("backdated-create-prorations-rejected", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "UTC", cadMonthly, []itemSpec{monthlyFlat})
			if err != nil {
				return err
			}
			body["start_date"] = startNow().AddDate(0, 0, -3).Format(time.RFC3339)
			body["proration_behavior"] = "create_prorations"
			_, err = e.client().createSubscription(ctx, body)
			e.expectRejected("backdated start with create_prorations (1.7)", err)

			body["proration_behavior"] = "none"
			_, err = e.client().createSubscription(ctx, body)
			e.expectAccepted("backdated start with none", err)
			return nil
		}),
		v("line-item-change-outside-period-rejected", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "UTC", cadMonthly, []itemSpec{monthlyFlat})
			if err != nil {
				return err
			}
			sub, err := e.client().createSubscription(ctx, body)
			if !e.expectAccepted("create monthly sub", err) || len(sub.LineItems) == 0 {
				return nil
			}
			for label, at := range map[string]time.Time{
				"before current period start": sub.CurrentPeriodStart.Add(-time.Hour),
				"at current period end":       sub.CurrentPeriodEnd,
			} {
				_, err := e.client().modify(ctx, sub.ID, map[string]any{
					"type": "line_item_change", "line_item_change_params": map[string]any{"line_items": []any{
						map[string]any{"id": sub.LineItems[0].ID, "quantity": 2, "effective_date": at.Format(time.RFC3339)},
					}},
				}, true)
				e.expectRejected("line_item_change "+label+" (D8)", err)
			}
			return nil
		}),
		v("plan-change-v2-mixed-cadence-create-prorations-accepted", func(ctx context.Context, e *env) error {
			body, _, err := e.rawSub(ctx, "UTC", cadQuarterly, []itemSpec{quarterlyFlat, {key: "monthly", cad: cadMonthly, pricing: flatPricing("31")}})
			if err != nil {
				return err
			}
			body["proration_behavior"] = "create_prorations"
			sub, err := e.client().createSubscription(ctx, body)
			if !e.expectAccepted("create mixed-cadence sub with create_prorations (3.4)", err) {
				return nil
			}
			c := e.client()
			target, err := c.createPlan(ctx, "e2eprobe-bm target "+e.uniq())
			if err != nil {
				return fmt.Errorf("create target plan: %w", err)
			}
			if _, err := c.createPrice(ctx, itemSpec{key: "target", cad: cadQuarterly, pricing: flatPricing("120")}.priceBody("PLAN", target, "")); err != nil {
				return fmt.Errorf("create target price: %w", err)
			}
			err = c.post(ctx, "/subscriptions/"+url.PathEscape(sub.ID)+"/change/v2/preview", map[string]any{
				"target_plan_id": target, "proration_behavior": "create_prorations", "change_at": "immediate",
			}, nil)
			e.expectAccepted("plan change v2 preview on a mixed-cadence sub with create_prorations", err)
			return nil
		}),
		v("trial-ends-in-local-days", func(ctx context.Context, e *env) error {
			for _, cycle := range []string{"calendar", "anniversary"} {
				body, _, err := e.rawSub(ctx, "America/New_York", cadMonthly, []itemSpec{monthlyFlat})
				if err != nil {
					return err
				}
				body["billing_cycle"] = cycle
				body["trial_period_days"] = 14
				sub, err := e.client().createSubscription(ctx, body)
				if !e.expectAccepted("create "+cycle+" trial sub", err) {
					continue
				}
				ny, _ := time.LoadLocation("America/New_York")
				start, _ := time.Parse(time.RFC3339, body["start_date"].(string))
				if sub.TrialEnd == nil {
					e.fail("%s: trial_end missing", cycle)
					continue
				}
				e.instant(cycle+" trial_end = start + 14 local days (1.3)", start.In(ny).AddDate(0, 0, 14), *sub.TrialEnd)
			}
			return nil
		}),
	}
}
