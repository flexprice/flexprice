package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// InvoicePreviewSuite tests preview invoice generation, customer invoice
// summaries across currencies, and breakdown helpers.
type InvoicePreviewSuite struct {
	testutil.BaseServiceTestSuite
	service  InvoiceService
	testData struct {
		now time.Time

		customer  *customer.Customer
		plan      *plan.Plan
		fixed     *price.Price // $50 fixed advance
		zeroFixed *price.Price // $0 fixed advance (for hide-zero-charges)
		sub       *subscription.Subscription
	}
}

func TestInvoicePreview(t *testing.T) {
	suite.Run(t, new(InvoicePreviewSuite))
}

func (s *InvoicePreviewSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewInvoiceService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.setupTestData()
}

func (s *InvoicePreviewSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.now = time.Now().UTC()
	now := s.testData.now

	s.testData.customer = &customer.Customer{
		ID:         "cust_pv",
		ExternalID: "ext_cust_pv",
		Name:       "Preview Customer",
		Email:      "pv@test.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:        "plan_pv",
		Name:      "Preview Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.plan))

	newFixedPrice := func(id string, amount decimal.Decimal) *price.Price {
		p := &price.Price{
			ID:                 id,
			Amount:             amount,
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           s.testData.plan.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
		return p
	}
	s.testData.fixed = newFixedPrice("price_pv_fixed", decimal.NewFromInt(50))
	s.testData.zeroFixed = newFixedPrice("price_pv_zero", decimal.Zero)

	periodStart := now.Add(-48 * time.Hour)
	periodEnd := now.Add(6 * 24 * time.Hour)
	s.testData.sub = &subscription.Subscription{
		ID:                 "sub_pv",
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		BillingAnchor:      periodEnd,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	newSubLineItem := func(p *price.Price, displayName string) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  s.testData.sub.ID,
			CustomerID:      s.testData.customer.ID,
			EntityID:        s.testData.plan.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: s.testData.plan.Name,
			PriceID:         p.ID,
			PriceType:       p.Type,
			DisplayName:     displayName,
			Quantity:        decimal.NewFromInt(1),
			Currency:        "usd",
			BillingPeriod:   types.BILLING_PERIOD_MONTHLY,
			InvoiceCadence:  types.InvoiceCadenceAdvance,
			StartDate:       s.testData.sub.StartDate,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		}
	}
	lineItems := []*subscription.SubscriptionLineItem{
		newSubLineItem(s.testData.fixed, "Fixed Fee"),
		newSubLineItem(s.testData.zeroFixed, "Free Component"),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.testData.sub, lineItems))
}

