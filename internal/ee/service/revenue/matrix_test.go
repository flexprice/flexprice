package revenue

import (
	"context"
	"fmt"
	"github.com/flexprice/flexprice/internal/ee/service"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// matrixLine declares one usage line of the pricing-matrix fixture: its meter
// aggregation, price shape, per-day events, and the decomposition the rollup
// must produce for it.
type matrixLine struct {
	key         string
	aggregation types.AggregationType
	buildPrice  func(ctx context.Context, planID, meterID, priceID string) *price.Price
	// eventsForDay returns the quantities fired on day d (0-based).
	eventsForDay func(d int) []decimal.Decimal
	// usageLimit configures a legacy entitlement with this free quantity.
	usageLimit *int64

	wantMode   types.DecompositionMode
	wantAmount string
}

func matrixFlatPrice(unit string) func(ctx context.Context, planID, meterID, priceID string) *price.Price {
	return func(ctx context.Context, planID, meterID, priceID string) *price.Price {
		return &price.Price{
			ID:                 priceID,
			Amount:             decimal.RequireFromString(unit),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           planID,
			Type:               types.PRICE_TYPE_USAGE,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			MeterID:            meterID,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}
}

func matrixTieredPrice(mode types.BillingTier) func(ctx context.Context, planID, meterID, priceID string) *price.Price {
	return func(ctx context.Context, planID, meterID, priceID string) *price.Price {
		upTo := uint64(1000)
		return &price.Price{
			ID:                 priceID,
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           planID,
			Type:               types.PRICE_TYPE_USAGE,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_TIERED,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			TierMode:           mode,
			Tiers: price.JSONBTiers{
				{UpTo: &upTo, UnitAmount: decimal.RequireFromString("0.01")},
				{UpTo: nil, UnitAmount: decimal.RequireFromString("0.005")},
			},
			MeterID:   meterID,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
	}
}

func matrixPackagePrice(ctx context.Context, planID, meterID, priceID string) *price.Price {
	return &price.Price{
		ID:                 priceID,
		Amount:             decimal.NewFromInt(1),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_PACKAGE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		TransformQuantity:  price.JSONBTransformQuantity{DivideBy: 250, Round: types.ROUND_UP},
		MeterID:            meterID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
}

// oneEvent fires a single event of qty per day.
func oneEvent(qty int64) func(d int) []decimal.Decimal {
	return func(int) []decimal.Decimal { return []decimal.Decimal{decimal.NewFromInt(qty)} }
}

// matrixLines is the pricing/metering matrix: every aggregation and pricing
// strategy the rollup classifies, each with a hand-computed expected charge
// over 30 days.
func matrixLines() []matrixLine {
	return []matrixLine{
		{
			// 100/day × 30 = 3000 × $0.01
			key: "flat", aggregation: types.AggregationSum,
			buildPrice: matrixFlatPrice("0.01"), eventsForDay: oneEvent(100),
			wantMode: types.Marginal, wantAmount: "30",
		},
		{
			// 3000 units: 1000×$0.01 + 2000×$0.005 — graduated tiers stay daily.
			key: "slab", aggregation: types.AggregationSum,
			buildPrice: matrixTieredPrice(types.BILLING_TIER_SLAB), eventsForDay: oneEvent(100),
			wantMode: types.Marginal, wantAmount: "20",
		},
		{
			// 3000 units all re-rated at the final tier: 3000×$0.005.
			key: "volume", aggregation: types.AggregationSum,
			buildPrice: matrixTieredPrice(types.BILLING_TIER_VOLUME), eventsForDay: oneEvent(100),
			wantMode: types.PeriodOnly, wantAmount: "15",
		},
		{
			// ceil(3000/250) = 12 packages × $1 — package steps are day-additive.
			key: "pkg", aggregation: types.AggregationSum,
			buildPrice: matrixPackagePrice, eventsForDay: oneEvent(100),
			wantMode: types.Marginal, wantAmount: "12",
		},
		{
			// Last reading 100+29 = 129 × $0.02 — LATEST is not day-additive.
			key: "latest", aggregation: types.AggregationLatest,
			buildPrice: matrixFlatPrice("0.02"),
			eventsForDay: func(d int) []decimal.Decimal {
				return []decimal.Decimal{decimal.NewFromInt(int64(100 + d))}
			},
			wantMode: types.PeriodOnly, wantAmount: "2.58",
		},
		{
			// Peak 500 on day 15 × $0.02 — MAX is not day-additive either.
			key: "maxm", aggregation: types.AggregationMax,
			buildPrice: matrixFlatPrice("0.02"),
			eventsForDay: func(d int) []decimal.Decimal {
				if d == 15 {
					return []decimal.Decimal{decimal.NewFromInt(500)}
				}
				return []decimal.Decimal{decimal.NewFromInt(100)}
			},
			wantMode: types.PeriodOnly, wantAmount: "10",
		},
		{
			// 2 events/day × 30 = 60 events × $0.05.
			key: "cnt", aggregation: types.AggregationCount,
			buildPrice: matrixFlatPrice("0.05"),
			eventsForDay: func(int) []decimal.Decimal {
				return []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(1)}
			},
			wantMode: types.Marginal, wantAmount: "3",
		},
		{
			// 3000 used, 1500 entitled free → 1500 billable × $0.01.
			key: "ent", aggregation: types.AggregationSum,
			buildPrice: matrixFlatPrice("0.01"), eventsForDay: oneEvent(100),
			usageLimit: lo.ToPtr(int64(1500)),
			wantMode:   types.Marginal, wantAmount: "15",
		},
	}
}

// seedMatrixSubscription builds one subscription with one line item per
// matrixLines entry (meter + price + optional legacy entitlement) and seeds
// 30 days of events for each.
func (s *RevenueRollupSuite) seedMatrixSubscription(ctx context.Context, lines []matrixLine) {
	s.periodStart = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEndExclusive := s.periodStart.AddDate(0, 0, 30)
	s.periodEnd = periodEndExclusive

	cust := &customer.Customer{
		ID:         "cust_matrix",
		ExternalID: "ext_matrix",
		Name:       "Pricing Matrix",
		Email:      "matrix@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{ID: "plan_matrix", Name: "Matrix Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	s.sub = &subscription.Subscription{
		ID:                 "sub_matrix",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          s.periodStart,
		BillingAnchor:      periodEndExclusive,
		CurrentPeriodStart: s.periodStart,
		CurrentPeriodEnd:   periodEndExclusive,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	var lineItems []*subscription.SubscriptionLineItem
	var records []*events.MeterUsage
	for _, ml := range lines {
		meterID := "meter_matrix_" + ml.key
		mtr := &meter.Meter{
			ID:          meterID,
			Name:        "Matrix " + ml.key,
			EventName:   "evt_matrix_" + ml.key,
			Aggregation: meter.Aggregation{Type: ml.aggregation},
			BaseModel:   types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, mtr))

		p := ml.buildPrice(ctx, pl.ID, meterID, "price_matrix_"+ml.key)
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

		if ml.usageLimit != nil {
			feat := &feature.Feature{
				ID:        "feat_matrix_" + ml.key,
				Name:      "Feature " + ml.key,
				Type:      types.FeatureTypeMetered,
				MeterID:   meterID,
				BaseModel: types.GetDefaultBaseModel(ctx),
			}
			s.NoError(s.GetStores().FeatureRepo.Create(ctx, feat))
			_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
				ID:               "ent_matrix_" + ml.key,
				EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
				EntityID:         pl.ID,
				FeatureID:        feat.ID,
				FeatureType:      types.FeatureTypeMetered,
				IsEnabled:        true,
				UsageLimit:       ml.usageLimit,
				UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
				IsSoftLimit:      false,
				BaseModel:        types.GetDefaultBaseModel(ctx),
			})
			s.NoError(err)
		}

		lineItems = append(lineItems, &subscription.SubscriptionLineItem{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  s.sub.ID,
			CustomerID:      s.sub.CustomerID,
			EntityID:        pl.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: pl.Name,
			PriceID:         p.ID,
			PriceType:       p.Type,
			MeterID:         meterID,
			DisplayName:     "Matrix " + ml.key,
			Quantity:        decimal.Zero,
			Currency:        s.sub.Currency,
			BillingPeriod:   s.sub.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceArrear,
			StartDate:       s.sub.StartDate,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		})

		for d := 0; d < 30; d++ {
			ts := s.periodStart.AddDate(0, 0, d).Add(12 * time.Hour)
			for i, qty := range ml.eventsForDay(d) {
				id := s.GetUUID()
				records = append(records, &events.MeterUsage{
					Event: events.Event{
						ID:                 id,
						TenantID:           types.GetTenantID(ctx),
						EnvironmentID:      types.GetEnvironmentID(ctx),
						EventName:          mtr.EventName,
						ExternalCustomerID: cust.ExternalID,
						CustomerID:         cust.ID,
						Timestamp:          ts.Add(time.Duration(i) * time.Minute),
						IngestedAt:         ts,
					},
					MeterID:    meterID,
					QtyTotal:   qty,
					UniqueHash: fmt.Sprintf("matrix_%s:%s", ml.key, id),
				})
			}
		}
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.sub, lineItems))
	s.sub.LineItems = lineItems
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))
}

