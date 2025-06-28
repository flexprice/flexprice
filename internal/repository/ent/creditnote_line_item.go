package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/creditnotelineitem"
	"github.com/flexprice/flexprice/internal/cache"
	domainCreditNote "github.com/flexprice/flexprice/internal/domain/creditnote"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type creditnoteLineItemRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CreditNoteLineItemQueryOptions
	cache     cache.Cache
}

// NewCreditNoteLineItemRepository creates a new credit note line item repository
func NewCreditNoteLineItemRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainCreditNote.CreditNoteLineItemRepository {
	return &creditnoteLineItemRepository{
		client:    client,
		log:       log,
		queryOpts: CreditNoteLineItemQueryOptions{},
		cache:     cache,
	}
}

// Create creates a new credit note line item
func (r *creditnoteLineItemRepository) Create(ctx context.Context, item *domainCreditNote.CreditNoteLineItem) error {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote_line_item", "create", map[string]interface{}{
		"credit_note_id":       item.CreditNoteID,
		"invoice_line_item_id": item.InvoiceLineItemID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if item.EnvironmentID == "" {
		item.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.CreditNoteLineItem.Create().
		SetID(item.ID).
		SetCreditNoteID(item.CreditNoteID).
		SetInvoiceLineItemID(item.InvoiceLineItemID).
		SetDisplayName(item.DisplayName).
		SetAmount(item.Amount).
		SetCurrency(item.Currency).
		SetMetadata(item.Metadata).
		SetTenantID(item.TenantID).
		SetEnvironmentID(item.EnvironmentID).
		SetStatus(string(item.Status)).
		SetCreatedBy(item.CreatedBy).
		SetUpdatedBy(item.UpdatedBy).
		SetCreatedAt(item.CreatedAt).
		SetUpdatedAt(item.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create credit note line item").
			WithReportableDetails(map[string]interface{}{
				"credit_note_id":       item.CreditNoteID,
				"invoice_line_item_id": item.InvoiceLineItemID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// Get retrieves a credit note line item by ID
func (r *creditnoteLineItemRepository) Get(ctx context.Context, id string) (*domainCreditNote.CreditNoteLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote_line_item", "get", map[string]interface{}{
		"line_item_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	if client == nil {
		err := ierr.NewError("failed to get database client").
			WithHint("Database client is not available").
			Mark(ierr.ErrDatabase)
		SetSpanError(span, err)
		return nil, err
	}

	item, err := client.CreditNoteLineItem.Query().
		Where(
			creditnotelineitem.ID(id),
			creditnotelineitem.TenantID(types.GetTenantID(ctx)),
			creditnotelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Credit note line item not found").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve credit note line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)

	// Convert to domain model
	lineItem := domainCreditNote.CreditNoteLineItem{}
	lineItem.FromEnt(item)

	return &lineItem, nil
}

// Update updates a credit note line item
func (r *creditnoteLineItemRepository) Update(ctx context.Context, item *domainCreditNote.CreditNoteLineItem) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote_line_item", "update", map[string]interface{}{
		"line_item_id": item.ID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	_, err := client.CreditNoteLineItem.UpdateOneID(item.ID).
		Where(
			creditnotelineitem.TenantID(types.GetTenantID(ctx)),
			creditnotelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetDisplayName(item.DisplayName).
		SetAmount(item.Amount).
		SetMetadata(item.Metadata).
		SetStatus(string(item.Status)).
		SetUpdatedBy(item.UpdatedBy).
		SetUpdatedAt(time.Now()).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Credit note line item not found").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": item.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update credit note line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": item.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// Delete deletes a credit note line item (soft delete)
func (r *creditnoteLineItemRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote_line_item", "delete", map[string]interface{}{
		"line_item_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	_, err := client.CreditNoteLineItem.Update().
		Where(
			creditnotelineitem.ID(id),
			creditnotelineitem.TenantID(types.GetTenantID(ctx)),
			creditnotelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now()).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to delete credit note line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// CreateBulk creates multiple credit note line items in bulk
func (r *creditnoteLineItemRepository) CreateBulk(ctx context.Context, items []*domainCreditNote.CreditNoteLineItem) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote_line_item", "create_bulk", map[string]interface{}{
		"items_count": len(items),
	})
	defer FinishSpan(span)

	if len(items) == 0 {
		return nil
	}

	client := r.client.Querier(ctx)
	builders := make([]*ent.CreditNoteLineItemCreate, len(items))

	for i, item := range items {
		// Set environment ID from context if not already set
		if item.EnvironmentID == "" {
			item.EnvironmentID = types.GetEnvironmentID(ctx)
		}

		builders[i] = client.CreditNoteLineItem.Create().
			SetID(item.ID).
			SetCreditNoteID(item.CreditNoteID).
			SetInvoiceLineItemID(item.InvoiceLineItemID).
			SetDisplayName(item.DisplayName).
			SetAmount(item.Amount).
			SetCurrency(item.Currency).
			SetMetadata(item.Metadata).
			SetTenantID(item.TenantID).
			SetEnvironmentID(item.EnvironmentID).
			SetStatus(string(item.Status)).
			SetCreatedBy(item.CreatedBy).
			SetUpdatedBy(item.UpdatedBy).
			SetCreatedAt(item.CreatedAt).
			SetUpdatedAt(item.UpdatedAt)
	}

	err := client.CreditNoteLineItem.CreateBulk(builders...).Exec(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create credit note line items in bulk").
			WithReportableDetails(map[string]interface{}{
				"items_count": len(items),
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// ListByCreditNote retrieves all line items for a specific credit note
func (r *creditnoteLineItemRepository) ListByCreditNote(ctx context.Context, creditNoteID string) ([]*domainCreditNote.CreditNoteLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote_line_item", "list_by_credit_note", map[string]interface{}{
		"credit_note_id": creditNoteID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	items, err := client.CreditNoteLineItem.Query().
		Where(
			creditnotelineitem.CreditNoteID(creditNoteID),
			creditnotelineitem.TenantID(types.GetTenantID(ctx)),
			creditnotelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
			creditnotelineitem.StatusNEQ(string(types.StatusDeleted)),
		).
		Order(ent.Asc(creditnotelineitem.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve credit note line items").
			WithReportableDetails(map[string]interface{}{
				"credit_note_id": creditNoteID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain models
	lineItem := &domainCreditNote.CreditNoteLineItem{}
	result := lineItem.FromEntList(items)

	SetSpanSuccess(span)
	return result, nil
}

// ListByInvoiceLineItem retrieves all credit note line items for a specific invoice line item
func (r *creditnoteLineItemRepository) ListByInvoiceLineItem(ctx context.Context, invoiceLineItemID string) ([]*domainCreditNote.CreditNoteLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote_line_item", "list_by_invoice_line_item", map[string]interface{}{
		"invoice_line_item_id": invoiceLineItemID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	items, err := client.CreditNoteLineItem.Query().
		Where(
			creditnotelineitem.InvoiceLineItemID(invoiceLineItemID),
			creditnotelineitem.TenantID(types.GetTenantID(ctx)),
			creditnotelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
			creditnotelineitem.StatusNEQ(string(types.StatusDeleted)),
		).
		Order(ent.Asc(creditnotelineitem.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve credit note line items").
			WithReportableDetails(map[string]interface{}{
				"invoice_line_item_id": invoiceLineItemID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain models
	lineItem := &domainCreditNote.CreditNoteLineItem{}
	result := lineItem.FromEntList(items)

	SetSpanSuccess(span)
	return result, nil
}

// List returns a paginated list of credit note line items based on the filter
func (r *creditnoteLineItemRepository) List(ctx context.Context, filter *types.CreditNoteLineItemFilter) ([]*domainCreditNote.CreditNoteLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote_line_item", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	query := client.CreditNoteLineItem.Query()

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	items, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("credit note line item listing failed").WithReportableDetails(
			map[string]any{
				"cause": err.Error(),
			},
		).Mark(ierr.ErrDatabase)
	}

	// Convert to domain models
	lineItem := &domainCreditNote.CreditNoteLineItem{}
	result := lineItem.FromEntList(items)

	SetSpanSuccess(span)
	return result, nil
}

// Count returns the total number of credit note line items based on the filter
func (r *creditnoteLineItemRepository) Count(ctx context.Context, filter *types.CreditNoteLineItemFilter) (int, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote_line_item", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	query := client.CreditNoteLineItem.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).WithHint("credit note line item counting failed").Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

// Query Options and Filters
type CreditNoteLineItemQuery = *ent.CreditNoteLineItemQuery

type CreditNoteLineItemQueryOptions struct{}

func (o CreditNoteLineItemQueryOptions) ApplyTenantFilter(ctx context.Context, query CreditNoteLineItemQuery) CreditNoteLineItemQuery {
	return query.Where(creditnotelineitem.TenantID(types.GetTenantID(ctx)))
}

func (o CreditNoteLineItemQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CreditNoteLineItemQuery) CreditNoteLineItemQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(creditnotelineitem.EnvironmentID(environmentID))
	}
	return query
}

func (o CreditNoteLineItemQueryOptions) ApplyStatusFilter(query CreditNoteLineItemQuery, status string) CreditNoteLineItemQuery {
	if status == "" {
		return query.Where(creditnotelineitem.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(creditnotelineitem.Status(status))
}

func (o CreditNoteLineItemQueryOptions) ApplySortFilter(query CreditNoteLineItemQuery, field string, order string) CreditNoteLineItemQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o CreditNoteLineItemQueryOptions) ApplyPaginationFilter(query CreditNoteLineItemQuery, limit int, offset int) CreditNoteLineItemQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o CreditNoteLineItemQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return creditnotelineitem.FieldCreatedAt
	case "updated_at":
		return creditnotelineitem.FieldUpdatedAt
	case "amount":
		return creditnotelineitem.FieldAmount
	case "display_name":
		return creditnotelineitem.FieldDisplayName
	case "currency":
		return creditnotelineitem.FieldCurrency
	default:
		return field
	}
}

func (o CreditNoteLineItemQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CreditNoteLineItemFilter, query CreditNoteLineItemQuery) CreditNoteLineItemQuery {
	if f == nil {
		return query
	}

	// Apply entity-specific filters
	if len(f.CreditNoteIDs) > 0 {
		query = query.Where(creditnotelineitem.CreditNoteIDIn(f.CreditNoteIDs...))
	}
	if len(f.InvoiceLineItemIDs) > 0 {
		query = query.Where(creditnotelineitem.InvoiceLineItemIDIn(f.InvoiceLineItemIDs...))
	}

	// Apply time range filters
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(creditnotelineitem.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(creditnotelineitem.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	return query
}
