package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// EntitlementGrantService owns the lifecycle of entitlement_grants rows.
type EntitlementGrantService interface {
	// GetGrant returns one grant by id (webhook payloads, lookups).
	GetGrant(ctx context.Context, id string) (*entitlementgrant.EntitlementGrant, error)

	EnsureGrants(ctx context.Context, cust *customer.Customer, at time.Time) ([]*entitlementgrant.EntitlementGrant, *grantEvalMeta, error)

	// EnsureGrantsForSubscriptions is the data-fed variant: the caller supplies
	// the customer's active subscriptions so no extra DB fetch happens for them.
	// The returned meta carries the lookups (features, meters, external ids)
	// built during the pass so the evaluator can reuse them.
	EnsureGrantsForSubscriptions(ctx context.Context, cust *customer.Customer, subs []*subscription.Subscription, at time.Time) ([]*entitlementgrant.EntitlementGrant, *grantEvalMeta, error)
	GrantStateByFeature(ctx context.Context, sub *subscription.Subscription, at time.Time) (map[string]*dto.GrantState, error)

	// LiveGrantsByFeature is the subscription's open windows at `at`, keyed by feature.
	LiveGrantsByFeature(ctx context.Context, sub *subscription.Subscription, at time.Time) (map[string][]*entitlementgrant.EntitlementGrant, error)

	CloseEntitlementGrants(ctx context.Context, grants []*entitlementgrant.EntitlementGrant, closeAt time.Time) (map[string]*entitlementgrant.EntitlementGrant, error)
	ReissueEntitlementGrants(ctx context.Context, req *dto.ReissueEntitlementGrantsRequest) ([]*entitlementgrant.EntitlementGrant, error)
	OpenFeatureBasedEntitlementGrants(ctx context.Context, reqs []OpenFeatureBasedEntitlementGrantsRequest) ([]*entitlementgrant.EntitlementGrant, error)
}

type entitlementGrantService struct {
	ServiceParams
}

func NewEntitlementGrantService(params ServiceParams) EntitlementGrantService {
	return &entitlementGrantService{ServiceParams: params}
}

func (s *entitlementGrantService) GetGrant(ctx context.Context, id string) (*entitlementgrant.EntitlementGrant, error) {
	return s.EntitlementGrantRepo.Get(ctx, id)
}

type OpenFeatureBasedEntitlementGrantsRequest struct {
	FeatureID   string
	Closed      *entitlementgrant.EntitlementGrant // If any for the same feature
	New         *entitlementgrant.EntitlementGrant
	ExistingECs []*entitlement.Entitlement
	IncomingECs []*entitlement.Entitlement
}

func (s *entitlementGrantService) CloseEntitlementGrants(
	ctx context.Context,
	grants []*entitlementgrant.EntitlementGrant,
	closeAt time.Time,
) (map[string]*entitlementgrant.EntitlementGrant, error) {
	closed := make(map[string]*entitlementgrant.EntitlementGrant, len(grants))

	for _, g := range grants {
		if g == nil {
			continue
		}

		lastComputed := lo.FromPtr(g.LastComputedAt)

		// Never evaluated, so the window has no measurable life. Closing it would leave both
		// windows permitting the same quota, because the usage it really saw was never
		// measured into the successor's balance. Remove it instead and let the successor
		// span the original window, where every event still counts against one pool.
		// Keyed on last_computed_at rather than the close boundary: a window nothing has
		// measured cannot be split at any instant, however the caller dates the change.
		// A window already in overage is exempt: it was born crossed, so it carries state no
		// tick wrote, and replacing it would hand its overage a fresh quota retroactively.
		if !lastComputed.After(g.ValidFrom) && g.QuotaCrossedAt == nil {
			if err := s.EntitlementGrantRepo.Delete(ctx, g.ID); err != nil {
				return nil, err
			}

			s.Logger.Info(ctx, "removed un-evaluated entitlement grant window",
				"grant_id", g.ID,
				"valid_from", g.ValidFrom)

			// Carries a zero-length window on purpose: valid_to is where the successor
			// starts, and for a removed row that is its own valid_from. Never persisted.
			closed[g.ID] = entitlementgrant.NewEntitlementGrantBuilder(g).
				WithWindow(g.ValidFrom, g.ValidFrom).
				Build()
			continue
		}

		// The change owns the boundary, so a future-dated one hands the successor its quota
		// when the change is actually live rather than now.
		boundary := types.EarliestOf(types.LatestOf(closeAt, lastComputed), g.ValidTo)

		// Leaves usage, grant_status and last_computed_at alone: last_computed_at < valid_to
		// is what keeps a closed row in the evaluator's unfinalized set for its final
		// refresh, so tail events still reach the billing reads.
		if err := s.EntitlementGrantRepo.CloseWindow(ctx, g.ID, boundary); err != nil {
			return nil, err
		}

		s.Logger.Info(ctx, "closed entitlement grant window",
			"grant_id", g.ID,
			"closed_at", boundary,
			"quota", g.Quota.String(),
			"usage", g.Usage.String())

		closed[g.ID] = entitlementgrant.NewEntitlementGrantBuilder(g).
			WithWindow(g.ValidFrom, boundary).
			Build()
	}

	return closed, nil
}

func (s *entitlementGrantService) LiveGrantsByFeature(
	ctx context.Context,
	sub *subscription.Subscription,
	at time.Time,
) (map[string][]*entitlementgrant.EntitlementGrant, error) {
	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithLiveOnly(at)

	rows, err := s.EntitlementGrantRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	byFeature := make(map[string][]*entitlementgrant.EntitlementGrant)
	for _, g := range rows {
		if g == nil || !g.IsFeatureScoped() {
			continue
		}
		byFeature[g.FeatureID()] = append(byFeature[g.FeatureID()], g)
	}
	return byFeature, nil
}