// TestE2E_PricingMatrixLifecycle drives every metering aggregation and
// pricing strategy through the full pipeline in one pass: rollup (mode and
// amount per line, row identity), the analytics response, finalize → flip,
// a clean reconcile sweep, and the export carrying the same rows.
func (s *RevenueRollupSuite) TestE2E_PricingMatrixLifecycle() {
	ctx := s.ctx
	lines := matrixLines()
	s.seedMatrixSubscription(ctx, lines)
	s.enableRevenueAnalytics(ctx)

	// --- Rollup: every line decomposes with its expected mode and amount ---
	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows)

	byPrice := map[string][]*revenuefact.RevenueFact{}
	total := decimal.Zero
	for _, r := range rows {
		byPrice[lo.FromPtr(r.PriceID)] = append(byPrice[lo.FromPtr(r.PriceID)], r)
		total = total.Add(r.NetAmount)
		if residual, identityOK := reconcileRow(r); !identityOK {
			s.Failf("row identity broken", "price %s day %s residual %s", lo.FromPtr(r.PriceID), r.Day, residual.String())
		}
	}
	for _, ml := range lines {
		priceID := "price_matrix_" + ml.key
		lineRows := byPrice[priceID]
		s.NotEmptyf(lineRows, "line %s must produce rows", ml.key)
		sum := decimal.Zero
		for _, r := range lineRows {
			s.Equalf(ml.wantMode, r.DecompositionMode, "line %s decomposition mode", ml.key)
			s.Equal(types.RevenueSourceUsage, r.RevenueSource)
			sum = sum.Add(r.NetAmount)
		}
		s.Truef(sum.Equal(decimal.RequireFromString(ml.wantAmount)), "line %s must sum to %s, got %s", ml.key, ml.wantAmount, sum)
		if ml.wantMode == types.PeriodOnly {
			s.Lenf(lineRows, 1, "line %s books one whole-period row", ml.key)
		} else {
			s.Lenf(lineRows, 30, "line %s books one row per day", ml.key)
		}
	}

	// The slab line's rows must carry a tier delta once usage crosses the
	// 1000-unit boundary (cheaper second tier).
	slabTierDelta := decimal.Zero
	for _, r := range byPrice["price_matrix_slab"] {
		slabTierDelta = slabTierDelta.Add(r.TierDelta)
	}
	s.True(slabTierDelta.IsNegative(), "graduated tiering must book a negative tier delta, got %s", slabTierDelta)

	// The entitlement line records daily entitled quantity and only bills the
	// excess: 1500 of 3000 units free.
	entBilled, entEntitled := decimal.Zero, decimal.Zero
	for _, r := range byPrice["price_matrix_ent"] {
		entBilled = entBilled.Add(r.BillableQty)
		entEntitled = entEntitled.Add(r.EntitlementQty)
	}
	s.True(entBilled.Equal(decimal.NewFromInt(1500)), "billable units beyond the limit, got %s", entBilled)
	s.True(entEntitled.Equal(decimal.NewFromInt(1500)), "entitled quantity recorded day by day, got %s", entEntitled)

	// The whole rollup reconciles to the engine preview.
	invReq, err := service.NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   s.sub,
		PeriodStart:    s.periodStart,
		PeriodEnd:      s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)
	s.True(total.Equal(invReq.Subtotal), "facts %s must equal the engine subtotal %s", total, invReq.Subtotal)

	// --- Analytics: the same numbers come back through the read surface ---
	req := &dto.RevenueAnalyticsRequest{
		StartTime:   s.periodStart,
		EndTime:     s.periodEnd,
		Status:      types.FactProvisional,
		Granularity: types.RevenueGranularityTotal,
		GroupBy:     []string{"price_id"},
	}
	res, err := s.svc.GetRevenueAnalytics(ctx, req)
	s.NoError(err)
	analyticsByPrice := map[string]decimal.Decimal{}
	for _, row := range res.Rows {
		analyticsByPrice[row.Group["price_id"]] = analyticsByPrice[row.Group["price_id"]].Add(row.NetAmount)
	}
	for _, ml := range lines {
		got := analyticsByPrice["price_matrix_"+ml.key]
		s.Truef(got.Equal(decimal.RequireFromString(ml.wantAmount)), "analytics for %s: want %s got %s", ml.key, ml.wantAmount, got)
	}

	// Amortized day view moves the same money, never changes it.
	req = &dto.RevenueAnalyticsRequest{
		StartTime:        s.periodStart,
		EndTime:          s.periodEnd,
		Status:           types.FactProvisional,
		Granularity:      types.RevenueGranularityDay,
		AllocationPolicy: types.RevenueAllocationAmortized,
	}
	res, err = s.svc.GetRevenueAnalytics(ctx, req)
	s.NoError(err)
	s.True(res.ContainsAllocated, "period_only lines make the day view allocated")
	amortized := decimal.Zero
	for _, row := range res.Rows {
		amortized = amortized.Add(row.NetAmount)
	}
	s.True(amortized.Equal(total), "amortized total %s must equal %s", amortized, total)

	// --- Finalize → flip, sweep clean, export carries the same rows ---
	inv := s.finalizeCurrentPreview(ctx)
	s.NoError(s.svc.FinalizeSubscriptionPeriod(ctx, inv.ID))
	remaining, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.Empty(remaining, "the flip must consume every provisional row")

	booked, err := s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	bookedTotal := decimal.Zero
	bookedByID := map[string]*revenuefact.RevenueFact{}
	for _, r := range booked {
		bookedTotal = bookedTotal.Add(r.NetAmount)
		bookedByID[r.ID] = r
	}
	s.True(bookedTotal.Equal(inv.Subtotal.Sub(inv.TotalDiscount)))

	checked, drifted, _, err := s.svc.ReconcileBookedInvoices(ctx, s.periodStart)
	s.NoError(err)
	s.GreaterOrEqual(checked, 1)
	s.Zero(drifted, "a freshly booked matrix must sweep clean")

	exported := s.exportAll(ctx, time.Time{})
	s.Len(exported, len(booked), "the export carries exactly the booked rows")
	for _, e := range exported {
		b, ok := bookedByID[e.ID]
		s.Truef(ok, "exported row %s must be a booked row", e.ID)
		s.True(e.NetAmount.Equal(b.NetAmount))
		s.Equal(b.RevenueSource, e.RevenueSource)
	}
}