// sumLineItems returns the decimal sum of line item amounts on the response.
func sumPreviewLineItems(resp *dto.InvoiceResponse) decimal.Decimal {
	total := decimal.Zero
	for _, li := range resp.LineItems {
		total = total.Add(li.Amount)
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// GetPreviewInvoice / GetInternalPreviewInvoice / GetMeterUsagePreviewInvoice
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoicePreviewSuite) TestGetPreviewInvoice() {
	ctx := s.GetContext()

	s.Run("defaults_periods_from_subscription_and_returns_charges", func() {
		resp, err := s.service.GetPreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{
			SubscriptionID: s.testData.sub.ID,
		})
		s.NoError(err)
		s.Require().NotNil(resp)

		s.Equal(types.InvoiceTypeSubscription, resp.InvoiceType)
		s.Equal(s.testData.customer.ID, resp.CustomerID)
		s.Equal("usd", resp.Currency)
		s.Require().NotEmpty(resp.LineItems, "preview must contain the fixed charge line items")
		s.True(resp.Subtotal.IsPositive(), "subtotal must include the $50 fixed fee, got %s", resp.Subtotal)
		s.True(resp.Subtotal.Equal(sumPreviewLineItems(resp)),
			"subtotal must equal the sum of line items: subtotal=%s items=%s", resp.Subtotal, sumPreviewLineItems(resp))

		// Customer expansion.
		s.Require().NotNil(resp.Customer)
		s.Equal(s.testData.customer.ID, resp.Customer.Customer.ID)

		// Preview is never persisted.
		_, err = s.GetStores().InvoiceRepo.Get(ctx, resp.ID)
		s.Error(err, "preview invoices must not be stored")
	})

	s.Run("hide_zero_charges_filters_zero_amount_line_items", func() {
		withZeros, err := s.service.GetPreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{
			SubscriptionID: s.testData.sub.ID,
		})
		s.NoError(err)
		s.Require().NotNil(withZeros)

		withoutZeros, err := s.service.GetPreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{
			SubscriptionID:           s.testData.sub.ID,
			HideZeroChargesLineItems: true,
		})
		s.NoError(err)
		s.Require().NotNil(withoutZeros)

		zeroCount := 0
		for _, li := range withZeros.LineItems {
			if li.Amount.IsZero() {
				zeroCount++
			}
		}
		s.Positive(zeroCount, "fixture must produce at least one zero-amount line item")
		s.Len(withoutZeros.LineItems, len(withZeros.LineItems)-zeroCount,
			"hide_zero_charges must remove exactly the zero-amount items")
		for _, li := range withoutZeros.LineItems {
			s.False(li.Amount.IsZero())
		}
	})

	s.Run("explicit_period_is_respected", func() {
		periodStart := s.testData.sub.CurrentPeriodStart
		periodEnd := s.testData.sub.CurrentPeriodEnd
		resp, err := s.service.GetPreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{
			SubscriptionID: s.testData.sub.ID,
			PeriodStart:    &periodStart,
			PeriodEnd:      &periodEnd,
		})
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Require().NotNil(resp.PeriodStart)
		s.Require().NotNil(resp.PeriodEnd)
		s.True(resp.PeriodStart.Equal(periodStart))
		s.True(resp.PeriodEnd.Equal(periodEnd))
	})

	s.Run("subscription_not_found_returns_error", func() {
		resp, err := s.service.GetPreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{
			SubscriptionID: "sub_does_not_exist",
		})
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *InvoicePreviewSuite) TestGetInternalPreviewInvoice() {
	ctx := s.GetContext()

	s.Run("returns_preview_with_customer_expanded", func() {
		resp, err := s.service.GetInternalPreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{
			SubscriptionID: s.testData.sub.ID,
		})
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(s.testData.customer.ID, resp.CustomerID)
		s.Require().NotNil(resp.Customer)
		s.Equal(s.testData.customer.ID, resp.Customer.Customer.ID)
		s.NotEmpty(resp.LineItems)
		s.True(resp.Subtotal.Equal(sumPreviewLineItems(resp)))
	})

	s.Run("subscription_not_found_returns_error", func() {
		resp, err := s.service.GetInternalPreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{
			SubscriptionID: "sub_does_not_exist",
		})
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *InvoicePreviewSuite) TestGetMeterUsagePreviewInvoice() {
	ctx := s.GetContext()

	s.Run("returns_preview_using_meter_usage_reference_point", func() {
		resp, err := s.service.GetMeterUsagePreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{
			SubscriptionID: s.testData.sub.ID,
		})
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(s.testData.customer.ID, resp.CustomerID)
		s.Require().NotNil(resp.Customer)
		s.NotEmpty(resp.LineItems, "fixed charges must still be present in meter usage preview")
		s.True(resp.Subtotal.Equal(sumPreviewLineItems(resp)))
	})

	s.Run("subscription_not_found_returns_error", func() {
		resp, err := s.service.GetMeterUsagePreviewInvoice(ctx, dto.GetPreviewInvoiceRequest{
			SubscriptionID: "sub_does_not_exist",
		})
		s.Error(err)
		s.Nil(resp)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetCustomerMultiCurrencyInvoiceSummary
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoicePreviewSuite) TestGetCustomerMultiCurrencyInvoiceSummary() {
	ctx := s.GetContext()

	s.Run("customer_without_subscriptions_returns_empty_summaries", func() {
		other := &customer.Customer{
			ID:         "cust_pv_nosubs",
			ExternalID: "ext_cust_pv_nosubs",
			Name:       "No Subs",
			BaseModel:  types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().CustomerRepo.Create(ctx, other))

		resp, err := s.service.GetCustomerMultiCurrencyInvoiceSummary(ctx, other.ID)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(other.ID, resp.CustomerID)
		s.Empty(resp.Summaries)
		s.Empty(resp.DefaultCurrency)
	})

	s.Run("summaries_are_grouped_by_subscription_currency", func() {
		// Add a EUR subscription for the same customer (usd sub exists in fixtures).
		eurSub := &subscription.Subscription{
			ID:                 "sub_pv_eur",
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			StartDate:          s.testData.now.Add(-10 * 24 * time.Hour),
			BillingAnchor:      s.testData.now.Add(20 * 24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-10 * 24 * time.Hour),
			CurrentPeriodEnd:   s.testData.now.Add(20 * 24 * time.Hour),
			Currency:           "eur",
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusActive,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, eurSub, nil))

		// A cancelled GBP subscription: its currency must NOT get a summary
		// (the service filters cancelled subscriptions out via
		// SubscriptionStatusNotIn / the active-only default).
		cancelledSub := &subscription.Subscription{
			ID:                 "sub_pv_gbp_cancelled",
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			StartDate:          s.testData.now.Add(-40 * 24 * time.Hour),
			BillingAnchor:      s.testData.now.Add(-10 * 24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-40 * 24 * time.Hour),
			CurrentPeriodEnd:   s.testData.now.Add(-10 * 24 * time.Hour),
			Currency:           "gbp",
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusCancelled,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, cancelledSub, nil))

		// One overdue finalized USD invoice.
		overdueDate := s.testData.now.Add(-24 * time.Hour)
		hundred := decimal.NewFromInt(100)
		inv := &invoice.Invoice{
			ID:              "inv_pv_usd_overdue",
			CustomerID:      s.testData.customer.ID,
			SubscriptionID:  lo.ToPtr(s.testData.sub.ID),
			InvoiceType:     types.InvoiceTypeSubscription,
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.PaymentStatusPending,
			Currency:        "usd",
			Subtotal:        hundred,
			Total:           hundred,
			AmountDue:       hundred,
			AmountRemaining: hundred,
			DueDate:         &overdueDate,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))

		resp, err := s.service.GetCustomerMultiCurrencyInvoiceSummary(ctx, s.testData.customer.ID)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(s.testData.customer.ID, resp.CustomerID)
		s.Require().Len(resp.Summaries, 2, "one summary per active-subscription currency")

		byCurrency := map[string]*dto.CustomerInvoiceSummary{}
		for _, sum := range resp.Summaries {
			byCurrency[sum.Currency] = sum
		}
		s.Nil(byCurrency["gbp"], "cancelled subscription currency must be excluded")

		usd := byCurrency["usd"]
		s.Require().NotNil(usd)
		s.Equal(1, usd.TotalInvoiceCount)
		s.Equal(1, usd.UnpaidInvoiceCount)
		s.Equal(1, usd.OverdueInvoiceCount)
		s.True(usd.TotalRevenueAmount.Equal(hundred))
		s.True(usd.TotalUnpaidAmount.Equal(hundred))
		s.True(usd.TotalOverdueAmount.Equal(hundred))

		eur := byCurrency["eur"]
		s.Require().NotNil(eur)
		s.Equal(0, eur.TotalInvoiceCount)
		s.True(eur.TotalUnpaidAmount.IsZero())
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Breakdown helpers
// ─────────────────────────────────────────────────────────────────────────────

// buildOneOffInvoiceWithFixedItems stores a finalized one-off invoice with two
// fixed-price line items (60 + 40) and the given amount already paid.
func (s *InvoicePreviewSuite) buildOneOffInvoiceWithFixedItems(id string, amountPaid decimal.Decimal) *invoice.Invoice {
	ctx := s.GetContext()
	hundred := decimal.NewFromInt(100)
	inv := &invoice.Invoice{
		ID:              id,
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "usd",
		Subtotal:        hundred,
		Total:           hundred,
		AmountDue:       hundred,
		AmountPaid:      amountPaid,
		AmountRemaining: hundred.Sub(amountPaid),
		BaseModel:       types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{
			{
				ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
				InvoiceID:   id,
				CustomerID:  s.testData.customer.ID,
				DisplayName: lo.ToPtr("Fixed A"),
				PriceType:   lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				Amount:      decimal.NewFromInt(60),
				Quantity:    decimal.NewFromInt(1),
				Currency:    "usd",
				BaseModel:   types.GetDefaultBaseModel(ctx),
			},
			{
				ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
				InvoiceID:   id,
				CustomerID:  s.testData.customer.ID,
				DisplayName: lo.ToPtr("Fixed B"),
				PriceType:   lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				Amount:      decimal.NewFromInt(40),
				Quantity:    decimal.NewFromInt(1),
				Currency:    "usd",
				BaseModel:   types.GetDefaultBaseModel(ctx),
			},
		},
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, inv))
	return inv
}