// ReissueEntitlementGrants closes what is live and opens a successor for the rest of the
// window. Each row measures its own span, so no usage figure is ever carried.
func (s *entitlementGrantService) ReissueEntitlementGrants(
	ctx context.Context,
	req *dto.ReissueEntitlementGrantsRequest,
) ([]*entitlementgrant.EntitlementGrant, error) {
	if req == nil || s.EntitlementGrantRepo == nil {
		return nil, nil
	}

	sub, err := s.SubRepo.Get(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	liveByFeature, err := s.LiveGrantsByFeature(ctx, sub, req.At)
	if err != nil {
		return nil, err
	}
	live := liveByFeature[req.FeatureID]
	if len(live) == 0 {
		return nil, nil
	}

	ecsByFeature, err := newSubscriptionGrantService(s.ServiceParams).GetSubscriptionGrantECsByFeature(ctx, sub)
	if err != nil {
		return nil, err
	}
	featureECs := ecsByFeature[req.FeatureID]

	var opened []*entitlementgrant.EntitlementGrant
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		closedByID, err := s.CloseEntitlementGrants(txCtx, live, req.At)
		if err != nil {
			return err
		}

		reqs := make([]OpenFeatureBasedEntitlementGrantsRequest, 0, len(live))
		for _, g := range live {
			closed := closedByID[g.ID]
			if closed == nil {
				continue
			}
			reqs = append(reqs, OpenFeatureBasedEntitlementGrantsRequest{
				FeatureID: req.FeatureID,
				Closed:    closed,
				New: entitlementgrant.NewEntitlementGrantBuilder(g).
					WithQuota(req.Delta).
					WithUnlimited(req.Unlimited).
					WithWindow(closed.ValidTo, g.ValidTo).
					WithMetadata(types.Metadata{
						"reissue_source": req.Source,
						"reissued_from":  g.ID,
						"reissue_delta":  req.Delta.String(),
					}).
					Build(),
				ExistingECs: featureECs,
			})
		}
		if len(reqs) == 0 {
			return nil
		}

		opened, err = s.OpenFeatureBasedEntitlementGrants(txCtx, reqs)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "re-issued entitlement grant windows",
		"feature_id", req.FeatureID,
		"closed", len(live),
		"opened", len(opened),
		"delta", req.Delta.String(),
		"source", req.Source)
	return opened, nil
}

func (s *entitlementGrantService) OpenFeatureBasedEntitlementGrants(
	ctx context.Context,
	reqs []OpenFeatureBasedEntitlementGrantsRequest,
) ([]*entitlementgrant.EntitlementGrant, error) {
	opened := make([]*entitlementgrant.EntitlementGrant, 0, len(reqs))

	for _, req := range reqs {
		featureECs := append(append([]*entitlement.Entitlement{}, req.ExistingECs...), req.IncomingECs...)
		if req.New == nil || len(featureECs) == 0 || hasParallelECs(featureECs) {
			s.Logger.Info(ctx, "skipping entitlement grant open",
				"feature_id", req.FeatureID)
			continue
		}

		// Unlimited carries no quota to be positive about.
		if req.Closed == nil && !req.New.Unlimited && !req.New.Quota.IsPositive() {
			s.Logger.Info(ctx, "skipping entitlement grant open; resulting quota is not positive",
				"feature_id", req.FeatureID,
				"quota", req.New.Quota.String())
			continue
		}

		candidate := grantCandidatesForFeature(featureECs)[0]
		slotECID := candidate.ec.ID
		validFrom := req.New.ValidFrom
		quota := req.New.Quota
		unlimited := candidate.unlimited

		if req.Closed != nil {
			if !validFrom.IsZero() && !validFrom.Equal(req.Closed.ValidTo) {
				return nil, ierr.NewError("successor window does not tile with the closed grant").
					WithHint("A grant segment must start where the window it succeeds ended").
					WithReportableDetails(map[string]interface{}{
						"feature_id":       req.FeatureID,
						"closed_grant_id":  req.Closed.ID,
						"closed_valid_to":  req.Closed.ValidTo,
						"opening_valid_at": validFrom,
					}).
					Mark(ierr.ErrValidation)
			}

			carried, _ := req.Closed.Remaining()
			quota = carried.Add(req.New.Quota)
		} else {
			for _, ec := range req.ExistingECs {
				quota = quota.Add(lo.FromPtr(ec.GrantQuota))
			}
		}

		if !validFrom.Before(req.New.ValidTo) {
			s.Logger.Info(ctx, "skipping entitlement grant open; no window left in the cycle",
				"feature_id", req.FeatureID,
				"valid_from", validFrom,
				"valid_to", req.New.ValidTo)
			continue
		}

		if quota.IsNegative() {
			// Cold start has no slot to hold, so nothing is lost by skipping. A successor
			// does: skip it and the closed predecessor leaves the slot unheld, and the tick
			// reissues a full allowance for the feature. Clamp and hold it instead.
			if req.Closed == nil {
				s.Logger.Info(ctx, "skipping entitlement grant open; resulting quota is negative",
					"feature_id", req.FeatureID,
					"quota", quota.String())
				continue
			}
			quota = decimal.Zero
		}

		// A spent predecessor hands forward nothing. The successor still has to exist —
		// it is what holds the slot on a live config — so it opens at zero, already
		// crossed from its first instant, and every unit in it bills. An unlimited window
		// is zero-quota by construction and has no ceiling to cross.
		status := types.EntitlementGrantStatusActive
		var crossedAt *time.Time
		if !unlimited && quota.IsZero() {
			status = types.EntitlementGrantStatusExhausted
			crossedAt = &validFrom
		}

		grant := entitlementgrant.NewEntitlementGrantBuilder(req.New).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT)).
			WithEntitlementConfigID(slotECID).
			WithQuota(quota).
			WithUnlimited(unlimited).
			WithWindow(validFrom, req.New.ValidTo).
			WithGrantStatus(status).
			WithUsage(decimal.Zero).
			WithLastComputedAt(nil).
			WithQuotaCrossedAt(crossedAt).
			WithEnvironmentID(types.GetEnvironmentID(ctx)).
			WithBaseModel(types.GetDefaultBaseModel(ctx)).
			Build()
		if err := grant.Validate(); err != nil {
			return nil, err
		}

		created, err := s.EntitlementGrantRepo.Create(ctx, grant)
		if err != nil {
			return nil, err
		}

		s.Logger.Info(ctx, "opened entitlement grant segment",
			"grant_id", created.ID,
			"feature_id", req.FeatureID,
			"valid_from", validFrom,
			"quota", quota.String(),
			"delta", req.New.Quota.String())

		opened = append(opened, created)
	}

	return opened, nil
}

