package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// errAddonChangeNotACharge is returned from the pay-first transaction so it rolls back instead of committing.
var errAddonChangeNotACharge = errors.New("addon change net is not a charge")

type AddonChangeService interface {
	Execute(ctx context.Context, req AddonChangeRequest) (*addonChangeConfig, *SettleProrationResult, error)

	Preview(ctx context.Context, req AddonChangeRequest) (*addonChangeConfig, *SettleProrationResult, error)

	Resolve(ctx context.Context, req AddonChangeRequest) (*addonChangeConfig, error)

	Persist(ctx context.Context, config *addonChangeConfig) error

	PersistPending(ctx context.Context, config *addonChangeConfig) error

	Settle(ctx context.Context, config *addonChangeConfig, mode SettleMode) (*SettleProrationResult, error)

	ExecutePayFirst(ctx context.Context, req AddonChangeRequest, checkout *dto.CheckoutParams) (*ExecutePayFirstResponse, error)

	// ExecuteGrouped applies changes across a grouped invoicing group and settles one document on the parent.
	ExecuteGrouped(ctx context.Context, parentID string, reqs []AddonChangeRequest) ([]*addonChangeConfig, *SettleProrationResult, error)

	PreviewGrouped(ctx context.Context, parent *subscription.Subscription, reqs []AddonChangeRequest) ([]*addonChangeConfig, *SettleProrationResult, error)

	ExecutePayFirstGrouped(ctx context.Context, parentID string, reqs []AddonChangeRequest, checkout *dto.CheckoutParams) (*ExecutePayFirstResponse, error)
}

// AddonAdd is one attach in a batch. Existing and PriceOverrides are set only on a checkout-completion
// replay, where the association already exists as pending and its override prices were minted at execute.
type AddonAdd struct {
	Request        *dto.AddAddonToSubscriptionRequest
	Existing       *addonassociation.AddonAssociation
	PriceOverrides []types.AddonPriceOverrideRef
}

type AddonChangeRequest struct {
	Subscription *subscription.Subscription
	Adds         []AddonAdd
	Removes      []*dto.RemoveAddonRequest
}

// NewAddonChangeRequest maps a batch modify payload onto the spine's request.
func NewAddonChangeRequest(sub *subscription.Subscription, params *dto.SubModifyBulkAddonParams) AddonChangeRequest {
	return AddonChangeRequest{
		Subscription: sub,
		Adds: lo.Map(params.Adds, func(add *dto.AddAddonToSubscriptionRequest, _ int) AddonAdd {
			return AddonAdd{Request: add}
		}),
		Removes: params.Removes,
	}
}

