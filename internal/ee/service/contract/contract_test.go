package contract

import (
	"context"
	"testing"
	"time"

	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestDeriveContractStatus(t *testing.T) {
	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	cancelledAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		existing      types.ContractStatus
		subStatus     types.SubscriptionStatus
		cancelledAt   *time.Time
		wantStatus    types.ContractStatus
		wantTermStart *time.Time
		wantEndedAt   *time.Time
	}{
		{name: "draft stays draft while subscription is draft", existing: types.ContractStatusDraft, subStatus: types.SubscriptionStatusDraft, wantStatus: types.ContractStatusDraft},
		{name: "draft stays draft while trialing", existing: types.ContractStatusDraft, subStatus: types.SubscriptionStatusTrialing, wantStatus: types.ContractStatusDraft},
		{name: "draft stays draft while incomplete", existing: types.ContractStatusDraft, subStatus: types.SubscriptionStatusIncomplete, wantStatus: types.ContractStatusDraft},
		{name: "draft activates on first activation", existing: types.ContractStatusDraft, subStatus: types.SubscriptionStatusActive, wantStatus: types.ContractStatusActive, wantTermStart: &start},
		{name: "draft activates when sync first sees a paused subscription", existing: types.ContractStatusDraft, subStatus: types.SubscriptionStatusPaused, wantStatus: types.ContractStatusActive, wantTermStart: &start},
		{name: "draft cancels when cancelled before activation", existing: types.ContractStatusDraft, subStatus: types.SubscriptionStatusCancelled, wantStatus: types.ContractStatusCancelled},
		{name: "draft cancels when cancelled at its start", existing: types.ContractStatusDraft, subStatus: types.SubscriptionStatusCancelled, cancelledAt: &start, wantStatus: types.ContractStatusCancelled},
		{name: "draft ends when first synced after a cancel past its start", existing: types.ContractStatusDraft, subStatus: types.SubscriptionStatusCancelled, cancelledAt: &cancelledAt, wantStatus: types.ContractStatusEnded, wantTermStart: &start, wantEndedAt: &cancelledAt},
		{name: "active stays active on incomplete", existing: types.ContractStatusActive, subStatus: types.SubscriptionStatusIncomplete, wantStatus: types.ContractStatusActive},
		{name: "active stays active on pause", existing: types.ContractStatusActive, subStatus: types.SubscriptionStatusPaused, wantStatus: types.ContractStatusActive},
		{name: "active ends on cancel at cancelled_at", existing: types.ContractStatusActive, subStatus: types.SubscriptionStatusCancelled, cancelledAt: &cancelledAt, wantStatus: types.ContractStatusEnded, wantEndedAt: &cancelledAt},
		{name: "active ends on cancel at now without cancelled_at", existing: types.ContractStatusActive, subStatus: types.SubscriptionStatusCancelled, wantStatus: types.ContractStatusEnded, wantEndedAt: &now},
		{name: "ended is terminal", existing: types.ContractStatusEnded, subStatus: types.SubscriptionStatusActive, wantStatus: types.ContractStatusEnded},
		{name: "cancelled is terminal", existing: types.ContractStatusCancelled, subStatus: types.SubscriptionStatusActive, wantStatus: types.ContractStatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := &subscription.Subscription{
				SubscriptionStatus: tt.subStatus,
				StartDate:          start,
				CancelledAt:        tt.cancelledAt,
			}
			existing := domainContract.NewBuilder(nil).
				WithContractStatus(tt.existing).
				WithStatus(types.StatusPublished).
				Build()

			got := deriveContractStatus(sub, existing, now)

			require.Equal(t, tt.wantStatus, got.ContractStatus())
			require.Equal(t, types.StatusPublished, got.Status())
			require.Equal(t, tt.wantTermStart, got.TermStart(), "term_start")
			require.Equal(t, tt.wantEndedAt, got.EndedAt(), "ended_at")
		})
	}
}

type ContractServiceSuite struct {
	suite.Suite
	ctx             context.Context
	contractService ContractService
	contractRepo    *testutil.InMemoryContractStore
}

func TestContractService(t *testing.T) {
	suite.Run(t, new(ContractServiceSuite))
}

func (s *ContractServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.contractRepo = testutil.NewInMemoryContractStore()
	s.contractService = NewContractService(s.contractRepo)
}

func (s *ContractServiceSuite) newSubscription(status types.SubscriptionStatus) *subscription.Subscription {
	return &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         "cust_1",
		Currency:           "usd",
		SubscriptionStatus: status,
		StartDate:          time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		BaseModel:          types.GetDefaultBaseModel(s.ctx),
	}
}

func (s *ContractServiceSuite) countContracts() int {
	count, err := s.contractRepo.Count(s.ctx, contractRepo.NewFilter())
	s.Require().NoError(err)
	return count
}

