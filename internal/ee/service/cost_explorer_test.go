package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func costExplorerRequest(s *MeterUsageServiceSuite) *dto.GetUsageAnalyticsRequest {
	return &dto.GetUsageAnalyticsRequest{ExternalCustomerID: s.customer.ExternalID, StartTime: s.periodStart, EndTime: s.periodEnd}
}

func TestCostExplorer_NoEventsKeepsLifetimePrincipalSeparate(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	ctx := s.GetContext()
	s.createLineItem(ctx, "line_collection", s.periodStart, s.periodEnd)
	owner := s.newCommitment("comm_collection", types.CommitmentScopeSubscription, s.sub.ID, "100")
	owner.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	s.storeCommitment(owner)
	svc.BillingTerms = &analyticsTermsService{state: &analyticsTermsState{owner: &domain.OwnerSnapshot{Owner: domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: owner.ID}, TenantID: owner.TenantID, EnvironmentID: owner.EnvironmentID, CustomerID: owner.CustomerID, SubscriptionID: lo.ToPtr(owner.SubscriptionID)}, summary: &domain.CollectionSummary{AsOf: s.periodEnd.AddDate(0, 2, 0), Currency: "usd", TargetAmount: dec("1200"), AssignedAmount: dec("300"), InvoicedAmount: dec("300"), ScheduledAmount: dec("900")}}}
	baseline, err := svc.GetDetailedAnalytics(ctx, commitmentAnalyticsParams(s))
	require.NoError(t, err)
	response, err := svc.GetCostExplorer(ctx, costExplorerRequest(s))
	require.NoError(t, err)
	require.True(t, response.TotalCost.Equal(baseline.TotalCost))
	require.True(t, response.TotalCost.IsZero())
	require.Len(t, response.Commitments, 1)
	entry := response.Commitments[0]
	require.Equal(t, owner.ID, entry.CommitmentID)
	require.Equal(t, "lifetime", entry.CollectionScope)
	require.Equal(t, s.periodStart, entry.UsageWindow.StartTime)
	require.Equal(t, s.periodEnd, entry.UsageWindow.EndTime)
	require.Equal(t, "300", entry.Collection.InvoicedAmount.String())
	require.True(t, entry.Collection.AsOf.After(s.periodEnd))
	require.NotNil(t, entry.Usage)
	require.False(t, entry.Usage.TrueUpEnabled)
	require.True(t, entry.Usage.TrueUpAmount.IsZero())
	require.Equal(t, 50, response.CommitmentsPagination.Limit)
}

func TestCostExplorer_CommitmentPagingBindsCustomerAndFilters(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	ctx := s.GetContext()
	for _, id := range []string{"comm_b", "comm_a"} {
		s.storeCommitment(s.newCommitment(id, types.CommitmentScopeSubscription, s.sub.ID, "100"))
	}
	req := costExplorerRequest(s)
	req.Sources = []string{"gateway"}
	req.CommitmentsLimit = 1
	first, err := svc.GetCostExplorer(ctx, req)
	require.NoError(t, err)
	require.Len(t, first.Commitments, 1)
	require.Equal(t, "comm_a", first.Commitments[0].CommitmentID)
	require.True(t, first.CommitmentsPagination.HasMore)
	require.Nil(t, first.Commitments[0].Usage)
	require.Equal(t, "filtered_usage", first.Commitments[0].UsageUnavailableReason)
	req.CommitmentsCursor = *first.CommitmentsPagination.NextCursor
	second, err := svc.GetCostExplorer(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "comm_b", second.Commitments[0].CommitmentID)
	require.False(t, second.CommitmentsPagination.HasMore)
	req.Sources = []string{"other"}
	_, err = svc.GetCostExplorer(ctx, req)
	require.Error(t, err)
	req.Sources = []string{"gateway"}
	req.ExternalCustomerID = "unknown"
	_, err = svc.GetCostExplorer(ctx, req)
	require.Error(t, err)
	req.CommitmentsCursor = ""
	response, err := svc.GetCostExplorer(ctx, req)
	require.NoError(t, err)
	require.Empty(t, response.Commitments)
	require.False(t, response.CommitmentsPagination.HasMore)
}

func TestCostExplorer_IncludeChildrenResolvesWithoutEvents(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	ctx := s.GetContext()
	s.sub.SubscriptionType = types.SubscriptionTypeParent
	childCustomer := &customer.Customer{ID: "customer_child", ExternalID: "child_external", BaseModel: types.GetDefaultBaseModel(ctx)}
	require.NoError(t, s.GetStores().CustomerRepo.Create(ctx, childCustomer))
	child := &subscription.Subscription{ID: "sub_child", CustomerID: childCustomer.ID, SubscriptionType: types.SubscriptionTypeGroupedInvoicing, ParentSubscriptionID: lo.ToPtr(s.sub.ID), SubscriptionStatus: types.SubscriptionStatusPaused, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	require.NoError(t, s.GetStores().SubscriptionRepo.Create(ctx, child))
	owner := s.newCommitment("comm_child", types.CommitmentScopeSubscription, child.ID, "100")
	owner.CustomerID = childCustomer.ID
	owner.SubscriptionID = child.ID
	s.storeCommitment(owner)
	req := costExplorerRequest(s)
	req.Sources = []string{"gateway"}
	response, err := svc.GetCostExplorer(ctx, req)
	require.NoError(t, err)
	require.Empty(t, response.Commitments)
	req.IncludeChildren = true
	response, err = svc.GetCostExplorer(ctx, req)
	require.NoError(t, err)
	require.Len(t, response.Commitments, 1)
	require.Equal(t, owner.ID, response.Commitments[0].CommitmentID)
}

func TestCostExplorer_CollectionAllowsAuthoritativeInvoiceRecipient(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	ctx := s.GetContext()
	s.sub.InvoicingCustomerID = lo.ToPtr("recipient_parent")
	owner := s.newCommitment("comm_delegated", types.CommitmentScopeSubscription, s.sub.ID, "100")
	terms := &analyticsTermsState{owner: &domain.OwnerSnapshot{Owner: domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: owner.ID}, TenantID: owner.TenantID, EnvironmentID: owner.EnvironmentID, CustomerID: "recipient_parent", SubscriptionID: lo.ToPtr(owner.SubscriptionID)}, summary: &domain.CollectionSummary{Currency: "usd", TargetAmount: dec("1200")}}
	svc.BillingTerms = &analyticsTermsService{state: terms}
	_, err := svc.analyticsCommitmentCollection(ctx, owner)
	require.NoError(t, err)
	terms.owner.CustomerID = "unrelated"
	_, err = svc.analyticsCommitmentCollection(ctx, owner)
	require.Error(t, err)
}

func TestCostExplorer_BoundsAndDisabledFeature(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	ctx := s.GetContext()
	for _, limit := range []int{-1, 101} {
		req := costExplorerRequest(s)
		req.CommitmentsLimit = limit
		_, err := svc.GetCostExplorer(ctx, req)
		require.Error(t, err)
	}
	setCommitmentsV2(t, ctx, s.GetStores(), false)
	response, err := svc.GetCostExplorer(ctx, costExplorerRequest(s))
	require.NoError(t, err)
	require.Nil(t, response.CommitmentsPagination)
}
