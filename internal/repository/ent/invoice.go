package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/invoicelineitem"
	domainInvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type invoiceRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewInvoiceRepository(client postgres.IClient, log *logger.Logger) domainInvoice.Repository {
	return &invoiceRepository{
		client: client,
		log:    log,
	}
}

// Create creates a new invoice (non-transactional)
func (r *invoiceRepository) Create(ctx context.Context, inv *domainInvoice.Invoice) error {
	client := r.client.Querier(ctx)
	invoice, err := client.Invoice.Create().
		SetID(inv.ID).
		SetTenantID(inv.TenantID).
		SetCustomerID(inv.CustomerID).
		SetNillableSubscriptionID(inv.SubscriptionID).
		SetInvoiceType(string(inv.InvoiceType)).
		SetInvoiceStatus(string(inv.InvoiceStatus)).
		SetPaymentStatus(string(inv.PaymentStatus)).
		SetCurrency(inv.Currency).
		SetAmountDue(inv.AmountDue).
		SetAmountPaid(inv.AmountPaid).
		SetAmountRemaining(inv.AmountRemaining).
		SetIdempotencyKey(lo.FromPtr(inv.IdempotencyKey)).
		SetInvoiceNumber(lo.FromPtr(inv.InvoiceNumber)).
		SetBillingSequence(lo.FromPtr(inv.BillingSequence)).
		SetDescription(inv.Description).
		SetNillableDueDate(inv.DueDate).
		SetNillablePaidAt(inv.PaidAt).
		SetNillableVoidedAt(inv.VoidedAt).
		SetNillableFinalizedAt(inv.FinalizedAt).
		SetNillableInvoicePdfURL(inv.InvoicePDFURL).
		SetBillingReason(inv.BillingReason).
		SetMetadata(inv.Metadata).
		SetVersion(inv.Version).
		SetStatus(string(inv.Status)).
		SetCreatedAt(inv.CreatedAt).
		SetUpdatedAt(inv.UpdatedAt).
		SetCreatedBy(inv.CreatedBy).
		SetUpdatedBy(inv.UpdatedBy).
		SetNillablePeriodStart(inv.PeriodStart).
		SetNillablePeriodEnd(inv.PeriodEnd).
		Save(ctx)

	if err != nil {
		r.log.Error("failed to create invoice", "error", err)
		return fmt.Errorf("creating invoice: %w", err)
	}

	*inv = *domainInvoice.FromEnt(invoice)
	return nil
}

