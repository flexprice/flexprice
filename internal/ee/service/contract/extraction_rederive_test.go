package contract

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestDeriveTerms(t *testing.T) {
	tests := []struct {
		name         string
		req          *dto.DeriveContractTermsRequest
		wantEnd      string
		wantNotice   string
		wantDates    int
		wantPerInv   string
		wantNoDerive bool
	}{
		{
			name:       "no edits gives the extracted values",
			req:        &dto.DeriveContractTermsRequest{},
			wantEnd:    "2027-10-31",
			wantNotice: "2027-09-01",
			wantDates:  4,
			wantPerInv: "46000.00",
		},
		{
			name: "edited term and billing period",
			req: &dto.DeriveContractTermsRequest{
				Term:    &dto.DeriveTermInputs{EffectiveDate: lo.ToPtr("2027-01-01"), TermMonths: lo.ToPtr("24"), NoticeDays: lo.ToPtr("30")},
				Charges: []dto.DeriveChargeInputs{{BillingPeriod: lo.ToPtr("MONTHLY")}},
			},
			wantEnd:    "2028-12-31",
			wantNotice: "2028-12-01",
			wantDates:  24,
			wantPerInv: "15333.33",
		},
		{
			name:         "a cleared effective date derives nothing",
			req:          &dto.DeriveContractTermsRequest{Term: &dto.DeriveTermInputs{EffectiveDate: lo.ToPtr("")}},
			wantNoDerive: true,
		},
		{
			name:         "a term over the supported length derives nothing",
			req:          &dto.DeriveContractTermsRequest{Term: &dto.DeriveTermInputs{TermMonths: lo.ToPtr("121")}},
			wantNoDerive: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			f := newIngestFixture()
			id := extractedContract(t, ctx, f)

			got, err := f.svc.DeriveTerms(ctx, id, tt.req)
			require.NoError(t, err)
			if tt.wantNoDerive {
				require.False(t, got.EndDate.IsSet())
				require.False(t, got.NoticeDeadline.IsSet())
				require.Empty(t, got.InvoiceSchedules)
				return
			}
			require.Equal(t, tt.wantEnd, got.EndDate.Value)
			require.True(t, got.EndDate.Derived)
			require.Equal(t, tt.wantNotice, got.NoticeDeadline.Value)
			require.Len(t, got.InvoiceSchedules, 1)
			require.Len(t, got.InvoiceSchedules[0].Dates, tt.wantDates)
			require.Equal(t, tt.wantPerInv, got.InvoiceSchedules[0].AmountPerInvoice)
		})
	}
}

func TestDeriveTerms_DoesNotSave(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	id := extractedContract(t, ctx, f)
	before, err := f.contracts.GetByID(ctx, id)
	require.NoError(t, err)

	_, err = f.svc.DeriveTerms(ctx, id, &dto.DeriveContractTermsRequest{Term: &dto.DeriveTermInputs{TermMonths: lo.ToPtr("36")}})
	require.NoError(t, err)

	after, err := f.contracts.GetByID(ctx, id)
	require.NoError(t, err)
	require.JSONEq(t, string(before.Extraction()), string(after.Extraction()))
	var out extraction.Extraction
	require.NoError(t, json.Unmarshal(after.Extraction(), &out))
	require.Equal(t, "12", out.Result.Term.TermMonths.Value)
}

func TestDeriveTerms_Rejects(t *testing.T) {
	tests := []struct {
		name string
		req  *dto.DeriveContractTermsRequest
	}{
		{"more charges than extracted", &dto.DeriveContractTermsRequest{Charges: make([]dto.DeriveChargeInputs, 4)}},
		{"bad date", &dto.DeriveContractTermsRequest{Term: &dto.DeriveTermInputs{EffectiveDate: lo.ToPtr("Nov 1, 2026")}}},
		{"negative term", &dto.DeriveContractTermsRequest{Term: &dto.DeriveTermInputs{TermMonths: lo.ToPtr("-1")}}},
		{"unknown period", &dto.DeriveContractTermsRequest{Charges: []dto.DeriveChargeInputs{{BillingPeriod: lo.ToPtr("FORTNIGHTLY")}}}},
		{"unknown timing", &dto.DeriveContractTermsRequest{Charges: []dto.DeriveChargeInputs{{BillingTiming: lo.ToPtr("later")}}}},
		{"bad amount", &dto.DeriveContractTermsRequest{Charges: []dto.DeriveChargeInputs{{Amount: lo.ToPtr("US$184,000")}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			f := newIngestFixture()
			id := extractedContract(t, ctx, f)

			_, err := f.svc.DeriveTerms(ctx, id, tt.req)
			require.True(t, ierr.IsValidation(err), "got %v", err)
		})
	}
}

func TestDeriveTerms_BeforeExtraction(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	c, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.NoError(t, err)

	_, err = f.svc.DeriveTerms(ctx, c.ID(), &dto.DeriveContractTermsRequest{})
	require.True(t, ierr.IsInvalidOperation(err), "got %v", err)
}
