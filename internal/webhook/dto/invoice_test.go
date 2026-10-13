package webhookDto

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// auditBaseModel is the internal bookkeeping every domain row carries. The webhook
// payload must never leak it.
func auditBaseModel() types.BaseModel {
	return types.BaseModel{
		TenantID:  "1cd1a8fb-9d1b-461d-accc-d32df594e436",
		Status:    types.StatusPublished,
		CreatedAt: time.Date(2026, 8, 31, 21, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 31, 21, 0, 0, 0, time.UTC),
		CreatedBy: "8176a17c-1921-492c-b118-003fda5d1fc1",
		UpdatedBy: "8176a17c-1921-492c-b118-003fda5d1fc1",
	}
}

func testPrice(id, meterID string, amount decimal.Decimal) *dto.PriceResponse {
	return &dto.PriceResponse{
		Price: &price.Price{
			ID:                 id,
			Amount:             amount,
			DisplayAmount:      "$" + amount.String(),
			Currency:           "usd",
			Type:               types.PRICE_TYPE_USAGE,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			DisplayName:        "Price " + id,
			MeterID:            meterID,
			EnvironmentID:      "env_01JN4G3Q7HX7YG5TQSZ0TFAY8K",
			BaseModel:          auditBaseModel(),
		},
		Meter: &dto.MeterResponse{
			ID:        meterID,
			Name:      "API Requests",
			EventName: "api_request",
			Aggregation: meter.Aggregation{
				Type:       types.AggregationSum,
				Field:      "tokens",
				Multiplier: lo.ToPtr(decimal.NewFromInt(1000)),
			},
			ResetUsage: types.ResetUsageBillingPeriod,
		},
	}
}

func testLineItem(id, priceID string, amount, quantity decimal.Decimal) *dto.InvoiceLineItemResponse {
	period := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	return &dto.InvoiceLineItemResponse{
		InvoiceLineItem: invoice.InvoiceLineItem{
			ID:                     id,
			InvoiceID:              "inv_01M1CHK8B6Y2B0PEFRPGDVQQYG",
			CustomerID:             "cust_01M1CHK8B6Y2B0PEFRPGDVQQYG",
			SubscriptionID:         lo.ToPtr("subs_01M1CHK8B6Y2B0PEFRPGDVQQYG"),
			EntityID:               lo.ToPtr("plan_01M1CHK8B6Y2B0PEFRPGDVQQYG"),
			EntityType:             lo.ToPtr("plan"),
			PlanDisplayName:        lo.ToPtr("Enterprise Usage Plan"),
			PriceID:                lo.ToPtr(priceID),
			PriceType:              lo.ToPtr("USAGE"),
			MeterID:                lo.ToPtr("meter_api"),
			MeterDisplayName:       lo.ToPtr("API Requests"),
			DisplayName:            lo.ToPtr("API Requests"),
			Amount:                 amount,
			Quantity:               quantity,
			Currency:               "usd",
			PeriodStart:            lo.ToPtr(period),
			PeriodEnd:              lo.ToPtr(period.AddDate(0, 1, 0)),
			SubscriptionLineItemID: lo.ToPtr("subs_li_01M1CHK8B6Y2B0PEFRPGDVQQYG"),
			Metadata:               types.Metadata{"description": "API Requests (Usage Charge)"},
			EnvironmentID:          "env_01JN4G3Q7HX7YG5TQSZ0TFAY8K",
			BaseModel:              auditBaseModel(),
		},
	}
}

func testCustomer() *dto.CustomerResponse {
	return &dto.CustomerResponse{
		Customer: &customer.Customer{
			ID:                "cust_01M1CHK8B6Y2B0PEFRPGDVQQYG",
			ExternalID:        "acct-9912",
			Name:              "Yotta Data Services",
			Email:             "billing@yotta.example",
			AddressLine1:      "Plot 7, Hiranandani Estate",
			AddressCity:       "Mumbai",
			AddressState:      "Maharashtra",
			AddressPostalCode: "400607",
			AddressCountry:    "IN",
			EnvironmentID:     "env_01JN4G3Q7HX7YG5TQSZ0TFAY8K",
			Metadata: map[string]string{
				"gstin__c":         "27AABCU9603R1ZM",
				"pan__c":           "AABCU9603R",
				"sez__c":           "false",
				"demographic":      "indian",
				"invoice_currency": "INR",
			},
			BaseModel: auditBaseModel(),
		},
	}
}

