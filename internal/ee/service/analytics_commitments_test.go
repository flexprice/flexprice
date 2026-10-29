package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
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

func TestCommitmentAnalyticsAcrossBillingPeriods(t *testing.T) {
	for _, tc := range []struct {
		name       string
		quantity   bool
		scheduled  bool
		partialEnd bool
	}{
		{name: "amount capacity resets each month"},
		{name: "quantity capacity resets each month", quantity: true},
		{name: "scheduled collection preserves economic usage", scheduled: true},
		{name: "open final period retains full capacity", partialEnd: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, svc := newCommitmentAnalyticsFixture(t)
			ctx := s.GetContext()
			end := s.periodEnd.AddDate(0, 1, 0)
			s.sub.CurrentPeriodStart, s.sub.CurrentPeriodEnd = s.periodEnd, end
			require.NoError(t, s.GetStores().SubscriptionRepo.Update(ctx, s.sub))
			line := s.createLineItem(ctx, "line_multi_period", s.periodStart, end)
			owner := s.newCommitment("comm_multi_period", types.CommitmentScopeSubscription, s.sub.ID, "100")
			firstUsage, secondUsage := float64(12000), float64(2000)
			wantCommitted, wantUsed, wantUnused := "200", "120", "80"
			if tc.quantity {
				owner.Type = types.COMMITMENT_TYPE_QUANTITY
				owner.ScopeEntityType, owner.ScopeEntityID = types.CommitmentScopePrice, line.PriceID
				owner.CommittedPriceID = lo.ToPtr(line.PriceID)
				firstUsage, secondUsage = 120, 20
				wantCommitted, wantUsed, wantUnused = "2", "1.2", "0.8"
			}
			if tc.scheduled {
				owner.CollectionMethod = lo.ToPtr(types.CommitmentCollectionBillingTerms)
				svc.BillingTerms = &analyticsTermsService{state: &analyticsTermsState{
					owner: &domain.OwnerSnapshot{Owner: domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: owner.ID},
						TenantID: owner.TenantID, EnvironmentID: owner.EnvironmentID, CustomerID: owner.CustomerID, SubscriptionID: &owner.SubscriptionID},
					summary: &domain.CollectionSummary{Currency: "usd", TargetAmount: dec("600"), InvoicedAmount: dec("100")},
				}}
			}
			s.storeCommitment(owner)
			s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, s.periodStart.AddDate(0, 0, 5), firstUsage)
			s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, s.periodEnd, secondUsage)
			params := commitmentAnalyticsParams(s)
			params.EndTime = end
			if tc.partialEnd {
				params.EndTime = s.periodEnd.AddDate(0, 0, 14)
				s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID, params.EndTime, 99999)
			}
			page, err := svc.commitmentAnalytics(ctx, newCommitmentAnalyticsQuery(params, []string{s.customer.ID}, 50, ""))
			require.NoError(t, err)
			require.Len(t, page.Entries(), 1)
			usage := page.Entries()[0].Usage()
			require.NotNil(t, usage)
			require.Equal(t, wantCommitted, usage.Committed.Amount.String())
			require.Equal(t, wantUsed, usage.Used.Amount.String())
			require.Equal(t, wantUnused, usage.Unused.Amount.String())
			require.Equal(t, "200", usage.Committed.Quantity.String())
			require.Equal(t, "120", usage.Used.Quantity.String())
			require.Equal(t, "80", usage.Unused.Quantity.String())
			require.True(t, usage.AppliesFrom.Equal(s.periodStart))
			require.True(t, usage.AppliesTo.Equal(end))
			if tc.scheduled {
				require.False(t, usage.TrueUpEnabled)
				require.True(t, usage.TrueUpAmount.IsZero())
				require.True(t, usage.BilledAmount.IsZero())
				require.Equal(t, "100", page.Entries()[0].Collection().InvoicedAmount.String())
			} else {
				require.Equal(t, wantUnused, usage.TrueUpAmount.String())
				require.Equal(t, wantCommitted, usage.BilledAmount.String())
			}
		})
	}
}

func TestCommitmentAnalyticsPeriodBudget(t *testing.T) {
	s, _ := newCommitmentAnalyticsFixture(t)
	owner := s.newCommitment("comm_budget", types.CommitmentScopeSubscription, s.sub.ID, "100")
	s.sub.BillingPeriod = types.BILLING_PERIOD_DAILY
	owner.EndDate = s.periodStart.AddDate(0, 0, 501)
	owners := []*commitment.Commitment{owner, owner}
	periods, err := analyticsCommitmentPeriods(s.sub, owners, s.periodStart, owner.EndDate.AddDate(0, 0, -1), commitmentAnalyticsPeriodLimit)
	require.NoError(t, err)
	require.Len(t, periods, 500, "multiple owners share each subscription period")
	_, err = analyticsCommitmentPeriods(s.sub, owners, s.periodStart, owner.EndDate, commitmentAnalyticsPeriodLimit)
	require.True(t, ierr.IsValidation(err))
	_, err = analyticsCommitmentPeriods(s.sub, owners, s.periodStart, s.periodStart.AddDate(0, 0, 2), 1)
	require.True(t, ierr.IsValidation(err), "a later subscription shares the remaining request budget")
	owner.StartDate, owner.EndDate = s.periodStart.AddDate(0, 0, 10), s.periodStart.AddDate(0, 0, 11)
	periods, err = analyticsCommitmentPeriods(s.sub, owners, s.periodStart, s.periodStart.AddDate(0, 0, 501), 1)
	require.NoError(t, err)
	require.Len(t, periods, 1, "periods without applicable commitments need no usage reads")
	require.True(t, periods[0].Start.Equal(owner.StartDate))
	cancelled := owner.StartDate
	s.sub.CancelledAt = &cancelled
	periods, err = analyticsCommitmentPeriods(s.sub, owners, s.periodStart, owner.EndDate, 1)
	require.NoError(t, err)
	require.Empty(t, periods)
}
