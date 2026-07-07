package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_application"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	pdfdomain "github.com/flexprice/flexprice/internal/domain/pdf"
	"github.com/flexprice/flexprice/internal/domain/price"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/s3"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

// InvoicePDFSuite tests invoice PDF generation: data mapping, recipient/biller
// info, applied taxes and discounts, and PDF URL resolution.
type InvoicePDFSuite struct {
	testutil.BaseServiceTestSuite
	service  InvoiceService
	testData struct {
		now      time.Time
		customer *customer.Customer
		tenant   *tenant.Tenant
		group    *group.Group
		price    *price.Price
	}
}

func TestInvoicePDF(t *testing.T) {
	suite.Run(t, new(InvoicePDFSuite))
}

func (s *InvoicePDFSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewInvoiceService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.setupTestData()
}

func (s *InvoicePDFSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:                "cust_pdf",
		ExternalID:        "ext_cust_pdf",
		Name:              "PDF Customer",
		Email:             "pdf@test.com",
		AddressLine1:      "1 Main St",
		AddressLine2:      "Suite 2",
		AddressCity:       "Metropolis",
		AddressState:      "CA",
		AddressPostalCode: "90210",
		AddressCountry:    "US",
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.tenant = &tenant.Tenant{
		ID:     types.GetTenantID(ctx),
		Name:   "PDF Tenant",
		Status: types.StatusPublished,
		BillingDetails: tenant.TenantBillingDetails{
			Email:     "billing@tenant.com",
			HelpEmail: "help@tenant.com",
			Address: tenant.TenantAddress{
				Line1:      "42 Biller Way",
				City:       "Gotham",
				State:      "NY",
				PostalCode: "10001",
				Country:    "US",
			},
		},
		CreatedAt: s.testData.now,
		UpdatedAt: s.testData.now,
	}
	s.NoError(s.GetStores().TenantRepo.Create(ctx, s.testData.tenant))

	s.testData.group = &group.Group{
		ID:            "grp_pdf",
		Name:          "PDF Group",
		EntityType:    types.GroupEntityTypePrice,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().GroupRepo.Create(ctx, s.testData.group))

	s.testData.price = &price.Price{
		ID:                 "price_pdf_grouped",
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           "plan_pdf",
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		GroupID:            s.testData.group.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.testData.price))
}

// buildPDFInvoice stores a finalized one-off invoice with:
//   - one $100 line item linked to the grouped price (entity type addon)
//   - one $0 line item (must be filtered from the PDF)
func (s *InvoicePDFSuite) buildPDFInvoice(id string, metadata types.Metadata) *invoice.Invoice {
	ctx := s.GetContext()
	now := s.testData.now
	dueDate := now.Add(7 * 24 * time.Hour)
	periodStart := now.Add(-30 * 24 * time.Hour)
	periodEnd := now

	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		InvoiceNumber:   lo.ToPtr("INV-PDF-00001"),
		Subtotal:        decimal.NewFromInt(100),
		Total:           decimal.NewFromInt(110),
		AmountDue:       decimal.NewFromInt(110),
		AmountPaid:      decimal.NewFromInt(10),
		AmountRemaining: decimal.NewFromInt(100),
		Description:     "PDF test invoice",
		BillingReason:   string(types.InvoiceBillingReasonManual),
		DueDate:         &dueDate,
		PeriodStart:     &periodStart,
		PeriodEnd:       &periodEnd,
		FinalizedAt:     &now,
		Metadata:        metadata,
		BaseModel:       types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          "li_main_" + id,
				InvoiceID:   id,
				CustomerID:  s.testData.customer.ID,
				DisplayName: lo.ToPtr("Grouped Item"),
				PriceID:     lo.ToPtr(s.testData.price.ID),
				PriceType:   lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				EntityType:  lo.ToPtr("addon"),
				Amount:      decimal.NewFromInt(100),
				Quantity:    decimal.NewFromInt(2),
				Currency:    "usd",
				PeriodStart: &periodStart,
				PeriodEnd:   &periodEnd,
				Metadata:    types.Metadata{"description": "line description"},
				BaseModel:   types.GetDefaultBaseModel(ctx),
			},
			{
				ID:          "li_zero_" + id,
				InvoiceID:   id,
				CustomerID:  s.testData.customer.ID,
				DisplayName: lo.ToPtr("Zero Item"),
				PriceType:   lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				Amount:      decimal.Zero,
				Quantity:    decimal.NewFromInt(1),
				Currency:    "usd",
				BaseModel:   types.GetDefaultBaseModel(ctx),
			},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
	return inv
}