// CreateWithLineItems creates an invoice with its line items in a single transaction
func (r *invoiceRepository) CreateWithLineItems(ctx context.Context, inv *domainInvoice.Invoice) error {
	r.log.Debugw("creating invoice with line items",
		"id", inv.ID,
		"line_items_count", len(inv.LineItems))

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// 1. Create invoice
		invoice, err := r.client.Querier(ctx).Invoice.Create().
			SetID(inv.ID).
			SetTenantID(inv.TenantID).
			SetCustomerID(inv.CustomerID).
			SetNillableSubscriptionID(inv.SubscriptionID).
			SetInvoiceType(string(inv.InvoiceType)).
			SetInvoiceStatus(string(inv.InvoiceStatus)).
			SetPaymentStatus(string(inv.PaymentStatus)).
			SetCurrency(inv.Currency).
			SetAmountDue(inv.AmountDue).
			SetAmountPaid(inv.AmountPaid).
			SetAmountRemaining(inv.AmountRemaining).
			SetIdempotencyKey(lo.FromPtr(inv.IdempotencyKey)).
			SetInvoiceNumber(lo.FromPtr(inv.InvoiceNumber)).
			SetBillingSequence(lo.FromPtr(inv.BillingSequence)).
			SetDescription(inv.Description).
			SetNillableDueDate(inv.DueDate).
			SetNillablePaidAt(inv.PaidAt).
			SetNillableVoidedAt(inv.VoidedAt).
			SetNillableFinalizedAt(inv.FinalizedAt).
			SetNillableInvoicePdfURL(inv.InvoicePDFURL).
			SetBillingReason(inv.BillingReason).
			SetMetadata(inv.Metadata).
			SetVersion(inv.Version).
			SetStatus(string(inv.Status)).
			SetCreatedAt(inv.CreatedAt).
			SetUpdatedAt(inv.UpdatedAt).
			SetCreatedBy(inv.CreatedBy).
			SetUpdatedBy(inv.UpdatedBy).
			SetNillablePeriodStart(inv.PeriodStart).
			SetNillablePeriodEnd(inv.PeriodEnd).
			Save(ctx)
		if err != nil {
			r.log.Error("failed to create invoice", "error", err)
			return fmt.Errorf("creating invoice: %w", err)
		}

		// 2. Create line items in bulk if present
		if len(inv.LineItems) > 0 {
			builders := make([]*ent.InvoiceLineItemCreate, len(inv.LineItems))
			for i, item := range inv.LineItems {
				builders[i] = r.client.Querier(ctx).InvoiceLineItem.Create().
					SetID(item.ID).
					SetTenantID(item.TenantID).
					SetInvoiceID(invoice.ID).
					SetCustomerID(item.CustomerID).
					SetNillableSubscriptionID(item.SubscriptionID).
					SetNillablePlanID(item.PlanID).
					SetNillablePlanDisplayName(item.PlanDisplayName).
					SetNillablePriceType(item.PriceType).
					SetPriceID(item.PriceID).
					SetNillableMeterID(item.MeterID).
					SetNillableMeterDisplayName(item.MeterDisplayName).
					SetAmount(item.Amount).
					SetQuantity(item.Quantity).
					SetCurrency(item.Currency).
					SetNillablePeriodStart(item.PeriodStart).
					SetNillablePeriodEnd(item.PeriodEnd).
					SetMetadata(item.Metadata).
					SetStatus(string(item.Status)).
					SetCreatedBy(item.CreatedBy).
					SetUpdatedBy(item.UpdatedBy).
					SetCreatedAt(item.CreatedAt).
					SetUpdatedAt(item.UpdatedAt)
			}

			if err := r.client.Querier(ctx).InvoiceLineItem.CreateBulk(builders...).Exec(ctx); err != nil {
				r.log.Error("failed to create line items", "error", err)
				return fmt.Errorf("creating line items: %w", err)
			}
		}
		*inv = *domainInvoice.FromEnt(invoice)
		return nil
	})
}

// AddLineItems adds line items to an existing invoice
func (r *invoiceRepository) AddLineItems(ctx context.Context, invoiceID string, items []*domainInvoice.InvoiceLineItem) error {
	r.log.Debugw("adding line items", "invoice_id", invoiceID, "count", len(items))

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Verify invoice exists
		exists, err := r.client.Querier(ctx).Invoice.Query().Where(invoice.ID(invoiceID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("checking invoice existence: %w", err)
		}
		if !exists {
			return fmt.Errorf("invoice %s not found", invoiceID)
		}

		builders := make([]*ent.InvoiceLineItemCreate, len(items))
		for i, item := range items {
			builders[i] = r.client.Querier(ctx).InvoiceLineItem.Create().
				SetID(item.ID).
				SetTenantID(item.TenantID).
				SetInvoiceID(invoiceID).
				SetCustomerID(item.CustomerID).
				SetNillableSubscriptionID(item.SubscriptionID).
				SetNillablePlanID(item.PlanID).
				SetNillablePlanDisplayName(item.PlanDisplayName).
				SetNillablePriceType(item.PriceType).
				SetPriceID(item.PriceID).
				SetNillableMeterID(item.MeterID).
				SetNillableMeterDisplayName(item.MeterDisplayName).
				SetAmount(item.Amount).
				SetQuantity(item.Quantity).
				SetCurrency(item.Currency).
				SetNillablePeriodStart(item.PeriodStart).
				SetNillablePeriodEnd(item.PeriodEnd).
				SetMetadata(item.Metadata).
				SetStatus(string(item.Status)).
				SetCreatedBy(item.CreatedBy).
				SetUpdatedBy(item.UpdatedBy).
				SetCreatedAt(item.CreatedAt).
				SetUpdatedAt(item.UpdatedAt)
		}

		if err := r.client.Querier(ctx).InvoiceLineItem.CreateBulk(builders...).Exec(ctx); err != nil {
			r.log.Error("failed to add line items", "error", err)
			return fmt.Errorf("adding line items: %w", err)
		}

		return nil
	})
}

