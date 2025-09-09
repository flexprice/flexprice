package testutil

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryInvoiceStore implements invoice.Repository
type InMemoryInvoiceStore struct {
	*InMemoryStore[*invoice.Invoice]
}

// NewInMemoryInvoiceStore creates a new in-memory invoice store
func NewInMemoryInvoiceStore() *InMemoryInvoiceStore {
	return &InMemoryInvoiceStore{
		InMemoryStore: NewInMemoryStore[*invoice.Invoice](),
	}
}

// Helper to copy invoice
func copyInvoice(inv *invoice.Invoice) *invoice.Invoice {
	if inv == nil {
		return nil
	}

	// Deep copy line items
	lineItems := make([]*invoice.InvoiceLineItem, 0, len(inv.LineItems))
	for _, item := range inv.LineItems {
		if item == nil {
			continue
		}
		lineItems = append(lineItems, &invoice.InvoiceLineItem{
			ID:               item.ID,
			InvoiceID:        item.InvoiceID,
			CustomerID:       item.CustomerID,
			SubscriptionID:   item.SubscriptionID,
			EntityID:         item.EntityID,
			EntityType:       item.EntityType,
			PlanDisplayName:  item.PlanDisplayName,
			PriceID:          item.PriceID,
			PriceType:        item.PriceType,
			MeterID:          item.MeterID,
			MeterDisplayName: item.MeterDisplayName,
			PriceUnitID:      item.PriceUnitID,
			PriceUnit:        item.PriceUnit,
			PriceUnitAmount:  item.PriceUnitAmount,
			DisplayName:      item.DisplayName,
			Amount:           item.Amount,
			Quantity:         item.Quantity,
			Currency:         item.Currency,
			PeriodStart:      item.PeriodStart,
			PeriodEnd:        item.PeriodEnd,
			Metadata:         item.Metadata,
			EnvironmentID:    item.EnvironmentID,
			BaseModel:        item.BaseModel,
		})
	}

	return &invoice.Invoice{
		ID:               inv.ID,
		CustomerID:       inv.CustomerID,
		SubscriptionID:   inv.SubscriptionID,
		InvoiceType:      inv.InvoiceType,
		InvoiceStatus:    inv.InvoiceStatus,
		PaymentStatus:    inv.PaymentStatus,
		Currency:         inv.Currency,
		AmountDue:        inv.AmountDue,
		AmountPaid:       inv.AmountPaid,
		Subtotal:         inv.Subtotal,
		Total:            inv.Total,
		AmountRemaining:  inv.AmountRemaining,
		AdjustmentAmount: inv.AdjustmentAmount,
		RefundedAmount:   inv.RefundedAmount,
		InvoiceNumber:    inv.InvoiceNumber,
		IdempotencyKey:   inv.IdempotencyKey,
		BillingSequence:  inv.BillingSequence,
		Description:      inv.Description,
		DueDate:          inv.DueDate,
		PaidAt:           inv.PaidAt,
		VoidedAt:         inv.VoidedAt,
		FinalizedAt:      inv.FinalizedAt,
		BillingPeriod:    inv.BillingPeriod,
		PeriodStart:      inv.PeriodStart,
		PeriodEnd:        inv.PeriodEnd,
		InvoicePDFURL:    inv.InvoicePDFURL,
		BillingReason:    inv.BillingReason,
		LineItems:        lineItems,
		Metadata:         inv.Metadata,
		Version:          inv.Version,
		EnvironmentID:    inv.EnvironmentID,
		BaseModel:        inv.BaseModel,
	}
}

func (s *InMemoryInvoiceStore) Create(ctx context.Context, inv *invoice.Invoice) error {
	if inv == nil {
		return ierr.NewError("invoice cannot be nil").
			WithHint("Invoice cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if inv.EnvironmentID == "" {
		inv.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, inv.ID, copyInvoice(inv))
}

func (s *InMemoryInvoiceStore) CreateWithLineItems(ctx context.Context, inv *invoice.Invoice) error {
	return s.Create(ctx, inv) // In memory store doesn't need special transaction handling
}

func (s *InMemoryInvoiceStore) AddLineItems(ctx context.Context, invoiceID string, items []*invoice.InvoiceLineItem) error {
	inv, err := s.Get(ctx, invoiceID)
	if err != nil {
		return err
	}
	// Copy and add each line item
	for _, item := range items {
		itemCopy := copyInvoice(&invoice.Invoice{LineItems: []*invoice.InvoiceLineItem{item}}).LineItems[0]
		itemCopy.InvoiceID = invoiceID
		inv.LineItems = append(inv.LineItems, itemCopy)
	}
	return s.Update(ctx, inv)
}

func (s *InMemoryInvoiceStore) RemoveLineItems(ctx context.Context, invoiceID string, itemIDs []string) error {
	inv, err := s.Get(ctx, invoiceID)
	if err != nil {
		return err
	}

	inv.LineItems = lo.Filter(inv.LineItems, func(item *invoice.InvoiceLineItem, _ int) bool {
		return !lo.Contains(itemIDs, item.ID)
	})

	return s.Update(ctx, inv)
}

func (s *InMemoryInvoiceStore) Get(ctx context.Context, id string) (*invoice.Invoice, error) {
	inv, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("invoice get failed").Mark(ierr.ErrDatabase)
	}
	return copyInvoice(inv), nil
}