// grantEvalMeta is the per-pass lookup bundle shared by grant opening and the
// evaluator's usage-refresh loop, so each entity is fetched at most once.
type grantEvalMeta struct {
	subsByID    map[string]*subscription.Subscription
	featureByID map[string]*feature.Feature
	meterByID   map[string]*meter.Meter

	// extIDsBySub memoizes ExternalCustomerIDsForSubscription — resolved lazily
	// so customers with no grant work pay nothing.
	extIDsBySub     map[string][]string
	subscriptionSvc SubscriptionService
}

func (m *grantEvalMeta) externalIDs(ctx context.Context, sub *subscription.Subscription) ([]string, error) {
	if ids, ok := m.extIDsBySub[sub.ID]; ok {
		return ids, nil
	}
	ids, err := m.subscriptionSvc.ExternalCustomerIDsForSubscription(ctx, sub)
	if err != nil {
		return nil, err
	}
	m.extIDsBySub[sub.ID] = ids
	return ids, nil
}

// -----------------------------------------------------------------------------
// EnsureGrants
// -----------------------------------------------------------------------------

func (s *entitlementGrantService) EnsureGrants(
	ctx context.Context,
	cust *customer.Customer,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, *grantEvalMeta, error) {
	if cust == nil {
		return nil, nil, ierr.NewError("customer is required").Mark(ierr.ErrValidation)
	}
	subs, err := s.listActiveSubscriptions(ctx, cust.ID, at)
	if err != nil {
		return nil, nil, err
	}
	grants, meta, err := s.EnsureGrantsForSubscriptions(ctx, cust, subs, at)
	return grants, meta, err
}

func (s *entitlementGrantService) EnsureGrantsForSubscriptions(
	ctx context.Context,
	cust *customer.Customer,
	subs []*subscription.Subscription,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, *grantEvalMeta, error) {
	if cust == nil {
		return nil, nil, ierr.NewError("customer is required").Mark(ierr.ErrValidation)
	}
	if len(subs) == 0 {
		return nil, nil, nil
	}

	subscriptionSvc := NewSubscriptionService(s.ServiceParams)
	ecsBySub := make(map[string][]*entitlement.Entitlement, len(subs))
	for _, sub := range subs {
		ents, err := subscriptionSvc.GetSubscriptionEntitlements(ctx, sub.ID)
		if err != nil {
			return nil, nil, err
		}
		rows := make([]*entitlement.Entitlement, 0, len(ents))
		for _, e := range ents {
			if e == nil || e.Entitlement == nil {
				continue
			}
			rows = append(rows, e.Entitlement)
		}
		ecsBySub[sub.ID] = rows
	}

	// Two constant-size reads regardless of how many windows the cycle has
	// accumulated: (1) max(valid_to) per slot — occupancy plus the covered-until
	// bound for the next window; (2) the working set — open windows plus closed
	// ones whose snapshot predates the close (they owe one final refresh so
	// tail events reach the usage billing reads).
	minCycleStart := subs[0].CurrentPeriodStart
	for _, sub := range subs[1:] {
		if sub.CurrentPeriodStart.Before(minCycleStart) {
			minCycleStart = sub.CurrentPeriodStart
		}
	}
	slotEnds, err := s.EntitlementGrantRepo.LatestWindowEndBySlot(ctx, cust.ID, minCycleStart)
	if err != nil {
		return nil, nil, err
	}
	latestEndBySlot := make(map[string]time.Time, len(slotEnds))
	for _, se := range slotEnds {
		latestEndBySlot[grantSlotKey(se.EntitlementConfigID, se.SubscriptionID)] = se.ValidTo
	}

	rows, err := s.EntitlementGrantRepo.ListOpenOrUnfinalized(ctx, cust.ID, at, minCycleStart)
	if err != nil {
		return nil, nil, err
	}
	live := make([]*entitlementgrant.EntitlementGrant, 0, len(rows))
	finalize := make([]*entitlementgrant.EntitlementGrant, 0)
	for _, g := range rows {
		if g.ValidTo.After(at) {
			live = append(live, g)
		} else {
			finalize = append(finalize, g)
		}
	}

	meta, err := s.buildGrantEvalMeta(ctx, subs, ecsBySub, rows, subscriptionSvc)
	if err != nil {
		return nil, nil, err
	}

	opened, err := s.openMissingGrants(ctx, subs, ecsBySub, latestEndBySlot, meta, at)
	if err != nil {
		return nil, nil, err
	}

	out := append(append(live, opened...), finalize...)
	if len(out) == 0 {
		return nil, nil, nil // nothing to evaluate — nil meta signals the evaluator to skip
	}
	return out, meta, nil
}