func (s *InvoicePreviewSuite) TestCalculatePriceBreakdown() {
	ctx := s.GetContext()

	s.Run("no_usage_line_items_returns_empty_map", func() {
		inv := s.buildOneOffInvoiceWithFixedItems("inv_pv_pb_fixed", decimal.Zero)
		resp, err := s.service.GetInvoice(ctx, inv.ID)
		s.Require().NoError(err)

		breakdown, err := s.service.CalculatePriceBreakdown(ctx, resp)
		s.NoError(err)
		s.NotNil(breakdown)
		s.Empty(breakdown, "fixed-only invoices have no usage breakdown")
	})
}

func (s *InvoicePreviewSuite) TestCalculateUsageBreakdown() {
	ctx := s.GetContext()
	inv := s.buildOneOffInvoiceWithFixedItems("inv_pv_ub_fixed", decimal.Zero)
	resp, err := s.service.GetInvoice(ctx, inv.ID)
	s.Require().NoError(err)

	s.Run("empty_group_by_returns_empty_map", func() {
		breakdown, err := s.service.CalculateUsageBreakdown(ctx, resp, nil, false)
		s.NoError(err)
		s.NotNil(breakdown)
		s.Empty(breakdown)
	})

	s.Run("no_usage_line_items_returns_empty_map", func() {
		breakdown, err := s.service.CalculateUsageBreakdown(ctx, resp, []string{"source"}, false)
		s.NoError(err)
		s.NotNil(breakdown)
		s.Empty(breakdown)
	})
}

