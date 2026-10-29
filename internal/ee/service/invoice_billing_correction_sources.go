package service

import (
	"context"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *invoiceService) invoiceCorrectionSources(ctx context.Context, inv *invoice.Invoice) ([]*domain.ScheduleItem, error) {
	expected := make(map[string]string)
	for _, line := range inv.LineItems {
		if line == nil || line.BillingScheduleItemID == nil {
			continue
		}
		id := *line.BillingScheduleItemID
		if !billingDueID(id) || !billingDueID(line.ID) || expected[id] != "" {
			return nil, invalidCorrectionSources()
		}
		expected[id] = line.ID
	}
	filter := &domain.ScheduleItemFilter{InvoiceIDs: []string{inv.ID}, IncludeArchived: true}
	items := make([]*domain.ScheduleItem, 0, len(expected))
	seen := make(map[string]bool, len(expected))
	for {
		// Invoice lines bound the scan; one extra row detects an unlisted reverse binding.
		filter.Limit = min(core.MaxItems, len(expected)-len(items)+1)
		page, err := s.BillingScheduleItems.List(ctx, filter)
		if err != nil {
			return nil, err
		}
		if len(page) > filter.Limit {
			return nil, invalidCorrectionSources()
		}
		for _, item := range page {
			if !billingDueItemScope(ctx, item) || seen[item.ID] || expected[item.ID] == "" ||
				lo.FromPtr(item.InvoiceID) != inv.ID || lo.FromPtr(item.InvoiceLineItemID) != expected[item.ID] || item.CustomerID != inv.CustomerID {
				return nil, invalidCorrectionSources()
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
		return nil, invalidCorrectionSources()
	}
	for from := 0; from < len(items); from += core.MaxItems {
		batch := items[from:min(from+core.MaxItems, len(items))]
		bindings, err := s.BillingTermsStore.BindingStates(ctx, batch)
		if err != nil {
			return nil, err
		}
		states := make(map[string]domain.BindingState, len(batch))
		for _, state := range bindings {
			if state.ItemID == "" || states[state.ItemID].ItemID != "" {
				return nil, invalidCorrectionSources()
			}
			states[state.ItemID] = state
		}
		if len(states) != len(batch) {
			return nil, invalidCorrectionSources()
		}
		for _, item := range batch {
			state, ok := states[item.ID]
			if !ok || state.InvoiceID != inv.ID || state.InvoiceLineItemID != expected[item.ID] ||
				state.TenantID != types.GetTenantID(ctx) || state.EnvironmentID != types.GetEnvironmentID(ctx) ||
				state.InvoiceStatus != inv.InvoiceStatus || state.InvoiceArchived {
				return nil, invalidCorrectionSources()
			}
		}
	}
	return items, nil
}

func invalidCorrectionSources() error {
	return ierr.NewError("invoice source history does not match its scoped invoice lines").Mark(ierr.ErrInvalidOperation)
}
