package world

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// Ephemeral is a throwaway customer stamped from a persona's traits.
type Ephemeral struct {
	externalID     string
	customerID     string
	subscriptionID string
	scenario       string
	createdAt      time.Time
}

func (e *Ephemeral) ExternalID() string     { return e.externalID }
func (e *Ephemeral) CustomerID() string     { return e.customerID }
func (e *Ephemeral) SubscriptionID() string { return e.subscriptionID }
func (e *Ephemeral) Scenario() string       { return e.scenario }
func (e *Ephemeral) CreatedAt() time.Time   { return e.createdAt }

// Clone creates an ephemeral customer carrying the persona's subscription-level
// traits (commitment, coupon, tax) and registers it for the janitor.
func (w *World) Clone(ctx context.Context, p *Persona, scenario string) (*Ephemeral, error) {
	fx := w.Fixtures()
	if !fx.Ready() {
		return nil, errors.New("world: fixtures not provisioned")
	}
	now := w.now().UTC()
	ext := fmt.Sprintf("%s%s-%d", EphemeralPrefix, scenario, now.UnixNano())
	metadata := w.personaMetadata(p, EphemeralCohort)
	metadata["e2eprobe_scenario"] = scenario
	created, err := w.client.Customers().Create(ctx, types.CreateCustomerRequest{
		ExternalID: ext,
		Name:       "E2EProbe Ephemeral " + scenario,
		Metadata:   metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("create customer %s: %w", ext, err)
	}
	eph := &Ephemeral{externalID: ext, scenario: scenario, createdAt: now}
	if created.CustomerResponse != nil && created.CustomerResponse.ID != nil {
		eph.customerID = *created.CustomerResponse.ID
	}
	w.eph.Register("customer", ext, now)
	if !p.Has(e2eprobe.TraitSubscription) {
		return eph, nil
	}

	spec := p.Spec()
	cycle := types.BillingCycleAnniversary
	req := types.CreateSubscriptionRequest{
		ExternalCustomerID: ptr(ext),
		PlanID:             fx.PlanID(),
		Currency:           "usd",
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: ptr(int64(1)),
		BillingCycle:       &cycle,
		StartDate:          &now,
		Metadata:           metadata,
	}
	if p.Has(e2eprobe.TraitCommitment) && spec.Commitment != nil {
		duration := types.BillingPeriodMonthly
		req.CommitmentAmount = ptr(spec.Commitment.Amount)
		req.CommitmentDuration = &duration
		req.OverageFactor = ptr(spec.Commitment.OverageFactor)
	}
	if p.Has(e2eprobe.TraitCoupon) && fx.CouponCode() != "" {
		req.SubscriptionCoupons = []types.SubscriptionCouponInput{{CouponCode: fx.CouponCode()}}
	}
	sub, err := w.client.Subscriptions().Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create subscription for %s: %w", ext, err)
	}
	if sub.SubscriptionResponse == nil || sub.SubscriptionResponse.ID == nil {
		return nil, fmt.Errorf("create subscription for %s: empty response", ext)
	}
	eph.subscriptionID = *sub.SubscriptionResponse.ID
	w.activateDraft(ctx, eph.subscriptionID, sub.SubscriptionResponse.SubscriptionStatus)
	w.eph.Register("subscription", eph.subscriptionID, now)

	if p.Has(e2eprobe.TraitTax) {
		taType := types.TaxRateEntityTypeSubscription
		if _, err := w.client.TaxAssociations().Create(ctx, types.CreateTaxAssociationRequest{
			TaxRateCode: fx.TaxRateCode(),
			EntityID:    ptr(eph.subscriptionID),
			EntityType:  &taType,
			AutoApply:   ptr(true),
			Metadata:    metadata,
		}); err != nil && !IsAlreadyExists(err) {
			return nil, fmt.Errorf("create tax association for %s: %w", ext, err)
		}
	}
	return eph, nil
}

func listCouponAssociations(subID string) dtos.ListCouponAssociationsRequest {
	return dtos.ListCouponAssociationsRequest{SubscriptionIds: []string{subID}}
}
