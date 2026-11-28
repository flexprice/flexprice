package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *InvoiceServiceSuite) TestListFinalizationCandidates() {
	ctx := s.GetContext()
	periodEnd := time.Now().Add(-time.Hour)
	before, after := periodEnd.Add(-time.Minute), periodEnd.Add(time.Minute)

	cycle := string(types.InvoiceBillingReasonSubscriptionCycle)
	tests := []struct {
		id        string
		status    types.Status
		invStatus types.InvoiceStatus
		reason    string
		computed  *time.Time
		periodEnd *time.Time
		want      bool
	}{
		{"inv_01", types.StatusPublished, types.InvoiceStatusDraft, "MANUAL", &after, nil, true},
		{"inv_02", types.StatusPublished, types.InvoiceStatusDraft, "MANUAL", nil, nil, false},
		{"inv_03", types.StatusPublished, types.InvoiceStatusDraft, string(types.InvoiceBillingReasonSubscriptionCreate), &after, nil, false},
		{"inv_04", types.StatusPublished, types.InvoiceStatusDraft, cycle, &before, &periodEnd, false},
		{"inv_05", types.StatusPublished, types.InvoiceStatusDraft, cycle, &after, &periodEnd, true},
		{"inv_06", types.StatusPublished, types.InvoiceStatusDraft, cycle, &after, nil, true},
		{"inv_07", types.StatusPublished, types.InvoiceStatusFinalized, "MANUAL", &after, nil, false},
		{"inv_08", types.StatusArchived, types.InvoiceStatusDraft, "MANUAL", &after, nil, false},
		{"inv_09", types.StatusPublished, types.InvoiceStatusDraft, "", &after, nil, true},
	}

	var want []string
	byID := map[string]*invoice.Invoice{}
	for _, tt := range tests {
		inv := &invoice.Invoice{
			ID:             tt.id,
			CustomerID:     s.testData.customer.ID,
			InvoiceType:    types.InvoiceTypeOneOff,
			InvoiceStatus:  tt.invStatus,
			Currency:       "usd",
			BillingReason:  tt.reason,
			LastComputedAt: tt.computed,
			PeriodEnd:      tt.periodEnd,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
		inv.Status = tt.status
		s.Require().NoError(s.invoiceRepo.Create(ctx, inv))
		byID[tt.id] = inv
		if tt.want {
			want = append(want, tt.id)
		}
	}

	var got []string
	afterID := ""
	for {
		page, err := s.service.ListFinalizationCandidates(ctx, afterID, 2)
		s.Require().NoError(err)
		got = append(got, lo.Map(page, func(i *invoice.Invoice, _ int) string { return i.ID })...)
		if len(page) < 2 {
			break
		}
		afterID = page[len(page)-1].ID
	}
	s.Equal(want, got)

	// The pre-filter must never drop a draft that is due.
	for id, inv := range byID {
		if lo.Contains(got, id) {
			continue
		}
		due, err := s.service.IsInvoiceFinalizationDue(ctx, inv)
		s.Require().NoError(err)
		s.False(due, id)
	}
}
