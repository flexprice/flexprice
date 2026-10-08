package service

import (
	"context"
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

type AddonChangeService interface {
	Execute(ctx context.Context, req AddonChangeRequest) (*addonChangeConfig, *SettleProrationResult, error)

	Preview(ctx context.Context, req AddonChangeRequest) (*addonChangeConfig, *SettleProrationResult, error)

	Resolve(ctx context.Context, req AddonChangeRequest) (*addonChangeConfig, error)

	Persist(ctx context.Context, config *addonChangeConfig) error

	PersistPending(ctx context.Context, config *addonChangeConfig) error

	Settle(ctx context.Context, config *addonChangeConfig, mode SettleMode) (*SettleProrationResult, error)

	ExecutePayFirst(ctx context.Context, req AddonChangeRequest, checkout *dto.CheckoutParams) (*ExecutePayFirstResponse, error)
}

// AddonAdd is one attach in a batch. Existing is set only on a checkout-completion replay,
// where the association already exists as pending and is activated rather than created.
type AddonAdd struct {
	Request  *dto.AddAddonToSubscriptionRequest
	Existing *addonassociation.AddonAssociation
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
	session *dto.CheckoutSessionResponse
	settled *SettleProrationResult
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
	config := &addonChangeConfig{sub: sub}

	// One `now` for the whole change: resolving per entry would give each immediate entry a
	// different timestamp and split one proration pass into several.
	now := time.Now().UTC()

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

func resolveAttachChangeAt(
	req *dto.AddAddonToSubscriptionRequest,
	now, periodEnd time.Time,
) dto.AddAddonToSubscriptionRequest {
	resolved := *req
	if resolved.ChangeAt != nil {
		resolved.StartDate = lo.ToPtr(changeAtDate(*resolved.ChangeAt, now, periodEnd))
		resolved.ChangeAt = nil
	}

	if resolved.StartDate == nil {
		resolved.StartDate = lo.ToPtr(now)
	}

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
	req := GrantChangeRequest{Sub: sub}

	for _, detach := range config.getDetaches() {
		req.Removed = append(req.Removed, GrantSource{
			ChangeType:    grantChangeTypeFor(sub, detach.getEffectiveDate()),
			EffectiveDate: detach.getEffectiveDate(),
			Origin:        grantProrationSourceAddonDetach,
			AddonID:       detach.getAssociation().AddonID,
		})
	}

	for _, attach := range config.getAttaches() {
		req.Incoming = append(req.Incoming, GrantSource{
			ChangeType:    grantChangeTypeFor(sub, attach.getEffectiveDate()),
			EffectiveDate: attach.getEffectiveDate(),
			RequestedDate: attach.getRequestedStart(),
			EndDate:       attach.getAssociation().EndDate,
			Behavior:      attach.getRequest().ProrationBehavior,
			Origin:        grantProrationSourceAddonAttach,
			AddonID:       attach.getRequest().AddonID,
		})
	}

	return req
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

	prorationSvc := NewLineItemProrationService(s.ServiceParams)
	for _, group := range groups {
		summary, err := prorationSvc.Compute(ctx, LineItemProrationRequest{
			Subscription:  config.getSubscription(),
			Entries:       group.entries,
			EffectiveDate: group.effectiveDate,
			Behavior:      types.ProrationBehaviorCreateProrations,
			Reason:        config.getReason(),
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

func (s *addonChangeService) persistAttaches(ctx context.Context, config *addonChangeConfig) error {
	sub := config.getSubscription()

	toCreate := []*addonassociation.AddonAssociation{}
	toActivate := []string{}

	for _, attach := range config.getAttaches() {
		req := attach.getRequest()

		// Overrides mint subscription-scoped prices and repoint the line items at them, so they
		// must land before the line items are written.
		if len(req.OverrideLineItems) > 0 {
			if err := s.sub.ProcessSubscriptionPriceOverrides(
				ctx, sub, req.OverrideLineItems, attach.getLineItems(), attach.getPriceMap(),
			); err != nil {
				return err
			}
		}

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

	return s.SubscriptionLineItemRepo.CreateBulk(ctx, lineItems)
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
//
// Steps 1-4 run under the subscription row lock; the provider call cannot, because it is
// outbound HTTP. That is safe because everything committed before it is inert: pending
// associations are invisible to billing and the draft is not finalized.
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
	)

	subscriptionID := req.Subscription.ID
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		locked, err := s.sub.loadSubscriptionForChange(txCtx, subscriptionID, true)
		if err != nil {
			return err
		}
		req.Subscription = locked

		// Re-checked against the locked row: the first pass ran before the lock, so a
		// subscription cancelled in between would otherwise get a checkout opened on it.
		if err := validateAddonChangeCheckout(req, checkout); err != nil {
			return err
		}

		// Taken under the row lock, so two concurrent payment-gated changes cannot both pass.
		if err := ensureNoPendingCheckoutSession(txCtx, s.ServiceParams, locked.CustomerID, locked.ID); err != nil {
			return err
		}

		resolved, err := s.Resolve(txCtx, req)
		if err != nil {
			return err
		}
		if !resolved.getQuote().NetAmount().GreaterThan(decimal.Zero) {
			return nil
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

		checkoutParams = addonChangeCheckoutParams(resolved)
		if err := checkoutParams.Validate(); err != nil {
			return err
		}

		config, settled = resolved, drafted
		return nil
	})
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, nil
	}

	sub := config.getSubscription()
	session, err := NewCheckoutSessionService(s.ServiceParams).StartPayFirstCheckoutSession(ctx, &dto.PayFirstCheckoutRequest{
		CustomerID: sub.CustomerID,
		Action:     types.CheckoutActionAddAddon,
		Configuration: types.CheckoutConfiguration{
			AddAddonParams: checkoutParams,
		},
		DraftInvoice: &settled.GetDraft().Invoice,
		Checkout:     checkout,
	})
	if err != nil {
		s.archiveGatedChange(ctx, config, settled.GetDraft(), err)
		return nil, err
	}

	return &ExecutePayFirstResponse{config: config, session: session, settled: settled}, nil
}

// addonChangeCheckoutParams records everything completion needs to replay the change without
// trusting execute-time state.
func addonChangeCheckoutParams(config *addonChangeConfig) *types.AddAddonParams {
	params := &types.AddAddonParams{SubscriptionID: config.getSubscription().ID}

	for _, attach := range config.getAttaches() {
		req := attach.getRequest()
		params.Addons = append(params.Addons, types.AddAddonRef{
			AssociationID:     attach.getAssociation().ID,
			AddonID:           req.AddonID,
			Cadence:           req.Cadence,
			ProrationBehavior: req.ProrationBehavior,
			StartDate:         attach.getRequestedStart(),
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
	config *addonChangeConfig,
	draft *dto.InvoiceResponse,
	cause error,
) {
	ids := lo.Map(config.getAttaches(), func(attach *addonAttachParams, _ int) string {
		return attach.getAssociation().ID
	})
	if len(ids) > 0 {
		if err := s.AddonAssociationRepo.DeleteBulk(ctx, ids); err != nil {
			s.Logger.Error(ctx, "failed to archive pending addon associations after pay-first failure",
				"error", err,
				"association_ids", ids,
				"original_error", cause,
			)
		}
	}

	if draft != nil {
		if err := s.InvoiceRepo.Delete(ctx, draft.ID); err != nil {
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
		if len(addReq.OverrideLineItems) > 0 || len(addReq.LineItemCommitments) > 0 {
			return ierr.NewError("override_line_items and line_item_commitments are not supported with checkout").
				WithHint("Send the change without checkout to use price overrides or line item commitments").
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
