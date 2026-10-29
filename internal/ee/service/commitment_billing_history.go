package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type billingPageCursor struct {
	Scope   string     `json:"scope"`
	ID      string     `json:"id"`
	Created time.Time  `json:"created"`
	BillOn  *time.Time `json:"bill_on"`
}

func billingPageScope(ctx context.Context, id string, req dto.ListBillingScheduleItemsRequest) string {
	data, _ := json.Marshal([]any{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), id, req.IncludeArchived, req.ScheduleStatus})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func decodeBillingPage(ctx context.Context, id string, req dto.ListBillingScheduleItemsRequest) (*billingPageCursor, error) {
	if req.Cursor == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(req.Cursor)
	if err != nil {
		return nil, ierr.Validation("invalid billing schedule cursor")
	}
	var cursor billingPageCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.ID == "" || len(cursor.ID) > 50 || cursor.Created.IsZero() || cursor.Created.Year() < 1 || cursor.Created.Year() > 9999 || (cursor.BillOn != nil && (cursor.BillOn.Year() < 1 || cursor.BillOn.Year() > 9999)) || cursor.Scope != billingPageScope(ctx, id, req) {
		return nil, ierr.Validation("billing schedule cursor does not match this scope or filter")
	}
	return &cursor, nil
}

func billingNextCursor(ctx context.Context, id string, req dto.ListBillingScheduleItemsRequest, last *domain.ScheduleItem) *string {
	data, _ := json.Marshal(billingPageCursor{Scope: billingPageScope(ctx, id, req), ID: last.ID, Created: last.CreatedAt, BillOn: last.BillOn})
	encoded := base64.RawURLEncoding.EncodeToString(data)
	return &encoded
}

func billingCurrentPage(ctx context.Context, id string, req dto.ListBillingScheduleItemsRequest, items []*domain.ScheduleItem, after *billingPageCursor) *dto.ListBillingScheduleItemsResponse {
	less := func(a, b *domain.ScheduleItem) bool {
		if (a.BillOn == nil) != (b.BillOn == nil) {
			return a.BillOn != nil
		}
		if a.BillOn != nil && !a.BillOn.Equal(*b.BillOn) {
			return a.BillOn.Before(*b.BillOn)
		}
		return a.ID < b.ID
	}
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	selected := make([]*domain.ScheduleItem, 0, len(items))
	for _, item := range items {
		if after != nil && !less(&domain.ScheduleItem{ID: after.ID, BillOn: after.BillOn}, item) {
			continue
		}
		selected = append(selected, item)
	}
	return billingItemPage(ctx, id, req, selected)
}

func billingItemPage(ctx context.Context, id string, req dto.ListBillingScheduleItemsRequest, items []*domain.ScheduleItem) *dto.ListBillingScheduleItemsResponse {
	page := &dto.ListBillingScheduleItemsResponse{Items: []*dto.BillingScheduleItemResponse{}, Pagination: dto.BillingPagination{Limit: req.Limit, HasMore: len(items) > req.Limit}}
	if page.Pagination.HasMore {
		items = items[:req.Limit]
		page.Pagination.NextCursor = billingNextCursor(ctx, id, req, items[len(items)-1])
	}
	for _, item := range items {
		page.Items = append(page.Items, billingScheduleResponse(item))
	}
	return page
}

func (s *commitmentBillingTerms) ListItems(ctx context.Context, id string, req dto.ListBillingScheduleItemsRequest) (*dto.ListBillingScheduleItemsResponse, error) {
	if err := req.Normalize(); err != nil {
		return nil, err
	}
	if _, err := s.owner(ctx, id); err != nil {
		return nil, err
	}
	after, err := decodeBillingPage(ctx, id, req)
	if err != nil {
		return nil, err
	}
	owner := commitmentBillingOwner(id)
	filter := &domain.ScheduleItemFilter{Owner: &owner, IncludeArchived: req.IncludeArchived, Limit: req.Limit + 1}
	if req.ScheduleStatus != "" {
		filter.ScheduleStatuses = []types.BillingScheduleStatus{req.ScheduleStatus}
	}
	if req.IncludeArchived && after != nil {
		filter.After = &domain.Cursor{ID: after.ID, CreatedAt: after.Created}
	}
	if !req.IncludeArchived {
		filter.Limit = core.MaxItems
	}
	items, err := s.items.List(types.WithForceWriter(ctx), filter)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !billingItemScoped(ctx, id, item) || (!req.IncludeArchived && item.Status != types.StatusPublished) || (req.ScheduleStatus != "" && item.ScheduleStatus != req.ScheduleStatus) {
			return nil, ierr.NewError("billing item query returned invalid scope").Mark(ierr.ErrDatabase)
		}
	}
	if !req.IncludeArchived && len(items) == core.MaxItems {
		last := items[len(items)-1]
		filter.After = &domain.Cursor{ID: last.ID, CreatedAt: last.CreatedAt}
		filter.Limit = 1
		extra, err := s.items.List(types.WithForceWriter(ctx), filter)
		if err != nil {
			return nil, err
		}
		if len(extra) != 0 {
			return nil, ierr.NewError("current billing items exceed the supported limit").Mark(ierr.ErrDatabase)
		}
	}
	var page *dto.ListBillingScheduleItemsResponse
	if req.IncludeArchived {
		page = billingItemPage(ctx, id, req, items)
	} else {
		page = billingCurrentPage(ctx, id, req, items, after)
	}
	if err := s.expandInvoices(ctx, page.Items); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *commitmentBillingTerms) GetItem(ctx context.Context, id, itemID string) (*dto.BillingScheduleItemResponse, error) {
	if _, err := s.owner(ctx, id); err != nil {
		return nil, err
	}
	if strings.TrimSpace(itemID) == "" || len(itemID) > 50 {
		return nil, ierr.Validation("billing schedule item ID is required")
	}
	item, err := s.items.Get(types.WithForceWriter(ctx), itemID)
	if err != nil {
		return nil, err
	}
	if !billingItemScoped(ctx, id, item) || item.ID != itemID {
		return nil, ierr.NewError("billing schedule item not found").Mark(ierr.ErrNotFound)
	}
	response := billingScheduleResponse(item)
	if err := s.expandInvoices(ctx, []*dto.BillingScheduleItemResponse{response}); err != nil {
		return nil, err
	}
	if err := s.expandRules(ctx, id, []*dto.BillingScheduleItemResponse{response}); err != nil {
		return nil, err
	}
	return response, nil
}