// buildGrantEvalMeta loads features and meters for every grant EC and existing
// grant in one query each. External customer ids resolve lazily on first use.
func (s *entitlementGrantService) buildGrantEvalMeta(
	ctx context.Context,
	subs []*subscription.Subscription,
	ecsBySub map[string][]*entitlement.Entitlement,
	grants []*entitlementgrant.EntitlementGrant,
	subscriptionSvc SubscriptionService,
) (*grantEvalMeta, error) {
	meta := &grantEvalMeta{
		subsByID:        lo.KeyBy(subs, func(sub *subscription.Subscription) string { return sub.ID }),
		featureByID:     map[string]*feature.Feature{},
		meterByID:       map[string]*meter.Meter{},
		extIDsBySub:     map[string][]string{},
		subscriptionSvc: subscriptionSvc,
	}

	featureIDs := make([]string, 0)
	for _, ecs := range ecsBySub {
		for _, ec := range ecs {
			if ec.HasGrantConfig() {
				featureIDs = append(featureIDs, ec.FeatureID)
			}
		}
	}
	for _, g := range grants {
		if g.IsFeatureScoped() {
			featureIDs = append(featureIDs, g.ScopeEntityID)
		}
	}
	featureIDs = lo.Uniq(featureIDs)
	if len(featureIDs) == 0 {
		return meta, nil
	}

	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.FeatureIDs = featureIDs
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		return nil, err
	}
	meta.featureByID = lo.KeyBy(features, func(f *feature.Feature) string { return f.ID })

	meterIDs := lo.Uniq(lo.FilterMap(features, func(f *feature.Feature, _ int) (string, bool) {
		return f.MeterID, f.MeterID != ""
	}))
	if len(meterIDs) > 0 {
		meters, err := s.MeterRepo.ListByIDs(ctx, meterIDs)
		if err != nil {
			return nil, err
		}
		meta.meterByID = lo.KeyBy(meters, func(m *meter.Meter) string { return m.ID })
	}
	return meta, nil
}

// grantSlotKey identifies the grant slot: one open window at a time per
// (config, subscription) per customer.
func grantSlotKey(entitlementConfigID, subscriptionID string) string {
	return entitlementConfigID + "/" + subscriptionID
}

func (s *entitlementGrantService) listActiveSubscriptions(
	ctx context.Context,
	customerID string,
	at time.Time,
) ([]*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = customerID
	filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive, types.SubscriptionStatusTrialing}
	filter.ActiveAt = &at
	return s.SubRepo.List(ctx, filter)
}

// grantCandidate is one grant to open: the EC whose slot it occupies and the
// quota it carries (the EC's own quota for parallel, the group sum for additive).
type grantCandidate struct {
	ec    *entitlement.Entitlement
	quota decimal.Decimal
	// unlimited: any contributor without a quota ceiling makes the whole pool
	// unlimited, matching how a nil usage_limit behaves in the legacy model.
	unlimited bool

	// startDate is the earliest instant any contributing EC was live from — an
	// addon's association start, typically. Zero when every contributor runs for
	// the whole cycle. The window cannot open before it, or a slot introduced
	// mid-cycle would backdate to the cycle start and eat usage that predates it.
	startDate time.Time
}

// openMissingGrants: parallel = one grant per EC, additive = one on the primary EC with
// the quotas summed. A config change only lands when the open window ends.
func (s *entitlementGrantService) openMissingGrants(
	ctx context.Context,
	subs []*subscription.Subscription,
	ecsBySub map[string][]*entitlement.Entitlement,
	latestEndBySlot map[string]time.Time,
	meta *grantEvalMeta,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	opened := make([]*entitlementgrant.EntitlementGrant, 0)
	for _, sub := range subs {
		for _, featureECs := range s.eligibleGrantConfigsByFeature(ctx, sub, ecsBySub[sub.ID]) {
			for _, candidate := range grantCandidatesForFeature(featureECs) {
				slotOpened, err := s.openIfSlotFree(ctx, sub, candidate, latestEndBySlot, meta, at)
				if err != nil {
					return nil, err
				}
				opened = append(opened, slotOpened...)
			}
		}
	}
	return opened, nil
}

// eligibleGrantConfigsByFeature skips durations >= the cycle: a cycle-long grant is
// just the cycle quota, which is usage_reset_period's job.
func (s *entitlementGrantService) eligibleGrantConfigsByFeature(
	ctx context.Context,
	sub *subscription.Subscription,
	ecs []*entitlement.Entitlement,
) map[string][]*entitlement.Entitlement {
	cycleLen := sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart)

	byFeature := make(map[string][]*entitlement.Entitlement)
	for _, ec := range ecs {
		if !ec.HasGrantConfig() {
			continue
		}
		dur, err := ec.GrantDuration()
		if err != nil {
			s.Logger.Error(ctx, "invalid grant duration on entitlement, skipping",
				"entitlement_id", ec.ID, "error", err)
			continue
		}
		if dur >= cycleLen {
			s.Logger.Debug(ctx, "grant duration >= subscription cycle, skipping grant open",
				"entitlement_id", ec.ID,
				"subscription_id", sub.ID,
				"grant_duration", dur.String(),
				"cycle_length", cycleLen.String())
			continue
		}
		byFeature[ec.FeatureID] = append(byFeature[ec.FeatureID], ec)
	}
	return byFeature
}

// grantCandidatesForFeature: parallel → one candidate per EC; additive → one
// candidate on the primary (lowest-ID) EC with the summed quota.
func grantCandidatesForFeature(featureECs []*entitlement.Entitlement) []grantCandidate {
	if hasParallelECs(featureECs) {
		return lo.Map(featureECs, func(ec *entitlement.Entitlement, _ int) grantCandidate {
			return grantCandidate{
				ec:        ec,
				quota:     lo.FromPtr(ec.GrantQuota),
				unlimited: ec.IsUnlimitedGrant(),
				startDate: lo.FromPtr(ec.StartDate),
			}
		})
	}

	primary := featureECs[0]
	total := decimal.Zero
	unlimited := false

	earliest := lo.FromPtr(featureECs[0].StartDate)

	for _, ec := range featureECs {
		if ec.ID < primary.ID {
			primary = ec
		}
		if ec.IsUnlimitedGrant() {
			unlimited = true
		}
		total = total.Add(lo.FromPtr(ec.GrantQuota))

		if start := lo.FromPtr(ec.StartDate); start.Before(earliest) {
			earliest = start
		}
	}

	// No ceiling, so no quota to record. Keeping the bounded contributors' sum here
	// reads as a limit that is never enforced.
	if unlimited {
		total = decimal.Zero
	}

	return []grantCandidate{{ec: primary, quota: total, unlimited: unlimited, startDate: earliest}}
}

