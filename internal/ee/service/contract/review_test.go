package contract

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/agents/services/customermatch"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestGetReview_StartsFromTheExtraction(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	id := extractedContract(t, ctx, f)

	got, err := f.svc.GetReview(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 0, got.Version)
	require.Empty(t, got.Edited)
	require.Empty(t, got.Confirmed)
	require.Nil(t, got.Customer)
	require.Equal(t, northwind().Charges[0].Amount.Value, got.Result.Charges[0].Amount.Value)
	require.Equal(t, "2027-10-31", got.Result.Derived.EndDate.Value)

	_, wantToCheck := storedMatch(t, ctx, f, id)
	wantToCheck.Charges += 3
	wantToCheck.Total += 3
	require.Equal(t, wantToCheck, got.Result.ToCheck, "with no products every charge needs a product decision")
	require.Empty(t, got.Products)
}

func TestSaveReviewStep_EditsAndConfirmations(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	id := extractedContract(t, ctx, f)
	before, err := f.svc.GetReview(ctx, id)
	require.NoError(t, err)

	got, err := f.svc.SaveReviewStep(ctx, id, stepCharges, &dto.SaveContractReviewRequest{
		Version:   0,
		Values:    map[string]string{"charges[0].amount": "200000", "charges[2].name": "Implementation Fee"},
		Confirmed: []string{"charges[1].tier_mode"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, got.Version)
	require.Equal(t, []string{"charges[0].amount"}, got.Edited, "a value saved unchanged is not an edit")
	require.Equal(t, []string{"charges[1].tier_mode"}, got.Confirmed)
	require.Equal(t, "200000", got.Result.Charges[0].Amount.Value)
	require.Equal(t, 9, got.Result.Charges[0].Amount.Page, "an edited term keeps where it was found")
	require.Equal(t, "50000.00", got.Result.Derived.InvoiceSchedules[0].AmountPerInvoice)
	require.Equal(t, before.Result.ToCheck.Charges-1, got.Result.ToCheck.Charges)

	reloaded, err := f.svc.GetReview(ctx, id)
	require.NoError(t, err)
	require.Equal(t, got, reloaded)

	stored, err := f.contracts.GetByID(ctx, id)
	require.NoError(t, err)
	var out extraction.Extraction
	require.NoError(t, json.Unmarshal(stored.Extraction(), &out))
	require.Equal(t, "184000", out.Result.Charges[0].Amount.Value, "the extraction is never changed")
}

func TestSaveReviewStep_UnchangedValueIsNotRevalidated(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	f.extractor.out.Result.Credits[0].Expiry.Value = "end of January 2027"
	id := extractedContract(t, ctx, f)

	got, err := f.svc.SaveReviewStep(ctx, id, stepCharges, &dto.SaveContractReviewRequest{Values: map[string]string{"credits[0].expiry": "end of January 2027"}})
	require.NoError(t, err)
	require.Empty(t, got.Edited)

	_, err = f.svc.SaveReviewStep(ctx, id, stepCharges, &dto.SaveContractReviewRequest{Version: 1, Values: map[string]string{"credits[0].expiry": "late January"}})
	require.True(t, ierr.IsValidation(err), "a changed value must be a date, got %v", err)
}

func TestSaveReviewStep_ConfirmedIsReplacedPerStep(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	id := extractedContract(t, ctx, f)

	_, err := f.svc.SaveReviewStep(ctx, id, stepBilling, &dto.SaveContractReviewRequest{Version: 0, Confirmed: []string{"term.notice_days"}})
	require.NoError(t, err)
	_, err = f.svc.SaveReviewStep(ctx, id, stepCharges, &dto.SaveContractReviewRequest{Version: 1, Confirmed: []string{"charges[1].tier_mode"}})
	require.NoError(t, err)
	got, err := f.svc.SaveReviewStep(ctx, id, stepCharges, &dto.SaveContractReviewRequest{Version: 2, Confirmed: []string{}})
	require.NoError(t, err)
	require.Equal(t, []string{"term.notice_days"}, got.Confirmed)

	got, err = f.svc.SaveReviewStep(ctx, id, stepBilling, &dto.SaveContractReviewRequest{Version: 3})
	require.NoError(t, err)
	require.Equal(t, []string{"term.notice_days"}, got.Confirmed, "omitting confirmed keeps it")
}

func TestSaveReviewStep_StaleVersionConflicts(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	id := extractedContract(t, ctx, f)
	req := &dto.SaveContractReviewRequest{Version: 0, Values: map[string]string{"term.term_months": "24"}}

	_, err := f.svc.SaveReviewStep(ctx, id, stepBilling, req)
	require.NoError(t, err)
	_, err = f.svc.SaveReviewStep(ctx, id, stepBilling, req)
	require.True(t, ierr.IsVersionConflict(err), "got %v", err)
}

func TestSaveReviewStep_Rejects(t *testing.T) {
	tests := []struct {
		name string
		step string
		req  *dto.SaveContractReviewRequest
	}{
		{"unknown step", "review", &dto.SaveContractReviewRequest{}},
		{"term of another step", stepCharges, &dto.SaveContractReviewRequest{Values: map[string]string{"term.term_months": "24"}}},
		{"unknown path", stepCharges, &dto.SaveContractReviewRequest{Values: map[string]string{"charges[9].amount": "1"}}},
		{"confirmed term of another step", stepBilling, &dto.SaveContractReviewRequest{Confirmed: []string{"customer.legal_name"}}},
		{"bad date", stepBilling, &dto.SaveContractReviewRequest{Values: map[string]string{"term.effective_date": "Nov 1"}}},
		{"bad currency", stepBilling, &dto.SaveContractReviewRequest{Values: map[string]string{"billing.currency": "usd"}}},
		{"bad tier mode", stepCharges, &dto.SaveContractReviewRequest{Values: map[string]string{"charges[1].tier_mode": "unknown"}}},
		{"customer on the charges step", stepCharges, &dto.SaveContractReviewRequest{Customer: &dto.ContractReviewCustomer{Choice: reviewChoiceNew}}},
		{"existing customer without id", stepCustomer, &dto.SaveContractReviewRequest{Customer: &dto.ContractReviewCustomer{Choice: reviewChoiceExisting}}},
		{"existing customer not found", stepCustomer, &dto.SaveContractReviewRequest{Customer: &dto.ContractReviewCustomer{Choice: reviewChoiceExisting, CustomerID: "cust_missing"}}},
		{"new customer with id", stepCustomer, &dto.SaveContractReviewRequest{Customer: &dto.ContractReviewCustomer{Choice: reviewChoiceNew, CustomerID: "cust_nw"}}},
		{"unknown choice", stepCustomer, &dto.SaveContractReviewRequest{Customer: &dto.ContractReviewCustomer{Choice: "maybe"}}},
		{"products on the billing step", stepBilling, productReq("charges[0]", reviewChoiceNew, "")},
		{"product for a charge past the last", stepCharges, productReq("charges[3]", reviewChoiceNew, "")},
		{"product for a term path", stepCharges, productReq("charges[0].name", reviewChoiceNew, "")},
		{"product for a padded index", stepCharges, productReq("charges[01]", reviewChoiceNew, "")},
		{"existing product without id", stepCharges, productReq("charges[0]", reviewChoiceExisting, "")},
		{"existing product not found", stepCharges, productReq("charges[0]", reviewChoiceExisting, "prod_missing")},
		{"archived product", stepCharges, productReq("charges[2]", reviewChoiceExisting, "prod_impl_old")},
		{"new product with id", stepCharges, productReq("charges[0]", reviewChoiceNew, "prod_platform")},
		{"unknown product choice", stepCharges, productReq("charges[0]", "maybe", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			f := newIngestFixture()
			seedCustomers(t, ctx, f.customers)
			seedProducts(t, ctx, f.products)
			id := extractedContract(t, ctx, f)

			_, err := f.svc.SaveReviewStep(ctx, id, tt.step, tt.req)
			require.True(t, ierr.IsValidation(err), "got %v", err)

			got, err := f.svc.GetReview(ctx, id)
			require.NoError(t, err)
			require.Equal(t, 0, got.Version, "nothing is saved")
		})
	}
}

func TestSaveReviewStep_CustomerDecisionSettlesAPossibleMatch(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	seedCustomers(t, ctx, f.customers)
	f.matcher.result = &customermatch.MatchResult{
		BestCustomerID: "cust_nw",
		Candidates:     []customermatch.Candidate{candidate("cust_nw", 80, customermatch.EvidenceNameVariant), candidate("cust_nw_eu", 75, customermatch.EvidenceNameVariant)},
	}
	id := extractedContract(t, ctx, f)
	require.NoError(t, f.svc.MatchCustomer(ctx, id))

	before, err := f.svc.GetReview(ctx, id)
	require.NoError(t, err)
	require.Equal(t, CustomerMatchPossible, before.Result.CustomerMatch.Status)

	got, err := f.svc.SaveReviewStep(ctx, id, stepCustomer, &dto.SaveContractReviewRequest{
		Customer: &dto.ContractReviewCustomer{Choice: reviewChoiceExisting, CustomerID: "cust_nw"},
	})
	require.NoError(t, err)
	require.Equal(t, &dto.ContractReviewCustomer{Choice: reviewChoiceExisting, CustomerID: "cust_nw"}, got.Customer)
	require.Equal(t, before.Result.ToCheck.Customer-1, got.Result.ToCheck.Customer)
	require.Equal(t, before.Result.ToCheck.Total-1, got.Result.ToCheck.Total)
}

func TestReview_SurvivesALaterMatch(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	f.matcher.result = &customermatch.MatchResult{Candidates: []customermatch.Candidate{}}
	id := extractedContract(t, ctx, f)

	_, err := f.svc.SaveReviewStep(ctx, id, stepBilling, &dto.SaveContractReviewRequest{Values: map[string]string{"term.term_months": "24"}})
	require.NoError(t, err)
	require.NoError(t, f.svc.MatchCustomer(ctx, id))

	got, err := f.svc.GetReview(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 1, got.Version)
	require.Equal(t, "24", got.Result.Term.TermMonths.Value)
	require.Equal(t, CustomerMatchNew, got.Result.CustomerMatch.Status, "the review shows the latest match")
}

func TestDeriveTerms_StartsFromTheSavedReview(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	id := extractedContract(t, ctx, f)
	_, err := f.svc.SaveReviewStep(ctx, id, stepBilling, &dto.SaveContractReviewRequest{Values: map[string]string{"term.term_months": "24"}})
	require.NoError(t, err)

	got, err := f.svc.DeriveTerms(ctx, id, &dto.DeriveContractTermsRequest{Term: &dto.DeriveTermInputs{NoticeDays: lo.ToPtr("30")}})
	require.NoError(t, err)
	require.Equal(t, "2028-10-31", got.EndDate.Value)
	require.Equal(t, "2028-10-01", got.NoticeDeadline.Value)
}

func TestGetReview_BeforeExtraction(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	c, err := f.svc.Upload(ctx, &UploadRequest{FileName: "c.pdf", Data: pdfBytes})
	require.NoError(t, err)

	_, err = f.svc.GetReview(ctx, c.ID())
	require.True(t, ierr.IsInvalidOperation(err), "got %v", err)
}

func productReq(path, choice, productID string) *dto.SaveContractReviewRequest {
	return &dto.SaveContractReviewRequest{Products: map[string]dto.ContractReviewProduct{path: {Choice: choice, ProductID: productID}}}
}