// RemoveLineItems removes line items from an invoice
func (r *invoiceRepository) RemoveLineItems(ctx context.Context, invoiceID string, itemIDs []string) error {
	r.log.Debugw("removing line items", "invoice_id", invoiceID, "count", len(itemIDs))

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Verify invoice exists
		exists, err := r.client.Querier(ctx).Invoice.Query().Where(invoice.ID(invoiceID)).Exist(ctx)
		if err != nil {
			return fmt.Errorf("checking invoice existence: %w", err)
		}
		if !exists {
			return fmt.Errorf("invoice %s not found", invoiceID)
		}

		_, err = r.client.Querier(ctx).InvoiceLineItem.Update().
			Where(
				invoicelineitem.TenantID(types.GetTenantID(ctx)),
				invoicelineitem.InvoiceID(invoiceID),
				invoicelineitem.IDIn(itemIDs...),
			).
			SetStatus(string(types.StatusDeleted)).
			SetUpdatedBy(types.GetUserID(ctx)).
			SetUpdatedAt(time.Now()).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("removing line items: %w", err)
		}
		return nil
	})
}

func (r *invoiceRepository) Get(ctx context.Context, id string) (*domainInvoice.Invoice, error) {
	r.log.Debugw("getting invoice", "id", id)

	invoice, err := r.client.Querier(ctx).Invoice.Query().
		Where(invoice.ID(id)).
		WithLineItems().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("invoice %s not found", id)
		}
		return nil, fmt.Errorf("getting invoice: %w", err)
	}

	return domainInvoice.FromEnt(invoice), nil
}

func (r *invoiceRepository) Update(ctx context.Context, inv *domainInvoice.Invoice) error {
	client := r.client.Querier(ctx)

	// Use predicate-based update for optimistic locking
	query := client.Invoice.Update().
		Where(
			invoice.ID(inv.ID),
			invoice.TenantID(types.GetTenantID(ctx)),
			invoice.Status(string(types.StatusPublished)),
			invoice.Version(inv.Version), // Version check for optimistic locking
		)

	// Set all fields
	query.
		SetInvoiceStatus(string(inv.InvoiceStatus)).
		SetPaymentStatus(string(inv.PaymentStatus)).
		SetAmountDue(inv.AmountDue).
		SetAmountPaid(inv.AmountPaid).
		SetAmountRemaining(inv.AmountRemaining).
		SetDescription(inv.Description).
		SetNillableDueDate(inv.DueDate).
		SetNillablePaidAt(inv.PaidAt).
		SetNillableVoidedAt(inv.VoidedAt).
		SetNillableFinalizedAt(inv.FinalizedAt).
		SetNillableInvoicePdfURL(inv.InvoicePDFURL).
		SetBillingReason(string(inv.BillingReason)).
		SetMetadata(inv.Metadata).
		SetUpdatedAt(inv.UpdatedAt).
		SetUpdatedBy(inv.UpdatedBy).
		AddVersion(1) // Increment version atomically

	// Execute update
	n, err := query.Save(ctx)
	if err != nil {
		return fmt.Errorf("failed to update invoice: %w", err)
	}
	if n == 0 {
		// No rows were updated - either record doesn't exist or version mismatch
		exists, err := client.Invoice.Query().
			Where(
				invoice.ID(inv.ID),
				invoice.TenantID(types.GetTenantID(ctx)),
			).
			Exist(ctx)
		if err != nil {
			return fmt.Errorf("failed to check invoice existence: %w", err)
		}
		if !exists {
			return domainInvoice.ErrInvoiceNotFound
		}
		// Record exists but version mismatch
		return domainInvoice.NewVersionConflictError(inv.ID, inv.Version, inv.Version+1)
	}

	return nil
}

