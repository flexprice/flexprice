package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func newCommitmentAnalyticsFixture(t *testing.T) (*MeterUsageServiceSuite, *meterUsageService) {
	t.Helper()
	s := new(MeterUsageServiceSuite)
	s.SetT(t)
	s.BaseServiceTestSuite.SetupSuite()
	s.BaseServiceTestSuite.SetupTest()
	s.WithEnvironment("env_analytics")
	s.periodStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.periodEnd = s.periodStart.AddDate(0, 1, 0)
	s.now = s.periodStart.AddDate(0, 0, 19)
	s.meterUsageRepo = s.GetStores().MeterUsageRepo.(*testutil.InMemoryMeterUsageStore)
	s.setupEntities()
	setCommitmentsV2(t, s.GetContext(), s.GetStores(), true)
	svc := NewMeterUsageService(fullServiceParams(&s.BaseServiceTestSuite)).(*meterUsageService)
	t.Cleanup(s.TearDownTest)
	return s, svc
}

func commitmentAnalyticsParams(s *MeterUsageServiceSuite) *events.MeterUsageDetailedAnalyticsParams {
	return &events.MeterUsageDetailedAnalyticsParams{
		TenantID: types.GetTenantID(s.GetContext()), EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		ExternalCustomerID: s.customer.ExternalID, StartTime: s.periodStart, EndTime: s.periodEnd,
	}
}

func TestCommitmentAnalyticsNoEvents(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	s.createLineItem(s.GetContext(), "line_analytics", s.periodStart, s.periodEnd)
	s.storeCommitment(s.newCommitment("comm_no_events", types.CommitmentScopeSubscription, s.sub.ID, "100"))
	page, err := svc.commitmentAnalytics(s.GetContext(), newCommitmentAnalyticsQuery(commitmentAnalyticsParams(s), []string{s.customer.ID}, 50, ""))
	require.NoError(t, err)
	require.Len(t, page.Entries(), 1)
	entry := page.Entries()[0]
	require.Empty(t, entry.UsageUnavailableReason())
	require.NotNil(t, entry.Usage())
	require.True(t, entry.Usage().Committed.Amount.Equal(dec("100")))
	require.True(t, entry.Usage().Used.Amount.IsZero())
	require.True(t, entry.Usage().Unused.Amount.Equal(dec("100")))
	require.True(t, entry.Usage().TrueUpAmount.Equal(dec("100")))
	require.Nil(t, entry.Collection())
	require.Empty(t, page.NextID())
}

func TestCommitmentAnalyticsMeterFilterUsesWholePool(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	ctx := s.GetContext()
	meterA := s.createMeterWithAggregation(ctx, "meter_a", "event_a", types.AggregationSum)
	meterB := s.createMeterWithAggregation(ctx, "meter_b", "event_b", types.AggregationSum)
	priceA := s.createPriceForMeter(ctx, "price_a", meterA.ID, dec("1"))
	priceB := s.createPriceForMeter(ctx, "price_b", meterB.ID, dec("1"))
	s.createLineItemForMeter(ctx, "line_a", meterA.ID, priceA.ID)
	s.createLineItemForMeter(ctx, "line_b", meterB.ID, priceB.ID)
	s.storeCommitment(s.newCommitment("comm_shared", types.CommitmentScopeSubscription, s.sub.ID, "150"))
	s.insertMeterUsage(ctx, meterA.ID, s.customer.ExternalID, s.periodStart.AddDate(0, 0, 5), 40)
	s.insertMeterUsage(ctx, meterB.ID, s.customer.ExternalID, s.periodStart.AddDate(0, 0, 6), 60)
	params := commitmentAnalyticsParams(s)
	params.MeterIDs = []string{meterA.ID}
	params.GroupBy = []string{"source"}
	page, err := svc.commitmentAnalytics(ctx, newCommitmentAnalyticsQuery(params, []string{s.customer.ID}, 50, ""))
	require.NoError(t, err)
	require.Len(t, page.Entries(), 1)
	require.True(t, page.Entries()[0].Usage().Used.Amount.Equal(dec("100")))
	require.True(t, page.Entries()[0].Usage().Unused.Amount.Equal(dec("50")))
	require.Equal(t, []string{meterA.ID}, params.MeterIDs, "assembly must not mutate the usage request")
}

type analyticsTermsState struct {
	core.State
	owner   *domain.OwnerSnapshot
	summary *domain.CollectionSummary
}

func (s *analyticsTermsState) Owner() *domain.OwnerSnapshot       { return s.owner }
func (s *analyticsTermsState) Summary() *domain.CollectionSummary { return s.summary }

type analyticsTermsService struct {
	core.Service
	state core.State
	reads int
}

func (s *analyticsTermsService) Get(context.Context, domain.OwnerRef) (core.State, error) {
	s.reads++
	return s.state, nil
}