// TestE2E_GrantsAndWalletLifecycle covers entitlement grants end to end —
// quantity-measure grants on parallel entitlement configs with disjoint
// overage windows, and an amount-measure grant — then proves wallet credits
// paying the invoice never touch revenue facts (revenue ≠ cash).
func (s *RevenueRollupSuite) TestE2E_GrantsAndWalletLifecycle() {
	ctx := s.ctx
	s.periodStart = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	periodEndExclusive := s.periodStart.AddDate(0, 0, 30)
	s.periodEnd = periodEndExclusive

	cust := &customer.Customer{
		ID:         "cust_gw",
		ExternalID: "ext_gw",
		Name:       "Grants And Wallet",
		Email:      "gw@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_gw", Name: "GW Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	s.sub = &subscription.Subscription{
		ID:                 "sub_gw",
		PlanID:             pl.ID,
		CustomerID:         cust.ID,
		StartDate:          s.periodStart,
		BillingAnchor:      periodEndExclusive,
		CurrentPeriodStart: s.periodStart,
		CurrentPeriodEnd:   periodEndExclusive,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	// Two SUM meters at $0.01/call, 100 calls/day each: qgrants carries two
	// quantity grants on separate entitlement configs, agrant one
	// amount-measure grant.
	var lineItems []*subscription.SubscriptionLineItem
	var records []*events.MeterUsage
	for _, key := range []string{"qgrants", "agrant"} {
		meterID := "meter_gw_" + key
		mtr := &meter.Meter{
			ID:          meterID,
			Name:        "GW " + key,
			EventName:   "evt_gw_" + key,
			Aggregation: meter.Aggregation{Type: types.AggregationSum},
			BaseModel:   types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, mtr))
		p := matrixFlatPrice("0.01")(ctx, pl.ID, meterID, "price_gw_"+key)
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

		feat := &feature.Feature{
			ID:        "feat_gw_" + key,
			Name:      "Feature " + key,
			Type:      types.FeatureTypeMetered,
			MeterID:   meterID,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().FeatureRepo.Create(ctx, feat))
		_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
			ID:               "ent_gw_" + key,
			EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
			EntityID:         pl.ID,
			FeatureID:        feat.ID,
			FeatureType:      types.FeatureTypeMetered,
			IsEnabled:        true,
			UsageLimit:       lo.ToPtr(int64(0)),
			UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
			IsSoftLimit:      false,
			BaseModel:        types.GetDefaultBaseModel(ctx),
		})
		s.NoError(err)

		lineItems = append(lineItems, &subscription.SubscriptionLineItem{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  s.sub.ID,
			CustomerID:      s.sub.CustomerID,
			EntityID:        pl.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: pl.Name,
			PriceID:         p.ID,
			PriceType:       p.Type,
			MeterID:         meterID,
			DisplayName:     "GW " + key,
			Quantity:        decimal.Zero,
			Currency:        s.sub.Currency,
			BillingPeriod:   s.sub.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceArrear,
			StartDate:       s.sub.StartDate,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		})
		for d := 0; d < 30; d++ {
			ts := s.periodStart.AddDate(0, 0, d).Add(12 * time.Hour)
			id := s.GetUUID()
			records = append(records, &events.MeterUsage{
				Event: events.Event{
					ID:                 id,
					TenantID:           types.GetTenantID(ctx),
					EnvironmentID:      types.GetEnvironmentID(ctx),
					EventName:          mtr.EventName,
					ExternalCustomerID: cust.ExternalID,
					CustomerID:         cust.ID,
					Timestamp:          ts,
					IngestedAt:         ts,
				},
				MeterID:    meterID,
				QtyTotal:   decimal.NewFromInt(100),
				UniqueHash: fmt.Sprintf("gw_%s:%s", key, id),
			})
		}
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.sub, lineItems))
	s.sub.LineItems = lineItems
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))

	// Quantity grants on two parallel entitlement configs with DISJOINT
	// crossed windows: [day5, day15) and [day20, day30) each bill their own
	// window's usage — 10 days × 100 calls × $0.01 apiece.
	mkGrant := func(id, ecID, featID string, measure types.EntitlementGrantMeasure, quota, usage decimal.Decimal, crossedAt, from, to time.Time) {
		g := &entitlementgrant.EntitlementGrant{
			ID:                  id,
			EntitlementConfigID: ecID,
			CustomerID:          cust.ID,
			SubscriptionID:      s.sub.ID,
			ScopeEntityType:     types.EntitlementGrantScopeFeature,
			ScopeEntityID:       featID,
			Measure:             measure,
			Quota:               quota,
			Usage:               usage,
			QuotaCrossedAt:      &crossedAt,
			ValidFrom:           from,
			ValidTo:             to,
			EnvironmentID:       types.GetEnvironmentID(ctx),
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}
		_, err := s.GetStores().EntitlementGrantRepo.Create(ctx, g)
		s.NoError(err)
	}
	mkGrant("eg_gw_q1", "ec_gw_1", "feat_gw_qgrants", types.EntitlementGrantMeasureQuantity,
		decimal.NewFromInt(500), decimal.NewFromInt(1500),
		s.periodStart.AddDate(0, 0, 5), s.periodStart, s.periodStart.AddDate(0, 0, 15))
	mkGrant("eg_gw_q2", "ec_gw_2", "feat_gw_qgrants", types.EntitlementGrantMeasureQuantity,
		decimal.NewFromInt(500), decimal.NewFromInt(1500),
		s.periodStart.AddDate(0, 0, 20), s.periodStart.AddDate(0, 0, 15), s.periodEnd)
	// Amount-measure grant: $10 quota crossed at day 10 (1000 calls × $0.01),
	// billing the remaining 20 days' value.
	mkGrant("eg_gw_a", "ec_gw_3", "feat_gw_agrant", types.EntitlementGrantMeasureAmount,
		decimal.NewFromInt(10), decimal.NewFromInt(30),
		s.periodStart.AddDate(0, 0, 10), s.periodStart, s.periodEnd)

	s.enableRevenueAnalytics(ctx)
	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)
	s.NotEmpty(rows)

	invReq, err := service.NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   s.sub,
		PeriodStart:    s.periodStart,
		PeriodEnd:      s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)
	engineByPrice := map[string]decimal.Decimal{}
	for i := range invReq.LineItems {
		li := &invReq.LineItems[i]
		engineByPrice[lo.FromPtr(li.PriceID)] = engineByPrice[lo.FromPtr(li.PriceID)].Add(li.Amount)
	}

	total := decimal.Zero
	factsByPrice := map[string]decimal.Decimal{}
	var qgrantRows []*revenuefact.RevenueFact
	for _, r := range rows {
		total = total.Add(r.NetAmount)
		factsByPrice[lo.FromPtr(r.PriceID)] = factsByPrice[lo.FromPtr(r.PriceID)].Add(r.NetAmount)
		if lo.FromPtr(r.PriceID) == "price_gw_qgrants" {
			qgrantRows = append(qgrantRows, r)
			s.Equal(types.Marginal, r.DecompositionMode, "grant-billed usage splits per day")
		}
		if residual, identityOK := reconcileRow(r); !identityOK {
			s.Failf("row identity broken", "price %s day %s residual %s", lo.FromPtr(r.PriceID), r.Day, residual.String())
		}
	}
	s.True(total.Equal(invReq.Subtotal), "facts %s must equal the engine subtotal %s", total, invReq.Subtotal)
	for priceID, engineAmount := range engineByPrice {
		s.Truef(factsByPrice[priceID].Equal(engineAmount), "price %s: facts %s vs engine %s", priceID, factsByPrice[priceID], engineAmount)
	}
	// Both parallel windows bill: 2 × 10 days × 100 calls × $0.01.
	s.True(factsByPrice["price_gw_qgrants"].Equal(decimal.NewFromInt(20)), "disjoint grant windows must both bill, got %s", factsByPrice["price_gw_qgrants"])
	// Days before the first crossing stay entitled at zero net.
	for _, r := range qgrantRows {
		if r.Day.Before(s.periodStart.AddDate(0, 0, 5)) {
			s.Truef(r.NetAmount.IsZero(), "day %s precedes every crossed window", r.Day)
		}
	}

	// --- Wallet credits pay the invoice; revenue facts never move ---
	w := &wallet.Wallet{
		ID:                  "wallet_gw",
		CustomerID:          cust.ID,
		Currency:            "usd",
		WalletType:          types.WalletTypePrePaid,
		Balance:             decimal.NewFromInt(100),
		CreditBalance:       decimal.NewFromInt(100),
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(ctx, w))

	inv := s.finalizeCurrentPreview(ctx)
	s.NoError(s.svc.FinalizeSubscriptionPeriod(ctx, inv.ID))
	booked, err := s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	beforePayment := map[string]int64{}
	for _, r := range booked {
		beforePayment[r.ID] = r.Version
	}

	// The wallet settles the invoice — a payment-side operation.
	inv.AmountPaid = inv.Total
	inv.AmountRemaining = decimal.Zero
	inv.PaymentStatus = types.PaymentStatusSucceeded
	s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

	_, drifted, corrected, err := s.svc.ReconcileBookedInvoices(ctx, s.periodStart)
	s.NoError(err)
	s.Zero(drifted, "payment must not look like revenue drift")
	s.Zero(corrected)

	afterPayment, err := s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Len(afterPayment, len(booked), "paying an invoice writes no revenue rows")
	for _, r := range afterPayment {
		s.Equalf(beforePayment[r.ID], r.Version, "row %s must not be recomputed by a payment", r.ID)
	}
}

