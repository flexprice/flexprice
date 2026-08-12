package webhookDto

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// buildInvoiceResponse builds a realistic full invoice API response, scaling line items
// -- the collection that dominates invoice payload size in production -- and nesting a
// representative full subscription and customer, exactly as InvoiceResponse carries them.
func buildInvoiceResponse(nLineItems int) *dto.InvoiceResponse {
	lineItems := make([]*dto.InvoiceLineItemResponse, nLineItems)
	for i := 0; i < nLineItems; i++ {
		lineItems[i] = &dto.InvoiceLineItemResponse{
			InvoiceLineItem: invoice.InvoiceLineItem{
				ID:              fmt.Sprintf("inv_li_%d", i),
				PriceID:         lo.ToPtr(fmt.Sprintf("price_%d", i)),
				DisplayName:     lo.ToPtr(fmt.Sprintf("API Usage - Tier %d", i)),
				PlanDisplayName: lo.ToPtr("Benchmark Plan"),
				Amount:          decimal.NewFromInt(int64(10 + i)),
				Quantity:        decimal.NewFromInt(int64(1 + i)),
				PeriodStart:     lo.ToPtr(benchTime),
				PeriodEnd:       lo.ToPtr(benchTime.AddDate(0, 1, 0)),
			},
		}
	}

	return &dto.InvoiceResponse{
		Invoice: invoice.Invoice{
			ID:              "inv_bench_invoice",
			CustomerID:      "cust_bench_customer",
			SubscriptionID:  lo.ToPtr("sub_bench_subscription"),
			InvoiceType:     types.InvoiceTypeSubscription,
			InvoiceStatus:   types.InvoiceStatusFinalized,
			PaymentStatus:   types.PaymentStatusSucceeded,
			Currency:        "usd",
			AmountDue:       decimal.NewFromInt(5000),
			AmountPaid:      decimal.NewFromInt(5000),
			AmountRemaining: decimal.NewFromInt(0),
			Total:           decimal.NewFromInt(5000),
			Subtotal:        decimal.NewFromInt(5000),
			InvoiceNumber:   lo.ToPtr("INV-BENCH-0001"),
			DueDate:         lo.ToPtr(benchTime.AddDate(0, 0, 14)),
			FinalizedAt:     lo.ToPtr(benchTime),
			PeriodStart:     lo.ToPtr(benchTime),
			PeriodEnd:       lo.ToPtr(benchTime.AddDate(0, 1, 0)),
			BillingReason:   "SUBSCRIPTION_CYCLE",
			Metadata:        types.Metadata{"source": "benchmark"},
		},
		Subscription: buildSubscriptionResponse(3, 3, 3),
		Customer:     buildCustomerResponse(3),
		LineItems:    lineItems,
	}
}

func BenchmarkInvoicePayloadSize(b *testing.B) {
	tiers := []struct {
		name string
		n    int
	}{{"small", 5}, {"medium", 50}, {"huge", 500}}

	for _, tier := range tiers {
		full := buildInvoiceResponse(tier.n)

		b.Run(tier.name+"/old", func(b *testing.B) {
			var size int
			for i := 0; i < b.N; i++ {
				data, err := json.Marshal(full)
				if err != nil {
					b.Fatal(err)
				}
				size = len(data)
			}
			b.ReportMetric(float64(size), "wire_bytes")
		})

		b.Run(tier.name+"/new", func(b *testing.B) {
			var size int
			for i := 0; i < b.N; i++ {
				payload := NewInvoiceWebhookPayload(full, types.WebhookEventInvoiceUpdateFinalized)
				data, err := json.Marshal(payload)
				if err != nil {
					b.Fatal(err)
				}
				size = len(data)
			}
			b.ReportMetric(float64(size), "wire_bytes")
		})
	}
}
