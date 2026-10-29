package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type invoiceCollection struct {
	owners        []domain.OwnerRef
	scopes        []domain.EligibilityScope
	subscriptions []string
}

func (s *invoiceService) withBillingScheduleCollection(ctx context.Context, inv *invoice.Invoice, prepared *dto.InvoiceComputeRequest, compute func(context.Context) error) error {
	if isBillingScheduleRead(ctx) {
		return compute(ctx)
	}
	if inv.InvoiceType != types.InvoiceTypeSubscription || inv.SubscriptionID == nil ||
		inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionTrialStart) || s.CommitmentRepo == nil {
		return compute(ctx)
	}
	if hasBillingScheduleLines(inv) {
		original, err := s.correctionSourceInvoice(ctx, inv)
		if err != nil {
			return err
		}
		if original != nil {
			if prepared == nil {
				return correctionConflict("replacement charges must be prepared before computation", original.ID, inv.ID)
			}
			if err := validateCorrectionCharges(original, prepared.LineItems); err != nil {
				return err
			}
			return compute(ctx)
		}
	}
	discovered, err := s.discoverInvoiceCollection(ctx, inv)
	if err != nil {
		return err
	}
	if len(discovered.owners) == 0 {
		return compute(ctx)
	}
	if s.BillingTerms == nil {
		return ierr.NewError("billing collection service is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	if inv.FxConversion != nil || inv.CustomCurrency != nil {
		return billingScheduleCorrectionRequired(inv)
	}
	cutoff, err := invoiceCollectionCutoff(inv)
	if err != nil {
		return err
	}
	return s.BillingTerms.WithCollection(ctx, core.CollectionRequest{
		InvoiceID: inv.ID, CustomerID: inv.CustomerID, Currency: inv.Currency, Delivery: types.BillingTermSubscriptionInvoice,
		Owners: discovered.owners, EligibilityScopes: discovered.scopes, Cutoff: cutoff, AsOf: time.Now().UTC(),
	}, func(tx context.Context, items []*domain.ScheduleItem) ([]core.InvoiceSource, error) {
		fresh, err := s.discoverInvoiceCollection(tx, inv)
		if err != nil {
			return nil, err
		}
		if !slices.Equal(discovered.owners, fresh.owners) || !slices.Equal(discovered.subscriptions, fresh.subscriptions) {
			return nil, ierr.NewError("invoice collection membership changed; retry the invoice").Mark(ierr.ErrVersionConflict)
		}
		bindings, err := s.appendBillingScheduleLines(tx, inv, items)
		if err != nil {
			return nil, err
		}
		if err := compute(tx); err != nil {
			return nil, err
		}
		return bindings, nil
	})
}

func invoiceCollectionCutoff(inv *invoice.Invoice) (time.Time, error) {
	boundary := inv.PeriodEnd
	switch types.InvoiceBillingReason(inv.BillingReason) {
	case types.InvoiceBillingReasonSubscriptionCreate, types.InvoiceBillingReasonSubscriptionTrialEnd,
		types.InvoiceBillingReasonSubscriptionUpdate, types.InvoiceBillingReasonSubscriptionResume:
		boundary = inv.PeriodStart
	}
	if !finiteScheduleDate(boundary) {
		return time.Time{}, ierr.Validation("subscription collection requires a finite invoice boundary")
	}
	return boundary.UTC(), nil
}

