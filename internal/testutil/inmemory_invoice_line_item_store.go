package testutil

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InMemoryInvoiceLineItemStore implements invoice.LineItemRepository for testing.
type InMemoryInvoiceLineItemStore struct {
	mu   sync.RWMutex
	data map[string]*invoice.InvoiceLineItem
}

func NewInMemoryInvoiceLineItemStore() *InMemoryInvoiceLineItemStore {
	return &InMemoryInvoiceLineItemStore{data: make(map[string]*invoice.InvoiceLineItem)}
}

func (s *InMemoryInvoiceLineItemStore) Create(ctx context.Context, item *invoice.InvoiceLineItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[item.ID]; exists {
		return ierr.NewError("invoice line item already exists").Mark(ierr.ErrAlreadyExists)
	}
	cp := *item
	s.data[item.ID] = &cp
	return nil
}

func (s *InMemoryInvoiceLineItemStore) CreateBulk(ctx context.Context, items []*invoice.InvoiceLineItem) error {
	for _, item := range items {
		if err := s.Create(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemoryInvoiceLineItemStore) Get(ctx context.Context, id string) (*invoice.InvoiceLineItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.data[id]
	if !ok {
		return nil, ierr.NewError("invoice line item not found").Mark(ierr.ErrNotFound)
	}
	cp := *item
	return &cp, nil
}

func (s *InMemoryInvoiceLineItemStore) Update(ctx context.Context, item *invoice.InvoiceLineItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[item.ID]; !exists {
		return ierr.NewError("invoice line item not found").Mark(ierr.ErrNotFound)
	}
	cp := *item
	s.data[item.ID] = &cp
	return nil
}

func (s *InMemoryInvoiceLineItemStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, exists := s.data[id]
	if !exists {
		return ierr.NewError("invoice line item not found").Mark(ierr.ErrNotFound)
	}
	cp := *item
	cp.Status = types.StatusDeleted
	s.data[id] = &cp
	return nil
}

func (s *InMemoryInvoiceLineItemStore) ListByInvoiceID(ctx context.Context, invoiceID string) ([]*invoice.InvoiceLineItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*invoice.InvoiceLineItem
	for _, item := range s.data {
		if item.InvoiceID == invoiceID && item.Status == types.StatusPublished {
			cp := *item
			result = append(result, &cp)
		}
	}
	// Stable order: map iteration is random in Go. Match a predictable ordering so service code
	// that iterates line items (e.g. sequential credit application) behaves deterministically in tests.
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (s *InMemoryInvoiceLineItemStore) List(ctx context.Context, filter *types.InvoiceLineItemFilter) ([]*invoice.InvoiceLineItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*invoice.InvoiceLineItem
	for _, item := range s.data {
		if filter != nil && len(filter.InvoiceIDs) > 0 {
			found := false
			for _, id := range filter.InvoiceIDs {
				if item.InvoiceID == id {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if filter != nil && len(filter.SubscriptionIDs) > 0 {
			// nil SubscriptionID never matches a subscription ID filter
			if item.SubscriptionID == nil {
				continue
			}
			found := false
			for _, id := range filter.SubscriptionIDs {
				if *item.SubscriptionID == id {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *InMemoryInvoiceLineItemStore) GetRevenueByCustomer(
	_ context.Context,
	periodStart, periodEnd time.Time,
	customerIDs []string,
) ([]invoice.RevenueByCustomerRow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	custFilter := make(map[string]bool, len(customerIDs))
	for _, id := range customerIDs {
		custFilter[id] = true
	}

	// Aggregate: key = customerID + "|" + priceType
	agg := make(map[string]decimal.Decimal)
	for _, item := range s.data {
		if item.Status != types.StatusPublished {
			continue
		}
		if item.PeriodStart != nil && item.PeriodStart.Before(periodStart) {
			continue
		}
		if item.PeriodEnd != nil && !item.PeriodEnd.Before(periodEnd) {
			continue
		}
		if len(custFilter) > 0 && !custFilter[item.CustomerID] {
			continue
		}
		pt := "FIXED"
		if item.PriceType != nil {
			pt = *item.PriceType
		}
		key := item.CustomerID + "|" + pt
		agg[key] = agg[key].Add(item.Amount)
	}

	var results []invoice.RevenueByCustomerRow
	for key, amount := range agg {
		parts := splitKeyOnce(key, "|")
		results = append(results, invoice.RevenueByCustomerRow{
			CustomerID: parts[0],
			PriceType:  parts[1],
			Amount:     amount,
		})
	}
	return results, nil
}

func (s *InMemoryInvoiceLineItemStore) GetVoiceMinutesByCustomer(
	_ context.Context,
	periodStart, periodEnd time.Time,
	meterID string,
	customerIDs []string,
) ([]invoice.VoiceMinutesRow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	custFilter := make(map[string]bool, len(customerIDs))
	for _, id := range customerIDs {
		custFilter[id] = true
	}

	agg := make(map[string]decimal.Decimal)
	for _, item := range s.data {
		if item.Status != types.StatusPublished {
			continue
		}
		if item.MeterID == nil || *item.MeterID != meterID {
			continue
		}
		if item.PeriodStart != nil && item.PeriodStart.Before(periodStart) {
			continue
		}
		if item.PeriodEnd != nil && !item.PeriodEnd.Before(periodEnd) {
			continue
		}
		if len(custFilter) > 0 && !custFilter[item.CustomerID] {
			continue
		}
		agg[item.CustomerID] = agg[item.CustomerID].Add(item.Quantity)
	}

	var results []invoice.VoiceMinutesRow
	for custID, usageMs := range agg {
		results = append(results, invoice.VoiceMinutesRow{
			CustomerID: custID,
			UsageMs:    usageMs,
		})
	}
	return results, nil
}

func truncateLineItemPeriodUTC(t time.Time, dateTruncPart string) time.Time {
	t = t.UTC()
	switch dateTruncPart {
	case "month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	case "day":
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	default:
		return t
	}
}

// GetRevenueTimeSeries implements invoice.LineItemRepository for tests (UTC buckets).
func (s *InMemoryInvoiceLineItemStore) GetRevenueTimeSeries(
	_ context.Context,
	periodStart, periodEnd time.Time,
	dateTruncPart string,
	customerIDs []string,
) ([]invoice.RevenueTimeSeriesRow, error) {
	if dateTruncPart != "day" && dateTruncPart != "month" {
		return nil, ierr.NewError("invalid date_trunc granularity").Mark(ierr.ErrValidation)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	custFilter := make(map[string]bool, len(customerIDs))
	for _, id := range customerIDs {
		custFilter[id] = true
	}

	type pair struct {
		w  time.Time
		pt string
	}
	agg := make(map[pair]decimal.Decimal)

	for _, item := range s.data {
		if item.Status != types.StatusPublished {
			continue
		}
		if item.PeriodStart != nil && item.PeriodStart.Before(periodStart) {
			continue
		}
		if item.PeriodEnd != nil && !item.PeriodEnd.Before(periodEnd) {
			continue
		}
		if len(custFilter) > 0 && !custFilter[item.CustomerID] {
			continue
		}
		if item.PeriodStart == nil {
			continue
		}
		pt := "FIXED"
		if item.PriceType != nil {
			pt = *item.PriceType
		}
		ws := truncateLineItemPeriodUTC(*item.PeriodStart, dateTruncPart)
		p := pair{w: ws, pt: pt}
		agg[p] = agg[p].Add(item.Amount)
	}

	results := make([]invoice.RevenueTimeSeriesRow, 0, len(agg))
	for k, amount := range agg {
		results = append(results, invoice.RevenueTimeSeriesRow{
			WindowStart: k.w,
			PriceType:   k.pt,
			Amount:      amount,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if !results[i].WindowStart.Equal(results[j].WindowStart) {
			return results[i].WindowStart.Before(results[j].WindowStart)
		}
		return results[i].PriceType < results[j].PriceType
	})
	return results, nil
}

// GetVoiceMinutesTimeSeries implements invoice.LineItemRepository for tests (UTC buckets).
func (s *InMemoryInvoiceLineItemStore) GetVoiceMinutesTimeSeries(
	_ context.Context,
	periodStart, periodEnd time.Time,
	meterID, dateTruncPart string,
	customerIDs []string,
) ([]invoice.VoiceMinutesTimeSeriesRow, error) {
	if dateTruncPart != "day" && dateTruncPart != "month" {
		return nil, ierr.NewError("invalid date_trunc granularity").Mark(ierr.ErrValidation)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	custFilter := make(map[string]bool, len(customerIDs))
	for _, id := range customerIDs {
		custFilter[id] = true
	}

	agg := make(map[time.Time]decimal.Decimal)
	for _, item := range s.data {
		if item.Status != types.StatusPublished {
			continue
		}
		if item.MeterID == nil || *item.MeterID != meterID {
			continue
		}
		if item.PeriodStart != nil && item.PeriodStart.Before(periodStart) {
			continue
		}
		if item.PeriodEnd != nil && !item.PeriodEnd.Before(periodEnd) {
			continue
		}
		if len(custFilter) > 0 && !custFilter[item.CustomerID] {
			continue
		}
		if item.PeriodStart == nil {
			continue
		}
		ws := truncateLineItemPeriodUTC(*item.PeriodStart, dateTruncPart)
		agg[ws] = agg[ws].Add(item.Quantity)
	}

	results := make([]invoice.VoiceMinutesTimeSeriesRow, 0, len(agg))
	for ws, usage := range agg {
		results = append(results, invoice.VoiceMinutesTimeSeriesRow{
			WindowStart: ws,
			UsageMs:     usage,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].WindowStart.Before(results[j].WindowStart)
	})
	return results, nil
}

// splitKeyOnce splits s on the first occurrence of sep into exactly 2 parts.
func splitKeyOnce(s, sep string) [2]string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep[0] {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, ""}
}

func (s *InMemoryInvoiceLineItemStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]*invoice.InvoiceLineItem)
}
