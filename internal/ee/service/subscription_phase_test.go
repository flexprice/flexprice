package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type SubscriptionPhaseServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  SubscriptionPhaseService
	testData struct {
		subscription *subscription.Subscription
	}
}

func TestSubscriptionPhaseService(t *testing.T) {
	suite.Run(t, new(SubscriptionPhaseServiceSuite))
}

func (s *SubscriptionPhaseServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *SubscriptionPhaseServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionPhaseServiceSuite) setupService() {
	s.service = NewSubscriptionPhaseService(ServiceParams{
		Logger:                s.GetLogger(),
		Config:                s.GetConfig(),
		DB:                    s.GetDB(),
		SubRepo:               s.GetStores().SubscriptionRepo,
		SubscriptionPhaseRepo: s.GetStores().SubscriptionPhaseRepo,
		WebhookPublisher:      s.GetWebhookPublisher(),
	})
}

func (s *SubscriptionPhaseServiceSuite) setupTestData() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create a test subscription with published status
	s.testData.subscription = &subscription.Subscription{
		ID:                 "sub_test_phase",
		CustomerID:         "cust_test",
		PlanID:             "plan_test",
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		StartDate:          time.Now().UTC(),
		BillingAnchor:      time.Now().UTC(),
		BillingCycle:       types.BillingCycleAnniversary,
		CurrentPeriodStart: time.Now().UTC(),
		CurrentPeriodEnd:   time.Now().UTC().Add(30 * 24 * time.Hour),
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Version:            1,
		EnvironmentID:      "test-env-id",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.testData.subscription.Status = types.StatusPublished
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.testData.subscription))
}

func (s *SubscriptionPhaseServiceSuite) TestCreateSubscriptionPhase_Success() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	startDate := time.Now().UTC()
	endDate := startDate.Add(30 * 24 * time.Hour)
	req := dto.CreateSubscriptionPhaseRequest{
		SubscriptionID: s.testData.subscription.ID,
		StartDate:      &startDate,
		EndDate:        &endDate,
		Metadata: types.Metadata{
			"key1": "value1",
			"key2": "value2",
		},
	}

	resp, err := s.service.CreateSubscriptionPhase(ctx, req)
	s.NoError(err)
	s.NotNil(resp)
	s.NotEmpty(resp.ID)
	s.Equal(s.testData.subscription.ID, resp.SubscriptionID)
	s.Equal(startDate.Truncate(time.Second), resp.StartDate.Truncate(time.Second))
	s.NotNil(resp.EndDate)
	s.Equal(endDate.Truncate(time.Second), resp.EndDate.Truncate(time.Second))
	s.Equal(req.Metadata, resp.Metadata)

	// Verify phase was created in repository
	phase, err := s.GetStores().SubscriptionPhaseRepo.Get(ctx, resp.ID)
	s.NoError(err)
	s.NotNil(phase)
	s.Equal(resp.ID, phase.ID)
}

func (s *SubscriptionPhaseServiceSuite) TestCreateSubscriptionPhase_WithoutDates() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create phase without start date - should return validation error
	req := dto.CreateSubscriptionPhaseRequest{
		SubscriptionID: s.testData.subscription.ID,
		Metadata: types.Metadata{
			"key": "value",
		},
	}

	resp, err := s.service.CreateSubscriptionPhase(ctx, req)
	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionPhaseServiceSuite) TestCreateSubscriptionPhase_ValidationError_EmptySubscriptionID() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	req := dto.CreateSubscriptionPhaseRequest{
		SubscriptionID: "", // Empty subscription ID
	}

	resp, err := s.service.CreateSubscriptionPhase(ctx, req)
	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionPhaseServiceSuite) TestCreateSubscriptionPhase_ValidationError_EndDateBeforeStartDate() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	startDate := time.Now().UTC()
	endDate := startDate.Add(-24 * time.Hour) // End date before start date
	req := dto.CreateSubscriptionPhaseRequest{
		SubscriptionID: s.testData.subscription.ID,
		StartDate:      &startDate,
		EndDate:        &endDate,
	}

	resp, err := s.service.CreateSubscriptionPhase(ctx, req)
	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionPhaseServiceSuite) TestCreateSubscriptionPhase_SubscriptionNotFound() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	startDate := time.Now().UTC()
	req := dto.CreateSubscriptionPhaseRequest{
		SubscriptionID: "non-existent-subscription",
		StartDate:      &startDate,
	}

	resp, err := s.service.CreateSubscriptionPhase(ctx, req)
	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsNotFound(err))
}

