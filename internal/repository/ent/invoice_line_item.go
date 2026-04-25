package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/invoicelineitem"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/cache"
	domaininvoice "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"strings"
)

const invoiceLineItemBatchSize = 1000

type invoiceLineItemRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	cache     cache.Cache
	queryOpts InvoiceLineItemQueryOptions
}

// NewInvoiceLineItemRepository creates a new invoice line item repository.
func NewInvoiceLineItemRepository(
	client postgres.IClient,
	log *logger.Logger,
	c cache.Cache,
) domaininvoice.LineItemRepository {
	return &invoiceLineItemRepository{client: client, log: log, cache: c, queryOpts: InvoiceLineItemQueryOptions{}}
}

// Cache helpers

func (r *invoiceLineItemRepository) SetCache(ctx context.Context, item *domaininvoice.InvoiceLineItem) {
	span := cache.StartCacheSpan(ctx, "invoice_line_item", "set", map[string]interface{}{
		"line_item_id": item.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixInvoiceLineItem, tenantID, environmentID, item.ID)
	r.cache.Set(ctx, cacheKey, item, cache.ExpiryDefaultInMemory)
}

func (r *invoiceLineItemRepository) GetCache(ctx context.Context, id string) *domaininvoice.InvoiceLineItem {
	span := cache.StartCacheSpan(ctx, "invoice_line_item", "get", map[string]interface{}{
		"line_item_id": id,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixInvoiceLineItem, tenantID, environmentID, id)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if item, ok := value.(*domaininvoice.InvoiceLineItem); ok {
			return item
		}
	}
	return nil
}

func (r *invoiceLineItemRepository) DeleteCache(ctx context.Context, id string) {
	span := cache.StartCacheSpan(ctx, "invoice_line_item", "delete", map[string]interface{}{
		"line_item_id": id,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixInvoiceLineItem, tenantID, environmentID, id)
	r.cache.Delete(ctx, cacheKey)
}

// Create creates a single invoice line item.
func (r *invoiceLineItemRepository) Create(ctx context.Context, item *domaininvoice.InvoiceLineItem) error {
	span := StartRepositorySpan(ctx, "invoice_line_item", "create", map[string]interface{}{
		"line_item_id": item.ID,
		"invoice_id":   item.InvoiceID,
	})
	defer FinishSpan(span)

	r.log.Debugw("creating invoice line item",
		"line_item_id", item.ID,
		"invoice_id", item.InvoiceID,
	)

	if item.EnvironmentID == "" {
		item.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := r.client.Writer(ctx).InvoiceLineItem.Create().
		SetID(item.ID).
		SetTenantID(types.GetTenantID(ctx)).
		SetInvoiceID(item.InvoiceID).
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
		SetPrepaidCreditsApplied(item.PrepaidCreditsApplied).
		SetLineItemDiscount(item.LineItemDiscount).
		SetInvoiceLevelDiscount(item.InvoiceLevelDiscount).
		SetStatus(string(item.Status)).
		SetCreatedBy(item.CreatedBy).
		SetUpdatedBy(item.UpdatedBy).
		SetCreatedAt(item.CreatedAt).
		SetUpdatedAt(item.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHintf("invoice line item with ID %s already exists", item.ID).
				WithReportableDetails(map[string]interface{}{
					"line_item_id": item.ID,
					"invoice_id":   item.InvoiceID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("invoice line item creation failed").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": item.ID,
				"invoice_id":   item.InvoiceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// CreateBulk creates multiple invoice line items, batching to avoid PostgreSQL's parameter limit.
func (r *invoiceLineItemRepository) CreateBulk(ctx context.Context, items []*domaininvoice.InvoiceLineItem) error {
	if len(items) == 0 {
		return nil
	}

	span := StartRepositorySpan(ctx, "invoice_line_item", "create_bulk", map[string]interface{}{
		"item_count": len(items),
	})
	defer FinishSpan(span)

	r.log.Debugw("creating invoice line items in bulk",
		"item_count", len(items),
		"tenant_id", types.GetTenantID(ctx),
	)

	err := r.client.WithTx(ctx, func(ctx context.Context) error {
		client := r.client.Writer(ctx)

		bulk := make([]*ent.InvoiceLineItemCreate, len(items))
		for i, item := range items {
			if item.EnvironmentID == "" {
				item.EnvironmentID = types.GetEnvironmentID(ctx)
			}

			bulk[i] = client.InvoiceLineItem.Create().
				SetID(item.ID).
				SetTenantID(types.GetTenantID(ctx)).
				SetInvoiceID(item.InvoiceID).
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
				SetPrepaidCreditsApplied(item.PrepaidCreditsApplied).
				SetLineItemDiscount(item.LineItemDiscount).
				SetInvoiceLevelDiscount(item.InvoiceLevelDiscount).
				SetStatus(string(item.Status)).
				SetCreatedBy(item.CreatedBy).
				SetUpdatedBy(item.UpdatedBy).
				SetCreatedAt(item.CreatedAt).
				SetUpdatedAt(item.UpdatedAt)
		}

		for i := 0; i < len(bulk); i += invoiceLineItemBatchSize {
			end := i + invoiceLineItemBatchSize
			if end > len(bulk) {
				end = len(bulk)
			}
			if _, err := client.InvoiceLineItem.CreateBulk(bulk[i:end]...).Save(ctx); err != nil {
				return ierr.WithError(err).
					WithHint("failed to create invoice line items in bulk").
					WithReportableDetails(map[string]interface{}{
						"count":       len(items),
						"batch_start": i,
						"batch_end":   end,
					}).
					Mark(ierr.ErrDatabase)
			}
		}
		return nil
	})

	if err != nil {
		SetSpanError(span, err)
		return err
	}

	SetSpanSuccess(span)
	return nil
}

// Get retrieves a single invoice line item by ID (tenant-scoped).
func (r *invoiceLineItemRepository) Get(ctx context.Context, id string) (*domaininvoice.InvoiceLineItem, error) {
	span := StartRepositorySpan(ctx, "invoice_line_item", "get", map[string]interface{}{
		"line_item_id": id,
		"tenant_id":    types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if cached := r.GetCache(ctx, id); cached != nil {
		SetSpanSuccess(span)
		return cached, nil
	}

	r.log.Debugw("getting invoice line item",
		"line_item_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	item, err := r.client.Reader(ctx).InvoiceLineItem.Query().
		Where(
			invoicelineitem.ID(id),
			invoicelineitem.TenantID(types.GetTenantID(ctx)),
			invoicelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("invoice line item %s not found", id).
				WithReportableDetails(map[string]interface{}{
					"line_item_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("getting invoice line item failed").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	result := domaininvoice.LineItemFromEnt(item)
	r.SetCache(ctx, result)
	SetSpanSuccess(span)
	return result, nil
}

// Update updates mutable fields on a line item.
func (r *invoiceLineItemRepository) Update(ctx context.Context, item *domaininvoice.InvoiceLineItem) error {
	span := StartRepositorySpan(ctx, "invoice_line_item", "update", map[string]interface{}{
		"line_item_id": item.ID,
	})
	defer FinishSpan(span)

	r.log.Debugw("updating invoice line item", "line_item_id", item.ID)

	_, err := r.client.Writer(ctx).InvoiceLineItem.UpdateOneID(item.ID).
		Where(
			invoicelineitem.TenantID(types.GetTenantID(ctx)),
			invoicelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetPrepaidCreditsApplied(item.PrepaidCreditsApplied).
		SetLineItemDiscount(item.LineItemDiscount).
		SetInvoiceLevelDiscount(item.InvoiceLevelDiscount).
		SetMetadata(item.Metadata).
		SetStatus(string(item.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("invoice line item not found").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": item.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("failed to update invoice line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": item.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, item.ID)
	SetSpanSuccess(span)
	return nil
}

// Delete soft-deletes an invoice line item by setting its status to deleted.
func (r *invoiceLineItemRepository) Delete(ctx context.Context, id string) error {
	span := StartRepositorySpan(ctx, "invoice_line_item", "delete", map[string]interface{}{
		"line_item_id": id,
		"tenant_id":    types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	r.log.Debugw("deleting invoice line item",
		"line_item_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := r.client.Writer(ctx).InvoiceLineItem.UpdateOneID(id).
		Where(
			invoicelineitem.TenantID(types.GetTenantID(ctx)),
			invoicelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("invoice line item %s not found", id).
				WithReportableDetails(map[string]interface{}{
					"line_item_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("failed to delete invoice line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, id)
	SetSpanSuccess(span)
	return nil
}

// ListByInvoiceID retrieves all published line items for the given invoice.
func (r *invoiceLineItemRepository) ListByInvoiceID(ctx context.Context, invoiceID string) ([]*domaininvoice.InvoiceLineItem, error) {
	span := StartRepositorySpan(ctx, "invoice_line_item", "list_by_invoice", map[string]interface{}{
		"invoice_id": invoiceID,
	})
	defer FinishSpan(span)

	r.log.Debugw("listing invoice line items by invoice",
		"invoice_id", invoiceID,
		"tenant_id", types.GetTenantID(ctx),
	)

	items, err := r.client.Reader(ctx).InvoiceLineItem.Query().
		Where(
			invoicelineitem.TenantID(types.GetTenantID(ctx)),
			invoicelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
			invoicelineitem.InvoiceID(invoiceID),
			invoicelineitem.Status(string(types.StatusPublished)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("listing invoice line items failed").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": invoiceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	result := make([]*domaininvoice.InvoiceLineItem, len(items))
	for i, item := range items {
		result[i] = domaininvoice.LineItemFromEnt(item)
	}

	SetSpanSuccess(span)
	return result, nil
}

// InvoiceLineItemQuery type alias for better readability.
type InvoiceLineItemQuery = *ent.InvoiceLineItemQuery

// InvoiceLineItemQueryOptions implements BaseQueryOptions for invoice line item queries.
type InvoiceLineItemQueryOptions struct{}

func (o InvoiceLineItemQueryOptions) ApplyTenantFilter(ctx context.Context, query InvoiceLineItemQuery) InvoiceLineItemQuery {
	return query.Where(invoicelineitem.TenantID(types.GetTenantID(ctx)))
}

func (o InvoiceLineItemQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query InvoiceLineItemQuery) InvoiceLineItemQuery {
	return query.Where(invoicelineitem.EnvironmentID(types.GetEnvironmentID(ctx)))
}

func (o InvoiceLineItemQueryOptions) ApplyStatusFilter(query InvoiceLineItemQuery, status string) InvoiceLineItemQuery {
	if status != "" {
		return query.Where(invoicelineitem.Status(status))
	}
	return query
}

func (o InvoiceLineItemQueryOptions) ApplySortFilter(query InvoiceLineItemQuery, field string, order string) InvoiceLineItemQuery {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return query
	}
	if order == types.OrderDesc {
		return query.Order(ent.Desc(fieldName))
	}
	return query.Order(ent.Asc(fieldName))
}

func (o InvoiceLineItemQueryOptions) ApplyPaginationFilter(query InvoiceLineItemQuery, limit int, offset int) InvoiceLineItemQuery {
	return query.Limit(limit).Offset(offset)
}

// GetFieldName returns the ent field name for invoice_line_item; delegates to ent's ValidColumn so new schema fields are supported automatically.
func (o InvoiceLineItemQueryOptions) GetFieldName(field string) string {
	if invoicelineitem.ValidColumn(field) {
		return field
	}
	return ""
}

// GetFieldResolver resolves a logical field name to an ent column name for DSL filters/sorts.
func (o InvoiceLineItemQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field '%s' in invoice line item query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

// applyEntityQueryOptions applies invoice line item-specific filters to the query.
func (o *InvoiceLineItemQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.InvoiceLineItemFilter, query InvoiceLineItemQuery) (InvoiceLineItemQuery, error) {
	if len(f.InvoiceIDs) > 0 {
		query = query.Where(invoicelineitem.InvoiceIDIn(f.InvoiceIDs...))
	}
	if len(f.CustomerIDs) > 0 {
		query = query.Where(invoicelineitem.CustomerIDIn(f.CustomerIDs...))
	}
	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(invoicelineitem.SubscriptionIDIn(f.SubscriptionIDs...))
	}
	if len(f.PriceIDs) > 0 {
		query = query.Where(invoicelineitem.PriceIDIn(f.PriceIDs...))
	}
	if len(f.MeterIDs) > 0 {
		query = query.Where(invoicelineitem.MeterIDIn(f.MeterIDs...))
	}
	if len(f.Currencies) > 0 {
		query = query.Where(invoicelineitem.CurrencyIn(f.Currencies...))
	}
	if len(f.EntityIDs) > 0 {
		query = query.Where(invoicelineitem.EntityIDIn(f.EntityIDs...))
	}
	if f.EntityType != nil {
		query = query.Where(invoicelineitem.EntityType(types.InvoiceLineItemEntityType(*f.EntityType)))
	}
	if f.PeriodStart != nil {
		query = query.Where(invoicelineitem.PeriodStartGTE(*f.PeriodStart))
	}
	if f.PeriodEnd != nil {
		query = query.Where(invoicelineitem.PeriodEndLTE(*f.PeriodEnd))
	}

	// DSL-based complex filters
	if f.DSLFilter != nil && len(f.Filters) > 0 {
		var err error
		query, err = dsl.ApplyFilters[InvoiceLineItemQuery, predicate.InvoiceLineItem](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.InvoiceLineItem { return predicate.InvoiceLineItem(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// DSL-based sorts
	if f.DSLFilter != nil && len(f.DSLFilter.Sort) > 0 {
		var err error
		query, err = dsl.ApplySorts[InvoiceLineItemQuery, invoicelineitem.OrderOption](
			query,
			f.DSLFilter.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) invoicelineitem.OrderOption { return invoicelineitem.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

// List retrieves invoice line items matching the filter.
func (r *invoiceLineItemRepository) List(ctx context.Context, filter *types.InvoiceLineItemFilter) ([]*domaininvoice.InvoiceLineItem, error) {
	if filter == nil {
		filter = types.NewDefaultInvoiceLineItemFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).WithHint("Invalid filter parameters").Mark(ierr.ErrValidation)
	}

	span := StartRepositorySpan(ctx, "invoice_line_item", "list", map[string]interface{}{
		"invoice_ids":      filter.InvoiceIDs,
		"subscription_ids": filter.SubscriptionIDs,
	})
	defer FinishSpan(span)

	query := r.client.Reader(ctx).InvoiceLineItem.Query()

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	items, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("listing invoice line items failed").Mark(ierr.ErrDatabase)
	}

	result := make([]*domaininvoice.InvoiceLineItem, len(items))
	for i, item := range items {
		result[i] = domaininvoice.LineItemFromEnt(item)
	}
	SetSpanSuccess(span)
	return result, nil
}

// GetRevenueByCustomer aggregates invoice line item amounts grouped by customer_id
// and price_type for DRAFT/FINALIZED invoices within the given period.
func (r *invoiceLineItemRepository) GetRevenueByCustomer(
	ctx context.Context,
	periodStart, periodEnd time.Time,
	customerIDs []string,
) ([]domaininvoice.RevenueByCustomerRow, error) {
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "invoice_line_item", "get_revenue_by_customer", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": envID,
		"period_start":   periodStart,
		"period_end":     periodEnd,
		"customer_count": len(customerIDs),
	})
	defer FinishSpan(span)

	// Build the query with optional customer filter
	customerFilter := ""
	args := []interface{}{tenantID, envID, periodStart, periodEnd}

	if len(customerIDs) > 0 {
		placeholders := make([]string, len(customerIDs))
		for i, id := range customerIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+5)
			args = append(args, id)
		}
		customerFilter = " AND ili.customer_id IN (" + strings.Join(placeholders, ", ") + ")"
	}

	query := fmt.Sprintf(`
		SELECT
			ili.customer_id,
			ili.price_type,
			COALESCE(SUM(ili.amount), 0)::text AS amount
		FROM invoice_line_items ili
		INNER JOIN invoices inv
			ON inv.id = ili.invoice_id
			AND inv.invoice_status IN ('DRAFT', 'FINALIZED')
			AND inv.status = 'published'
		WHERE ili.period_start >= $3
			AND ili.period_end < $4
			AND ili.status = 'published'
			AND ili.tenant_id = $1
			AND ili.environment_id = $2
			%s
		GROUP BY ili.customer_id, ili.price_type
	`, customerFilter)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("failed to get revenue by customer").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []domaininvoice.RevenueByCustomerRow
	for rows.Next() {
		var row domaininvoice.RevenueByCustomerRow
		var amountStr string
		if err := rows.Scan(&row.CustomerID, &row.PriceType, &amountStr); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("failed to scan revenue row").
				Mark(ierr.ErrDatabase)
		}
		row.Amount, err = decimal.NewFromString(amountStr)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("failed to parse revenue amount").
				Mark(ierr.ErrDatabase)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("failed to iterate revenue rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return results, nil
}

// GetVoiceMinutesByCustomer aggregates invoice line item quantity (in milliseconds)
// grouped by customer_id for a specific meter within the given period.
func (r *invoiceLineItemRepository) GetVoiceMinutesByCustomer(
	ctx context.Context,
	periodStart, periodEnd time.Time,
	meterID string,
	customerIDs []string,
) ([]domaininvoice.VoiceMinutesRow, error) {
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "invoice_line_item", "get_voice_minutes_by_customer", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": envID,
		"period_start":   periodStart,
		"period_end":     periodEnd,
		"meter_id":       meterID,
		"customer_count": len(customerIDs),
	})
	defer FinishSpan(span)

	// Build the query with optional customer filter
	customerFilter := ""
	args := []interface{}{tenantID, envID, periodStart, periodEnd, meterID}

	if len(customerIDs) > 0 {
		placeholders := make([]string, len(customerIDs))
		for i, id := range customerIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+6)
			args = append(args, id)
		}
		customerFilter = " AND ili.customer_id IN (" + strings.Join(placeholders, ", ") + ")"
	}

	query := fmt.Sprintf(`
		SELECT
			ili.customer_id,
			COALESCE(SUM(ili.quantity), 0)::text AS usage_ms
		FROM invoice_line_items ili
		INNER JOIN invoices inv
			ON inv.id = ili.invoice_id
			AND inv.invoice_status IN ('DRAFT', 'FINALIZED')
			AND inv.status = 'published'
		WHERE ili.period_start >= $3
			AND ili.period_end < $4
			AND ili.status = 'published'
			AND ili.meter_id = $5
			AND ili.tenant_id = $1
			AND ili.environment_id = $2
			%s
		GROUP BY ili.customer_id
	`, customerFilter)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("failed to get voice minutes by customer").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []domaininvoice.VoiceMinutesRow
	for rows.Next() {
		var row domaininvoice.VoiceMinutesRow
		var usageMsStr string
		if err := rows.Scan(&row.CustomerID, &usageMsStr); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("failed to scan voice minutes row").
				Mark(ierr.ErrDatabase)
		}
		row.UsageMs, err = decimal.NewFromString(usageMsStr)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("failed to parse voice minutes quantity").
				Mark(ierr.ErrDatabase)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("failed to iterate voice minutes rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return results, nil
}

func validateRevenueGraphDateTruncPart(part string) error {
	if part == "day" || part == "month" {
		return nil
	}
	return ierr.NewError("invalid date_trunc granularity").
		WithHint("date_trunc part must be 'day' or 'month'").
		WithReportableDetails(map[string]interface{}{"date_trunc_part": part}).
		Mark(ierr.ErrValidation)
}

// GetRevenueTimeSeries aggregates invoice line item amounts by calendar bucket and price_type.
func (r *invoiceLineItemRepository) GetRevenueTimeSeries(
	ctx context.Context,
	periodStart, periodEnd time.Time,
	dateTruncPart string,
	customerIDs []string,
) ([]domaininvoice.RevenueTimeSeriesRow, error) {
	if err := validateRevenueGraphDateTruncPart(dateTruncPart); err != nil {
		return nil, err
	}

	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "invoice_line_item", "get_revenue_time_series", map[string]interface{}{
		"tenant_id":       tenantID,
		"environment_id":  envID,
		"period_start":    periodStart,
		"period_end":      periodEnd,
		"date_trunc_part": dateTruncPart,
		"customer_count":  len(customerIDs),
	})
	defer FinishSpan(span)

	tzTruncExpr := "date_trunc($1::text, ili.period_start AT TIME ZONE 'UTC')"
	customerFilter := ""
	args := []interface{}{dateTruncPart, tenantID, envID, periodStart, periodEnd}

	if len(customerIDs) > 0 {
		placeholders := make([]string, len(customerIDs))
		for i, id := range customerIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+6)
			args = append(args, id)
		}
		customerFilter = " AND ili.customer_id IN (" + strings.Join(placeholders, ", ") + ")"
	}

	query := fmt.Sprintf(`
		SELECT
			%s AS window_start,
			ili.price_type,
			COALESCE(SUM(ili.amount), 0)::text AS amount
		FROM invoice_line_items ili
		INNER JOIN invoices inv
			ON inv.id = ili.invoice_id
			AND inv.invoice_status IN ('DRAFT', 'FINALIZED')
			AND inv.status = 'published'
		WHERE ili.period_start >= $4
			AND ili.period_end < $5
			AND ili.status = 'published'
			AND ili.tenant_id = $2
			AND ili.environment_id = $3
			%s
		GROUP BY %s, ili.price_type
		ORDER BY window_start ASC
	`, tzTruncExpr, customerFilter, tzTruncExpr)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("failed to get revenue time series").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []domaininvoice.RevenueTimeSeriesRow
	for rows.Next() {
		var row domaininvoice.RevenueTimeSeriesRow
		var amountStr string
		if err := rows.Scan(&row.WindowStart, &row.PriceType, &amountStr); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("failed to scan revenue time series row").
				Mark(ierr.ErrDatabase)
		}
		row.Amount, err = decimal.NewFromString(amountStr)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("failed to parse revenue time series amount").
				Mark(ierr.ErrDatabase)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("failed to iterate revenue time series rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return results, nil
}

// GetVoiceMinutesTimeSeries aggregates voice meter quantity (ms) by calendar bucket.
func (r *invoiceLineItemRepository) GetVoiceMinutesTimeSeries(
	ctx context.Context,
	periodStart, periodEnd time.Time,
	meterID, dateTruncPart string,
	customerIDs []string,
) ([]domaininvoice.VoiceMinutesTimeSeriesRow, error) {
	if err := validateRevenueGraphDateTruncPart(dateTruncPart); err != nil {
		return nil, err
	}

	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "invoice_line_item", "get_voice_minutes_time_series", map[string]interface{}{
		"tenant_id":       tenantID,
		"environment_id":  envID,
		"period_start":    periodStart,
		"period_end":      periodEnd,
		"meter_id":        meterID,
		"date_trunc_part": dateTruncPart,
		"customer_count":  len(customerIDs),
	})
	defer FinishSpan(span)

	tzTruncExpr := "date_trunc($1::text, ili.period_start AT TIME ZONE 'UTC')"
	customerFilter := ""
	args := []interface{}{dateTruncPart, tenantID, envID, periodStart, periodEnd, meterID}

	if len(customerIDs) > 0 {
		placeholders := make([]string, len(customerIDs))
		for i, id := range customerIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+7)
			args = append(args, id)
		}
		customerFilter = " AND ili.customer_id IN (" + strings.Join(placeholders, ", ") + ")"
	}

	query := fmt.Sprintf(`
		SELECT
			%s AS window_start,
			COALESCE(SUM(ili.quantity), 0)::text AS usage_ms
		FROM invoice_line_items ili
		INNER JOIN invoices inv
			ON inv.id = ili.invoice_id
			AND inv.invoice_status IN ('DRAFT', 'FINALIZED')
			AND inv.status = 'published'
		WHERE ili.period_start >= $4
			AND ili.period_end < $5
			AND ili.status = 'published'
			AND ili.meter_id = $6
			AND ili.tenant_id = $2
			AND ili.environment_id = $3
			%s
		GROUP BY %s
		ORDER BY window_start ASC
	`, tzTruncExpr, customerFilter, tzTruncExpr)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("failed to get voice minutes time series").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []domaininvoice.VoiceMinutesTimeSeriesRow
	for rows.Next() {
		var row domaininvoice.VoiceMinutesTimeSeriesRow
		var usageMsStr string
		if err := rows.Scan(&row.WindowStart, &usageMsStr); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("failed to scan voice minutes time series row").
				Mark(ierr.ErrDatabase)
		}
		row.UsageMs, err = decimal.NewFromString(usageMsStr)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("failed to parse voice minutes time series quantity").
				Mark(ierr.ErrDatabase)
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("failed to iterate voice minutes time series rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return results, nil
}