// testInvoiceResponse builds a finalized subscription invoice from the given line
// items, on a plan carrying planPrices. The subscription's own line item references
// price_api, so that price is reachable even when no invoice line item names it.
func testInvoiceResponse(lineItems []*dto.InvoiceLineItemResponse, planPrices []*dto.PriceResponse) *dto.InvoiceResponse {
	period := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	cust := testCustomer()

	sub := &subscription.Subscription{
		ID:                 "subs_01M1CHK8B6Y2B0PEFRPGDVQQYG",
		CustomerID:         cust.ID,
		PlanID:             "plan_01M1CHK8B6Y2B0PEFRPGDVQQYG",
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		StartDate:          period,
		CurrentPeriodStart: period,
		CurrentPeriodEnd:   period.AddDate(0, 1, 0),
		EnvironmentID:      "env_01JN4G3Q7HX7YG5TQSZ0TFAY8K",
		LineItems: []*subscription.SubscriptionLineItem{
			{
				ID:              "subs_li_01M1CHK8B6Y2B0PEFRPGDVQQYG",
				SubscriptionID:  "subs_01M1CHK8B6Y2B0PEFRPGDVQQYG",
				CustomerID:      cust.ID,
				PriceID:         "price_api",
				PriceType:       types.PRICE_TYPE_USAGE,
				MeterID:         "meter_api",
				DisplayName:     "API Requests",
				PlanDisplayName: "Enterprise Usage Plan",
				Quantity:        decimal.NewFromInt(1),
				Currency:        "usd",
				EnvironmentID:   "env_01JN4G3Q7HX7YG5TQSZ0TFAY8K",
				BaseModel:       auditBaseModel(),
			},
		},
		BaseModel: auditBaseModel(),
	}

	return &dto.InvoiceResponse{
		Invoice: invoice.Invoice{
			ID:                         "inv_01M1CHK8B6Y2B0PEFRPGDVQQYG",
			CustomerID:                 cust.ID,
			SubscriptionID:             lo.ToPtr(sub.ID),
			InvoiceType:                types.InvoiceTypeSubscription,
			InvoiceStatus:              types.InvoiceStatusFinalized,
			PaymentStatus:              types.PaymentStatusPending,
			Currency:                   "usd",
			AmountDue:                  decimal.NewFromInt(120),
			AmountPaid:                 decimal.Zero,
			AmountRemaining:            decimal.NewFromInt(120),
			Subtotal:                   decimal.NewFromInt(120),
			Total:                      decimal.NewFromInt(120),
			TotalTax:                   decimal.NewFromInt(20),
			TotalDiscount:              decimal.Zero,
			TotalPrepaidCreditsApplied: decimal.NewFromInt(5),
			InvoiceNumber:              lo.ToPtr("INV-2026-0042"),
			IdempotencyKey:             lo.ToPtr("idem_01M1CHK8B6Y2B0PEFRPGDVQQYG"),
			PeriodStart:                lo.ToPtr(period),
			PeriodEnd:                  lo.ToPtr(period.AddDate(0, 1, 0)),
			FinalizedAt:                lo.ToPtr(period.AddDate(0, 1, 0)),
			InvoicePDFURL:              lo.ToPtr("https://pdf.example/inv.pdf"),
			BillingReason:              string(types.InvoiceBillingReasonSubscriptionCycle),
			EnvironmentID:              "env_01JN4G3Q7HX7YG5TQSZ0TFAY8K",
			BaseModel:                  auditBaseModel(),
		},
		LineItems: lineItems,
		Customer:  cust,
		Subscription: &dto.SubscriptionResponse{
			Subscription: sub,
			Customer:     cust,
			Plan: &dto.PlanResponse{
				Plan: &plan.Plan{
					ID:            "plan_01M1CHK8B6Y2B0PEFRPGDVQQYG",
					Name:          "Enterprise Usage Plan",
					LookupKey:     "enterprise_usage",
					Description:   "Metered enterprise plan",
					EnvironmentID: "env_01JN4G3Q7HX7YG5TQSZ0TFAY8K",
					BaseModel:     auditBaseModel(),
				},
				Prices: planPrices,
			},
		},
	}
}

