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
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type billingScheduleReadKey struct{}

func withoutNewBillingClaims(ctx context.Context) context.Context {
	return context.WithValue(ctx, billingScheduleReadKey{}, true)
}

func isBillingScheduleRead(ctx context.Context) bool {
	read, _ := ctx.Value(billingScheduleReadKey{}).(bool)
	return read
}

type billingScheduleProjection struct {
	due      []*domain.ScheduleItem
	assigned []*domain.ScheduleItem
}

func projectBillingSchedule(ctx context.Context, params ServiceParams, owners []domain.OwnerRef, customerID, currency string, asOf time.Time) (*billingScheduleProjection, error) {
	projection := &billingScheduleProjection{}
	if len(owners) == 0 {
		return projection, nil
	}
	if params.BillingTerms == nil || len(owners) > core.MaxItems {
		return nil, ierr.NewError("billing schedule projection is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	seen := make(map[string]bool)
	for _, ref := range owners {
		state, err := params.BillingTerms.Get(ctx, ref)
		if ierr.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		owner, summary := state.Owner(), state.Summary()
		if owner == nil || summary == nil || owner.Owner != ref || owner.TenantID != types.GetTenantID(ctx) || owner.EnvironmentID != types.GetEnvironmentID(ctx) {
			return nil, ierr.NewError("invalid projected billing owner scope").Mark(ierr.ErrDatabase)
		}
		if owner.CustomerID != customerID || !strings.EqualFold(owner.Currency, currency) {
			continue
		}
		for _, item := range state.Items() {
			if item == nil || item.ID == "" || seen[item.ID] || item.TenantID != owner.TenantID || item.EnvironmentID != owner.EnvironmentID || item.OwnerType != ref.Type || item.OwnerID != ref.ID || item.CustomerID != customerID || !strings.EqualFold(item.Currency, currency) {
				return nil, ierr.NewError("invalid projected billing source scope").Mark(ierr.ErrDatabase)
			}
			seen[item.ID] = true
			if len(seen) > core.MaxItems {
				return nil, ierr.Validation("billing projection supports at most 500 relevant schedule items")
			}
			switch item.ScheduleStatus {
			case types.BillingScheduleScheduled:
				if owner.CanCollect && !summary.RequiresReplan && item.Status == types.StatusPublished && item.BillOn != nil && !item.BillOn.After(asOf) {
					projection.due = append(projection.due, item)
				}
			case types.BillingScheduleInvoiced:
				projection.assigned = append(projection.assigned, item)
			}
		}
	}
	return projection, nil
}

func billingProjectionOwners(ctx context.Context, params ServiceParams, subscriptions []string) ([]domain.OwnerRef, error) {
	if len(subscriptions) == 0 || params.CommitmentRepo == nil {
		return nil, nil
	}
	rows, err := params.CommitmentRepo.List(types.WithForceWriter(ctx), &commitment.Filter{
		QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(core.MaxItems + 1), Status: lo.ToPtr(types.StatusPublished)}, SubscriptionIDs: subscriptions,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) > core.MaxItems {
		return nil, ierr.Validation("billing projection supports at most 500 commitments")
	}
	owners := make([]domain.OwnerRef, 0, len(rows))
	for _, owner := range rows {
		if owner == nil || !commitmentTermsScope(ctx, owner.TenantID, owner.EnvironmentID, owner.Status) || !slices.Contains(subscriptions, owner.SubscriptionID) {
			return nil, ierr.NewError("invalid billing projection commitment scope").Mark(ierr.ErrDatabase)
		}
		if commitmentUsesTerms(owner) {
			owners = append(owners, commitmentBillingOwner(owner.ID))
		}
	}
	slices.SortFunc(owners, func(a, b domain.OwnerRef) int { return strings.Compare(a.ID, b.ID) })
	return owners, nil
}

func billingProjectionSubscriptions(ctx context.Context, params ServiceParams, customerID string) ([]string, error) {
	if types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" || customerID == "" {
		return nil, ierr.Validation("billing projection requires a scoped customer")
	}
	ids, parents := make(map[string]bool), []string{}
	filters := []*types.SubscriptionFilter{{CustomerID: customerID}, {InvoicingCustomerIDs: []string{customerID}}}
	for index := 0; index < len(filters); index++ {
		filter := filters[index]
		filter.QueryFilter = &types.QueryFilter{Limit: lo.ToPtr(domain.MaxEligibilityScopes + 1), Status: lo.ToPtr(types.StatusPublished)}
		filter.SubscriptionStatus = types.AllSubscriptionStatuses()
		rows, err := params.SubRepo.List(types.WithForceWriter(ctx), filter)
		if err != nil {
			return nil, err
		}
		if len(rows) > domain.MaxEligibilityScopes {
			return nil, ierr.Validation("billing projection supports at most 500 subscriptions")
		}
		for _, sub := range rows {
			if sub == nil || !subscriptionUpdateScoped(ctx, sub, sub.ID) {
				return nil, ierr.NewError("invalid billing projection subscription scope").Mark(ierr.ErrDatabase)
			}
			grouped := index == 2 && sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing && slices.Contains(parents, lo.FromPtr(sub.ParentSubscriptionID))
			if !grouped && sub.GetInvoicingCustomerID() != customerID {
				continue
			}
			if !ids[sub.ID] && sub.SubscriptionType == types.SubscriptionTypeParent {
				parents = append(parents, sub.ID)
			}
			ids[sub.ID] = true
		}
		if index == 1 && len(parents) != 0 {
			filters = append(filters, &types.SubscriptionFilter{ParentSubscriptionIDs: parents, SubscriptionTypes: []types.SubscriptionType{types.SubscriptionTypeGroupedInvoicing}})
		}
	}
	if len(ids) > domain.MaxEligibilityScopes {
		return nil, ierr.Validation("billing projection supports at most 500 subscriptions")
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	slices.Sort(ordered)
	return ordered, nil
}

func projectedScheduleLine(ctx context.Context, params ServiceParams, item *domain.ScheduleItem, invoices map[string]*invoice.Invoice) (*invoice.Invoice, *invoice.InvoiceLineItem, error) {
	if item.InvoiceID == nil || item.InvoiceLineItemID == nil {
		return nil, nil, ierr.NewError("assigned billing source has no invoice attribution").Mark(ierr.ErrDatabase)
	}
	inv := invoices[*item.InvoiceID]
	if inv == nil {
		var err error
		inv, err = params.InvoiceRepo.Get(types.WithForceWriter(ctx), *item.InvoiceID)
		if err != nil {
			return nil, nil, err
		}
		invoices[inv.ID] = inv
	}
	if inv.TenantID != item.TenantID || inv.EnvironmentID != item.EnvironmentID || inv.CustomerID != item.CustomerID || !strings.EqualFold(inv.Currency, item.Currency) {
		return nil, nil, ierr.NewError("invalid projected billing invoice scope").Mark(ierr.ErrDatabase)
	}
	if inv.Status != types.StatusPublished || (inv.InvoiceStatus != types.InvoiceStatusDraft && inv.InvoiceStatus != types.InvoiceStatusFinalized) {
		return inv, nil, nil
	}
	for _, line := range inv.LineItems {
		if line != nil && line.ID == *item.InvoiceLineItemID && lo.FromPtr(line.BillingScheduleItemID) == item.ID {
			if !validRetainedScheduleLine(inv, line) || !line.Amount.Equal(item.Amount) {
				return nil, nil, ierr.NewError("invalid projected invoice source principal").Mark(ierr.ErrDatabase)
			}
			return inv, line, nil
		}
	}
	return nil, nil, ierr.NewError("projected billing invoice is missing its source line").Mark(ierr.ErrDatabase)
}

func schedulePreviewRequest(item *domain.ScheduleItem) dto.CreateInvoiceLineItemRequest {
	line := dto.CreateInvoiceLineItemRequest{
		BillingScheduleItemID: lo.ToPtr(item.ID), SubscriptionID: item.SubscriptionID,
		Amount: item.Amount, Quantity: decimal.NewFromInt(1), PriceType: lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
		PeriodStart: lo.ToPtr(item.ServicePeriodStart), PeriodEnd: lo.ToPtr(item.ServicePeriodEnd), DisplayName: lo.ToPtr(item.Description),
	}
	if item.OwnerType == types.BillingTermOwnerCommitment {
		line.CommitmentID = lo.ToPtr(item.OwnerID)
		line.EntityType = lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment))
		line.EntityID = lo.ToPtr(item.OwnerID)
	}
	return line
}
