package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto" // pragma: allowlist secret
	"github.com/flexprice/flexprice/internal/domain/creditgrant" // pragma: allowlist secret
	"github.com/flexprice/flexprice/internal/domain/subscription" // pragma: allowlist secret
	ierr "github.com/flexprice/flexprice/internal/errors" // pragma: allowlist secret
	"github.com/flexprice/flexprice/internal/types" // pragma: allowlist secret
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func (s *SubscriptionModificationServiceSuite) createFixedTermSub(customerID string, endDate time.Time) *subscription.Subscription {
	ctx := s.GetContext()
	now := s.GetNow()
	p := s.createPlan()
	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		CustomerID:         customerID,
		PlanID:             p.ID,
		Currency:           "USD",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingAnchor:      now,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		StartDate:          now,
		EndDate:            lo.ToPtr(endDate),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	return sub
}

func (s *SubscriptionModificationServiceSuite) createLineItemWithEnd(
	subID, customerID string,
	endDate time.Time,
	billingPeriod types.BillingPeriod,
) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	now := s.GetNow()
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: subID,
		CustomerID:     customerID,
		PriceID:        types.GenerateUUID(),
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       decimal.NewFromInt(1),
		Currency:       "USD",
		BillingPeriod:  billingPeriod,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      now,
		EndDate:        endDate,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

func (s *SubscriptionModificationServiceSuite) createSubscriptionCreditGrant(subID string, endDate *time.Time) *creditgrant.CreditGrant {
	ctx := s.GetContext()
	now := s.GetNow()
	grant := &creditgrant.CreditGrant{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT),
		Name:           "test-grant",
		Scope:          types.CreditGrantScopeSubscription,
		SubscriptionID: lo.ToPtr(subID),
		Credits:        decimal.NewFromInt(100),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		StartDate:      lo.ToPtr(now),
		EndDate:        endDate,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	created, err := s.GetStores().CreditGrantRepo.Create(ctx, grant)
	s.Require().NoError(err)
	return created
}

