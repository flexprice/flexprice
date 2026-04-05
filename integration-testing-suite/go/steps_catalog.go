package main

import (
	"context"
	"fmt"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// runCatalogSteps executes Phase 1: Product Catalog.
func (r *SanityRunner) runCatalogSteps(ctx context.Context) {
	r.setPhase("PHASE 1: Product Catalog")
	r.printPhaseHeader(r.phase)

	// ── Create Feature Group ────────────────────────────────────────────
	// SDK: client.Groups.CreateGroup(ctx, types.CreateGroupRequest{...})

	r.run("Create Feature Group", "Groups.CreateGroup", false, func() error {
		req := types.CreateGroupRequest{
			Name:       fmt.Sprintf("sanity-feature-group-%d", ts()),
			EntityType: "feature",
			LookupKey:  fmt.Sprintf("sanity_feat_grp_%d", ts()),
		}

		resp, err := r.client.Groups.CreateGroup(ctx, req)
		if err != nil {
			return err
		}
		group := resp.GroupResponse
		if group == nil || group.ID == nil {
			return fmt.Errorf("create group returned no body")
		}
		r.featureGroupID = *group.ID
		r.lastResult().EntityID = *group.ID
		r.lastResult().Details = fmt.Sprintf("group_id=%s, entity_type=feature", *group.ID)
		return nil
	})

	// ── Create Price Group ──────────────────────────────────────────────
	// SDK: client.Groups.CreateGroup(ctx, types.CreateGroupRequest{...})

	r.run("Create Price Group", "Groups.CreateGroup", false, func() error {
		req := types.CreateGroupRequest{
			Name:       fmt.Sprintf("sanity-price-group-%d", ts()),
			EntityType: "price",
			LookupKey:  fmt.Sprintf("sanity_price_grp_%d", ts()),
		}

		resp, err := r.client.Groups.CreateGroup(ctx, req)
		if err != nil {
			return err
		}
		group := resp.GroupResponse
		if group == nil || group.ID == nil {
			return fmt.Errorf("create group returned no body")
		}
		r.priceGroupID = *group.ID
		r.lastResult().EntityID = *group.ID
		r.lastResult().Details = fmt.Sprintf("group_id=%s, entity_type=price", *group.ID)
		return nil
	})

	// ── Create Metered Feature A (grouped) ──────────────────────────────
	// SDK: client.Features.CreateFeature(ctx, types.CreateFeatureRequest{...})

	r.run("Create Metered Feature A (grouped)", "Features.CreateFeature", false, func() error {
		r.eventNameA = fmt.Sprintf("api_call_%d", ts())

		aggType := types.AggregationTypeSum
		req := types.CreateFeatureRequest{
			Name:      fmt.Sprintf("API Calls %d", ts()),
			Type:      types.FeatureTypeMetered,
			LookupKey: strPtr(fmt.Sprintf("api_calls_%d", ts())),
			Meter: &types.CreateMeterRequest{
				Name:      fmt.Sprintf("api_call_meter_%d", ts()),
				EventName: r.eventNameA,
				Aggregation: types.MeterAggregation{
					Type:  &aggType,
					Field: strPtr("tokens"),
				},
				ResetUsage: types.ResetUsageBillingPeriod,
			},
			GroupID:  strPtr(r.featureGroupID),
			Metadata: map[string]string{"source": "sanity_test"},
		}

		resp, err := r.client.Features.CreateFeature(ctx, req)
		if err != nil {
			return err
		}
		feature := resp.Feature
		if feature == nil || feature.ID == nil {
			return fmt.Errorf("create feature returned no body")
		}
		r.featureAID = *feature.ID
		r.lastResult().EntityID = *feature.ID

		// Extract meter ID.
		if feature.MeterID != nil {
			r.meterAID = *feature.MeterID
		} else if feature.Meter != nil && feature.Meter.ID != nil {
			r.meterAID = *feature.Meter.ID
		}

		r.lastResult().Details = fmt.Sprintf("feat_id=%s, meter_id=%s, event=%s", *feature.ID, r.meterAID, r.eventNameA)
		return nil
	})

	// ── Create Metered Feature B (ungrouped) ────────────────────────────
	// SDK: client.Features.CreateFeature(ctx, types.CreateFeatureRequest{...})

	r.run("Create Metered Feature B", "Features.CreateFeature", false, func() error {
		r.eventNameB = fmt.Sprintf("storage_usage_%d", ts())

		aggType := types.AggregationTypeSum
		req := types.CreateFeatureRequest{
			Name:      fmt.Sprintf("Storage Usage %d", ts()),
			Type:      types.FeatureTypeMetered,
			LookupKey: strPtr(fmt.Sprintf("storage_usage_%d", ts())),
			Meter: &types.CreateMeterRequest{
				Name:      fmt.Sprintf("storage_meter_%d", ts()),
				EventName: r.eventNameB,
				Aggregation: types.MeterAggregation{
					Type:  &aggType,
					Field: strPtr("gb_hours"),
				},
				ResetUsage: types.ResetUsageBillingPeriod,
			},
			Metadata: map[string]string{"source": "sanity_test"},
		}

		resp, err := r.client.Features.CreateFeature(ctx, req)
		if err != nil {
			return err
		}
		feature := resp.Feature
		if feature == nil || feature.ID == nil {
			return fmt.Errorf("create feature returned no body")
		}
		r.featureBID = *feature.ID
		r.lastResult().EntityID = *feature.ID

		if feature.MeterID != nil {
			r.meterBID = *feature.MeterID
		} else if feature.Meter != nil && feature.Meter.ID != nil {
			r.meterBID = *feature.Meter.ID
		}

		r.lastResult().Details = fmt.Sprintf("feat_id=%s, meter_id=%s, event=%s", *feature.ID, r.meterBID, r.eventNameB)
		return nil
	})

	// ── Create Plan ─────────────────────────────────────────────────────
	// SDK: client.Plans.CreatePlan(ctx, types.CreatePlanRequest{...})

	r.run("Create Plan", "Plans.CreatePlan", false, func() error {
		req := types.CreatePlanRequest{
			Name:        fmt.Sprintf("Sanity Plan %d", ts()),
			LookupKey:   strPtr(fmt.Sprintf("sanity_plan_%d", ts())),
			Description: strPtr("Integration test plan with recurring + usage charges"),
		}

		resp, err := r.client.Plans.CreatePlan(ctx, req)
		if err != nil {
			return err
		}
		plan := resp.Plan
		if plan == nil || plan.ID == nil {
			return fmt.Errorf("create plan returned no body")
		}
		r.planID = *plan.ID
		r.lastResult().EntityID = *plan.ID
		r.lastResult().Details = fmt.Sprintf("plan_id=%s, name=%s", *plan.ID, derefStr(plan.Name))
		return nil
	})

	// ── Add Recurring Price 1 (grouped) ─────────────────────────────────
	// SDK: client.Prices.CreatePrice(ctx, types.CreatePriceRequest{...})

	if !r.require(r.planID, "Create Plan", "Add Recurring Price 1 (grouped)") {
		r.skip("Add Recurring Price 2", "depends on Create Plan which failed")
		r.skip("Add Usage Price (Feature A)", "depends on Create Plan which failed")
		r.skip("Add Usage Price (Feature B)", "depends on Create Plan which failed")
		return
	}

	r.run("Add Recurring Price 1 (grouped)", "Prices.CreatePrice", false, func() error {
		req := types.CreatePriceRequest{
			EntityID:           r.planID,
			EntityType:         types.PriceEntityTypePlan,
			Type:               types.PriceTypeFixed,
			BillingModel:       types.BillingModelFlatFee,
			BillingCadence:     types.BillingCadenceRecurring,
			BillingPeriod:      types.BillingPeriodMonthly,
			BillingPeriodCount: int64Ptr(1),
			InvoiceCadence:     types.InvoiceCadenceArrear,
			PriceUnitType:      types.PriceUnitTypeFiat,
			Amount:             strPtr("49.99"),
			Currency:           "USD",
			DisplayName:        strPtr("Platform Fee (Grouped)"),
			GroupID:            strPtr(r.priceGroupID),
		}

		resp, err := r.client.Prices.CreatePrice(ctx, req)
		if err != nil {
			return err
		}
		price := resp.Price
		if price == nil || price.ID == nil {
			return fmt.Errorf("create price returned no body")
		}
		r.priceRecurr1 = *price.ID
		r.lastResult().EntityID = *price.ID
		r.lastResult().Details = fmt.Sprintf("price_id=%s, amount=$49.99, grouped, plan=%s", *price.ID, r.planID)
		return nil
	})

	// ── Add Recurring Price 2 (ungrouped) ───────────────────────────────

	r.run("Add Recurring Price 2", "Prices.CreatePrice", false, func() error {
		req := types.CreatePriceRequest{
			EntityID:           r.planID,
			EntityType:         types.PriceEntityTypePlan,
			Type:               types.PriceTypeFixed,
			BillingModel:       types.BillingModelFlatFee,
			BillingCadence:     types.BillingCadenceRecurring,
			BillingPeriod:      types.BillingPeriodMonthly,
			BillingPeriodCount: int64Ptr(1),
			InvoiceCadence:     types.InvoiceCadenceArrear,
			PriceUnitType:      types.PriceUnitTypeFiat,
			Amount:             strPtr("19.99"),
			Currency:           "USD",
			DisplayName:        strPtr("Base Fee"),
		}

		resp, err := r.client.Prices.CreatePrice(ctx, req)
		if err != nil {
			return err
		}
		price := resp.Price
		if price == nil || price.ID == nil {
			return fmt.Errorf("create price returned no body")
		}
		r.priceRecurr2 = *price.ID
		r.lastResult().EntityID = *price.ID
		r.lastResult().Details = fmt.Sprintf("price_id=%s, amount=$19.99, ungrouped, plan=%s", *price.ID, r.planID)
		return nil
	})

	// ── Add Usage Price for Feature A ────────────────────────────────────

	if !r.require(r.meterAID, "Feature A meter", "Add Usage Price (Feature A)") {
		r.skip("Add Usage Price (Feature B)", "depends on prior steps")
		return
	}

	r.run("Add Usage Price (Feature A)", "Prices.CreatePrice", false, func() error {
		req := types.CreatePriceRequest{
			EntityID:           r.planID,
			EntityType:         types.PriceEntityTypePlan,
			Type:               types.PriceTypeUsage,
			BillingModel:       types.BillingModelFlatFee,
			BillingCadence:     types.BillingCadenceRecurring,
			BillingPeriod:      types.BillingPeriodMonthly,
			BillingPeriodCount: int64Ptr(1),
			InvoiceCadence:     types.InvoiceCadenceArrear,
			PriceUnitType:      types.PriceUnitTypeFiat,
			Amount:             strPtr("0.01"),
			Currency:           "USD",
			MeterID:            strPtr(r.meterAID),
			DisplayName:        strPtr("API Call Usage"),
		}

		resp, err := r.client.Prices.CreatePrice(ctx, req)
		if err != nil {
			return err
		}
		price := resp.Price
		if price == nil || price.ID == nil {
			return fmt.Errorf("create price returned no body")
		}
		r.priceUsageA = *price.ID
		r.lastResult().EntityID = *price.ID
		r.lastResult().Details = fmt.Sprintf("price_id=%s, per_unit=$0.01/token, meter=%s", *price.ID, r.meterAID)
		return nil
	})

	// ── Add Usage Price for Feature B ────────────────────────────────────

	if !r.require(r.meterBID, "Feature B meter", "Add Usage Price (Feature B)") {
		return
	}

	r.run("Add Usage Price (Feature B)", "Prices.CreatePrice", false, func() error {
		req := types.CreatePriceRequest{
			EntityID:           r.planID,
			EntityType:         types.PriceEntityTypePlan,
			Type:               types.PriceTypeUsage,
			BillingModel:       types.BillingModelFlatFee,
			BillingCadence:     types.BillingCadenceRecurring,
			BillingPeriod:      types.BillingPeriodMonthly,
			BillingPeriodCount: int64Ptr(1),
			InvoiceCadence:     types.InvoiceCadenceArrear,
			PriceUnitType:      types.PriceUnitTypeFiat,
			Amount:             strPtr("0.05"),
			Currency:           "USD",
			MeterID:            strPtr(r.meterBID),
			DisplayName:        strPtr("Storage Usage"),
		}

		resp, err := r.client.Prices.CreatePrice(ctx, req)
		if err != nil {
			return err
		}
		price := resp.Price
		if price == nil || price.ID == nil {
			return fmt.Errorf("create price returned no body")
		}
		r.priceUsageB = *price.ID
		r.lastResult().EntityID = *price.ID
		r.lastResult().Details = fmt.Sprintf("price_id=%s, per_unit=$0.05/gb_hour, meter=%s", *price.ID, r.meterBID)
		return nil
	})
}