// openIfSlotFree walks the slot forward until it is caught up, so one tick clears a
// backlog instead of needing an event per missed window. Each pass advances the covered
// range, so it terminates at cycle_end.
func (s *entitlementGrantService) openIfSlotFree(
	ctx context.Context,
	sub *subscription.Subscription,
	candidate grantCandidate,
	latestEndBySlot map[string]time.Time,
	meta *grantEvalMeta,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	slot := grantSlotKey(candidate.ec.ID, sub.ID)
	opened := make([]*entitlementgrant.EntitlementGrant, 0, 1)
	for {
		lastEnd := latestEndBySlot[slot]
		// Done when the latest window is still open, or coverage already reaches
		// cycle_end (catch-up finished, or rollover lag with a stale sub) — the
		// anchor range would be empty, so skip the usage query outright.
		if lastEnd.After(at) || !lastEnd.Before(sub.CurrentPeriodEnd) {
			return opened, nil
		}

		g, err := s.openOneGrant(ctx, sub, candidate, lastEnd, meta, at)
		if err != nil {
			return opened, err
		}
		if g == nil {
			return opened, nil // no uncovered usage left
		}
		opened = append(opened, g)
		latestEndBySlot[slot] = g.ValidTo
	}
}

// openOneGrant inserts the slot's next grant. On INSERT conflict — valid_from
// is deterministic, so two racers collide on the unique (slot, valid_from)
// index — it re-reads the winner. Returns nil when there is no window to open.
func (s *entitlementGrantService) openOneGrant(
	ctx context.Context,
	sub *subscription.Subscription,
	candidate grantCandidate,
	lastWindowEnd time.Time,
	meta *grantEvalMeta,
	at time.Time,
) (*entitlementgrant.EntitlementGrant, error) {
	ec := candidate.ec
	quota := candidate.quota

	dur, err := ec.GrantDuration()
	if err != nil {
		return nil, err
	}

	validFrom, validTo, ok, err := s.computeGrantWindow(ctx, candidate, sub, meta, lastWindowEnd, at, dur)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.Logger.Debug(ctx, "no uncovered usage, no grant to open",
			"entitlement_config_id", ec.ID,
			"subscription_id", sub.ID,
			"customer_id", sub.CustomerID)
		return nil, nil
	}

	newGrant := entitlementgrant.NewEntitlementGrantBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT)).
		WithEntitlementConfigID(ec.ID).
		WithCustomerID(sub.CustomerID).
		WithSubscriptionID(sub.ID).
		WithScope(types.EntitlementGrantScopeFeature, ec.FeatureID).
		WithMeasure(ec.GrantMeasure).
		WithQuota(quota).
		WithUnlimited(candidate.unlimited).
		WithWindow(validFrom, validTo).
		WithGrantStatus(types.EntitlementGrantStatusActive).
		WithEnvironmentID(types.GetEnvironmentID(ctx)).
		WithBaseModel(types.GetDefaultBaseModel(ctx)).
		Build()
	if err := newGrant.Validate(); err != nil {
		return nil, err
	}

	created, err := s.EntitlementGrantRepo.Create(ctx, newGrant)
	if err == nil {
		return created, nil
	}
	if !ierr.IsAlreadyExists(err) {
		return nil, err
	}
	// Lost the race; re-read the winner so callers see a consistent set.
	return s.EntitlementGrantRepo.FindLastBySlot(ctx, ec.ID, sub.CustomerID, sub.ID)
}

