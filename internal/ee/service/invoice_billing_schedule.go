package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

func hasBillingScheduleLines(inv *invoice.Invoice) bool {
	for _, line := range inv.LineItems {
		if line != nil && line.BillingScheduleItemID != nil {
			return true
		}
	}
	return false
}

func billingScheduleCorrectionRequired(inv *invoice.Invoice) error {
	return ierr.NewError("invoice contains scheduled payments").
		WithHint("Use a reviewed billing-terms correction; scheduled payment lines cannot be edited, removed or copied to another invoice").
		WithReportableDetails(map[string]any{"invoice_id": inv.ID}).Mark(ierr.ErrInvalidOperation)
}

func validateBillingScheduleRecompute(ctx context.Context, inv *invoice.Invoice) error {
	if !hasBillingScheduleLines(inv) {
		return nil
	}
	if ctx == nil || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" || inv.TenantID != types.GetTenantID(ctx) || inv.EnvironmentID != types.GetEnvironmentID(ctx) {
		return billingScheduleCorrectionRequired(inv)
	}
	if inv.FxConversion != nil || inv.CustomCurrency != nil || inv.InvoiceStatus == types.InvoiceStatusSkipped {
		return billingScheduleCorrectionRequired(inv)
	}
	switch types.InvoiceBillingReason(inv.BillingReason) {
	case types.InvoiceBillingReasonSubscriptionPaused, types.InvoiceBillingReasonSubscriptionTrialStart:
		return billingScheduleCorrectionRequired(inv)
	}
	seen := make(map[string]bool)
	for _, line := range inv.LineItems {
		if line == nil || line.BillingScheduleItemID == nil {
			continue
		}
		id := *line.BillingScheduleItemID
		if id == "" || seen[id] || !validRetainedScheduleLine(inv, line) {
			return billingScheduleCorrectionRequired(inv)
		}
		seen[id] = true
	}
	return nil
}

func validateBillingScheduleDestination(inv *invoice.Invoice, customerID, currency string) error {
	if hasBillingScheduleLines(inv) && (inv.CustomerID != customerID || !strings.EqualFold(inv.Currency, currency)) {
		return billingScheduleCorrectionRequired(inv)
	}
	return nil
}

func sameInvoiceComputationContext(prepared, locked *invoice.Invoice) bool {
	return prepared.InvoiceType == locked.InvoiceType && prepared.CustomerID == locked.CustomerID &&
		strings.EqualFold(prepared.Currency, locked.Currency) && prepared.BillingReason == locked.BillingReason &&
		sameScheduleString(prepared.SubscriptionID, locked.SubscriptionID) &&
		sameInvoicePeriodDate(prepared.PeriodStart, locked.PeriodStart) && sameInvoicePeriodDate(prepared.PeriodEnd, locked.PeriodEnd)
}

