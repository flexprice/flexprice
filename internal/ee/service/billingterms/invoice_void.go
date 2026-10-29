package billingterms

import (
	"context"
	"sort"
	"strings"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceSource identifies the original scheduled charge on a locked invoice.
type InvoiceSource struct {
	ItemID            string
	InvoiceLineItemID string
}

// InvoiceVoidRequest carries the complete source list from the locked invoice.
type InvoiceVoidRequest struct {
	InvoiceID string
	Sources   []InvoiceSource
}

func (s *service) WithInvoiceVoid(ctx context.Context, req InvoiceVoidRequest, void func(context.Context) error) error {
	if s == nil {
		return unavailable("billing terms service is unavailable")
	}
	if nilInterface(ctx) || !s.store.InTransaction(ctx) {
		return ierr.Validation("billing invoice voiding requires the caller transaction and invoice lock")
	}
	if types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" || !voidSourceID(req.InvoiceID) || void == nil {
		return ierr.Validation("scoped invoice and void callback are required")
	}
	expected := make(map[string]string, len(req.Sources))
	lines := make(map[string]bool, len(req.Sources))
	for _, source := range req.Sources {
		if !voidSourceID(source.ItemID) || !voidSourceID(source.InvoiceLineItemID) || expected[source.ItemID] != "" || lines[source.InvoiceLineItemID] {
			return ierr.Validation("invoice schedule sources must be nonempty and unique")
		}
		expected[source.ItemID] = source.InvoiceLineItemID
		lines[source.InvoiceLineItemID] = true
	}
	items, err := s.invoiceVoidSources(ctx, req.InvoiceID, expected)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return void(ctx)
	}
	owners := invoiceVoidOwners(items)
	for _, owner := range owners {
		if err := s.store.LockOwner(ctx, owner); err != nil {
			return err
		}
	}
	current, err := s.invoiceVoidSources(ctx, req.InvoiceID, expected)
	if err != nil {
		return err
	}
	if !sameVoidOwners(owners, invoiceVoidOwners(current)) {
		return integrity("invoice schedule owners changed while acquiring locks")
	}
	alreadyVoided, err := s.checkVoidBindings(ctx, req.InvoiceID, current)
	if err != nil {
		return err
	}
	if !alreadyVoided {
		if err := void(ctx); err != nil {
			return err
		}
		voided, err := s.checkVoidBindings(ctx, req.InvoiceID, current)
		if err != nil {
			return err
		}
		if !voided {
			return integrity("invoice void callback did not persist a voided invoice")
		}
	}
	for _, item := range current {
		if item.ScheduleStatus == types.BillingScheduleCanceled {
			continue
		}
		changed, err := s.items.Cancel(ctx, item.ID, types.BillingScheduleInvoiceVoided)
		if err != nil {
			return err
		}
		if !changed {
			return integrity("invoice schedule binding changed during voiding")
		}
	}
	return nil
}

func voidSourceID(id string) bool { return strings.TrimSpace(id) != "" && len(id) <= 50 }

func (s *service) invoiceVoidSources(ctx context.Context, invoiceID string, expected map[string]string) ([]*domain.ScheduleItem, error) {
	filter := &domain.ScheduleItemFilter{InvoiceIDs: []string{invoiceID}, IncludeArchived: true, Limit: MaxItems}
	items := make([]*domain.ScheduleItem, 0, len(expected))
	seen := make(map[string]bool, len(expected))
	for {
		// The extra row detects omitted reverse bindings without scanning unrelated history.
		filter.Limit = min(MaxItems, len(expected)-len(items)+1)
		page, err := s.items.List(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, item := range page {
			if item == nil || !voidSourceID(item.ID) || seen[item.ID] || expected[item.ID] == "" || item.InvoiceID == nil || *item.InvoiceID != invoiceID || item.InvoiceLineItemID == nil || *item.InvoiceLineItemID != expected[item.ID] || item.TenantID != types.GetTenantID(ctx) || item.EnvironmentID != types.GetEnvironmentID(ctx) {
				return nil, integrity("invoice schedule sources do not match the locked invoice")
			}
			if err := item.OwnerType.Validate(); err != nil || !voidSourceID(item.OwnerID) {
				return nil, integrity("invoice schedule owner is invalid")
			}
			if item.Status != types.StatusPublished && item.Status != types.StatusArchived {
				return nil, integrity("invoice schedule visibility is invalid")
			}
			switch item.ScheduleStatus {
			case types.BillingScheduleInvoiced:
				if item.CancellationReason != nil {
					return nil, integrity("assigned schedule item has cancellation history")
				}
			case types.BillingScheduleCanceled:
				if item.Status != types.StatusArchived || item.CancellationReason == nil || *item.CancellationReason != types.BillingScheduleInvoiceVoided {
					return nil, integrity("invoice schedule cancellation reason does not match voiding")
				}
			default:
				return nil, integrity("invoice schedule source is not assigned")
			}
			seen[item.ID] = true
			items = append(items, item)
		}
		if len(page) < filter.Limit {
			break
		}
		last := page[len(page)-1]
		filter.After = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if len(items) != len(expected) {
		return nil, integrity("invoice schedule source is missing")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func invoiceVoidOwners(items []*domain.ScheduleItem) []domain.OwnerRef {
	seen := make(map[domain.OwnerRef]bool)
	owners := make([]domain.OwnerRef, 0)
	for _, item := range items {
		owner := domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}
		if !seen[owner] {
			seen[owner] = true
			owners = append(owners, owner)
		}
	}
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].Type != owners[j].Type {
			return owners[i].Type < owners[j].Type
		}
		return owners[i].ID < owners[j].ID
	})
	return owners
}

func sameVoidOwners(a, b []domain.OwnerRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *service) checkVoidBindings(ctx context.Context, invoiceID string, items []*domain.ScheduleItem) (bool, error) {
	states := make(map[string]domain.BindingState, len(items))
	for from := 0; from < len(items); from += MaxItems {
		page, err := s.store.BindingStates(ctx, items[from:min(from+MaxItems, len(items))])
		if err != nil {
			return false, err
		}
		for _, state := range page {
			if state.ItemID == "" || states[state.ItemID].ItemID != "" {
				return false, integrity("invoice schedule binding audit is incomplete")
			}
			states[state.ItemID] = state
		}
	}
	if len(states) != len(items) {
		return false, integrity("invoice schedule binding audit is incomplete")
	}
	voided := 0
	for _, item := range items {
		state, ok := states[item.ID]
		if !ok || state.InvoiceID != invoiceID || state.InvoiceLineItemID != *item.InvoiceLineItemID || state.TenantID != types.GetTenantID(ctx) || state.EnvironmentID != types.GetEnvironmentID(ctx) || state.InvoiceArchived {
			return false, integrity("invoice schedule binding does not match its scoped source")
		}
		switch state.InvoiceStatus {
		case types.InvoiceStatusVoided:
			voided++
		case types.InvoiceStatusDraft, types.InvoiceStatusFinalized:
			if item.ScheduleStatus == types.BillingScheduleCanceled {
				return false, integrity("live invoice has a canceled schedule source")
			}
		default:
			return false, integrity("invoice schedule binding has an invalid invoice status")
		}
	}
	if voided != 0 && voided != len(items) {
		return false, integrity("invoice schedule audit returned inconsistent invoice states")
	}
	return voided == len(items), nil
}
