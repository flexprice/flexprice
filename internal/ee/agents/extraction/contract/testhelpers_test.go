package contract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/agents/extraction"

	"github.com/flexprice/flexprice/internal/ee/agents/openrouter"
	"github.com/stretchr/testify/require"
)

type fakeOpenRouter struct {
	model     string
	responses []*openrouter.Response
	errs      []error
	requests  []*openrouter.Request
}

func (f *fakeOpenRouter) Complete(_ context.Context, req *openrouter.Request) (*openrouter.Response, error) {
	i := len(f.requests)
	f.requests = append(f.requests, req)
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	return f.responses[i], nil
}

func (f *fakeOpenRouter) DefaultModel() string { return f.model }

func (f *fakeOpenRouter) IsConfigured() bool { return true }

func read(value string, confidence, page int, quote string) extraction.Term {
	return extraction.Term{Value: value, Confidence: confidence, Page: page, Quote: quote, Options: []string{}}
}

func missing() extraction.Term { return extraction.Term{Options: []string{}} }

func northwind() *extraction.Result {
	return &extraction.Result{
		Documents: []extraction.Document{
			{Type: DocumentTypeMSA, Title: "Master Services Agreement", PageStart: 1, PageEnd: 14},
			{Type: DocumentTypeOrderForm, Title: "Order Form", PageStart: 9, PageEnd: 10},
		},
		ContractType: read("new", 95, 1, "Master Services Agreement"),
		Customer: extraction.Customer{
			LegalName:      read("Northwind Health, Inc.", 99, 1, "Northwind Health, Inc."),
			BillingAddress: read("1200 Harbor Boulevard, Seattle, WA 98101", 96, 1, "1200 Harbor Boulevard, Seattle, WA 98101"),
			PrimaryContact: read("Dana Whitfield, Chief Financial Officer", 97, 14, "Dana Whitfield, Chief Financial Officer"),
			BillingEmail:   read("ap@northwindhealth.org", 94, 4, "ap@northwindhealth.org"),
		},
		Term: extraction.ContractTerm{
			SignedDate:         read("2026-10-24", 98, 14, "Date: October 24, 2026"),
			EffectiveDate:      read("2026-11-01", 97, 3, "November 1, 2026"),
			TermMonths:         read("12", 92, 3, "twelve (12) months"),
			AutoRenew:          read("true", 84, 12, "renews automatically"),
			RenewalTermMonths:  read("12", 84, 12, "successive twelve-month terms"),
			NoticeDays:         read("60", 84, 12, "sixty (60) days’ written notice"),
			RenewalPriceChange: read("Up to 5% with 30 days' notice", 95, 12, "increase fees by up to 5%"),
			Cancellation:       read("Material breach not cured within 30 days", 93, 12, "terminate for a material breach"),
		},
		Billing: extraction.Billing{
			Structure:    read("Platform fee plus usage", 96, 9, "Platform Fee and Usage Fees"),
			Currency:     read("USD", 95, 9, "US$"),
			InvoiceDay:   missing(),
			PaymentTerms: read("Net 30", 96, 4, "within thirty (30) days"),
			LateFee:      read("1% per month", 95, 4, "1% per month"),
			Tax:          read("Customer pays applicable taxes", 90, 4, "Customer pays any sales, use or VAT taxes"),
		},
		Charges: []extraction.Charge{
			{
				Kind: ChargeKindRecurringFixed, Name: read("Platform Fee", 95, 9, "Platform Fee"),
				Amount: read("184000", 95, 9, "US$184,000 per year"), AmountPeriod: read("ANNUAL", 95, 9, "per year"),
				BillingPeriod: read("QUARTERLY", 88, 9, "invoiced quarterly in advance"), BillingTiming: read("advance", 88, 9, "in advance"),
				IncludedQuantity: missing(), Unit: missing(), TierMode: missing(), Tiers: []extraction.Tier{}, Trigger: missing(),
				RevenueRecognition: read("Evenly over the term", 80, 9, "Platform Fee"),
			},
			{
				Kind: ChargeKindUsage, Name: read("API calls", 93, 9, "Additional calls"),
				Amount: missing(), AmountPeriod: missing(),
				BillingPeriod: read("MONTHLY", 93, 9, "invoiced monthly in arrears"), BillingTiming: read("arrears", 93, 9, "in arrears"),
				IncludedQuantity: read("5000000", 94, 9, "5,000,000 API calls per month"), Unit: read("API call", 94, 9, "API calls"),
				TierMode: extraction.Term{Confidence: 72, Page: 9, Quote: "tiered basis", Options: []string{"Graduated", "Volume"}, Note: "Tiered without saying which kind."},
				Tiers:    []extraction.Tier{{UpTo: "10000000", UnitAmount: "0.002"}, {UnitAmount: "0.0015"}},
				Trigger:  missing(), RevenueRecognition: read("As used", 90, 9, "Usage Fees"),
			},
			{
				Kind: ChargeKindOneTime, Name: read("Implementation Fee", 82, 9, "Implementation Fee"),
				Amount: read("12000", 82, 9, "US$12,000"), AmountPeriod: read("ONETIME", 82, 9, "one-time"),
				BillingPeriod: missing(), BillingTiming: missing(), IncludedQuantity: missing(), Unit: missing(), TierMode: missing(),
				Tiers: []extraction.Tier{}, Trigger: read("Go-live", 82, 9, "invoiced on Go-Live"), RevenueRecognition: read("At go-live", 82, 9, "Go-Live"),
			},
		},
		Commitments: []extraction.Commitment{{
			Amount: read("200000", 93, 10, "minimum annual spend of US$200,000"), Period: read("ANNUAL", 93, 10, "annual"),
			Covers: read("All fees", 93, 10, "across all Fees"), Shortfall: read("Invoiced at the end of each contract year", 91, 10, "shortfall is invoiced"),
		}},
		Credits: []extraction.Credit{{
			Amount: read("10000", 97, 10, "US$10,000"), Kind: read("onboarding", 97, 10, "onboarding credit"),
			Expiry: read("2027-01-31", 97, 10, "expires January 31, 2027"),
		}},
		ServiceLevels: []extraction.ServiceLevel{{Description: read("5% of monthly fee per 0.1% under 99.9%, up to 20%", 90, 11, "credit of 5%")}},
		Doubts:        []string{},
	}
}

func modelOutput(t *testing.T, r *extraction.Result) string {
	t.Helper()
	b, err := json.Marshal(r)
	require.NoError(t, err)
	return string(b)
}