// TestE2E_SameMeterAdjacentLineItems: a mid-period price change leaves two
// line items on ONE meter with adjacent windows (the shape
// meter_usage_test.go's TestMultipleLineItemsSameMeterDifferentDates covers on
// the billing side). Each line must decompose over its own window only — the
// curve reads usage per meter, so a window that ignored the line item's dates
// would bill the same day twice.
func (s *RevenueRollupSuite) TestE2E_SameMeterAdjacentLineItems() {
	ctx := s.ctx
	s.periodStart = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	periodEndExclusive := s.periodStart.AddDate(0, 0, 30)
	s.periodEnd = periodEndExclusive
	switchDay := s.periodStart.AddDate(0, 0, 15)

	cust := &customer.Customer{
		ID: "cust_split", ExternalID: "ext_split", Name: "Adjacent Line Items",
		Email: "split@example.com", BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_split", Name: "Split Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	mtr := &meter.Meter{
		ID: "meter_split", Name: "Split Calls", EventName: "call_split",
		Aggregation: meter.Aggregation{Type: types.AggregationSum},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, mtr))

	// Two prices on the SAME meter: the second is twice the first.
	mkPrice := func(id, amount string) *price.Price {
		p := matrixFlatPrice(amount)(ctx, pl.ID, mtr.ID, id)
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		return p
	}
	cheap := mkPrice("price_split_cheap", "0.01")
	dear := mkPrice("price_split_dear", "0.02")

	s.sub = &subscription.Subscription{
		ID: "sub_split", PlanID: pl.ID, CustomerID: cust.ID,
		StartDate: s.periodStart, BillingAnchor: periodEndExclusive,
		CurrentPeriodStart: s.periodStart, CurrentPeriodEnd: periodEndExclusive,
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	mkLine := func(id string, p *price.Price, start, end time.Time) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID: id, SubscriptionID: s.sub.ID, CustomerID: cust.ID,
			EntityID: pl.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: pl.Name, PriceID: p.ID, PriceType: p.Type, MeterID: mtr.ID,
			DisplayName: id, Quantity: decimal.Zero, Currency: "usd",
			BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
			StartDate: start, EndDate: end, BaseModel: types.GetDefaultBaseModel(ctx),
		}
	}
	lineItems := []*subscription.SubscriptionLineItem{
		mkLine("sli_first_half", cheap, s.periodStart, switchDay),
		mkLine("sli_second_half", dear, switchDay, periodEndExclusive),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.sub, lineItems))
	s.sub.LineItems = lineItems

	// 100 units every day of the period.
	var records []*events.MeterUsage
	for d := 0; d < 30; d++ {
		ts := s.periodStart.AddDate(0, 0, d).Add(12 * time.Hour)
		id := s.GetUUID()
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
				EventName: mtr.EventName, ExternalCustomerID: cust.ExternalID,
				CustomerID: cust.ID, Timestamp: ts, IngestedAt: ts,
			},
			MeterID: mtr.ID, QtyTotal: decimal.NewFromInt(100),
			UniqueHash: fmt.Sprintf("split:%s", id),
		})
	}
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))
	s.enableRevenueAnalytics(ctx)

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)

	daysByPrice := map[string]map[string]bool{}
	qtyByPrice := map[string]decimal.Decimal{}
	total := decimal.Zero
	for _, r := range rows {
		pid := lo.FromPtr(r.PriceID)
		if daysByPrice[pid] == nil {
			daysByPrice[pid] = map[string]bool{}
		}
		daysByPrice[pid][r.Day.Format("2006-01-02")] = true
		qtyByPrice[pid] = qtyByPrice[pid].Add(r.BillableQty)
		total = total.Add(r.NetAmount)
		if residual, ok := reconcileRow(r); !ok {
			s.Failf("row identity broken", "price %s day %s residual %s", pid, r.Day, residual)
		}
	}

	// Neither line may claim a day belonging to the other.
	for day := range daysByPrice[cheap.ID] {
		s.False(daysByPrice[dear.ID][day], "day %s is claimed by both line items", day)
		s.True(day < switchDay.Format("2006-01-02"), "the cheap line stops at the switch, saw %s", day)
	}
	for day := range daysByPrice[dear.ID] {
		s.True(day >= switchDay.Format("2006-01-02"), "the dear line starts at the switch, saw %s", day)
	}

	// Each half meters its own 15 days of usage, once.
	s.True(qtyByPrice[cheap.ID].Equal(decimal.NewFromInt(1500)), "first half qty, got %s", qtyByPrice[cheap.ID])
	s.True(qtyByPrice[dear.ID].Equal(decimal.NewFromInt(1500)), "second half qty, got %s", qtyByPrice[dear.ID])

	invReq, err := service.NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription: s.sub, PeriodStart: s.periodStart, PeriodEnd: s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)
	s.True(total.Equal(invReq.Subtotal), "facts %s must equal the engine subtotal %s", total, invReq.Subtotal)
}