// computeGrantWindow derives [valid_from, valid_to) from the first usage past the
// covered range; no uncovered usage means no window. The 1h minimum is best-effort: a
// window stretches to cycle_end rather than leave a stub, but a forced tail may be
// short anyway — coverage beats tidy lengths.
func (s *entitlementGrantService) computeGrantWindow(
	ctx context.Context,
	candidate grantCandidate,
	sub *subscription.Subscription,
	meta *grantEvalMeta,
	lastWindowEnd time.Time,
	at time.Time,
	dur time.Duration,
) (time.Time, time.Time, bool, error) {
	ec := candidate.ec
	cycleStart := sub.CurrentPeriodStart
	cycleEnd := sub.CurrentPeriodEnd

	// A slot whose configs only became live mid-cycle starts there, not at the cycle
	// start: backdating it would hand the window usage recorded before it existed.
	coveredUntil := types.LatestOf(types.LatestOf(lastWindowEnd, cycleStart), candidate.startDate)
	searchUntil := types.EarliestOf(at, cycleEnd)

	s.Logger.Debug(ctx, "computing grant window",
		"coveredUntil", coveredUntil,
		"searchUntil", searchUntil,
		"cycleStart", cycleStart,
		"cycleEnd", cycleEnd,
		"eventTime", at,
		"grantDuration", dur,
		"durationUnit", ec.GrantDurationUnit,
		"allocationBehavior", ec.GrantAllocationBehavior,
	)

	// subscription_period: window is the whole cycle. Skip the earliestUncoveredUsage
	// query — the event timestamp is not used for validFrom, and openIfSlotFree
	// terminates after one grant per cycle (lastEnd == cycleEnd on the next pass).
	if ec.GrantDurationUnit == types.EntitlementGrantDurationUnitSubscriptionPeriod {
		s.Logger.Debug(ctx, "computed grant window (subscription_period)",
			"validFrom", coveredUntil, "validTo", cycleEnd)
		return coveredUntil, cycleEnd, true, nil
	}

	// hour/day/week: no uncovered usage in the cycle → no grant (windows are
	// anchored to first-event time, so nothing to anchor without an event).
	firstUncoveredEventAt, err := s.earliestUncoveredUsage(ctx, meta, sub, ec, coveredUntil, searchUntil)
	if err != nil || firstUncoveredEventAt == nil {
		return time.Time{}, time.Time{}, false, err
	}

	validFrom := *firstUncoveredEventAt

	if ec.GrantAllocationBehavior == types.EntitlementGrantAllocationBehaviorUnitStart {
		// N-unit buckets anchored at the unit boundary containing cycleStart.
		// Count strides until the next stride starts after firstEvent; the
		// containing stride is the aligned start. Uses calendar arithmetic
		// (AddDate) for day/week so DST transitions do not drift the boundary.
		// Hour uses fixed .Add because top-of-hour instants remain stable in
		// UTC across DST transitions.
		n := lo.FromPtr(ec.GrantDurationValue)
		if n < 1 {
			n = 1
		}
		tz := sub.Timezone
		var anchor time.Time
		var advance func(time.Time) time.Time
		switch ec.GrantDurationUnit {
		case types.EntitlementGrantDurationUnitHour:
			anchor = types.FloorToStartOfHour(cycleStart, tz)
			stride := time.Duration(n) * time.Hour
			advance = func(t time.Time) time.Time { return t.Add(stride) }
		case types.EntitlementGrantDurationUnitDay:
			anchor = types.FloorToStartOfDay(cycleStart, tz)
			advance = func(t time.Time) time.Time { return types.AdvanceDays(t, n, tz) }
		case types.EntitlementGrantDurationUnitWeek:
			anchor = types.FloorToStartOfWeek(cycleStart, tz)
			advance = func(t time.Time) time.Time { return types.AdvanceDays(t, 7*n, tz) }
		}
		if !anchor.IsZero() && advance != nil {
			bucket := anchor
			for {
				next := advance(bucket)
				if firstUncoveredEventAt.Before(next) {
					break
				}
				bucket = next
			}
			validFrom = types.LatestOf(bucket, coveredUntil)
		}
	}

	validTo := validFrom.Add(dur)
	// Cap at cycle_end AND absorb a sub-minimum trailing remainder in one rule
	if cycleEnd.Sub(validTo) < types.EntitlementGrantMinDuration {
		validTo = cycleEnd
	}

	s.Logger.Debug(ctx, "computed grant window",
		"validFrom", validFrom,
		"validTo", validTo,
	)
	return validFrom, validTo, true, nil
}

// earliestUncoveredUsage is the first event in [coveredUntil, until), or nil.
func (s *entitlementGrantService) earliestUncoveredUsage(
	ctx context.Context,
	meta *grantEvalMeta,
	sub *subscription.Subscription,
	ec *entitlement.Entitlement,
	coveredUntil, until time.Time,
) (*time.Time, error) {
	f := meta.featureByID[ec.FeatureID]
	if f == nil || f.MeterID == "" {
		s.Logger.Debug(ctx, "computing grant window: no meter found for feature",
			"feature_id", ec.FeatureID,
			"subscription_id", sub.ID,
			"customer_id", sub.CustomerID,
		)
		return nil, nil
	}

	extIDs, err := meta.externalIDs(ctx, sub)
	if err != nil {
		return nil, err
	}
	timestamp, err := s.MeterUsageRepo.GetEarliestUsageTimestamp(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: extIDs,
		MeterID:             f.MeterID,
		StartTime:           coveredUntil,
		EndTime:             until,
	})
	if err != nil {
		s.Logger.Error(ctx, "computing grant window: error getting earliest usage timestamp", "error", err)
		return nil, err
	}

	s.Logger.Debug(ctx, "computing grant window: earliest un-covered usage timestamp", "timestamp", timestamp)
	return timestamp, nil
}

// validateEntitlementGrantShape enforces the rules that need the meter, its prices and
// the sibling ECs. No-op without a grant config; each rejection explains itself.
func (s *entitlementService) validateEntitlementGrantShape(
	ctx context.Context,
	e *entitlement.Entitlement,
	m *meter.Meter,
) error {
	if e == nil || !e.HasGrantConfig() {
		return nil
	}

	if m == nil {
		return ierr.NewError("meter is required to validate grant-based entitlements").
			Mark(ierr.ErrValidation)
	}

	if err := s.grantMeterEligibility(ctx, m, e.GrantMeasure); err != nil {
		return err
	}

	if err := s.validateGrantSiblingCoherence(ctx, e); err != nil {
		return err
	}

	return nil
}