func (s *InvoicePDFSuite) createAppliedTax(invoiceID string) *taxrate.TaxRate {
	ctx := s.GetContext()
	pct := decimal.NewFromInt(10)
	tr := &taxrate.TaxRate{
		ID:              "tax_pdf_10",
		Name:            "PDF VAT",
		Code:            "PDF_VAT",
		TaxRateStatus:   types.TaxRateStatusActive,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: &pct,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().TaxRateRepo.Create(ctx, tr))

	ta := &taxapplied.TaxApplied{
		ID:            "taxapplied_pdf_1",
		TaxRateID:     tr.ID,
		EntityType:    types.TaxRateEntityTypeInvoice,
		EntityID:      invoiceID,
		TaxableAmount: decimal.NewFromInt(100),
		TaxAmount:     decimal.NewFromInt(10),
		Currency:      "usd",
		AppliedAt:     s.testData.now,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().TaxAppliedRepo.Create(ctx, ta))
	return tr
}

func (s *InvoicePDFSuite) createAppliedDiscount(invoiceID, lineItemID string) *coupon.Coupon {
	ctx := s.GetContext()
	pct := decimal.NewFromInt(15)
	c := &coupon.Coupon{
		ID:            "coupon_pdf_15",
		Name:          "PDF Promo",
		Type:          types.CouponTypePercentage,
		PercentageOff: &pct,
		Currency:      "USD",
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponRepo.Create(ctx, c))

	app := &coupon_application.CouponApplication{
		ID:                 "couponapp_pdf_1",
		CouponID:           c.ID,
		InvoiceID:          invoiceID,
		InvoiceLineItemID:  lo.ToPtr(lineItemID),
		AppliedAt:          s.testData.now,
		OriginalPrice:      decimal.NewFromInt(100),
		FinalPrice:         decimal.NewFromInt(85),
		DiscountedAmount:   decimal.NewFromInt(15),
		DiscountType:       types.CouponTypePercentage,
		DiscountPercentage: &pct,
		Currency:           "usd",
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponApplicationRepo.Create(ctx, app))
	return c
}

