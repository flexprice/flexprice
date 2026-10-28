package selfbilling

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func (c *remoteClient) summaryPlan(ctx context.Context, extID string) (*remoteSubscription, *dto.SelfBillingPlanResponse, error) {
	// Retry selection once if cancellation raced the detail fetch, so an older live subscription can win.
	for attempt := 0; attempt < 2; attempt++ {
		sub, err := c.LatestSubscription(ctx, extID)
		if err != nil {
			return nil, nil, err
		}

		if sub == nil || sub.status == types.SubscriptionStatusCancelled {
			plan, err := c.offeredPlan(ctx)
			return sub, plan, err
		}

		resp, err := c.sdk.Subscriptions.GetSubscriptionV2(ctx, sub.id, lo.ToPtr("plan,subscription_line_items.prices"))
		if err != nil {
			return nil, nil, cloudErr(err)
		}

		details := resp.SubscriptionResponseV2
		if details == nil || lo.FromPtr(details.ID) != sub.id || lo.FromPtr(details.SubscriptionStatus) == "" {
			return nil, nil, unreadablePricing()
		}

		if lo.FromPtr(details.SubscriptionStatus) == sdktypes.SubscriptionStatusCancelled {
			continue
		}

		plan, err := c.subscribedPlan(ctx, details, time.Now().UTC())
		return &remoteSubscription{
			id:       sub.id,
			status:   types.SubscriptionStatus(lo.FromPtr(details.SubscriptionStatus)),
			trialEnd: details.TrialEnd,
		}, plan, err
	}

	return nil, nil, ierr.NewError("subscription changed while reading billing summary").
		WithHint("The subscription changed; reload the billing summary").Mark(ierr.ErrInvalidOperation)
}

func (c *remoteClient) offeredPlan(ctx context.Context) (*dto.SelfBillingPlanResponse, error) {
	resp, err := c.sdk.Plans.GetPlan(ctx, c.planID)
	if err != nil {
		return nil, cloudErr(err)
	}

	plan := resp.PlanResponse
	if plan == nil || lo.FromPtr(plan.ID) == "" {
		return nil, unreadablePricing()
	}

	out := newSummaryPlan(plan)
	now := time.Now().UTC()
	meters := make(map[string]*sdktypes.MeterResponse)

	for i := range plan.Prices {
		p := &plan.Prices[i]
		if !activePricing(lo.FromPtr(p.Status), p.StartDate, p.EndDate, now) || !strings.EqualFold(lo.FromPtr(p.Currency), trialCurrency) {
			continue
		}

		quantity := decimal.NewFromInt(1)
		if lo.FromPtr(p.Type) == sdktypes.PriceTypeUsage && lo.FromPtr(p.MeterID) != "" {
			quantity = decimal.Zero
		}

		if minimum, _ := p.MinQuantity.Get(); minimum != nil {
			quantity, err = pricingDecimal(minimum)
			if err != nil {
				return nil, err
			}
		}

		if err := c.hydratePricingMeter(ctx, p, meters); err != nil {
			return nil, err
		}

		charge, err := summaryCharge(p, quantity)
		if err != nil {
			return nil, err
		}

		out.Charges = append(out.Charges, charge)
	}

	return out, nil
}

func (c *remoteClient) subscribedPlan(ctx context.Context, sub *sdktypes.SubscriptionResponseV2, now time.Time) (*dto.SelfBillingPlanResponse, error) {
	if sub.Plan == nil || lo.FromPtr(sub.Plan.ID) == "" {
		return nil, unreadablePricing()
	}

	out := newSummaryPlan(sub.Plan)
	meters := make(map[string]*sdktypes.MeterResponse)

	for i := range sub.LineItems {
		item := &sub.LineItems[i]
		if item.StartDate == nil || item.StartDate.IsZero() || !activePricing(lo.FromPtr(item.Status), item.StartDate, item.EndDate, now) {
			continue
		}

		if item.Price == nil || lo.FromPtr(item.Price.ID) == "" || lo.FromPtr(item.Price.ID) != lo.FromPtr(item.PriceID) || item.Quantity == nil {
			return nil, unreadablePricing()
		}

		quantity, err := pricingDecimal(item.Quantity)
		if err != nil {
			return nil, err
		}

		if err := c.hydratePricingMeter(ctx, item.Price, meters); err != nil {
			return nil, err
		}

		// A grandfathered line can still reference an expired catalog price.
		charge, err := summaryCharge(item.Price, quantity)
		if err != nil {
			return nil, err
		}

		charge.LineItemID = lo.FromPtr(item.ID)
		charge.Currency = lo.FromPtr(item.Currency)
		charge.BillingPeriod = types.BillingPeriod(lo.FromPtr(item.BillingPeriod))
		charge.BillingPeriodCount = int(lo.FromPtr(item.BillingPeriodCount))
		charge.InvoiceCadence = types.InvoiceCadence(lo.FromPtr(item.InvoiceCadence))

		if displayName := lo.FromPtr(item.DisplayName); displayName != "" {
			charge.DisplayName = displayName
		}

		if meterName := lo.FromPtr(item.MeterDisplayName); meterName != "" {
			charge.MeterDisplayName = meterName
		}

		out.Charges = append(out.Charges, charge)
	}

	return out, nil
}

func newSummaryPlan(plan *sdktypes.PlanResponse) *dto.SelfBillingPlanResponse {
	return &dto.SelfBillingPlanResponse{
		ID:          lo.FromPtr(plan.ID),
		Name:        lo.FromPtr(plan.Name),
		Description: lo.FromPtr(plan.Description),
		Charges:     []*dto.SelfBillingChargeResponse{},
	}
}