func TestCommitmentAnalyticsScheduledUsageHasNoTrueUp(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	ctx := s.GetContext()
	s.createLineItem(ctx, "line_scheduled", s.periodStart, s.periodEnd)
	owner := s.newCommitment("comm_scheduled", types.CommitmentScopeSubscription, s.sub.ID, "100")
	owner.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	s.storeCommitment(owner)
	svc.BillingTerms = &analyticsTermsService{state: &analyticsTermsState{
		owner: &domain.OwnerSnapshot{
			Owner:    domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: owner.ID},
			TenantID: owner.TenantID, EnvironmentID: owner.EnvironmentID,
			CustomerID: owner.CustomerID, SubscriptionID: lo.ToPtr(owner.SubscriptionID),
		},
		summary: &domain.CollectionSummary{Currency: "usd", TargetAmount: dec("100"), InvoicedAmount: dec("100")},
	}}
	page, err := svc.commitmentAnalytics(ctx, newCommitmentAnalyticsQuery(commitmentAnalyticsParams(s), []string{s.customer.ID}, 50, ""))
	require.NoError(t, err)
	require.Len(t, page.Entries(), 1)
	entry := page.Entries()[0]
	require.NotNil(t, entry.Usage())
	require.False(t, entry.Usage().TrueUpEnabled)
	require.True(t, entry.Usage().TrueUpAmount.IsZero())
	require.True(t, entry.Usage().BilledAmount.IsZero())
	require.True(t, entry.Collection().InvoicedAmount.Equal(dec("100")))
	require.True(t, entry.Usage().Unused.Amount.Equal(dec("100")))
}

func TestCommitmentAnalyticsFilteredUsageKeepsLifetimeCollection(t *testing.T) {
	for _, filtered := range []string{"source", "property"} {
		t.Run(filtered, func(t *testing.T) {
			s, svc := newCommitmentAnalyticsFixture(t)
			owner := s.newCommitment("comm_scheduled", types.CommitmentScopeSubscription, s.sub.ID, "100")
			owner.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
			s.storeCommitment(owner)
			asOf := s.periodEnd.AddDate(0, 1, 0)
			summary := &domain.CollectionSummary{AsOf: asOf, Currency: "usd", TargetAmount: dec("1200"), AssignedAmount: dec("400"), InvoicedAmount: dec("300"), ScheduledAmount: dec("800")}
			terms := &analyticsTermsService{state: &analyticsTermsState{owner: &domain.OwnerSnapshot{Owner: domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: owner.ID}, TenantID: owner.TenantID, EnvironmentID: owner.EnvironmentID, CustomerID: owner.CustomerID, SubscriptionID: lo.ToPtr(owner.SubscriptionID)}, summary: summary}}
			svc.BillingTerms = terms
			params := commitmentAnalyticsParams(s)
			if filtered == "source" {
				params.Sources = []string{"gateway"}
			} else {
				params.PropertyFilters = map[string][]string{"region": {"us"}}
			}
			page, err := svc.commitmentAnalytics(s.GetContext(), newCommitmentAnalyticsQuery(params, []string{s.customer.ID}, 50, ""))
			require.NoError(t, err)
			require.Len(t, page.Entries(), 1)
			entry := page.Entries()[0]
			require.Nil(t, entry.Usage())
			require.Equal(t, "filtered_usage", entry.UsageUnavailableReason())
			require.Equal(t, summary, entry.Collection())
			require.Equal(t, asOf, entry.Collection().AsOf)
			require.Equal(t, 1, terms.reads)
			entry.Collection().InvoicedAmount = dec("999")
			require.True(t, entry.Collection().InvoicedAmount.Equal(dec("300")))
		})
	}
}

func TestCommitmentAnalyticsPagingAndIsolation(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	for _, id := range []string{"comm_c", "comm_a", "comm_b"} {
		s.storeCommitment(s.newCommitment(id, types.CommitmentScopeSubscription, s.sub.ID, "100"))
	}
	for _, scope := range []string{"tenant", "environment", "customer"} {
		owner := s.newCommitment("comm_foreign_"+scope, types.CommitmentScopeSubscription, s.sub.ID, "100")
		switch scope {
		case "tenant":
			owner.TenantID = "other_tenant"
		case "environment":
			owner.EnvironmentID = "other_environment"
		case "customer":
			owner.CustomerID = "other_customer"
		}
		s.storeCommitment(owner)
	}
	params := commitmentAnalyticsParams(s)
	params.Sources = []string{"filtered"}
	first, err := svc.commitmentAnalytics(s.GetContext(), newCommitmentAnalyticsQuery(params, []string{s.customer.ID}, 2, ""))
	require.NoError(t, err)
	require.Equal(t, []string{"comm_a", "comm_b"}, analyticsEntryIDs(first))
	require.Equal(t, "comm_b", first.NextID())
	second, err := svc.commitmentAnalytics(s.GetContext(), newCommitmentAnalyticsQuery(params, []string{s.customer.ID}, 2, first.NextID()))
	require.NoError(t, err)
	require.Equal(t, []string{"comm_c"}, analyticsEntryIDs(second))
	require.Empty(t, second.NextID())
}