// TestE2E_BucketedPackagePerDay: a bucketed price charges each window on its
// own quantity — 5 units/day on a $1-per-10 package with daily buckets bills
// $1 EVERY day. Re-pricing the running total would bill $1 once, so this is
// the shape that forced bucketed lines to whole-period until the curve learned
// to price per window.
func (s *RevenueRollupSuite) TestE2E_BucketedPackagePerDay() {
	ctx := s.ctx
	const days = 10
	today := time.Now().UTC().Truncate(24 * time.Hour)
	s.periodStart = today.AddDate(0, 0, -days)
	periodEndExclusive := s.periodStart.AddDate(0, 1, 0)
	s.periodEnd = periodEndExclusive

	cust := &customer.Customer{
		ID: "cust_bkt", ExternalID: "ext_bkt", Name: "Bucketed",
		Email: "bkt@example.com", BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_bkt", Name: "Bucketed Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	mtr := &meter.Meter{
		ID: "meter_bkt", Name: "Bucketed Units", EventName: "bkt_event",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, BucketSize: types.WindowSizeDay},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, mtr))

	p := matrixPackagePrice(ctx, pl.ID, mtr.ID, "price_bkt")
	p.TransformQuantity = price.JSONBTransformQuantity{DivideBy: 10, Round: types.ROUND_UP}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

	s.sub = &subscription.Subscription{
		ID: "sub_bkt", PlanID: pl.ID, CustomerID: cust.ID,
		StartDate: s.periodStart, BillingAnchor: periodEndExclusive,
		CurrentPeriodStart: s.periodStart, CurrentPeriodEnd: periodEndExclusive,
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := []*subscription.SubscriptionLineItem{{
		ID: "sli_bkt", SubscriptionID: s.sub.ID, CustomerID: cust.ID,
		EntityID: pl.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: pl.Name, PriceID: p.ID, PriceType: p.Type, MeterID: mtr.ID,
		DisplayName: "Bucketed", Quantity: decimal.Zero, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate: s.sub.StartDate, BaseModel: types.GetDefaultBaseModel(ctx),
	}}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.sub, lineItems))
	s.sub.LineItems = lineItems

	// 5 units on each elapsed day: under one package, so every day bills $1.
	var records []*events.MeterUsage
	for d := 0; d < days; d++ {
		ts := s.periodStart.AddDate(0, 0, d).Add(12 * time.Hour)
		id := s.GetUUID()
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
				EventName: mtr.EventName, ExternalCustomerID: cust.ExternalID,
				CustomerID: cust.ID, Timestamp: ts, IngestedAt: ts,
			},
			MeterID: mtr.ID, QtyTotal: decimal.NewFromInt(5),
			UniqueHash: fmt.Sprintf("bkt:%s", id),
		})
	}
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))
	s.enableRevenueAnalytics(ctx)

	invReq, err := service.NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription: s.sub, PeriodStart: s.periodStart, PeriodEnd: s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)
	s.True(invReq.Subtotal.Equal(decimal.NewFromInt(days)),
		"engine bills one package per day, got %s", invReq.Subtotal)

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)

	total := decimal.Zero
	billed := 0
	for _, r := range rows {
		s.Equal(types.Marginal, r.DecompositionMode, "windows nesting in a day split per day")
		total = total.Add(r.NetAmount)
		if r.NetAmount.IsPositive() {
			billed++
			s.True(r.NetAmount.Equal(decimal.NewFromInt(1)), "each day bills one package, got %s on %s", r.NetAmount, r.Day)
		}
	}
	s.Equal(days, billed, "one billed row per day of usage")
	s.True(total.Equal(invReq.Subtotal), "facts %s must equal the engine subtotal %s", total, invReq.Subtotal)
}

