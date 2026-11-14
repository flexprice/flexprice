package contract

import (
	"context"
	"regexp"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/ee/agents/extraction"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const maxNameCandidates = 50

var nonAlnum = regexp.MustCompile(`[^a-z0-9 ]+`)

func (c *contractIngestService) matchCustomer(ctx context.Context, customers CustomerFinder, cust extraction.Customer) (*extraction.CustomerMatch, error) {
	noMatch := &extraction.CustomerMatch{Status: CustomerMatchNew}
	if customers == nil {
		return noMatch, nil
	}

	email := strings.ToLower(strings.TrimSpace(cust.BillingEmail.Value))
	if email != "" && strings.Contains(email, "@") {
		found, err := c.findCustomers(ctx, customers, &types.CustomerFilter{QueryFilter: c.limitedQuery(1), Email: email})
		if err != nil {
			return nil, err
		}
		if len(found) > 0 {
			return c.existingMatch(found[0], MatchedOnEmail), nil
		}
	}

	name := c.normalizeName(cust.LegalName.Value)
	if name == "" {
		return noMatch, nil
	}
	found, err := c.findCustomers(ctx, customers, &types.CustomerFilter{
		QueryFilter: c.limitedQuery(maxNameCandidates),
		Filters:     []*types.FilterCondition{c.nameContains(c.coreToken(name))},
	})
	if err != nil {
		return nil, err
	}
	domain := c.companyDomain(email)
	for _, cand := range found {
		if c.normalizeName(cand.Name) != name {
			continue
		}
		matchedOn := []string{MatchedOnLegalName}
		if domain != "" && c.companyDomain(cand.Email) == domain {
			matchedOn = append(matchedOn, MatchedOnEmailDomain)
		}
		return c.existingMatch(cand, matchedOn...), nil
	}
	return noMatch, nil
}

func (c *contractIngestService) findCustomers(ctx context.Context, customers CustomerFinder, filter *types.CustomerFilter) ([]*customer.Customer, error) {
	resp, err := customers.GetCustomers(ctx, filter)
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

func (c *contractIngestService) existingMatch(cand *customer.Customer, matchedOn ...string) *extraction.CustomerMatch {
	return &extraction.CustomerMatch{Status: CustomerMatchExisting, CustomerID: cand.ID, Name: cand.Name, MatchedOn: matchedOn}
}

func (c *contractIngestService) limitedQuery(limit int) *types.QueryFilter {
	q := types.NewDefaultQueryFilter()
	q.Limit = lo.ToPtr(limit)
	q.Status = lo.ToPtr(types.StatusPublished)
	return q
}

func (c *contractIngestService) nameContains(token string) *types.FilterCondition {
	return &types.FilterCondition{
		Field:    lo.ToPtr("name"),
		Operator: lo.ToPtr(types.CONTAINS),
		DataType: lo.ToPtr(types.DataTypeString),
		Value:    &types.Value{String: lo.ToPtr(token)},
	}
}

func (c *contractIngestService) normalizeName(name string) string {
	n := nonAlnum.ReplaceAllString(strings.ToLower(strings.ReplaceAll(name, ".", "")), " ")
	words := strings.Fields(n)
	for len(words) > 1 && lo.Contains(legalSuffixes, words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

func (c *contractIngestService) coreToken(normalized string) string {
	return lo.MaxBy(strings.Fields(normalized), func(a, b string) bool { return len(a) > len(b) })
}

func (c *contractIngestService) companyDomain(email string) string {
	_, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if !ok || domain == "" || genericEmailDomains[domain] {
		return ""
	}
	return domain
}
