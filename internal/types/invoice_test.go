package types

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceBillingReason_TrialStart_Validate(t *testing.T) {
	err := InvoiceBillingReasonSubscriptionTrialStart.Validate()
	require.NoError(t, err, "SUBSCRIPTION_TRIAL_START must be a valid billing reason")
}

func TestInvoiceBillingReason_TrialStart_NotFirstOpenInvoiceReason(t *testing.T) {
	// Trial start invoices must NOT trigger subscription activation when paid.
	assert.False(t,
		InvoiceBillingReasonSubscriptionTrialStart.IsFirstSubscriptionOpenInvoiceReason(),
		"SUBSCRIPTION_TRIAL_START must not activate subscription on payment",
	)
}

func TestInvoiceBillingReason_TrialStart_StringValue(t *testing.T) {
	assert.Equal(t, "SUBSCRIPTION_TRIAL_START", string(InvoiceBillingReasonSubscriptionTrialStart))
}

func TestWithCollapsedInvoiceDisplayName(t *testing.T) {
	tests := []struct {
		name string
		md   Metadata
		in   string
		want string
	}{
		{name: "sets on nil map", md: nil, in: "Upgrade: Team → Starter", want: "Upgrade: Team → Starter"},
		{name: "preserves other keys", md: Metadata{"foo": "bar"}, in: "Quantity change", want: "Quantity change"},
		{name: "trims space", md: nil, in: "  Plan change  ", want: "Plan change"},
		{name: "ignores empty", md: Metadata{"foo": "bar"}, in: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WithCollapsedInvoiceDisplayName(tt.md, tt.in)
			assert.Equal(t, tt.want, CollapsedInvoiceDisplayName(got))
			if tt.md != nil {
				assert.Equal(t, "bar", got["foo"])
			}
		})
	}
}

func TestPaymentTermsDueDate(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	utc := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

	tests := []struct {
		name      string
		terms     PaymentTerms
		issueDate time.Time
		want      time.Time
		wantOK    bool
	}{
		{"net 30 from issue date", PaymentTerms30Net, utc(2026, 10, 13), utc(2026, 11, 12), true},
		{"net 30 keeps calendar day across dst", PaymentTerms30Net, time.Date(2026, 10, 15, 0, 30, 0, 0, newYork), time.Date(2026, 11, 14, 0, 30, 0, 0, newYork), true},
		{"end of month issued 1st", PaymentTermsEndOfMonth, time.Date(2026, 9, 1, 0, 0, 0, 0, kolkata), time.Date(2026, 9, 30, 0, 0, 0, 0, kolkata), true},
		{"end of next month issued 28th", PaymentTermsEndOfNextMonth, utc(2026, 9, 28), utc(2026, 10, 31), true},
		{"end of month february", PaymentTermsEndOfMonth, utc(2028, 2, 10), utc(2028, 2, 29), true},
		{"end of next month from january 31", PaymentTermsEndOfNextMonth, utc(2027, 1, 31), utc(2027, 2, 28), true},
		{"end of next month across year", PaymentTermsEndOfNextMonth, utc(2026, 12, 15), utc(2027, 1, 31), true},
		{"end of month uses location month", PaymentTermsEndOfMonth, utc(2026, 10, 1).In(newYork), time.Date(2026, 9, 30, 20, 0, 0, 0, newYork), true},
		{"empty", "", utc(2026, 10, 1), time.Time{}, false},
		{"invalid", "7 NET", utc(2026, 10, 1), time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PaymentTermsDueDate(tt.terms, tt.issueDate)
			assert.Equal(t, tt.wantOK, ok)
			assert.True(t, tt.want.Equal(got), "want %s, got %s", tt.want, got)
		})
	}
}

func TestInvoiceConfig_PaymentTermsValidation(t *testing.T) {
	cfg := InvoiceConfig{
		InvoiceNumberPrefix:        "INV",
		InvoiceNumberFormat:        InvoiceNumberFormatYYYYMM,
		InvoiceNumberStartSequence: 1,
		InvoiceNumberTimezone:      "UTC",
		InvoiceNumberSuffixLength:  5,
	}
	require.NoError(t, cfg.Validate())

	cfg.PaymentTerms = lo.ToPtr(PaymentTermsEndOfMonth)
	require.NoError(t, cfg.Validate())

	cfg.PaymentTerms = lo.ToPtr(PaymentTerms("END_OF_WEEK"))
	require.Error(t, cfg.Validate())
}
