package contract

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	contractagent "github.com/flexprice/flexprice/internal/ee/agents/services/extraction/contract"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

func read(value string, confidence, page int, quote string) extraction.Term {
	return extraction.Term{Value: value, Confidence: confidence, Page: page, Quote: quote, Options: []string{}}
}

func missing() extraction.Term { return extraction.Term{Options: []string{}} }

func northwind() *extraction.Result {
	return &extraction.Result{
		Documents: []extraction.Document{
			{Type: contractagent.DocumentTypeMSA, Title: "Master Services Agreement", PageStart: 1, PageEnd: 14},
			{Type: contractagent.DocumentTypeOrderForm, Title: "Order Form", PageStart: 9, PageEnd: 10},
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
				Kind: contractagent.ChargeKindRecurringFixed, Name: read("Platform Fee", 95, 9, "Platform Fee"),
				Amount: read("184000", 95, 9, "US$184,000 per year"), AmountPeriod: read("ANNUAL", 95, 9, "per year"),
				BillingPeriod: read("QUARTERLY", 88, 9, "invoiced quarterly in advance"), BillingTiming: read("advance", 88, 9, "in advance"),
				IncludedQuantity: missing(), Unit: missing(), TierMode: missing(), Tiers: []extraction.Tier{}, Trigger: missing(),
				RevenueRecognition: read("Evenly over the term", 80, 9, "Platform Fee"),
			},
			{
				Kind: contractagent.ChargeKindUsage, Name: read("API calls", 93, 9, "Additional calls"),
				Amount: missing(), AmountPeriod: missing(),
				BillingPeriod: read("MONTHLY", 93, 9, "invoiced monthly in arrears"), BillingTiming: read("arrears", 93, 9, "in arrears"),
				IncludedQuantity: read("5000000", 94, 9, "5,000,000 API calls per month"), Unit: read("API call", 94, 9, "API calls"),
				TierMode: extraction.Term{Confidence: 72, Page: 9, Quote: "tiered basis", Options: []string{"Graduated", "Volume"}, Note: "Tiered without saying which kind."},
				Tiers:    []extraction.Tier{{UpTo: "10000000", UnitAmount: "0.002"}, {UnitAmount: "0.0015"}},
				Trigger:  missing(), RevenueRecognition: read("As used", 90, 9, "Usage Fees"),
			},
			{
				Kind: contractagent.ChargeKindOneTime, Name: read("Implementation Fee", 82, 9, "Implementation Fee"),
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

func TestDerive(t *testing.T) {
	base := func() *extraction.Result {
		r := &extraction.Result{}
		r.Term.EffectiveDate = read("2026-01-15", 95, 1, "q")
		r.Term.TermMonths = read("24", 92, 1, "q")
		return r
	}
	t.Run("end date and no notice", func(t *testing.T) {
		d := enricher().derive(base())
		require.Equal(t, "2028-01-14", d.EndDate.Value)
		require.Equal(t, 92, d.EndDate.Confidence)
		require.False(t, d.NoticeDeadline.IsSet())
	})
	t.Run("missing inputs derive nothing", func(t *testing.T) {
		r := base()
		r.Term.TermMonths = missing()
		d := enricher().derive(r)
		require.False(t, d.EndDate.IsSet())
		require.Empty(t, d.InvoiceSchedules)
	})
	schedule := func(period, quoted, timing, amount string) []extraction.InvoiceSchedule {
		r := base()
		r.Charges = []extraction.Charge{{Kind: contractagent.ChargeKindRecurringFixed, Name: read("Fee", 90, 1, "q"), Amount: read(amount, 90, 1, "q"),
			AmountPeriod: read(quoted, 90, 1, "q"), BillingPeriod: read(period, 90, 1, "q"), BillingTiming: read(timing, 90, 1, "q")}}
		return enricher().derive(r).InvoiceSchedules
	}
	t.Run("monthly from annual", func(t *testing.T) {
		s := schedule("MONTHLY", "ANNUAL", "advance", "120000")
		require.Len(t, s, 1)
		require.Equal(t, "10000.00", s[0].AmountPerInvoice)
		require.Len(t, s[0].Dates, 24)
	})
	t.Run("half yearly quoted per period", func(t *testing.T) {
		s := schedule("HALF_YEARLY", "", "advance", "5000")
		require.Equal(t, "5000.00", s[0].AmountPerInvoice)
		require.Equal(t, []string{"2026-01-15", "2026-07-15", "2027-01-15", "2027-07-15"}, s[0].Dates)
	})
	t.Run("annual", func(t *testing.T) {
		require.Len(t, schedule("ANNUAL", "ANNUAL", "advance", "99")[0].Dates, 2)
	})
	t.Run("arrears and bad amount skipped", func(t *testing.T) {
		require.Empty(t, schedule("MONTHLY", "ANNUAL", "arrears", "100"))
		require.Empty(t, schedule("MONTHLY", "ANNUAL", "advance", "abc"))
		require.Empty(t, schedule("WEEKLY", "ANNUAL", "advance", "100"))
	})
}

func TestNormalizeName(t *testing.T) {
	tests := map[string]string{
		"Northwind Health, Inc.": "northwind health",
		"northwind health":       "northwind health",
		"ACME Corp. Ltd":         "acme",
		"Globex Private Limited": "globex",
		"The Co":                 "the",
		"  Initech, L.L.C.  ":    "initech",
		"":                       "",
	}
	for in, want := range tests {
		require.Equal(t, want, newCustomerDirectory(nil).normalizeName(in), in)
	}
}

func TestNeedsCheck(t *testing.T) {
	require.True(t, enricher().needsCheck(read("x", 89, 1, "q"), 90))
	require.False(t, enricher().needsCheck(read("x", 90, 1, "q"), 90))
	require.True(t, enricher().needsCheck(extraction.Term{Page: 2, Options: []string{"a", "b"}, Confidence: 95}, 90))
	require.False(t, enricher().needsCheck(missing(), 90))
	require.False(t, enricher().needsCheck(enricher().derivedTerm("2027-01-01", 10), 90))
}

func TestAddMonthsClampsMonthEnd(t *testing.T) {
	tests := []struct {
		start  string
		months int
		want   string
	}{
		{"2026-01-31", 1, "2026-02-28"},
		{"2028-01-31", 1, "2028-02-29"},
		{"2026-08-31", 3, "2026-11-30"},
		{"2026-11-15", 3, "2027-02-15"},
	}
	for _, tt := range tests {
		start, _ := time.Parse(dateLayout, tt.start)
		require.Equal(t, tt.want, enricher().addMonths(start, tt.months).Format(dateLayout), tt.start)
	}
}

func TestEnrich_Northwind(t *testing.T) {
	r := northwind()
	require.NoError(t, enricher().enrich(r))
	require.Nil(t, r.CustomerMatch)

	require.Equal(t, "2027-10-31", r.Derived.EndDate.Value)
	require.True(t, r.Derived.EndDate.Derived)
	require.Equal(t, "2027-09-01", r.Derived.NoticeDeadline.Value)
	require.Equal(t, 84, r.Derived.NoticeDeadline.Confidence)
	require.Len(t, r.Derived.InvoiceSchedules, 1)
	require.Equal(t, "46000.00", r.Derived.InvoiceSchedules[0].AmountPerInvoice)
	require.Equal(t, []string{"2026-11-01", "2027-02-01", "2027-05-01", "2027-08-01"}, r.Derived.InvoiceSchedules[0].Dates)

	require.Equal(t, &extraction.ToCheck{Total: 12, Customer: 0, Charges: 7, Billing: 5}, r.ToCheck)
}

func TestEnrich_ResultSurvivesJSONRoundTrip(t *testing.T) {
	r := northwind()
	require.NoError(t, enricher().enrich(r))
	b, err := json.Marshal(r)
	require.NoError(t, err)
	var back extraction.Result
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, r.ToCheck, back.ToCheck)
	require.Equal(t, r.Derived, back.Derived)
}

func enricher() *contractIngestService {
	return &contractIngestService{logger: logger.NewNoopLogger(), reviewThreshold: DefaultReviewThreshold}
}
