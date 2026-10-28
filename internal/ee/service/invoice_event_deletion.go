package service

import (
	"context"
	"sort"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// invoiceExternalCustomerIDs resolves the external ids the fence is keyed on. Both
// customer columns: a delegated_invoicing subscription puts the invoicing customer
// on the invoice, not the one that owns the usage.
func (s *invoiceService) invoiceExternalCustomerIDs(ctx context.Context, inv *invoice.Invoice) ([]string, error) {
	customerIDs := lo.Uniq(lo.Compact([]string{inv.CustomerID, lo.FromPtr(inv.SubscriptionCustomerID)}))
	if len(customerIDs) == 0 {
		return nil, nil
	}

	filter := types.NewNoLimitCustomerFilter()
	filter.CustomerIDs = customerIDs
	customers, err := s.CustomerRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return lo.Uniq(lo.Compact(lo.Map(customers, func(c *customer.Customer, _ int) string {
		return c.ExternalID
	}))), nil
}

// isInvoiceGatedOnEventDeletion reports the deletion holding this invoice, or nil
// when none is. Call it after the invoice row lock and after
// lockInvoiceCustomersForEventDeletion, or it deadlocks against the deletion.
func (s *invoiceService) isInvoiceGatedOnEventDeletion(ctx context.Context, inv *invoice.Invoice) (*events.DeletionRequest, error) {
	if s.EventDeletionRequestRepo == nil || inv == nil || inv.PeriodStart == nil || inv.PeriodEnd == nil {
		return nil, nil
	}

	externalIDs, err := s.invoiceExternalCustomerIDs(ctx, inv)
	if err != nil {
		return nil, err
	}
	if len(externalIDs) == 0 {
		return nil, nil
	}

	// Same operators as the deletion guard, so the gate can never be narrower.
	filter := inProgressDeletionFilter(externalIDs)
	filter.PeriodStartLT = inv.PeriodEnd
	filter.PeriodEndGT = inv.PeriodStart

	requests, err := s.EventDeletionRequestRepo.ListAll(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, nil
	}
	return requests[0], nil
}

// lockInvoiceCustomersForEventDeletion takes the same advisory key the deletion
// guard takes, so neither read can pass without seeing the other's write. Must run
// inside the finalization transaction.
func (s *invoiceService) lockInvoiceCustomersForEventDeletion(ctx context.Context, inv *invoice.Invoice) error {
	if s.EventDeletionRequestRepo == nil || inv == nil || inv.PeriodStart == nil || inv.PeriodEnd == nil {
		return nil
	}

	externalIDs, err := s.invoiceExternalCustomerIDs(ctx, inv)
	if err != nil {
		return err
	}
	// Sorted, matching the deletion side, so two holders cannot wait on each other.
	sort.Strings(externalIDs)
	for _, externalCustomerID := range externalIDs {
		if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: deletionLockKey(ctx, externalCustomerID)}); err != nil {
			return ierr.WithError(err).
				WithHint("Could not serialize finalization against a running event deletion").
				WithReportableDetails(map[string]interface{}{"external_customer_id": externalCustomerID}).
				Mark(ierr.ErrDatabase)
		}
	}
	return nil
}

func errInvoiceEventDeletionGated(invoiceID, requestID string) error {
	return ierr.NewError("an event deletion is in progress for this customer and period").
		WithHint("Wait for the running event deletion to finish before finalizing this invoice").
		WithReportableDetails(map[string]interface{}{
			"invoice_id": invoiceID,
			"request_id": requestID,
		}).
		Mark(ierr.ErrAlreadyExists)
}
