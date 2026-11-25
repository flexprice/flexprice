package contract

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	productRepo "github.com/flexprice/flexprice/internal/repository/product"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const (
	ProductMatchMatched     = "matched"
	ProductMatchPossible    = "possible"
	ProductMatchNew         = "new"
	ProductMatchUnavailable = "unavailable"

	productReasonExactName   = "exact_name"
	productReasonLookupKey   = "lookup_key"
	productReasonSimilarName = "similar_name"

	catalogPageSize      = 100
	catalogMaxPages      = 10
	maxProductCandidates = 5
	minNameSimilarity    = 0.5
)

// productCatalog suggests existing published products for contract charges by name.
// The product service scopes every query to the tenant and environment in ctx.
type productCatalog struct {
	products interfaces.ProductService
}

type rankedProduct struct {
	candidate dto.ContractProductCandidate
	rank      int
	score     float64
}

func newProductCatalog(products interfaces.ProductService) *productCatalog {
	return &productCatalog{products: products}
}

// Get returns a published product, or a not found error.
func (p *productCatalog) Get(ctx context.Context, id string) (*dto.ProductResponse, error) {
	resp, err := p.products.GetProduct(ctx, id)
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Status != types.StatusPublished {
		return nil, ierr.NewError("product not found").Mark(ierr.ErrNotFound)
	}
	return resp, nil
}

// Match suggests products for every charge, keyed by charge path.
func (p *productCatalog) Match(ctx context.Context, charges []extraction.Charge) (map[string]*dto.ContractProductMatch, error) {
	matches := make(map[string]*dto.ContractProductMatch, len(charges))
	if len(charges) == 0 {
		return matches, nil
	}
	products, err := p.published(ctx)
	if err != nil {
		return nil, err
	}
	for i := range charges {
		matches[p.chargePath(i)] = p.match(charges[i].Name.Value, products)
	}
	return matches, nil
}

// Unavailable marks every charge's match unavailable, for when the products could not be read.
func (p *productCatalog) Unavailable(charges []extraction.Charge) map[string]*dto.ContractProductMatch {
	matches := make(map[string]*dto.ContractProductMatch, len(charges))
	for i := range charges {
		matches[p.chargePath(i)] = &dto.ContractProductMatch{Status: ProductMatchUnavailable, Candidates: []dto.ContractProductCandidate{}}
	}
	return matches
}

func (p *productCatalog) chargePath(i int) string {
	return "charges[" + strconv.Itoa(i) + "]"
}

// match is matched when exactly one product has the charge's name or lookup key, possible when
// only similar names or several exact ones are found, and new when nothing is close.
func (p *productCatalog) match(chargeName string, products []*dto.ProductResponse) *dto.ContractProductMatch {
	name := p.normalize(chargeName)
	out := &dto.ContractProductMatch{Status: ProductMatchNew, Candidates: []dto.ContractProductCandidate{}}
	if name == "" {
		return out
	}

	ranked := []rankedProduct{}
	for _, prod := range products {
		if r, ok := p.rank(name, prod); ok {
			ranked = append(ranked, r)
		}
	}
	if len(ranked) == 0 {
		return out
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].rank != ranked[j].rank {
			return ranked[i].rank > ranked[j].rank
		}
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].candidate.Name < ranked[j].candidate.Name
	})

	exact := lo.CountBy(ranked, func(r rankedProduct) bool { return r.candidate.Reason != productReasonSimilarName })
	out.Status = ProductMatchPossible
	if exact == 1 {
		out.Status = ProductMatchMatched
		out.ProductID = ranked[0].candidate.ProductID
	}
	for _, r := range lo.Slice(ranked, 0, maxProductCandidates) {
		out.Candidates = append(out.Candidates, r.candidate)
	}
	return out
}

func (p *productCatalog) rank(name string, prod *dto.ProductResponse) (rankedProduct, bool) {
	r := rankedProduct{candidate: dto.ContractProductCandidate{
		ProductID:   prod.ID,
		Name:        prod.Name,
		LookupKey:   prod.LookupKey,
		Description: prod.Description,
	}}
	switch {
	case p.normalize(prod.Name) == name:
		r.candidate.Reason, r.rank, r.score = productReasonExactName, 3, 1
	case p.normalize(prod.LookupKey) == name:
		r.candidate.Reason, r.rank, r.score = productReasonLookupKey, 2, 1
	default:
		r.score = p.similarity(name, p.normalize(prod.Name))
		if r.score < minNameSimilarity {
			return r, false
		}
		r.candidate.Reason, r.rank = productReasonSimilarName, 1
	}
	return r, true
}

// similarity is the share of distinct words two normalized names have in common.
func (p *productCatalog) similarity(a, b string) float64 {
	wordsA, wordsB := lo.Uniq(strings.Fields(a)), lo.Uniq(strings.Fields(b))
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}
	shared := len(lo.Intersect(wordsA, wordsB))
	return float64(shared) / float64(len(wordsA)+len(wordsB)-shared)
}

// normalize lowercases a name and turns punctuation, underscores and dashes into single spaces.
func (p *productCatalog) normalize(name string) string {
	n := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, strings.ToLower(name))
	return strings.Join(strings.Fields(n), " ")
}

// published reads at most catalogMaxPages pages of published products.
func (p *productCatalog) published(ctx context.Context) ([]*dto.ProductResponse, error) {
	out := []*dto.ProductResponse{}
	for page := 0; page < catalogMaxPages; page++ {
		filter := productRepo.NewFilter()
		filter.QueryFilter.Limit = lo.ToPtr(catalogPageSize)
		filter.QueryFilter.Offset = lo.ToPtr(page * catalogPageSize)
		filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
		resp, err := p.products.GetProducts(ctx, filter)
		if err != nil {
			return nil, err
		}
		out = append(out, lo.Compact(resp.Items)...)
		if len(resp.Items) < catalogPageSize {
			break
		}
	}
	return out, nil
}
