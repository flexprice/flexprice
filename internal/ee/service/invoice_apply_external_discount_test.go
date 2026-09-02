package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type ApplyExternalInvoiceDiscountSuite struct {
	testutil.BaseServiceTestSuite
	service  InvoiceService
	testData struct {
		customer *customer.Customer
	}
}

func TestApplyExternalInvoiceDiscount(t *testing.T) {
	suite.Run(t, new(ApplyExternalInvoiceDiscountSuite))
}

func (s *ApplyExternalInvoiceDiscountSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *ApplyExternalInvoiceDiscountSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *ApplyExternalInvoiceDiscountSuite) setupService() {
	s.service = NewInvoiceService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditNoteRepo:             s.GetStores().CreditNoteRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CreditNoteLineItemRepo:     s.GetStores().CreditNoteLineItemRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		TaxAppliedRepo:             s.GetStores().TaxAppliedRepo,
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		WalletBalanceAlertPubSub:   types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	})
}

func (s *ApplyExternalInvoiceDiscountSuite) setupTestData() {
	s.BaseServiceTestSuite.ClearStores()
	s.testData.customer = &customer.Customer{
		ID:         "cust_ext_discount_test",
		ExternalID: "ext_cust_ext_discount_test",
		Name:       "External Discount Test Customer",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))
}

// createDraftInvoice creates a one-off draft invoice with a single line item, amountDue == amount,
// amountRemaining == amount (so it satisfies Invoice.Validate()'s AmountPaid+AmountRemaining==AmountDue).
func (s *ApplyExternalInvoiceDiscountSuite) createDraftInvoice(id string, amount decimal.Decimal, existingInvoiceLevelDiscount decimal.Decimal) *invoice.Invoice {
	li := &invoice.InvoiceLineItem{
		ID:                   s.GetUUID(),
		InvoiceID:            id,
		CustomerID:           s.testData.customer.ID,
		Amount:               amount,
		Quantity:             decimal.NewFromInt(1),
		Currency:             "usd",
		LineItemDiscount:     decimal.Zero,
		InvoiceLevelDiscount: existingInvoiceLevelDiscount,
		BaseModel:            types.GetDefaultBaseModel(s.GetContext()),
	}
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      s.testData.customer.ID,
		Currency:        "usd",
		Subtotal:        amount,
		Total:           amount.Sub(existingInvoiceLevelDiscount),
		TotalDiscount:   existingInvoiceLevelDiscount,
		AmountDue:       amount.Sub(existingInvoiceLevelDiscount),
		AmountRemaining: amount.Sub(existingInvoiceLevelDiscount),
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
		LineItems:       []*invoice.InvoiceLineItem{li},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(s.GetContext(), inv))
	return inv
}

func (s *ApplyExternalInvoiceDiscountSuite) TestFreshInvoice_NoExistingDiscount() {
	inv := s.createDraftInvoice("inv_fresh", decimal.NewFromInt(100), decimal.Zero)

	err := s.service.ApplyExternalInvoiceDiscount(s.GetContext(), inv.ID, dto.ApplyExternalInvoiceDiscountRequest{
		DiscountAmount: decimal.NewFromInt(20),
		MetadataKey:    "stripe_checkout_discounts",
		MetadataJSON:   `[{"stripe_coupon_id":"cp_1"}]`,
	})
	s.NoError(err)

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.NoError(err)
	s.True(decimal.NewFromInt(20).Equal(updated.TotalDiscount))
	s.True(decimal.NewFromInt(80).Equal(updated.Total))
	s.True(decimal.NewFromInt(80).Equal(updated.AmountDue))
	s.True(decimal.NewFromInt(80).Equal(updated.AmountRemaining))
	s.NoError(updated.Validate())

	items, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), inv.ID)
	s.NoError(err)
	s.Require().Len(items, 1)
	s.True(decimal.NewFromInt(20).Equal(items[0].InvoiceLevelDiscount))

	s.Equal(`[{"stripe_coupon_id":"cp_1"}]`, updated.Metadata["stripe_checkout_discounts"])
}

