package contract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/agents/reducto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

type uploadCall struct {
	fileName, mediaType string
	data                []byte
}

type fakeReducto struct {
	uploadResp  *reducto.UploadResponse
	uploadErr   error
	extractResp *reducto.ExtractResponse
	extractErr  error
	uploads     []uploadCall
	extracts    []*reducto.ExtractRequest
}

func (f *fakeReducto) Upload(_ context.Context, fileName, mediaType string, data []byte) (*reducto.UploadResponse, error) {
	f.uploads = append(f.uploads, uploadCall{fileName, mediaType, data})
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	return f.uploadResp, nil
}

func (f *fakeReducto) Extract(_ context.Context, req *reducto.ExtractRequest) (*reducto.ExtractResponse, error) {
	f.extracts = append(f.extracts, req)
	if f.extractErr != nil {
		return nil, f.extractErr
	}
	return f.extractResp, nil
}

func (f *fakeReducto) IsConfigured() bool { return true }

func extractor() *ContractExtractionService {
	return NewContractExtractionService(&fakeReducto{}, logger.NewNoopLogger()).(*ContractExtractionService)
}

func cited(value any, page int, quote string, confidence float64) map[string]any {
	return map[string]any{
		"value": value,
		"citations": []any{map[string]any{
			"type": "Text", "content": quote, "confidence": "high",
			"bbox":                map[string]any{"left": 0.1, "top": 0.2, "width": 0.3, "height": 0.01, "page": page, "original_page": page},
			"granular_confidence": map[string]any{"extract_confidence": confidence, "parse_confidence": 1.0},
		}},
	}
}

func empty() map[string]any { return map[string]any{"value": nil, "citations": []any{}} }

func northwindCited() map[string]any {
	return map[string]any{
		"documents": []any{map[string]any{
			"type": cited("msa", 1, "Master Services Agreement", 0.95), "title": cited("Master Services Agreement", 1, "Master Services Agreement", 0.95),
			"page_start": cited(1, 1, "1", 0.9), "page_end": cited(14, 14, "14", 0.9),
		}},
		"contract_type": cited("new", 1, "Master Services Agreement", 0.88),
		"customer": map[string]any{
			"legal_name": cited("Northwind Health, Inc.", 1, "Northwind Health, Inc.", 0.99), "billing_address": cited("1200 Harbor Boulevard, Seattle, WA 98101", 1, "1200 Harbor Boulevard", 0.96),
			"primary_contact": cited("Dana Whitfield", 14, "Dana Whitfield", 0.97), "billing_email": empty(),
		},
		"term": map[string]any{
			"signed_date": cited("2026-10-24", 14, "October 24, 2026", 0.98), "effective_date": cited("2026-11-01", 3, "November 1, 2026", 0.97),
			"term_months": cited("12", 3, "twelve (12) months", 0.92), "auto_renew": cited("true", 12, "renews automatically", 0.84),
			"renewal_term_months": cited(12, 12, "twelve-month terms", 0.84), "notice_days": cited("60", 12, "sixty (60) days", 0.84),
			"renewal_price_change": cited("Up to 5%", 12, "up to 5%", 0.95), "cancellation": cited("Material breach only", 12, "material breach", 0.93),
		},
		"billing": map[string]any{
			"structure": cited("Platform fee plus usage", 9, "Platform Fee and Usage Fees", 0.96), "currency": cited("USD", 9, "US$", 0.95),
			"invoice_day": empty(), "payment_terms": cited("Net 30", 4, "within thirty (30) days", 0.96),
			"late_fee": cited("1% per month", 4, "1% per month", 0.95), "tax": cited("Customer pays", 4, "Customer pays", 0.9),
		},
		"charges": []any{
			map[string]any{
				"kind": cited("recurring_fixed", 9, "Platform Fee", 0.95), "name": cited("Platform Fee", 9, "Platform Fee", 0.95),
				"amount": cited("184000", 9, "US$184,000 per year", 0.95), "amount_period": cited("ANNUAL", 9, "per year", 0.95),
				"billing_period": cited("QUARTERLY", 9, "invoiced quarterly in advance", 0.88), "billing_timing": cited("advance", 9, "in advance", 0.88),
				"included_quantity": empty(), "unit": empty(), "tier_mode": empty(), "tiers": []any{},
				"trigger": empty(), "revenue_recognition": cited("Evenly over the term", 9, "Platform Fee", 0.8),
			},
			map[string]any{
				"kind": cited("usage", 9, "Additional calls", 0.93), "name": cited("API calls", 9, "Additional calls", 0.93),
				"amount": empty(), "amount_period": empty(),
				"billing_period": cited("MONTHLY", 9, "monthly in arrears", 0.93), "billing_timing": cited("arrears", 9, "in arrears", 0.93),
				"included_quantity": cited(5000000, 9, "5,000,000 API calls", 0.94), "unit": cited("API call", 9, "API calls", 0.94),
				"tier_mode": cited("unknown", 9, "tiered basis", 0.72),
				"tiers": []any{
					map[string]any{"up_to": cited(10000000, 9, "up to 10,000,000", 0.9), "unit_amount": cited("0.002", 9, "US$0.002", 0.9), "flat_amount": empty()},
					map[string]any{"up_to": empty(), "unit_amount": cited("0.0015", 9, "US$0.0015", 0.9), "flat_amount": empty()},
				},
				"trigger": empty(), "revenue_recognition": cited("As used", 9, "Usage Fees", 0.9),
			},
		},
		"commitments":    []any{map[string]any{"amount": cited("200000", 10, "US$200,000", 0.93), "period": cited("ANNUAL", 10, "annual", 0.93), "covers": cited("All fees", 10, "all Fees", 0.93), "shortfall": cited("Year end", 10, "end of each contract year", 0.91)}},
		"credits":        []any{map[string]any{"amount": cited("10000", 10, "US$10,000", 0.97), "kind": cited("onboarding", 10, "onboarding credit", 0.97), "expiry": cited("2027-01-31", 10, "January 31, 2027", 0.97)}},
		"service_levels": []any{map[string]any{"description": cited("5% per 0.1% under 99.9%", 11, "credit of 5%", 0.9)}},
		"doubts":         []any{},
	}
}

func extractResponse(t *testing.T, result map[string]any) *reducto.ExtractResponse {
	t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	credits := 4.0
	return &reducto.ExtractResponse{
		ResponseType: "v3_extract", Result: raw, JobID: "job-1", Duration: 7.9,
		Usage: &reducto.ExtractUsage{NumPages: 14, NumFields: 61, Credits: &credits, ExtractMode: "extract"},
	}
}
