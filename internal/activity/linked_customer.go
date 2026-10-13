package activity

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/types"
)

// LinkedCustomerLookup returns the customer an entity is linked to; "" when it has none.
type LinkedCustomerLookup interface {
	GetLinkedCustomerID(ctx context.Context, id string) (string, error)
}

// LinkedCustomers holds the customer lookup of each entity type whose state has no customer_id.
type LinkedCustomers map[types.SystemEntityType]LinkedCustomerLookup

func NewLinkedCustomers(payments payment.Repository, refunds refund.Repository, prices price.Repository) LinkedCustomers {
	return LinkedCustomers{
		types.SystemEntityTypePayment: payments,
		types.SystemEntityTypeRefund:  refunds,
		types.SystemEntityTypePrice:   prices,
	}
}

// customerID is the entry's customer: the customer itself, the state's customer_id, else the entity's lookup.
func (l LinkedCustomers) customerID(ctx context.Context, entityType types.SystemEntityType, entityID string, state map[string]any) (string, error) {
	if entityType == types.SystemEntityTypeCustomer {
		return entityID, nil
	}
	if id := stringField(state, "customer_id"); id != "" {
		return id, nil
	}
	lookup, ok := l[entityType]
	if !ok {
		return "", nil
	}
	return lookup.GetLinkedCustomerID(ctx, entityID)
}
