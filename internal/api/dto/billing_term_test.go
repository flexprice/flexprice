package dto

import (
	"context"
	"strings"
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestBillingTermInput_ToTerm(t *testing.T) {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")

	startDate := time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		input     BillingTermInput
		wantValue *decimal.Decimal
		wantStart *time.Time
	}{
		{
			name:      "fixed keeps its value",
			input:     BillingTermInput{Kind: types.BillingTermKindOnce, ValueType: types.BillingTermValueFixed, Value: lo.ToPtr(decimal.NewFromInt(300))},
			wantValue: lo.ToPtr(decimal.NewFromInt(300)),
		},
		{
			name:      "remainder keeps the recurring fields",
			input:     BillingTermInput{Kind: types.BillingTermKindRecurring, ValueType: types.BillingTermValueRemainder, StartDate: &startDate},
			wantStart: &startDate,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term := tc.input.ToTerm(ctx, "cmt_1")

			require.Equal(t, "bterm_", term.ID[:6])
			require.Equal(t, types.BillingTermOwnerCommitment, term.OwnerType)
			require.Equal(t, "cmt_1", term.OwnerID)
			require.Equal(t, tc.input.Kind, term.Kind)
			require.Equal(t, tc.input.ValueType, term.ValueType)
			require.Equal(t, tc.wantValue, term.Value)
			require.Equal(t, tc.wantStart, term.StartDate)
			require.Equal(t, "tenant_1", term.TenantID)
			require.Equal(t, "env_1", term.EnvironmentID)
			require.Equal(t, types.StatusPublished, term.Status)
		})
	}

	t.Run("every call builds a new term", func(t *testing.T) {
		in := BillingTermInput{Kind: types.BillingTermKindOnce, ValueType: types.BillingTermValueFixed}
		require.NotEqual(t, in.ToTerm(ctx, "cmt_1").ID, in.ToTerm(ctx, "cmt_1").ID)
	})
}

func TestBillingTermInput_ApplyDefaults(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	in := BillingTermInput{
		BillOn:    lo.ToPtr(jan.Add(750 * time.Millisecond)),
		StartDate: lo.ToPtr(jan.Add(time.Second + 1)),
	}

	in.ApplyDefaults()

	require.True(t, in.BillOn.Equal(jan))
	require.True(t, in.StartDate.Equal(jan.Add(time.Second)))
	require.Nil(t, in.EndDate)
}

func TestCancellationFeeInput_Validate(t *testing.T) {
	value := decimal.NewFromInt(500)
	tests := []struct {
		name    string
		input   CancellationFeeInput
		wantErr string
	}{
		{"fixed", CancellationFeeInput{Type: types.CancellationFeeFixed, Value: value}, ""},
		{"fixed with a decimal value", CancellationFeeInput{Type: types.CancellationFeeFixed, Value: decimal.RequireFromString("250.5")}, ""},
		{"fixed without a value", CancellationFeeInput{Type: types.CancellationFeeFixed}, "cancellation_fee.value"},
		{"fixed with a negative value", CancellationFeeInput{Type: types.CancellationFeeFixed, Value: decimal.NewFromInt(-5)}, "cancellation_fee.value"},
		{"percentage is not supported yet", CancellationFeeInput{Type: types.CancellationFeePercentage, Value: decimal.NewFromInt(10)}, "percentage cancellation fees are not supported yet"},
		{"missing type", CancellationFeeInput{Value: value}, "cancellation_fee.type"},
		{"unknown type", CancellationFeeInput{Type: "flat", Value: value}, "cancellation_fee.type"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, tc.wantErr)
			require.True(t, ierr.IsValidation(err), "got %v", err)
		})
	}
}

func TestReplaceBillingTermsRequest_Validate(t *testing.T) {
	remainder := BillingTermInput{ValueType: types.BillingTermValueRemainder}
	tests := []struct {
		name    string
		req     ReplaceBillingTermsRequest
		wantErr string
	}{
		{"one term", ReplaceBillingTermsRequest{BillingTerms: []BillingTermInput{remainder}}, ""},
		{"no terms", ReplaceBillingTermsRequest{}, "BillingTerms"},
		{"empty terms", ReplaceBillingTermsRequest{BillingTerms: []BillingTermInput{}}, "BillingTerms"},
		{
			"an invoice_label over 255 characters",
			ReplaceBillingTermsRequest{BillingTerms: []BillingTermInput{
				{ValueType: types.BillingTermValueRemainder, InvoiceLabel: lo.ToPtr(strings.Repeat("a", 256))},
			}},
			"BillingTerms[0].InvoiceLabel",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.True(t, ierr.IsValidation(err), "got %v", err)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestReplaceBillingTermsRequest_ApplyDefaults(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	req := ReplaceBillingTermsRequest{BillingTerms: []BillingTermInput{
		{BillOn: lo.ToPtr(jan.Add(750 * time.Millisecond))},
		{StartDate: lo.ToPtr(jan.Add(time.Second + 1))},
	}}

	req.ApplyDefaults()

	require.True(t, req.BillingTerms[0].BillOn.Equal(jan))
	require.True(t, req.BillingTerms[1].StartDate.Equal(jan.Add(time.Second)))
}

func TestTriggerInstalmentRequest_ApplyDefaults(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	req := TriggerInstalmentRequest{BillOn: lo.ToPtr(jan.Add(750 * time.Millisecond))}
	req.ApplyDefaults()
	require.True(t, req.BillOn.Equal(jan))

	none := TriggerInstalmentRequest{}
	none.ApplyDefaults()
	require.Nil(t, none.BillOn)
}
