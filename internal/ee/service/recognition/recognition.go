package recognition

import (
	"context"
	"time"

	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/domain/customer"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

// Recognizer computes the recognition entries the journal is missing for one kind of unit; it never posts them.
type Recognizer interface {
	// Due is, sorted, the units a run as of asOf recognizes: within the scope, or else those that may still owe a month
	// and those whose billing changed since `since`.
	Due(ctx context.Context, since, asOf time.Time, scope Scope) ([]string, error)
	// Recognize is the entries the unit is missing for the months ended before asOf's local day, in posting order; with
	// openMonth it also counts the open month up to asOf.
	Recognize(ctx context.Context, id string, asOf time.Time, openMonth bool) ([]*domainJournal.Entry, error)
	// LockKey serializes recognizing and posting the unit.
	LockKey(id string) string
}

// Scope limits a run to one contract or one customer; the zero value is the whole environment.
type Scope struct {
	ContractID string
	CustomerID string
}

// ContractRecognizer recognizes a contract's POBs.
type ContractRecognizer interface{ Recognizer }

// InvoiceRecognizer recognizes the invoices of subscriptions with no contract.
type InvoiceRecognizer interface{ Recognizer }

// monthEnds is the local month ends from first's month to today's; with openMonth the open month ends at today, unless
// today is its first day.
// e.g. Jan 15 to Apr 2 → Feb 1, Mar 1, Apr 1: the ends of January, February and March; with openMonth also Apr 2.
func monthEnds(first, today time.Time, openMonth bool) []time.Time {
	ends := lo.Drop(utils.Months(first, today), 1)
	if openMonth && today.Day() != 1 && today.After(first) {
		ends = append(ends, today)
	}
	return ends
}

// localMidnight is the local midnight of a month end, as the earning functions read it.
func localMidnight(monthEnd time.Time, loc *time.Location) time.Time {
	return time.Date(monthEnd.Year(), monthEnd.Month(), monthEnd.Day(), 0, 0, 0, 0, loc)
}

// timezones resolves the timezone a unit's months are counted in.
type timezones struct {
	subRepo      subscription.Repository
	customerRepo customer.Repository
}

// of is the subscription's timezone, else the customer's.
func (z timezones) of(ctx context.Context, customerID, subscriptionID string) (*time.Location, error) {
	if subscriptionID != "" {
		sub, err := z.subRepo.Get(ctx, subscriptionID)
		if err != nil {
			return nil, err
		}
		return types.LoadTimezone(sub.Timezone), nil
	}
	c, err := z.customerRepo.Get(ctx, customerID)
	if err != nil {
		return nil, err
	}
	return types.LoadTimezone(c.Timezone), nil
}
