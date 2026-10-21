package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type InvoiceEventDeletionGateSuite struct {
	testutil.BaseServiceTestSuite
	svc         *invoiceService
	requestRepo *testutil.InMemoryEventDeletionRequestStore
	periodStart time.Time
	periodEnd   time.Time
}

func TestInvoiceEventDeletionGate(t *testing.T) {
	suite.Run(t, new(InvoiceEventDeletionGateSuite))
}

func (s *InvoiceEventDeletionGateSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.periodStart = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s.periodEnd = time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)
	s.requestRepo = testutil.NewInMemoryEventDeletionRequestStore()

	s.svc = NewInvoiceService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		Locker:                   s.GetLocker(),
		CustomerRepo:             s.GetStores().CustomerRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:      s.GetStores().InvoiceLineItemRepo,
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                 s.GetStores().PlanRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		WalletRepo:               s.GetStores().WalletRepo,
		SettingsRepo:             s.GetStores().SettingsRepo,
		CheckoutSessionRepo:      s.GetStores().CheckoutSessionRepo,
		EnvironmentRepo:          s.GetStores().EnvironmentRepo,
		TenantRepo:               s.GetStores().TenantRepo,
		EventDeletionRequestRepo: s.requestRepo,
	}).(*invoiceService)
}

func (s *InvoiceEventDeletionGateSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.requestRepo.Clear()
}

func (s *InvoiceEventDeletionGateSuite) seedCustomer(id, externalID string) {
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), &customer.Customer{
		ID:         id,
		ExternalID: externalID,
		Name:       id,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}))
}

func (s *InvoiceEventDeletionGateSuite) seedDeleting(externalCustomerID string, start, end time.Time) {
	s.NoError(s.requestRepo.CreateBulk(s.GetContext(), []*events.DeletionRequest{{
		ID:                 "evtdel_" + externalCustomerID,
		RequestID:          "req_live",
		ExternalCustomerID: externalCustomerID,
		PeriodStart:        start,
		PeriodEnd:          end,
		DeletionStatus:     types.EventDeletionStatusDeleting,
		StartedAt:          start,
	}}))
}

func (s *InvoiceEventDeletionGateSuite) draftInvoice(
	id, customerID string,
	subscriptionCustomerID *string,
	periodStart, periodEnd time.Time,
) *invoice.Invoice {
	computed := periodEnd.AddDate(0, 0, 1)
	inv := &invoice.Invoice{
		ID:                     id,
		CustomerID:             customerID,
		SubscriptionCustomerID: subscriptionCustomerID,
		InvoiceType:            types.InvoiceTypeOneOff,
		InvoiceStatus:          types.InvoiceStatusDraft,
		PaymentStatus:          types.PaymentStatusPending,
		BillingReason:          string(types.InvoiceBillingReasonManual),
		Currency:               "usd",
		AmountDue:              decimal.NewFromInt(10),
		AmountRemaining:        decimal.NewFromInt(10),
		Total:                  decimal.NewFromInt(10),
		Subtotal:               decimal.NewFromInt(10),
		PeriodStart:            &periodStart,
		PeriodEnd:              &periodEnd,
		LastComputedAt:         &computed,
		BaseModel:              types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))
	return inv
}

func (s *InvoiceEventDeletionGateSuite) TestGate_HoldsAnInvoiceOverTheDeletedPeriod() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedDeleting("ext_1", s.periodStart, s.periodEnd)
	inv := s.draftInvoice("inv_1", "cust_1", nil, s.periodStart, s.periodEnd)

	gated, err := s.svc.isInvoiceGatedOnEventDeletion(s.GetContext(), inv)

	s.NoError(err)
	s.Require().NotNil(gated)
	s.Equal("req_live", gated.RequestID)
}

func (s *InvoiceEventDeletionGateSuite) TestGate_OverlapShapes() {
	tests := []struct {
		name        string
		periodStart time.Time
		periodEnd   time.Time
		gated       bool
	}{
		{
			name:        "invoice period identical",
			periodStart: s.periodStart,
			periodEnd:   s.periodEnd,
			gated:       true,
		},
		{
			name:        "invoice starts before and ends inside",
			periodStart: s.periodStart.AddDate(0, 0, -10),
			periodEnd:   s.periodStart.AddDate(0, 0, 5),
			gated:       true,
		},
		{
			name:        "invoice starts inside and ends after",
			periodStart: s.periodEnd.AddDate(0, 0, -5),
			periodEnd:   s.periodEnd.AddDate(0, 0, 10),
			gated:       true,
		},
		{
			name:        "invoice contains the deleted period",
			periodStart: s.periodStart.AddDate(0, 0, -10),
			periodEnd:   s.periodEnd.AddDate(0, 0, 10),
			gated:       true,
		},
		{
			// A March deletion must not hold a January invoice.
			name:        "invoice entirely before",
			periodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			periodEnd:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			gated:       false,
		},
		{
			name:        "invoice entirely after",
			periodStart: s.periodEnd.AddDate(0, 0, 1),
			periodEnd:   s.periodEnd.AddDate(0, 0, 20),
			gated:       false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.seedCustomer("cust_1", "ext_1")
			s.seedDeleting("ext_1", s.periodStart, s.periodEnd)
			inv := s.draftInvoice("inv_1", "cust_1", nil, tt.periodStart, tt.periodEnd)

			gated, err := s.svc.isInvoiceGatedOnEventDeletion(s.GetContext(), inv)

			s.NoError(err)
			if tt.gated {
				s.NotNil(gated)
				return
			}
			s.Nil(gated)
		})
	}
}