func (r AddonChangeRequest) Validate() error {
	if r.Subscription == nil {
		return ierr.NewError("subscription is required for an addon change").
			Mark(ierr.ErrValidation)
	}

	if len(r.Adds) == 0 && len(r.Removes) == 0 {
		return ierr.NewError("addon change requires at least one add or remove").
			WithHint("Provide at least one addon to add or remove").
			Mark(ierr.ErrValidation)
	}

	for _, add := range r.Adds {
		if add.Request == nil {
			return ierr.NewError("addon add requires a request").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// ExecutePayFirstResponse is a change waiting on payment: the plan that will apply once the
// customer pays, the session collecting it, and what Settle raised — a draft holding the net.
type ExecutePayFirstResponse struct {
	config  *addonChangeConfig
	configs []*addonChangeConfig
	session *dto.CheckoutSessionResponse
	settled *SettleProrationResult
}

func (r *ExecutePayFirstResponse) getConfigs() []*addonChangeConfig {
	if r == nil {
		return nil
	}
	return r.configs
}

func (r *ExecutePayFirstResponse) getConfig() *addonChangeConfig {
	if r == nil {
		return nil
	}
	return r.config
}

func (r *ExecutePayFirstResponse) getSession() *dto.CheckoutSessionResponse {
	if r == nil {
		return nil
	}
	return r.session
}

func (r *ExecutePayFirstResponse) getSettled() *SettleProrationResult {
	if r == nil {
		return nil
	}
	return r.settled
}

type addonChangeConfig struct {
	sub      *subscription.Subscription
	attaches []*addonAttachParams
	detaches []*addonDetachParams

	grants *GrantChangeConfig

	quote *LineItemProrationSummary

	periodStart time.Time
	idemKey     string
	reason      string
}

func (c *addonChangeConfig) getSubscription() *subscription.Subscription {
	if c == nil {
		return nil
	}
	return c.sub
}

func (c *addonChangeConfig) getAttaches() []*addonAttachParams {
	if c == nil {
		return nil
	}
	return c.attaches
}

func (c *addonChangeConfig) getDetaches() []*addonDetachParams {
	if c == nil {
		return nil
	}
	return c.detaches
}

func (c *addonChangeConfig) getGrants() *GrantChangeConfig {
	if c == nil {
		return nil
	}
	return c.grants
}

func (c *addonChangeConfig) getQuote() *LineItemProrationSummary {
	if c == nil {
		return nil
	}
	return c.quote
}

func (c *addonChangeConfig) getPeriodStart() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.periodStart
}

func (c *addonChangeConfig) getIdempotencyKey() string {
	if c == nil {
		return ""
	}
	return c.idemKey
}

// hasPriceOverrides reports whether any attach mints subscription-scoped prices during Persist.
func (c *addonChangeConfig) hasPriceOverrides() bool {
	for _, attach := range c.getAttaches() {
		if len(attach.getRequest().OverrideLineItems) > 0 {
			return true
		}
	}

	return false
}

func (c *addonChangeConfig) getReason() string {
	if c == nil {
		return ""
	}
	return c.reason
}

type addonChangeService struct {
	ServiceParams
	sub *subscriptionService
}

func NewAddonChangeService(params ServiceParams) AddonChangeService {
	return &addonChangeService{
		ServiceParams: params,
		sub:           &subscriptionService{ServiceParams: params},
	}
}

func (s *addonChangeService) Resolve(ctx context.Context, req AddonChangeRequest) (*addonChangeConfig, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	sub := req.Subscription
	if sub.SubscriptionStatus == types.SubscriptionStatusPaused {
		return nil, ierr.NewError("cannot change addons on a paused subscription").
			WithHint("Resume the subscription before changing its addons").
			WithReportableDetails(map[string]any{
				"subscription_id": sub.ID,
			}).
			Mark(ierr.ErrValidation)
	}
	config := &addonChangeConfig{sub: sub}

	// One `now` for the whole change: resolving per entry would give each immediate entry a
	// different timestamp and split one proration pass into several. It is cut to Postgres'
	// microseconds, so the commitment guard sees the stored dates it compares against.
	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, remove := range req.Removes {
		removeReq := resolveRemoveChangeAt(remove, now, sub.CurrentPeriodEnd)

		params, err := s.sub.createAddonDetachParams(ctx, sub, &removeReq)
		if err != nil {
			return nil, err
		}

		config.detaches = append(config.detaches, params)
		if config.reason == "" {
			config.reason = params.getReason()
		}
	}

	// Each attach validates its commitments against the line items resolved before it, so a
	// batch is checked as the shape it will leave behind rather than one addon at a time.
	originalLineItems := sub.LineItems
	for _, add := range req.Adds {
		addReq := resolveAttachChangeAt(add.Request, now, sub.CurrentPeriodEnd)

		params, err := s.sub.createAddonAttachParams(ctx, sub, &addReq, add.Existing)
		if err != nil {
			sub.LineItems = originalLineItems
			return nil, err
		}

		refs := addReq.CouponRefs
		if refs == nil {
			priceIDs := lo.Map(params.getLineItems(), func(li *subscription.SubscriptionLineItem, _ int) string { return li.PriceID })
			refs, err = NewCouponAssociationService(s.ServiceParams).ResolveCouponRefs(ctx, sub, addReq.Coupons, params.getRequestedStart(), priceIDs)
			if err != nil {
				sub.LineItems = originalLineItems
				return nil, err
			}
		}

		// coupon to line item mapping
		params.coupons = map[string][]types.CouponRef{}
		for _, ref := range refs {
			if li, ok := lo.Find(params.getLineItems(), func(li *subscription.SubscriptionLineItem) bool { return li.PriceID == lo.FromPtr(ref.PriceID) }); ok {
				params.coupons[li.ID] = append(params.coupons[li.ID], ref)
			}
		}

		if err := applyPriceOverrideRefs(params, add.PriceOverrides); err != nil {
			sub.LineItems = originalLineItems
			return nil, err
		}

		config.attaches = append(config.attaches, params)
		sub.LineItems = lo.Flatten([][]*subscription.SubscriptionLineItem{sub.LineItems, params.getLineItems()})
	}
	sub.LineItems = originalLineItems

	grants, err := newSubscriptionGrantService(s.ServiceParams).Resolve(ctx, s.grantChangeRequest(config))
	if err != nil {
		return nil, err
	}
	config.grants = grants

	if err := s.quote(ctx, config); err != nil {
		return nil, err
	}

	return config, nil
}

// applyPriceOverrideRefs repoints a replayed attach's line items at the override prices minted at execute.
func applyPriceOverrideRefs(attach *addonAttachParams, refs []types.AddonPriceOverrideRef) error {
	for _, ref := range refs {
		lineItem, ok := lo.Find(attach.getLineItems(), func(li *subscription.SubscriptionLineItem) bool {
			return li.PriceID == ref.PriceID
		})
		if !ok {
			return ierr.NewError("addon price named by the checkout override no longer exists").
				WithHint("The addon's prices changed while its checkout was outstanding").
				WithReportableDetails(map[string]any{
					"association_id": attach.getAssociation().ID,
					"price_id":       ref.PriceID,
				}).
				Mark(ierr.ErrValidation)
		}

		lineItem.PriceID = ref.OverridePriceID
		if ref.Quantity != nil {
			lineItem.Quantity = *ref.Quantity
		}
	}

	return nil
}

func resolveAttachChangeAt(
	req *dto.AddAddonToSubscriptionRequest,
	now, periodEnd time.Time,
) dto.AddAddonToSubscriptionRequest {
	resolved := *req
	if resolved.ChangeAt != nil {
		resolved.StartDate = lo.ToPtr(changeAtDate(*resolved.ChangeAt, now, periodEnd))
		resolved.ChangeAt = nil
	}

	// A caller's start_date is cut to microseconds as `now` is, so the stored start is the one the guard checks.
	resolved.StartDate = lo.ToPtr(lo.FromPtrOr(resolved.StartDate, now).Truncate(time.Microsecond))
	return resolved
}

func resolveRemoveChangeAt(
	req *dto.RemoveAddonRequest,
	now, periodEnd time.Time,
) dto.RemoveAddonRequest {
	resolved := *req
	if resolved.ChangeAt != nil {
		resolved.EffectiveDate = lo.ToPtr(changeAtDate(*resolved.ChangeAt, now, periodEnd))
		resolved.ChangeAt = nil
	}

	return resolved
}

func changeAtDate(changeAt types.ScheduleType, now, periodEnd time.Time) time.Time {
	if changeAt == types.ScheduleTypePeriodEnd {
		return periodEnd
	}

	return now
}

func (s *addonChangeService) grantChangeRequest(config *addonChangeConfig) GrantChangeRequest {
	sub := config.getSubscription()
	grantReq := GrantChangeRequest{Sub: sub}

	for _, detach := range config.getDetaches() {
		grantReq.Removed = append(grantReq.Removed, GrantSource{
			ChangeType:    grantChangeTypeFor(sub, detach.getEffectiveDate()),
			EffectiveDate: detach.getEffectiveDate(),
			Origin:        grantProrationSourceAddonDetach,
			AddonID:       detach.getAssociation().AddonID,
		})
	}

	for _, attach := range config.getAttaches() {
		req := attach.getRequest()
		grantReq.Incoming = append(grantReq.Incoming, GrantSource{
			ChangeType:        grantChangeTypeFor(sub, attach.getEffectiveDate()),
			EffectiveDate:     attach.getEffectiveDate(),
			RequestedDate:     attach.getRequestedStart(),
			EndDate:           attach.getAssociation().EndDate,
			ProrationSettings: req.ProrationSettings.WithDefault(req.ProrationBehavior),
			Origin:            grantProrationSourceAddonAttach,
			AddonID:           req.AddonID,
		})
	}

	return grantReq
}

// prorationGroup is the batch's entries sharing one effective date: proration is priced against
// the remaining period, so entries landing on different dates cannot be computed together.
type prorationGroup struct {
	effectiveDate time.Time
	entries       []LineItemProrationEntry
}

func (s *addonChangeService) quote(ctx context.Context, config *addonChangeConfig) error {
	groups, err := s.prorationGroups(ctx, config)
	if err != nil {
		return err
	}

	config.quote = emptyProrationSummary(config.getSubscription())
	if len(groups) == 0 {
		config.periodStart = config.getSubscription().CurrentPeriodEnd
		return nil
	}

	config.periodStart = groups[0].effectiveDate

	var coupons []dto.InvoiceLineItemCoupon
	for _, attach := range config.getAttaches() {
		for _, li := range attach.getLineItems() {
			for _, ref := range attach.getCoupons()[li.ID] {
				coupons = append(coupons, dto.InvoiceLineItemCoupon{
					LineItemID:             li.PriceID,
					SubscriptionLineItemID: lo.ToPtr(li.ID),
					CouponID:               ref.CouponID,
					CouponAssociationID:    lo.ToPtr(ref.AssociationID),
				})
			}
		}
	}

	prorationSvc := NewLineItemProrationService(s.ServiceParams)
	for _, group := range groups {
		summary, err := prorationSvc.Compute(ctx, LineItemProrationRequest{
			Subscription:  config.getSubscription(),
			Entries:       group.entries,
			EffectiveDate: group.effectiveDate,
			Behavior:      types.ProrationBehaviorCreateProrations,
			Reason:        config.getReason(),
			Coupons:       coupons,
		})
		if err != nil {
			return err
		}

		config.quote.Merge(summary)
	}

	config.idemKey = s.idempotencyKey(config)
	return nil
}

func (s *addonChangeService) prorationGroups(ctx context.Context, config *addonChangeConfig) ([]prorationGroup, error) {
	byDate := map[int64]*prorationGroup{}

	add := func(effectiveDate time.Time, entries []LineItemProrationEntry) {
		key := effectiveDate.UTC().UnixNano()
		if byDate[key] == nil {
			byDate[key] = &prorationGroup{effectiveDate: effectiveDate}
		}
		byDate[key].entries = append(byDate[key].entries, entries...)
	}

	for _, detach := range config.getDetaches() {
		if detach.getBehavior() != types.ProrationBehaviorCreateProrations {
			continue
		}

		entries, err := s.sub.buildAddonProrationEntries(ctx, detach.getLineItems(), types.ProrationActionRemoveItem)
		if err != nil {
			return nil, err
		}
		add(detach.getEffectiveDate(), entries)
	}

	for _, attach := range config.getAttaches() {
		if attach.getRequest().ProrationBehavior != types.ProrationBehaviorCreateProrations {
			continue
		}

		entries, err := s.sub.buildAddonProrationEntries(ctx, attach.getLineItems(), types.ProrationActionAddItem)
		if err != nil {
			return nil, err
		}
		add(attach.getEffectiveDate(), entries)
	}

	groups := lo.Values(byDate)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].effectiveDate.Before(groups[j].effectiveDate)
	})

	return lo.Map(groups, func(g *prorationGroup, _ int) prorationGroup { return *g }), nil
}