func (s *commitmentBillingTerms) History(ctx context.Context, id string, req dto.ListBillingScheduleItemsRequest) (*dto.ListBillingScheduleItemsResponse, error) {
	req.IncludeArchived = true
	page, err := s.ListItems(ctx, id, req)
	if err != nil {
		return nil, err
	}
	if err := s.expandRules(ctx, id, page.Items); err != nil {
		return nil, err
	}
	return page, nil
}

func billingItemScoped(ctx context.Context, id string, item *domain.ScheduleItem) bool {
	return item != nil && item.ID != "" && item.OwnerType == types.BillingTermOwnerCommitment && item.OwnerID == id &&
		item.TenantID == types.GetTenantID(ctx) && item.EnvironmentID == types.GetEnvironmentID(ctx) &&
		(item.Status == types.StatusPublished || item.Status == types.StatusArchived)
}

func (s *commitmentBillingTerms) expandInvoices(ctx context.Context, items []*dto.BillingScheduleItemResponse) error {
	return expandBillingInvoices(ctx, s.params.BillingTermsStore, items)
}

func expandBillingInvoices(ctx context.Context, store domain.ExecutionStore, items []*dto.BillingScheduleItemResponse) error {
	bound := make([]*domain.ScheduleItem, 0, len(items))
	byID := make(map[string]*dto.BillingScheduleItemResponse, len(items))
	for _, item := range items {
		if item.InvoiceID != nil || item.InvoiceLineItemID != nil {
			bound = append(bound, item.ScheduleItem)
			byID[item.ID] = item
		}
	}
	if len(bound) == 0 {
		return nil
	}
	if store == nil {
		return ierr.NewError("billing invoice details are unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	bindings, err := store.BindingStates(types.WithForceWriter(ctx), bound)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		item := byID[binding.ItemID]
		if item == nil || item.InvoiceStatus != nil || item.InvoiceID == nil || item.InvoiceLineItemID == nil ||
			binding.InvoiceID != *item.InvoiceID || binding.InvoiceLineItemID != *item.InvoiceLineItemID ||
			binding.TenantID != types.GetTenantID(ctx) || binding.EnvironmentID != types.GetEnvironmentID(ctx) ||
			binding.InvoiceStatus.Validate() != nil {
			return ierr.NewError("billing invoice source is inconsistent").Mark(ierr.ErrDatabase)
		}
		status := binding.InvoiceStatus
		item.InvoiceStatus = &status
	}
	for _, item := range byID {
		if item.InvoiceStatus == nil {
			return ierr.NewError("billing invoice source is missing").Mark(ierr.ErrDatabase)
		}
	}
	return nil
}

func (s *commitmentBillingTerms) expandRules(ctx context.Context, id string, items []*dto.BillingScheduleItemResponse) error {
	ids := []string{}
	for _, item := range items {
		if item.BillingTermID != nil && !slices.Contains(ids, *item.BillingTermID) {
			ids = append(ids, *item.BillingTermID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	owner := commitmentBillingOwner(id)
	rows, err := s.rules.List(types.WithForceWriter(ctx), &domain.RuleFilter{Owner: &owner, IDs: ids, IncludeArchived: true, Limit: len(ids)})
	if err != nil {
		return err
	}
	rules := make(map[string]*dto.BillingTermResponse, len(rows))
	for _, rule := range rows {
		if rule == nil || !slices.Contains(ids, rule.ID) || rules[rule.ID] != nil || (rule.Status != types.StatusPublished && rule.Status != types.StatusArchived) || rule.OwnerType != owner.Type || rule.OwnerID != id || rule.TenantID != types.GetTenantID(ctx) || rule.EnvironmentID != types.GetEnvironmentID(ctx) {
			return ierr.NewError("billing rule query returned invalid scope").Mark(ierr.ErrDatabase)
		}
		rules[rule.ID] = billingRuleResponse(rule)
	}
	for _, item := range items {
		if item.BillingTermID != nil {
			item.BillingTerm = rules[*item.BillingTermID]
			if item.BillingTerm == nil {
				return ierr.NewError("billing item source rule is missing").Mark(ierr.ErrDatabase)
			}
		}
	}
	return nil
}
