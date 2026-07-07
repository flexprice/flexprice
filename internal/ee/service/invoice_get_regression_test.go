package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// InvoiceGetRegressionSuite pins GetInvoice guard behavior for malformed
// subscription invoices. Regression for a nil-pointer panic: GetInvoice
// dereferenced *inv.SubscriptionID for SUBSCRIPTION-type invoices without a
// nil check, so an invoice missing its subscription reference crashed the
// read path instead of returning a typed error.
type InvoiceGetRegressionSuite struct {
	testutil.BaseServiceTestSuite
	service InvoiceService
}

func TestInvoiceGetRegression(t *testing.T) {
	suite.Run(t, new(InvoiceGetRegressionSuite))
}

func (s *InvoiceGetRegressionSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewInvoiceService(newTestServiceParams(&s.BaseServiceTestSuite))
}

func (s *InvoiceGetRegressionSuite) createInvoiceFixture(id string, subscriptionID *string) *invoice.Invoice {
	cust := &customer.Customer{
		ID:        "cust_" + id,
		Name:      "Regression Customer",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), cust))

	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      cust.ID,
		SubscriptionID:  subscriptionID,
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		Subtotal:        decimal.Zero,
		Total:           decimal.Zero,
		AmountDue:       decimal.Zero,
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.Zero,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
	return inv
}

func (s *InvoiceGetRegressionSuite) TestGetInvoiceSubscriptionTypeWithoutSubscriptionReference() {
	testCases := []struct {
		name           string
		invoiceID      string
		subscriptionID *string
	}{
		{
			name:           "nil_subscription_id_returns_internal_error_not_panic",
			invoiceID:      "inv_regr_nil_sub_id",
			subscriptionID: nil,
		},
		{
			name:           "empty_subscription_id_returns_internal_error_not_panic",
			invoiceID:      "inv_regr_empty_sub_id",
			subscriptionID: lo.ToPtr(""),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			inv := s.createInvoiceFixture(tc.invoiceID, tc.subscriptionID)

			resp, err := s.service.GetInvoice(s.GetContext(), inv.ID)

			s.Error(err)
			s.Nil(resp)
			s.True(ierr.IsInternal(err), "expected internal error, got: %v", err)

			// The invoice itself must remain readable at the repo level —
			// the guard rejects the read, it must not mutate anything.
			stored, repoErr := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
			s.NoError(repoErr)
			s.Equal(types.InvoiceTypeSubscription, stored.InvoiceType)
		})
	}
}
