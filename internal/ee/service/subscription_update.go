package service

import (
	"context"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

func subscriptionUpdateScoped(ctx context.Context, sub *subscription.Subscription, id string) bool {
	return sub != nil && sub.ID == id && sub.Status == types.StatusPublished &&
		sub.TenantID == types.GetTenantID(ctx) && sub.EnvironmentID == types.GetEnvironmentID(ctx)
}
