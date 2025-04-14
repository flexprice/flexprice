package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/invoicelineitem"
	domainInvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type invoiceRepository struct {
	client    postgres.IClient
	logger    *logger.Logger
	queryOpts InvoiceQueryOptions
}

func NewInvoiceRepository(client postgres.IClient, logger *logger.Logger) domainInvoice.Repository {
	return &invoiceRepository{
		client:    client,
		logger:    logger,
		queryOpts: InvoiceQueryOptions{},
	}
}

// Create creates a new invoice (non-transactional)
func (r *invoiceRepository) Create(ctx context.Context, inv *domainInvoice.Invoice) error {
	client := r.client.Querier(ctx)

	// Set environment ID from context if not already set
	if inv.EnvironmentID == "" {
		inv.EnvironmentID = types.GetEnvironmentID(ctx)
	}

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
		SetNillableBillingPeriod(inv.BillingPeriod).
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
		SetEnvironmentID(inv.EnvironmentID).
		Save(ctx)

	if err != nil {
		r.logger.Error("failed to create invoice", "error", err)
		return ierr.WithError(err).WithHint("invoice creation failed").Mark(ierr.ErrDatabase)
	}

	*inv = *domainInvoice.FromEnt(invoice)
	return nil
}

// CreateWithLineItems creates an invoice with its line items in a single transaction
func (r *invoiceRepository) CreateWithLineItems(ctx context.Context, inv *domainInvoice.Invoice) error {
	r.logger.Debugw("creating invoice with line items",
		"id", inv.ID,
		"line_items_count", len(inv.LineItems))

	// Set environment ID from context if not already set
	if inv.EnvironmentID == "" {
		inv.EnvironmentID = types.GetEnvironmentID(ctx)
	}

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
			SetNillableBillingPeriod(inv.BillingPeriod).
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
			SetEnvironmentID(inv.EnvironmentID).
			Save(ctx)
		if err != nil {
			r.logger.Error("failed to create invoice", "error", err)
			return ierr.WithError(err).WithHint("invoice creation failed").Mark(ierr.ErrDatabase)
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
					SetNillablePriceID(item.PriceID).
					SetNillableMeterID(item.MeterID).
					SetNillableMeterDisplayName(item.MeterDisplayName).
					SetNillableDisplayName(item.DisplayName).
					SetAmount(item.Amount).
					SetQuantity(item.Quantity).
					SetCurrency(item.Currency).
					SetNillablePeriodStart(item.PeriodStart).
					SetNillablePeriodEnd(item.PeriodEnd).
					SetMetadata(item.Metadata).
					SetEnvironmentID(item.EnvironmentID).
					SetStatus(string(item.Status)).
					SetCreatedBy(item.CreatedBy).
					SetUpdatedBy(item.UpdatedBy).
					SetCreatedAt(item.CreatedAt).
					SetUpdatedAt(item.UpdatedAt)
			}

			if err := r.client.Querier(ctx).InvoiceLineItem.CreateBulk(builders...).Exec(ctx); err != nil {
				r.logger.Error("failed to create line items", "error", err)
				return ierr.WithError(err).WithHint("line item creation failed").Mark(ierr.ErrDatabase)
			}
		}
		*inv = *domainInvoice.FromEnt(invoice)
		return nil
	})
}

// AddLineItems adds line items to an existing invoice
func (r *invoiceRepository) AddLineItems(ctx context.Context, invoiceID string, items []*domainInvoice.InvoiceLineItem) error {
	r.logger.Debugw("adding line items", "invoice_id", invoiceID, "count", len(items))

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Verify invoice exists
		exists, err := r.client.Querier(ctx).Invoice.Query().Where(invoice.ID(invoiceID)).Exist(ctx)
		if err != nil {
			return ierr.WithError(err).WithHint("invoice existence check failed").Mark(ierr.ErrDatabase)
		}
		if !exists {
			return ierr.WithError(err).WithHintf("invoice %s not found", invoiceID).Mark(ierr.ErrNotFound)
		}

		builders := make([]*ent.InvoiceLineItemCreate, len(items))
		for i, item := range items {
			builders[i] = r.client.Querier(ctx).InvoiceLineItem.Create().
				SetID(item.ID).
				SetTenantID(item.TenantID).
				SetEnvironmentID(item.EnvironmentID).
				SetInvoiceID(invoiceID).
				SetCustomerID(item.CustomerID).
				SetNillableSubscriptionID(item.SubscriptionID).
				SetNillablePlanID(item.PlanID).
				SetNillablePlanDisplayName(item.PlanDisplayName).
				SetNillablePriceType(item.PriceType).
				SetNillablePriceID(item.PriceID).
				SetNillableMeterID(item.MeterID).
				SetNillableMeterDisplayName(item.MeterDisplayName).
				SetNillableDisplayName(item.DisplayName).
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
			r.logger.Error("failed to add line items", "error", err)
			return ierr.WithError(err).WithHint("line item addition failed").Mark(ierr.ErrDatabase)
		}

		return nil
	})
}