func (s *SubscriptionPhaseServiceSuite) TestCreateSubscriptionPhase_UnpublishedSubscription() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create an unpublished subscription
	unpublishedSub := &subscription.Subscription{
		ID:                 "sub_unpublished",
		CustomerID:         "cust_test",
		PlanID:             "plan_test",
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		StartDate:          time.Now().UTC(),
		BillingAnchor:      time.Now().UTC(),
		BillingCycle:       types.BillingCycleAnniversary,
		CurrentPeriodStart: time.Now().UTC(),
		CurrentPeriodEnd:   time.Now().UTC().Add(30 * 24 * time.Hour),
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Version:            1,
		EnvironmentID:      "test-env-id",
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	unpublishedSub.Status = "" // Not published (empty status)
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, unpublishedSub))

	req := dto.CreateSubscriptionPhaseRequest{
		SubscriptionID: unpublishedSub.ID,
	}

	resp, err := s.service.CreateSubscriptionPhase(ctx, req)
	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionPhaseServiceSuite) TestGetSubscriptionPhase_Success() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create a phase first
	phase := &subscription.SubscriptionPhase{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
		SubscriptionID: s.testData.subscription.ID,
		StartDate:      time.Now().UTC(),
		EndDate:        lo.ToPtr(time.Now().UTC().Add(30 * 24 * time.Hour)),
		Metadata:       types.Metadata{"key": "value"},
		EnvironmentID:  "test-env-id",
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionPhaseRepo.Create(ctx, phase))

	resp, err := s.service.GetSubscriptionPhase(ctx, phase.ID)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(phase.ID, resp.ID)
	s.Equal(phase.SubscriptionID, resp.SubscriptionID)
	s.Equal(phase.StartDate.Truncate(time.Second), resp.StartDate.Truncate(time.Second))
	s.Equal(phase.Metadata, resp.Metadata)
}

func (s *SubscriptionPhaseServiceSuite) TestGetSubscriptionPhase_EmptyID() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	resp, err := s.service.GetSubscriptionPhase(ctx, "")
	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionPhaseServiceSuite) TestGetSubscriptionPhase_NotFound() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	resp, err := s.service.GetSubscriptionPhase(ctx, "non-existent-phase-id")
	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsNotFound(err))
}

func (s *SubscriptionPhaseServiceSuite) TestGetSubscriptionPhases_Success() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create multiple phases
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		phase := &subscription.SubscriptionPhase{
			ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
			SubscriptionID: s.testData.subscription.ID,
			StartDate:      now.Add(time.Duration(i) * 24 * time.Hour),
			EndDate:        lo.ToPtr(now.Add(time.Duration(i+1) * 24 * time.Hour)),
			EnvironmentID:  "test-env-id",
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().SubscriptionPhaseRepo.Create(ctx, phase))
	}

	filter := types.NewSubscriptionPhaseFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}

	resp, err := s.service.GetSubscriptionPhases(ctx, filter)
	s.NoError(err)
	s.NotNil(resp)
	s.NotNil(resp.Items)
	s.Len(resp.Items, 5)
	s.NotNil(resp.Pagination)
	s.Equal(5, resp.Pagination.Total)
}

func (s *SubscriptionPhaseServiceSuite) TestGetSubscriptionPhases_WithPagination() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create multiple phases
	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		phase := &subscription.SubscriptionPhase{
			ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
			SubscriptionID: s.testData.subscription.ID,
			StartDate:      now.Add(time.Duration(i) * 24 * time.Hour),
			EnvironmentID:  "test-env-id",
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().SubscriptionPhaseRepo.Create(ctx, phase))
	}

	filter := types.NewSubscriptionPhaseFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}
	filter.Limit = lo.ToPtr(5)
	filter.Offset = lo.ToPtr(0)

	resp, err := s.service.GetSubscriptionPhases(ctx, filter)
	s.NoError(err)
	s.NotNil(resp)
	s.NotNil(resp.Items)
	s.Len(resp.Items, 5)
	s.NotNil(resp.Pagination)
	s.Equal(10, resp.Pagination.Total)
	s.Equal(5, resp.Pagination.Limit)
}

func (s *SubscriptionPhaseServiceSuite) TestGetSubscriptionPhases_NilFilter() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create a phase
	phase := &subscription.SubscriptionPhase{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
		SubscriptionID: s.testData.subscription.ID,
		StartDate:      time.Now().UTC(),
		EnvironmentID:  "test-env-id",
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionPhaseRepo.Create(ctx, phase))

	resp, err := s.service.GetSubscriptionPhases(ctx, nil)
	s.NoError(err)
	s.NotNil(resp)
	s.NotNil(resp.Items)
}

