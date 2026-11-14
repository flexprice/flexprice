package contract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/ee/agents/services/customermatch"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type fakeMatcher struct {
	result     *customermatch.MatchResult
	err        error
	configured bool
	requests   []*customermatch.MatchRequest
}

func (f *fakeMatcher) Match(_ context.Context, req *customermatch.MatchRequest, _ customermatch.CustomerDirectory) (*customermatch.MatchResult, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeMatcher) IsConfigured() bool { return f.configured }

// storeCustomerService serves the two customer reads the directory uses from an in-memory store.
type storeCustomerService struct {
	interfaces.CustomerService
	store *testutil.InMemoryCustomerStore
}

func (s *storeCustomerService) GetCustomer(ctx context.Context, id string) (*dto.CustomerResponse, error) {
	c, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &dto.CustomerResponse{Customer: c}, nil
}

func (s *storeCustomerService) GetCustomers(ctx context.Context, filter *types.CustomerFilter) (*dto.ListCustomersResponse, error) {
	rows, err := s.store.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	resp := &dto.ListCustomersResponse{Items: []*dto.CustomerResponse{}}
	for _, c := range rows {
		resp.Items = append(resp.Items, &dto.CustomerResponse{Customer: c})
	}
	return resp, nil
}

func seedCustomers(t *testing.T, ctx context.Context, store *testutil.InMemoryCustomerStore) {
	t.Helper()
	rows := []*customer.Customer{
		{ID: "cust_nw", Name: "Northwind Health", Email: "ap@northwindhealth.org"},
		{ID: "cust_nw_eu", Name: "Northwind Health Europe", Email: "eu@northwind.eu"},
		{ID: "cust_old", Name: "Northwind Old", Email: "old@northwindhealth.org", BaseModel: types.BaseModel{Status: types.StatusArchived}},
		{ID: "cust_gmail", Name: "Solo Founder", Email: "solo@gmail.com"},
	}
	for _, c := range rows {
		c.TenantID = types.GetTenantID(ctx)
		c.EnvironmentID = types.GetEnvironmentID(ctx)
		if c.Status == "" {
			c.Status = types.StatusPublished
		}
		require.NoError(t, store.Create(ctx, c))
	}
}

func extractedContract(t *testing.T, ctx context.Context, f *ingestFixture) string {
	t.Helper()
	c, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.NoError(t, err)
	require.NoError(t, f.svc.Process(ctx, c.ID()))
	return c.ID()
}

func storedMatch(t *testing.T, ctx context.Context, f *ingestFixture, id string) (*extraction.CustomerMatch, *extraction.ToCheck) {
	t.Helper()
	stored, err := f.contracts.GetByID(ctx, id)
	require.NoError(t, err)
	var out extraction.Extraction
	require.NoError(t, json.Unmarshal(stored.Extraction(), &out))
	return out.Result.CustomerMatch, out.Result.ToCheck
}

func candidate(id string, confidence int, evidence ...string) customermatch.Candidate {
	return customermatch.Candidate{CustomerID: id, Confidence: confidence, Evidence: evidence, Reason: "reason " + id}
}