func (r *invoiceRepository) Delete(ctx context.Context, id string) error {
	r.log.Info("deleting invoice", "id", id)

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Delete line items first
		_, err := r.client.Querier(ctx).InvoiceLineItem.Update().
			Where(
				invoicelineitem.InvoiceID(id),
				invoicelineitem.TenantID(types.GetTenantID(ctx)),
			).
			SetStatus(string(types.StatusDeleted)).
			SetUpdatedBy(types.GetUserID(ctx)).
			SetUpdatedAt(time.Now()).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("deleting line items: %w", err)
		}

		// Then delete invoice
		_, err = r.client.Querier(ctx).Invoice.Update().
			Where(
				invoice.ID(id),
				invoice.TenantID(types.GetTenantID(ctx)),
				invoice.Status(string(types.StatusPublished)),
			).
			SetStatus(string(types.StatusDeleted)).
			SetUpdatedBy(types.GetUserID(ctx)).
			SetUpdatedAt(time.Now()).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("deleting invoice: %w", err)
		}

		return nil
	})
}

func (r *invoiceRepository) List(ctx context.Context, filter *types.InvoiceFilter) ([]*domainInvoice.Invoice, error) {
	client := r.client.Querier(ctx)
	query := client.Invoice.Query().
		WithLineItems()

	query = ToEntQuery(ctx, filter, query)

	// Apply order by
	query = query.Order(ent.Desc(invoice.FieldCreatedAt))

	// Apply pagination
	if filter != nil && filter.Limit > 0 {
		query = query.Limit(filter.Limit)
		if filter.Offset > 0 {
			query = query.Offset(filter.Offset)
		}
	}

	invoices, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list invoices: %w", err)
	}

	// Convert to domain model
	result := make([]*domainInvoice.Invoice, len(invoices))
	for i, inv := range invoices {
		result[i] = domainInvoice.FromEnt(inv)
	}

	return result, nil
}

func (r *invoiceRepository) Count(ctx context.Context, filter *types.InvoiceFilter) (int, error) {
	client := r.client.Querier(ctx)
	query := client.Invoice.Query()

	if filter != nil {
		query = ToEntQuery(ctx, filter, query)
	}

	count, err := query.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count invoices: %w", err)
	}
	return count, nil
}

func (r *invoiceRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domainInvoice.Invoice, error) {
	inv, err := r.client.Querier(ctx).Invoice.Query().
		Where(
			invoice.IdempotencyKeyEQ(key),
			invoice.TenantID(types.GetTenantID(ctx)),
			invoice.StatusEQ(string(types.StatusPublished)),
			invoice.InvoiceStatusNEQ(string(types.InvoiceStatusVoided)),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, domainInvoice.ErrInvoiceNotFound
		}
		return nil, fmt.Errorf("failed to get invoice by idempotency key: %w", err)
	}

	return domainInvoice.FromEnt(inv), nil
}

func (r *invoiceRepository) ExistsForPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
	exists, err := r.client.Querier(ctx).Invoice.Query().
		Where(
			invoice.And(
				invoice.TenantID(types.GetTenantID(ctx)),
				invoice.SubscriptionIDEQ(subscriptionID),
				invoice.PeriodStartEQ(periodStart),
				invoice.PeriodEndEQ(periodEnd),
				invoice.StatusEQ(string(types.StatusPublished)),
				invoice.InvoiceStatusNEQ(string(types.InvoiceStatusVoided)),
			),
		).
		Exist(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check invoice existence: %w", err)
	}

	return exists, nil
}