func (s *InvoicePreviewSuite) TestGetInvoiceWithBreakdown() {
	ctx := s.GetContext()

	s.Run("validation_error_for_missing_id", func() {
		resp, err := s.service.GetInvoiceWithBreakdown(ctx, dto.GetInvoiceWithBreakdownRequest{})
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("invoice_not_found_returns_error", func() {
		resp, err := s.service.GetInvoiceWithBreakdown(ctx, dto.GetInvoiceWithBreakdownRequest{ID: "inv_does_not_exist"})
		s.Error(err)
		s.True(ierr.IsNotFound(err))
		s.Nil(resp)
	})

	s.Run("without_group_by_returns_invoice_as_is", func() {
		inv := s.buildOneOffInvoiceWithFixedItems("inv_pv_bd_plain", decimal.Zero)
		resp, err := s.service.GetInvoiceWithBreakdown(ctx, dto.GetInvoiceWithBreakdownRequest{ID: inv.ID})
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(inv.ID, resp.ID)
		s.Len(resp.LineItems, 2)
		s.True(resp.Total.Equal(decimal.NewFromInt(100)))
	})

	s.Run("force_runtime_recalculation_recomputes_totals_from_line_items", func() {
		// AmountPaid 30 → after recalculation from line items (60+40=100):
		// subtotal=100, total=100, amount_due=100, remaining=70.
		inv := s.buildOneOffInvoiceWithFixedItems("inv_pv_bd_force", decimal.NewFromInt(30))

		resp, err := s.service.GetInvoiceWithBreakdown(ctx, dto.GetInvoiceWithBreakdownRequest{
			ID:                        inv.ID,
			GroupBy:                   []string{"source"},
			ForceRuntimeRecalculation: true,
		})
		s.NoError(err)
		s.Require().NotNil(resp)
		s.True(resp.Subtotal.Equal(decimal.NewFromInt(100)), "subtotal, got %s", resp.Subtotal)
		s.True(resp.Total.Equal(decimal.NewFromInt(100)), "total, got %s", resp.Total)
		s.True(resp.AmountDue.Equal(decimal.NewFromInt(100)), "amount_due, got %s", resp.AmountDue)
		s.True(resp.AmountRemaining.Equal(decimal.NewFromInt(70)), "amount_remaining, got %s", resp.AmountRemaining)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetUnpaidInvoicesToBePaid — inline compute of uncomputed drafts
// ─────────────────────────────────────────────────────────────────────────────

func (s *InvoicePreviewSuite) TestGetUnpaidInvoicesComputesUncomputedDrafts() {
	ctx := s.GetContext()

	// Draft for a period that has already ended, never computed.
	periodStart := s.testData.now.Add(-40 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(-10 * 24 * time.Hour)
	draft, err := s.service.CreateDraftInvoiceForSubscription(
		ctx, s.testData.sub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
	s.Require().NoError(err)
	s.Require().True(draft.Total.IsZero(), "draft starts at zero before compute")

	resp, err := s.service.GetUnpaidInvoicesToBePaid(ctx, dto.GetUnpaidInvoicesToBePaidRequest{
		CustomerID: s.testData.customer.ID,
		Currency:   "usd",
	})
	s.NoError(err)
	s.Require().NotNil(resp)

	// The draft must have been computed inline and included with real amounts.
	s.Require().Len(resp.Invoices, 1)
	s.Equal(draft.ID, resp.Invoices[0].ID)
	s.True(resp.TotalUnpaidAmount.IsPositive(),
		"inline compute must surface the fixed charge, got %s", resp.TotalUnpaidAmount)

	// Read back: the draft is now computed.
	stored, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.NoError(err)
	s.NotNil(stored.LastComputedAt)
	s.True(stored.AmountRemaining.IsPositive())

	s.Run("validation_error_for_missing_customer", func() {
		_, err := s.service.GetUnpaidInvoicesToBePaid(ctx, dto.GetUnpaidInvoicesToBePaidRequest{Currency: "usd"})
		s.Error(err)
	})
}

func (s *InvoicePreviewSuite) TestPreviewHideZeroChargesAcrossVariants() {
	ctx := s.GetContext()

	type previewFn func(ctx context.Context, req dto.GetPreviewInvoiceRequest) (*dto.InvoiceResponse, error)
	variants := []struct {
		name string
		fn   previewFn
	}{
		{name: "internal_preview", fn: s.service.GetInternalPreviewInvoice},
		{name: "meter_usage_preview", fn: s.service.GetMeterUsagePreviewInvoice},
	}

	for _, v := range variants {
		s.Run(v.name+"_hides_zero_amount_line_items", func() {
			resp, err := v.fn(ctx, dto.GetPreviewInvoiceRequest{
				SubscriptionID:           s.testData.sub.ID,
				HideZeroChargesLineItems: true,
			})
			s.NoError(err)
			s.Require().NotNil(resp)
			s.Require().NotEmpty(resp.LineItems)
			for _, li := range resp.LineItems {
				s.False(li.Amount.IsZero(), "zero-amount items must be hidden")
			}
		})
	}
}

func (s *InvoicePreviewSuite) TestGetUnpaidInvoicesExcludesFuturePeriodDrafts() {
	ctx := s.GetContext()

	// Draft whose billing period has not ended yet: charges are not due.
	periodStart := s.testData.now.Add(-24 * time.Hour)
	periodEnd := s.testData.now.Add(6 * 24 * time.Hour)
	draft, err := s.service.CreateDraftInvoiceForSubscription(
		ctx, s.testData.sub.ID, periodStart, periodEnd, types.ReferencePointPeriodEnd)
	s.Require().NoError(err)
	_, err = s.service.ComputeInvoice(ctx, draft.ID, nil)
	s.Require().NoError(err)

	resp, err := s.service.GetUnpaidInvoicesToBePaid(ctx, dto.GetUnpaidInvoicesToBePaidRequest{
		CustomerID: s.testData.customer.ID,
		Currency:   "usd",
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	for _, inv := range resp.Invoices {
		s.NotEqual(draft.ID, inv.ID, "drafts for open periods must not count as unpaid")
	}
	s.True(resp.TotalUnpaidAmount.IsZero(), "no due invoices expected, got %s", resp.TotalUnpaidAmount)
}

func (s *InvoicePreviewSuite) TestPreviewMissingCustomerReturnsError() {
	ctx := s.GetContext()

	// Subscription pointing at a customer that does not exist.
	ghostSub := &subscription.Subscription{
		ID:                 "sub_pv_ghost_cust",
		PlanID:             s.testData.plan.ID,
		CustomerID:         "cust_ghost_pv",
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		BillingAnchor:      s.testData.sub.CurrentPeriodEnd,
		CurrentPeriodStart: s.testData.sub.CurrentPeriodStart,
		CurrentPeriodEnd:   s.testData.sub.CurrentPeriodEnd,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, ghostSub, nil))

	req := dto.GetPreviewInvoiceRequest{SubscriptionID: ghostSub.ID}

	s.Run("preview", func() {
		resp, err := s.service.GetPreviewInvoice(ctx, req)
		s.Error(err)
		s.Nil(resp)
	})
	s.Run("internal_preview", func() {
		resp, err := s.service.GetInternalPreviewInvoice(ctx, req)
		s.Error(err)
		s.Nil(resp)
	})
	s.Run("meter_usage_preview", func() {
		resp, err := s.service.GetMeterUsagePreviewInvoice(ctx, req)
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *InvoicePreviewSuite) TestGetUnpaidInvoicesSkipsUncomputableDraftsAndPaidInvoices() {
	ctx := s.GetContext()

	// Uncomputed draft whose subscription is missing: inline compute fails and
	// the draft is skipped instead of failing the whole listing.
	periodStart := s.testData.now.Add(-40 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(-10 * 24 * time.Hour)
	broken := &invoice.Invoice{
		ID:             "inv_pv_unpaid_broken",
		CustomerID:     s.testData.customer.ID,
		SubscriptionID: lo.ToPtr("sub_ghost_unpaid"),
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  types.InvoiceStatusDraft,
		PaymentStatus:  types.PaymentStatusPending,
		Currency:       "usd",
		BillingReason:  string(types.InvoiceBillingReasonSubscriptionCycle),
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, broken))

	// Finalized invoice already paid (remaining amount recorded but status
	// succeeded): must be excluded from the unpaid total.
	paid := &invoice.Invoice{
		ID:              "inv_pv_unpaid_paid",
		CustomerID:      s.testData.customer.ID,
		InvoiceType:     types.InvoiceTypeOneOff,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusSucceeded,
		Currency:        "usd",
		Subtotal:        decimal.NewFromInt(30),
		Total:           decimal.NewFromInt(30),
		AmountDue:       decimal.NewFromInt(30),
		AmountRemaining: decimal.NewFromInt(30),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.CreateWithLineItems(ctx, paid))

	resp, err := s.service.GetUnpaidInvoicesToBePaid(ctx, dto.GetUnpaidInvoicesToBePaidRequest{
		CustomerID: s.testData.customer.ID,
		Currency:   "usd",
	})
	s.NoError(err)
	s.Require().NotNil(resp)
	s.Empty(resp.Invoices, "neither the broken draft nor the paid invoice is due")
	s.True(resp.TotalUnpaidAmount.IsZero())
}
