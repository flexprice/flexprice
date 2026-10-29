package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// BillingScheduleService prepares scoped work and atomically creates or recovers its invoice.
type BillingScheduleService interface {
	Prepare(context.Context, core.DueBatchRequest) (*core.DueBatch, error)
	CreateOrRecover(context.Context, core.DueInvoiceRequest) (*core.DueInvoice, error)
}

type billingScheduleService struct {
	params ServiceParams
	items  domain.ScheduleItemRepository
}

// NewBillingScheduleService connects due delivery to the shared collection and invoice services.
func NewBillingScheduleService(params ServiceParams, items domain.ScheduleItemRepository) BillingScheduleService {
	return &billingScheduleService{params: params, items: items}
}

type billingDueCursor struct {
	Scope string         `json:"scope"`
	Phase int            `json:"phase"`
	After *domain.Cursor `json:"after,omitempty"`
}

func (s *billingScheduleService) ready(ctx context.Context, asOf time.Time) error {
	if ctx == nil || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" || asOf.IsZero() || asOf.Year() < 1 || asOf.Year() > 9999 {
		return ierr.Validation("billing schedule delivery requires tenant, environment and a finite cutoff")
	}
	if s == nil || s.items == nil || s.params.BillingTerms == nil || s.params.BillingTermsStore == nil || s.params.DB == nil || s.params.InvoiceRepo == nil || s.params.Logger == nil {
		return ierr.NewError("billing schedule delivery is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	if s.params.BillingTermsStore.InTransaction(ctx) || s.params.DB.TxFromContext(ctx) != nil {
		return ierr.Validation("billing schedule delivery requires its own transaction")
	}
	return nil
}

func (s *billingScheduleService) Prepare(ctx context.Context, req core.DueBatchRequest) (*core.DueBatch, error) {
	if err := s.ready(ctx, req.AsOf); err != nil {
		return nil, err
	}
	if req.Limit < 1 || req.Limit > 100 || len(req.Cursor) > 2048 {
		return nil, ierr.Validation("billing schedule batch limit must be between 1 and 100")
	}
	cursor, err := decodeBillingDueCursor(ctx, req)
	if err != nil {
		return nil, err
	}
	filter := &domain.ScheduleItemFilter{Limit: req.Limit, After: cursor.After, CreatedBefore: &req.AsOf}
	switch cursor.Phase {
	case 0:
		filter.ScheduleStatuses = []types.BillingScheduleStatus{types.BillingScheduleScheduled}
		filter.UnresolvedManualBefore = &req.AsOf
	case 1:
		filter.ScheduleStatuses = []types.BillingScheduleStatus{types.BillingScheduleInvoiced}
		filter.Delivery = lo.ToPtr(types.BillingTermSeparateInvoice)
		filter.IncludeArchived = true
		filter.RecoverableInvoicesOnly = true
	case 2:
		filter.ScheduleStatuses = []types.BillingScheduleStatus{types.BillingScheduleScheduled}
		filter.Delivery = lo.ToPtr(types.BillingTermSeparateInvoice)
		filter.DueBefore = &req.AsOf
	}
	items, err := s.items.List(types.WithForceWriter(ctx), filter)
	if err != nil {
		return nil, err
	}
	if len(items) > req.Limit {
		return nil, ierr.NewError("billing schedule repository exceeded its batch limit").Mark(ierr.ErrDatabase)
	}
	result := &core.DueBatch{Items: []core.DueCandidate{}, Scanned: len(items)}
	for _, item := range items {
		if !billingDueItemScope(ctx, item) || item.CreatedAt.IsZero() {
			return nil, ierr.NewError("billing schedule batch returned an invalid source").Mark(ierr.ErrDatabase)
		}
		if cursor.Phase == 0 {
			err := s.params.BillingTerms.ResolveFallback(ctx, core.FallbackRequest{Owner: domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}, ItemID: item.ID, AsOf: req.AsOf})
			if ierr.IsNotFound(err) {
				continue
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				s.params.Logger.Error(ctx, "billing schedule fallback failed", "error", err, "item_id", item.ID)
				result.FailedItemIDs = append(result.FailedItemIDs, item.ID)
			}
			continue
		}
		result.Items = append(result.Items, core.DueCandidate{ItemID: item.ID})
	}
	if len(items) == req.Limit {
		last := items[len(items)-1]
		if cursor.After != nil && cursor.After.ID == last.ID && cursor.After.CreatedAt.Equal(last.CreatedAt) {
			return nil, ierr.NewError("billing schedule cursor did not advance").Mark(ierr.ErrDatabase)
		}
		cursor.After = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	} else {
		cursor.Phase++
		cursor.After = nil
	}
	if cursor.Phase <= 2 {
		encoded, err := json.Marshal(cursor)
		if err != nil {
			return nil, err
		}
		result.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return result, nil
}

func decodeBillingDueCursor(ctx context.Context, req core.DueBatchRequest) (*billingDueCursor, error) {
	data, _ := json.Marshal([]any{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), req.AsOf.UTC()})
	hash := sha256.Sum256(data)
	scope := hex.EncodeToString(hash[:])
	if req.Cursor == "" {
		return &billingDueCursor{Scope: scope}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(req.Cursor)
	if err != nil {
		return nil, ierr.Validation("invalid billing schedule cursor")
	}
	var cursor billingDueCursor
	if json.Unmarshal(decoded, &cursor) != nil || cursor.Scope != scope || cursor.Phase < 0 || cursor.Phase > 2 ||
		(cursor.After != nil && (!billingDueID(cursor.After.ID) || cursor.After.CreatedAt.IsZero() || cursor.After.CreatedAt.Year() < 1 || cursor.After.CreatedAt.Year() > 9999)) {
		return nil, ierr.Validation("billing schedule cursor does not match this scope and cutoff")
	}
	return &cursor, nil
}

func billingDueID(id string) bool { return strings.TrimSpace(id) != "" && len(id) <= 50 }

func billingDueItemScope(ctx context.Context, item *domain.ScheduleItem) bool {
	return item != nil && billingDueID(item.ID) && item.TenantID == types.GetTenantID(ctx) && item.EnvironmentID == types.GetEnvironmentID(ctx)
}