func endDateModifyReq(newEnd time.Time) dto.ExecuteSubscriptionModifyRequest {
	return dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeEndDate,
		EndDateParams: &dto.SubModifyEndDateRequest{
			NewEndDate: newEnd,
		},
	}
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_HappyPathExtendsSubAndMatchingLineItems() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-happy")
	oldEnd := s.GetNow().AddDate(0, 6, 0).UTC()
	newEnd := oldEnd.AddDate(0, 6, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)

	matching := s.createLineItemWithEnd(sub.ID, cust.ID, oldEnd, types.BILLING_PERIOD_MONTHLY)
	override := s.createLineItemWithEnd(sub.ID, cust.ID, oldEnd.AddDate(0, -2, 0), types.BILLING_PERIOD_MONTHLY)
	openEnded := s.createLineItemWithEnd(sub.ID, cust.ID, time.Time{}, types.BILLING_PERIOD_MONTHLY)
	oneTime := s.createLineItemWithEnd(sub.ID, cust.ID, oldEnd.AddDate(0, -3, 0), types.BILLING_PERIOD_ONETIME)

	resp, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(newEnd))
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().NotNil(resp.Subscription)
	s.Require().NotNil(resp.Subscription.EndDate)
	s.Equal(newEnd, resp.Subscription.EndDate.UTC())

	s.Require().Len(resp.ChangedResources.Subscriptions, 1)
	s.Equal(sub.ID, resp.ChangedResources.Subscriptions[0].ID)
	s.Equal(dto.ChangedSubscriptionActionUpdated, resp.ChangedResources.Subscriptions[0].Action)
	s.Require().NotNil(resp.ChangedResources.Subscriptions[0].EndDate)
	s.Equal(newEnd, resp.ChangedResources.Subscriptions[0].EndDate.UTC())

	changedIDs := map[string]bool{}
	for _, li := range resp.ChangedResources.LineItems {
		changedIDs[li.ID] = true
		s.Equal(dto.ChangedLineItemActionUpdated, li.ChangeAction)
		s.Require().NotNil(li.EndDate)
		s.Equal(newEnd, li.EndDate.UTC())
	}
	s.True(changedIDs[matching.ID])
	s.True(changedIDs[openEnded.ID])
	s.False(changedIDs[override.ID])
	s.False(changedIDs[oneTime.ID])

	storedMatching, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, matching.ID)
	s.Require().NoError(err)
	s.Equal(newEnd, storedMatching.EndDate.UTC())

	storedOverride, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, override.ID)
	s.Require().NoError(err)
	s.Equal(oldEnd.AddDate(0, -2, 0).UTC(), storedOverride.EndDate.UTC())

	storedOpen, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, openEnded.ID)
	s.Require().NoError(err)
	s.Equal(newEnd, storedOpen.EndDate.UTC())

	storedOneTime, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, oneTime.ID)
	s.Require().NoError(err)
	s.Equal(oldEnd.AddDate(0, -3, 0).UTC(), storedOneTime.EndDate.UTC())
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_PreviewDoesNotPersist() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-preview")
	oldEnd := s.GetNow().AddDate(0, 6, 0).UTC()
	newEnd := oldEnd.AddDate(0, 3, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)
	li := s.createLineItemWithEnd(sub.ID, cust.ID, oldEnd, types.BILLING_PERIOD_MONTHLY)

	resp, err := s.service.Preview(ctx, sub.ID, endDateModifyReq(newEnd))
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().Len(resp.ChangedResources.Subscriptions, 1)
	s.Require().Len(resp.ChangedResources.LineItems, 1)

	storedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().NotNil(storedSub.EndDate)
	s.Equal(oldEnd, storedSub.EndDate.UTC())

	storedLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.Equal(oldEnd, storedLI.EndDate.UTC())
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_NilEndRejects() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-nil")
	sub := s.createActiveSub(cust.ID) // no EndDate
	newEnd := s.GetNow().AddDate(1, 0, 0).UTC()

	_, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(newEnd))
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_ShortenRejects() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-shorten")
	oldEnd := s.GetNow().AddDate(0, 6, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)

	_, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(oldEnd.AddDate(0, -1, 0)))
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_EqualIsNoOp() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-noop")
	oldEnd := s.GetNow().AddDate(0, 6, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)
	li := s.createLineItemWithEnd(sub.ID, cust.ID, oldEnd, types.BILLING_PERIOD_MONTHLY)

	resp, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(oldEnd))
	s.Require().NoError(err)
	s.Empty(resp.ChangedResources.Subscriptions)
	s.Empty(resp.ChangedResources.LineItems)

	storedLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.Equal(oldEnd, storedLI.EndDate.UTC())
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_CancelledRejects() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-cancelled")
	oldEnd := s.GetNow().AddDate(0, 6, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)
	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(oldEnd.AddDate(0, 3, 0)))
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_InheritedRejects() {
	ctx := s.GetContext()
	_, _, _, inherited := s.createParentSubWithChild("end-date-parent", "end-date-child")
	inherited.EndDate = lo.ToPtr(s.GetNow().AddDate(0, 6, 0).UTC())
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, inherited))

	_, err := s.service.Execute(ctx, inherited.ID, endDateModifyReq(s.GetNow().AddDate(1, 0, 0).UTC()))
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_TrialingBeforeTrialEndRejects() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-trial")
	now := s.GetNow()
	oldEnd := now.AddDate(0, 6, 0).UTC()
	trialEnd := now.AddDate(0, 2, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)
	sub.SubscriptionStatus = types.SubscriptionStatusTrialing
	sub.TrialStart = lo.ToPtr(now)
	sub.TrialEnd = lo.ToPtr(trialEnd)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	// new end after old end but before trial end — impossible if trialEnd < oldEnd;
	// set trial end after old end first to exercise the rule.
	sub.TrialEnd = lo.ToPtr(oldEnd.AddDate(0, 3, 0).UTC())
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(oldEnd.AddDate(0, 1, 0).UTC()))
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_ClippedPeriodEndRecomputed() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-period")
	now := s.GetNow().UTC()
	// Term cliffs mid-cycle: natural next period would be now+1mo, but EndDate clips to +15d.
	oldEnd := now.AddDate(0, 0, 15).UTC()
	newEnd := now.AddDate(0, 6, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)
	sub.CurrentPeriodStart = now
	sub.CurrentPeriodEnd = oldEnd
	sub.BillingAnchor = now
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	resp, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(newEnd))
	s.Require().NoError(err)

	stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(newEnd, stored.EndDate.UTC())
	s.True(stored.CurrentPeriodEnd.After(oldEnd),
		"period end should be recomputed past the old cliff, got %v", stored.CurrentPeriodEnd)
	s.True(!stored.CurrentPeriodEnd.After(newEnd))
	s.Require().NotNil(resp.ChangedResources.Subscriptions[0].CurrentPeriodEnd)
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_CreditGrantsExtendedAndReturned() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-grants")
	oldEnd := s.GetNow().AddDate(0, 6, 0).UTC()
	newEnd := oldEnd.AddDate(0, 6, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)

	matchingGrant := s.createSubscriptionCreditGrant(sub.ID, lo.ToPtr(oldEnd))
	earlierGrant := s.createSubscriptionCreditGrant(sub.ID, lo.ToPtr(oldEnd.AddDate(0, -1, 0)))
	openGrant := s.createSubscriptionCreditGrant(sub.ID, nil)

	resp, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(newEnd))
	s.Require().NoError(err)
	s.Require().Len(resp.ChangedResources.CreditGrants, 1)
	s.Equal(matchingGrant.ID, resp.ChangedResources.CreditGrants[0].ID)
	s.Equal(dto.ChangedCreditGrantActionUpdated, resp.ChangedResources.CreditGrants[0].Action)
	s.Require().NotNil(resp.ChangedResources.CreditGrants[0].EndDate)
	s.Equal(newEnd, resp.ChangedResources.CreditGrants[0].EndDate.UTC())
	s.Require().NotNil(resp.ChangedResources.CreditGrants[0].CreditGrant)

	storedMatching, err := s.GetStores().CreditGrantRepo.Get(ctx, matchingGrant.ID)
	s.Require().NoError(err)
	s.Require().NotNil(storedMatching.EndDate)
	s.Equal(newEnd, storedMatching.EndDate.UTC())

	storedEarlier, err := s.GetStores().CreditGrantRepo.Get(ctx, earlierGrant.ID)
	s.Require().NoError(err)
	s.Require().NotNil(storedEarlier.EndDate)
	s.Equal(oldEnd.AddDate(0, -1, 0).UTC(), storedEarlier.EndDate.UTC())

	storedOpen, err := s.GetStores().CreditGrantRepo.Get(ctx, openGrant.ID)
	s.Require().NoError(err)
	s.Nil(storedOpen.EndDate)
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_PreviewReturnsCreditGrants() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-grants-preview")
	oldEnd := s.GetNow().AddDate(0, 6, 0).UTC()
	newEnd := oldEnd.AddDate(0, 6, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)
	grant := s.createSubscriptionCreditGrant(sub.ID, lo.ToPtr(oldEnd))

	resp, err := s.service.Preview(ctx, sub.ID, endDateModifyReq(newEnd))
	s.Require().NoError(err)
	s.Require().Len(resp.ChangedResources.CreditGrants, 1)
	s.Equal(grant.ID, resp.ChangedResources.CreditGrants[0].ID)

	stored, err := s.GetStores().CreditGrantRepo.Get(ctx, grant.ID)
	s.Require().NoError(err)
	s.Equal(oldEnd, stored.EndDate.UTC(), "preview must not persist grant end")
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_UnwindScheduledCancellation() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-cancel")
	now := s.GetNow().UTC()
	oldEnd := now.AddDate(0, 6, 0).UTC()
	cancelAt := now.AddDate(0, 2, 0).UTC()
	newEnd := now.AddDate(0, 8, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)
	sub.CancelAt = lo.ToPtr(cancelAt)
	sub.CancelAtPeriodEnd = true
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	// Line item bulk-terminated at cancel boundary.
	li := s.createLineItemWithEnd(sub.ID, cust.ID, cancelAt, types.BILLING_PERIOD_MONTHLY)

	resp, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(newEnd))
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	stored, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Nil(stored.CancelAt)
	s.False(stored.CancelAtPeriodEnd)
	s.Equal(newEnd, stored.EndDate.UTC())

	storedLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.Equal(newEnd, storedLI.EndDate.UTC())
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_CancelAtNotPastRejects() {
	ctx := s.GetContext()
	cust := s.createCustomer("end-date-cancel-reject")
	now := s.GetNow().UTC()
	oldEnd := now.AddDate(0, 6, 0).UTC()
	// Scheduled cancel after current end: extending past old end but not past cancel_at must reject.
	cancelAt := now.AddDate(0, 8, 0).UTC()
	sub := s.createFixedTermSub(cust.ID, oldEnd)
	sub.CancelAt = lo.ToPtr(cancelAt)
	sub.CancelAtPeriodEnd = true
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := s.service.Execute(ctx, sub.ID, endDateModifyReq(oldEnd.AddDate(0, 1, 0).UTC()))
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionModificationServiceSuite) TestEndDate_CascadesToInheritedChildren() {
	ctx := s.GetContext()
	parentCust := s.createCustomer("end-date-cascade-parent")
	childCust := s.createCustomer("end-date-cascade-child")
	oldEnd := s.GetNow().AddDate(0, 6, 0).UTC()
	newEnd := oldEnd.AddDate(0, 6, 0).UTC()

	parentSub := s.createFixedTermSub(parentCust.ID, oldEnd)
	parentLI := s.createLineItemWithEnd(parentSub.ID, parentCust.ID, oldEnd, types.BILLING_PERIOD_MONTHLY)

	_, err := s.service.Execute(ctx, parentSub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeInheritance,
		InheritanceParams: &dto.SubModifyInheritanceRequest{
			ExternalCustomerIDsToInheritSubscription: []string{childCust.ExternalID},
		},
	})
	s.Require().NoError(err)

	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = childCust.ID
	children, err := s.GetStores().SubscriptionRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(children, 1)
	childSub := children[0]
	childSub.EndDate = lo.ToPtr(oldEnd)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, childSub))
	childLI := s.createLineItemWithEnd(childSub.ID, childCust.ID, oldEnd, types.BILLING_PERIOD_MONTHLY)

	resp, err := s.service.Execute(ctx, parentSub.ID, endDateModifyReq(newEnd))
	s.Require().NoError(err)

	subIDs := map[string]bool{}
	for _, cs := range resp.ChangedResources.Subscriptions {
		subIDs[cs.ID] = true
	}
	s.True(subIDs[parentSub.ID])
	s.True(subIDs[childSub.ID])

	storedChild, err := s.GetStores().SubscriptionRepo.Get(ctx, childSub.ID)
	s.Require().NoError(err)
	s.Equal(newEnd, storedChild.EndDate.UTC())

	storedParentLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, parentLI.ID)
	s.Require().NoError(err)
	s.Equal(newEnd, storedParentLI.EndDate.UTC())

	storedChildLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, childLI.ID)
	s.Require().NoError(err)
	s.Equal(newEnd, storedChildLI.EndDate.UTC())
}

