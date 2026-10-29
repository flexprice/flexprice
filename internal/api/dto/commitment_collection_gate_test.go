package dto

import (
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCreateCommitmentRequest_CollectionMethodValidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		method  *types.CommitmentCollectionMethod
		wantErr string
	}{
		{"default per_period", nil, ""},
		{"explicit per_period", lo.ToPtr(types.CommitmentCollectionPerPeriod), ""},
		{"billing_terms requires rules", lo.ToPtr(types.CommitmentCollectionBillingTerms), "billing_terms is required"},
		{"explicit empty rejected", lo.ToPtr(types.CommitmentCollectionMethod("")), "collection_method must not be empty"},
		{"unknown rejected", lo.ToPtr(types.CommitmentCollectionMethod("unsupported")), "invalid collection_method"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			request := CreateCommitmentRequest{
				SubscriptionID: "sub_gated", Name: "Commitment", Scope: CommitmentScopeInput{EntityType: types.CommitmentScopePrice, EntityID: "price_gated"},
				Type: types.COMMITMENT_TYPE_AMOUNT, Value: decimal.NewFromInt(100), Measurement: types.CommitmentMeasurementBillingPeriod,
				Binding: lo.ToPtr(true), CollectionMethod: tt.method, StartDate: start, EndDate: start.AddDate(1, 0, 0),
			}
			request.ApplyDefaults("UTC")
			err := request.Validate()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.True(t, ierr.IsValidation(err))
				return
			}
			require.NoError(t, err)
			require.Equal(t, types.CommitmentCollectionPerPeriod, *request.CollectionMethod)
		})
	}
}

func TestCreateCommitmentRequest_BillingTermsOwnershipAndDelivery(t *testing.T) {
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	valid := CreateCommitmentRequest{SubscriptionID: "sub", Name: "Commitment", Scope: CommitmentScopeInput{EntityType: types.CommitmentScopeSubscription, EntityID: "sub"}, Type: types.COMMITMENT_TYPE_AMOUNT, Value: decimal.NewFromInt(100), Measurement: types.CommitmentMeasurementBillingPeriod, Binding: lo.ToPtr(true), CollectionMethod: lo.ToPtr(types.CommitmentCollectionBillingTerms), StartDate: start, EndDate: start.AddDate(1, 0, 0), BillingTerms: &BillingTermsInput{Rows: []BillingTermInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder}}}}
	valid.ApplyDefaults("UTC")
	require.NoError(t, valid.Validate())
	for _, tc := range []struct {
		name   string
		change func(*CreateCommitmentRequest)
	}{
		{"non binding", func(r *CreateCommitmentRequest) { r.Binding = lo.ToPtr(false); r.CollectionMethod = nil }},
		{"per period", func(r *CreateCommitmentRequest) { r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionPerPeriod) }},
		{"missing rules", func(r *CreateCommitmentRequest) { r.BillingTerms = nil }},
		{"empty rules", func(r *CreateCommitmentRequest) { r.BillingTerms = &BillingTermsInput{} }},
		{"unsupported delivery", func(r *CreateCommitmentRequest) { r.InvoiceDelivery = lo.ToPtr(types.BillingTermDelivery("manual")) }},
		{"protected settlement", func(r *CreateCommitmentRequest) {
			r.Metadata = types.Metadata{"flexprice.billing_terms.settlement": "override"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { r := valid; tc.change(&r); require.Error(t, r.Validate()) })
	}
	for _, delivery := range []types.BillingTermDelivery{types.BillingTermSubscriptionInvoice, types.BillingTermSeparateInvoice} {
		r := valid
		r.InvoiceDelivery = &delivery
		require.NoError(t, r.Validate())
	}
}
