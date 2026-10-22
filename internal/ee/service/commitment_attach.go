package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// checkLineItemCommitmentAttach runs the commitment price-attach guard for a usage line item joining sub.
// Other lines cost nothing; with commitments disabled the cost is the cached settings read.
func checkLineItemCommitmentAttach(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
) error {
	if li == nil || li.PriceType != types.PRICE_TYPE_USAGE {
		return nil
	}

	svc := &commitmentService{ServiceParams: params}
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return err
	}

	p, err := params.PriceRepo.Get(ctx, li.PriceID)
	if err != nil {
		return err
	}

	return svc.CheckPriceAttach(ctx, sub, p, li.StartDate)
}

// rejectLegacyCommitmentInScope rejects legacy line-item commitment fields on a line that an active
// commitment covers, since the two would bill the same usage twice.
func rejectLegacyCommitmentInScope(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	li *subscription.SubscriptionLineItem,
) error {
	if li == nil || !li.HasAnyCommitment() {
		return nil
	}

	svc := &commitmentService{ServiceParams: params}
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return err
	}

	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{sub.ID}
	f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive}
	active, err := params.CommitmentRepo.List(ctx, f)
	if err != nil || len(active) == 0 {
		return err
	}

	p, err := params.PriceRepo.Get(ctx, li.PriceID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, c := range active {
		if now.Before(c.EndDate) && commitmentCoversLine(c, li, p) {
			return invalidf(
				"line item %s is covered by commitment %s; legacy line-item commitment fields are not allowed",
				li.ID,
				c.ID,
			)
		}
	}

	return nil
}

func commitmentCoversLine(c *commitment.Commitment, li *subscription.SubscriptionLineItem, p *price.Price) bool {
	switch c.ScopeEntityType {
	case types.CommitmentScopeSubscription:
		return true
	case types.CommitmentScopeGroup:
		return p.GroupID == c.ScopeEntityID
	case types.CommitmentScopeMeter:
		return li.MeterID == c.ScopeEntityID
	case types.CommitmentScopePrice:
		return li.PriceID == c.ScopeEntityID
	}

	return false
}

// planSyncSkipsPrice reports whether plan sync must leave p off sub because it would break an active
// commitment. A violation is logged and skipped so one subscription never fails the whole sync.
func planSyncSkipsPrice(
	ctx context.Context,
	params ServiceParams,
	sub *subscription.Subscription,
	p *price.Price,
	at time.Time,
) (bool, error) {
	svc := &commitmentService{ServiceParams: params}
	var commitmentID string
	err := params.DB.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		commitmentID, err = svc.checkPriceAttach(txCtx, sub, p, at)
		return err
	})
	if err == nil {
		return false, nil
	}

	if !ierr.IsValidation(err) {
		return false, err
	}

	params.Logger.Info(ctx, "plan sync skipped a price that would break a commitment",
		"subscription_id", sub.ID, "price_id", p.ID, "commitment_id", commitmentID, "reason", err.Error())
	return true, nil
}
