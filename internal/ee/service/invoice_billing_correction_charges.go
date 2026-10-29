package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type correctionChargesKey struct{}

func withCorrectionCharges(ctx context.Context) context.Context {
	return context.WithValue(ctx, correctionChargesKey{}, true)
}

func retainCorrectionZeroUsage(ctx context.Context) bool {
	retain, _ := ctx.Value(correctionChargesKey{}).(bool)
	return retain
}

func (s *invoiceService) correctionSourceInvoice(ctx context.Context, inv *invoice.Invoice) (*invoice.Invoice, error) {
	if !hasBillingScheduleLines(inv) {
		return nil, nil
	}
	original, err := s.InvoiceRepo.GetByRecalculatedInvoiceID(ctx, inv.ID)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if original == nil {
		return nil, nil
	}
	// The reverse-link lookup returns only the header; source validation needs its lines.
	original, err = s.InvoiceRepo.Get(types.WithForceWriter(ctx), original.ID)
	if err != nil {
		return nil, err
	}
	if original == nil || lo.FromPtr(original.RecalculatedInvoiceID) != inv.ID ||
		original.TenantID != types.GetTenantID(ctx) || original.EnvironmentID != types.GetEnvironmentID(ctx) ||
		original.InvoiceStatus != types.InvoiceStatusVoided || original.CustomerID != inv.CustomerID || !strings.EqualFold(original.Currency, inv.Currency) {
		return nil, billingScheduleCorrectionRequired(inv)
	}
	return original, nil
}

type correctionChargeSource struct {
	subscriptionLineID string
	commitmentID       string
	commitmentKind     string
	priceID            string
	subscriptionID     string
	periodStart        time.Time
	periodEnd          time.Time
}

func correctionChargeIdentity(line dto.CreateInvoiceLineItemRequest) correctionChargeSource {
	source := correctionChargeSource{subscriptionLineID: lo.FromPtr(line.SubscriptionLineItemID), commitmentID: lo.FromPtr(line.CommitmentID),
		commitmentKind: line.Metadata[types.MetadataKeyCommitmentLineKind]}
	if source.subscriptionLineID == "" && source.commitmentID == "" {
		source.priceID = lo.FromPtr(line.PriceID)
		source.subscriptionID = lo.FromPtr(line.SubscriptionID)
	}
	if line.PeriodStart != nil {
		source.periodStart = line.PeriodStart.UTC()
	}
	if line.PeriodEnd != nil {
		source.periodEnd = line.PeriodEnd.UTC()
	}
	return source
}

func validateCorrectionCharges(original *invoice.Invoice, prepared []dto.CreateInvoiceLineItemRequest) error {
	available := make(map[correctionChargeSource]bool, len(prepared))
	for _, line := range prepared {
		if line.BillingScheduleItemID == nil {
			available[correctionChargeIdentity(line)] = true
		}
	}
	for _, line := range original.LineItems {
		if line == nil || line.BillingScheduleItemID != nil || line.Amount.IsZero() {
			continue
		}
		source := correctionChargeIdentity(billingScheduleLineRequest(line))
		identified := source.subscriptionLineID != "" || source.commitmentID != "" || source.priceID != ""
		if identified && available[source] {
			continue
		}
		return ierr.NewError("a historical charge is missing from the replacement invoice").
			WithHint("Restore the missing historical charge source before retrying the correction.").
			WithReportableDetails(map[string]any{"original_invoice_id": original.ID, "invoice_line_item_id": line.ID,
				"subscription_line_item_id": source.subscriptionLineID, "commitment_id": source.commitmentID, "price_id": lo.FromPtr(line.PriceID)}).
			Mark(ierr.ErrVersionConflict)
	}
	return nil
}