func (s *SubscriptionPhaseServiceSuite) TestUpdateSubscriptionPhase_Success() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create a phase first
	phase := &subscription.SubscriptionPhase{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
		SubscriptionID: s.testData.subscription.ID,
		StartDate:      time.Now().UTC(),
		EndDate:        lo.ToPtr(time.Now().UTC().Add(30 * 24 * time.Hour)),
		Metadata:       types.Metadata{"old_key": "old_value"},
		EnvironmentID:  "test-env-id",
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionPhaseRepo.Create(ctx, phase))

	// Update metadata
	newMetadata := types.Metadata{
		"new_key": "new_value",
		"key2":    "value2",
	}
	req := dto.UpdateSubscriptionPhaseRequest{
		Metadata: &newMetadata,
	}

	resp, err := s.service.UpdateSubscriptionPhase(ctx, phase.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(phase.ID, resp.ID)
	s.Equal(newMetadata, resp.Metadata)
	s.NotEmpty(resp.UpdatedAt)
	s.NotEmpty(resp.UpdatedBy)

	// Verify update in repository
	updatedPhase, err := s.GetStores().SubscriptionPhaseRepo.Get(ctx, phase.ID)
	s.NoError(err)
	s.Equal(newMetadata, updatedPhase.Metadata)
}

func (s *SubscriptionPhaseServiceSuite) TestUpdateSubscriptionPhase_EmptyID() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	req := dto.UpdateSubscriptionPhaseRequest{}

	resp, err := s.service.UpdateSubscriptionPhase(ctx, "", req)
	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionPhaseServiceSuite) TestUpdateSubscriptionPhase_NotFound() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	metadata := types.Metadata{"key": "value"}
	req := dto.UpdateSubscriptionPhaseRequest{
		Metadata: &metadata,
	}

	resp, err := s.service.UpdateSubscriptionPhase(ctx, "non-existent-phase-id", req)
	s.Error(err)
	s.Nil(resp)
	s.True(ierr.IsNotFound(err))
}

func (s *SubscriptionPhaseServiceSuite) TestUpdateSubscriptionPhase_NilMetadata() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create a phase first
	phase := &subscription.SubscriptionPhase{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
		SubscriptionID: s.testData.subscription.ID,
		StartDate:      time.Now().UTC(),
		Metadata:       types.Metadata{"key": "value"},
		EnvironmentID:  "test-env-id",
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionPhaseRepo.Create(ctx, phase))

	req := dto.UpdateSubscriptionPhaseRequest{
		Metadata: nil, // No metadata update
	}

	resp, err := s.service.UpdateSubscriptionPhase(ctx, phase.ID, req)
	s.NoError(err)
	s.NotNil(resp)
	// Metadata should remain unchanged
	s.Equal(phase.Metadata, resp.Metadata)
}

func (s *SubscriptionPhaseServiceSuite) TestDeleteSubscriptionPhase_Success() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create a published phase
	phase := &subscription.SubscriptionPhase{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
		SubscriptionID: s.testData.subscription.ID,
		StartDate:      time.Now().UTC(),
		EndDate:        lo.ToPtr(time.Now().UTC().Add(30 * 24 * time.Hour)),
		EnvironmentID:  "test-env-id",
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	phase.Status = types.StatusPublished
	s.NoError(s.GetStores().SubscriptionPhaseRepo.Create(ctx, phase))

	err := s.service.DeleteSubscriptionPhase(ctx, phase.ID)
	s.NoError(err)

	// Verify phase was deleted
	_, err = s.GetStores().SubscriptionPhaseRepo.Get(ctx, phase.ID)
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *SubscriptionPhaseServiceSuite) TestDeleteSubscriptionPhase_EmptyID() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	err := s.service.DeleteSubscriptionPhase(ctx, "")
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *SubscriptionPhaseServiceSuite) TestDeleteSubscriptionPhase_NotFound() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	err := s.service.DeleteSubscriptionPhase(ctx, "non-existent-phase-id")
	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *SubscriptionPhaseServiceSuite) TestDeleteSubscriptionPhase_Unpublished() {
	ctx := s.GetContext()
	ctx = types.SetEnvironmentID(ctx, "test-env-id")

	// Create an unpublished phase
	phase := &subscription.SubscriptionPhase{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
		SubscriptionID: s.testData.subscription.ID,
		StartDate:      time.Now().UTC(),
		EnvironmentID:  "test-env-id",
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	phase.Status = "" // Not published (empty status)
	s.NoError(s.GetStores().SubscriptionPhaseRepo.Create(ctx, phase))

	err := s.service.DeleteSubscriptionPhase(ctx, phase.ID)
	s.Error(err)
	s.True(ierr.IsNotFound(err)) // Returns NotFound error for unpublished phase
}
