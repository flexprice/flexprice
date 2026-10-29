package service

import (
	"context"
	"testing"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
)

// BillingScheduleTemporalFixture shares the real PostgreSQL source fixture with external-package Temporal tests.
func BillingScheduleTemporalFixture(t *testing.T) (context.Context, ServiceParams, domain.ScheduleItemRepository, *domain.ScheduleItem) {
	t.Helper()
	f, _, item := newBillingDueFixture(t, newInvoiceVoidPostgresClient(t), false)
	params := f.service.ServiceParams
	params.BillingScheduleItems = f.items
	return f.ctx, params, f.items, item
}
