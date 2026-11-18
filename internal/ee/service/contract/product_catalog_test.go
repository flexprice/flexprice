package contract

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/product"
	"github.com/flexprice/flexprice/internal/interfaces"
	productRepo "github.com/flexprice/flexprice/internal/repository/product"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// storeProductService serves the two product reads the catalog uses from an in-memory store.
type storeProductService struct {
	interfaces.ProductService
	store   *testutil.InMemoryProductStore
	listErr error
}

func (s *storeProductService) GetProduct(ctx context.Context, id string) (*dto.ProductResponse, error) {
	p, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return dto.NewProductResponse(p), nil
}

func (s *storeProductService) GetProducts(ctx context.Context, filter *productRepo.Filter) (*dto.ListProductsResponse, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	rows, err := s.store.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return &dto.ListProductsResponse{Items: lo.Map(rows, func(p *product.Product, _ int) *dto.ProductResponse { return dto.NewProductResponse(p) })}, nil
}

// seedProducts gives "Platform Fee" an exact match, "API calls" a similar one and "Implementation Fee" none.
func seedProducts(t *testing.T, ctx context.Context, s *storeProductService) {
	t.Helper()
	rows := []struct {
		id, name, lookupKey string
		status              types.Status
	}{
		{"prod_platform", "Platform Fee", "platform_fee", types.StatusPublished},
		{"prod_platform_license", "Platform License Fee", "platform_license", types.StatusPublished},
		{"prod_api_overage", "API Calls Overage", "api_overage", types.StatusPublished},
		{"prod_impl_old", "Implementation Fee", "implementation", types.StatusArchived},
	}
	for _, r := range rows {
		require.NoError(t, s.store.Create(ctx, product.NewBuilder(nil).
			WithID(r.id).WithName(r.name).WithLookupKey(r.lookupKey).WithStatus(r.status).Build()))
	}
}

func TestProductCatalog_Match(t *testing.T) {
	products := []*dto.ProductResponse{
		{ID: "prod_platform", Name: "Platform Fee", LookupKey: "platform_fee"},
		{ID: "prod_platform_license", Name: "Platform License Fee", LookupKey: "platform_license"},
		{ID: "prod_seats", Name: "Seats", LookupKey: "user_seats"},
		{ID: "prod_support_a", Name: "Premium Support", LookupKey: "support_a"},
		{ID: "prod_support_b", Name: "premium-support", LookupKey: "support_b"},
	}
	tests := []struct {
		name        string
		charge      string
		wantStatus  string
		wantProduct string
		wantIDs     []string
		wantReasons []string
	}{
		{"exact name, similar ranked after", "platform fee.", ProductMatchMatched, "prod_platform",
			[]string{"prod_platform", "prod_platform_license"}, []string{productReasonExactName, productReasonSimilarName}},
		{"lookup key", "User Seats", ProductMatchMatched, "prod_seats",
			[]string{"prod_seats"}, []string{productReasonLookupKey}},
		{"similar only", "Platform Licence Fee", ProductMatchPossible, "",
			[]string{"prod_platform", "prod_platform_license"}, []string{productReasonSimilarName, productReasonSimilarName}},
		{"two exact names", "Premium Support", ProductMatchPossible, "",
			[]string{"prod_support_a", "prod_support_b"}, []string{productReasonExactName, productReasonExactName}},
		{"nothing close", "Implementation Fee", ProductMatchNew, "", []string{}, []string{}},
		{"empty name", "  ", ProductMatchNew, "", []string{}, []string{}},
	}
	catalog := newProductCatalog(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := catalog.match(tt.charge, products)
			require.Equal(t, tt.wantStatus, got.Status)
			require.Equal(t, tt.wantProduct, got.ProductID)
			require.Equal(t, tt.wantIDs, lo.Map(got.Candidates, func(c dto.ContractProductCandidate, _ int) string { return c.ProductID }))
			require.Equal(t, tt.wantReasons, lo.Map(got.Candidates, func(c dto.ContractProductCandidate, _ int) string { return c.Reason }))
		})
	}
}

