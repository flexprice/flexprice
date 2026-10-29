package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type commitmentAnalyticsCursor struct {
	Scope   string `json:"scope"`
	AfterID string `json:"after_id"`
}

func (s *meterUsageService) GetCostExplorer(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	if req == nil || ctx == nil || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return nil, ierr.Validation("cost explorer requires a scoped analytics request")
	}
	limit := req.CommitmentsLimit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || len(req.CommitmentsCursor) > 2048 {
		return nil, ierr.Validation("commitments_limit must be between 1 and 100 and commitments_cursor must fit 2048 bytes")
	}
	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
		ExternalCustomerID: req.ExternalCustomerID, ExternalCustomerIDs: slices.Clone(req.ExternalCustomerIDs), FeatureIDs: slices.Clone(req.FeatureIDs),
		StartTime: req.StartTime, EndTime: req.EndTime, GroupBy: slices.Clone(req.GroupBy), PropertyFilters: req.PropertyFilters,
		Sources: slices.Clone(req.Sources), WindowSize: req.WindowSize, Expand: slices.Clone(req.Expand), IncludeChildren: req.IncludeChildren, BreakdownBucket: req.BreakdownBucket,
	}
	if params.EndTime.IsZero() {
		params.EndTime = time.Now().UTC()
	}
	if params.StartTime.IsZero() {
		params.StartTime = params.EndTime.Add(-6 * time.Hour)
	}
	if !params.StartTime.Before(params.EndTime) {
		return nil, ierr.Validation("start_time must be before end_time")
	}
	enabled, err := newCommitmentService(s.ServiceParams).IsEnabled(ctx)
	if err != nil {
		return nil, err
	}
	var query *commitmentAnalyticsQuery
	var scope string
	if enabled {
		customers, err := s.commitmentAnalyticsCustomers(ctx, params)
		if err != nil {
			return nil, err
		}
		scope, err = commitmentAnalyticsScope(ctx, params, customers)
		if err != nil {
			return nil, err
		}
		after, err := decodeCommitmentAnalyticsCursor(req.CommitmentsCursor, scope)
		if err != nil {
			return nil, err
		}
		query = newCommitmentAnalyticsQuery(params, customers, limit, after)
	}
	response, err := s.GetDetailedAnalytics(ctx, params)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return response, nil
	}
	response.Commitments = []*dto.CommitmentAnalyticsResponse{}
	response.CommitmentsPagination = &dto.BillingPagination{Limit: limit}
	if len(query.customerIDs) == 0 {
		return response, nil
	}
	page, err := s.commitmentAnalytics(ctx, query)
	if err != nil {
		return nil, err
	}
	for _, entry := range page.Entries() {
		owner := entry.Owner()
		response.Commitments = append(response.Commitments, &dto.CommitmentAnalyticsResponse{
			CommitmentID: owner.ID, SubscriptionID: owner.SubscriptionID,
			UsageWindow: dto.CommitmentUsageWindow{StartTime: params.StartTime, EndTime: params.EndTime},
			Usage:       entry.Usage(), UsageUnavailableReason: entry.UsageUnavailableReason(),
			CollectionScope: "lifetime", Collection: entry.Collection(),
		})
	}
	if page.NextID() != "" {
		encoded, _ := json.Marshal(commitmentAnalyticsCursor{Scope: scope, AfterID: page.NextID()})
		response.CommitmentsPagination.HasMore = true
		response.CommitmentsPagination.NextCursor = lo.ToPtr(base64.RawURLEncoding.EncodeToString(encoded))
	}
	return response, nil
}

func commitmentAnalyticsScope(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams, customers []string) (string, error) {
	data, err := json.Marshal([]any{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), customers, params})
	if err != nil {
		return "", ierr.WithError(err).WithHint("Invalid commitment analytics filters").Mark(ierr.ErrValidation)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func decodeCommitmentAnalyticsCursor(encoded, scope string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", ierr.Validation("invalid commitments_cursor")
	}
	var cursor commitmentAnalyticsCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.Scope != scope || strings.TrimSpace(cursor.AfterID) == "" || len(cursor.AfterID) > 50 {
		return "", ierr.Validation("commitments_cursor does not match this scope or filter")
	}
	return cursor.AfterID, nil
}