// TestE2E_BucketedWindowedCommitmentPerDay: a per-bucket commitment settles
// every window on its own, so a line can run over on one day and fall short on
// the next. With daily buckets each part is datable — committed usage, overage
// and true-up all land on the day that produced them, instead of collapsing
// onto one period_only row apiece.
func (s *RevenueRollupSuite) TestE2E_BucketedWindowedCommitmentPerDay() {
	ctx := s.ctx
	const days = 10
	today := time.Now().UTC().Truncate(24 * time.Hour)
	s.periodStart = today.AddDate(0, 0, -days)
	periodEndExclusive := s.periodStart.AddDate(0, 1, 0)
	s.periodEnd = periodEndExclusive

	cust := &customer.Customer{
		ID: "cust_wbkt", ExternalID: "ext_wbkt", Name: "Windowed Bucketed",
		Email: "wbkt@example.com", BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_wbkt", Name: "Windowed Bucketed Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	mtr := &meter.Meter{
		ID: "meter_wbkt", Name: "Windowed Units", EventName: "wbkt_event",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, BucketSize: types.WindowSizeDay},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, mtr))
	p := matrixFlatPrice("1")(ctx, pl.ID, mtr.ID, "price_wbkt")
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

	s.sub = &subscription.Subscription{
		ID: "sub_wbkt", PlanID: pl.ID, CustomerID: cust.ID,
		StartDate: s.periodStart, BillingAnchor: periodEndExclusive,
		CurrentPeriodStart: s.periodStart, CurrentPeriodEnd: periodEndExclusive,
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	// $10 committed per DAY, 2x overage, true-up on: a short day still bills
	// the $10, a busy day bills $10 plus twice the excess.
	lineItems := []*subscription.SubscriptionLineItem{{
		ID: "sli_wbkt", SubscriptionID: s.sub.ID, CustomerID: cust.ID,
		EntityID: pl.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: pl.Name, PriceID: p.ID, PriceType: p.Type, MeterID: mtr.ID,
		DisplayName: "Windowed", Quantity: decimal.Zero, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:               s.sub.StartDate,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        lo.ToPtr(decimal.NewFromInt(10)),
		CommitmentOverageFactor: lo.ToPtr(decimal.NewFromInt(2)),
		CommitmentTrueUpEnabled: true,
		CommitmentWindowed:      true,
		BaseModel:               types.GetDefaultBaseModel(ctx),
	}}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.sub, lineItems))
	s.sub.LineItems = lineItems

	// Alternate short days (5 units -> true-up) and busy days (15 -> overage).
	var records []*events.MeterUsage
	for d := 0; d < days; d++ {
		qty := int64(5)
		if d%2 == 1 {
			qty = 15
		}
		ts := s.periodStart.AddDate(0, 0, d).Add(12 * time.Hour)
		id := s.GetUUID()
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
				EventName: mtr.EventName, ExternalCustomerID: cust.ExternalID,
				CustomerID: cust.ID, Timestamp: ts, IngestedAt: ts,
			},
			MeterID: mtr.ID, QtyTotal: decimal.NewFromInt(qty),
			UniqueHash: fmt.Sprintf("wbkt:%s", id),
		})
	}
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))
	s.enableRevenueAnalytics(ctx)

	invReq, err := service.NewBillingService(s.serviceParams()).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription: s.sub, PeriodStart: s.periodStart, PeriodEnd: s.periodEnd,
		ReferencePoint: types.ReferencePointRevenueFacts,
	})
	s.NoError(err)

	s.NoError(s.svc.RollupSubscription(ctx, s.sub.ID))
	rows, err := s.store.ListBySubscriptionPeriod(ctx, s.sub.ID, s.periodStart, s.periodEnd, types.FactProvisional)
	s.NoError(err)

	bySource := map[types.RevenueSource]decimal.Decimal{}
	daysWithOverage := map[string]bool{}
	daysWithTrueUp := map[string]bool{}
	total := decimal.Zero
	for _, r := range rows {
		bySource[r.RevenueSource] = bySource[r.RevenueSource].Add(r.NetAmount)
		total = total.Add(r.NetAmount)
		if !r.NetAmount.IsPositive() {
			continue
		}
		s.Equal(types.Marginal, r.DecompositionMode, "every part is dated per day")
		switch r.RevenueSource {
		case types.RevenueSourceOverage:
			daysWithOverage[r.Day.Format("2006-01-02")] = true
		case types.RevenueSourceCommitmentTrueup:
			daysWithTrueUp[r.Day.Format("2006-01-02")] = true
		}
	}

	// 5 short days true-up $5 each; 5 busy days overage (15-10)x2 = $10 each.
	// The elapsed part of today has no events at all, and with true-up enabled
	// the engine still bills that empty window its committed $10 — so the
	// true-up is $35 over 6 days, not $25 over 5.
	s.True(bySource[types.RevenueSourceCommitmentTrueup].Equal(decimal.NewFromInt(35)),
		"true-up total, got %s", bySource[types.RevenueSourceCommitmentTrueup])
	s.True(bySource[types.RevenueSourceOverage].Equal(decimal.NewFromInt(50)),
		"overage total, got %s", bySource[types.RevenueSourceOverage])
	s.Len(daysWithTrueUp, 6, "true-up lands on each short day, not once for the period")
	s.Len(daysWithOverage, 5, "overage lands on each busy day")

	// A day never carries both: a window either ran over or fell short.
	for day := range daysWithOverage {
		s.False(daysWithTrueUp[day], "day %s cannot both over-run and fall short", day)
	}
	s.True(total.Equal(invReq.Subtotal), "facts %s must equal the engine subtotal %s", total, invReq.Subtotal)
}

