package ent

import (
	"context"
	"time"

	entgen "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/invoice"
	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type selfBillingInvoiceValueRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

func NewSelfBillingInvoiceValueRepository(client postgres.IClient, logger *logger.Logger) domainsb.InvoiceValueRepository {
	return &selfBillingInvoiceValueRepository{client: client, logger: logger}
}

// ListFinalized reads across every tenant on purpose: Flexprice bills each tenant on invoice value.
func (r *selfBillingInvoiceValueRepository) ListFinalized(ctx context.Context, from, to time.Time) ([]*domainsb.FinalizedInvoice, error) {
	rows, err := r.client.Reader(ctx).Invoice.Query().
		Where(
			invoice.Status(string(types.StatusPublished)),
			invoice.InvoiceStatusEQ(types.InvoiceStatusFinalized),
			invoice.FinalizedAtGTE(from.UTC()),
			invoice.FinalizedAtLT(to.UTC()),
		).
		Select(
			invoice.FieldID, invoice.FieldTenantID, invoice.FieldEnvironmentID, invoice.FieldSubscriptionID,
			invoice.FieldInvoiceType, invoice.FieldBillingReason, invoice.FieldCurrency, invoice.FieldSubtotal,
			invoice.FieldTotalDiscount, invoice.FieldTotalPrepaidCreditsApplied, invoice.FieldTotalTax, invoice.FieldTotal,
			invoice.FieldAmountDue, invoice.FieldAmountPaid, invoice.FieldPeriodStart, invoice.FieldPeriodEnd, invoice.FieldFinalizedAt,
		).
		All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to list finalized invoices for self-billing").Mark(ierr.ErrDatabase)
	}
	return lo.Map(rows, func(e *entgen.Invoice, _ int) *domainsb.FinalizedInvoice {
		return &domainsb.FinalizedInvoice{
			ID: e.ID, TenantID: e.TenantID, EnvironmentID: e.EnvironmentID,
			SubscriptionID: lo.FromPtr(e.SubscriptionID), InvoiceType: string(e.InvoiceType), BillingReason: e.BillingReason,
			Currency: e.Currency, Subtotal: e.Subtotal, TotalDiscount: lo.FromPtr(e.TotalDiscount),
			TotalPrepaidCreditsApplied: lo.FromPtr(e.TotalPrepaidCreditsApplied), TotalTax: lo.FromPtr(e.TotalTax),
			Total: e.Total, AmountDue: e.AmountDue, AmountPaid: e.AmountPaid,
			PeriodStart: e.PeriodStart, PeriodEnd: e.PeriodEnd, FinalizedAt: lo.FromPtr(e.FinalizedAt),
		}
	}), nil
}
