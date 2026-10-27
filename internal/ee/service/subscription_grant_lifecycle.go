package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// updateSubscriptionGrant changes a subscription grant's quota. The live window closes and a successor
// carries what was left plus the change, so an edit never hands out a fresh allowance.
func (s *entitlementService) updateSubscriptionGrant(ctx context.Context, existing *entitlement.Entitlement, req dto.UpdateEntitlementRequest) (*dto.EntitlementResponse, error) {
	if req.IsEnabled != nil || req.UsageLimit != nil || req.UsageResetPeriod != "" || req.IsSoftLimit != nil ||
		req.StaticValue != "" || req.ConfigValue != nil || req.ClearGrantConfig != nil || req.GrantMeasure != nil ||
		req.GrantDurationValue != nil || req.GrantDurationUnit != nil || req.GrantAllocationBehavior != nil ||
		req.AggregationMode != nil || req.GrantUnlimited != nil {
		return nil, ierr.NewError("only grant_quota can change on a subscription grant").
			WithHint("Delete the subscription grant and create a new one to change anything else.").
			WithReportableDetails(map[string]any{"entitlement_id": existing.ID}).
			Mark(ierr.ErrValidation)
	}
	if req.GrantQuota == nil || entitlement.QuotaEquals(existing.GrantQuota, req.GrantQuota) {
		return &dto.EntitlementResponse{Entitlement: existing}, nil
	}

	delta := req.GrantQuota.Sub(lo.FromPtr(existing.GrantQuota))
	existing.GrantQuota = req.GrantQuota
	if err := existing.Validate(); err != nil {
		return nil, err
	}

	var updated *entitlement.Entitlement
	if err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		updated, err = s.EntitlementRepo.Update(txCtx, existing)
		if err != nil {
			return err
		}
		live, err := s.liveSubscriptionGrantWindows(txCtx, updated)
		if err != nil {
			return err
		}
		return s.reissueSubscriptionGrantWindows(txCtx, live, "", delta, time.Now().UTC(), "entitlement_updated")
	}); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventEntitlementUpdated, updated.ID)
	return &dto.EntitlementResponse{Entitlement: updated}, nil
}

// settleDeletedSubscriptionGrant ends what a deleted subscription grant funded. Additive survivors keep
// the pool less the deleted quota; otherwise the windows close.
func (s *entitlementService) settleDeletedSubscriptionGrant(ctx context.Context, deleted *entitlement.Entitlement) error {
	live, err := s.liveSubscriptionGrantWindows(ctx, deleted)
	if err != nil || len(live) == 0 {
		return err
	}
	at := time.Now().UTC()

	if defaultedMode(deleted.AggregationMode) == types.EntitlementAggregationModeAdditive {
		survivors, err := s.subscriptionGrantConfigs(ctx, deleted.EntityID)
		if err != nil {
			return err
		}
		survivors = lo.Reject(survivors, func(ec *entitlement.Entitlement, _ int) bool { return ec.ID == deleted.ID })
		if len(survivors) > 0 {
			slotECID := grantCandidates(survivors)[0].ec.ID
			return s.reissueSubscriptionGrantWindows(ctx, live, slotECID, lo.FromPtr(deleted.GrantQuota).Neg(), at, "entitlement_deleted")
		}
	}

	_, err = NewEntitlementGrantService(s.ServiceParams).CloseEntitlementGrants(ctx, live, at)
	return err
}

// liveSubscriptionGrantWindows returns the live windows an entitlement feeds: the pooled window when
// additive, its own windows when parallel.
func (s *entitlementService) liveSubscriptionGrantWindows(ctx context.Context, ec *entitlement.Entitlement) ([]*entitlementgrant.EntitlementGrant, error) {
	sub, err := s.SubRepo.Get(ctx, ec.EntityID)
	if err != nil {
		return nil, err
	}
	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithScopeEntityType(types.EntitlementGrantScopeSubscription).
		WithLiveOnly(time.Now().UTC())
	if defaultedMode(ec.AggregationMode) == types.EntitlementAggregationModeParallel {
		filter = filter.WithEntitlementConfigIDs(ec.ID)
	}
	return s.EntitlementGrantRepo.List(ctx, filter)
}

// reissueSubscriptionGrantWindows closes the windows at `at` and opens successors for the rest of each
// window with what was left plus delta. An empty slotECID keeps each window's slot.
func (s *entitlementService) reissueSubscriptionGrantWindows(
	ctx context.Context,
	live []*entitlementgrant.EntitlementGrant,
	slotECID string,
	delta decimal.Decimal,
	at time.Time,
	source string,
) error {
	closedByID, err := NewEntitlementGrantService(s.ServiceParams).CloseEntitlementGrants(ctx, live, at)
	if err != nil {
		return err
	}

	for _, g := range live {
		closed := closedByID[g.ID]
		if closed == nil || !closed.ValidTo.Before(g.ValidTo) {
			continue
		}
		// A never-evaluated window is removed rather than closed; the successor then spans all of it.
		start := closed.ValidTo
		remaining, _ := closed.Remaining()
		quota := decimal.Max(remaining.Add(delta), decimal.Zero)

		// A spent successor still holds the slot: it opens already crossed, so every unit in it bills.
		status := types.EntitlementGrantStatusActive
		var crossedAt *time.Time
		if quota.IsZero() {
			status = types.EntitlementGrantStatusExhausted
			crossedAt = &start
		}

		successor := entitlementgrant.NewEntitlementGrantBuilder(g).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT)).
			WithEntitlementConfigID(lo.Ternary(slotECID == "", g.EntitlementConfigID, slotECID)).
			WithQuota(quota).
			WithWindow(start, g.ValidTo).
			WithGrantStatus(status).
			WithUsage(decimal.Zero).
			WithLastComputedAt(nil).
			WithQuotaCrossedAt(crossedAt).
			WithMetadata(types.Metadata{
				"reissue_source": source,
				"reissued_from":  g.ID,
				"reissue_delta":  delta.String(),
			}).
			WithEnvironmentID(types.GetEnvironmentID(ctx)).
			WithBaseModel(types.GetDefaultBaseModel(ctx)).
			Build()
		if err := successor.Validate(); err != nil {
			return err
		}
		if _, err := s.EntitlementGrantRepo.Create(ctx, successor); err != nil {
			return err
		}
	}
	return nil
}