// A delegated_invoicing subscription puts the invoicing customer on the invoice,
// not the one that owns the usage, so the gate has to read both columns.
func (s *InvoiceEventDeletionGateSuite) TestGate_MatchesOnSubscriptionCustomer() {
	s.seedCustomer("cust_payer", "ext_payer")
	s.seedCustomer("cust_usage", "ext_usage")
	s.seedDeleting("ext_usage", s.periodStart, s.periodEnd)

	inv := s.draftInvoice("inv_1", "cust_payer", lo.ToPtr("cust_usage"), s.periodStart, s.periodEnd)

	gated, err := s.svc.isInvoiceGatedOnEventDeletion(s.GetContext(), inv)

	s.NoError(err)
	s.Require().NotNil(gated)
	s.Equal("ext_usage", gated.ExternalCustomerID)
}

func (s *InvoiceEventDeletionGateSuite) TestGate_IgnoresAnotherCustomersDeletion() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedCustomer("cust_2", "ext_2")
	s.seedDeleting("ext_2", s.periodStart, s.periodEnd)

	inv := s.draftInvoice("inv_1", "cust_1", nil, s.periodStart, s.periodEnd)

	gated, err := s.svc.isInvoiceGatedOnEventDeletion(s.GetContext(), inv)

	s.NoError(err)
	s.Nil(gated)
}

func (s *InvoiceEventDeletionGateSuite) TestGate_ReleasesOnceTheDeletionIsTerminal() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedDeleting("ext_1", s.periodStart, s.periodEnd)
	inv := s.draftInvoice("inv_1", "cust_1", nil, s.periodStart, s.periodEnd)

	gated, err := s.svc.isInvoiceGatedOnEventDeletion(s.GetContext(), inv)
	s.NoError(err)
	s.NotNil(gated)

	for _, status := range []types.EventDeletionStatus{
		types.EventDeletionStatusCompleted,
		types.EventDeletionStatusFailed,
	} {
		s.Run(string(status), func() {
			s.SetupTest()
			s.seedCustomer("cust_1", "ext_1")
			s.seedDeleting("ext_1", s.periodStart, s.periodEnd)
			inv := s.draftInvoice("inv_1", "cust_1", nil, s.periodStart, s.periodEnd)

			s.NoError(s.requestRepo.MarkTerminal(s.GetContext(), []string{"evtdel_ext_1"}, status,
				types.EventDeletionReasonMutationFailed))

			gated, err := s.svc.isInvoiceGatedOnEventDeletion(s.GetContext(), inv)
			s.NoError(err)
			s.Nil(gated, "a terminal deletion must not hold invoicing")
		})
	}
}

func (s *InvoiceEventDeletionGateSuite) TestGate_IgnoresAnInvoiceWithNoPeriod() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedDeleting("ext_1", s.periodStart, s.periodEnd)

	inv := s.draftInvoice("inv_1", "cust_1", nil, s.periodStart, s.periodEnd)
	inv.PeriodStart = nil
	inv.PeriodEnd = nil

	gated, err := s.svc.isInvoiceGatedOnEventDeletion(s.GetContext(), inv)

	s.NoError(err)
	s.Nil(gated)
}

// The cron path defers through the existing clean-exit channel rather than
// erroring, so a held invoice does not burn its retries.
func (s *InvoiceEventDeletionGateSuite) TestIsFinalizationDue_FalseWhileADeletionIsInProgress() {
	s.seedCustomer("cust_1", "ext_1")
	s.seedDeleting("ext_1", s.periodStart, s.periodEnd)
	inv := s.draftInvoice("inv_1", "cust_1", nil, s.periodStart, s.periodEnd)

	due, err := s.svc.IsFinalizationDue(s.GetContext(), inv.ID)

	s.NoError(err, "the cron must not see an error, only a deferral")
	s.False(due)
}

func (s *InvoiceEventDeletionGateSuite) TestLockInvoiceCustomersForEventDeletion_TakesTheSharedKey() {
	s.seedCustomer("cust_payer", "ext_payer")
	s.seedCustomer("cust_usage", "ext_usage")
	inv := s.draftInvoice("inv_1", "cust_payer", lo.ToPtr("cust_usage"), s.periodStart, s.periodEnd)

	// The mock client's advisory lock is a no-op, so this asserts the call path
	// resolves both customer columns without erroring.
	s.NoError(s.svc.lockInvoiceCustomersForEventDeletion(s.GetContext(), inv))

	externalIDs, err := s.svc.invoiceExternalCustomerIDs(s.GetContext(), inv)
	s.NoError(err)
	s.ElementsMatch([]string{"ext_payer", "ext_usage"}, externalIDs)
}
