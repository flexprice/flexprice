package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// SubscriptionTrialCascadeE2ESuite is a regression suite for inherited children
// getting stuck after a parent's trial ends: cascadeTrialEndToInherited parks
// children in `incomplete`, but the activation cascade fetched children with the
// default status set (active/trialing/draft) — an incomplete child was never
// found, so it was never activated.
type SubscriptionTrialCascadeE2ESuite struct {
	testutil.BaseServiceTestSuite
	svc SubscriptionService
}

func TestSubscriptionTrialCascadeE2E(t *testing.T) {
	suite.Run(t, new(SubscriptionTrialCascadeE2ESuite))
}

func (s *SubscriptionTrialCascadeE2ESuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewSubscriptionService(newTestServiceParams(&s.BaseServiceTestSuite))
}

func (s *SubscriptionTrialCascadeE2ESuite) internalService() *subscriptionService {
	return s.svc.(*subscriptionService)
}

// createTrialSubWithParent creates customer + plan + subscription whose trial
// ended an hour ago. withFixedCharge controls whether the trial-end invoice
// has an amount (paid path) or is zero-amount (auto-activation path).
func (s *SubscriptionTrialCascadeE2ESuite) createTrialSubWithParent(subType types.SubscriptionType, parentSubID *string, withFixedCharge bool) *subscription.Subscription {
	ctx := s.GetContext()

	cust := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_" + s.GetUUID(),
		Name:       "Trial Cascade Customer",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Trial Cascade Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	trialStart := time.Now().UTC().Add(-14 * 24 * time.Hour)
	trialEnd := time.Now().UTC().Add(-1 * time.Hour)

	sub := &subscription.Subscription{
		ID:                   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:           cust.ID,
		PlanID:               pl.ID,
		SubscriptionStatus:   types.SubscriptionStatusTrialing,
		SubscriptionType:     subType,
		ParentSubscriptionID: parentSubID,
		Currency:             "usd",
		BillingAnchor:        trialStart,
		BillingCycle:         types.BillingCycleAnniversary,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		BillingCadence:       types.BILLING_CADENCE_RECURRING,
		StartDate:            trialStart,
		CurrentPeriodStart:   trialStart,
		CurrentPeriodEnd:     trialEnd,
		TrialStart:           &trialStart,
		TrialEnd:             &trialEnd,
		CollectionMethod:     string(types.CollectionMethodSendInvoice),
		PaymentBehavior:      string(types.PaymentBehaviorDefaultIncomplete),
		BaseModel:            types.GetDefaultBaseModel(ctx),
	}

	var lineItems []*subscription.SubscriptionLineItem
	if withFixedCharge {
		p := &price.Price{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
			Amount:             decimal.NewFromInt(10),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, p))
		lineItems = append(lineItems, &subscription.SubscriptionLineItem{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  sub.ID,
			CustomerID:      cust.ID,
			EntityID:        pl.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: pl.Name,
			PriceID:         p.ID,
			PriceType:       p.Type,
			DisplayName:     "Fixed Charge",
			Quantity:        decimal.NewFromInt(1),
			Currency:        sub.Currency,
			BillingPeriod:   sub.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceAdvance,
			StartDate:       trialStart,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		})
	}

	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	return sub
}

func (s *SubscriptionTrialCascadeE2ESuite) subStatus(id string) types.SubscriptionStatus {
	stored, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), id)
	s.Require().NoError(err)
	return stored.SubscriptionStatus
}

// TestTrialEndThenInvoicePaidActivatesInheritedChildren covers the full paid
// path: trial ends → parent + children go incomplete with an open trial-end
// invoice → invoice is paid → parent AND children become active.
func (s *SubscriptionTrialCascadeE2ESuite) TestTrialEndThenInvoicePaidActivatesInheritedChildren() {
	ctx := s.GetContext()
	now := time.Now().UTC()

	parent := s.createTrialSubWithParent(types.SubscriptionTypeParent, nil, true)
	childA := s.createTrialSubWithParent(types.SubscriptionTypeInherited, &parent.ID, false)
	childB := s.createTrialSubWithParent(types.SubscriptionTypeInherited, &parent.ID, false)

	inv, err := s.internalService().ProcessSingleSubscriptionTrialEnd(ctx, parent, now)
	s.Require().NoError(err)
	s.Require().NotNil(inv, "fixed charge must produce a trial-end invoice")

	s.Run("trial_end_parks_parent_and_children_in_incomplete", func() {
		s.Equal(types.SubscriptionStatusIncomplete, s.subStatus(parent.ID))
		s.Equal(types.SubscriptionStatusIncomplete, s.subStatus(childA.ID))
		s.Equal(types.SubscriptionStatusIncomplete, s.subStatus(childB.ID))
	})

	s.Run("paying_the_trial_end_invoice_activates_parent_and_children", func() {
		domainInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.Require().NoError(err)

		s.Require().NoError(s.internalService().HandleSubscriptionActivatingInvoicePaid(ctx, domainInv))

		s.Equal(types.SubscriptionStatusActive, s.subStatus(parent.ID))
		s.Equal(types.SubscriptionStatusActive, s.subStatus(childA.ID),
			"inherited child must not stay stuck in incomplete after the activating invoice is paid")
		s.Equal(types.SubscriptionStatusActive, s.subStatus(childB.ID),
			"inherited child must not stay stuck in incomplete after the activating invoice is paid")
	})

	s.Run("repeat_activation_is_idempotent", func() {
		domainInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
		s.Require().NoError(err)
		s.Require().NoError(s.internalService().HandleSubscriptionActivatingInvoicePaid(ctx, domainInv))
		s.Equal(types.SubscriptionStatusActive, s.subStatus(parent.ID))
		s.Equal(types.SubscriptionStatusActive, s.subStatus(childA.ID))
	})
}

// TestZeroAmountTrialEndActivatesInheritedChildren covers the zero-amount
// path: no trial-end invoice is created, the parent auto-activates inline —
// the children must come along instead of staying incomplete.
func (s *SubscriptionTrialCascadeE2ESuite) TestZeroAmountTrialEndActivatesInheritedChildren() {
	ctx := s.GetContext()
	now := time.Now().UTC()

	parent := s.createTrialSubWithParent(types.SubscriptionTypeParent, nil, false)
	child := s.createTrialSubWithParent(types.SubscriptionTypeInherited, &parent.ID, false)

	inv, err := s.internalService().ProcessSingleSubscriptionTrialEnd(ctx, parent, now)
	s.Require().NoError(err)
	s.Nil(inv, "zero-amount trial end must not create an invoice")

	s.Equal(types.SubscriptionStatusActive, s.subStatus(parent.ID))
	s.Equal(types.SubscriptionStatusActive, s.subStatus(child.ID),
		"inherited child must be activated by the zero-amount trial-end path")
}
