package billingterms

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CollectionRequest identifies the destination and fixed collection boundary, independently of its service period.
type CollectionRequest struct {
	InvoiceID         string
	CustomerID        string
	Currency          string
	Delivery          types.BillingTermDelivery
	Owners            []domain.OwnerRef
	EligibilityScopes []domain.EligibilityScope
	Cutoff            time.Time
	AsOf              time.Time
	SelectedItemIDs   []string
}

func (s *service) WithCollection(ctx context.Context, req CollectionRequest, persist func(context.Context, []*domain.ScheduleItem) ([]InvoiceSource, error)) error {
	if s == nil {
		return unavailable("billing terms service is unavailable")
	}
	if nilInterface(ctx) || !s.store.InTransaction(ctx) || persist == nil {
		return ierr.Validation("billing collection requires the caller transaction, invoice lock and persistence callback")
	}
	if !voidSourceID(req.InvoiceID) || !voidSourceID(req.CustomerID) || req.Currency == "" || !validDate(req.Cutoff) || !validDate(req.AsOf) {
		return ierr.Validation("billing collection requires a scoped destination and finite collection dates")
	}
	if err := req.Delivery.Validate(); err != nil {
		return err
	}
	owners, err := collectionOwners(ctx, req.Owners)
	if err != nil {
		return err
	}
	selected := make(map[string]bool, len(req.SelectedItemIDs))
	if len(req.SelectedItemIDs) > MaxItems {
		return ierr.Validation("too many selected billing schedule items")
	}
	for _, id := range req.SelectedItemIDs {
		if !voidSourceID(id) || selected[id] {
			return ierr.Validation("selected billing schedule IDs must be valid and unique")
		}
		selected[id] = true
	}
	discovered := make(map[domain.OwnerRef]*domain.OwnerSnapshot, len(owners))
	scopes, err := canonicalEligibilityScopes(req.EligibilityScopes)
	if err != nil {
		return err
	}
	scopeSet := make(map[domain.EligibilityScope]bool, len(scopes))
	for _, scope := range scopes {
		scopeSet[scope] = true
	}
	for _, owner := range owners {
		snapshot, err := s.snapshot(ctx, owner)
		if err != nil {
			return err
		}
		discovered[owner] = snapshot
		for _, scope := range snapshot.EligibilityScopes {
			if !scopeSet[scope] {
				scopeSet[scope] = true
				scopes = append(scopes, scope)
			}
		}
	}
	scopes, err = canonicalEligibilityScopes(scopes)
	if err != nil {
		return err
	}
	if err := s.store.LockEligibility(ctx, scopes); err != nil {
		return err
	}
	for _, owner := range owners {
		if err := s.store.LockOwner(ctx, owner); err != nil {
			return err
		}
	}
	items := make([]*domain.ScheduleItem, 0)
	for _, owner := range owners {
		fresh, err := s.snapshot(ctx, owner)
		if err != nil {
			return err
		}
		if !slices.Equal(discovered[owner].EligibilityScopes, fresh.EligibilityScopes) {
			return ierr.NewError("collection eligibility changed; retry the invoice").Mark(ierr.ErrVersionConflict)
		}
		current, err := s.load(ctx, fresh, req.AsOf)
		if err != nil {
			return err
		}
		if !fresh.CanCollect || current.summary.RequiresReplan {
			continue
		}
		if fresh.CustomerID != req.CustomerID || !strings.EqualFold(fresh.Currency, req.Currency) || fresh.Delivery != req.Delivery {
			return ierr.NewError("billing collection destination changed; retry the invoice").Mark(ierr.ErrVersionConflict)
		}
		for _, item := range current.items {
			if item.Status != types.StatusPublished || item.ScheduleStatus != types.BillingScheduleScheduled || item.BillOn == nil ||
				item.BillOn.After(req.Cutoff) || item.BillOn.After(req.AsOf) || (len(selected) != 0 && !selected[item.ID]) {
				continue
			}
			items = append(items, domain.NewScheduleItemBuilder(item).Build())
			if len(items) > MaxItems {
				return ierr.Validation("an invoice may collect at most 500 billing schedule items")
			}
		}
	}
	if len(selected) != 0 && len(items) != len(selected) {
		return ierr.NewError("selected billing schedule items are no longer eligible").Mark(ierr.ErrVersionConflict)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].BillOn.Equal(*items[j].BillOn) {
			return items[i].ID < items[j].ID
		}
		return items[i].BillOn.Before(*items[j].BillOn)
	})
	projected := make([]*domain.ScheduleItem, len(items))
	for i, item := range items {
		projected[i] = domain.NewScheduleItemBuilder(item).Build()
	}
	bindings, err := persist(ctx, projected)
	if err != nil {
		return err
	}
	return s.bindCollection(ctx, req.InvoiceID, items, bindings)
}