func TestSelectLineItemsToExtend(t *testing.T) {
	oldEnd := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	cancelAt := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	matching := &subscription.SubscriptionLineItem{
		ID: "match", EndDate: oldEnd, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}
	open := &subscription.SubscriptionLineItem{
		ID: "open", EndDate: time.Time{}, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}
	override := &subscription.SubscriptionLineItem{
		ID: "override", EndDate: oldEnd.AddDate(0, -1, 0), BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}
	oneTime := &subscription.SubscriptionLineItem{
		ID: "onetime", EndDate: oldEnd, BillingPeriod: types.BILLING_PERIOD_ONETIME,
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}
	deleted := &subscription.SubscriptionLineItem{
		ID: "deleted", EndDate: oldEnd, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BaseModel: types.BaseModel{Status: types.StatusDeleted},
	}
	cancelTerminated := &subscription.SubscriptionLineItem{
		ID: "cancel", EndDate: cancelAt, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}

	selected := selectLineItemsToExtend([]*subscription.SubscriptionLineItem{
		matching, open, override, oneTime, deleted, cancelTerminated,
	}, oldEnd, nil)
	ids := map[string]bool{}
	for _, li := range selected {
		ids[li.ID] = true
	}
	require.True(t, ids["match"])
	require.True(t, ids["open"])
	require.False(t, ids["override"])
	require.False(t, ids["onetime"])
	require.False(t, ids["deleted"])
	require.False(t, ids["cancel"])

	selectedWithCancel := selectLineItemsToExtend([]*subscription.SubscriptionLineItem{
		matching, override, cancelTerminated,
	}, oldEnd, &cancelAt)
	ids2 := map[string]bool{}
	for _, li := range selectedWithCancel {
		ids2[li.ID] = true
	}
	require.True(t, ids2["match"])
	require.True(t, ids2["cancel"])
	require.False(t, ids2["override"])
}
