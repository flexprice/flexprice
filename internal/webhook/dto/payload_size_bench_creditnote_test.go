package webhookDto

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// buildCreditNoteResponse builds a realistic full credit note API response, scaling
// line items and nesting representative full invoice, subscription, and customer objects,
// exactly as CreditNoteResponse carries them.
func buildCreditNoteResponse(nLineItems int) *dto.CreditNoteResponse {
	lineItems := make([]*creditnote.CreditNoteLineItem, nLineItems)
	for i := 0; i < nLineItems; i++ {
		lineItems[i] = &creditnote.CreditNoteLineItem{
			ID:                fmt.Sprintf("cnli_%d", i),
			CreditNoteID:      "cn_bench_creditnote",
			InvoiceLineItemID: fmt.Sprintf("inv_li_%d", i),
			DisplayName:       fmt.Sprintf("Refund - Tier %d", i),
			Amount:            decimal.NewFromInt(int64(10 + i)),
			Currency:          "usd",
			Metadata:          types.Metadata{"source": "benchmark"},
			EnvironmentID:     "env_bench",
		}
	}

	return &dto.CreditNoteResponse{
		CreditNote: &creditnote.CreditNote{
			ID:               "cn_bench_creditnote",
			CreditNoteNumber: "CN-BENCH-0001",
			InvoiceID:        "inv_bench_invoice",
			CustomerID:       "cust_bench_customer",
			SubscriptionID:   lo.ToPtr("sub_bench_subscription"),
			CreditNoteStatus: types.CreditNoteStatusFinalized,
			CreditNoteType:   types.CreditNoteTypeAdjustment,
			Reason:           types.CreditNoteReasonSubscriptionCancellation,
			Memo:             "Benchmark credit note",
			Currency:         "usd",
			TotalAmount:      decimal.NewFromInt(500),
			FinalizedAt:      lo.ToPtr(benchTime),
			Metadata:         types.Metadata{"source": "benchmark"},
			LineItems:        lineItems,
		},
		Invoice:      buildInvoiceResponse(3),
		Subscription: buildSubscriptionResponse(3, 3, 3),
		Customer: &customer.Customer{
			ID:         "cust_bench_customer",
			ExternalID: "ext_cust_bench_customer",
			Name:       "Benchmark Customer Inc.",
			Email:      "billing@benchmark-customer.example",
		},
	}
}

func BenchmarkCreditNotePayloadSize(b *testing.B) {
	tiers := []struct {
		name string
		n    int
	}{{"small", 200}, {"medium", 800}, {"huge", 2000}}

	for _, tier := range tiers {
		full := buildCreditNoteResponse(tier.n)

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
				payload := NewCreditNoteWebhookPayload(full, types.WebhookEventCreditNoteUpdated)
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
