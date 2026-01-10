package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/couponapplication"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/invoicelineitem"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/cache"
	domainInvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/samber/lo"
)

type invoiceRepository struct {
	client    postgres.IClient
	logger    *logger.Logger
	queryOpts InvoiceQueryOptions
	cache     cache.Cache
}

func NewInvoiceRepository(client postgres.IClient, logger *logger.Logger, cache cache.Cache) domainInvoice.Repository {
	return &invoiceRepository{
		client:    client,
		logger:    logger,
		queryOpts: InvoiceQueryOptions{},
		cache:     cache,
	}
}

// Create creates a new invoice (non-transactional)
func (r *invoiceRepository) Create(ctx context.Context, inv *domainInvoice.Invoice) error {
	client := r.client.Writer(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "create", map[string]interface{}{
		"invoice_id":  inv.ID,
		"customer_id": inv.CustomerID,
		"tenant_id":   inv.TenantID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if inv.EnvironmentID == "" {
		inv.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	invoice, err := client.Invoice.Create().
		SetID(inv.ID).
		SetTenantID(inv.TenantID).
		SetCustomerID(inv.CustomerID).
		SetNillableSubscriptionID(inv.SubscriptionID).
		SetInvoiceType(inv.InvoiceType).
		SetInvoiceStatus(inv.InvoiceStatus).
		SetPaymentStatus(inv.PaymentStatus).
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
		SetBillingPeriod(types.BillingPeriod(lo.FromPtr(inv.BillingPeriod))).
		SetNillableInvoicePdfURL(inv.InvoicePDFURL).
		SetBillingReason(inv.BillingReason).
		SetMetadata(inv.Metadata).
		SetVersion(inv.Version).
		SetStatus(string(inv.Status)).
		SetCreatedAt(inv.CreatedAt).
		SetTotal(inv.Total).
		SetSubtotal(inv.Subtotal).
		SetUpdatedAt(inv.UpdatedAt).
		SetCreatedBy(inv.CreatedBy).
		SetTotalTax(inv.TotalTax).
		SetUpdatedBy(inv.UpdatedBy).
		SetNillablePeriodStart(inv.PeriodStart).
		SetNillablePeriodEnd(inv.PeriodEnd).
		SetEnvironmentID(inv.EnvironmentID).
		SetAdjustmentAmount(inv.AdjustmentAmount).
		SetRefundedAmount(inv.RefundedAmount).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		r.logger.Error("failed to create invoice", "error", err)
		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Idx_tenant_environment_invoice_number_unique {
					return ierr.WithError(err).
						WithHint("Invoice with same invoice number already exists").
						WithReportableDetails(map[string]any{
							"invoice_id":     inv.ID,
							"invoice_number": inv.InvoiceNumber,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
				if pqErr.Constraint == schema.Idx_tenant_environment_idempotency_key_unique {
					return ierr.WithError(err).
						WithHint("Invoice with same idempotency key already exists").
						WithReportableDetails(map[string]any{
							"invoice_id":      inv.ID,
							"idempotency_key": inv.IdempotencyKey,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}

			return ierr.WithError(err).
				WithHint("invoice creation failed").
				WithReportableDetails(map[string]any{
					"invoice_id": inv.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
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

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "create_with_line_items", map[string]interface{}{
		"invoice_id":       inv.ID,
		"customer_id":      inv.CustomerID,
		"line_items_count": len(inv.LineItems),
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if inv.EnvironmentID == "" {
		inv.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// 1. Create invoice
		invoice, err := r.client.Writer(ctx).Invoice.Create().
			SetID(inv.ID).
			SetTenantID(inv.TenantID).
			SetCustomerID(inv.CustomerID).
			SetNillableSubscriptionID(inv.SubscriptionID).
			SetInvoiceType(inv.InvoiceType).
			SetInvoiceStatus(inv.InvoiceStatus).
			SetPaymentStatus(inv.PaymentStatus).
			SetCurrency(inv.Currency).
			SetAmountDue(inv.AmountDue).
			SetAmountPaid(inv.AmountPaid).
			SetTotalTax(inv.TotalTax).
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
			SetBillingPeriod(types.BillingPeriod(lo.FromPtr(inv.BillingPeriod))).
			SetBillingReason(inv.BillingReason).
			SetMetadata(inv.Metadata).
			SetVersion(inv.Version).
			SetStatus(string(inv.Status)).
			SetCreatedAt(inv.CreatedAt).
			SetUpdatedAt(inv.UpdatedAt).
			SetCreatedBy(inv.CreatedBy).
			SetTotal(inv.Total).
			SetSubtotal(inv.Subtotal).
			SetUpdatedBy(inv.UpdatedBy).
			SetAdjustmentAmount(inv.AdjustmentAmount).
			SetRefundedAmount(inv.RefundedAmount).
			SetNillablePeriodStart(inv.PeriodStart).
			SetNillablePeriodEnd(inv.PeriodEnd).
			SetEnvironmentID(inv.EnvironmentID).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				var pqErr *pq.Error
				if errors.As(err, &pqErr) {
					// Log or print the exact constraint name
					fmt.Printf("Violated constraint: %s\n", pqErr.Constraint)
					if pqErr.Constraint == schema.Idx_tenant_environment_invoice_number_unique {
						return ierr.WithError(err).
							WithHint("Invoice with same invoice number already exists").
							WithReportableDetails(map[string]any{
								"invoice_id":     inv.ID,
								"invoice_number": inv.InvoiceNumber,
							}).
							Mark(ierr.ErrAlreadyExists)
					}
					if pqErr.Constraint == schema.Idx_tenant_environment_idempotency_key_unique {
						return ierr.WithError(err).
							WithHint("Invoice with same idempotency key already exists").
							WithReportableDetails(map[string]any{
								"invoice_id":      inv.ID,
								"idempotency_key": inv.IdempotencyKey,
							}).
							Mark(ierr.ErrAlreadyExists)
					}
				}
				return ierr.WithError(err).
					WithHint("Invoice with same invoice number or idempotency key already exists").
					WithReportableDetails(map[string]any{
						"invoice_id": inv.ID,
					}).
					Mark(ierr.ErrAlreadyExists)
			}
			r.logger.Error("failed to create invoice", "error", err)
			return ierr.WithError(err).WithHint("invoice creation failed").Mark(ierr.ErrDatabase)
		}

		// 2. Create line items in bulk if present
		if len(inv.LineItems) > 0 {
			builders := make([]*ent.InvoiceLineItemCreate, len(inv.LineItems))
			for i, item := range inv.LineItems {
				builders[i] = r.client.Writer(ctx).InvoiceLineItem.Create().
					SetID(item.ID).
					SetTenantID(item.TenantID).
					SetInvoiceID(invoice.ID).
					SetCustomerID(item.CustomerID).
					SetNillableSubscriptionID(item.SubscriptionID).
					SetNillableEntityID(item.EntityID).
					SetNillableEntityType(convertStringPtrToInvoiceLineItemEntityTypePtr(item.EntityType)).
					SetNillablePlanDisplayName(item.PlanDisplayName).
					SetNillablePriceType(convertStringPtrToPriceTypePtr(item.PriceType)).
					SetNillablePriceID(item.PriceID).
					SetNillableMeterID(item.MeterID).
					SetNillableMeterDisplayName(item.MeterDisplayName).
					SetNillablePriceUnitID(item.PriceUnitID).
					SetNillablePriceUnit(item.PriceUnit).
					SetNillablePriceUnitAmount(item.PriceUnitAmount).
					SetNillableDisplayName(item.DisplayName).
					SetAmount(item.Amount).
					SetQuantity(item.Quantity).
					SetCurrency(item.Currency).
					SetNillablePeriodStart(item.PeriodStart).
					SetNillablePeriodEnd(item.PeriodEnd).
					SetMetadata(item.Metadata).
					SetEnvironmentID(item.EnvironmentID).
					SetCommitmentInfo(item.CommitmentInfo).
					SetStatus(string(item.Status)).
					SetCreatedBy(item.CreatedBy).
					SetUpdatedBy(item.UpdatedBy).
					SetCreatedAt(item.CreatedAt).
					SetUpdatedAt(item.UpdatedAt)
			}

			if err := r.client.Writer(ctx).InvoiceLineItem.CreateBulk(builders...).Exec(ctx); err != nil {
				r.logger.Error("failed to create line items", "error", err)
				return ierr.WithError(err).WithHint("line item creation failed").Mark(ierr.ErrDatabase)
			}
		}

		invoiceWithLineItems, err := r.Get(ctx, invoice.ID)
		if err != nil {
			r.logger.Error("failed to get invoice with line items", "error", err)
			return err
		}
		*inv = *invoiceWithLineItems
		return nil
	})
}

// AddLineItems adds line items to an existing invoice
func (r *invoiceRepository) AddLineItems(ctx context.Context, invoiceID string, items []*domainInvoice.InvoiceLineItem) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "add_line_items", map[string]interface{}{
		"invoice_id":  invoiceID,
		"items_count": len(items),
	})
	defer FinishSpan(span)

	r.logger.Debugw("adding line items", "invoice_id", invoiceID, "count", len(items))

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Verify invoice exists
		exists, err := r.client.Writer(ctx).Invoice.Query().Where(invoice.ID(invoiceID)).Exist(ctx)
		if err != nil {
			return ierr.WithError(err).WithHint("invoice existence check failed").Mark(ierr.ErrDatabase)
		}
		if !exists {
			return ierr.WithError(err).WithHintf("invoice %s not found", invoiceID).Mark(ierr.ErrNotFound)
		}

		builders := make([]*ent.InvoiceLineItemCreate, len(items))
		for i, item := range items {
			builders[i] = r.client.Writer(ctx).InvoiceLineItem.Create().
				SetID(item.ID).
				SetTenantID(item.TenantID).
				SetEnvironmentID(item.EnvironmentID).
				SetInvoiceID(invoiceID).
				SetCustomerID(item.CustomerID).
				SetNillableSubscriptionID(item.SubscriptionID).
				SetNillableEntityID(item.EntityID).
				SetNillableEntityType(convertStringPtrToInvoiceLineItemEntityTypePtr(item.EntityType)).
				SetNillablePlanDisplayName(item.PlanDisplayName).
				SetNillablePriceType(convertStringPtrToPriceTypePtr(item.PriceType)).
				SetNillablePriceID(item.PriceID).
				SetNillableMeterID(item.MeterID).
				SetNillableMeterDisplayName(item.MeterDisplayName).
				SetNillablePriceUnitID(item.PriceUnitID).
				SetNillablePriceUnit(item.PriceUnit).
				SetNillableDisplayName(item.DisplayName).
				SetAmount(item.Amount).
				SetQuantity(item.Quantity).
				SetCurrency(item.Currency).
				SetNillablePeriodStart(item.PeriodStart).
				SetNillablePeriodEnd(item.PeriodEnd).
				SetMetadata(item.Metadata).
				SetCommitmentInfo(item.CommitmentInfo).
				SetStatus(string(item.Status)).
				SetCreatedBy(item.CreatedBy).
				SetUpdatedBy(item.UpdatedBy).
				SetCreatedAt(item.CreatedAt).
				SetUpdatedAt(item.UpdatedAt)
		}

		if err := r.client.Writer(ctx).InvoiceLineItem.CreateBulk(builders...).Exec(ctx); err != nil {
			r.logger.Error("failed to add line items", "error", err)
			return ierr.WithError(err).WithHint("line item addition failed").Mark(ierr.ErrDatabase)
		}

		return nil
	})
}

// RemoveLineItems removes line items from an invoice
func (r *invoiceRepository) RemoveLineItems(ctx context.Context, invoiceID string, itemIDs []string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "remove_line_items", map[string]interface{}{
		"invoice_id":  invoiceID,
		"items_count": len(itemIDs),
	})
	defer FinishSpan(span)

	r.logger.Debugw("removing line items", "invoice_id", invoiceID, "count", len(itemIDs))

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Verify invoice exists
		exists, err := r.client.Writer(ctx).Invoice.Query().Where(invoice.ID(invoiceID)).Exist(ctx)
		if err != nil {
			return ierr.WithError(err).WithHint("invoice existence check failed").Mark(ierr.ErrDatabase)
		}
		if !exists {
			return ierr.WithError(err).WithHintf("invoice %s not found", invoiceID).Mark(ierr.ErrNotFound)
		}

		_, err = r.client.Writer(ctx).InvoiceLineItem.Update().
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
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "get", map[string]interface{}{
		"invoice_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedInvoice := r.GetCache(ctx, id); cachedInvoice != nil {
		return cachedInvoice, nil
	}

	r.logger.Debugw("getting invoice", "id", id)

	invoice, err := r.client.Writer(ctx).Invoice.Query().
		Where(invoice.ID(id),
			invoice.TenantID(types.GetTenantID(ctx)),
			invoice.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		WithLineItems(func(q *ent.InvoiceLineItemQuery) {
			q.Where(invoicelineitem.Status(string(types.StatusPublished)))
		}).
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

	invoiceData := domainInvoice.FromEnt(invoice)
	r.SetCache(ctx, invoiceData)
	return invoiceData, nil
}

func (r *invoiceRepository) Update(ctx context.Context, inv *domainInvoice.Invoice) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "update", map[string]interface{}{
		"invoice_id": inv.ID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	// Use predicate-based update for optimistic locking
	query := client.Invoice.Update().
		Where(
			invoice.ID(inv.ID),
			invoice.TenantID(types.GetTenantID(ctx)),
			invoice.Status(string(types.StatusPublished)),
			invoice.EnvironmentID(types.GetEnvironmentID(ctx)),
			// invoice.Version(inv.Version), // Version check for optimistic locking
		)

	// Set all fields
	query.
		SetInvoiceStatus(inv.InvoiceStatus).
		SetPaymentStatus(inv.PaymentStatus).
		SetAmountDue(inv.AmountDue).
		SetAmountPaid(inv.AmountPaid).
		SetAmountRemaining(inv.AmountRemaining).
		SetSubtotal(inv.Subtotal).
		SetTotalTax(inv.TotalTax).
		SetTotal(inv.Total).
		SetDescription(inv.Description).
		SetNillableDueDate(inv.DueDate).
		SetNillablePaidAt(inv.PaidAt).
		SetNillableVoidedAt(inv.VoidedAt).
		SetNillableFinalizedAt(inv.FinalizedAt).
		SetNillableInvoicePdfURL(inv.InvoicePDFURL).
		SetBillingReason(string(inv.BillingReason)).
		SetMetadata(inv.Metadata).
		SetAdjustmentAmount(inv.AdjustmentAmount).
		SetRefundedAmount(inv.RefundedAmount).
		SetUpdatedAt(time.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetTotal(inv.Total).
		SetSubtotal(inv.Subtotal).
		SetTotalDiscount(inv.TotalDiscount).
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
	r.DeleteCache(ctx, inv.ID)
	return nil
}

func (r *invoiceRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "delete", map[string]interface{}{
		"invoice_id": id,
	})
	defer FinishSpan(span)

	r.logger.Info("deleting invoice", "id", id)

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Delete line items first
		_, err := r.client.Writer(ctx).InvoiceLineItem.Update().
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
		_, err = r.client.Writer(ctx).Invoice.Update().
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
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	query := client.Invoice.Query().
		WithCouponApplications(func(q *ent.CouponApplicationQuery) {
			q.Where(couponapplication.Status(string(types.StatusPublished)))
		})

	if !filter.SkipLineItems {
		query = query.WithLineItems(func(q *ent.InvoiceLineItemQuery) {
			q.Where(invoicelineitem.Status(string(types.StatusPublished)))
		})
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

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
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	query := client.Invoice.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).WithHint("invoice counting failed").Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (r *invoiceRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domainInvoice.Invoice, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "get_by_idempotency_key", map[string]interface{}{
		"idempotency_key": key,
	})
	defer FinishSpan(span)
	// Try to get from cache first
	if cachedInvoice := r.GetCache(ctx, key); cachedInvoice != nil {
		return cachedInvoice, nil
	}

	inv, err := r.client.Writer(ctx).Invoice.Query().
		Where(
			invoice.IdempotencyKeyEQ(key),
			invoice.EnvironmentID(types.GetEnvironmentID(ctx)),
			invoice.TenantID(types.GetTenantID(ctx)),
			invoice.StatusEQ(string(types.StatusPublished)),
			invoice.InvoiceStatusNEQ(types.InvoiceStatusVoided),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).WithHint("invoice not found").Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("failed to get invoice by idempotency key").Mark(ierr.ErrDatabase)
	}

	invoiceData := domainInvoice.FromEnt(inv)
	r.SetCache(ctx, invoiceData)
	return invoiceData, nil
}

func (r *invoiceRepository) ExistsForPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "exists_for_period", map[string]interface{}{
		"subscription_id": subscriptionID,
		"period_start":    periodStart,
		"period_end":      periodEnd,
	})
	defer FinishSpan(span)

	exists, err := r.client.Writer(ctx).Invoice.Query().
		Where(
			invoice.And(
				invoice.TenantID(types.GetTenantID(ctx)),
				invoice.EnvironmentID(types.GetEnvironmentID(ctx)),
				invoice.SubscriptionIDEQ(subscriptionID),
				invoice.PeriodStartEQ(periodStart),
				invoice.PeriodEndEQ(periodEnd),
				invoice.StatusEQ(string(types.StatusPublished)),
				invoice.InvoiceStatusNEQ(types.InvoiceStatusVoided),
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

func (r *invoiceRepository) getYearMonth(format types.InvoiceNumberFormat, timezone string) string {

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		// If timezone parsing fails, fall back to UTC
		loc = time.UTC
	}

	// Get current time in the specified timezone
	now := time.Now().In(loc)

	switch format {
	case types.InvoiceNumberFormatYYYYMM:
		return now.Format("200601")
	case types.InvoiceNumberFormatYYYY:
		return now.Format("2006")
	case types.InvoiceNumberFormatYYMMDD:
		return now.Format("060102")
	case types.InvoiceNumberFormatYYYYMMDD:
		return now.Format("20060102")
	case types.InvoiceNumberFormatYY:
		return now.Format("06")
	default:
		// Default to YYYYMM if format is not recognized
		return now.Format("200601")
	}
}

func (r *invoiceRepository) GetNextInvoiceNumber(ctx context.Context, invoiceConfig *types.InvoiceConfig) (string, error) {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "get_next_invoice_number", map[string]interface{}{})
	defer FinishSpan(span)

	yearMonth := r.getYearMonth(invoiceConfig.InvoiceNumberFormat, types.ResolveTimezone(invoiceConfig.InvoiceNumberTimezone))
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Use raw SQL for atomic increment since ent doesn't support RETURNING with OnConflict
	query := `
		INSERT INTO invoice_sequences (tenant_id,environment_id,year_month, last_value, created_at, updated_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (tenant_id, environment_id, year_month) DO UPDATE
		SET last_value = invoice_sequences.last_value + 1,
			updated_at = CURRENT_TIMESTAMP
		RETURNING last_value`

	var lastValue int64
	rows, err := r.client.Writer(ctx).QueryContext(ctx, query, tenantID, environmentID, yearMonth, invoiceConfig.InvoiceNumberStartSequence)
	if err != nil {
		return "", ierr.WithError(err).WithHint("invoice number generation failed").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", ierr.WithError(err).WithHint("failed to fetch sequence value").Mark(ierr.ErrDatabase)
		}
		return "", ierr.NewError("no sequence value returned").Mark(ierr.ErrInternal)
	}

	if err := rows.Scan(&lastValue); err != nil {
		return "", ierr.WithError(err).WithHint("invoice number generation failed").Mark(ierr.ErrDatabase)
	}

	r.logger.Infow("generated invoice number",
		"tenant_id", tenantID,
		"year_month", yearMonth,
		"sequence", lastValue)

	// Format the sequence number with the specified suffix length
	sequenceFormat := fmt.Sprintf("%%0%dd", invoiceConfig.InvoiceNumberSuffixLength)
	paddedSequence := fmt.Sprintf(sequenceFormat, lastValue)

	return fmt.Sprintf("%s%s%s%s%s", invoiceConfig.InvoiceNumberPrefix, invoiceConfig.InvoiceNumberSeparator, yearMonth, invoiceConfig.InvoiceNumberSeparator, paddedSequence), nil
}

func (r *invoiceRepository) GetNextBillingSequence(ctx context.Context, subscriptionID string) (int, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "invoice", "get_next_billing_sequence", map[string]interface{}{
		"subscription_id": subscriptionID,
	})
	defer FinishSpan(span)

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
	rows, err := r.client.Writer(ctx).QueryContext(ctx, query, tenantID, subscriptionID)
	if err != nil {
		return 0, ierr.WithError(err).WithHint("billing sequence generation failed").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, ierr.WithError(err).WithHint("failed to fetch billing sequence value").Mark(ierr.ErrDatabase)
		}
		return 0, ierr.NewError("no billing sequence value returned").Mark(ierr.ErrInternal)
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
	case "invoice_number":
		return invoice.FieldInvoiceNumber
	case "invoice_type":
		return invoice.FieldInvoiceType
	case "invoice_status":
		return invoice.FieldInvoiceStatus
	case "payment_status":
		return invoice.FieldPaymentStatus
	case "status":
		return invoice.FieldStatus
	case "amount_due":
		return invoice.FieldAmountDue
	case "amount_paid":
		return invoice.FieldAmountPaid
	case "amount_remaining":
		return invoice.FieldAmountRemaining
	case "adjustment_amount":
		return invoice.FieldAdjustmentAmount
	case "refunded_amount":
		return invoice.FieldRefundedAmount
	case "subtotal":
		return invoice.FieldSubtotal
	case "total":
		return invoice.FieldTotal
	case "currency":
		return invoice.FieldCurrency
	case "due_date":
		return invoice.FieldDueDate
	case "period_start":
		return invoice.FieldPeriodStart
	case "period_end":
		return invoice.FieldPeriodEnd
	case "billing_period":
		return invoice.FieldBillingPeriod
	case "paid_at":
		return invoice.FieldPaidAt
	case "voided_at":
		return invoice.FieldVoidedAt
	case "finalized_at":
		return invoice.FieldFinalizedAt
	case "created_at":
		return invoice.FieldCreatedAt
	case "updated_at":
		return invoice.FieldUpdatedAt
	case "idempotency_key":
		return invoice.FieldIdempotencyKey
	case "customer_id":
		return invoice.FieldCustomerID
	case "subscription_id":
		return invoice.FieldSubscriptionID
	case "description":
		return invoice.FieldDescription
	default:
		// unknown field
		return ""
	}
}