func (s *ContractServiceSuite) TestSyncIsIdempotent() {
	sub := s.newSubscription(types.SubscriptionStatusActive)

	first, err := s.contractService.SyncForSubscription(s.ctx, sub)
	s.Require().NoError(err)
	second, err := s.contractService.SyncForSubscription(s.ctx, sub)
	s.Require().NoError(err)

	s.Equal(1, s.countContracts())
	s.Equal(first.ID(), second.ID())
	s.Equal(types.ContractStatusActive, second.ContractStatus())
	s.Equal("cust_1", second.CustomerID())
	s.Equal("usd", second.Currency())
}

func (s *ContractServiceSuite) TestSyncFollowsLifecycle() {
	sub := s.newSubscription(types.SubscriptionStatusDraft)

	c, err := s.contractService.SyncForSubscription(s.ctx, sub)
	s.Require().NoError(err)
	s.Equal(types.ContractStatusDraft, c.ContractStatus())
	s.Nil(c.TermStart())

	sub.SubscriptionStatus = types.SubscriptionStatusActive
	c, err = s.contractService.SyncForSubscription(s.ctx, sub)
	s.Require().NoError(err)
	s.Equal(types.ContractStatusActive, c.ContractStatus())
	s.Require().NotNil(c.TermStart())
	s.True(c.TermStart().Equal(sub.StartDate))

	cancelledAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	sub.SubscriptionStatus = types.SubscriptionStatusCancelled
	sub.CancelledAt = &cancelledAt
	c, err = s.contractService.SyncForSubscription(s.ctx, sub)
	s.Require().NoError(err)
	s.Equal(types.ContractStatusEnded, c.ContractStatus())
	s.Require().NotNil(c.EndedAt())
	s.True(c.EndedAt().Equal(cancelledAt))

	stored, err := s.contractRepo.GetBySubscriptionID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.ContractStatusEnded, stored.ContractStatus())
	s.Equal(1, s.countContracts())
}

func (s *ContractServiceSuite) TestArchiveForSubscription() {
	draft := s.newSubscription(types.SubscriptionStatusDraft)
	_, err := s.contractService.SyncForSubscription(s.ctx, draft)
	s.Require().NoError(err)
	active := s.newSubscription(types.SubscriptionStatusActive)
	_, err = s.contractService.SyncForSubscription(s.ctx, active)
	s.Require().NoError(err)

	s.Require().NoError(s.contractService.ArchiveForSubscription(s.ctx, draft.ID))
	s.Require().NoError(s.contractService.ArchiveForSubscription(s.ctx, active.ID))
	s.Require().NoError(s.contractService.ArchiveForSubscription(s.ctx, "subs_missing"))

	archived, err := s.contractRepo.GetBySubscriptionID(s.ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusArchived, archived.Status())
	kept, err := s.contractRepo.GetBySubscriptionID(s.ctx, active.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusPublished, kept.Status())
	s.Equal(1, s.countContracts())
}

func (s *ContractServiceSuite) TestGetContractIncludesAssociations() {
	sub := s.newSubscription(types.SubscriptionStatusActive)
	c, err := s.contractService.SyncForSubscription(s.ctx, sub)
	s.Require().NoError(err)

	resp, err := s.contractService.GetContract(s.ctx, c.ID())
	s.Require().NoError(err)
	s.Equal(c.ID(), resp.ID)
	s.Equal(types.ContractStatusActive, resp.ContractStatus)
	s.Require().Len(resp.Associations, 1)
	s.Equal(types.RevRecEntityTypeSubscription, resp.Associations[0].EntityType)
	s.Equal(sub.ID, resp.Associations[0].EntityID)
}

func (s *ContractServiceSuite) TestGetContractsFilters() {
	active := s.newSubscription(types.SubscriptionStatusActive)
	activeContract, err := s.contractService.SyncForSubscription(s.ctx, active)
	s.Require().NoError(err)
	draft := s.newSubscription(types.SubscriptionStatusDraft)
	draftContract, err := s.contractService.SyncForSubscription(s.ctx, draft)
	s.Require().NoError(err)

	tests := []struct {
		name    string
		filter  *contractRepo.Filter
		wantIDs []string
	}{
		{name: "no filter", filter: contractRepo.NewFilter(), wantIDs: []string{activeContract.ID(), draftContract.ID()}},
		{name: "by subscription", filter: &contractRepo.Filter{SubscriptionIDs: []string{draft.ID}}, wantIDs: []string{draftContract.ID()}},
		{name: "by contract status", filter: &contractRepo.Filter{ContractStatuses: []types.ContractStatus{types.ContractStatusActive}}, wantIDs: []string{activeContract.ID()}},
		{name: "by customer", filter: &contractRepo.Filter{CustomerIDs: []string{"cust_other"}}, wantIDs: []string{}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.contractService.GetContracts(s.ctx, tt.filter)
			s.Require().NoError(err)
			gotIDs := make([]string, 0, len(resp.Items))
			for _, item := range resp.Items {
				gotIDs = append(gotIDs, item.ID)
			}
			s.ElementsMatch(tt.wantIDs, gotIDs)
			s.Equal(len(tt.wantIDs), resp.Pagination.Total)
		})
	}
}