func TestCommitmentAnalyticsFilteredTraversalBound(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	ctx := s.GetContext()
	line := s.createLineItem(ctx, "line_scanned", s.periodStart, s.periodEnd)
	for i := 0; i <= commitmentAnalyticsScanLimit; i++ {
		owner := s.newCommitment(fmt.Sprintf("comm_%03d", i), types.CommitmentScopePrice, "unrelated_price", "100")
		if i == commitmentAnalyticsScanLimit {
			owner.ScopeEntityID = line.PriceID
		}
		s.storeCommitment(owner)
	}
	params := commitmentAnalyticsParams(s)
	params.MeterIDs, params.Sources = []string{line.MeterID}, []string{"filtered"}
	first, err := svc.commitmentAnalytics(ctx, newCommitmentAnalyticsQuery(params, []string{s.customer.ID}, 50, ""))
	require.NoError(t, err)
	require.Empty(t, first.Entries())
	require.Equal(t, "comm_499", first.NextID())
	second, err := svc.commitmentAnalytics(ctx, newCommitmentAnalyticsQuery(params, []string{s.customer.ID}, 50, first.NextID()))
	require.NoError(t, err)
	require.Equal(t, []string{"comm_500"}, analyticsEntryIDs(second))
	require.Empty(t, second.NextID())
}

func TestCommitmentAnalyticsRequiresScopeAndDependencies(t *testing.T) {
	s, svc := newCommitmentAnalyticsFixture(t)
	query := newCommitmentAnalyticsQuery(commitmentAnalyticsParams(s), []string{s.customer.ID}, 50, "")
	for _, ctx := range []context.Context{nil, context.Background(), types.SetTenantID(context.Background(), "only_tenant")} {
		_, err := svc.commitmentAnalytics(ctx, query)
		require.True(t, ierr.IsValidation(err), "%+v", err)
	}
	for _, bad := range []*commitmentAnalyticsQuery{nil, newCommitmentAnalyticsQuery(commitmentAnalyticsParams(s), nil, 50, ""), newCommitmentAnalyticsQuery(commitmentAnalyticsParams(s), []string{s.customer.ID}, 101, "")} {
		_, err := svc.commitmentAnalytics(s.GetContext(), bad)
		require.True(t, ierr.IsValidation(err), "%+v", err)
	}
	owner := s.newCommitment("comm_missing_core", types.CommitmentScopeSubscription, s.sub.ID, "100")
	owner.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
	s.storeCommitment(owner)
	query.params.Sources = []string{"filtered"}
	_, err := svc.commitmentAnalytics(s.GetContext(), query)
	require.Error(t, err)
	require.True(t, ierr.IsServiceUnavailable(err), "%+v", err)
}

func analyticsEntryIDs(page *commitmentAnalyticsPage) []string {
	return lo.Map(page.Entries(), func(entry *commitmentAnalyticsEntry, _ int) string { return entry.Owner().ID })
}

func TestCommitmentAnalyticsGroupedCollectionRecipient(t *testing.T) {
	f := newCommitmentTermsFixture(t)
	parent := subscription.NewSubscriptionBuilder(f.sub).WithSubscriptionType(types.SubscriptionTypeParent).Build()
	parent.ID = "analytics_parent"
	parent.CustomerID = "parent_customer"
	parent.InvoicingCustomerID = lo.ToPtr("parent_payer")
	require.NoError(t, f.subs.Create(f.ctx, parent))
	f.sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
	f.sub.ParentSubscriptionID = &parent.ID
	snapshot, err := f.adapter.Snapshot(f.ctx, f.owner)
	require.NoError(t, err)
	require.NotEqual(t, f.sub.GetInvoicingCustomerID(), snapshot.CustomerID)
	summary := &domain.CollectionSummary{Currency: "usd", TargetAmount: dec("1200"), InvoicedAmount: dec("300")}
	svc := &meterUsageService{ServiceParams: ServiceParams{SubRepo: f.subs, BillingTerms: &analyticsTermsService{state: &analyticsTermsState{owner: snapshot, summary: summary}}}}
	got, err := svc.analyticsCommitmentCollection(f.ctx, f.commitment)
	require.NoError(t, err)
	require.Equal(t, summary, got)
	snapshot.EnvironmentID = "another_environment"
	_, err = svc.analyticsCommitmentCollection(f.ctx, f.commitment)
	require.True(t, ierr.IsDatabase(err), "%v", err)
}