func (s *ApplyExternalInvoiceDiscountSuite) TestDoesNotClobberExistingInvoiceLevelDiscount() {
	// Invoice already has a $10 invoice-level discount from FlexPrice's own coupon engine.
	inv := s.createDraftInvoice("inv_existing_discount", decimal.NewFromInt(100), decimal.NewFromInt(10))

	err := s.service.ApplyExternalInvoiceDiscount(s.GetContext(), inv.ID, dto.ApplyExternalInvoiceDiscountRequest{
		DiscountAmount: decimal.NewFromInt(20),
		MetadataKey:    "stripe_checkout_discounts",
		MetadataJSON:   `[{"stripe_coupon_id":"cp_2"}]`,
	})
	s.NoError(err)

	items, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), inv.ID)
	s.NoError(err)
	s.Require().Len(items, 1)
	// Must be the CUMULATIVE total (10 existing + 20 new = 30), not just the new 20 —
	// this is the regression test for the DistributeInvoiceLevelDiscount reset bug.
	s.True(decimal.NewFromInt(30).Equal(items[0].InvoiceLevelDiscount))

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.NoError(err)
	s.True(decimal.NewFromInt(30).Equal(updated.TotalDiscount))
	s.True(decimal.NewFromInt(70).Equal(updated.Total))
	s.True(decimal.NewFromInt(70).Equal(updated.AmountDue))
	s.NoError(updated.Validate())
}

func (s *ApplyExternalInvoiceDiscountSuite) TestAppendsMetadataRatherThanOverwriting() {
	inv := s.createDraftInvoice("inv_second_payment", decimal.NewFromInt(200), decimal.Zero)

	s.NoError(s.service.ApplyExternalInvoiceDiscount(s.GetContext(), inv.ID, dto.ApplyExternalInvoiceDiscountRequest{
		DiscountAmount: decimal.NewFromInt(10),
		MetadataKey:    "stripe_checkout_discounts",
		MetadataJSON:   `[{"stripe_coupon_id":"cp_first"}]`,
	}))
	s.NoError(s.service.ApplyExternalInvoiceDiscount(s.GetContext(), inv.ID, dto.ApplyExternalInvoiceDiscountRequest{
		DiscountAmount: decimal.NewFromInt(5),
		MetadataKey:    "stripe_checkout_discounts",
		MetadataJSON:   `[{"stripe_coupon_id":"cp_second"}]`,
	}))

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.NoError(err)

	var entries []map[string]interface{}
	s.NoError(json.Unmarshal([]byte(updated.Metadata["stripe_checkout_discounts"]), &entries))
	s.Require().Len(entries, 2)
	s.Equal("cp_first", entries[0]["stripe_coupon_id"])
	s.Equal("cp_second", entries[1]["stripe_coupon_id"])

	s.True(decimal.NewFromInt(15).Equal(updated.TotalDiscount))
	s.True(decimal.NewFromInt(185).Equal(updated.Total))
	s.True(decimal.NewFromInt(185).Equal(updated.AmountDue))
}

func (s *ApplyExternalInvoiceDiscountSuite) TestZeroDiscountIsANoOp() {
	inv := s.createDraftInvoice("inv_zero", decimal.NewFromInt(50), decimal.Zero)

	err := s.service.ApplyExternalInvoiceDiscount(s.GetContext(), inv.ID, dto.ApplyExternalInvoiceDiscountRequest{
		DiscountAmount: decimal.Zero,
		MetadataKey:    "stripe_checkout_discounts",
	})
	s.NoError(err)

	updated, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), inv.ID)
	s.NoError(err)
	s.True(decimal.NewFromInt(50).Equal(updated.AmountDue))
	s.True(decimal.NewFromInt(50).Equal(updated.Total))
	s.True(decimal.Zero.Equal(updated.TotalDiscount))
}