// idempotencyKey reproduces the per-path keys the single-addon flows stamp today: a removal-only
// change credits the wallet under its raw key, everything else hashes into a charge key.
func (s *addonChangeService) idempotencyKey(config *addonChangeConfig) string {
	if len(config.getAttaches()) == 0 && len(config.getDetaches()) == 1 {
		return config.getDetaches()[0].prorationIdempotencyKey()
	}

	sources := make([]string, 0, len(config.getAttaches())+len(config.getDetaches()))
	for _, attach := range config.getAttaches() {
		sources = append(sources, attach.prorationIdempotencyKey())
	}
	for _, detach := range config.getDetaches() {
		sources = append(sources, detach.prorationIdempotencyKey())
	}
	sort.Strings(sources)

	return prorationChargeInvoiceKey(LineItemProrationRequest{
		Subscription:   config.getSubscription(),
		EffectiveDate:  config.getPeriodStart(),
		IdempotencyKey: strings.Join(sources, ","),
	})
}

// Persist writes the resolved config and raises no money. It opens no transaction of its own:
// the caller's must already be open, so the whole batch commits or rolls back together.
func (s *addonChangeService) Persist(ctx context.Context, config *addonChangeConfig) error {
	if config == nil {
		return ierr.NewError("addon change config is required").
			Mark(ierr.ErrValidation)
	}

	// Removals run first so a swap frees its entitlement slot before the additions claim it.
	if err := s.persistRemovals(ctx, config); err != nil {
		return err
	}

	if err := s.persistAttaches(ctx, config); err != nil {
		return err
	}

	// Checked once the batch's own attaches exist, so a swap's incoming line keeps a commitment's scope covered.
	if err := s.checkRemovalCommitments(ctx, config); err != nil {
		return err
	}

	// Overrides repoint line items at prices that did not exist when Resolve quoted, so the
	// quote has to be retaken against what will actually be billed.
	if config.hasPriceOverrides() {
		if err := s.quote(ctx, config); err != nil {
			return err
		}
	}

	return newSubscriptionGrantService(s.ServiceParams).Apply(ctx, config.getGrants())
}