func (o InvoiceQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in invoice query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o InvoiceQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.InvoiceFilter, query InvoiceQuery) (InvoiceQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	// Apply entity-specific filters
	if f.CustomerID != "" {
		query = query.Where(invoice.CustomerID(f.CustomerID))
	}
	if f.SubscriptionID != "" {
		query = query.Where(invoice.SubscriptionID(f.SubscriptionID))
	}
	if f.InvoiceType != "" {
		query = query.Where(invoice.InvoiceType(f.InvoiceType))
	}
	if len(f.InvoiceIDs) > 0 {
		query = query.Where(invoice.IDIn(f.InvoiceIDs...))
	}
	if len(f.InvoiceStatus) > 0 {
		query = query.Where(invoice.InvoiceStatusIn(f.InvoiceStatus...))
	}
	if len(f.PaymentStatus) > 0 {
		query = query.Where(invoice.PaymentStatusIn(f.PaymentStatus...))
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

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[InvoiceQuery, predicate.Invoice](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Invoice { return predicate.Invoice(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[InvoiceQuery, invoice.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) invoice.OrderOption { return invoice.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

func (r *invoiceRepository) SetCache(ctx context.Context, inv *domainInvoice.Invoice) {
	span := cache.StartCacheSpan(ctx, "invoice", "set", map[string]interface{}{
		"invoice_id": inv.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixInvoice, tenantID, environmentID, inv.ID)
	r.cache.Set(ctx, cacheKey, inv, cache.ExpiryDefaultInMemory)

	idempotencyKey := cache.GenerateKey(cache.PrefixInvoice, tenantID, environmentID, inv.IdempotencyKey)
	r.cache.Set(ctx, idempotencyKey, inv, cache.ExpiryDefaultInMemory)

	r.logger.Debugw("set invoice in cache", "id", inv.ID, "cache_key", cacheKey)
}

func (r *invoiceRepository) GetCache(ctx context.Context, key string) *domainInvoice.Invoice {
	span := cache.StartCacheSpan(ctx, "invoice", "get", map[string]interface{}{
		"invoice_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixInvoice, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainInvoice.Invoice)
	}
	return nil
}

func (r *invoiceRepository) DeleteCache(ctx context.Context, key string) {
	span := cache.StartCacheSpan(ctx, "invoice", "delete", map[string]interface{}{
		"invoice_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixInvoice, tenantID, environmentID, key)
	r.cache.Delete(ctx, cacheKey)

	// get idempotency key
	invoice, err := r.Get(ctx, key)
	if err != nil {
		r.logger.Errorw("failed to get invoice by idempotency key", "error", err)
		return
	}
	idempotencyKey := cache.GenerateKey(cache.PrefixInvoice, tenantID, environmentID, invoice.IdempotencyKey)
	r.cache.Delete(ctx, idempotencyKey)
}

// GetInvoicesForExport retrieves invoices for export purposes with pagination
func (r *invoiceRepository) GetInvoicesForExport(ctx context.Context, tenantID, envID string, startTime, endTime time.Time, limit, offset int) ([]*domainInvoice.Invoice, error) {
	span := StartRepositorySpan(ctx, "invoice", "get_invoices_for_export", map[string]interface{}{
		"tenant_id":  tenantID,
		"env_id":     envID,
		"start_time": startTime,
		"end_time":   endTime,
		"limit":      limit,
		"offset":     offset,
	})
	defer FinishSpan(span)

	r.logger.Debugw("fetching invoices for export",
		"tenant_id", tenantID,
		"env_id", envID,
		"start_time", startTime,
		"end_time", endTime,
		"limit", limit,
		"offset", offset)

	invoices, err := r.client.Reader(ctx).Invoice.Query().
		Where(
			invoice.TenantID(tenantID),
			invoice.EnvironmentID(envID),
			invoice.StatusEQ(string(types.StatusPublished)),
			invoice.CreatedAtGTE(startTime),
			invoice.CreatedAtLTE(endTime),
		).
		Order(ent.Asc(invoice.FieldCreatedAt)).
		Limit(limit).
		Offset(offset).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch invoices for export").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": tenantID,
				"env_id":    envID,
			}).
			Mark(ierr.ErrDatabase)
	}

	result := make([]*domainInvoice.Invoice, len(invoices))
	for i, inv := range invoices {
		result[i] = domainInvoice.FromEnt(inv)
	}

	return result, nil
}

// Helper functions for type conversion
func convertStringPtrToInvoiceLineItemEntityTypePtr(s *string) *types.InvoiceLineItemEntityType {
	if s == nil {
		return nil
	}
	t := types.InvoiceLineItemEntityType(*s)
	return &t
}

func convertStringPtrToPriceTypePtr(s *string) *types.PriceType {
	if s == nil {
		return nil
	}
	t := types.PriceType(*s)
	return &t
}