func TestGetReview_ProductMatches(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	seedProducts(t, ctx, f.products)
	id := extractedContract(t, ctx, f)

	got, err := f.svc.GetReview(ctx, id)
	require.NoError(t, err)
	require.Len(t, got.ProductMatches, 3)
	require.Equal(t, ProductMatchMatched, got.ProductMatches["charges[0]"].Status)
	require.Equal(t, "prod_platform", got.ProductMatches["charges[0]"].ProductID)
	require.Equal(t, ProductMatchPossible, got.ProductMatches["charges[1]"].Status)
	require.Equal(t, "prod_api_overage", got.ProductMatches["charges[1]"].Candidates[0].ProductID)
	require.Equal(t, ProductMatchNew, got.ProductMatches["charges[2]"].Status, "an archived product is not suggested")
	require.Empty(t, got.Products)

	_, stored := storedMatch(t, ctx, f, id)
	require.Equal(t, stored.Charges+2, got.Result.ToCheck.Charges, "the possible and the new charge are left to check")
	require.Equal(t, stored.Total+2, got.Result.ToCheck.Total)
}

func TestSaveReviewStep_ProductDecisions(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	seedProducts(t, ctx, f.products)
	id := extractedContract(t, ctx, f)
	before, err := f.svc.GetReview(ctx, id)
	require.NoError(t, err)

	decisions := map[string]dto.ContractReviewProduct{
		"charges[1]": {Choice: reviewChoiceExisting, ProductID: "prod_api_overage"},
		"charges[2]": {Choice: reviewChoiceNew},
	}
	got, err := f.svc.SaveReviewStep(ctx, id, stepCharges, &dto.SaveContractReviewRequest{Products: decisions})
	require.NoError(t, err)
	require.Equal(t, decisions, got.Products)
	require.Equal(t, before.Result.ToCheck.Charges-2, got.Result.ToCheck.Charges)

	reloaded, err := f.svc.GetReview(ctx, id)
	require.NoError(t, err)
	require.Equal(t, got, reloaded)

	got, err = f.svc.SaveReviewStep(ctx, id, stepCharges, &dto.SaveContractReviewRequest{Version: 1, Values: map[string]string{"charges[0].unit": "license"}})
	require.NoError(t, err)
	require.Equal(t, decisions, got.Products, "omitting products keeps them")

	got, err = f.svc.SaveReviewStep(ctx, id, stepCharges, &dto.SaveContractReviewRequest{Version: 2, Products: map[string]dto.ContractReviewProduct{}})
	require.NoError(t, err)
	require.Empty(t, got.Products, "products replaces the whole map")
	require.Equal(t, before.Result.ToCheck.Charges, got.Result.ToCheck.Charges)
}

func TestReview_ProductMatchFollowsTheEditedName(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	seedProducts(t, ctx, f.products)
	id := extractedContract(t, ctx, f)

	got, err := f.svc.SaveReviewStep(ctx, id, stepCharges, &dto.SaveContractReviewRequest{Values: map[string]string{"charges[2].name": "Platform License Fee"}})
	require.NoError(t, err)
	require.Equal(t, ProductMatchMatched, got.ProductMatches["charges[2]"].Status)
	require.Equal(t, "prod_platform_license", got.ProductMatches["charges[2]"].ProductID)
}

func TestReview_ProductMatchUnavailable(t *testing.T) {
	ctx := testutil.SetupContext()
	f := newIngestFixture()
	f.products.listErr = errors.New("products down")
	id := extractedContract(t, ctx, f)

	got, err := f.svc.GetReview(ctx, id)
	require.NoError(t, err, "a failed product lookup does not fail the review")
	for _, m := range got.ProductMatches {
		require.Equal(t, ProductMatchUnavailable, m.Status)
		require.Empty(t, m.Candidates)
	}
	_, stored := storedMatch(t, ctx, f, id)
	require.Equal(t, stored.Charges+3, got.Result.ToCheck.Charges)
}
