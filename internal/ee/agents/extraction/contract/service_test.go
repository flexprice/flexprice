package contract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/agents/extraction"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/ee/agents/openrouter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
)

func newTestService(client openrouter.OpenRouterClient, customers CustomerFinder) *ContractExtractionService {
	return NewContractExtractionService(client, customers, logger.NewNoopLogger()).(*ContractExtractionService)
}

func TestExtract_PDF(t *testing.T) {
	or := &fakeOpenRouter{model: "test/model", responses: []*openrouter.Response{{
		Model: "test/model", Content: modelOutput(t, northwind()),
		Usage: &openrouter.Usage{PromptTokens: 9000, CompletionTokens: 2000, CostUSD: 0.05},
	}}}
	s := newTestService(or, nil)

	out, err := s.Extract(context.Background(), &extraction.ExtractRequest{FileName: "msa.pdf", MediaType: MediaTypePDF, Data: []byte("%PDF-1.7")})
	require.NoError(t, err)
	require.Equal(t, "test/model", out.Model)
	require.Equal(t, 0.05, out.Usage.CostUSD)
	require.Len(t, out.Result.Charges, 3)
	require.Equal(t, "Northwind Health, Inc.", out.Result.Customer.LegalName.Value)

	req := or.requests[0]
	require.Equal(t, "test/model", req.Model)
	require.NotNil(t, req.Temperature)
	require.Equal(t, openrouter.ResponseFormatJSONSchema, req.ResponseFormat.Type)
	require.True(t, req.ResponseFormat.JSONSchema.Strict)
	user := req.Messages[1].Content
	require.Equal(t, openrouter.ContentTypeFile, user[1].Type)
	require.Equal(t, "msa.pdf", user[1].File.Filename)
	require.Contains(t, user[1].File.FileData, "data:application/pdf;base64,")
}

func TestExtract_ImageUsesImagePart(t *testing.T) {
	or := &fakeOpenRouter{model: "m", responses: []*openrouter.Response{{Content: modelOutput(t, northwind())}}}
	_, err := newTestService(or, nil).Extract(context.Background(), &extraction.ExtractRequest{MediaType: MediaTypePNG, Data: []byte{0x89, 'P', 'N', 'G'}})
	require.NoError(t, err)
	part := or.requests[0].Messages[1].Content[1]
	require.Equal(t, openrouter.ContentTypeImageURL, part.Type)
	require.Contains(t, part.ImageURL.URL, "data:image/png;base64,")
}

func TestExtract_RequestModelOverridesDefault(t *testing.T) {
	or := &fakeOpenRouter{model: "default/model", responses: []*openrouter.Response{{Content: modelOutput(t, northwind())}}}
	_, err := newTestService(or, nil).Extract(context.Background(), &extraction.ExtractRequest{MediaType: MediaTypePDF, Data: []byte("x"), Model: "other/model"})
	require.NoError(t, err)
	require.Equal(t, "other/model", or.requests[0].Model)
}

func TestExtract_RetriesOnceOnInvalidJSONAndSumsUsage(t *testing.T) {
	or := &fakeOpenRouter{model: "m", responses: []*openrouter.Response{
		{Content: "I could not read it", Usage: &openrouter.Usage{CostUSD: 0.01}},
		{Content: "```json\n" + modelOutput(t, northwind()) + "\n```", Usage: &openrouter.Usage{CostUSD: 0.02}},
	}}
	out, err := newTestService(or, nil).Extract(context.Background(), &extraction.ExtractRequest{MediaType: MediaTypePDF, Data: []byte("x")})
	require.NoError(t, err)
	require.Len(t, or.requests, 2)
	require.InDelta(t, 0.03, out.Usage.CostUSD, 1e-9)
}

func TestExtract_Errors(t *testing.T) {
	tests := []struct {
		name    string
		or      *fakeOpenRouter
		req     *extraction.ExtractRequest
		matches func(error) bool
		calls   int
	}{
		{name: "no file", or: &fakeOpenRouter{model: "m"}, req: &extraction.ExtractRequest{MediaType: MediaTypePDF}, matches: ierr.IsValidation},
		{name: "unsupported type", or: &fakeOpenRouter{model: "m"}, req: &extraction.ExtractRequest{MediaType: "application/msword", Data: []byte("x")}, matches: ierr.IsValidation},
		{name: "no model", or: &fakeOpenRouter{}, req: &extraction.ExtractRequest{MediaType: MediaTypePDF, Data: []byte("x")}, matches: ierr.IsServiceUnavailable},
		{name: "upstream error", or: &fakeOpenRouter{model: "m", errs: []error{ierr.NewError("rate").Mark(ierr.ErrTooManyRequests)}},
			req: &extraction.ExtractRequest{MediaType: MediaTypePDF, Data: []byte("x")}, matches: ierr.IsTooManyRequests, calls: 1},
		{name: "invalid twice", or: &fakeOpenRouter{model: "m", responses: []*openrouter.Response{{Content: "nope"}, {Content: "still nope"}}},
			req: &extraction.ExtractRequest{MediaType: MediaTypePDF, Data: []byte("x")}, matches: ierr.IsHTTPClient, calls: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTestService(tt.or, nil).Extract(context.Background(), tt.req)
			require.Error(t, err)
			require.True(t, tt.matches(err), "unexpected error kind: %v", err)
			require.Len(t, tt.or.requests, tt.calls)
		})
	}
}

func TestEnrich_Northwind(t *testing.T) {
	customers := &fakeCustomers{rows: []*customer.Customer{{ID: "cust_1", Name: "Northwind Health", Email: "billing@northwindhealth.org"}}}
	r := northwind()
	require.NoError(t, newTestService(&fakeOpenRouter{}, customers).Enrich(context.Background(), r))

	require.Equal(t, &extraction.CustomerMatch{Status: CustomerMatchExisting, CustomerID: "cust_1", Name: "Northwind Health",
		MatchedOn: []string{MatchedOnLegalName, MatchedOnEmailDomain}}, r.CustomerMatch)

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
	require.NoError(t, newTestService(&fakeOpenRouter{}, nil).Enrich(context.Background(), r))
	b, err := json.Marshal(r)
	require.NoError(t, err)
	var back extraction.Result
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, r.ToCheck, back.ToCheck)
	require.Equal(t, CustomerMatchNew, back.CustomerMatch.Status)
}
