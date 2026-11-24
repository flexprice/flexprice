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
		{"custom net 7", "7 NET", utc(2026, 10, 1), utc(2026, 10, 8), true},
		{"net 1", "1 NET", utc(2026, 10, 1), utc(2026, 10, 2), true},
		{"net 365", "365 NET", utc(2026, 10, 1), utc(2027, 10, 1), true},
		{"due on receipt", PaymentTermsDueOnReceipt, utc(2026, 10, 1), utc(2026, 10, 1), true},
		{"empty", "", utc(2026, 10, 1), time.Time{}, false},
		{"net 0", "0 NET", utc(2026, 10, 1), time.Time{}, false},
		{"net 366", "366 NET", utc(2026, 10, 1), time.Time{}, false},
		{"leading zero", "07 NET", utc(2026, 10, 1), time.Time{}, false},
		{"plus sign", "+7 NET", utc(2026, 10, 1), time.Time{}, false},
		{"no days", " NET", utc(2026, 10, 1), time.Time{}, false},
		{"lowercase", "7 net", utc(2026, 10, 1), time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			termsType, days := tt.terms.Parts()
			got, ok := lo.FromPtr(termsType).DueDate(days, tt.issueDate)
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

	cfg.PaymentTerms = lo.ToPtr(PaymentTerms("120 NET"))
	require.NoError(t, cfg.Validate())

	cfg.PaymentTerms = lo.ToPtr(PaymentTermsDueOnReceipt)
	require.NoError(t, cfg.Validate())

	cfg.PaymentTerms = lo.ToPtr(PaymentTerms("END_OF_WEEK"))
	require.Error(t, cfg.Validate())

	cfg.PaymentTerms = lo.ToPtr(PaymentTerms("366 NET"))
	require.Error(t, cfg.Validate())
}

func TestResolvePaymentTerms(t *testing.T) {
	terms := func(p PaymentTerms) *PaymentTerms { return &p }
	tests := []struct {
		name      string
		legacy    *PaymentTerms
		termsType *PaymentTermsType
		days      *int
		want      *PaymentTerms
		wantErr   bool
	}{
		{name: "nothing sent"},
		{name: "payment_terms only", legacy: terms("30 NET"), want: terms("30 NET")},
		{name: "payment_terms clear", legacy: terms(""), want: terms("")},
		{name: "invalid payment_terms", legacy: terms("0 NET"), wantErr: true},
		{name: "days", termsType: lo.ToPtr(PaymentTermsTypeDays), days: lo.ToPtr(120), want: terms("120 NET")},
		{name: "end of next month", termsType: lo.ToPtr(PaymentTermsTypeEndOfNextMonth), want: terms(PaymentTermsEndOfNextMonth)},
		{name: "due on receipt", termsType: lo.ToPtr(PaymentTermsTypeDueOnReceipt), want: terms(PaymentTermsDueOnReceipt)},
		{name: "type clear", termsType: lo.ToPtr(PaymentTermsType("")), want: terms("")},
		{name: "type overrides payment_terms", legacy: terms("30 NET"), termsType: lo.ToPtr(PaymentTermsTypeDays), days: lo.ToPtr(45), want: terms("45 NET")},
		{name: "type overrides invalid payment_terms", legacy: terms("bad"), termsType: lo.ToPtr(PaymentTermsTypeEndOfMonth), want: terms(PaymentTermsEndOfMonth)},
		{name: "days without count", termsType: lo.ToPtr(PaymentTermsTypeDays), wantErr: true},
		{name: "days over 365", termsType: lo.ToPtr(PaymentTermsTypeDays), days: lo.ToPtr(366), wantErr: true},
		{name: "count with end of month", termsType: lo.ToPtr(PaymentTermsTypeEndOfMonth), days: lo.ToPtr(5), wantErr: true},
		{name: "count without type", days: lo.ToPtr(45), wantErr: true},
		{name: "unknown type", termsType: lo.ToPtr(PaymentTermsType("END_OF_WEEK")), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePaymentTerms(tt.legacy, tt.termsType, tt.days)
			assert.Equal(t, tt.wantErr, err != nil)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPaymentTermsParts(t *testing.T) {
	termsType, days := PaymentTerms("45 NET").Parts()
	assert.Equal(t, lo.ToPtr(PaymentTermsTypeDays), termsType)
	assert.Equal(t, lo.ToPtr(45), days)

	termsType, days = PaymentTermsEndOfMonth.Parts()
	assert.Equal(t, lo.ToPtr(PaymentTermsTypeEndOfMonth), termsType)
	assert.Nil(t, days)

	termsType, days = PaymentTerms("").Parts()
	assert.Nil(t, termsType)
	assert.Nil(t, days)
}