func TestNewInvoice_DropsOnlyLineItemsWithNoAmountAndNoQuantity(t *testing.T) {
	tests := []struct {
		name     string
		amount   decimal.Decimal
		quantity decimal.Decimal
		wantKept bool
	}{
		{
			name:     "billed usage is kept",
			amount:   decimal.NewFromInt(120),
			quantity: decimal.NewFromInt(1200000),
			wantKept: true,
		},
		{
			name:     "usage fully covered by an entitlement is kept",
			amount:   decimal.Zero,
			quantity: decimal.NewFromInt(1200000),
			wantKept: true,
		},
		{
			name:     "usage fully discounted by a coupon is kept",
			amount:   decimal.Zero,
			quantity: decimal.NewFromInt(40),
			wantKept: true,
		},
		{
			name:     "fan-out window with no usage at all is dropped",
			amount:   decimal.Zero,
			quantity: decimal.Zero,
			wantKept: false,
		},
		{
			name:     "credit line with negative amount and no quantity is kept",
			amount:   decimal.NewFromInt(-25),
			quantity: decimal.Zero,
			wantKept: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := testInvoiceResponse(
				[]*dto.InvoiceLineItemResponse{testLineItem("inv_li_1", "price_api", tt.amount, tt.quantity)},
				[]*dto.PriceResponse{testPrice("price_api", "meter_api", decimal.NewFromInt(1))},
			)

			got := NewInvoice(resp)
			require.NotNil(t, got)

			if tt.wantKept {
				require.Len(t, got.LineItems, 1)
				assert.Equal(t, "inv_li_1", got.LineItems[0].ID)
			} else {
				assert.Empty(t, got.LineItems)
			}
		})
	}
}

func TestNewInvoice_KeepsSurvivingLineItemsInOrder(t *testing.T) {
	resp := testInvoiceResponse(
		[]*dto.InvoiceLineItemResponse{
			testLineItem("inv_li_1", "price_api", decimal.NewFromInt(100), decimal.NewFromInt(10)),
			testLineItem("inv_li_2", "price_api", decimal.Zero, decimal.Zero),
			testLineItem("inv_li_3", "price_storage", decimal.NewFromInt(20), decimal.NewFromInt(2)),
		},
		[]*dto.PriceResponse{testPrice("price_api", "meter_api", decimal.NewFromInt(1))},
	)

	got := NewInvoice(resp)
	require.Len(t, got.LineItems, 2)
	assert.Equal(t, "inv_li_1", got.LineItems[0].ID)
	assert.Equal(t, "inv_li_3", got.LineItems[1].ID)
}

func TestNewInvoice_PlanPricesLimitedToPricesTheInvoiceReferences(t *testing.T) {
	resp := testInvoiceResponse(
		[]*dto.InvoiceLineItemResponse{
			testLineItem("inv_li_1", "price_api", decimal.NewFromInt(100), decimal.NewFromInt(10)),
			// zero line item: dropped from line_items, but its price is still referenced
			// by the surviving subscription line item below, so it must not vanish.
			testLineItem("inv_li_2", "price_gone", decimal.Zero, decimal.Zero),
		},
		[]*dto.PriceResponse{
			testPrice("price_api", "meter_api", decimal.NewFromInt(1)),
			testPrice("price_storage", "meter_storage", decimal.NewFromInt(2)),
			testPrice("price_egress", "meter_egress", decimal.NewFromInt(3)),
			testPrice("price_support", "meter_support", decimal.NewFromInt(4)),
		},
	)

	got := NewInvoice(resp)
	require.NotNil(t, got.Subscription)
	require.NotNil(t, got.Subscription.Plan)

	ids := lo.Map(got.Subscription.Plan.Prices, func(p *Price, _ int) string { return p.ID })
	// price_api: on a surviving line item. price_storage: on the subscription line item
	// (testInvoiceResponse wires subs_li to price_api, so only price_api qualifies there).
	assert.ElementsMatch(t, []string{"price_api"}, ids)
}