// TestDecomposeOverageRows_BucketedUsesPerWindowCurve: an overage line with no
// normal sibling (the commitment was already exhausted) must not fall back to
// the cumulative curve when the price is bucketed. With a $1-per-10 package on
// daily windows and 5 units a day, every day bills one package; re-pricing the
// running total and splitting it would report $2, $0, $2, $0 instead.
func (s *RevenueRollupSuite) TestDecomposeOverageRows_BucketedUsesPerWindowCurve() {
	ctx := s.ctx
	const days = 4
	today := time.Now().UTC().Truncate(24 * time.Hour)
	periodStart := today.AddDate(0, 0, -days)
	itemPeriod := revenuePeriod{Start: periodStart, End: today.AddDate(0, 0, -1)}

	mtr := &meter.Meter{
		ID: "meter_obkt", Name: "Overage Bucketed", EventName: "obkt_event",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, BucketSize: types.WindowSizeDay},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, mtr))
	p := matrixPackagePrice(ctx, "plan_obkt", mtr.ID, "price_obkt")
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

	var records []*events.MeterUsage
	for d := 0; d < days; d++ {
		ts := periodStart.AddDate(0, 0, d).Add(12 * time.Hour)
		id := s.GetUUID()
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
				EventName: mtr.EventName, ExternalCustomerID: "ext_obkt",
				CustomerID: "cust_obkt", Timestamp: ts, IngestedAt: ts,
			},
			MeterID: mtr.ID, QtyTotal: decimal.NewFromInt(5),
			UniqueHash: fmt.Sprintf("obkt:%s", id),
		})
	}
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, records))

	sub := &subscription.Subscription{ID: "sub_obkt", BillingAnchor: itemPeriod.exclusiveEnd()}
	inputs := &rollupInputs{
		prices:         map[string]*price.Price{p.ID: p},
		meters:         map[string]*meter.Meter{mtr.ID: mtr},
		extCustomerIDs: []string{"ext_obkt"},
	}
	base := previewLineItem{
		TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
		CustomerID: "cust_obkt", SubscriptionID: sub.ID, SubLineItemID: "sli_obkt",
		Price: &price.Price{ID: "overage:sli_obkt"}, Currency: "usd",
		Source:      types.RevenueSourceOverage,
		PeriodStart: itemPeriod.Start, PeriodEnd: itemPeriod.End,
	}
	item := &dto.CreateInvoiceLineItemRequest{
		Amount:  decimal.NewFromInt(days),
		PriceID: lo.ToPtr(p.ID), MeterID: lo.ToPtr(mtr.ID),
	}

	// No entry in overageCurves: this line has no normal sibling.
	rows := s.svc.(*revenueService).decomposeOverageRows(ctx, sub, inputs, base, item, itemPeriod, map[string][]dayCharge{})

	total := decimal.Zero
	billed := 0
	for _, r := range rows {
		s.Equal(types.RevenueSourceOverage, r.RevenueSource)
		s.Equal(types.Marginal, r.DecompositionMode)
		total = total.Add(r.NetAmount)
		if r.NetAmount.IsPositive() {
			billed++
			s.True(r.NetAmount.Equal(decimal.NewFromInt(1)),
				"each window bills one package, got %s on %s", r.NetAmount, r.Day)
		}
	}
	s.Equal(days, billed, "one billed row per window, not a re-priced running total")
	s.True(total.Equal(item.Amount), "rows must still sum to the engine's line amount")
}

