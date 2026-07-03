package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

// PlanCrudMoreServiceSuite fills CRUD edge-case coverage gaps in plan.go
// (validation, not-found, full-field updates, delete guards).
type PlanCrudMoreServiceSuite struct {
	testutil.BaseServiceTestSuite
	service PlanService
}

func TestPlanCrudMoreService(t *testing.T) {
	suite.Run(t, new(PlanCrudMoreServiceSuite))
}

func (s *PlanCrudMoreServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewPlanService(newTestServiceParams(&s.BaseServiceTestSuite))
}

func (s *PlanCrudMoreServiceSuite) createPlanFixture(id string) *plan.Plan {
	p := &plan.Plan{
		ID:        id,
		Name:      "Plan " + id,
		LookupKey: "lookup-" + id,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))
	return p
}

func (s *PlanCrudMoreServiceSuite) TestCreatePlanInvalidRequest() {
	// Missing name fails request validation before any repo write
	_, err := s.service.CreatePlan(s.GetContext(), dto.CreatePlanRequest{})
	s.Error(err)

	plans, listErr := s.GetStores().PlanRepo.List(s.GetContext(), types.NewNoLimitPlanFilter())
	s.NoError(listErr)
	s.Len(plans, 0, "no plan must be persisted on validation failure")
}

func (s *PlanCrudMoreServiceSuite) TestGetPlanValidation() {
	s.Run("empty_id_returns_validation_error", func() {
		_, err := s.service.GetPlan(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("unknown_id_returns_not_found", func() {
		_, err := s.service.GetPlan(s.GetContext(), "plan_nope")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

func (s *PlanCrudMoreServiceSuite) TestUpdatePlanAllFields() {
	p := s.createPlanFixture("plan_upd_all")

	newName := "Updated Name"
	newDescription := "Updated Description"
	newLookupKey := "updated-lookup"
	newDisplayOrder := 42

	resp, err := s.service.UpdatePlan(s.GetContext(), p.ID, dto.UpdatePlanRequest{
		Name:         &newName,
		Description:  &newDescription,
		LookupKey:    &newLookupKey,
		DisplayOrder: &newDisplayOrder,
		Metadata:     types.Metadata{"channel": "self-serve"},
	})
	s.NoError(err)
	s.Equal(newName, resp.Plan.Name)

	// Read back through the repo and verify every field persisted
	stored, err := s.GetStores().PlanRepo.Get(s.GetContext(), p.ID)
	s.NoError(err)
	s.Equal(newName, stored.Name)
	s.Equal(newDescription, stored.Description)
	s.Equal(newLookupKey, stored.LookupKey)
	s.Equal(42, lo.FromPtr(stored.DisplayOrder))
	s.Equal("self-serve", stored.Metadata["channel"])
}

func (s *PlanCrudMoreServiceSuite) TestUpdatePlanValidation() {
	name := "X"

	s.Run("empty_id_returns_validation_error", func() {
		_, err := s.service.UpdatePlan(s.GetContext(), "", dto.UpdatePlanRequest{Name: &name})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("unknown_id_returns_not_found", func() {
		_, err := s.service.UpdatePlan(s.GetContext(), "plan_nope", dto.UpdatePlanRequest{Name: &name})
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

func (s *PlanCrudMoreServiceSuite) TestDeletePlanGuards() {
	s.Run("empty_id_returns_validation_error", func() {
		err := s.service.DeletePlan(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("unknown_id_returns_not_found", func() {
		err := s.service.DeletePlan(s.GetContext(), "plan_nope")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("plan_with_active_subscription_cannot_be_deleted", func() {
		p := s.createPlanFixture("plan_del_active")
		now := time.Now().UTC()
		sub := &subscription.Subscription{
			ID:                 "sub_del_guard",
			PlanID:             p.ID,
			CustomerID:         "cust_del_guard",
			Currency:           "usd",
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusActive,
			SubscriptionType:   types.SubscriptionTypeStandalone,
			StartDate:          now.Add(-24 * time.Hour),
			CurrentPeriodStart: now.Add(-24 * time.Hour),
			CurrentPeriodEnd:   now.Add(29 * 24 * time.Hour),
			BillingAnchor:      now.Add(-24 * time.Hour),
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		}
		s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

		err := s.service.DeletePlan(s.GetContext(), p.ID)
		s.Error(err)
		s.True(ierr.IsInvalidOperation(err))

		// Plan must still exist
		stored, getErr := s.GetStores().PlanRepo.Get(s.GetContext(), p.ID)
		s.NoError(getErr)
		s.Equal(p.ID, stored.ID)
	})

	s.Run("plan_without_subscriptions_is_deleted", func() {
		p := s.createPlanFixture("plan_del_free")
		err := s.service.DeletePlan(s.GetContext(), p.ID)
		s.NoError(err)

		_, getErr := s.GetStores().PlanRepo.Get(s.GetContext(), p.ID)
		s.Error(getErr)
		s.True(ierr.IsNotFound(getErr))
	})
}