func collectionOwners(ctx context.Context, supplied []domain.OwnerRef) ([]domain.OwnerRef, error) {
	if len(supplied) > domain.MaxEligibilityScopes {
		return nil, ierr.Validation("too many billing owners for one invoice")
	}
	owners := append([]domain.OwnerRef(nil), supplied...)
	for _, owner := range owners {
		if err := requestScope(ctx, owner); err != nil {
			return nil, err
		}
	}
	if types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return nil, ierr.Validation("billing collection requires tenant and environment scope")
	}
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].Type != owners[j].Type {
			return owners[i].Type < owners[j].Type
		}
		return owners[i].ID < owners[j].ID
	})
	return slices.Compact(owners), nil
}

func (s *service) bindCollection(ctx context.Context, invoiceID string, items []*domain.ScheduleItem, bindings []InvoiceSource) error {
	if len(bindings) != len(items) {
		return integrity("invoice persistence did not return every scheduled source")
	}
	byItem := make(map[string]string, len(bindings))
	lines := make(map[string]bool, len(bindings))
	for _, binding := range bindings {
		if !voidSourceID(binding.ItemID) || !voidSourceID(binding.InvoiceLineItemID) || byItem[binding.ItemID] != "" || lines[binding.InvoiceLineItemID] {
			return integrity("invoice schedule bindings must have unique item and line IDs")
		}
		byItem[binding.ItemID], lines[binding.InvoiceLineItemID] = binding.InvoiceLineItemID, true
	}
	claimed := make([]*domain.ScheduleItem, 0, len(items))
	for _, item := range items {
		lineID := byItem[item.ID]
		if lineID == "" {
			return integrity("invoice persistence returned an unrelated schedule source")
		}
		changed, err := s.items.Claim(ctx, item.ID, invoiceID, lineID)
		if err != nil {
			return err
		}
		if !changed {
			return ierr.NewError("billing schedule item was already claimed or replaced").Mark(ierr.ErrVersionConflict)
		}
		claimed = append(claimed, domain.NewScheduleItemBuilder(item).WithInvoiceID(&invoiceID).
			WithInvoiceLineItemID(&lineID).WithScheduleStatus(types.BillingScheduleInvoiced).Build())
	}
	for from := 0; from < len(claimed); from += MaxItems {
		page := claimed[from:min(from+MaxItems, len(claimed))]
		states, err := s.store.BindingStates(ctx, page)
		if err != nil {
			return err
		}
		if len(states) != len(page) {
			return integrity("new billing schedule bindings are incomplete")
		}
		seen := make(map[string]bool, len(states))
		for _, state := range states {
			if seen[state.ItemID] || byItem[state.ItemID] == "" || state.InvoiceID != invoiceID || state.InvoiceLineItemID != byItem[state.ItemID] ||
				state.TenantID != types.GetTenantID(ctx) || state.EnvironmentID != types.GetEnvironmentID(ctx) || state.InvoiceStatus != types.InvoiceStatusDraft || state.InvoiceArchived {
				return integrity("new billing schedule source does not match its draft invoice")
			}
			seen[state.ItemID] = true
		}
	}
	return nil
}