func TestMatchCustomer_Statuses(t *testing.T) {
	tests := []struct {
		name       string
		result     *customermatch.MatchResult
		wantStatus string
		wantID     string
		wantConf   int
		wantCands  []string
		wantCheck  int
	}{
		{
			name: "confident match becomes existing",
			result: &customermatch.MatchResult{BestCustomerID: "cust_nw", Candidates: []customermatch.Candidate{
				candidate("cust_nw", 93, customermatch.EvidenceExactName, customermatch.EvidenceEmailDomain),
				candidate("cust_nw_eu", 50, customermatch.EvidenceNameVariant),
			}},
			wantStatus: CustomerMatchExisting, wantID: "cust_nw", wantConf: 93, wantCands: []string{"cust_nw", "cust_nw_eu"},
		},
		{
			name: "close runner-up needs a person",
			result: &customermatch.MatchResult{BestCustomerID: "cust_nw", Candidates: []customermatch.Candidate{
				candidate("cust_nw", 90, customermatch.EvidenceNameVariant),
				candidate("cust_nw_eu", 85, customermatch.EvidenceNameVariant),
			}},
			wantStatus: CustomerMatchPossible, wantConf: 90, wantCands: []string{"cust_nw", "cust_nw_eu"}, wantCheck: 1,
		},
		{
			name: "evidence that does not hold lowers the score",
			result: &customermatch.MatchResult{BestCustomerID: "cust_nw_eu", Candidates: []customermatch.Candidate{
				candidate("cust_nw_eu", 95, customermatch.EvidenceExactName, customermatch.EvidenceEmailExact),
			}},
			wantStatus: CustomerMatchPossible, wantConf: 65, wantCands: []string{"cust_nw_eu"}, wantCheck: 1,
		},
		{
			name: "no best answer stays possible",
			result: &customermatch.MatchResult{Candidates: []customermatch.Candidate{
				candidate("cust_nw", 92, customermatch.EvidenceExactName),
			}},
			wantStatus: CustomerMatchPossible, wantConf: 92, wantCands: []string{"cust_nw"}, wantCheck: 1,
		},
		{
			name: "archived, unknown and weak candidates are dropped",
			result: &customermatch.MatchResult{BestCustomerID: "cust_old", Candidates: []customermatch.Candidate{
				candidate("cust_old", 99), candidate("cust_missing", 99), candidate("cust_nw_eu", 30),
			}},
			wantStatus: CustomerMatchNew, wantCands: []string{},
		},
		{
			name:       "nothing found",
			result:     &customermatch.MatchResult{Candidates: []customermatch.Candidate{}},
			wantStatus: CustomerMatchNew, wantCands: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			f := newIngestFixture()
			seedCustomers(t, ctx, f.customers)
			f.matcher.result = tt.result
			id := extractedContract(t, ctx, f)

			require.NoError(t, f.svc.MatchCustomer(ctx, id))

			match, toCheck := storedMatch(t, ctx, f, id)
			require.Equal(t, tt.wantStatus, match.Status)
			require.Equal(t, tt.wantID, match.CustomerID)
			require.Equal(t, tt.wantConf, match.Confidence)
			ids := []string{}
			for _, c := range match.Candidates {
				ids = append(ids, c.CustomerID)
			}
			require.Equal(t, tt.wantCands, ids)
			require.Equal(t, tt.wantCheck, toCheck.Customer)
			require.Equal(t, 12+tt.wantCheck, toCheck.Total)
		})
	}
}

func TestMatchCustomer_ExistingCarriesCustomerDetails(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	seedCustomers(t, ctx, f.customers)
	f.matcher.result = &customermatch.MatchResult{Model: "m", Steps: 3, BestCustomerID: "cust_nw", Candidates: []customermatch.Candidate{
		candidate("cust_nw", 96, customermatch.EvidenceExactName, customermatch.EvidenceEmailDomain, customermatch.EvidenceAddress),
	}}
	id := extractedContract(t, ctx, f)

	require.NoError(t, f.svc.MatchCustomer(ctx, id))

	match, _ := storedMatch(t, ctx, f, id)
	require.Equal(t, &extraction.CustomerMatch{
		Status: CustomerMatchExisting, CustomerID: "cust_nw", Name: "Northwind Health", Confidence: 96,
		MatchedOn: []string{customermatch.EvidenceExactName, customermatch.EvidenceEmailDomain, customermatch.EvidenceAddress},
		Reason:    "reason cust_nw", Model: "m", Steps: 3,
		Candidates: []extraction.MatchCandidate{{
			CustomerID: "cust_nw", Name: "Northwind Health", Email: "ap@northwindhealth.org", Confidence: 96,
			MatchedOn: []string{customermatch.EvidenceExactName, customermatch.EvidenceEmailDomain, customermatch.EvidenceAddress},
			Reason:    "reason cust_nw",
		}},
	}, match)
	require.Equal(t, &customermatch.MatchRequest{
		LegalName: "Northwind Health, Inc.", BillingEmail: "ap@northwindhealth.org",
		BillingAddress: "1200 Harbor Boulevard, Seattle, WA 98101", PrimaryContact: "Dana Whitfield, Chief Financial Officer",
	}, f.matcher.requests[0])
}

