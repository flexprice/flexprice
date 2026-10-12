package activity

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type refKind int

const (
	refNone refKind = iota
	refSelf
	refField
	refParent
	refFirstOf
)

// CustomerRef says where an entry's customer is found; only the consumer resolves it.
type CustomerRef struct {
	kind    refKind
	column  string
	parent  types.SystemEntityType
	whenCol string
	whenVal string
	refs    []CustomerRef
}

// Self marks the row itself as the customer.
func Self() CustomerRef { return CustomerRef{kind: refSelf} }

// Field reads the customer id from a snapshot column.
func Field(col string) CustomerRef { return CustomerRef{kind: refField, column: col} }

// Parent reads a parent entity id from a snapshot column; the consumer looks up the parent's customer.
func Parent(t types.SystemEntityType, col string) CustomerRef {
	return CustomerRef{kind: refParent, parent: t, column: col}
}

func None() CustomerRef { return CustomerRef{} }

// FirstOf takes the first ref whose condition matches.
func FirstOf(refs ...CustomerRef) CustomerRef { return CustomerRef{kind: refFirstOf, refs: refs} }

// When makes the ref apply only when the snapshot column equals val.
func (r CustomerRef) When(col, val string) CustomerRef {
	r.whenCol, r.whenVal = col, val
	return r
}

// columns lists every snapshot column the ref reads.
func (r CustomerRef) columns() []string {
	var cols []string
	if r.column != "" {
		cols = append(cols, r.column)
	}
	if r.whenCol != "" {
		cols = append(cols, r.whenCol)
	}
	for _, sub := range r.refs {
		cols = append(cols, sub.columns()...)
	}
	return cols
}

// CustomerResolver returns the customer id of a parent entity (an invoice or a subscription).
type CustomerResolver interface {
	CustomerID(ctx context.Context, t types.SystemEntityType, id string) (string, error)
}

type repoCustomerResolver struct {
	invoices      invoice.Repository
	subscriptions subscription.Repository
}

func NewCustomerResolver(invoices invoice.Repository, subscriptions subscription.Repository) CustomerResolver {
	return &repoCustomerResolver{invoices: invoices, subscriptions: subscriptions}
}

func (r *repoCustomerResolver) CustomerID(ctx context.Context, t types.SystemEntityType, id string) (string, error) {
	switch t {
	case types.SystemEntityTypeInvoice:
		inv, err := r.invoices.Get(ctx, id)
		if err != nil {
			return "", err
		}
		return inv.CustomerID, nil
	case types.SystemEntityTypeSubscription:
		sub, err := r.subscriptions.Get(ctx, id)
		if ierr.IsNotFound(err) {
			sub, err = r.archivedSubscription(ctx, id)
		}
		if err != nil {
			return "", err
		}
		return sub.CustomerID, nil
	}
	return "", fmt.Errorf("activity: no customer lookup for parent %q", t)
}

// archivedSubscription reads a deleted subscription, which Get does not return.
func (r *repoCustomerResolver) archivedSubscription(ctx context.Context, id string) (*subscription.Subscription, error) {
	filter := types.NewSubscriptionFilter()
	filter.SubscriptionIDs = []string{id}
	filter.QueryFilter.Status = lo.ToPtr(types.StatusArchived)
	subs, err := r.subscriptions.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, ierr.NewErrorf("subscription %s not found", id).Mark(ierr.ErrNotFound)
	}
	return subs[0], nil
}

// resolveCustomer finds the customer id an entry rolls up to; "" when the ref points at no customer.
func resolveCustomer(ctx context.Context, ref CustomerRef, state map[string]any, customers CustomerResolver) (string, error) {
	if ref.whenCol != "" && stringField(state, ref.whenCol) != ref.whenVal {
		return "", nil
	}
	switch ref.kind {
	case refSelf:
		return stringField(state, "id"), nil
	case refField:
		return stringField(state, ref.column), nil
	case refParent:
		parentID := stringField(state, ref.column)
		if parentID == "" {
			return "", nil
		}
		return customers.CustomerID(ctx, ref.parent, parentID)
	case refFirstOf:
		for _, sub := range ref.refs {
			if sub.whenCol == "" || stringField(state, sub.whenCol) == sub.whenVal {
				return resolveCustomer(ctx, sub, state, customers)
			}
		}
	}
	return "", nil
}
