package ent

import (
	"context"
	"slices"

	"github.com/flexprice/flexprice/ent"
	itemdb "github.com/flexprice/flexprice/ent/billingscheduleitem"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

func validateInvoiceBillingSources(ctx context.Context, client *ent.Client, lines []*invoice.InvoiceLineItem) error {
	var ids []string
	for _, line := range lines {
		if line != nil && line.BillingScheduleItemID != nil {
			ids = append(ids, *line.BillingScheduleItemID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return err
	}
	for _, line := range lines {
		if line == nil || line.BillingScheduleItemID == nil {
			continue
		}
		if err := billingIdentifier("invoice billing schedule source", *line.BillingScheduleItemID); err != nil {
			return err
		}
		if line.TenantID != "" && line.TenantID != tenantID || line.EnvironmentID != "" && line.EnvironmentID != environmentID {
			return ierr.Validation("invoice billing source scope does not match the request")
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for start := 0; start < len(ids); start += invoiceLineItemBatchSize {
		end := min(start+invoiceLineItemBatchSize, len(ids))
		count, err := client.BillingScheduleItem.Query().Where(
			itemdb.TenantID(tenantID), itemdb.EnvironmentID(environmentID), itemdb.IDIn(ids[start:end]...),
		).Count(ctx)
		if err != nil {
			return billingPersistenceError(err, "resolve invoice billing schedule sources")
		}
		if count != end-start {
			return ierr.NewError("invoice billing schedule source not found").Mark(ierr.ErrNotFound)
		}
	}
	return nil
}