func (s *invoiceService) discoverInvoiceCollection(ctx context.Context, inv *invoice.Invoice) (*invoiceCollection, error) {
	ctx = types.WithForceWriter(ctx)
	root, err := s.SubRepo.Get(ctx, *inv.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if !subscriptionUpdateScoped(ctx, root, *inv.SubscriptionID) || root.GetInvoicingCustomerID() != inv.CustomerID || !strings.EqualFold(root.Currency, inv.Currency) {
		return nil, ierr.NewError("invoice collection destination changed").Mark(ierr.ErrVersionConflict)
	}
	if root.SubscriptionType == types.SubscriptionTypeGroupedInvoicing || root.SubscriptionType == types.SubscriptionTypeInherited {
		return nil, ierr.NewError("child subscription cannot collect through its own invoice").Mark(ierr.ErrVersionConflict)
	}
	participants := []*subscription.Subscription{root}
	if root.SubscriptionType == types.SubscriptionTypeParent {
		children, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
			QueryFilter:           &types.QueryFilter{Limit: lo.ToPtr(domain.MaxEligibilityScopes), Status: lo.ToPtr(types.StatusPublished)},
			ParentSubscriptionIDs: []string{root.ID}, SubscriptionTypes: []types.SubscriptionType{types.SubscriptionTypeGroupedInvoicing},
			SubscriptionStatus: types.AllSubscriptionStatuses(),
		})
		if err != nil {
			return nil, err
		}
		if len(children) >= domain.MaxEligibilityScopes {
			return nil, ierr.Validation("one invoice supports at most 500 subscription participants")
		}
		participants = append(participants, children...)
	}
	out := &invoiceCollection{}
	for _, sub := range participants {
		if sub == nil || !subscriptionUpdateScoped(ctx, sub, sub.ID) || (sub.ID != root.ID && lo.FromPtr(sub.ParentSubscriptionID) != root.ID) {
			return nil, ierr.NewError("invalid invoice collection membership").Mark(ierr.ErrDatabase)
		}
		out.subscriptions = append(out.subscriptions, sub.ID)
	}
	slices.Sort(out.subscriptions)
	out.subscriptions = slices.Compact(out.subscriptions)
	for _, id := range out.subscriptions {
		out.scopes = append(out.scopes, domain.EligibilityScope{Type: "subscription", ID: id})
	}
	rows, err := s.CommitmentRepo.List(ctx, &commitment.Filter{QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(core.MaxItems + 1), Status: lo.ToPtr(types.StatusPublished)}, SubscriptionIDs: out.subscriptions})
	if err != nil {
		return nil, err
	}
	if len(rows) > core.MaxItems {
		return nil, ierr.Validation("one invoice supports at most 500 commitments")
	}
	for _, owner := range rows {
		if owner == nil || owner.TenantID != types.GetTenantID(ctx) || owner.EnvironmentID != types.GetEnvironmentID(ctx) || !slices.Contains(out.subscriptions, owner.SubscriptionID) {
			return nil, ierr.NewError("invalid invoice collection owner scope").Mark(ierr.ErrDatabase)
		}
		if commitmentCollectionMethod(owner) != types.CommitmentCollectionBillingTerms {
			continue
		}
		delivery := types.BillingTermSubscriptionInvoice
		if owner.InstalmentInvoicing != nil {
			delivery = types.BillingTermDelivery(*owner.InstalmentInvoicing)
		}
		settlement, err := readCommitmentSettlement(owner)
		if err != nil {
			return nil, err
		}
		if settlement != nil {
			delivery = settlement.Delivery
		}
		if delivery.Validate() != nil {
			return nil, ierr.Validation("commitment collection delivery is invalid")
		}
		if delivery != types.BillingTermSubscriptionInvoice {
			continue
		}
		out.owners = append(out.owners, domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: owner.ID})
	}
	slices.SortFunc(out.owners, func(a, b domain.OwnerRef) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// appendBillingScheduleLines accepts only the sources selected under the collection core's locks.
func (s *invoiceService) appendBillingScheduleLines(ctx context.Context, inv *invoice.Invoice, items []*domain.ScheduleItem) ([]core.InvoiceSource, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if inv == nil || inv.InvoiceStatus != types.InvoiceStatusDraft || inv.TenantID != types.GetTenantID(ctx) || inv.EnvironmentID != types.GetEnvironmentID(ctx) || inv.FxConversion != nil || inv.CustomCurrency != nil {
		return nil, ierr.Validation("scheduled charges require a scoped draft in the obligation currency")
	}
	if err := validateScheduleCurrency(ctx, s.CustomerRepo, inv.CustomerID, inv.Currency); err != nil {
		return nil, err
	}
	lines := make([]*invoice.InvoiceLineItem, 0, len(items))
	bindings := make([]core.InvoiceSource, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, line := range inv.LineItems {
		if line != nil && line.BillingScheduleItemID != nil {
			seen[*line.BillingScheduleItemID] = true
		}
	}
	for _, item := range items {
		if item == nil || item.ID == "" || seen[item.ID] || item.TenantID != inv.TenantID || item.EnvironmentID != inv.EnvironmentID || item.CustomerID != inv.CustomerID || !strings.EqualFold(item.Currency, inv.Currency) || item.Status != types.StatusPublished || item.ScheduleStatus != types.BillingScheduleScheduled || item.InvoiceID != nil || item.InvoiceLineItemID != nil || !item.Amount.IsPositive() {
			return nil, ierr.Validation("invalid scheduled charge for invoice destination")
		}
		seen[item.ID] = true
		req := dto.CreateInvoiceLineItemRequest{BillingScheduleItemID: lo.ToPtr(item.ID), SubscriptionID: item.SubscriptionID,
			Amount: item.Amount, Quantity: decimal.NewFromInt(1), PeriodStart: lo.ToPtr(item.ServicePeriodStart), PeriodEnd: lo.ToPtr(item.ServicePeriodEnd),
			DisplayName: lo.ToPtr(item.Description), Metadata: lo.Assign(types.Metadata{}, item.Metadata),
		}
		if item.OwnerType == types.BillingTermOwnerCommitment {
			req.CommitmentID = lo.ToPtr(item.OwnerID)
			req.EntityType = lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment))
			req.EntityID = lo.ToPtr(item.OwnerID)
		}
		line := req.ToInvoiceLineItem(ctx, inv)
		if !validRetainedScheduleLine(inv, line) || !line.Amount.Equal(item.Amount) {
			return nil, ierr.Validation("invalid scheduled charge principal or service period")
		}
		lines = append(lines, line)
		bindings = append(bindings, core.InvoiceSource{ItemID: item.ID, InvoiceLineItemID: line.ID})
	}
	if err := s.InvoiceRepo.AddLineItems(ctx, inv.ID, lines); err != nil {
		return nil, err
	}
	inv.LineItems = append(inv.LineItems, lines...)
	return bindings, nil
}
