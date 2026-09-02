package service

import (
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// planChangeFingerprint is everything a caller decides on: what kind of change this is,
// what money moves and in which direction, which lines move, what else the change
// touches, and what it warns about. Ids and timestamps are deliberately absent — preview
// mints none of them, so they are the one thing that must differ.
type planChangeFingerprint struct {
	ChangeType    types.SubscriptionChangeType
	IsScheduled   bool
	Documents     []string
	LineItems     []string
	EntityChanges []string
	Warnings      []string
}

func fingerprintPlanChange(resp *dto.SubscriptionChangeV2Response) planChangeFingerprint {
	fp := planChangeFingerprint{
		ChangeType:    resp.ChangeType,
		IsScheduled:   resp.IsScheduled,
		Documents:     []string{},
		LineItems:     []string{},
		EntityChanges: []string{},
		Warnings:      append([]string{}, resp.Warnings...),
	}

	for _, doc := range resp.ChangedResources.Invoices {
		amount := decimal.Zero
		switch {
		case doc.Invoice != nil:
			amount = doc.Invoice.AmountDue
		case doc.WalletTransaction != nil:
			amount = doc.WalletTransaction.Amount
		}
		fp.Documents = append(fp.Documents, fmt.Sprintf("%s:%s", doc.Action, amount.String()))
	}
	for _, li := range resp.ChangedResources.LineItems {
		fp.LineItems = append(fp.LineItems, string(li.ChangeAction))
	}
	for _, c := range resp.EntityChanges {
		fp.EntityChanges = append(fp.EntityChanges, fmt.Sprintf("%s:%s", c.EntityType, c.Behaviour))
	}

	for _, list := range [][]string{fp.Documents, fp.LineItems, fp.EntityChanges, fp.Warnings} {
		sort.Strings(list)
	}
	return fp
}

// paritySubscription is a subscription on the outgoing plan with the plan's grants
// already materialised, so a change has both line items and entity changes to report.
func (s *SubscriptionChangeV2Suite) paritySubscription() *subscription.Subscription {
	sub := s.createSubscription(s.td.starter.ID)
	s.createLineItem(sub, s.td.starterBase, s.td.starter)
	s.materialisePlanGrantsFor(sub, s.td.starter.ID)
	return sub
}

func deferred(req dto.SubscriptionChangeV2Request) dto.SubscriptionChangeV2Request {
	req.ChangeAt = lo.ToPtr(types.ScheduleTypePeriodEnd)
	return req
}

// Preview is the quote the caller commits to. Individual tests below check single
// fields on single scenarios; this one holds the whole answer to account across every
// shape of change, so a field that starts diverging cannot hide behind the others.
func (s *SubscriptionChangeV2Suite) TestPreviewMatchesExecute_AcrossScenarios() {
	ctx := s.GetContext()

	cheap := s.createPlan("Cheap", "cheap_parity")
	s.createFixedPrice(cheap.ID, "base_fee", 5)
	lateral := s.createPlan("Lateral", "lateral_parity")
	s.createFixedPrice(lateral.ID, "base_fee", 20)

	s.createPlanGrant(s.td.starter.ID, "starter credits", 100)
	s.createPlanGrant(s.td.pro.ID, "pro credits", 500)

	unknownAddon := s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)
	unknownAddon.EntityPolicies = &dto.SubscriptionChangeEntityPolicies{
		Addons: &dto.EntityChangePolicy{
			Overrides: map[string]types.EntityChangeBehaviour{
				"subsaddon_not_attached": types.EntityChangeBehaviourDrop,
			},
		},
	}

	tests := []struct {
		name string
		req  dto.SubscriptionChangeV2Request
	}{
		{"upgrade with prorations", s.changeRequest(s.td.pro.ID, types.ProrationBehaviorCreateProrations)},
		{"downgrade with prorations", s.changeRequest(cheap.ID, types.ProrationBehaviorCreateProrations)},
		{"lateral move", s.changeRequest(lateral.ID, types.ProrationBehaviorCreateProrations)},
		{"proration none", s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)},
		{"anchor reset crediting the remainder", s.resetRequest(s.td.pro.ID)},
		{"anchor reset forfeiting the remainder", s.forfeitResetRequest(s.td.pro.ID)},
		{"anchor reset on a downgrade", s.resetRequest(cheap.ID)},
		{"deferred change", deferred(s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone))},
		{"deferred change with a reset", deferred(s.forfeitResetRequest(s.td.pro.ID))},
		{"warns about an addon that is not attached", unknownAddon},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			sub := s.paritySubscription()

			preview, err := s.svc.PreviewPlanChange(ctx, sub.ID, tt.req)
			s.Require().NoError(err)

			executed, err := s.svc.ExecutePlanChange(ctx, sub.ID, tt.req, time.Now().UTC())
			s.Require().NoError(err)

			quoted := fingerprintPlanChange(preview)
			// Equality is worthless if both sides are empty, and a fingerprint that
			// silently stopped reading a field would be exactly that.
			s.Require().NotEmpty(quoted.ChangeType)
			s.Require().NotEmpty(quoted.LineItems)
			s.Require().NotEmpty(quoted.EntityChanges)

			s.Equal(quoted, fingerprintPlanChange(executed))
		})
	}
}