func (r *invoiceRepository) GetNextInvoiceNumber(ctx context.Context) (string, error) {
	yearMonth := time.Now().Format("200601") // YYYYMM
	tenantID := types.GetTenantID(ctx)

	// Use raw SQL for atomic increment since ent doesn't support RETURNING with OnConflict
	query := `
		INSERT INTO invoice_sequences (tenant_id, year_month, last_value, created_at, updated_at)
		VALUES ($1, $2, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (tenant_id, year_month) DO UPDATE
		SET last_value = invoice_sequences.last_value + 1,
			updated_at = CURRENT_TIMESTAMP
		RETURNING last_value`

	var lastValue int64
	rows, err := r.client.Querier(ctx).QueryContext(ctx, query, tenantID, yearMonth)
	if err != nil {
		return "", fmt.Errorf("failed to execute sequence query: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return "", fmt.Errorf("no sequence value returned")
	}

	if err := rows.Scan(&lastValue); err != nil {
		return "", fmt.Errorf("failed to scan sequence value: %w", err)
	}

	r.log.Infow("generated invoice number",
		"tenant_id", tenantID,
		"year_month", yearMonth,
		"sequence", lastValue)

	return fmt.Sprintf("INV-%s-%05d", yearMonth, lastValue), nil
}

func (r *invoiceRepository) GetNextBillingSequence(ctx context.Context, subscriptionID string) (int, error) {
	tenantID := types.GetTenantID(ctx)
	// Use raw SQL for atomic increment since ent doesn't support RETURNING with OnConflict
	query := `
		INSERT INTO billing_sequences (tenant_id, subscription_id, last_sequence, created_at, updated_at)
		VALUES ($1, $2, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (tenant_id, subscription_id) DO UPDATE
		SET last_sequence = billing_sequences.last_sequence + 1,
			updated_at = CURRENT_TIMESTAMP
		RETURNING last_sequence`

	var lastSequence int
	rows, err := r.client.Querier(ctx).QueryContext(ctx, query, tenantID, subscriptionID)
	if err != nil {
		return 0, fmt.Errorf("failed to execute sequence query: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return 0, fmt.Errorf("no sequence value returned")
	}

	if err := rows.Scan(&lastSequence); err != nil {
		return 0, fmt.Errorf("failed to scan sequence value: %w", err)
	}

	r.log.Infow("generated billing sequence",
		"tenant_id", tenantID,
		"subscription_id", subscriptionID,
		"sequence", lastSequence)

	return lastSequence, nil
}

// helper functions

// Add a helper function to parse the InvoiceFilter struct to relevant ent base *ent.InvoiceQuery
func ToEntQuery(ctx context.Context, f *types.InvoiceFilter, query *ent.InvoiceQuery) *ent.InvoiceQuery {
	if f == nil {
		return query
	}

	query.Where(
		invoice.TenantID(types.GetTenantID(ctx)),
		invoice.Status(string(types.StatusPublished)),
	)
	if f.CustomerID != "" {
		query = query.Where(invoice.CustomerID(f.CustomerID))
	}
	if f.SubscriptionID != "" {
		query = query.Where(invoice.SubscriptionID(f.SubscriptionID))
	}
	if f.InvoiceType != "" {
		query = query.Where(invoice.InvoiceType(string(f.InvoiceType)))
	}
	if len(f.InvoiceStatus) > 0 {
		invoiceStatuses := make([]string, len(f.InvoiceStatus))
		for i, status := range f.InvoiceStatus {
			invoiceStatuses[i] = string(status)
		}
		query = query.Where(invoice.InvoiceStatusIn(invoiceStatuses...))
	}
	if len(f.PaymentStatus) > 0 {
		paymentStatuses := make([]string, len(f.PaymentStatus))
		for i, status := range f.PaymentStatus {
			paymentStatuses[i] = string(status)
		}
		query = query.Where(invoice.PaymentStatusIn(paymentStatuses...))
	}
	if f.StartTime != nil {
		query = query.Where(invoice.CreatedAtGTE(*f.StartTime))
	}
	if f.EndTime != nil {
		query = query.Where(invoice.CreatedAtLTE(*f.EndTime))
	}
	return query
}
