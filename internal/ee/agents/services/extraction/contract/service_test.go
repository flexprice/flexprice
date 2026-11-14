package contract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/agents/providers/reducto"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

func newTestService(client reducto.ReductoClient) *ContractExtractionService {
	return NewContractExtractionService(client, logger.NewNoopLogger()).(*ContractExtractionService)
}

func pdfRequest() *extraction.ExtractRequest {
	return &extraction.ExtractRequest{FileName: "msa.pdf", MediaType: MediaTypePDF, Data: []byte("%PDF-1.7")}
}

func TestExtract_UploadsThenExtractsWithCitations(t *testing.T) {
	fr := &fakeReducto{
		uploadResp:  &reducto.UploadResponse{FileID: "reducto://abc.pdf"},
		extractResp: extractResponse(t, northwindCited()),
	}

	out, err := newTestService(fr).Extract(context.Background(), pdfRequest())
	require.NoError(t, err)

	require.Equal(t, []uploadCall{{"msa.pdf", MediaTypePDF, []byte("%PDF-1.7")}}, fr.uploads)
	req := fr.extracts[0]
	require.Equal(t, "reducto://abc.pdf", req.Input)
	require.True(t, req.Settings.Citations.Enabled)
	require.Equal(t, systemPrompt, req.Instructions.SystemPrompt)
	require.True(t, json.Valid(req.Instructions.Schema))

	require.Equal(t, "reducto/extract", out.Model)
	require.Equal(t, "job-1", out.JobID)
	require.Equal(t, 14, out.Usage.Pages)
	require.Equal(t, 61, out.Usage.Fields)
	require.Equal(t, 4.0, *out.Usage.Credits)
	require.Equal(t, 7.9, out.Usage.DurationSeconds)
}

func TestExtract_MapsCitedFieldsToTerms(t *testing.T) {
	fr := &fakeReducto{uploadResp: &reducto.UploadResponse{FileID: "reducto://abc.pdf"}, extractResp: extractResponse(t, northwindCited())}
	out, err := newTestService(fr).Extract(context.Background(), pdfRequest())
	require.NoError(t, err)
	r := out.Result

	name := r.Customer.LegalName
	require.Equal(t, "Northwind Health, Inc.", name.Value)
	require.Equal(t, 99, name.Confidence)
	require.Equal(t, 1, name.Page)
	require.Equal(t, "Northwind Health, Inc.", name.Quote)
	require.Equal(t, []extraction.Box{{Page: 1, Left: 0.1, Top: 0.2, Width: 0.3, Height: 0.01}}, name.Boxes)
	require.Equal(t, []string{}, name.Options)

	require.Equal(t, extraction.Term{Options: []string{}}, r.Customer.BillingEmail, "a missing field carries no evidence")
	require.Equal(t, "12", r.Term.RenewalTermMonths.Value, "numbers become strings")
	require.Equal(t, "true", r.Term.AutoRenew.Value)

	require.Equal(t, []extraction.Document{{Type: DocumentTypeMSA, Title: "Master Services Agreement", PageStart: 1, PageEnd: 14}}, r.Documents)
	require.Len(t, r.Charges, 2)
	require.Equal(t, ChargeKindRecurringFixed, r.Charges[0].Kind)
	require.Equal(t, "QUARTERLY", r.Charges[0].BillingPeriod.Value)
	require.Equal(t, 88, r.Charges[0].BillingPeriod.Confidence)
	require.Equal(t, ChargeKindUsage, r.Charges[1].Kind)
	require.Equal(t, "5000000", r.Charges[1].IncludedQuantity.Value)
	require.Equal(t, []extraction.Tier{{UpTo: "10000000", UnitAmount: "0.002"}, {UnitAmount: "0.0015"}}, r.Charges[1].Tiers)
	require.Equal(t, "200000", r.Commitments[0].Amount.Value)
	require.Equal(t, "2027-01-31", r.Credits[0].Expiry.Value)
	require.Equal(t, []string{}, r.Doubts)
}

func TestExtract_ConfidenceFallsBackToLabelAndUsesWeakestCitation(t *testing.T) {
	s := extractor()
	low := 0.29
	require.Equal(t, 29, s.confidence([]reducto.Citation{
		{Confidence: reducto.ConfidenceHigh, GranularConfidence: &reducto.GranularConfidence{ExtractConfidence: floatPtr(0.97)}},
		{Confidence: reducto.ConfidenceLow, GranularConfidence: &reducto.GranularConfidence{ExtractConfidence: &low}},
	}))
	require.Equal(t, lowLabelConfidence, s.confidence([]reducto.Citation{{Confidence: reducto.ConfidenceLow}}))
	require.Equal(t, highLabelConfidence, s.confidence([]reducto.Citation{{Confidence: reducto.ConfidenceHigh}}))
}

func TestExtract_ImageUploadsWithItsMediaType(t *testing.T) {
	fr := &fakeReducto{uploadResp: &reducto.UploadResponse{FileID: "reducto://img.png"}, extractResp: extractResponse(t, northwindCited())}
	_, err := newTestService(fr).Extract(context.Background(), &extraction.ExtractRequest{MediaType: MediaTypePNG, Data: []byte{0x89, 'P', 'N', 'G'}})
	require.NoError(t, err)
	require.Equal(t, MediaTypePNG, fr.uploads[0].mediaType)
	require.Equal(t, defaultFileName, fr.uploads[0].fileName)
}

func TestExtract_Errors(t *testing.T) {
	tests := []struct {
		name    string
		fr      *fakeReducto
		req     *extraction.ExtractRequest
		matches func(error) bool
	}{
		{name: "no file", fr: &fakeReducto{}, req: &extraction.ExtractRequest{MediaType: MediaTypePDF}, matches: ierr.IsValidation},
		{name: "unsupported type", fr: &fakeReducto{}, req: &extraction.ExtractRequest{MediaType: "application/msword", Data: []byte("x")}, matches: ierr.IsValidation},
		{name: "upload fails", fr: &fakeReducto{uploadErr: ierr.NewError("down").Mark(ierr.ErrServiceUnavailable)}, req: pdfRequest(), matches: ierr.IsServiceUnavailable},
		{name: "extract fails", fr: &fakeReducto{uploadResp: &reducto.UploadResponse{FileID: "reducto://a"}, extractErr: ierr.NewError("rate").Mark(ierr.ErrTooManyRequests)}, req: pdfRequest(), matches: ierr.IsTooManyRequests},
		{name: "result in wrong shape", fr: &fakeReducto{uploadResp: &reducto.UploadResponse{FileID: "reducto://a"}, extractResp: &reducto.ExtractResponse{Result: json.RawMessage(`{"charges":"not a list"}`)}}, req: pdfRequest(), matches: ierr.IsHTTPClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTestService(tt.fr).Extract(context.Background(), tt.req)
			require.Error(t, err)
			require.True(t, tt.matches(err), "unexpected error kind: %v", err)
		})
	}
}

func floatPtr(f float64) *float64 { return &f }
