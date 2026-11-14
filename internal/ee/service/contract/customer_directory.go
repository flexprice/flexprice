package contract

import (
	"context"
	"regexp"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/ee/agents/services/customermatch"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9 ]+`)

// customerDirectory gives the matching agent read-only access to the tenant's published customers.
// The customer service scopes every query to the tenant and environment in ctx.
type customerDirectory struct {
	customers interfaces.CustomerService
}

func newCustomerDirectory(customers interfaces.CustomerService) *customerDirectory {
	return &customerDirectory{customers: customers}
}

func (d *customerDirectory) SearchByName(ctx context.Context, query string, limit int) ([]customermatch.CustomerRecord, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []customermatch.CustomerRecord{}, nil
	}
	rows, err := d.list(ctx, &types.CustomerFilter{
		QueryFilter: d.limitedQuery(limit),
		Filters:     []*types.FilterCondition{d.contains("name", query)},
	})
	if err != nil {
		return nil, err
	}
	return d.records(rows, limit, func(c *customer.Customer) bool {
		return strings.Contains(strings.ToLower(c.Name), strings.ToLower(query))
	}), nil
}

func (d *customerDirectory) SearchByEmail(ctx context.Context, emailOrDomain string, limit int) ([]customermatch.CustomerRecord, error) {
	value := strings.ToLower(strings.TrimSpace(emailOrDomain))
	if strings.Contains(value, "@") {
		rows, err := d.list(ctx, &types.CustomerFilter{
			QueryFilter: d.limitedQuery(limit),
			Filters:     []*types.FilterCondition{d.contains("email", value)},
		})
		if err != nil {
			return nil, err
		}
		return d.records(rows, limit, func(c *customer.Customer) bool { return strings.EqualFold(c.Email, value) }), nil
	}

	domain := strings.TrimPrefix(value, "@")
	if domain == "" || !strings.Contains(domain, ".") || genericEmailDomains[domain] {
		return nil, ierr.NewError("unsupported email domain").
			WithHint("Search a company domain, not a free mail provider").
			Mark(ierr.ErrValidation)
	}
	rows, err := d.list(ctx, &types.CustomerFilter{
		QueryFilter: d.limitedQuery(limit),
		Filters:     []*types.FilterCondition{d.contains("email", "@"+domain)},
	})
	if err != nil {
		return nil, err
	}
	return d.records(rows, limit, func(c *customer.Customer) bool { return d.companyDomain(c.Email) == domain }), nil
}

func (d *customerDirectory) Get(ctx context.Context, id string) (*customermatch.CustomerRecord, error) {
	resp, err := d.customers.GetCustomer(ctx, id)
	if err != nil {
		return nil, err
	}
	c := resp.Customer
	if c == nil || !d.published(c) {
		return nil, ierr.NewError("customer not found").Mark(ierr.ErrNotFound)
	}
	record := d.record(c)
	return &record, nil
}

func (d *customerDirectory) list(ctx context.Context, filter *types.CustomerFilter) ([]*customer.Customer, error) {
	resp, err := d.customers.GetCustomers(ctx, filter)
	if err != nil {
		return nil, err
	}
	rows := make([]*customer.Customer, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item != nil {
			rows = append(rows, item.Customer)
		}
	}
	return rows, nil
}

func (d *customerDirectory) records(rows []*customer.Customer, limit int, keep func(*customer.Customer) bool) []customermatch.CustomerRecord {
	out := []customermatch.CustomerRecord{}
	for _, c := range rows {
		if c == nil || !d.published(c) || !keep(c) {
			continue
		}
		out = append(out, d.record(c))
		if len(out) == limit {
			break
		}
	}
	return out
}

func (d *customerDirectory) record(c *customer.Customer) customermatch.CustomerRecord {
	return customermatch.CustomerRecord{
		ID:         c.ID,
		Name:       c.Name,
		Email:      c.Email,
		ExternalID: c.ExternalID,
		Contact:    d.contactName(c),
		City:       c.AddressCity,
		Country:    c.AddressCountry,
		Currency:   lo.FromPtr(c.BillingCurrency),
		CreatedAt:  c.CreatedAt,
	}
}

func (d *customerDirectory) contactName(c *customer.Customer) string {
	for _, p := range c.ContactPersons {
		if p != nil && p.IsPrimary {
			return strings.TrimSpace(p.FirstName + " " + p.LastName)
		}
	}
	if len(c.ContactPersons) > 0 && c.ContactPersons[0] != nil {
		return strings.TrimSpace(c.ContactPersons[0].FirstName + " " + c.ContactPersons[0].LastName)
	}
	return ""
}

func (d *customerDirectory) published(c *customer.Customer) bool {
	return c.Status == "" || c.Status == types.StatusPublished
}

func (d *customerDirectory) limitedQuery(limit int) *types.QueryFilter {
	q := types.NewDefaultQueryFilter()
	q.Limit = lo.ToPtr(limit)
	q.Status = lo.ToPtr(types.StatusPublished)
	return q
}

func (d *customerDirectory) contains(field, value string) *types.FilterCondition {
	return &types.FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(types.CONTAINS),
		DataType: lo.ToPtr(types.DataTypeString),
		Value:    &types.Value{String: lo.ToPtr(value)},
	}
}

func (d *customerDirectory) normalizeName(name string) string {
	n := nonAlnum.ReplaceAllString(strings.ToLower(strings.ReplaceAll(name, ".", "")), " ")
	words := strings.Fields(n)
	for len(words) > 1 && lo.Contains(legalSuffixes, words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

func (d *customerDirectory) companyDomain(email string) string {
	_, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if !ok || domain == "" || genericEmailDomains[domain] {
		return ""
	}
	return domain
}
