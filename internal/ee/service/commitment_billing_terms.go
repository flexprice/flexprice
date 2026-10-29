package service

import (
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/types"
)

func commitmentBillingOwner(id string) domain.OwnerRef {
	return domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: id}
}