func TestNewInvoice_PlanPricesIncludePricesReferencedOnlyBySubscriptionLineItems(t *testing.T) {
	resp := testInvoiceResponse(
		[]*dto.InvoiceLineItemResponse{
			testLineItem("inv_li_1", "price_storage", decimal.NewFromInt(100), decimal.NewFromInt(10)),
		},
		[]*dto.PriceResponse{
			testPrice("price_api", "meter_api", decimal.NewFromInt(1)),
			testPrice("price_storage", "meter_storage", decimal.NewFromInt(2)),
			testPrice("price_egress", "meter_egress", decimal.NewFromInt(3)),
		},
	)

	got := NewInvoice(resp)
	ids := lo.Map(got.Subscription.Plan.Prices, func(p *Price, _ int) string { return p.ID })
	// price_storage from the invoice line item, price_api from the subscription line item.
	assert.ElementsMatch(t, []string{"price_storage", "price_api"}, ids)
	assert.NotContains(t, ids, "price_egress")
}

func TestNewInvoice_OmitsInternalAuditFields(t *testing.T) {
	resp := testInvoiceResponse(
		[]*dto.InvoiceLineItemResponse{
			testLineItem("inv_li_1", "price_api", decimal.NewFromInt(100), decimal.NewFromInt(10)),
		},
		[]*dto.PriceResponse{testPrice("price_api", "meter_api", decimal.NewFromInt(1))},
	)

	raw, err := json.Marshal(NewInvoiceWebhookPayload(resp, types.WebhookEventInvoiceUpdateFinalized))
	require.NoError(t, err)

	for _, leaked := range []string{"created_by", "updated_by", "tenant_id"} {
		assert.NotContains(t, string(raw), `"`+leaked+`"`,
			"webhook payload must not expose internal field %q", leaked)
	}
}

// TestNewInvoice_PreservesFieldsTheInvoicePDFPipelineReads pins the field inventory a
// downstream PDF/e-invoice consumer reads off this webhook. A field disappearing here
// silently breaks their rendering, so the contract is asserted explicitly.
func TestNewInvoice_PreservesFieldsTheInvoicePDFPipelineReads(t *testing.T) {
	resp := testInvoiceResponse(
		[]*dto.InvoiceLineItemResponse{
			testLineItem("inv_li_1", "price_api", decimal.NewFromInt(100), decimal.NewFromInt(10)),
		},
		[]*dto.PriceResponse{testPrice("price_api", "meter_api", decimal.NewFromInt(1))},
	)

	raw, err := json.Marshal(NewInvoiceWebhookPayload(resp, types.WebhookEventInvoiceUpdateFinalized))
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))

	requiredPaths := []string{
		// header + statutory block
		"invoice.id",
		"invoice.invoice_number",
		"invoice.created_at",
		"invoice.period_start",
		"invoice.period_end",
		"invoice.invoice_type",
		"invoice.idempotency_key",
		"invoice.subscription_id",
		"invoice.currency",
		"invoice.invoice_pdf_url",
		// totals
		"invoice.amount_due",
		"invoice.amount_paid",
		"invoice.total",
		"invoice.subtotal",
		"invoice.total_tax",
		"invoice.total_discount",
		"invoice.total_prepaid_credits_applied",
		// bill-to
		"invoice.customer.id",
		"invoice.customer.external_id",
		"invoice.customer.name",
		"invoice.customer.email",
		"invoice.customer.address_line1",
		"invoice.customer.address_city",
		"invoice.customer.address_state",
		"invoice.customer.address_postal_code",
		"invoice.customer.address_country",
		"invoice.customer.metadata",
		// line items
		"invoice.line_items.0.amount",
		"invoice.line_items.0.price_id",
		"invoice.line_items.0.quantity",
		"invoice.line_items.0.price_type",
		"invoice.line_items.0.display_name",
		"invoice.line_items.0.plan_display_name",
		"invoice.line_items.0.meter_id",
		"invoice.line_items.0.metadata",
		// subscription + plan pricing metadata
		"invoice.subscription.id",
		"invoice.subscription.line_items.0.price_id",
		"invoice.subscription.plan.prices.0.amount",
		"invoice.subscription.plan.prices.0.display_amount",
		"invoice.subscription.plan.prices.0.meter.name",
		"invoice.subscription.plan.prices.0.meter.aggregation.type",
		"invoice.subscription.plan.prices.0.meter.aggregation.field",
		"invoice.subscription.plan.prices.0.meter.aggregation.multiplier",
	}

	for _, path := range requiredPaths {
		assert.NotNil(t, lookupJSONPath(payload, path), "missing required webhook field %q", path)
	}
}

