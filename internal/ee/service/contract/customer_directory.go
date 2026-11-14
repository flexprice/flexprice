package contract

import (
	"context"
	"strings"
	"unicode"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/ee/agents/services/customermatch"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const (
	directoryPageSize = 50
	directoryMaxPages = 5
)

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
	return d.scan(ctx, d.contains("name", query), limit, func(c *customer.Customer) bool {
		return strings.Contains(strings.ToLower(c.Name), strings.ToLower(query))
	})
}

func (d *customerDirectory) SearchByEmail(ctx context.Context, emailOrDomain string, limit int) ([]customermatch.CustomerRecord, error) {
	value := strings.ToLower(strings.TrimSpace(emailOrDomain))
	if strings.Contains(value, "@") {
		return d.scan(ctx, d.contains("email", value), limit, func(c *customer.Customer) bool { return strings.EqualFold(c.Email, value) })
	}

	domain := strings.TrimPrefix(value, "@")
	if domain == "" || !strings.Contains(domain, ".") || d.isFreeMailDomain(domain) {
		return nil, ierr.NewError("unsupported email domain").
			WithHint("Search a company domain, not a free mail provider").
			Mark(ierr.ErrValidation)
	}
	return d.scan(ctx, d.contains("email", "@"+domain), limit, func(c *customer.Customer) bool { return d.companyDomain(c.Email) == domain })
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

// scan pages through the substring matches and keeps those that pass the exact check, so rows
// that fail it cannot use up the limit. It reads at most directoryMaxPages pages.
func (d *customerDirectory) scan(ctx context.Context, cond *types.FilterCondition, limit int, keep func(*customer.Customer) bool) ([]customermatch.CustomerRecord, error) {
	out := []customermatch.CustomerRecord{}
	for page := 0; page < directoryMaxPages; page++ {
		rows, err := d.list(ctx, &types.CustomerFilter{
			QueryFilter: d.pageQuery(page),
			Filters:     []*types.FilterCondition{cond},
		})
		if err != nil {
			return nil, err
		}
		for _, c := range rows {
			if c == nil || !d.published(c) || !keep(c) {
				continue
			}
			out = append(out, d.record(c))
			if len(out) == limit {
				return out, nil
			}
		}
		if len(rows) < directoryPageSize {
			break
		}
	}
	return out, nil
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

func (d *customerDirectory) pageQuery(page int) *types.QueryFilter {
	q := types.NewDefaultQueryFilter()
	q.Limit = lo.ToPtr(directoryPageSize)
	q.Offset = lo.ToPtr(page * directoryPageSize)
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

// normalizeName lowercases a name, drops periods, turns other punctuation into spaces and strips
// trailing legal suffixes. Letters and digits of every script are kept.
func (d *customerDirectory) normalizeName(name string) string {
	n := strings.Map(func(r rune) rune {
		switch {
		case r == '.':
			return -1
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return r
		default:
			return ' '
		}
	}, strings.ToLower(name))
	words := strings.Fields(n)
	for len(words) > 1 && d.isLegalSuffix(words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

func (d *customerDirectory) companyDomain(email string) string {
	_, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if !ok || domain == "" || d.isFreeMailDomain(domain) {
		return ""
	}
	return domain
}

func (d *customerDirectory) isFreeMailDomain(domain string) bool {
	switch domain {
	case "gmail.com", "googlemail.com", "outlook.com", "hotmail.com", "yahoo.com", "icloud.com",
		"proton.me", "protonmail.com", "aol.com":
		return true
	}
	return false
}

func (d *customerDirectory) isLegalSuffix(word string) bool {
	switch word {
	case "incorporated", "inc", "llc", "ltd", "limited", "corp", "corporation", "co", "company",
		"plc", "gmbh", "pvt", "private", "llp", "lp", "sa", "ag", "bv", "pty":
		return true
	}
	return false
}