// RemoveLineItems removes line items from an invoice
func (r *invoiceRepository) RemoveLineItems(ctx context.Context, invoiceID string, itemIDs []string) error {
	r.logger.Debugw("removing line items", "invoice_id", invoiceID, "count", len(itemIDs))

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Verify invoice exists
		exists, err := r.client.Querier(ctx).Invoice.Query().Where(invoice.ID(invoiceID)).Exist(ctx)
		if err != nil {
			return ierr.WithError(err).WithHint("invoice existence check failed").Mark(ierr.ErrDatabase)
		}
		if !exists {
			return ierr.WithError(err).WithHintf("invoice %s not found", invoiceID).Mark(ierr.ErrNotFound)
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
			return ierr.WithError(err).WithHint("line item removal failed").Mark(ierr.ErrDatabase)
		}
		return nil
	})
}

func (r *invoiceRepository) Get(ctx context.Context, id string) (*domainInvoice.Invoice, error) {
	r.logger.Debugw("getting invoice", "id", id)

	invoice, err := r.client.Querier(ctx).Invoice.Query().
		Where(invoice.ID(id)).
		WithLineItems().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.
				WithError(err).
				WithHintf("invoice %s not found", id).
				WithReportableDetails(map[string]any{
					"id": id,
				}).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("getting invoice failed").Mark(ierr.ErrDatabase)
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
		SetUpdatedAt(time.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		AddVersion(1) // Increment version atomically

	// Execute update
	n, err := query.Save(ctx)
	if err != nil {
		return ierr.WithError(err).WithHint("invoice update failed").Mark(ierr.ErrDatabase)
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
			return ierr.WithError(err).WithHint("invoice existence check failed").Mark(ierr.ErrDatabase)
		}
		if !exists {
			return ierr.NewError("invoice not found").WithHint("invoice not found").Mark(ierr.ErrNotFound)
		}
		// Record exists but version mismatch
		return ierr.NewError("invoice version mismatch").
			WithHintf("invoice version mismatch for id: %s", inv.ID).
			WithReportableDetails(map[string]any{
				"id":               inv.ID,
				"current_version":  inv.Version,
				"expected_version": inv.Version + 1,
			}).Mark(ierr.ErrVersionConflict)
	}

	return nil
}

func (r *invoiceRepository) Delete(ctx context.Context, id string) error {
	r.logger.Info("deleting invoice", "id", id)

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
			return ierr.WithError(err).WithHint("line item deletion failed").Mark(ierr.ErrDatabase)
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
			return ierr.WithError(err).WithHint("invoice deletion failed").Mark(ierr.ErrDatabase)
		}

		return nil
	})
}

// List returns a paginated list of invoices based on the filter
func (r *invoiceRepository) List(ctx context.Context, filter *types.InvoiceFilter) ([]*domainInvoice.Invoice, error) {
	client := r.client.Querier(ctx)
	query := client.Invoice.Query().
		WithLineItems()

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	invoices, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("invoice listing failed").WithReportableDetails(
			map[string]any{
				"cause": err.Error(),
			},
		).Mark(ierr.ErrDatabase)
	}

	// Convert to domain model
	result := make([]*domainInvoice.Invoice, len(invoices))
	for i, inv := range invoices {
		result[i] = domainInvoice.FromEnt(inv)
	}

	return result, nil
}