func (s *meterUsageService) commitmentAnalyticsCustomers(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) ([]string, error) {
	externalIDs := slices.Clone(params.ExternalCustomerIDs)
	if params.ExternalCustomerID != "" {
		externalIDs = append(externalIDs, params.ExternalCustomerID)
	}
	externalIDs = lo.Uniq(externalIDs)
	if len(externalIDs) == 0 {
		return []string{}, nil
	}
	if len(externalIDs) > commitmentAnalyticsScanLimit {
		return nil, ierr.Validation("commitment analytics supports at most 500 selected customers")
	}
	customers, err := s.CustomerRepo.List(ctx, &types.CustomerFilter{QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(commitmentAnalyticsScanLimit + 1), Status: lo.ToPtr(types.StatusPublished)}, ExternalIDs: externalIDs})
	if err != nil {
		return nil, err
	}
	if len(customers) > commitmentAnalyticsScanLimit {
		return nil, ierr.Validation("commitment analytics customer scope exceeds 500 customers")
	}
	ids := make([]string, 0, len(customers))
	for _, customer := range customers {
		if customer == nil || customer.ID == "" || customer.TenantID != types.GetTenantID(ctx) || customer.EnvironmentID != types.GetEnvironmentID(ctx) || !slices.Contains(externalIDs, customer.ExternalID) {
			return nil, ierr.NewError("commitment analytics returned invalid customer scope").Mark(ierr.ErrDatabase)
		}
		ids = append(ids, customer.ID)
	}
	if !params.IncludeChildren || len(ids) == 0 {
		slices.Sort(ids)
		return ids, nil
	}
	parents, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(commitmentAnalyticsScanLimit + 1), Status: lo.ToPtr(types.StatusPublished)}, CustomerIDs: ids, SubscriptionTypes: []types.SubscriptionType{types.SubscriptionTypeParent}, SubscriptionStatus: types.AllSubscriptionStatuses()})
	if err != nil {
		return nil, err
	}
	if len(parents) > commitmentAnalyticsScanLimit {
		return nil, ierr.Validation("commitment analytics parent scope exceeds 500 subscriptions")
	}
	parentIDs := make([]string, 0, len(parents))
	for _, parent := range parents {
		if parent == nil || parent.ID == "" || parent.Status != types.StatusPublished || parent.TenantID != types.GetTenantID(ctx) || parent.EnvironmentID != types.GetEnvironmentID(ctx) || !slices.Contains(ids, parent.CustomerID) || parent.SubscriptionType != types.SubscriptionTypeParent {
			return nil, ierr.NewError("commitment analytics returned invalid parent scope").Mark(ierr.ErrDatabase)
		}
		parentIDs = append(parentIDs, parent.ID)
	}
	if len(parentIDs) == 0 {
		slices.Sort(ids)
		return ids, nil
	}
	children, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(commitmentAnalyticsScanLimit + 1), Status: lo.ToPtr(types.StatusPublished)}, ParentSubscriptionIDs: parentIDs, SubscriptionTypes: []types.SubscriptionType{types.SubscriptionTypeInherited, types.SubscriptionTypeGroupedInvoicing}, SubscriptionStatus: types.AllSubscriptionStatuses()})
	if err != nil {
		return nil, err
	}
	if len(children) > commitmentAnalyticsScanLimit {
		return nil, ierr.Validation("commitment analytics child scope exceeds 500 subscriptions")
	}
	for _, child := range children {
		if child == nil || child.CustomerID == "" || child.Status != types.StatusPublished || (child.SubscriptionType != types.SubscriptionTypeInherited && child.SubscriptionType != types.SubscriptionTypeGroupedInvoicing) || child.TenantID != types.GetTenantID(ctx) || child.EnvironmentID != types.GetEnvironmentID(ctx) || !slices.Contains(parentIDs, lo.FromPtr(child.ParentSubscriptionID)) {
			return nil, ierr.NewError("commitment analytics returned invalid child scope").Mark(ierr.ErrDatabase)
		}
		ids = append(ids, child.CustomerID)
	}
	ids = lo.Uniq(ids)
	if len(ids) > commitmentAnalyticsScanLimit {
		return nil, ierr.Validation("commitment analytics customer scope exceeds 500 customers")
	}
	slices.Sort(ids)
	return ids, nil
}