// grantMeterEligibility reports why this meter cannot carry a grant. measure only
// enriches the error; every rule applies to both lanes.
func (s *entitlementService) grantMeterEligibility(
	ctx context.Context,
	m *meter.Meter,
	measure types.EntitlementGrantMeasure,
) error {
	if m == nil {
		return ierr.NewError("meter is required to validate grant-based entitlements").
			Mark(ierr.ErrValidation)
	}

	if m.Aggregation.Type == types.AggregationMax {
		return ierr.NewError("grant-based entitlements are not supported for MAX meters").
			WithHint("This meter records a peak value, not a running total, so there is nothing for an allowance to draw down. Use a SUM or COUNT meter, or remove the allowance.").
			WithReportableDetails(map[string]interface{}{
				"meter_id":         m.ID,
				"aggregation_type": m.Aggregation.Type,
			}).
			Mark(ierr.ErrValidation)
	}
	//nolint:staticcheck // deprecated but still honoured: a meter carrying one still buckets
	if m.Aggregation.BucketSize != "" {
		return ierr.NewError("grant-based entitlements are not supported for bucketed meters").
			WithHint("This meter already groups usage into its own fixed windows, which an allowance window would cut across. Remove the bucket size from the meter, or remove the allowance.").
			WithReportableDetails(map[string]interface{}{
				"meter_id":    m.ID,
				"bucket_size": m.Aggregation.BucketSize,
			}).
			Mark(ierr.ErrValidation)
	}

	if err := s.validateEntitlementAgainstBucketedPrices(ctx, m, true); err != nil {
		return err
	}

	// Tier rates walk with cumulative cycle position, which a standalone window cannot
	// reproduce, so both measures are rejected.
	if s.PriceRepo == nil {
		return nil
	}
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.MeterIDs = []string{m.ID}
	prices, err := s.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		return ierr.WithError(err).
			WithReportableDetails(map[string]interface{}{"meter_id": m.ID}).
			Mark(ierr.ErrDatabase)
	}
	for _, p := range prices {
		if p.BillingModel == types.BILLING_MODEL_TIERED {
			return ierr.NewError("grant-based entitlements are not supported on tiered pricing").
				WithHint(fmt.Sprintf(
					"Price %s uses tiered billing (%s); use a flat-fee price, or remove the grant config.",
					p.ID, p.TierMode)).
				WithReportableDetails(map[string]interface{}{
					"price_id":      p.ID,
					"billing_model": p.BillingModel,
					"tier_mode":     p.TierMode,
					"meter_id":      m.ID,
					"grant_measure": measure,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// sharesNoResolvedSet reports whether sib can never apply alongside e. Approximate on
// purpose: proving a subscription runs sib's plan is a query per sibling, so that pairing
// stays in. Addons stay in too, since several can be attached at once.
func sharesNoResolvedSet(sib, e *entitlement.Entitlement) bool {
	if sib.EntityType != e.EntityType || sib.EntityID == e.EntityID {
		return false
	}
	return sib.EntityType == types.ENTITLEMENT_ENTITY_TYPE_PLAN ||
		sib.EntityType == types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION
}

// validateGrantSiblingCoherence: one aggregation mode and measure per feature, and one
// duration across an additive group, whose quotas share a single window.
func (s *entitlementService) validateGrantSiblingCoherence(ctx context.Context, e *entitlement.Entitlement) error {
	filter := types.NewNoLimitEntitlementFilter()
	filter.FeatureIDs = []string{e.FeatureID}
	filter.HasGrantConfig = lo.ToPtr(true)
	siblings, err := s.EntitlementRepo.List(ctx, filter)
	if err != nil {
		return ierr.WithError(err).
			WithReportableDetails(map[string]interface{}{"feature_id": e.FeatureID}).
			Mark(ierr.ErrDatabase)
	}

	mode := defaultedMode(e.AggregationMode)
	parentID := lo.FromPtr(e.ParentEntitlementID)
	for _, sib := range siblings {
		if sib.ID == e.ID {
			continue
		}
		// An override replaces its parent in the resolved set, so the two never share a
		// window.
		if sib.ID == parentID || lo.FromPtr(sib.ParentEntitlementID) == e.ID {
			continue
		}
		if sharesNoResolvedSet(sib, e) {
			continue
		}
		if defaultedMode(sib.AggregationMode) != mode {
			return ierr.NewError("aggregation_mode must match the other entitlements on this feature").
				WithHint("A feature's entitlements are either all additive or all parallel").
				WithReportableDetails(map[string]interface{}{
					"feature_id":     e.FeatureID,
					"entitlement_id": sib.ID,
					"existing_mode":  defaultedMode(sib.AggregationMode),
					"requested_mode": mode,
				}).
				Mark(ierr.ErrValidation)
		}
		if sib.GrantMeasure != e.GrantMeasure {
			return ierr.NewError("grant_measure must match the other entitlements on this feature").
				WithHint("A feature's entitlements all meter the same thing: either quantity or amount").
				WithReportableDetails(map[string]interface{}{
					"feature_id":       e.FeatureID,
					"entitlement_id":   sib.ID,
					"existing_measure": sib.GrantMeasure,
					"requested":        e.GrantMeasure,
				}).
				Mark(ierr.ErrValidation)
		}
		if mode == types.EntitlementAggregationModeAdditive {
			if lo.FromPtr(sib.GrantDurationValue) != lo.FromPtr(e.GrantDurationValue) ||
				sib.GrantDurationUnit != e.GrantDurationUnit {
				return ierr.NewError("additive entitlements on one feature must share grant_duration").
					WithHint("Additive quotas sum into one window; use parallel for independent windows").
					WithReportableDetails(map[string]interface{}{
						"feature_id":     e.FeatureID,
						"entitlement_id": sib.ID,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}
	return nil
}

func hasParallelECs(featureECs []*entitlement.Entitlement) bool {
	return lo.SomeBy(featureECs, func(ec *entitlement.Entitlement) bool {
		return defaultedMode(ec.AggregationMode) == types.EntitlementAggregationModeParallel
	})
}

func defaultedMode(m types.EntitlementAggregationMode) types.EntitlementAggregationMode {
	if m == "" {
		return types.EntitlementAggregationModeAdditive
	}
	return m
}

// GrantStateByFeature is the current period's ledger for a subscription, keyed by
// feature id. Spent allowances included; a feature with no grant config has no entry.
func (s *entitlementGrantService) GrantStateByFeature(
	ctx context.Context,
	sub *subscription.Subscription,
	at time.Time,
) (map[string]*dto.GrantState, error) {
	if sub == nil || s.EntitlementGrantRepo == nil {
		return nil, nil
	}

	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithScopeEntityType(types.EntitlementGrantScopeFeature)
	filter.WithCycleOverlap(sub.CurrentPeriodStart, sub.CurrentPeriodEnd)

	// Capped in the query: an hourly allowance leaves hundreds of rows in a monthly
	// cycle, and a read wants the live window and what led to it.
	grants, err := s.EntitlementGrantRepo.ListLatestWindows(ctx, filter, GrantWindowsPerSlot)
	if err != nil {
		return nil, err
	}
	if len(grants) == 0 {
		return nil, nil
	}

	out := make(map[string]*dto.GrantState)
	for _, g := range grants {
		if g == nil || !g.IsFeatureScoped() {
			continue
		}
		featureID := g.FeatureID()
		state, ok := out[featureID]
		if !ok {
			state = &dto.GrantState{Allowances: make([]*dto.GrantAllowanceState, 0, 4)}
			out[featureID] = state
		}

		// Null for an unlimited window: a number there reads as a balance the customer
		// does not have.
		var remaining *decimal.Decimal
		if left, bounded := g.Remaining(); bounded {
			remaining = &left
		}

		window := &dto.GrantAllowanceState{
			GrantID:        g.ID,
			EntitlementID:  g.EntitlementConfigID,
			Measure:        g.Measure,
			Unlimited:      g.Unlimited,
			Quota:          g.Quota,
			Usage:          g.Usage,
			Remaining:      remaining,
			ValidFrom:      g.ValidFrom,
			ValidTo:        g.ValidTo,
			Status:         g.GrantStatus,
			LastComputedAt: g.LastComputedAt,
			QuotaCrossedAt: g.QuotaCrossedAt,
			IsActive:       !g.ValidFrom.After(at) && g.ValidTo.After(at),
		}
		state.Allowances = append(state.Allowances, window)
	}

	// Oldest first: the ledger reads as a timeline.
	for _, state := range out {
		sort.Slice(state.Allowances, func(i, j int) bool { return state.Allowances[i].ValidFrom.Before(state.Allowances[j].ValidFrom) })
	}

	return out, nil
}

// GrantWindowsPerRead caps how much of the ledger a read returns. An hourly allowance on
// a monthly cycle produces several hundred windows, and a reader wants the live one and
// what led to it. Split across the slots in play, never fewer than one each, so a
// GrantWindowsPerSlot caps how much of the ledger a read returns, per entitlement rather
// than per feature: a parallel feature has one series per entitlement, and a shared cap
// would let a busy one crowd the others out entirely.
const GrantWindowsPerSlot = 3

// ValidateGrantShape resolves the entitlement's meter and applies the shared
// meter/price rules. A no-op for entitlements without a grant config.
func (s *entitlementService) ValidateGrantShape(ctx context.Context, e *entitlement.Entitlement) error {
	if e == nil || !e.HasGrantConfig() || e.FeatureType != types.FeatureTypeMetered {
		return nil
	}
	f, err := s.FeatureRepo.Get(ctx, e.FeatureID)
	if err != nil {
		return err
	}
	m, err := s.MeterRepo.GetMeter(ctx, f.MeterID)
	if err != nil {
		return err
	}
	return s.validateEntitlementGrantShape(ctx, e, m)
}

// takeOverGrantWindowsFromParent runs on a first override: the override replaces the
// plan's rule, so the live window is re-cut by the difference between the two.
func (s *entitlementService) takeOverGrantWindowsFromParent(ctx context.Context, e *entitlement.Entitlement) error {
	parent, err := s.EntitlementRepo.Get(ctx, lo.FromPtr(e.ParentEntitlementID))
	if err != nil {
		return err
	}
	delta := lo.FromPtr(e.GrantQuota).Sub(lo.FromPtr(parent.GrantQuota))
	_, err = NewEntitlementGrantService(s.ServiceParams).ReissueEntitlementGrants(ctx, &dto.ReissueEntitlementGrantsRequest{
		SubscriptionID: e.EntityID,
		FeatureID:      e.FeatureID,
		Delta:          delta,
		Unlimited:      e.IsUnlimitedGrant(),
		At:             time.Now().UTC(),
		Source:         "override_created",
	})
	return err
}

// settleGrantWindowsForDeletedEC hands an override's allowance back to its parent;
// anything else pooled, so the pool re-cuts without it — the addon detach path.
func (s *entitlementService) settleGrantWindowsForDeletedEC(ctx context.Context, e *entitlement.Entitlement) error {
	if e == nil || e.EntityType != types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION || !e.HasGrantConfig() {
		return nil
	}
	if s.EntitlementGrantRepo == nil {
		return nil
	}

	if parentID := lo.FromPtr(e.ParentEntitlementID); parentID != "" {
		parent, err := s.EntitlementRepo.Get(ctx, parentID)
		if err != nil {
			return err
		}
		// A legacy parent has no allowance to hand the window to; it runs out instead.
		if !parent.HasGrantConfig() {
			return nil
		}
		delta := lo.FromPtr(parent.GrantQuota).Sub(lo.FromPtr(e.GrantQuota))
		_, err = NewEntitlementGrantService(s.ServiceParams).ReissueEntitlementGrants(ctx, &dto.ReissueEntitlementGrantsRequest{
			SubscriptionID: e.EntityID,
			FeatureID:      e.FeatureID,
			Delta:          delta,
			Unlimited:      parent.IsUnlimitedGrant(),
			At:             time.Now().UTC(),
			Source:         "override_removed",
		})
		return err
	}

	sub, err := s.SubRepo.Get(ctx, e.EntityID)
	if err != nil {
		return err
	}
	// The addon paths reach this through Resolve, which builds the config from addon
	// sources. A deleted entitlement has no addon behind it, so the config is built
	// here — one entitlement leaving, nothing arriving.
	grantSvc := newSubscriptionGrantService(s.ServiceParams)
	removed := []*entitlement.Entitlement{e}
	surviving, err := grantSvc.resolveSurvivingGrantECs(ctx, sub, removed)
	if err != nil {
		return err
	}

	return grantSvc.applyEntitlementGrantChange(ctx, &GrantChangeConfig{
		sub:                     sub,
		entitlementsToRemove:    removed,
		survivingECsByFeature:   surviving,
		entitlementChangeAt:     time.Now().UTC(),
		entitlementChangeOrigin: grantProrationSourceEntitlementGone,
	})
}
