package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/creditnote"
	"github.com/flexprice/flexprice/ent/creditnotelineitem"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/cache"
	domainCreditNote "github.com/flexprice/flexprice/internal/domain/creditnote"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/samber/lo"
)

type creditnoteRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CreditNoteQueryOptions
	cache     cache.Cache
}

func NewCreditNoteRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainCreditNote.Repository {
	return &creditnoteRepository{
		client:    client,
		log:       log,
		queryOpts: CreditNoteQueryOptions{},
		cache:     cache,
	}
}

// Create creates a new credit note (non-transactional)
func (r *creditnoteRepository) Create(ctx context.Context, cn *domainCreditNote.CreditNote) error {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "create", map[string]interface{}{
		"creditnote_id": cn.ID,
		"invoice_id":    cn.InvoiceID,
		"tenant_id":     cn.TenantID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if cn.EnvironmentID == "" {
		cn.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.CreditNote.Create().
		SetID(cn.ID).
		SetTenantID(cn.TenantID).
		SetInvoiceID(cn.InvoiceID).
		SetCreditNoteNumber(cn.CreditNoteNumber).
		SetCreditNoteStatus(cn.CreditNoteStatus).
		SetCreditNoteType(cn.CreditNoteType).
		SetNillableRefundStatus(cn.RefundStatus).
		SetReason(cn.Reason).
		SetMemo(cn.Memo).
		SetCurrency(cn.Currency).
		SetMetadata(cn.Metadata).
		SetStatus(string(cn.Status)).
		SetCreatedAt(cn.CreatedAt).
		SetUpdatedAt(cn.UpdatedAt).
		SetCreatedBy(cn.CreatedBy).
		SetUpdatedBy(cn.UpdatedBy).
		SetNillableVoidedAt(cn.VoidedAt).
		SetNillableFinalizedAt(cn.FinalizedAt).
		SetCustomerID(cn.CustomerID).
		SetSubscriptionID(lo.FromPtr(cn.SubscriptionID)).
		SetEnvironmentID(cn.EnvironmentID).
		SetTotalAmount(cn.TotalAmount).
		SetIdempotencyKey(lo.FromPtr(cn.IdempotencyKey)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		r.log.Error("failed to create credit note", "error", err)
		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Idx_tenant_environment_credit_note_number_unique {
					return ierr.WithError(err).
						WithHint("Credit note with same credit note number already exists").
						WithReportableDetails(map[string]any{
							"creditnote_id":     cn.ID,
							"creditnote_number": cn.CreditNoteNumber,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}

			return ierr.WithError(err).
				WithHint("Credit note with same credit note number already exists").
				WithReportableDetails(map[string]any{
					"creditnote_id": cn.ID,
				}).
				Mark(ierr.ErrDatabase)
		}
		return ierr.WithError(err).
			WithHint("credit note creation failed").
			WithReportableDetails(map[string]any{
				"creditnote_id": cn.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// CreateWithLineItems creates a credit note with its line items in a single transaction
func (r *creditnoteRepository) CreateWithLineItems(ctx context.Context, cn *domainCreditNote.CreditNote) error {
	r.log.Debugw("creating credit note with line items",
		"id", cn.ID,
		"line_items_count", len(cn.LineItems))

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "create_with_line_items", map[string]interface{}{
		"creditnote_id":    cn.ID,
		"invoice_id":       cn.InvoiceID,
		"line_items_count": len(cn.LineItems),
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if cn.EnvironmentID == "" {
		cn.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := r.client.WithTx(ctx, func(ctx context.Context) error {
		// 1. Create credit note
		creditNote, err := r.client.Querier(ctx).CreditNote.Create().
			SetID(cn.ID).
			SetTenantID(cn.TenantID).
			SetInvoiceID(cn.InvoiceID).
			SetCreditNoteNumber(cn.CreditNoteNumber).
			SetCreditNoteStatus(cn.CreditNoteStatus).
			SetCreditNoteType(cn.CreditNoteType).
			SetNillableRefundStatus(cn.RefundStatus).
			SetReason(cn.Reason).
			SetMemo(cn.Memo).
			SetCurrency(cn.Currency).
			SetMetadata(cn.Metadata).
			SetStatus(string(cn.Status)).
			SetCreatedAt(cn.CreatedAt).
			SetUpdatedAt(cn.UpdatedAt).
					SetCreatedBy(cn.CreatedBy).
		SetCustomerID(cn.CustomerID).
		SetNillableSubscriptionID(cn.SubscriptionID).
		SetNillableVoidedAt(cn.VoidedAt).
			SetNillableFinalizedAt(cn.FinalizedAt).
			SetUpdatedBy(cn.UpdatedBy).
			SetTotalAmount(cn.TotalAmount).
			SetEnvironmentID(cn.EnvironmentID).
			SetIdempotencyKey(lo.FromPtr(cn.IdempotencyKey)).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				var pqErr *pq.Error
				if errors.As(err, &pqErr) {
					if pqErr.Constraint == schema.Idx_tenant_environment_credit_note_number_unique {
						return ierr.WithError(err).
							WithHint("Credit note with same credit note number already exists").
							WithReportableDetails(map[string]any{
								"creditnote_id":     cn.ID,
								"creditnote_number": cn.CreditNoteNumber,
							}).
							Mark(ierr.ErrAlreadyExists)
					}
				}
				return ierr.WithError(err).
					WithHint("Credit note with same credit note number already exists").
					WithReportableDetails(map[string]any{
						"creditnote_id": cn.ID,
					}).
					Mark(ierr.ErrAlreadyExists)
			}
			r.log.Errorw("failed to create credit note", "error", err, "creditnote_id", cn.ID)
			return ierr.WithError(err).
				WithHint("credit note creation failed").
				WithReportableDetails(map[string]any{
					"creditnote_id": cn.ID,
				}).
				Mark(ierr.ErrDatabase)
		}

		// 2. Create line items in bulk if present
		if len(cn.LineItems) > 0 {
			builders := make([]*ent.CreditNoteLineItemCreate, len(cn.LineItems))

			for i, item := range cn.LineItems {
				builders[i] = r.client.Querier(ctx).CreditNoteLineItem.Create().
					SetID(item.ID).
					SetTenantID(item.TenantID).
					SetCreditNoteID(creditNote.ID).
					SetInvoiceLineItemID(item.InvoiceLineItemID).
					SetDisplayName(item.DisplayName).
					SetAmount(item.Amount).
					SetCurrency(item.Currency).
					SetMetadata(item.Metadata).
					SetEnvironmentID(item.EnvironmentID).
					SetStatus(string(item.Status)).
					SetCreatedBy(item.CreatedBy).
					SetUpdatedBy(item.UpdatedBy).
					SetCreatedAt(item.CreatedAt).
					SetUpdatedAt(item.UpdatedAt)
			}

			if err := r.client.Querier(ctx).CreditNoteLineItem.CreateBulk(builders...).Exec(ctx); err != nil {
				r.log.Errorw("failed to create line items", "error", err, "creditnote_id", cn.ID)
				return ierr.WithError(err).WithHint("line item creation failed").Mark(ierr.ErrDatabase)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// AddLineItems adds line items to an existing credit note
func (r *creditnoteRepository) AddLineItems(ctx context.Context, creditNoteID string, items []*domainCreditNote.CreditNoteLineItem) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "add_line_items", map[string]interface{}{
		"creditnote_id": creditNoteID,
		"items_count":   len(items),
	})
	defer FinishSpan(span)

	r.log.Debugw("adding line items", "creditnote_id", creditNoteID, "count", len(items))

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Verify credit note exists
		exists, err := r.client.Querier(ctx).CreditNote.Query().Where(creditnote.ID(creditNoteID)).Exist(ctx)
		if err != nil {
			return ierr.WithError(err).WithHint("credit note existence check failed").Mark(ierr.ErrDatabase)
		}
		if !exists {
			return ierr.WithError(err).WithHintf("credit note %s not found", creditNoteID).Mark(ierr.ErrNotFound)
		}

		builders := make([]*ent.CreditNoteLineItemCreate, len(items))
		for i, item := range items {
			builders[i] = r.client.Querier(ctx).CreditNoteLineItem.Create().
				SetID(item.ID).
				SetTenantID(item.TenantID).
				SetEnvironmentID(item.EnvironmentID).
				SetCreditNoteID(creditNoteID).
				SetInvoiceLineItemID(item.InvoiceLineItemID).
				SetDisplayName(item.DisplayName).
				SetAmount(item.Amount).
				SetCurrency(item.Currency).
				SetMetadata(item.Metadata).
				SetStatus(string(item.Status)).
				SetCreatedBy(item.CreatedBy).
				SetUpdatedBy(item.UpdatedBy).
				SetCreatedAt(item.CreatedAt).
				SetUpdatedAt(item.UpdatedAt)
		}

		if err := r.client.Querier(ctx).CreditNoteLineItem.CreateBulk(builders...).Exec(ctx); err != nil {
			r.log.Errorw("failed to add line items", "error", err, "creditnote_id", creditNoteID)
			return ierr.WithError(err).WithHint("line item addition failed").Mark(ierr.ErrDatabase)
		}

		return nil
	})
}

// RemoveLineItems removes line items from a credit note
func (r *creditnoteRepository) RemoveLineItems(ctx context.Context, creditNoteID string, itemIDs []string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "remove_line_items", map[string]interface{}{
		"creditnote_id": creditNoteID,
		"items_count":   len(itemIDs),
	})
	defer FinishSpan(span)

	r.log.Debugw("removing line items", "creditnote_id", creditNoteID, "items_count", len(itemIDs))

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Verify credit note exists
		exists, err := r.client.Querier(ctx).CreditNote.Query().Where(creditnote.ID(creditNoteID)).Exist(ctx)
		if err != nil {
			return ierr.WithError(err).WithHint("credit note existence check failed").Mark(ierr.ErrDatabase)
		}
		if !exists {
			return ierr.WithError(err).WithHintf("credit note %s not found", creditNoteID).Mark(ierr.ErrNotFound)
		}

		_, err = r.client.Querier(ctx).CreditNoteLineItem.Update().
			Where(
				creditnotelineitem.TenantID(types.GetTenantID(ctx)),
				creditnotelineitem.CreditNoteID(creditNoteID),
				creditnotelineitem.IDIn(itemIDs...),
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

func (r *creditnoteRepository) Get(ctx context.Context, id string) (*domainCreditNote.CreditNote, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "get", map[string]interface{}{
		"creditnote_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedCreditNote := r.GetCache(ctx, id); cachedCreditNote != nil {
		return cachedCreditNote, nil
	}

	r.log.Debugw("getting credit note", "creditnote_id", id)

	creditNote, err := r.client.Querier(ctx).CreditNote.Query().
		Where(creditnote.ID(id),
			creditnote.TenantID(types.GetTenantID(ctx)),
			creditnote.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		WithLineItems().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.
				WithError(err).
				WithHintf("credit note %s not found", id).
				WithReportableDetails(map[string]any{
					"id": id,
				}).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("getting credit note failed").Mark(ierr.ErrDatabase)
	}

	creditNoteData := domainCreditNote.FromEnt(creditNote)
	r.SetCache(ctx, creditNoteData)
	return creditNoteData, nil
}

func (r *creditnoteRepository) Update(ctx context.Context, cn *domainCreditNote.CreditNote) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "update", map[string]interface{}{
		"creditnote_id": cn.ID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	// Use predicate-based update for optimistic locking
	query := client.CreditNote.Update().
		Where(
			creditnote.ID(cn.ID),
			creditnote.TenantID(types.GetTenantID(ctx)),
			creditnote.Status(string(types.StatusPublished)),
			creditnote.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetCreditNoteStatus(cn.CreditNoteStatus).
		SetNillableRefundStatus(cn.RefundStatus).
		SetMetadata(cn.Metadata).
		SetNillableVoidedAt(cn.VoidedAt).
		SetNillableFinalizedAt(cn.FinalizedAt).
		SetUpdatedAt(time.Now()).
		SetUpdatedBy(types.GetUserID(ctx))

	// Execute update
	n, err := query.Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("credit note update failed").
			WithReportableDetails(map[string]any{
				"creditnote_id": cn.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	if n == 0 {
		// No rows were updated - either record doesn't exist
		exists, err := client.CreditNote.Query().
			Where(
				creditnote.ID(cn.ID),
				creditnote.TenantID(types.GetTenantID(ctx)),
				creditnote.EnvironmentID(types.GetEnvironmentID(ctx)),
			).
			Exist(ctx)
		if err != nil {
			return ierr.WithError(err).
				WithHint("credit note existence check failed").
				WithReportableDetails(map[string]any{
					"creditnote_id": cn.ID,
				}).
				Mark(ierr.ErrDatabase)
		}
		if !exists {
			return ierr.WithError(err).
				WithHint("credit note not found").
				WithReportableDetails(map[string]any{
					"creditnote_id": cn.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
	}
	r.DeleteCache(ctx, cn.ID)
	return nil
}

func (r *creditnoteRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "delete", map[string]interface{}{
		"creditnote_id": id,
	})
	defer FinishSpan(span)

	r.log.Info("deleting credit note", "creditnote_id", id)

	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Delete line items first
		_, err := r.client.Querier(ctx).CreditNoteLineItem.Update().
			Where(
				creditnotelineitem.CreditNoteID(id),
				creditnotelineitem.TenantID(types.GetTenantID(ctx)),
				creditnotelineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
			).
			SetStatus(string(types.StatusDeleted)).
			SetUpdatedBy(types.GetUserID(ctx)).
			SetUpdatedAt(time.Now()).
			Save(ctx)
		if err != nil {
			return ierr.WithError(err).WithHint("line item deletion failed").Mark(ierr.ErrDatabase)
		}

		// Then delete credit note
		_, err = r.client.Querier(ctx).CreditNote.Update().
			Where(
				creditnote.ID(id),
				creditnote.TenantID(types.GetTenantID(ctx)),
				creditnote.Status(string(types.StatusPublished)),
				creditnote.EnvironmentID(types.GetEnvironmentID(ctx)),
			).
			SetStatus(string(types.StatusDeleted)).
			SetUpdatedBy(types.GetUserID(ctx)).
			SetUpdatedAt(time.Now()).
			Save(ctx)
		if err != nil {
			return ierr.WithError(err).WithHint("credit note deletion failed").Mark(ierr.ErrDatabase)
		}

		return nil
	})
}

// List returns a paginated list of credit notes based on the filter
func (r *creditnoteRepository) List(ctx context.Context, filter *types.CreditNoteFilter) ([]*domainCreditNote.CreditNote, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	query := client.CreditNote.Query().
		WithLineItems()

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	creditNotes, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("credit note listing failed").WithReportableDetails(
			map[string]any{
				"cause": err.Error(),
			},
		).Mark(ierr.ErrDatabase)
	}

	// Convert to domain model
	result := make([]*domainCreditNote.CreditNote, len(creditNotes))
	for i, cn := range creditNotes {
		result[i] = domainCreditNote.FromEnt(cn)
	}

	return result, nil
}

// Count returns the total number of credit notes based on the filter
func (r *creditnoteRepository) Count(ctx context.Context, filter *types.CreditNoteFilter) (int, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	query := client.CreditNote.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).WithHint("credit note counting failed").Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (r *creditnoteRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domainCreditNote.CreditNote, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditnote", "get_by_idempotency_key", map[string]interface{}{
		"idempotency_key": key,
	})
	defer FinishSpan(span)
	// Try to get from cache first
	if cachedCreditNote := r.GetCache(ctx, key); cachedCreditNote != nil {
		return cachedCreditNote, nil
	}

	cn, err := r.client.Querier(ctx).CreditNote.Query().
		Where(
			creditnote.IdempotencyKey(key),
			creditnote.EnvironmentID(types.GetEnvironmentID(ctx)),
			creditnote.TenantID(types.GetTenantID(ctx)),
			creditnote.Status(string(types.StatusPublished)),
			creditnote.CreditNoteStatus(types.CreditNoteStatusFinalized),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).WithHint("credit note not found").Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("failed to get credit note by idempotency key").Mark(ierr.ErrDatabase)
	}

	creditNoteData := domainCreditNote.FromEnt(cn)
	r.SetCache(ctx, creditNoteData)
	return creditNoteData, nil
}

// CreditNoteQuery type alias for better readability
type CreditNoteQuery = *ent.CreditNoteQuery

// CreditNoteQueryOptions implements BaseQueryOptions for credit note queries
type CreditNoteQueryOptions struct{}

func (o CreditNoteQueryOptions) ApplyTenantFilter(ctx context.Context, query CreditNoteQuery) CreditNoteQuery {
	return query.Where(creditnote.TenantID(types.GetTenantID(ctx)))
}

func (o CreditNoteQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CreditNoteQuery) CreditNoteQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(creditnote.EnvironmentID(environmentID))
	}
	return query
}

func (o CreditNoteQueryOptions) ApplyStatusFilter(query CreditNoteQuery, status string) CreditNoteQuery {
	if status == "" {
		return query.Where(creditnote.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(creditnote.Status(status))
}

func (o CreditNoteQueryOptions) ApplySortFilter(query CreditNoteQuery, field string, order string) CreditNoteQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o CreditNoteQueryOptions) ApplyPaginationFilter(query CreditNoteQuery, limit int, offset int) CreditNoteQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o CreditNoteQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return creditnote.FieldCreatedAt
	case "updated_at":
		return creditnote.FieldUpdatedAt
	case "credit_note_number":
		return creditnote.FieldCreditNoteNumber
	default:
		return field
	}
}

func (o CreditNoteQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CreditNoteFilter, query CreditNoteQuery) CreditNoteQuery {
	if f == nil {
		return query
	}

	// Apply entity-specific filters
	if f.InvoiceID != "" {
		query = query.Where(creditnote.InvoiceID(f.InvoiceID))
	}
	if f.CreditNoteType != "" {
		query = query.Where(creditnote.CreditNoteType(f.CreditNoteType))
	}
	if len(f.CreditNoteIDs) > 0 {
		query = query.Where(creditnote.IDIn(f.CreditNoteIDs...))
	}
	if len(f.CreditNoteStatus) > 0 {
		query = query.Where(creditnote.CreditNoteStatusIn(f.CreditNoteStatus...))
	}

	// Apply time range filters
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(creditnote.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(creditnote.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	return query
}

func (r *creditnoteRepository) SetCache(ctx context.Context, cn *domainCreditNote.CreditNote) {
	span := cache.StartCacheSpan(ctx, "creditnote", "set", map[string]interface{}{
		"creditnote_id": cn.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixCreditNote, tenantID, environmentID, cn.ID)
	r.cache.Set(ctx, cacheKey, cn, cache.ExpiryDefaultInMemory)

	r.log.Debugw("set credit note in cache", "id", cn.ID, "cache_key", cacheKey)
}

func (r *creditnoteRepository) GetCache(ctx context.Context, key string) *domainCreditNote.CreditNote {
	span := cache.StartCacheSpan(ctx, "creditnote", "get", map[string]interface{}{
		"creditnote_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixCreditNote, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainCreditNote.CreditNote)
	}
	return nil
}

func (r *creditnoteRepository) DeleteCache(ctx context.Context, key string) {
	span := cache.StartCacheSpan(ctx, "creditnote", "delete", map[string]interface{}{
		"creditnote_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixCreditNote, tenantID, environmentID, key)
	r.cache.Delete(ctx, cacheKey)
}
