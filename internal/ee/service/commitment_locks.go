package service

import (
	"context"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"slices"
	"strings"
)

func commitmentEligibilityScopes(ctx context.Context, adapter *commitmentTermsOwner, sub *subscription.Subscription) ([]domain.EligibilityScope, error) {
	scopes := []domain.EligibilityScope{{Type: "subscription", ID: sub.ID}}
	if sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing {
		if sub.ParentSubscriptionID == nil || strings.TrimSpace(*sub.ParentSubscriptionID) == "" || *sub.ParentSubscriptionID == sub.ID {
			return nil, ierr.Validation("grouped collection requires its parent subscription")
		}
		parent, err := adapter.subscription(ctx, *sub.ParentSubscriptionID)
		if err != nil {
			return nil, err
		}
		if parent.SubscriptionType != types.SubscriptionTypeParent || parent.ParentSubscriptionID != nil {
			return nil, ierr.Validation("grouped collection has an invalid parent")
		}
		scopes = append(scopes, domain.EligibilityScope{Type: "subscription", ID: parent.ID})
	}
	slices.SortFunc(scopes, func(a, b domain.EligibilityScope) int { return strings.Compare(a.ID, b.ID) })
	return scopes, nil
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
