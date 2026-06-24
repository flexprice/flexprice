package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type CheckoutSessionServiceSuite struct {
	testutil.BaseServiceTestSuite
	service      CheckoutSessionService
	sessionStore *testutil.InMemoryCheckoutSessionStore
}

func TestCheckoutSessionService(t *testing.T) {
	suite.Run(t, new(CheckoutSessionServiceSuite))
}

func (s *CheckoutSessionServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.sessionStore = testutil.NewInMemoryCheckoutSessionStore()
	s.service = NewCheckoutSessionService(ServiceParams{
		Logger:              s.GetLogger(),
		DB:                  s.GetDB(),
		CheckoutSessionRepo: s.sessionStore,
	})
}

func (s *CheckoutSessionServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.sessionStore.Clear()
}

func (s *CheckoutSessionServiceSuite) makeCreateReq() dto.CreateCheckoutSessionRequest {
	return dto.CreateCheckoutSessionRequest{
		CustomerID:      "cust_test",
		Action:          types.CheckoutActionCreateSubscription,
		PaymentProvider: types.CheckoutPaymentProviderStripe,
		Configuration: types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				PlanID:        "plan_test",
				Currency:      "usd",
				BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			},
		},
	}
}

func (s *CheckoutSessionServiceSuite) TestCreateCheckoutSession_Success() {
	resp, err := s.service.Create(s.GetContext(), s.makeCreateReq())
	s.NoError(err)
	s.NotNil(resp)
	s.Equal("cust_test", resp.CustomerID)
	s.Equal(types.CheckoutStatusInitiated, resp.CheckoutStatus)
	s.NotEmpty(resp.ID)
}

func (s *CheckoutSessionServiceSuite) TestCreateCheckoutSession_MissingCustomerID() {
	req := s.makeCreateReq()
	req.CustomerID = ""
	_, err := s.service.Create(s.GetContext(), req)
	s.Error(err)
	s.True(ierr.IsValidation(err), "expected validation error")
}

func (s *CheckoutSessionServiceSuite) TestCreateCheckoutSession_IdempotencyConflict() {
	req := s.makeCreateReq()
	req.IdempotencyKey = lo.ToPtr("key-123")

	_, err := s.service.Create(s.GetContext(), req)
	s.NoError(err)

	_, err = s.service.Create(s.GetContext(), req)
	s.Error(err)
	s.True(ierr.IsAlreadyExists(err), "expected already exists error")
}

func (s *CheckoutSessionServiceSuite) TestGetCheckoutSession_Success() {
	created, err := s.service.Create(s.GetContext(), s.makeCreateReq())
	s.NoError(err)

	got, err := s.service.Get(s.GetContext(), created.ID)
	s.NoError(err)
	s.Equal(created.ID, got.ID)
}

func (s *CheckoutSessionServiceSuite) TestGetCheckoutSession_NotFound() {
	_, err := s.service.Get(s.GetContext(), "nonexistent")
	s.Error(err)
	s.True(ierr.IsNotFound(err), "expected not found error")
}

func (s *CheckoutSessionServiceSuite) TestListCheckoutSessions_FilterByCustomer() {
	req := s.makeCreateReq()
	_, err := s.service.Create(s.GetContext(), req)
	s.NoError(err)

	req2 := s.makeCreateReq()
	req2.CustomerID = "cust_other"
	_, err = s.service.Create(s.GetContext(), req2)
	s.NoError(err)

	filter := types.NewDefaultCheckoutSessionFilter()
	filter.CustomerIDs = []string{"cust_test"}
	resp, err := s.service.List(s.GetContext(), filter)
	s.NoError(err)
	s.Len(resp.Items, 1)
	s.Equal("cust_test", resp.Items[0].CustomerID)
}

func (s *CheckoutSessionServiceSuite) TestUpdateCheckoutSession_StatusTransition() {
	created, err := s.service.Create(s.GetContext(), s.makeCreateReq())
	s.NoError(err)

	updated, err := s.service.Update(s.GetContext(), created.ID, dto.UpdateCheckoutSessionRequest{
		CheckoutStatus: lo.ToPtr(types.CheckoutStatusCompleted),
		CompletedAt:    lo.ToPtr(time.Now().UTC()),
	})
	s.NoError(err)
	s.Equal(types.CheckoutStatusCompleted, updated.CheckoutStatus)
	s.NotNil(updated.CompletedAt)
}

func (s *CheckoutSessionServiceSuite) TestUpdateCheckoutSession_ProviderResultDerivesPaymentAction() {
	created, err := s.service.Create(s.GetContext(), s.makeCreateReq())
	s.NoError(err)

	updated, err := s.service.Update(s.GetContext(), created.ID, dto.UpdateCheckoutSessionRequest{
		ProviderResult: &types.CheckoutProviderResult{
			CreateSubscriptionResult: &types.ProviderSubscriptionResult{
				SessionURL: "https://checkout.stripe.com/pay/cs_test_123",
			},
		},
	})
	s.NoError(err)
	s.NotNil(updated.PaymentAction)
	s.Equal(types.PaymentActionTypeCheckoutURL, updated.PaymentAction.Type)
	s.Equal("https://checkout.stripe.com/pay/cs_test_123", updated.PaymentAction.URL)
}

func (s *CheckoutSessionServiceSuite) TestDeleteCheckoutSession_SoftDelete() {
	created, err := s.service.Create(s.GetContext(), s.makeCreateReq())
	s.NoError(err)

	err = s.service.Delete(s.GetContext(), created.ID)
	s.NoError(err)

	got, err := s.sessionStore.Get(s.GetContext(), created.ID)
	s.NoError(err)
	s.Equal(types.StatusArchived, got.Status)
}