func summaryCharge(p *sdktypes.PriceResponse, quantity decimal.Decimal) (*dto.SelfBillingChargeResponse, error) {
	if p == nil || lo.FromPtr(p.ID) == "" {
		return nil, unreadablePricing()
	}

	amount, err := pricingDecimal(p.Amount)
	if err != nil {
		return nil, err
	}

	tiers, err := pricingTiers(p.Tiers)
	if err != nil {
		return nil, err
	}

	unitTiers, err := pricingTiers(p.PriceUnitTiers)
	if err != nil {
		return nil, err
	}

	charge := &dto.SelfBillingChargeResponse{
		PriceID:            lo.FromPtr(p.ID),
		DisplayName:        lo.FromPtr(p.DisplayName),
		Type:               types.PriceType(lo.FromPtr(p.Type)),
		Quantity:           quantity,
		Amount:             amount,
		Currency:           lo.FromPtr(p.Currency),
		BillingModel:       types.BillingModel(lo.FromPtr(p.BillingModel)),
		BillingPeriod:      types.BillingPeriod(lo.FromPtr(p.BillingPeriod)),
		BillingPeriodCount: int(lo.FromPtr(p.BillingPeriodCount)),
		InvoiceCadence:     types.InvoiceCadence(lo.FromPtr(p.InvoiceCadence)),
		TierMode:           types.BillingTier(lo.FromPtr(p.TierMode)),
		Tiers:              tiers,
		TransformQuantity:  pricingTransform(p.TransformQuantity),
		MeterID:            lo.FromPtr(p.MeterID),
		BucketSize:         types.WindowSize(lo.FromPtr(p.BucketSize)),
		PriceUnitType:      types.PriceUnitType(lo.FromPtr(p.PriceUnitType)),
		PriceUnit:          p.PriceUnit,
		PriceUnitTiers:     unitTiers,
	}

	if p.PriceUnitAmount != nil {
		unitAmount, err := pricingDecimal(p.PriceUnitAmount)
		if err != nil {
			return nil, err
		}

		charge.PriceUnitAmount = &unitAmount
	}

	if p.Meter != nil {
		charge.MeterDisplayName = lo.FromPtr(p.Meter.Name)
		if charge.BucketSize == "" {
			charge.BucketSize = types.WindowSize(lo.FromPtr(p.Meter.Aggregation.GetBucketSize()))
		}
	}

	return charge, nil
}

// Subscription price expansion omits meters; fetch missing legacy bucketing metadata once per meter.
func (c *remoteClient) hydratePricingMeter(ctx context.Context, p *sdktypes.PriceResponse, meters map[string]*sdktypes.MeterResponse) error {
	meterID := lo.FromPtr(p.MeterID)
	if lo.FromPtr(p.Type) != sdktypes.PriceTypeUsage || meterID == "" || lo.FromPtr(p.BucketSize) != "" || p.Meter != nil {
		return nil
	}

	if cached, ok := meters[meterID]; ok {
		p.Meter = cached
		return nil
	}

	resp, err := c.sdk.Prices.GetPrice(ctx, lo.FromPtr(p.ID))
	if err != nil {
		return cloudErr(err)
	}

	if resp.PriceResponse == nil || resp.PriceResponse.Meter == nil || lo.FromPtr(resp.PriceResponse.Meter.ID) != meterID {
		return unreadablePricing()
	}

	p.Meter = resp.PriceResponse.Meter
	meters[meterID] = p.Meter
	return nil
}

func pricingTiers(in []sdktypes.PricePriceTier) (price.JSONBTiers, error) {
	if len(in) == 0 {
		return nil, nil
	}

	out := make(price.JSONBTiers, 0, len(in))
	for _, tier := range in {
		if tier.UnitAmount == nil || (tier.UpTo != nil && *tier.UpTo < 0) {
			return nil, unreadablePricing()
		}

		unitAmount, err := pricingDecimal(tier.UnitAmount)
		if err != nil {
			return nil, err
		}

		mapped := price.PriceTier{
			UnitAmount: unitAmount,
		}

		if tier.UpTo != nil {
			mapped.UpTo = lo.ToPtr(uint64(*tier.UpTo))
		}

		if tier.FlatAmount != nil {
			flatAmount, err := pricingDecimal(tier.FlatAmount)
			if err != nil {
				return nil, err
			}

			mapped.FlatAmount = &flatAmount
		}

		out = append(out, mapped)
	}

	return out, nil
}

func pricingTransform(in *sdktypes.PriceJSONBTransformQuantity) *price.TransformQuantity {
	if in == nil {
		return nil
	}

	return &price.TransformQuantity{
		DivideBy: int(lo.FromPtr(in.GetDivideBy())),
		Round:    types.RoundType(lo.FromPtr(in.GetRound())),
	}
}

func pricingDecimal(raw *string) (decimal.Decimal, error) {
	if raw == nil {
		return decimal.Zero, nil
	}

	value, err := decimal.NewFromString(*raw)
	if err != nil {
		return decimal.Zero, ierr.WithError(err).WithHint("Flexprice Cloud returned unreadable pricing").Mark(ierr.ErrInternal)
	}

	return value, nil
}

func activePricing(status sdktypes.Status, start, end *time.Time, now time.Time) bool {
	return status == sdktypes.StatusPublished &&
		(start == nil || !start.After(now)) &&
		(end == nil || end.IsZero() || !end.Before(now))
}

func unreadablePricing() error {
	return ierr.NewError("flexprice cloud returned incomplete billing details").
		WithHint("Flexprice Cloud returned unreadable pricing").Mark(ierr.ErrInternal)
}