// TestE2E_SameDayPeriodFlipsToFinal reproduces a subscription created and
// cancelled the same day: its whole billing period is a few hours on one date.
// The JIT rollup derives rows from the finalized invoice, and the flip has to
// match them on the same day-grained bounds — before, it matched on raw
// timestamps against date columns, so nothing ever flipped and the facts sat
// PROVISIONAL forever, leaving a later void with nothing to reverse.
func (s *RevenueRollupSuite) TestE2E_SameDayPeriodFlipsToFinal() {
	ctx := s.ctx
	today := time.Now().UTC().Truncate(24 * time.Hour)
	s.periodStart = today.Add(9*time.Hour + 37*time.Minute)
	periodEndExclusive := today.Add(13*time.Hour + 7*time.Minute)
	s.periodEnd = periodEndExclusive

	cust := &customer.Customer{
		ID: "cust_sameday", ExternalID: "ext_sameday", Name: "Same Day",
		Email: "sameday@example.com", BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	pl := &plan.Plan{ID: "plan_sameday", Name: "Same Day Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	p := &price.Price{
		ID: "price_sameday", Amount: decimal.NewFromInt(20), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: pl.ID,
		Type: types.PRICE_TYPE_FIXED, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
		BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

	s.sub = &subscription.Subscription{
		ID: "sub_sameday", PlanID: pl.ID, CustomerID: cust.ID,
		StartDate: s.periodStart, BillingAnchor: periodEndExclusive,
		CurrentPeriodStart: s.periodStart, CurrentPeriodEnd: periodEndExclusive,
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	lineItems := []*subscription.SubscriptionLineItem{{
		ID: "sli_sameday", SubscriptionID: s.sub.ID, CustomerID: cust.ID,
		EntityID: pl.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: pl.Name, PriceID: p.ID, PriceType: p.Type,
		DisplayName: "Same Day Fee", Quantity: decimal.NewFromInt(1), Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate: s.sub.StartDate, BaseModel: types.GetDefaultBaseModel(ctx),
	}}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.sub, lineItems))
	s.sub.LineItems = lineItems
	s.enableRevenueAnalytics(ctx)

	// No provisional rows exist yet — the daily rollup never saw this
	// subscription — so finalization takes the JIT path.
	inv := s.finalizeCurrentPreview(ctx)
	s.NoError(s.svc.FinalizeSubscriptionPeriod(ctx, inv.ID))

	booked, err := s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.NotEmpty(booked, "a same-day period must still book its facts against the invoice")
	for _, r := range booked {
		s.Equal(types.FactFinal, r.Status, "rows must reach FINAL, not sit provisional")
		s.Equal(inv.ID, lo.FromPtr(r.InvoiceID))
		s.False(r.PeriodEnd.Before(r.PeriodStart), "period_end %s precedes period_start %s", r.PeriodEnd, r.PeriodStart)
		s.Equal(today.Format("2006-01-02"), r.PeriodEnd.Format("2006-01-02"))
	}

	// A void now has FINAL rows to reverse, which is what was missing.
	s.NoError(s.svc.RevertInvoiceFacts(ctx, inv.ID))
	afterVoid, err := s.store.ListByInvoiceID(ctx, inv.ID)
	s.NoError(err)
	s.Greater(len(afterVoid), len(booked), "the void must append contra rows")
	total := decimal.Zero
	for _, r := range afterVoid {
		total = total.Add(r.NetAmount)
	}
	s.True(total.IsZero(), "a voided invoice's facts must net to zero, got %s", total)
}