// lookupJSONPath walks a decoded JSON tree using dot-separated keys; numeric segments
// index into arrays.
func lookupJSONPath(root any, path string) any {
	cur := root
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil
			}
			cur = node[idx]
		default:
			return nil
		}
	}
	return cur
}

// TestNewInvoice_LineItemEncodingCostStaysWithinBudget guards the per-line-item
// encoding cost against regression; it is not a delivery-time guarantee. Enough
// line items, or large enough per-line metadata, can still exceed the limit —
// there is deliberately no size enforcement in the delivery path, so an oversized
// payload fails and is recorded on the system_event with its failure reason.
func TestNewInvoice_LineItemEncodingCostStaysWithinBudget(t *testing.T) {
	const svixPayloadLimitBytes = 1024 * 1024

	lineItems := make([]*dto.InvoiceLineItemResponse, 0, 1000)
	for i := 0; i < 1000; i++ {
		lineItems = append(lineItems, testLineItem(
			"inv_li_01M1CHK8B6Y2B0PEFRPGDVQ"+string(rune('a'+i%26))+string(rune('a'+i/26%26)),
			"price_api",
			decimal.NewFromInt(int64(i+1)),
			decimal.NewFromInt(int64(i+1)),
		))
	}

	resp := testInvoiceResponse(lineItems, []*dto.PriceResponse{testPrice("price_api", "meter_api", decimal.NewFromInt(1))})

	raw, err := json.Marshal(NewInvoiceWebhookPayload(resp, types.WebhookEventInvoiceUpdateFinalized))
	require.NoError(t, err)

	assert.Less(t, len(raw), svixPayloadLimitBytes,
		"1000 non-zero line items marshalled to %d bytes, over the Svix limit", len(raw))
}

// A converted invoice's webhook payload exposes fx_conversion on the invoice and on each line, the
// same detail as the API.
func TestNewInvoice_IncludesFxConversionAndLineOriginals(t *testing.T) {
	li := testLineItem("inv_li_fx", "price_api", decimal.NewFromInt(8300), decimal.NewFromInt(1))
	li.Currency = "inr"
	li.FxConversion = &types.FxConversion{
		ChargeCurrency: "usd",
		Source:         types.FxConversionSource{Subtotal: decimal.NewFromInt(100), Net: decimal.NewFromInt(100)},
	}

	resp := testInvoiceResponse([]*dto.InvoiceLineItemResponse{li}, nil)
	resp.Currency = "inr"
	resp.FxConversion = &types.FxConversion{
		ChargeCurrency:  "usd",
		BillingCurrency: "inr",
		Rate:            decimal.NewFromInt(83),
		Scope:           "tenant",
	}

	raw, err := json.Marshal(NewInvoiceWebhookPayload(resp, types.WebhookEventInvoiceUpdateFinalized))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	assert.Contains(t, s, `"fx_conversion"`, "webhook must include fx_conversion")
	assert.Contains(t, s, `"charge_currency":"usd"`)
	assert.Contains(t, s, `"subtotal":"100"`, "line item must include its fx_conversion source")
}