// ─────────────────────────────────────────────────────────────────────────────
// GetInvoicePDF
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoicePDFSuite) TestGetInvoicePDF() {
	ctx := s.GetContext()

	s.Run("renders_pdf_with_mapped_invoice_data", func() {
		inv := s.buildPDFInvoice("inv_pdf_happy", types.Metadata{"notes": "thanks!", "vat": "7.5"})
		s.createAppliedTax(inv.ID)
		s.createAppliedDiscount(inv.ID, "li_main_"+inv.ID)

		var captured *pdfdomain.InvoiceData
		mockGen := s.GetPDFGenerator().(*testutil.MockPDFGenerator)
		mockGen.On("RenderInvoicePdf", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				captured = args.Get(1).(*pdfdomain.InvoiceData)
			}).
			Return([]byte("%PDF-fake"), nil).Once()

		data, err := s.service.GetInvoicePDF(ctx, inv.ID)
		s.NoError(err)
		s.Equal([]byte("%PDF-fake"), data)

		s.Require().NotNil(captured, "PDF generator must receive mapped invoice data")
		s.Equal(inv.ID, captured.ID)
		s.Equal("INV-PDF-00001", captured.InvoiceNumber)
		s.Equal("PDF test invoice", captured.Description)
		s.Equal("thanks!", captured.Notes)
		s.InDelta(7.5, captured.VAT, 0.0001)
		s.InDelta(100.0, captured.Subtotal, 0.0001)
		s.InDelta(10.0, captured.AmountPaid, 0.0001)
		s.InDelta(100.0, captured.AmountRemaining, 0.0001)

		// Zero-amount line item must be filtered.
		s.Require().Len(captured.LineItems, 1)
		li := captured.LineItems[0]
		s.Equal("Grouped Item", li.DisplayName)
		s.Equal("line description", li.Description)
		s.Equal("PDF Group", li.Group, "group name must be resolved through price.group_id")
		s.Equal("addon", li.Type)
		s.InDelta(100.0, li.Amount, 0.0001)
		s.InDelta(2.0, li.Quantity, 0.0001)

		// Recipient (customer) info.
		s.Require().NotNil(captured.Recipient)
		s.Equal("PDF Customer", captured.Recipient.Name)
		s.Equal("pdf@test.com", captured.Recipient.Email)
		s.Contains(captured.Recipient.Address.Street, "1 Main St")
		s.Contains(captured.Recipient.Address.Street, "Suite 2")

		// Biller (tenant) info.
		s.Require().NotNil(captured.Biller)
		s.Equal("PDF Tenant", captured.Biller.Name)
		s.Equal("billing@tenant.com", captured.Biller.Email)
		s.Equal("Gotham", captured.Biller.Address.City)

		// Applied taxes and discounts.
		s.Require().Len(captured.AppliedTaxes, 1)
		s.Equal("PDF VAT", captured.AppliedTaxes[0].TaxName)
		s.Equal("PDF_VAT", captured.AppliedTaxes[0].TaxCode)
		s.InDelta(10.0, captured.AppliedTaxes[0].TaxAmount, 0.0001)
		s.InDelta(10.0, captured.AppliedTaxes[0].TaxRate, 0.0001)

		s.Require().Len(captured.AppliedDiscounts, 1)
		s.Equal("PDF Promo", captured.AppliedDiscounts[0].DiscountName)
		s.InDelta(15.0, captured.AppliedDiscounts[0].DiscountAmount, 0.0001)
		s.Equal("Grouped Item", captured.AppliedDiscounts[0].LineItemRef,
			"line-item-level discount must reference the line item display name")

		mockGen.AssertExpectations(s.T())
	})

	s.Run("invoice_not_found_returns_error", func() {
		_, err := s.service.GetInvoicePDF(ctx, "inv_does_not_exist")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// getInvoiceDataForPDFGen edge cases
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoicePDFSuite) TestGetInvoiceDataForPDFGen() {
	ctx := s.GetContext()
	svc, ok := s.service.(*invoiceService)
	s.Require().True(ok)

	s.Run("invalid_vat_metadata_returns_error", func() {
		inv := s.buildPDFInvoice("inv_pdf_badvat", types.Metadata{"vat": "not-a-number"})
		resp, err := s.service.GetInvoice(ctx, inv.ID)
		s.Require().NoError(err)

		data, err := svc.getInvoiceDataForPDFGen(ctx, resp, s.testData.customer, s.testData.tenant)
		s.Error(err)
		s.Nil(data)
	})

	s.Run("issuing_date_falls_back_to_finalized_at", func() {
		// The fixture has no IssueDate, so the FinalizedAt fallback branch is
		// what this case exercises.
		inv := s.buildPDFInvoice("inv_pdf_issuedate", nil)
		resp, err := s.service.GetInvoice(ctx, inv.ID)
		s.Require().NoError(err)
		s.Require().Nil(resp.IssueDate)

		data, err := svc.getInvoiceDataForPDFGen(ctx, resp, s.testData.customer, s.testData.tenant)
		s.NoError(err)
		s.Require().NotNil(data)
		s.WithinDuration(s.testData.now, data.IssuingDate.Time, time.Second,
			"issuing date must fall back to finalized_at")
		s.WithinDuration(s.testData.now.Add(7*24*time.Hour), data.DueDate.Time, time.Second)
	})

	s.Run("persisted_issue_date_takes_precedence_over_finalized_at", func() {
		issueDate := s.testData.now.Add(-3 * 24 * time.Hour)
		inv := s.buildPDFInvoice("inv_pdf_issuedate_set", nil)
		inv.IssueDate = &issueDate
		s.Require().NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

		resp, err := s.service.GetInvoice(ctx, inv.ID)
		s.Require().NoError(err)
		s.Require().NotNil(resp.IssueDate, "IssueDate must survive the store round-trip")

		data, err := svc.getInvoiceDataForPDFGen(ctx, resp, s.testData.customer, s.testData.tenant)
		s.NoError(err)
		s.Require().NotNil(data)
		s.WithinDuration(issueDate, data.IssuingDate.Time, time.Second,
			"issuing date must use the persisted issue_date, not finalized_at")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// getRecipientInfo / getBillerInfo
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoicePDFSuite) TestGetRecipientInfo() {
	svc := s.service.(*invoiceService)

	s.Run("nil_customer_returns_nil", func() {
		s.Nil(svc.getRecipientInfo(nil))
	})

	s.Run("unnamed_customer_falls_back_to_id", func() {
		info := svc.getRecipientInfo(&customer.Customer{ID: "cust_anon"})
		s.Require().NotNil(info)
		s.Equal("Customer cust_anon", info.Name)
		s.Empty(info.Email)
	})

	s.Run("full_address_is_mapped", func() {
		info := svc.getRecipientInfo(s.testData.customer)
		s.Require().NotNil(info)
		s.Equal("PDF Customer", info.Name)
		s.Equal("pdf@test.com", info.Email)
		s.Equal("1 Main St\nSuite 2", info.Address.Street)
		s.Equal("Metropolis", info.Address.City)
		s.Equal("CA", info.Address.State)
		s.Equal("90210", info.Address.PostalCode)
		s.Equal("US", info.Address.Country)
	})
}

func (s *InvoicePDFSuite) TestGetBillerInfo() {
	svc := s.service.(*invoiceService)

	s.Run("nil_tenant_returns_nil", func() {
		s.Nil(svc.getBillerInfo(nil))
	})

	s.Run("tenant_without_billing_details_maps_name_only", func() {
		info := svc.getBillerInfo(&tenant.Tenant{ID: "tenant_bare", Name: "Bare Tenant"})
		s.Require().NotNil(info)
		s.Equal("Bare Tenant", info.Name)
		s.Empty(info.Email)
		s.Empty(info.Address.City)
	})

	s.Run("billing_details_are_mapped", func() {
		info := svc.getBillerInfo(s.testData.tenant)
		s.Require().NotNil(info)
		s.Equal("PDF Tenant", info.Name)
		s.Equal("billing@tenant.com", info.Email)
		s.Equal("help@tenant.com", info.HelpEmail)
		s.Equal("Gotham", info.Address.City)
		s.Equal("NY", info.Address.State)
		s.Equal("10001", info.Address.PostalCode)
		s.Equal("US", info.Address.Country)
		s.Contains(info.Address.Street, "42 Biller Way")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// getAppliedTaxesForPDF / getAppliedDiscountsForPDF
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoicePDFSuite) TestGetAppliedTaxesForPDF() {
	ctx := s.GetContext()
	svc := s.service.(*invoiceService)

	s.Run("no_applied_taxes_returns_empty_slice", func() {
		inv := s.buildPDFInvoice("inv_pdf_notax", nil)
		taxes, err := svc.getAppliedTaxesForPDF(ctx, inv.ID)
		s.NoError(err)
		s.NotNil(taxes)
		s.Empty(taxes)
	})

	s.Run("percentage_tax_is_expanded_with_rate_details", func() {
		inv := s.buildPDFInvoice("inv_pdf_withtax", nil)
		s.createAppliedTax(inv.ID)

		taxes, err := svc.getAppliedTaxesForPDF(ctx, inv.ID)
		s.NoError(err)
		s.Require().Len(taxes, 1)
		s.Equal("PDF VAT", taxes[0].TaxName)
		s.Equal("PDF_VAT", taxes[0].TaxCode)
		s.Equal(string(types.TaxRateTypePercentage), taxes[0].TaxType)
		s.InDelta(10.0, taxes[0].TaxRate, 0.0001)
		s.InDelta(100.0, taxes[0].TaxableAmount, 0.0001)
		s.InDelta(10.0, taxes[0].TaxAmount, 0.0001)
	})

	s.Run("fixed_tax_uses_fixed_value_as_rate", func() {
		inv := s.buildPDFInvoice("inv_pdf_fixedtax", nil)
		fixedVal := decimal.NewFromInt(5)
		tr := &taxrate.TaxRate{
			ID:            "tax_pdf_fixed",
			Name:          "Fixed Fee Tax",
			Code:          "PDF_FIXED",
			TaxRateStatus: types.TaxRateStatusActive,
			TaxRateType:   types.TaxRateTypeFixed,
			FixedValue:    &fixedVal,
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().TaxRateRepo.Create(ctx, tr))
		ta := &taxapplied.TaxApplied{
			ID:            "taxapplied_pdf_fixed",
			TaxRateID:     tr.ID,
			EntityType:    types.TaxRateEntityTypeInvoice,
			EntityID:      inv.ID,
			TaxableAmount: decimal.NewFromInt(100),
			TaxAmount:     fixedVal,
			Currency:      "usd",
			AppliedAt:     s.testData.now,
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().TaxAppliedRepo.Create(ctx, ta))

		taxes, err := svc.getAppliedTaxesForPDF(ctx, inv.ID)
		s.NoError(err)
		s.Require().Len(taxes, 1)
		s.Equal("Fixed Fee Tax", taxes[0].TaxName)
		s.Equal(string(types.TaxRateTypeFixed), taxes[0].TaxType)
		s.InDelta(5.0, taxes[0].TaxRate, 0.0001)
		s.InDelta(5.0, taxes[0].TaxAmount, 0.0001)
	})
}

func (s *InvoicePDFSuite) TestGetAppliedDiscountsForPDF() {
	ctx := s.GetContext()
	svc := s.service.(*invoiceService)

	s.Run("no_coupon_applications_returns_empty_slice", func() {
		inv := s.buildPDFInvoice("inv_pdf_nodiscount", nil)
		resp, err := s.service.GetInvoice(ctx, inv.ID)
		s.Require().NoError(err)

		discounts, err := svc.getAppliedDiscountsForPDF(ctx, resp)
		s.NoError(err)
		s.NotNil(discounts)
		s.Empty(discounts)
	})

	s.Run("percentage_discount_maps_name_value_and_line_item_ref", func() {
		inv := s.buildPDFInvoice("inv_pdf_discount", nil)
		s.createAppliedDiscount(inv.ID, "li_main_"+inv.ID)
		resp, err := s.service.GetInvoice(ctx, inv.ID)
		s.Require().NoError(err)

		discounts, err := svc.getAppliedDiscountsForPDF(ctx, resp)
		s.NoError(err)
		s.Require().Len(discounts, 1)
		s.Equal("PDF Promo", discounts[0].DiscountName)
		s.Equal(string(types.CouponTypePercentage), discounts[0].Type)
		s.InDelta(15.0, discounts[0].Value, 0.0001)
		s.InDelta(15.0, discounts[0].DiscountAmount, 0.0001)
		s.Equal("Grouped Item", discounts[0].LineItemRef)
	})

	s.Run("invoice_level_discount_has_no_line_item_ref", func() {
		inv := s.buildPDFInvoice("inv_pdf_invdiscount", nil)
		fixedOff := decimal.NewFromInt(20)
		c := &coupon.Coupon{
			ID:        "coupon_pdf_fixed",
			Name:      "Fixed Promo",
			Type:      types.CouponTypeFixed,
			AmountOff: &fixedOff,
			Currency:  "USD",
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().CouponRepo.Create(ctx, c))
		app := &coupon_application.CouponApplication{
			ID:               "couponapp_pdf_fixed",
			CouponID:         c.ID,
			InvoiceID:        inv.ID,
			AppliedAt:        s.testData.now,
			OriginalPrice:    decimal.NewFromInt(100),
			FinalPrice:       decimal.NewFromInt(80),
			DiscountedAmount: fixedOff,
			DiscountType:     types.CouponTypeFixed,
			Currency:         "usd",
			EnvironmentID:    types.GetEnvironmentID(ctx),
			BaseModel:        types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().CouponApplicationRepo.Create(ctx, app))

		resp, err := s.service.GetInvoice(ctx, inv.ID)
		s.Require().NoError(err)

		discounts, err := svc.getAppliedDiscountsForPDF(ctx, resp)
		s.NoError(err)
		s.Require().Len(discounts, 1)
		s.Equal("Fixed Promo", discounts[0].DiscountName)
		s.Equal(string(types.CouponTypeFixed), discounts[0].Type)
		s.InDelta(20.0, discounts[0].DiscountAmount, 0.0001)
		s.Equal("--", discounts[0].LineItemRef, "invoice-level discounts have no line item reference")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetInvoicePDFUrl
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoicePDFSuite) TestGetInvoicePDFUrl() {
	ctx := s.GetContext()

	s.Run("existing_pdf_url_is_returned_without_generation", func() {
		inv := s.buildPDFInvoice("inv_pdf_url_existing", nil)
		inv.InvoicePDFURL = lo.ToPtr("https://cdn.example.com/inv.pdf")
		s.NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))

		url, err := s.service.GetInvoicePDFUrl(ctx, inv.ID, false)
		s.NoError(err)
		s.Equal("https://cdn.example.com/inv.pdf", url)
	})

	s.Run("missing_s3_returns_system_error", func() {
		inv := s.buildPDFInvoice("inv_pdf_url_nos3", nil)
		url, err := s.service.GetInvoicePDFUrl(ctx, inv.ID, false)
		s.Error(err)
		s.True(ierr.IsSystem(err), "S3 not configured must be a system error, got %v", err)
		s.Empty(url)
	})

	s.Run("invoice_not_found_returns_error", func() {
		url, err := s.service.GetInvoicePDFUrl(ctx, "inv_does_not_exist", false)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
		s.Empty(url)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetInvoicePDFUrl with an S3 double
// ─────────────────────────────────────────────────────────────────────────────

// fakeS3Service is a minimal in-memory s3.Service double for URL generation tests.
type fakeS3Service struct {
	objects map[string][]byte
	uploads int
}

func newFakeS3Service() *fakeS3Service {
	return &fakeS3Service{objects: map[string][]byte{}}
}

func (f *fakeS3Service) UploadDocument(_ context.Context, doc *s3.Document) error {
	f.uploads++
	f.objects[doc.ID] = doc.Data
	return nil
}

func (f *fakeS3Service) GetPresignedUrl(_ context.Context, id string, _ s3.DocumentType) (string, error) {
	return "https://s3.fake/" + id, nil
}

func (f *fakeS3Service) GetDocument(_ context.Context, id string, _ s3.DocumentType) ([]byte, error) {
	return f.objects[id], nil
}

func (f *fakeS3Service) Exists(_ context.Context, id string, _ s3.DocumentType) (bool, error) {
	_, ok := f.objects[id]
	return ok, nil
}

func (f *fakeS3Service) GeneratePresignedURL(_ context.Context, bucket, key string, _ time.Duration) (string, error) {
	return "https://s3.fake/" + bucket + "/" + key, nil
}

func (s *InvoicePDFSuite) TestGetInvoicePDFUrlWithS3() {
	ctx := s.GetContext()

	fake := newFakeS3Service()
	params := newTestServiceParams(&s.BaseServiceTestSuite)
	params.S3 = fake
	svcWithS3 := NewInvoiceService(params)

	mockGen := s.GetPDFGenerator().(*testutil.MockPDFGenerator)
	mockGen.On("RenderInvoicePdf", mock.Anything, mock.Anything, mock.Anything).
		Return([]byte("%PDF-s3"), nil)

	s.Run("generates_uploads_and_returns_presigned_url", func() {
		inv := s.buildPDFInvoice("inv_pdf_s3_generate", nil)

		url, err := svcWithS3.GetInvoicePDFUrl(ctx, inv.ID, false)
		s.NoError(err)
		key := inv.TenantID + "/" + inv.ID
		s.Equal("https://s3.fake/"+key, url)
		s.Equal(1, fake.uploads, "PDF must be uploaded once")
		s.Equal([]byte("%PDF-s3"), fake.objects[key], "uploaded document must be the rendered PDF")
	})

	s.Run("existing_s3_object_is_reused_without_regeneration", func() {
		inv := s.buildPDFInvoice("inv_pdf_s3_cached", nil)
		key := inv.TenantID + "/" + inv.ID
		fake.objects[key] = []byte("%PDF-old")
		uploadsBefore := fake.uploads

		url, err := svcWithS3.GetInvoicePDFUrl(ctx, inv.ID, false)
		s.NoError(err)
		s.Equal("https://s3.fake/"+key, url)
		s.Equal(uploadsBefore, fake.uploads, "cached objects must not be re-uploaded")
		s.Equal([]byte("%PDF-old"), fake.objects[key])
	})

	s.Run("force_generate_overwrites_existing_object", func() {
		inv := s.buildPDFInvoice("inv_pdf_s3_force", nil)
		key := inv.TenantID + "/" + inv.ID
		fake.objects[key] = []byte("%PDF-stale")
		uploadsBefore := fake.uploads

		url, err := svcWithS3.GetInvoicePDFUrl(ctx, inv.ID, true)
		s.NoError(err)
		s.Equal("https://s3.fake/"+key, url)
		s.Equal(uploadsBefore+1, fake.uploads, "force generation must re-upload")
		s.Equal([]byte("%PDF-s3"), fake.objects[key], "stale object must be replaced")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// getInvoiceDataForPDFGen — entity type mapping
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoicePDFSuite) TestGetInvoiceDataForPDFGenEntityTypes() {
	ctx := s.GetContext()
	svc := s.service.(*invoiceService)

	inv := &invoice.Invoice{
		ID:            "inv_pdf_entity_types",
		CustomerID:    s.testData.customer.ID,
		InvoiceType:   types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusPending,
		Currency:      "usd",
		Subtotal:      decimal.NewFromInt(30),
		Total:         decimal.NewFromInt(30),
		AmountDue:     decimal.NewFromInt(30),
		BaseModel:     types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:         "li_et_plan",
				InvoiceID:  "inv_pdf_entity_types",
				CustomerID: s.testData.customer.ID,
				// entity type plan maps to "subscription", empty display names
				// fall back: PlanDisplayName <- DisplayName.
				DisplayName: lo.ToPtr("Plan Item"),
				EntityType:  lo.ToPtr("plan"),
				Amount:      decimal.NewFromInt(10),
				Quantity:    decimal.NewFromInt(1),
				Currency:    "usd",
				BaseModel:   types.GetDefaultBaseModel(ctx),
			},
			{
				ID:          "li_et_custom",
				InvoiceID:   "inv_pdf_entity_types",
				CustomerID:  s.testData.customer.ID,
				DisplayName: lo.ToPtr("Commitment Item"),
				EntityType:  lo.ToPtr("commitment"),
				Amount:      decimal.NewFromInt(10),
				Quantity:    decimal.NewFromInt(1),
				Currency:    "usd",
				BaseModel:   types.GetDefaultBaseModel(ctx),
			},
			{
				ID:         "li_et_none",
				InvoiceID:  "inv_pdf_entity_types",
				CustomerID: s.testData.customer.ID,
				// nil entity type falls back to "subscription"
				DisplayName: lo.ToPtr("Bare Item"),
				Amount:      decimal.NewFromInt(10),
				Quantity:    decimal.NewFromInt(1),
				Currency:    "usd",
				BaseModel:   types.GetDefaultBaseModel(ctx),
			},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))

	resp, err := s.service.GetInvoice(ctx, inv.ID)
	s.Require().NoError(err)

	data, err := svc.getInvoiceDataForPDFGen(ctx, resp, s.testData.customer, s.testData.tenant)
	s.NoError(err)
	s.Require().NotNil(data)
	s.Require().Len(data.LineItems, 3)

	byName := map[string]string{}
	for _, li := range data.LineItems {
		byName[li.DisplayName] = li.Type
		s.Equal(li.DisplayName, li.PlanDisplayName,
			"missing plan display name must fall back to the item display name")
		s.Equal("--", li.Group, "items without a price have no group")
	}
	s.Equal("subscription", byName["Plan Item"])
	s.Equal("commitment", byName["Commitment Item"])
	s.Equal("subscription", byName["Bare Item"])
}

func (s *InvoicePDFSuite) TestGetAppliedTaxesForPDFFallbackWithoutTaxRate() {
	ctx := s.GetContext()
	svc := s.service.(*invoiceService)
	inv := s.buildPDFInvoice("inv_pdf_orphan_tax", nil)

	// Tax applied record referencing a tax rate that no longer exists:
	// the PDF data falls back to basic info instead of failing.
	ta := &taxapplied.TaxApplied{
		ID:            "taxapplied_pdf_orphan",
		TaxRateID:     "tax_rate_gone_123456",
		EntityType:    types.TaxRateEntityTypeInvoice,
		EntityID:      inv.ID,
		TaxableAmount: decimal.NewFromInt(100),
		TaxAmount:     decimal.NewFromInt(8),
		Currency:      "usd",
		AppliedAt:     s.testData.now,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().TaxAppliedRepo.Create(ctx, ta))

	taxes, err := svc.getAppliedTaxesForPDF(ctx, inv.ID)
	s.NoError(err)
	s.Require().Len(taxes, 1)
	s.Equal("Tax Rate 123456", taxes[0].TaxName, "fallback name uses the last 6 chars of the id")
	s.Equal("tax_rate_gone_123456", taxes[0].TaxCode)
	s.Equal("Unknown", taxes[0].TaxType)
	s.InDelta(0.0, taxes[0].TaxRate, 0.0001)
	s.InDelta(8.0, taxes[0].TaxAmount, 0.0001)
}
