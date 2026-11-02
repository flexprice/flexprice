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

	startsOn := time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		input     BillingTermInput
		wantType  types.BillingTermValueType
		wantAmt   *decimal.Decimal
		wantPct   *decimal.Decimal
		wantStart *time.Time
	}{
		{
			name:     "percentage is stored as a fixed term that keeps the percentage",
			input:    BillingTermInput{Kind: types.BillingTermKindOnce, ValueType: types.BillingTermValuePercentage, Percentage: lo.ToPtr(decimal.NewFromInt(30))},
			wantType: types.BillingTermValueFixed,
			wantPct:  lo.ToPtr(decimal.NewFromInt(30)),
		},
		{
			name:     "fixed keeps its amount",
			input:    BillingTermInput{Kind: types.BillingTermKindOnce, ValueType: types.BillingTermValueFixed, Amount: lo.ToPtr(decimal.NewFromInt(300))},
			wantType: types.BillingTermValueFixed,
			wantAmt:  lo.ToPtr(decimal.NewFromInt(300)),
		},
		{
			name:      "remainder keeps the recurring fields",
			input:     BillingTermInput{Kind: types.BillingTermKindRecurring, ValueType: types.BillingTermValueRemainder, StartsOn: &startsOn},
			wantType:  types.BillingTermValueRemainder,
			wantStart: &startsOn,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term := tc.input.ToTerm(ctx, "cmt_1")

			require.Equal(t, "bterm_", term.ID[:6])
			require.Equal(t, types.BillingTermOwnerCommitment, term.OwnerType)
			require.Equal(t, "cmt_1", term.OwnerID)
			require.Equal(t, tc.input.Kind, term.Kind)
			require.Equal(t, tc.wantType, term.ValueType)
			require.Equal(t, tc.wantAmt, term.Amount)
			require.Equal(t, tc.wantPct, term.Percentage)
			require.Equal(t, tc.wantStart, term.StartsOn)
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

func TestBillingTermInput_Validate(t *testing.T) {
	amount, percentage := lo.ToPtr(decimal.NewFromInt(300)), lo.ToPtr(decimal.NewFromInt(30))
	tests := []struct {
		name    string
		input   BillingTermInput
		wantErr string
	}{
		{"percentage", BillingTermInput{ValueType: types.BillingTermValuePercentage, Percentage: percentage}, ""},
		{"fixed", BillingTermInput{ValueType: types.BillingTermValueFixed, Amount: amount}, ""},
		{"remainder", BillingTermInput{ValueType: types.BillingTermValueRemainder}, ""},
		{"percentage with an amount", BillingTermInput{ValueType: types.BillingTermValuePercentage, Amount: amount, Percentage: percentage}, "billing_terms[2].amount"},
		{"percentage without a percentage", BillingTermInput{ValueType: types.BillingTermValuePercentage}, "billing_terms[2].percentage"},
		{"fixed with a percentage", BillingTermInput{ValueType: types.BillingTermValueFixed, Percentage: percentage}, "billing_terms[2].percentage"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate(2)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestBillingTermInput_ApplyDefaults(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	in := BillingTermInput{
		BillOn:   lo.ToPtr(jan.Add(750 * time.Millisecond)),
		StartsOn: lo.ToPtr(jan.Add(time.Second + 1)),
	}

	in.ApplyDefaults()

	require.True(t, in.BillOn.Equal(jan))
	require.True(t, in.StartsOn.Equal(jan.Add(time.Second)))
	require.Nil(t, in.EndsOn)
	require.Nil(t, in.CoversFrom)
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
			"a term that fails its own rule",
			ReplaceBillingTermsRequest{BillingTerms: []BillingTermInput{
				remainder,
				{ValueType: types.BillingTermValuePercentage},
			}},
			"billing_terms[1].percentage",
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
		{StartsOn: lo.ToPtr(jan.Add(time.Second + 1))},
	}}

	req.ApplyDefaults()

	require.True(t, req.BillingTerms[0].BillOn.Equal(jan))
	require.True(t, req.BillingTerms[1].StartsOn.Equal(jan.Add(time.Second)))
}

func TestReplanInstalmentsRequest_Validate(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	dated := ReplanInstalmentInput{Amount: decimal.NewFromInt(100), BillOn: lo.ToPtr(jan)}
	manual := ReplanInstalmentInput{Amount: decimal.NewFromInt(100), BillTrigger: lo.ToPtr(types.InstalmentBillTriggerManual)}

	tests := []struct {
		name    string
		req     ReplanInstalmentsRequest
		wantErr string
	}{
		{"a date instalment", ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{dated}}, ""},
		{"an explicit date trigger", ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{
			{Amount: dated.Amount, BillOn: dated.BillOn, BillTrigger: lo.ToPtr(types.InstalmentBillTriggerDate)},
		}}, ""},
		{"a manual instalment", ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{manual}}, ""},
		{"no instalments", ReplanInstalmentsRequest{}, "Instalments"},
		{"empty instalments", ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{}}, "Instalments"},
		{
			"too many instalments",
			ReplanInstalmentsRequest{Instalments: lo.Times(501, func(int) ReplanInstalmentInput { return dated })},
			"1 to 500",
		},
		{
			"a zero amount",
			ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{dated, {BillOn: dated.BillOn}}},
			"instalments[1].amount",
		},
		{
			"a negative amount",
			ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{{Amount: decimal.NewFromInt(-1), BillOn: dated.BillOn}}},
			"instalments[0].amount",
		},
		{
			"a date instalment without bill_on",
			ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{{Amount: dated.Amount}}},
			"instalments[0].bill_on",
		},
		{
			"a manual instalment with bill_on",
			ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{
				{Amount: dated.Amount, BillOn: dated.BillOn, BillTrigger: manual.BillTrigger},
			}},
			"instalments[0].bill_on",
		},
		{
			"an unknown trigger",
			ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{
				{Amount: dated.Amount, BillOn: dated.BillOn, BillTrigger: lo.ToPtr(types.InstalmentBillTrigger("later"))},
			}},
			"instalments[0].bill_trigger",
		},
		{
			"a description over 255 characters",
			ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{
				{Amount: dated.Amount, BillOn: dated.BillOn, Description: lo.ToPtr(strings.Repeat("x", 256))},
			}},
			"Description",
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

func TestReplanInstalmentsRequest_ApplyDefaults(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	req := ReplanInstalmentsRequest{Instalments: []ReplanInstalmentInput{
		{BillOn: lo.ToPtr(jan.Add(750 * time.Millisecond))},
		{},
	}}

	req.ApplyDefaults()

	require.True(t, req.Instalments[0].BillOn.Equal(jan))
	require.Nil(t, req.Instalments[1].BillOn)
}

func TestBringForwardInstalmentsRequest(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, BringForwardInstalmentsRequest{Amount: decimal.NewFromInt(300)}.Validate())

	for _, amount := range []decimal.Decimal{decimal.Zero, decimal.NewFromInt(-5)} {
		err := BringForwardInstalmentsRequest{Amount: amount}.Validate()
		require.True(t, ierr.IsValidation(err), "amount %s: got %v", amount, err)
	}

	req := BringForwardInstalmentsRequest{Amount: decimal.NewFromInt(300), BillOn: lo.ToPtr(jan.Add(750 * time.Millisecond))}
	req.ApplyDefaults()
	require.True(t, req.BillOn.Equal(jan))

	none := BringForwardInstalmentsRequest{Amount: decimal.NewFromInt(300)}
	none.ApplyDefaults()
	require.Nil(t, none.BillOn)
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
