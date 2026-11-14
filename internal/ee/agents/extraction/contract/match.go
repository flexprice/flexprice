package contract

import (
	"context"
	"github.com/flexprice/flexprice/internal/ee/agents/extraction"
	"regexp"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const maxNameCandidates = 50

var nonAlnum = regexp.MustCompile(`[^a-z0-9 ]+`)

func (s *ContractExtractionService) matchCustomer(ctx context.Context, c extraction.Customer) (*extraction.CustomerMatch, error) {
	noMatch := &extraction.CustomerMatch{Status: CustomerMatchNew}
	if s.customers == nil {
		return noMatch, nil
	}

	email := strings.ToLower(strings.TrimSpace(c.BillingEmail.Value))
	if email != "" && strings.Contains(email, "@") {
		found, err := s.findCustomers(ctx, &types.CustomerFilter{QueryFilter: limitedQuery(1), Email: email})
		if err != nil {
			return nil, err
		}
		if len(found) > 0 {
			return existingMatch(found[0], MatchedOnEmail), nil
		}
	}

	name := normalizeName(c.LegalName.Value)
	if name == "" {
		return noMatch, nil
	}
	found, err := s.findCustomers(ctx, &types.CustomerFilter{
		QueryFilter: limitedQuery(maxNameCandidates),
		Filters:     []*types.FilterCondition{nameContains(coreToken(name))},
	})
	if err != nil {
		return nil, err
	}
	domain := companyDomain(email)
	for _, cand := range found {
		if normalizeName(cand.Name) != name {
			continue
		}
		matchedOn := []string{MatchedOnLegalName}
		if domain != "" && companyDomain(cand.Email) == domain {
			matchedOn = append(matchedOn, MatchedOnEmailDomain)
		}
		return existingMatch(cand, matchedOn...), nil
	}
	return noMatch, nil
}

func (s *ContractExtractionService) findCustomers(ctx context.Context, filter *types.CustomerFilter) ([]*customer.Customer, error) {
	resp, err := s.customers.GetCustomers(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]*customer.Customer, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item != nil && item.Customer != nil {
			out = append(out, item.Customer)
		}
	}
	return out, nil
}

func existingMatch(c *customer.Customer, matchedOn ...string) *extraction.CustomerMatch {
	return &extraction.CustomerMatch{Status: CustomerMatchExisting, CustomerID: c.ID, Name: c.Name, MatchedOn: matchedOn}
}

func limitedQuery(limit int) *types.QueryFilter {
	q := types.NewDefaultQueryFilter()
	q.Limit = lo.ToPtr(limit)
	q.Status = lo.ToPtr(types.StatusPublished)
	return q
}

func nameContains(token string) *types.FilterCondition {
	return &types.FilterCondition{
		Field:    lo.ToPtr("name"),
		Operator: lo.ToPtr(types.CONTAINS),
		DataType: lo.ToPtr(types.DataTypeString),
		Value:    &types.Value{String: lo.ToPtr(token)},
	}
}

func normalizeName(name string) string {
	n := nonAlnum.ReplaceAllString(strings.ToLower(strings.ReplaceAll(name, ".", "")), " ")
	words := strings.Fields(n)
	for len(words) > 1 && lo.Contains(legalSuffixes, words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

func coreToken(normalized string) string {
	return lo.MaxBy(strings.Fields(normalized), func(a, b string) bool { return len(a) > len(b) })
}

func companyDomain(email string) string {
	_, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if !ok || domain == "" || genericEmailDomains[domain] {
		return ""
	}
	return domain
}