func (s *InMemoryInvoiceStore) Update(ctx context.Context, inv *invoice.Invoice) error {
	if inv == nil {
		return ierr.NewError("invoice cannot be nil").WithHint("invoice cannot be nil").Mark(ierr.ErrValidation)
	}
	return s.InMemoryStore.Update(ctx, inv.ID, copyInvoice(inv))
}

func (s *InMemoryInvoiceStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemoryInvoiceStore) List(ctx context.Context, filter *types.InvoiceFilter) ([]*invoice.Invoice, error) {
	return s.InMemoryStore.List(ctx, filter, invoiceFilterFn, invoiceSortFn)
}

func (s *InMemoryInvoiceStore) Count(ctx context.Context, filter *types.InvoiceFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, invoiceFilterFn)
}

func (s *InMemoryInvoiceStore) GetByIdempotencyKey(ctx context.Context, key string) (*invoice.Invoice, error) {
	filter := types.NewNoLimitInvoiceFilter()
	invoices, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	for _, inv := range invoices {
		if inv.IdempotencyKey != nil && *inv.IdempotencyKey == key {
			return copyInvoice(inv), nil
		}
	}

	return nil, ierr.NewError("invoice not found").WithHint("invoice not found").Mark(ierr.ErrNotFound)
}

func (s *InMemoryInvoiceStore) ExistsForPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subscriptionID
	invoices, err := s.List(ctx, filter)
	if err != nil {
		return false, err
	}

	for _, inv := range invoices {
		if inv.PeriodStart != nil && inv.PeriodEnd != nil {
			if (periodStart.Equal(*inv.PeriodStart) || periodStart.After(*inv.PeriodStart)) &&
				(periodEnd.Equal(*inv.PeriodEnd) || periodEnd.Before(*inv.PeriodEnd)) {
				return true, nil
			}
		}
	}

	return false, nil
}

func (s *InMemoryInvoiceStore) GetNextInvoiceNumber(ctx context.Context, invoiceConfig *types.InvoiceConfig) (string, error) {
	filter := types.NewNoLimitInvoiceFilter()
	invoices, err := s.List(ctx, filter)
	if err != nil {
		return "", err
	}

	maxNum := 0
	for _, inv := range invoices {
		if inv.InvoiceNumber != nil {
			var num int
			_, err := fmt.Sscanf(*inv.InvoiceNumber, "INV-%d", &num)
			if err == nil && num > maxNum {
				maxNum = num
			}
		}
	}

	return fmt.Sprintf("INV-%08d", maxNum+1), nil
}

func (s *InMemoryInvoiceStore) GetNextBillingSequence(ctx context.Context, subscriptionID string) (int, error) {
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subscriptionID
	invoices, err := s.List(ctx, filter)
	if err != nil {
		return 0, err
	}

	maxSeq := 0
	for _, inv := range invoices {
		if inv.BillingSequence != nil && *inv.BillingSequence > maxSeq {
			maxSeq = *inv.BillingSequence
		}
	}

	return maxSeq + 1, nil
}

// invoiceFilterFn implements filtering logic for invoices
func invoiceFilterFn(ctx context.Context, inv *invoice.Invoice, filter interface{}) bool {
	if inv == nil {
		return false
	}

	f, ok := filter.(*types.InvoiceFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if inv.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, inv.EnvironmentID) {
		return false
	}

	// Filter by customer ID
	if f.CustomerID != "" && inv.CustomerID != f.CustomerID {
		return false
	}

	// Filter by subscription ID
	if f.SubscriptionID != "" {
		if inv.SubscriptionID == nil || *inv.SubscriptionID != f.SubscriptionID {
			return false
		}
	}

	// Filter by invoice type
	if f.InvoiceType != "" && inv.InvoiceType != f.InvoiceType {
		return false
	}

	// Filter by invoice status
	if len(f.InvoiceStatus) > 0 && !lo.Contains(f.InvoiceStatus, inv.InvoiceStatus) {
		return false
	}

	// Filter by payment status
	if len(f.PaymentStatus) > 0 && !lo.Contains(f.PaymentStatus, inv.PaymentStatus) {
		return false
	}

	// Filter by due amount
	if f.AmountDueGt != nil && inv.AmountDue.LessThanOrEqual(*f.AmountDueGt) {
		return false
	}

	// Filter by amount remaining
	if f.AmountRemainingGt != nil && inv.AmountRemaining.LessThanOrEqual(*f.AmountRemainingGt) {
		return false
	}

	// Filter by status
	if f.Status != nil && inv.Status != *f.Status {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil && (f.TimeRangeFilter.StartTime != nil || f.TimeRangeFilter.EndTime != nil) {
		if f.TimeRangeFilter.StartTime != nil {
			if inv.PeriodStart == nil || inv.PeriodStart.After(*f.TimeRangeFilter.StartTime) {
				return false
			}
		}
		if f.TimeRangeFilter.EndTime != nil {
			if inv.PeriodEnd == nil || inv.PeriodEnd.Before(*f.TimeRangeFilter.EndTime) {
				return false
			}
		}
	}

	return true
}

// invoiceSortFn implements sorting logic for invoices
func invoiceSortFn(i, j *invoice.Invoice) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

// Clear removes all invoices from the store
func (s *InMemoryInvoiceStore) Clear() {
	s.InMemoryStore.Clear()
}