func TestMatchCustomer_RetrySafe(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	seedCustomers(t, ctx, f.customers)
	f.matcher.result = &customermatch.MatchResult{Candidates: []customermatch.Candidate{}}
	id := extractedContract(t, ctx, f)

	require.NoError(t, f.svc.MatchCustomer(ctx, id))
	require.NoError(t, f.svc.MatchCustomer(ctx, id))
	require.NoError(t, f.svc.MarkCustomerMatchUnavailable(ctx, id, "late failure"))

	require.Len(t, f.matcher.requests, 1)
	match, _ := storedMatch(t, ctx, f, id)
	require.Equal(t, CustomerMatchNew, match.Status)
}

func TestMatchCustomer_ProviderErrorIsReturnedForRetry(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	f.matcher.err = ierr.NewError("rate limited").Mark(ierr.ErrTooManyRequests)
	id := extractedContract(t, ctx, f)

	require.Error(t, f.svc.MatchCustomer(ctx, id))
	match, _ := storedMatch(t, ctx, f, id)
	require.Equal(t, CustomerMatchPending, match.Status)

	require.NoError(t, f.svc.MarkCustomerMatchUnavailable(ctx, id, "The AI provider is rate limiting requests"))
	match, _ = storedMatch(t, ctx, f, id)
	require.Equal(t, &extraction.CustomerMatch{Status: CustomerMatchUnavailable, Reason: "The AI provider is rate limiting requests"}, match)
}

func TestMatchCustomer_NoCustomerDetails(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	f.extractor.out.Result.Customer = extraction.Customer{}
	id := extractedContract(t, ctx, f)

	require.NoError(t, f.svc.MatchCustomer(ctx, id))
	require.Empty(t, f.matcher.requests)
	match, _ := storedMatch(t, ctx, f, id)
	require.Equal(t, CustomerMatchNew, match.Status)
}

func TestMatchCustomer_DisabledMarksUnavailableAtExtraction(t *testing.T) {
	for name, configure := range map[string]func(*ingestFixture){
		"turned off":     func(f *ingestFixture) { f.svc.config.CustomerMatch.Enabled = false },
		"not configured": func(f *ingestFixture) { f.matcher.configured = false },
	} {
		t.Run(name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			f := newIngestFixture()
			configure(f)
			id := extractedContract(t, ctx, f)

			match, _ := storedMatch(t, ctx, f, id)
			require.Equal(t, CustomerMatchUnavailable, match.Status)
			require.NoError(t, f.svc.MatchCustomer(ctx, id))
			require.Empty(t, f.matcher.requests)
		})
	}
}

func TestMatchCustomer_SkipsUntilExtractionCompletes(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	c, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.NoError(t, err)

	require.NoError(t, f.svc.MatchCustomer(ctx, c.ID()))
	require.Empty(t, f.matcher.requests)
}

func TestCustomerDirectory(t *testing.T) {
	ctx := testutil.SetupContext()
	store := testutil.NewInMemoryCustomerStore()
	seedCustomers(t, ctx, store)
	d := newCustomerDirectory(&storeCustomerService{store: store})

	byName, err := d.SearchByName(ctx, "northwind", 10)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"cust_nw", "cust_nw_eu"}, recordIDs(byName))

	limited, err := d.SearchByName(ctx, "northwind", 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)

	byEmail, err := d.SearchByEmail(ctx, "AP@northwindhealth.org", 10)
	require.NoError(t, err)
	require.Equal(t, []string{"cust_nw"}, recordIDs(byEmail))

	byDomain, err := d.SearchByEmail(ctx, "northwindhealth.org", 10)
	require.NoError(t, err)
	require.Equal(t, []string{"cust_nw"}, recordIDs(byDomain), "archived customers are hidden")

	_, err = d.SearchByEmail(ctx, "gmail.com", 10)
	require.True(t, ierr.IsValidation(err))

	got, err := d.Get(ctx, "cust_nw")
	require.NoError(t, err)
	require.Equal(t, "Northwind Health", got.Name)

	_, err = d.Get(ctx, "cust_old")
	require.Error(t, err)

	other := testutil.SetupContext()
	other = context.WithValue(other, types.CtxEnvironmentID, "env_other")
	none, err := d.SearchByName(other, "northwind", 10)
	require.NoError(t, err)
	require.Empty(t, none, "other environments see nothing")
}

func recordIDs(records []customermatch.CustomerRecord) []string {
	ids := make([]string, 0, len(records))
	for _, r := range records {
		ids = append(ids, r.ID)
	}
	return ids
}