// Count returns the total number of invoices based on the filter
func (r *invoiceRepository) Count(ctx context.Context, filter *types.InvoiceFilter) (int, error) {
	client := r.client.Querier(ctx)
	query := client.Invoice.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).WithHint("invoice counting failed").Mark(ierr.ErrDatabase)
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
			return nil, ierr.WithError(err).WithHint("invoice not found").Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("failed to get invoice by idempotency key").Mark(ierr.ErrDatabase)
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
		return false, ierr.WithError(err).WithHint("invoice existence check failed").WithReportableDetails(map[string]any{
			"subscription_id": subscriptionID,
			"period_start":    periodStart.String(),
			"period_end":      periodEnd.String(),
		}).Mark(ierr.ErrDatabase)
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
		return "", ierr.WithError(err).WithHint("invoice number generation failed").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	if !rows.Next() {
		return "", ierr.WithError(err).WithHint("no sequence value returned").Mark(ierr.ErrDatabase)
	}

	if err := rows.Scan(&lastValue); err != nil {
		return "", ierr.WithError(err).WithHint("invoice number generation failed").Mark(ierr.ErrDatabase)
	}

	r.logger.Infow("generated invoice number",
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
		return 0, ierr.WithError(err).WithHint("billing sequence generation failed").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	if !rows.Next() {
		return 0, ierr.WithError(err).WithHint("no sequence value returned").Mark(ierr.ErrDatabase)
	}

	if err := rows.Scan(&lastSequence); err != nil {
		return 0, ierr.WithError(err).WithHint("billing sequence generation failed").Mark(ierr.ErrDatabase)
	}

	r.logger.Infow("generated billing sequence",
		"tenant_id", tenantID,
		"subscription_id", subscriptionID,
		"sequence", lastSequence)

	return lastSequence, nil
}

// InvoiceQuery type alias for better readability
type InvoiceQuery = *ent.InvoiceQuery

// InvoiceQueryOptions implements BaseQueryOptions for invoice queries
type InvoiceQueryOptions struct{}

func (o InvoiceQueryOptions) ApplyTenantFilter(ctx context.Context, query InvoiceQuery) InvoiceQuery {
	return query.Where(invoice.TenantID(types.GetTenantID(ctx)))
}

func (o InvoiceQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query InvoiceQuery) InvoiceQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(invoice.EnvironmentID(environmentID))
	}
	return query
}

func (o InvoiceQueryOptions) ApplyStatusFilter(query InvoiceQuery, status string) InvoiceQuery {
	if status == "" {
		return query.Where(invoice.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(invoice.Status(status))
}

func (o InvoiceQueryOptions) ApplySortFilter(query InvoiceQuery, field string, order string) InvoiceQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o InvoiceQueryOptions) ApplyPaginationFilter(query InvoiceQuery, limit int, offset int) InvoiceQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o InvoiceQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return invoice.FieldCreatedAt
	case "updated_at":
		return invoice.FieldUpdatedAt
	case "invoice_number":
		return invoice.FieldInvoiceNumber
	default:
		return field
	}
}

func (o InvoiceQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.InvoiceFilter, query InvoiceQuery) InvoiceQuery {
	if f == nil {
		return query
	}

	// Apply entity-specific filters
	if f.CustomerID != "" {
		query = query.Where(invoice.CustomerID(f.CustomerID))
	}
	if f.SubscriptionID != "" {
		query = query.Where(invoice.SubscriptionID(f.SubscriptionID))
	}
	if f.InvoiceType != "" {
		query = query.Where(invoice.InvoiceType(string(f.InvoiceType)))
	}
	if len(f.InvoiceIDs) > 0 {
		query = query.Where(invoice.IDIn(f.InvoiceIDs...))
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
	if f.AmountDueGt != nil {
		query = query.Where(invoice.AmountDueGT(*f.AmountDueGt))
	}
	if f.AmountRemainingGt != nil {
		query = query.Where(invoice.AmountRemainingGT(*f.AmountRemainingGt))
	}

	// Apply time range filters
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(invoice.PeriodStartGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(invoice.PeriodEndLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	return query
}