func sameInvoicePeriodDate(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

// splitBillingScheduleLines validates before writes and keeps source lines out of legacy reconciliation.
func splitBillingScheduleLines(ctx context.Context, inv *invoice.Invoice, requests []dto.CreateInvoiceLineItemRequest) ([]*invoice.InvoiceLineItem, []*invoice.InvoiceLineItem, []dto.CreateInvoiceLineItemRequest, error) {
	if err := validateBillingScheduleRecompute(ctx, inv); err != nil {
		return nil, nil, nil, err
	}
	ordinary := make([]*invoice.InvoiceLineItem, 0, len(inv.LineItems))
	retained := make([]*invoice.InvoiceLineItem, 0)
	bySource := make(map[string]*invoice.InvoiceLineItem)
	for _, line := range inv.LineItems {
		if line == nil {
			return nil, nil, nil, billingScheduleCorrectionRequired(inv)
		}
		if line.BillingScheduleItemID == nil {
			ordinary = append(ordinary, line)
			continue
		}
		id := *line.BillingScheduleItemID
		bySource[id] = line
		retained = append(retained, invoice.NewInvoiceLineItemBuilder(line).WithoutAdjustments().Build())
	}
	computed := make([]dto.CreateInvoiceLineItemRequest, 0, len(requests))
	seen := make(map[string]bool)
	for _, req := range requests {
		if req.BillingScheduleItemID == nil {
			computed = append(computed, req)
			continue
		}
		id := *req.BillingScheduleItemID
		old := bySource[id]
		if old == nil || seen[id] || !sameBillingSchedulePrincipal(inv, old, req) {
			return nil, nil, nil, billingScheduleCorrectionRequired(inv)
		}
		seen[id] = true
	}
	return ordinary, retained, computed, nil
}

func validRetainedScheduleLine(inv *invoice.Invoice, line *invoice.InvoiceLineItem) bool {
	if line.ID == "" || line.InvoiceID != inv.ID || line.CustomerID != inv.CustomerID || line.TenantID != inv.TenantID || line.EnvironmentID != inv.EnvironmentID || line.Status != types.StatusPublished {
		return false
	}
	if !strings.EqualFold(line.Currency, inv.Currency) || !line.Amount.IsPositive() || !line.Quantity.Equal(decimal.NewFromInt(1)) || line.FxConversion != nil || line.CustomCurrency != nil {
		return false
	}
	if !line.Amount.Equal(types.RoundToCurrencyPrecision(line.Amount, inv.Currency)) {
		return false
	}
	return finiteScheduleDate(line.PeriodStart) && finiteScheduleDate(line.PeriodEnd) && line.PeriodStart.Before(*line.PeriodEnd)
}

func finiteScheduleDate(date *time.Time) bool {
	return date != nil && !date.IsZero() && date.Year() >= 1 && date.Year() <= 9999 && date.UTC().Year() >= 1 && date.UTC().Year() <= 9999
}

func sameBillingSchedulePrincipal(inv *invoice.Invoice, old *invoice.InvoiceLineItem, req dto.CreateInvoiceLineItemRequest) bool {
	subscriptionID := req.SubscriptionID
	if subscriptionID == nil {
		subscriptionID = inv.SubscriptionID
	}
	return old.Amount.Equal(req.Amount) && old.Quantity.Equal(req.Quantity) &&
		sameScheduleDate(old.PeriodStart, req.PeriodStart) && sameScheduleDate(old.PeriodEnd, req.PeriodEnd) &&
		sameScheduleString(old.SubscriptionID, subscriptionID) && sameScheduleString(old.CommitmentID, req.CommitmentID) && sameScheduleString(old.PriceID, req.PriceID)
}

func sameScheduleDate(a, b *time.Time) bool {
	return a != nil && b != nil && a.Equal(*b)
}

func sameScheduleString(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func billingScheduleLineRequest(line *invoice.InvoiceLineItem) dto.CreateInvoiceLineItemRequest {
	return dto.CreateInvoiceLineItemRequest{
		BillingScheduleItemID: line.BillingScheduleItemID, CommitmentID: line.CommitmentID,
		SubscriptionID: line.SubscriptionID, SubscriptionLineItemID: line.SubscriptionLineItemID,
		Amount: line.Amount, Quantity: line.Quantity, PeriodStart: line.PeriodStart, PeriodEnd: line.PeriodEnd,
		PriceID: line.PriceID, PriceType: line.PriceType, EntityID: line.EntityID, EntityType: line.EntityType,
		PlanDisplayName: line.PlanDisplayName, MeterID: line.MeterID, MeterDisplayName: line.MeterDisplayName,
		PriceUnit: line.PriceUnit, PriceUnitAmount: line.PriceUnitAmount, DisplayName: line.DisplayName,
		Metadata: line.Metadata, CommitmentInfo: line.CommitmentInfo, AdjustedEntitlementQuantity: line.AdjustedEntitlementQuantity,
	}
}

func preserveBillingScheduleRequest(ctx context.Context, inv *invoice.Invoice, req *dto.InvoiceComputeRequest) (*dto.InvoiceComputeRequest, error) {
	if !hasBillingScheduleLines(inv) {
		return req, nil
	}
	if req == nil {
		return nil, billingScheduleCorrectionRequired(inv)
	}
	requestedSubtotal := decimal.Zero
	for _, line := range req.LineItems {
		requestedSubtotal = requestedSubtotal.Add(types.RoundToCurrencyPrecision(line.Amount, inv.Currency))
	}
	if !req.Subtotal.Equal(decimal.Max(requestedSubtotal, decimal.Zero)) {
		return nil, billingScheduleCorrectionRequired(inv)
	}
	_, retained, computed, err := splitBillingScheduleLines(ctx, inv, req.LineItems)
	if err != nil {
		return nil, err
	}
	copy := *req
	copy.LineItems = computed
	for _, line := range retained {
		copy.LineItems = append(copy.LineItems, billingScheduleLineRequest(line))
	}
	subtotal := decimal.Zero
	for _, line := range copy.LineItems {
		subtotal = subtotal.Add(types.RoundToCurrencyPrecision(line.Amount, inv.Currency))
	}
	copy.Subtotal = decimal.Max(subtotal, decimal.Zero)
	copy.Total = copy.Subtotal
	copy.AmountDue = copy.Total
	return &copy, nil
}

func (s *invoiceService) reconcileLineItems(ctx context.Context, inv *invoice.Invoice, requests []dto.CreateInvoiceLineItemRequest) ([]*invoice.InvoiceLineItem, error) {
	ordinary, retained, computed, err := splitBillingScheduleLines(ctx, inv, requests)
	if err != nil {
		return nil, err
	}
	copy := *inv
	copy.LineItems = ordinary
	items, err := s.reconcileOrdinaryLineItems(ctx, &copy, computed)
	if err != nil {
		return nil, err
	}
	for _, line := range retained {
		if err := s.InvoiceLineItemRepo.Update(ctx, line); err != nil {
			return nil, err
		}
	}
	return append(items, retained...), nil
}