// The unknown-association warning is raised while resolving the change, which both
// entry points share — but only execute asserted it, so a preview that dropped its
// warnings would have gone unnoticed.
func (s *SubscriptionChangeV2Suite) TestPreview_CarriesTheSameWarningsAsExecute() {
	ctx := s.GetContext()

	req := s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)
	req.EntityPolicies = &dto.SubscriptionChangeEntityPolicies{
		Addons: &dto.EntityChangePolicy{
			Overrides: map[string]types.EntityChangeBehaviour{
				"subsaddon_not_attached": types.EntityChangeBehaviourDrop,
			},
		},
	}

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, req)
	s.Require().NoError(err)
	s.Require().Len(preview.Warnings, 1)
	s.Contains(preview.Warnings[0], "subsaddon_not_attached")

	executed, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req, time.Now().UTC())
	s.Require().NoError(err)
	s.Equal(preview.Warnings, executed.Warnings)
}

// usageAndSettlement splits a response's documents by whether they carry the usage
// price, which is the whole point of raising two: usage is billed for the window that
// just ended, the plan-change net is settled on its own document.
func (s *SubscriptionChangeV2Suite) usageAndSettlement(
	resp *dto.SubscriptionChangeV2Response,
	usagePriceID string,
) (usage, settlement *dto.InvoiceResponse) {
	for _, changed := range resp.ChangedResources.Invoices {
		if changed.Invoice == nil {
			continue
		}
		if lo.SomeBy(changed.Invoice.LineItems, func(li *dto.InvoiceLineItemResponse) bool {
			return lo.FromPtr(li.PriceID) == usagePriceID
		}) {
			usage = changed.Invoice
			continue
		}
		settlement = changed.Invoice
	}
	return usage, settlement
}

// Execute raises two documents under a reset with usage. The quote has to show both:
// a caller shown only the settlement would under-read the bill by the usage total.
func (s *SubscriptionChangeV2Suite) TestPreview_ResetQuotesUsageAndSettlementSeparately() {
	ctx := s.GetContext()

	usagePrice := s.seedOutgoingUsage(40, 2)
	s.recordBilled(s.td.baseLine.ID, s.td.starterBase.Amount)

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID))
	s.Require().NoError(err)
	s.Require().Len(preview.ChangedResources.Invoices, 2, "a reset with usage quotes two documents")

	quotedUsage, quotedNet := s.usageAndSettlement(preview, usagePrice.ID)
	s.Require().NotNil(quotedUsage, "the outgoing window's usage must be quoted")
	s.Require().NotNil(quotedNet, "the plan-change net must be quoted separately")
	s.Equal("80", quotedUsage.AmountDue.String(), "40 units at 2")
	s.False(lo.SomeBy(quotedNet.LineItems, func(li *dto.InvoiceLineItemResponse) bool {
		return lo.FromPtr(li.PriceID) == usagePrice.ID
	}), "the quote must not count usage twice either")

	executed, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.resetRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Len(executed.ChangedResources.Invoices, 2)

	raisedUsage, raisedNet := s.usageAndSettlement(executed, usagePrice.ID)
	s.Require().NotNil(raisedUsage)
	s.Require().NotNil(raisedNet)
	s.Equal(quotedUsage.AmountDue.String(), raisedUsage.AmountDue.String(), "usage quoted vs raised")
	s.Equal(quotedNet.AmountDue.String(), raisedNet.AmountDue.String(), "settlement quoted vs raised")
}
