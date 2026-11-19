package world

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// provisionPersona makes the live state carry every declared trait, one
// read-then-write step per trait, recording what cannot be retrofitted.
func (w *World) provisionPersona(ctx context.Context, p *Persona) error {
	fx := w.Fixtures()
	if err := w.checkPaymentFixedCustomer(ctx, p); err != nil {
		return err
	}
	if p.Has(e2eprobe.TraitAdditiveGrant) {
		p.setGap(e2eprobe.TraitAdditiveGrant, gapIf(fx.GrantEntitlementID() == "", "plan has no additive grant entitlement"))
	}
	if p.Has(e2eprobe.TraitSoftLimit) {
		p.setGap(e2eprobe.TraitSoftLimit, gapIf(fx.SoftLimitEntitlementID("count") == "", "plan has no soft-limit entitlement on the count meter"))
	}
	if !needsCustomer(p) {
		return nil
	}
	if err := w.ensureCustomer(ctx, p); err != nil {
		return err
	}
	if p.Has(e2eprobe.TraitSubscription) {
		if err := w.ensureSubscription(ctx, fx, p); err != nil {
			return err
		}
	}
	if p.Has(e2eprobe.TraitTax) {
		if err := w.ensureTaxAssociation(ctx, fx, p); err != nil {
			return err
		}
	}
	if p.Has(e2eprobe.TraitMultiCadence) {
		if err := w.ensureMultiCadence(ctx, fx, p); err != nil {
			return err
		}
	}
	if p.Has(e2eprobe.TraitBucketed) {
		if err := w.ensurePriceSync(ctx, fx, p); err != nil {
			// Drift repair is best effort; the next pass retries.
			w.info(ctx, "world: plan price sync failed", "persona", p.Name(), "error", err.Error())
		}
	}
	if p.Has(e2eprobe.TraitWallet) {
		if err := w.ensureWallet(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func needsCustomer(p *Persona) bool {
	for _, t := range []e2eprobe.Trait{e2eprobe.TraitSubscription, e2eprobe.TraitWallet, e2eprobe.TraitTax, e2eprobe.TraitMultiCadence, e2eprobe.TraitBucketed, e2eprobe.TraitEvents} {
		if p.Has(t) {
			return true
		}
	}
	return false
}

func gapIf(cond bool, reason string) string {
	if cond {
		return reason
	}
	return ""
}

func (w *World) personaMetadata(p *Persona, cohort string) map[string]string {
	return map[string]string{
		"e2eprobe":         "true",
		"e2eprobe_cohort":  cohort,
		"e2eprobe_persona": p.Name(),
		"e2eprobe_run_id":  w.runID,
	}
}

func (w *World) ensureCustomer(ctx context.Context, p *Persona) error {
	ext := p.ExternalID()
	resp, err := w.client.Customers().GetByExternalID(ctx, ext)
	if err == nil {
		if resp.CustomerResponse != nil && resp.CustomerResponse.ID != nil {
			id := *resp.CustomerResponse.ID
			p.update(func(p *Persona) { p.customerID = id })
		}
		return nil
	}
	if !IsNotFound(err) {
		return fmt.Errorf("lookup customer %s: %w", ext, err)
	}
	created, err := w.client.Customers().Create(ctx, types.CreateCustomerRequest{
		ExternalID: ext,
		Name:       "E2EProbe " + p.Name(),
		Email:      ptr(ext + "@e2eprobe.flexprice.invalid"),
		Metadata:   w.personaMetadata(p, PersistentCohort),
	})
	if err != nil {
		return fmt.Errorf("create customer %s: %w", ext, err)
	}
	if created.CustomerResponse != nil && created.CustomerResponse.ID != nil {
		id := *created.CustomerResponse.ID
		p.update(func(p *Persona) { p.customerID = id })
	}
	return nil
}

// primarySubscription picks the persona's monthly subscription out of the
// customer's subscriptions on the plan: tagged with the persona, else the
// persistent cohort, else the oldest untagged one. The quarterly multi-cadence
// subscription is never a candidate.
func primarySubscription(items []types.SubscriptionResponse, persona string) *types.SubscriptionResponse {
	var cohort, untagged []types.SubscriptionResponse
	for _, sub := range items {
		if sub.ID == nil {
			continue
		}
		switch sub.Metadata["e2eprobe_cohort"] {
		case MultiCadenceCohort:
			continue
		case PersistentCohort:
			if sub.Metadata["e2eprobe_persona"] == persona {
				return &sub
			}
			cohort = append(cohort, sub)
		case "":
			untagged = append(untagged, sub)
		}
	}
	for _, pool := range [][]types.SubscriptionResponse{cohort, untagged} {
		if len(pool) == 0 {
			continue
		}
		sort.Slice(pool, func(i, j int) bool { return *pool[i].ID < *pool[j].ID })
		return &pool[0]
	}
	return nil
}

func (w *World) ensureSubscription(ctx context.Context, fx *Fixtures, p *Persona) error {
	ext, planID, spec := p.ExternalID(), fx.PlanID(), p.Spec()
	resp, err := w.client.Subscriptions().Query(ctx, types.SubscriptionFilter{ExternalCustomerID: ptr(ext), PlanID: ptr(planID)})
	if err != nil {
		return fmt.Errorf("query subscriptions: %w", err)
	}
	var items []types.SubscriptionResponse
	if resp.ListSubscriptionsResponse != nil {
		items = resp.ListSubscriptionsResponse.Items
	}
	if existing := primarySubscription(items, p.Name()); existing != nil {
		subID := *existing.ID
		w.activateDraft(ctx, subID, existing.SubscriptionStatus)
		p.update(func(p *Persona) { p.subscriptionID = subID })
		if p.Has(e2eprobe.TraitCommitment) {
			switch {
			case existing.CommitmentAmount == nil || *existing.CommitmentAmount == "":
				p.setGap(e2eprobe.TraitCommitment, "subscription predates the commitment declaration; it can only be set at create")
			case existing.EnableTrueUp == nil || !*existing.EnableTrueUp:
				p.setGap(e2eprobe.TraitCommitment, "subscription predates the commitment declaration: true-up is off and can only be set at create")
			default:
				p.setGap(e2eprobe.TraitCommitment, "")
			}
		}
		if p.Has(e2eprobe.TraitCoupon) {
			attached, err := w.couponAttached(ctx, fx, subID)
			if err != nil {
				return err
			}
			p.setGap(e2eprobe.TraitCoupon, gapIf(!attached, "subscription predates the coupon declaration; it can only be attached at create"))
		}
		return nil
	}

	now := w.now().UTC()
	cycle := types.BillingCycleAnniversary
	req := types.CreateSubscriptionRequest{
		ExternalCustomerID: ptr(ext),
		PlanID:             planID,
		Currency:           "usd",
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: ptr(int64(1)),
		BillingCycle:       &cycle,
		StartDate:          &now,
		Metadata:           w.personaMetadata(p, PersistentCohort),
	}
	if p.Has(e2eprobe.TraitCommitment) && spec.Commitment != nil {
		duration := types.BillingPeriodMonthly
		req.CommitmentAmount = ptr(spec.Commitment.Amount)
		req.CommitmentDuration = &duration
		req.OverageFactor = ptr(spec.Commitment.OverageFactor)
		req.EnableTrueUp = ptr(true)
	}
	if p.Has(e2eprobe.TraitCoupon) {
		if fx.CouponCode() == "" {
			p.setGap(e2eprobe.TraitCoupon, "shared coupon not provisioned")
		} else {
			req.SubscriptionCoupons = []types.SubscriptionCouponInput{{CouponCode: fx.CouponCode()}}
		}
	}
	created, err := w.client.Subscriptions().Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	if created.SubscriptionResponse == nil || created.SubscriptionResponse.ID == nil {
		return errors.New("create subscription: empty response")
	}
	subID := *created.SubscriptionResponse.ID
	w.activateDraft(ctx, subID, created.SubscriptionResponse.SubscriptionStatus)
	p.update(func(p *Persona) { p.subscriptionID = subID })
	p.setGap(e2eprobe.TraitCommitment, "")
	if p.Has(e2eprobe.TraitCoupon) && fx.CouponCode() != "" {
		p.setGap(e2eprobe.TraitCoupon, "")
	}
	return nil
}

// activateDraft activates a draft subscription; a failure is logged and the next pass retries.
func (w *World) activateDraft(ctx context.Context, subID string, status *types.SubscriptionStatus) {
	if status == nil || *status != types.SubscriptionStatusDraft {
		return
	}
	if _, err := w.client.Subscriptions().ActivateSubscription(ctx, subID, types.ActivateDraftSubscriptionRequest{StartDate: w.now().UTC()}); err != nil {
		w.info(ctx, "world: subscription activation failed; retrying next pass", "subscription_id", subID, "error", err.Error())
	}
}

func (w *World) couponAttached(ctx context.Context, fx *Fixtures, subID string) (bool, error) {
	resp, err := w.client.CouponAssociations().List(ctx, listCouponAssociations(subID))
	if err != nil {
		return false, fmt.Errorf("list coupon associations: %w", err)
	}
	if resp == nil || resp.ListCouponAssociationsResponse == nil {
		return false, nil
	}
	for _, ca := range resp.ListCouponAssociationsResponse.Items {
		if fx.CouponID() == "" || (ca.CouponID != nil && *ca.CouponID == fx.CouponID()) {
			return true, nil
		}
	}
	return false, nil
}

func (w *World) ensureTaxAssociation(ctx context.Context, fx *Fixtures, p *Persona) error {
	subID := p.SubscriptionID()
	if subID == "" {
		p.setGap(e2eprobe.TraitTax, "tax requires a subscription")
		return nil
	}
	entityType := string(types.TaxRateEntityTypeSubscription)
	resp, err := w.client.TaxAssociations().List(ctx, &entityType, &subID, nil, nil)
	if err != nil {
		return fmt.Errorf("list tax associations: %w", err)
	}
	if resp != nil && resp.ListTaxAssociationsResponse != nil {
		for _, ta := range resp.ListTaxAssociationsResponse.Items {
			if !taxAssociationMatches(ta, fx) {
				continue
			}
			if ta.TaxRateID != nil {
				fx.setTaxRateID(*ta.TaxRateID)
			}
			w.recordTaxActive(p, ta.StartDate, ta.CreatedAt)
			p.setGap(e2eprobe.TraitTax, "")
			return nil
		}
	}
	taType := types.TaxRateEntityTypeSubscription
	created, err := w.client.TaxAssociations().Create(ctx, types.CreateTaxAssociationRequest{
		TaxRateCode: fx.TaxRateCode(),
		EntityID:    ptr(subID),
		EntityType:  &taType,
		AutoApply:   ptr(true),
		Metadata:    w.personaMetadata(p, PersistentCohort),
	})
	if err != nil {
		if IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("create tax association: %w", err)
	}
	if created.TaxAssociationResponse != nil {
		if created.TaxAssociationResponse.TaxRateID != nil {
			fx.setTaxRateID(*created.TaxAssociationResponse.TaxRateID)
		}
		w.recordTaxActive(p, created.TaxAssociationResponse.StartDate, created.TaxAssociationResponse.CreatedAt)
	}
	p.setGap(e2eprobe.TraitTax, "")
	return nil
}

// taxAssociationMatches accepts the shared rate by id, by code, or any
// association when the rate id is still unknown: the probe only ever attaches
// one rate to a persona's subscription.
func taxAssociationMatches(ta types.TaxAssociationResponse, fx *Fixtures) bool {
	if fx.TaxRateID() != "" && ta.TaxRateID != nil && *ta.TaxRateID == fx.TaxRateID() {
		return true
	}
	if ta.TaxRate != nil && ta.TaxRate.Code != nil && *ta.TaxRate.Code == fx.TaxRateCode() {
		return true
	}
	return fx.TaxRateID() == ""
}

// recordTaxActive pins when the association started, once; a later pass
// without a start date must not walk the boundary forward.
func (w *World) recordTaxActive(p *Persona, start, created *time.Time) {
	p.update(func(p *Persona) {
		switch {
		case start != nil:
			p.taxActiveFrom = *start
		case created != nil:
			p.taxActiveFrom = *created
		case p.taxActiveFrom.IsZero():
			p.taxActiveFrom = w.now().UTC()
		}
	})
}

func (w *World) ensureMultiCadence(ctx context.Context, fx *Fixtures, p *Persona) error {
	ext, planID := p.ExternalID(), fx.PlanID()
	resp, err := w.client.Subscriptions().Query(ctx, types.SubscriptionFilter{ExternalCustomerID: ptr(ext), PlanID: ptr(planID)})
	if err != nil {
		return fmt.Errorf("query subscriptions: %w", err)
	}
	if resp.ListSubscriptionsResponse != nil {
		for _, sub := range resp.ListSubscriptionsResponse.Items {
			if sub.ID == nil || sub.Metadata["e2eprobe_cohort"] != MultiCadenceCohort {
				continue
			}
			subID := *sub.ID
			w.activateDraft(ctx, subID, sub.SubscriptionStatus)
			p.update(func(p *Persona) { p.multiCadenceSubID = subID })
			return nil
		}
	}
	prices, err := w.client.Prices().Query(ctx, planPriceFilter(planID))
	if err != nil {
		return fmt.Errorf("list plan prices: %w", err)
	}
	var priceIDs []string
	if prices != nil && prices.ListPricesResponse != nil {
		for _, pr := range prices.ListPricesResponse.Items {
			if pr.ID != nil && *pr.ID != "" {
				priceIDs = append(priceIDs, *pr.ID)
			}
		}
	}
	if len(priceIDs) == 0 {
		p.setGap(e2eprobe.TraitMultiCadence, "plan has no prices to fan out yet")
		return nil
	}
	now := w.now().UTC()
	cycle := types.BillingCycleAnniversary
	created, err := w.client.Subscriptions().Create(ctx, types.CreateSubscriptionRequest{
		ExternalCustomerID: ptr(ext),
		PlanID:             planID,
		Currency:           "usd",
		BillingPeriod:      types.BillingPeriodQuarterly,
		BillingPeriodCount: ptr(int64(1)),
		BillingCycle:       &cycle,
		StartDate:          &now,
		// Attach every monthly plan price so invoicing fans them out per sub-window.
		IncludePriceIds: priceIDs,
		Metadata:        w.personaMetadata(p, MultiCadenceCohort),
	})
	if err != nil {
		return fmt.Errorf("create quarterly subscription: %w", err)
	}
	if created.SubscriptionResponse == nil || created.SubscriptionResponse.ID == nil {
		return errors.New("create quarterly subscription: empty response")
	}
	subID := *created.SubscriptionResponse.ID
	w.activateDraft(ctx, subID, created.SubscriptionResponse.SubscriptionStatus)
	p.update(func(p *Persona) { p.multiCadenceSubID = subID })
	p.setGap(e2eprobe.TraitMultiCadence, "")
	return nil
}

// ensurePriceSync repairs a subscription whose line items predate a plan price.
// Usage reads filter by the meters on active line items, so a meter added
// after the subscription was created would read zero forever.
func (w *World) ensurePriceSync(ctx context.Context, fx *Fixtures, p *Persona) error {
	subID := p.SubscriptionID()
	if subID == "" {
		return nil
	}
	resp, err := w.client.Subscriptions().Get(ctx, subID)
	if err != nil {
		return fmt.Errorf("get subscription %s: %w", subID, err)
	}
	have := map[string]bool{}
	if resp.SubscriptionResponse != nil {
		for _, li := range resp.SubscriptionResponse.LineItems {
			if li.MeterID != nil {
				have[*li.MeterID] = true
			}
		}
	}
	var missing []string
	for _, m := range e2eprobe.Meters() {
		if id := fx.MeterID(m.Key); id != "" && fx.PriceID(m.Key) != "" && !have[id] {
			missing = append(missing, m.Key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if _, err := w.client.Plans().SyncPrices(ctx, fx.PlanID()); err != nil {
		if isSyncInProgress(err) {
			return nil
		}
		return fmt.Errorf("sync plan prices: %w", err)
	}
	w.info(ctx, "world: synced plan prices onto subscription", "persona", p.Name(), "subscription_id", subID, "missing_meters", strings.Join(missing, ","))
	return nil
}

// isSyncInProgress matches the price sync's own already_exists conflict; a bare
// 409 is not enough, since an unrelated conflict must still surface.
func isSyncInProgress(err error) bool {
	var errResp *sdkerrors.ErrorResponse
	if errors.As(err, &errResp) && errResp.Code != nil && *errResp.Code == types.ErrorCodeAlreadyExists {
		return true
	}
	return err != nil && strings.Contains(err.Error(), string(types.ErrorCodeAlreadyExists))
}

func (w *World) ensureWallet(ctx context.Context, p *Persona) error {
	spec := p.Spec()
	resp, err := w.client.Wallets().GetWalletsByCustomerID(ctx, p.CustomerID())
	if err != nil {
		return fmt.Errorf("list wallets: %w", err)
	}
	if resp != nil && len(resp.WalletResponses) > 0 && resp.WalletResponses[0].ID != nil {
		existing := resp.WalletResponses[0]
		p.update(func(p *Persona) { p.walletID = *existing.ID })
		if p.Has(e2eprobe.TraitAlerts) && spec.Wallet != nil {
			p.setGap(e2eprobe.TraitAlerts, gapIf(!thresholdsMatch(existing.AlertSettings, spec.Wallet.Thresholds), "wallet thresholds differ from the suite"))
		}
		return nil
	}
	req := types.CreateWalletRequest{
		ExternalCustomerID: ptr(p.ExternalID()),
		Currency:           "USD",
		Metadata:           w.personaMetadata(p, PersistentCohort),
	}
	if p.Has(e2eprobe.TraitAlerts) && spec.Wallet != nil {
		req.AlertSettings = alertSettings(spec.Wallet.Thresholds)
	}
	created, err := w.client.Wallets().Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create wallet: %w", err)
	}
	if created.WalletResponse == nil || created.WalletResponse.ID == nil {
		return errors.New("create wallet: empty response")
	}
	walletID := *created.WalletResponse.ID
	p.update(func(p *Persona) { p.walletID = walletID })
	p.setGap(e2eprobe.TraitAlerts, "")
	if spec.Wallet == nil || spec.Wallet.Balance == "" {
		return nil
	}
	if _, err := w.client.Wallets().TopUp(ctx, walletID, types.TopUpWalletRequest{
		Amount:            ptr(spec.Wallet.Balance),
		Description:       ptr("e2eprobe initial balance"),
		TransactionReason: types.TransactionReasonPurchasedCreditDirect,
	}); err != nil {
		return fmt.Errorf("top up wallet %s: %w", walletID, err)
	}
	return nil
}

func alertSettings(th *suite.Thresholds) *types.AlertSettings {
	if th == nil {
		return nil
	}
	below := types.AlertConditionBelow
	threshold := func(v string) *types.AlertThreshold {
		return &types.AlertThreshold{Threshold: ptr(v), Condition: &below}
	}
	return &types.AlertSettings{AlertEnabled: ptr(true), Info: threshold(th.Info), Warning: threshold(th.Warning), Critical: threshold(th.Critical)}
}

func thresholdsMatch(have *types.AlertSettings, want *suite.Thresholds) bool {
	if have == nil || want == nil || have.AlertEnabled == nil || !*have.AlertEnabled {
		return false
	}
	same := func(t *types.AlertThreshold, v string) bool {
		if t == nil || t.Threshold == nil {
			return false
		}
		a, err1 := decimal.NewFromString(*t.Threshold)
		b, err2 := decimal.NewFromString(v)
		return err1 == nil && err2 == nil && a.Equal(b)
	}
	return same(have.Info, want.Info) && same(have.Warning, want.Warning) && same(have.Critical, want.Critical)
}

// checkPaymentFixedCustomer verifies the hand-provisioned card customer exists.
func (w *World) checkPaymentFixedCustomer(ctx context.Context, p *Persona) error {
	spec := p.Spec()
	if !p.Has(e2eprobe.TraitPayment) || spec.Payment == nil || spec.Payment.FixedCustomer == "" {
		return nil
	}
	_, err := w.client.Customers().GetByExternalID(ctx, spec.Payment.FixedCustomer)
	switch {
	case err == nil:
		p.setGap(e2eprobe.TraitPayment, "")
	case IsNotFound(err):
		p.setGap(e2eprobe.TraitPayment, fmt.Sprintf("fixed customer %s not found; create it and save its cards by hand", spec.Payment.FixedCustomer))
	default:
		return fmt.Errorf("lookup fixed customer %s: %w", spec.Payment.FixedCustomer, err)
	}
	return nil
}