// PersistPending writes the batch's attaches as pending associations and nothing else — no line
// items, no grants, and no removals — so the subscription keeps billing exactly as it did until
// payment lands.
func (s *addonChangeService) PersistPending(ctx context.Context, config *addonChangeConfig) error {
	if config == nil {
		return ierr.NewError("addon change config is required").
			Mark(ierr.ErrValidation)
	}

	associations := make([]*addonassociation.AddonAssociation, 0, len(config.getAttaches()))
	for _, attach := range config.getAttaches() {
		association := attach.getAssociation()
		association.AddonStatus = types.AddonStatusPending
		associations = append(associations, association)
	}

	return s.AddonAssociationRepo.CreateBulk(ctx, associations)
}

func (s *addonChangeService) persistRemovals(ctx context.Context, config *addonChangeConfig) error {
	// Each removal carries its own date and reason, so cancellation batches by both.
	type cancellation struct {
		effectiveDate time.Time
		reason        string
	}

	idsByCancellation := map[cancellation][]string{}
	for _, detach := range config.getDetaches() {
		key := cancellation{effectiveDate: detach.getEffectiveDate(), reason: detach.getReason()}
		idsByCancellation[key] = append(idsByCancellation[key], detach.getAssociation().ID)
	}

	for key, ids := range idsByCancellation {
		if err := s.AddonAssociationRepo.CancelBulk(ctx, ids, key.effectiveDate, key.reason); err != nil {
			return err
		}
	}

	for _, detach := range config.getDetaches() {
		detach.association = addonassociation.NewAddonAssociationBuilder(detach.getAssociation()).
			WithCancellation(detach.getEffectiveDate(), detach.getReason()).
			Build()

		deleteReq := dto.DeleteSubscriptionLineItemRequest{EffectiveFrom: lo.ToPtr(detach.getEffectiveDate())}
		for _, lineItem := range detach.getLineItems() {
			if _, err := s.sub.deleteSubscriptionLineItem(ctx, lineItem.ID, deleteReq); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *addonChangeService) checkRemovalCommitments(ctx context.Context, config *addonChangeConfig) error {
	for _, detach := range config.getDetaches() {
		for _, lineItem := range detach.getLineItems() {
			if err := checkLineItemCommitmentDetach(ctx, s.ServiceParams, config.getSubscription(), lineItem, detach.getEffectiveDate()); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *addonChangeService) persistAttaches(ctx context.Context, config *addonChangeConfig) error {
	sub := config.getSubscription()

	toCreate := []*addonassociation.AddonAssociation{}
	toActivate := []string{}

	// Overrides repoint the line items at new prices, so they must land before the line items are written.
	if _, err := s.applyPriceOverrides(ctx, config); err != nil {
		return err
	}

	for _, attach := range config.getAttaches() {
		association := attach.getAssociation()
		if attach.isReplayAttach() {
			association.AddonStatus = types.AddonStatusActive
			toActivate = append(toActivate, association.ID)
			continue
		}

		toCreate = append(toCreate, association)
	}

	if err := s.AddonAssociationRepo.CreateBulk(ctx, toCreate); err != nil {
		return err
	}

	if err := s.AddonAssociationRepo.ActivateBulk(ctx, toActivate); err != nil {
		return err
	}

	lineItems := make([]*subscription.SubscriptionLineItem, 0, len(config.getAttaches()))
	for _, attach := range config.getAttaches() {
		if err := s.sub.createBucketPricesForLineItems(ctx, sub, attach.getLineItems(), attach.getBucketCfgs()); err != nil {
			return err
		}
		lineItems = append(lineItems, attach.getLineItems()...)
	}

	for _, li := range lineItems {
		if err := checkLineItemCommitmentAttach(ctx, s.ServiceParams, sub, li); err != nil {
			return err
		}
	}

	if err := s.SubscriptionLineItemRepo.CreateBulk(ctx, lineItems); err != nil {
		return err
	}

	couponSvc := NewCouponAssociationService(s.ServiceParams)
	for _, attach := range config.getAttaches() {
		for lineItemID, refs := range attach.getCoupons() {
			if err := couponSvc.CreateCouponRefAssociations(ctx, sub, refs, lineItemID); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyPriceOverrides mints each attach's override prices and repoints its line items at them.
// Returns the swaps keyed by association ID.
func (s *addonChangeService) applyPriceOverrides(
	ctx context.Context,
	config *addonChangeConfig,
) (map[string][]types.AddonPriceOverrideRef, error) {
	refs := map[string][]types.AddonPriceOverrideRef{}

	for _, attach := range config.getAttaches() {
		overrides := attach.getRequest().OverrideLineItems
		if len(overrides) == 0 {
			continue
		}

		lineItems := attach.getLineItems()
		before := lo.Map(lineItems, func(li *subscription.SubscriptionLineItem, _ int) subscription.SubscriptionLineItem {
			return *li
		})

		if err := s.sub.ProcessSubscriptionPriceOverrides(
			ctx, config.getSubscription(), overrides, lineItems, attach.getPriceMap(),
		); err != nil {
			return nil, err
		}

		for i, li := range lineItems {
			if li.PriceID == before[i].PriceID {
				continue
			}
			ref := types.AddonPriceOverrideRef{PriceID: before[i].PriceID, OverridePriceID: li.PriceID}
			if !li.Quantity.Equal(before[i].Quantity) {
				ref.Quantity = lo.ToPtr(li.Quantity)
			}
			associationID := attach.getAssociation().ID
			refs[associationID] = append(refs[associationID], ref)
		}
	}

	return refs, nil
}

// Settle raises the one netted document the batch owes: an invoice when the net is a charge,
// a wallet credit when it is a refund, nothing when it is zero.
func (s *addonChangeService) Settle(
	ctx context.Context,
	config *addonChangeConfig,
	mode SettleMode,
) (*SettleProrationResult, error) {
	if config == nil {
		return nil, ierr.NewError("addon change config is required").
			Mark(ierr.ErrValidation)
	}

	sub := config.getSubscription()

	// A preview writes nothing, so it carries no key to deduplicate against.
	idempotencyKey := config.getIdempotencyKey()
	if mode == SettleModePreview {
		idempotencyKey = ""
	}

	req := NewSettleProrationRequest(
		sub, config.getQuote(), config.getPeriodStart(), sub.CurrentPeriodEnd,
		"Subscription update", idempotencyKey, mode,
	)
	req.Reason = config.getReason()

	return NewLineItemProrationService(s.ServiceParams).Settle(ctx, req)
}

// Execute applies the batch as one transaction with the subscription row locked as its first
// statement, so concurrent changes serialise and a settlement failure rolls the change back.
func (s *addonChangeService) Execute(
	ctx context.Context,
	req AddonChangeRequest,
) (*addonChangeConfig, *SettleProrationResult, error) {
	if err := req.Validate(); err != nil {
		return nil, nil, err
	}

	var (
		config  *addonChangeConfig
		settled *SettleProrationResult
	)

	subscriptionID := req.Subscription.ID
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		locked, err := s.sub.loadSubscriptionForChange(ctx, subscriptionID, true)
		if err != nil {
			return err
		}
		req.Subscription = locked

		if config, err = s.Resolve(ctx, req); err != nil {
			return err
		}

		if err := s.Persist(ctx, config); err != nil {
			return err
		}

		settled, err = s.Settle(ctx, config, SettleModeIssue)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	attemptProrationPayments(ctx, s.ServiceParams, settled.GetChanged())

	return config, settled, nil
}

// ExecutePayFirst writes the change's pending half and locks its net on a draft invoice, then
// opens a checkout for it. Returns a nil config when the net is not a charge — there is nothing
// to collect, so the caller applies the change immediately instead.
func (s *addonChangeService) ExecutePayFirst(
	ctx context.Context,
	req AddonChangeRequest,
	checkout *dto.CheckoutParams,
) (*ExecutePayFirstResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := validateAddonChangeCheckout(req, checkout); err != nil {
		return nil, err
	}

	var (
		config         *addonChangeConfig
		settled        *SettleProrationResult
		checkoutParams *types.AddAddonParams
		payerID        string
	)

	subscriptionID := req.Subscription.ID
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		lockedSubs, err := s.sub.loadSubscriptionForChange(txCtx, subscriptionID, true)
		if err != nil {
			return err
		}
		req.Subscription = lockedSubs

		// Re-checked against the locked row: the first pass ran before the lock, so a
		// subscription cancelled in between would otherwise get a checkout opened on it.
		if err := validateAddonChangeCheckout(req, checkout); err != nil {
			return err
		}

		payerID, err = checkoutPayerID(txCtx, s.ServiceParams, lockedSubs)
		if err != nil {
			return err
		}

		// Taken under the row lock, so two concurrent payment-gated changes cannot both pass.
		if err := ensureNoPendingCheckoutSession(txCtx, s.ServiceParams, lockedSubs.ID); err != nil {
			return err
		}

		resolved, overrides, err := s.resolvePayFirst(txCtx, req)
		if err != nil {
			return err
		}
		// Not a charge: the error keeps the transaction from committing the override prices just minted.
		if !resolved.getQuote().NetAmount().GreaterThan(decimal.Zero) {
			return errAddonChangeNotACharge
		}

		// Attaches only: applying a removal now would end line items before payment, and
		// cancelling the checkout would strand the customer on the cheaper state.
		if err := s.PersistPending(txCtx, resolved); err != nil {
			return err
		}

		// The draft locks exactly what pay-later would have billed.
		drafted, err := s.Settle(txCtx, resolved, SettleModeDraft)
		if err != nil {
			return err
		}

		checkoutParams = addonChangeCheckoutParams(resolved, overrides)
		if err := checkoutParams.Validate(); err != nil {
			return err
		}

		config, settled = resolved, drafted
		return nil
	})
	if errors.Is(err, errAddonChangeNotACharge) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	session, err := NewCheckoutSessionService(s.ServiceParams).StartPayFirstCheckoutSession(ctx, &dto.PayFirstCheckoutRequest{
		CustomerID: payerID,
		Action:     types.CheckoutActionAddAddon,
		Configuration: types.CheckoutConfiguration{
			AddAddonParams: checkoutParams,
		},
		DraftInvoice: &settled.GetDraft().Invoice,
		Checkout:     checkout,
	})
	if err != nil {
		s.archiveGatedChange(ctx, checkoutParams, settled.GetDraft(), err)
		return nil, err
	}

	return &ExecutePayFirstResponse{config: config, session: session, settled: settled}, nil
}

// resolvePayFirst resolves a gated change and, when it carries overrides, mints them and re-quotes
// so the draft bills the override prices.
func (s *addonChangeService) resolvePayFirst(
	ctx context.Context,
	req AddonChangeRequest,
) (*addonChangeConfig, map[string][]types.AddonPriceOverrideRef, error) {
	config, err := s.Resolve(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !config.hasPriceOverrides() {
		return config, nil, nil
	}

	overrides, err := s.applyPriceOverrides(ctx, config)
	if err != nil {
		return nil, nil, err
	}
	if err := s.quote(ctx, config); err != nil {
		return nil, nil, err
	}

	return config, overrides, nil
}

// addonChangeCheckoutParams records everything completion needs to replay the change without
// trusting execute-time state.
func addonChangeCheckoutParams(
	config *addonChangeConfig,
	overrides map[string][]types.AddonPriceOverrideRef,
) *types.AddAddonParams {
	params := &types.AddAddonParams{SubscriptionID: config.getSubscription().ID}

	for _, attach := range config.getAttaches() {
		req := attach.getRequest()
		params.Addons = append(params.Addons, types.AddAddonRef{
			AssociationID:     attach.getAssociation().ID,
			AddonID:           req.AddonID,
			Cadence:           req.Cadence,
			ProrationBehavior: req.ProrationBehavior,
			StartDate:         attach.getRequestedStart(),
			Coupons:           lo.Flatten(lo.Values(attach.getCoupons())),
			PriceOverrides:    overrides[attach.getAssociation().ID],
		})
	}

	for _, detach := range config.getDetaches() {
		params.Removes = append(params.Removes, types.RemoveAddonRef{
			AssociationID:     detach.getAssociation().ID,
			Reason:            detach.getReason(),
			ProrationBehavior: detach.getBehavior(),
			EffectiveDate:     detach.getEffectiveDate(),
		})
	}

	return params
}

// archiveGatedChange undoes the inert state a failed provider call left behind. The associations
// never activated, so they are archived rather than cancelled.
func (s *addonChangeService) archiveGatedChange(
	ctx context.Context,
	params *types.AddAddonParams,
	draft *dto.InvoiceResponse,
	cause error,
) {
	ids := lo.Map(params.Addons, func(ref types.AddAddonRef, _ int) string { return ref.AssociationID })
	if len(ids) > 0 {
		if err := s.AddonAssociationRepo.DeleteBulk(ctx, ids); err != nil {
			s.Logger.Error(ctx, "failed to archive pending addon associations after pay-first failure",
				"error", err,
				"association_ids", ids,
				"original_error", cause,
			)
		}
	}

	priceIDs := lo.FlatMap(params.Addons, func(ref types.AddAddonRef, _ int) []string { return ref.OverridePriceIDs() })
	if len(priceIDs) > 0 {
		if err := s.PriceRepo.DeleteBulk(ctx, priceIDs); err != nil {
			s.Logger.Error(ctx, "failed to delete override prices after pay-first failure",
				"error", err,
				"price_ids", priceIDs,
				"original_error", cause,
			)
		}
	}

	if draft != nil {
		if err := archiveDraftInvoice(ctx, s.ServiceParams, draft.ID); err != nil {
			s.Logger.Error(ctx, "failed to archive draft invoice after pay-first failure",
				"error", err,
				"invoice_id", draft.ID,
				"original_error", cause,
			)
		}
	}
}

func validateAddonChangeCheckout(req AddonChangeRequest, checkout *dto.CheckoutParams) error {
	if checkout == nil {
		return ierr.NewError("payment-gated addon change requires checkout params").
			Mark(ierr.ErrValidation)
	}
	if err := checkout.Validate(); err != nil {
		return err
	}

	sub := req.Subscription
	for _, add := range req.Adds {
		addReq := add.Request
		if len(addReq.LineItemCommitments) > 0 {
			return ierr.NewError("line_item_commitments is not supported with checkout").
				WithHint("Send the change without checkout to use line item commitments").
				WithReportableDetails(map[string]any{
					"subscription_id": sub.ID,
					"addon_id":        addReq.AddonID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return ierr.NewError("subscription status does not allow a payment-gated addon change").
			WithHint("Checkout is only supported for active subscriptions").
			WithReportableDetails(map[string]any{
				"subscription_id":     sub.ID,
				"subscription_status": sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *addonChangeService) Preview(
	ctx context.Context,
	req AddonChangeRequest,
) (*addonChangeConfig, *SettleProrationResult, error) {
	config, err := s.Resolve(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	settled, err := s.Settle(ctx, config, SettleModePreview)
	if err != nil {
		return nil, nil, err
	}

	return config, settled, nil
}

func (s *addonChangeService) ExecuteGrouped(
	ctx context.Context,
	parentID string,
	reqs []AddonChangeRequest,
) ([]*addonChangeConfig, *SettleProrationResult, error) {
	var (
		configs []*addonChangeConfig
		settled *SettleProrationResult
	)

	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		parent, err := s.lockGroupedTargets(ctx, parentID, reqs)
		if err != nil {
			return err
		}

		configs = make([]*addonChangeConfig, 0, len(reqs))
		for _, req := range reqs {
			config, err := s.Resolve(ctx, req)
			if err != nil {
				return err
			}
			if err := s.Persist(ctx, config); err != nil {
				return err
			}
			configs = append(configs, config)
		}

		// Merged only after Persist, which re-quotes a change with price overrides.
		settled, err = s.settleGrouped(ctx, parent, configs, SettleModeIssue)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	attemptProrationPayments(ctx, s.ServiceParams, settled.GetChanged())

	return configs, settled, nil
}

// ExecutePayFirstGrouped is ExecutePayFirst across a grouped invoicing group: one draft on the
// parent and one checkout for the parent's invoicing customer. Returns nil when the net is not a charge.
func (s *addonChangeService) ExecutePayFirstGrouped(
	ctx context.Context,
	parentID string,
	reqs []AddonChangeRequest,
	checkout *dto.CheckoutParams,
) (*ExecutePayFirstResponse, error) {
	for _, req := range reqs {
		if err := validateAddonChangeCheckout(req, checkout); err != nil {
			return nil, err
		}
	}

	var (
		configs []*addonChangeConfig
		settled *SettleProrationResult
		batch   []*types.AddAddonParams
		payerID string
	)

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		parent, err := s.lockGroupedTargets(txCtx, parentID, reqs)
		if err != nil {
			return err
		}
		payerID = parent.GetInvoicingCustomerID()

		resolved := make([]*addonChangeConfig, 0, len(reqs))
		overrides := map[string][]types.AddonPriceOverrideRef{}
		net := decimal.Zero
		for _, req := range reqs {
			if err := validateAddonChangeCheckout(req, checkout); err != nil {
				return err
			}
			if err := ensureNoPendingCheckoutSession(txCtx, s.ServiceParams, req.Subscription.ID); err != nil {
				return err
			}

			config, configOverrides, err := s.resolvePayFirst(txCtx, req)
			if err != nil {
				return err
			}
			net = net.Add(config.getQuote().NetAmount())
			resolved = append(resolved, config)
			// Association IDs are unique across the group, so one map serves every entry.
			overrides = lo.Assign(overrides, configOverrides)
		}
		// Not a charge: the error keeps the transaction from committing the override prices just minted.
		if !net.GreaterThan(decimal.Zero) {
			return errAddonChangeNotACharge
		}

		for _, config := range resolved {
			if err := s.PersistPending(txCtx, config); err != nil {
				return err
			}
		}

		drafted, err := s.settleGrouped(txCtx, parent, resolved, SettleModeDraft)
		if err != nil {
			return err
		}

		batch = lo.Map(resolved, func(config *addonChangeConfig, _ int) *types.AddAddonParams {
			return addonChangeCheckoutParams(config, overrides)
		})
		// Parent first, matching lockGroupedTargets, so completion locks in the same order.
		sort.SliceStable(batch, func(i, j int) bool { return batch[i].SubscriptionID == parent.ID })
		if err := (&types.CheckoutConfiguration{AddAddonBatchParams: batch}).Validate(types.CheckoutActionAddAddon); err != nil {
			return err
		}

		configs, settled = resolved, drafted
		return nil
	})
	if errors.Is(err, errAddonChangeNotACharge) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	session, err := NewCheckoutSessionService(s.ServiceParams).StartPayFirstCheckoutSession(ctx, &dto.PayFirstCheckoutRequest{
		CustomerID: payerID,
		Action:     types.CheckoutActionAddAddon,
		Configuration: types.CheckoutConfiguration{
			AddAddonBatchParams: batch,
		},
		DraftInvoice: &settled.GetDraft().Invoice,
		Checkout:     checkout,
	})
	if err != nil {
		draft := settled.GetDraft()
		for _, params := range batch {
			s.archiveGatedChange(ctx, params, draft, err)
			draft = nil
		}
		return nil, err
	}

	return &ExecutePayFirstResponse{configs: configs, session: session, settled: settled}, nil
}

func (s *addonChangeService) PreviewGrouped(
	ctx context.Context,
	parent *subscription.Subscription,
	reqs []AddonChangeRequest,
) ([]*addonChangeConfig, *SettleProrationResult, error) {
	configs := make([]*addonChangeConfig, 0, len(reqs))
	for _, req := range reqs {
		if err := validateGroupedChangeTarget(parent, req.Subscription); err != nil {
			return nil, nil, err
		}

		config, err := s.Resolve(ctx, req)
		if err != nil {
			return nil, nil, err
		}
		configs = append(configs, config)
	}

	settled, err := s.settleGrouped(ctx, parent, configs, SettleModePreview)
	if err != nil {
		return nil, nil, err
	}

	return configs, settled, nil
}

// lockGroupedTargets locks the parent, then the other targets in ID order so concurrent group changes cannot deadlock.
func (s *addonChangeService) lockGroupedTargets(
	ctx context.Context,
	parentID string,
	reqs []AddonChangeRequest,
) (*subscription.Subscription, error) {
	parent, err := s.sub.loadSubscriptionForChange(ctx, parentID, true)
	if err != nil {
		return nil, err
	}

	sort.Slice(reqs, func(i, j int) bool { return reqs[i].Subscription.ID < reqs[j].Subscription.ID })
	for i := range reqs {
		locked := parent
		if reqs[i].Subscription.ID != parentID {
			if locked, err = s.sub.loadSubscriptionForChange(ctx, reqs[i].Subscription.ID, true); err != nil {
				return nil, err
			}
		}
		if err := validateGroupedChangeTarget(parent, locked); err != nil {
			return nil, err
		}
		reqs[i].Subscription = locked
	}

	return parent, nil
}

// settleGrouped nets every target's quote into one document on the parent.
func (s *addonChangeService) settleGrouped(
	ctx context.Context,
	parent *subscription.Subscription,
	configs []*addonChangeConfig,
	mode SettleMode,
) (*SettleProrationResult, error) {
	merged := emptyProrationSummary(parent)
	periodStart := parent.CurrentPeriodEnd
	keys := make([]string, 0, len(configs))
	reason := ""

	for _, config := range configs {
		merged.Merge(config.getQuote())
		if config.getPeriodStart().Before(periodStart) {
			periodStart = config.getPeriodStart()
		}
		keys = append(keys, config.getIdempotencyKey())
		if reason == "" {
			reason = config.getReason()
		}
	}
	sort.Strings(keys)

	idempotencyKey := ""
	if mode != SettleModePreview {
		idempotencyKey = prorationChargeInvoiceKey(LineItemProrationRequest{
			Subscription:   parent,
			EffectiveDate:  periodStart,
			IdempotencyKey: strings.Join(keys, ","),
		})
	}

	req := NewSettleProrationRequest(
		parent, merged, periodStart, parent.CurrentPeriodEnd,
		"Subscription update", idempotencyKey, mode,
	)
	req.Reason = reason

	return NewLineItemProrationService(s.ServiceParams).Settle(ctx, req)
}
