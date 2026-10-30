package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CommitmentService manages first-class subscription commitments.
type CommitmentService interface {
	Preview(ctx context.Context, req dto.CreateCommitmentRequest) (*dto.CommitmentResponse, error)
	Create(ctx context.Context, req dto.CreateCommitmentRequest) (*dto.CommitmentResponse, error)
	Get(ctx context.Context, id string) (*dto.CommitmentResponse, error)
	List(ctx context.Context, filter *commitment.Filter) (*dto.ListCommitmentsResponse, error)
	Update(ctx context.Context, id string, req dto.UpdateCommitmentRequest) (*dto.CommitmentResponse, error)
	Terminate(ctx context.Context, id string, req dto.TerminateCommitmentRequest) (*dto.CommitmentResponse, error)
	TerminateForSubscription(ctx context.Context, subscriptionID string, effectiveAt time.Time) error
	IsEnabled(ctx context.Context) (bool, error)
	ActiveForPeriod(ctx context.Context, subscriptionID string, from, to time.Time) ([]*commitment.Commitment, error)
	// CheckPriceAttach rejects a usage price, billed from `at` until `until` (nil when open-ended), that would join a
	// commitment's scope but break its rules: a single-meter commitment gaining a second meter, a bucket-size mismatch,
	// or overlapping commitments that disagree.
	CheckPriceAttach(ctx context.Context, sub *subscription.Subscription, p *price.Price, at time.Time, until *time.Time) error
}

type commitmentService struct {
	ServiceParams
}

func NewCommitmentService(params ServiceParams) CommitmentService {
	return newCommitmentService(params)
}

// newCommitmentService hands same-package callers the concrete type, for create options beyond the public API.
func newCommitmentService(params ServiceParams) *commitmentService {
	return &commitmentService{ServiceParams: params}
}

func (s *commitmentService) IsEnabled(ctx context.Context) (bool, error) {
	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
	cfg, err := GetSetting[types.CommitmentsV2Config](settingsSvc, ctx, types.SettingKeyCommitmentsV2)
	if err != nil {
		return false, err
	}

	return cfg.Enabled, nil
}

func (s *commitmentService) Preview(
	ctx context.Context,
	req dto.CreateCommitmentRequest,
) (*dto.CommitmentResponse, error) {
	c, _, value, err := s.prepare(ctx, &req, commitmentCreateOptions{})
	if err != nil {
		return nil, err
	}

	return &dto.CommitmentResponse{Commitment: c, CommitmentValue: value}, nil
}

func (s *commitmentService) requireEnabled(ctx context.Context) error {
	enabled, err := s.IsEnabled(ctx)
	if err != nil {
		return err
	}

	if !enabled {
		return ierr.NewError("commitments are not enabled for this environment").
			WithHint("Enable the commitments_v2 setting").
			Mark(ierr.ErrInvalidOperation)
	}

	return nil
}

// commitmentCreateOptions tunes validation for callers other than the commitments API.
type commitmentCreateOptions struct {
	// allowPendingSubscription accepts a subscription that is still incomplete or draft.
	allowPendingSubscription bool
}

// Create validates and persists a commitment with its rate prices. A repeated idempotency key returns the
// existing commitment; the repository's unique index on the key is the backstop for concurrent retries.
func (s *commitmentService) Create(
	ctx context.Context,
	req dto.CreateCommitmentRequest,
) (*dto.CommitmentResponse, error) {
	return s.create(ctx, req, commitmentCreateOptions{})
}

// create runs under the subscription's commitments lock so the overlap checks see committed rows; a key replayed
// while waiting for the lock returns the original commitment (first write wins).
func (s *commitmentService) create(
	ctx context.Context,
	req dto.CreateCommitmentRequest,
	opts commitmentCreateOptions,
) (*dto.CommitmentResponse, error) {
	existing, err := s.findReplay(ctx, req)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		return s.response(ctx, existing, nil)
	}

	var out *commitment.Commitment
	var cc *commitmentContext
	inserted := false
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.DB.LockWithWait(txCtx, postgres.LockRequest{Key: "commitments:" + req.SubscriptionID}); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to lock subscription commitments").
				Mark(ierr.ErrInternal)
		}

		replayed, err := s.findReplay(txCtx, req)
		if err != nil {
			return err
		}

		if replayed != nil {
			out = replayed
			return nil
		}

		granted, err := (&entitlementGrantService{ServiceParams: s.ServiceParams}).hasSubscriptionScopedEntitlement(txCtx, req.SubscriptionID)
		if err != nil {
			return err
		}
		if granted {
			return ierr.NewError("subscription has a subscription entitlement grant").
				WithHint("A subscription entitlement grant cannot be combined with a commitment; delete the grant first.").
				WithReportableDetails(map[string]any{"subscription_id": req.SubscriptionID}).
				Mark(ierr.ErrValidation)
		}

		var c *commitment.Commitment
		if c, cc, _, err = s.prepare(txCtx, &req, opts); err != nil {
			return err
		}

		meterID := scopeMeter(c, cc)
		if c.CommittedPriceID, err = s.createRatePrice(txCtx, c, cc.Sub, meterID, req.CommittedPrice, "committed"); err != nil {
			return err
		}

		if c.OveragePriceID, err = s.createRatePrice(txCtx, c, cc.Sub, meterID, req.OveragePrice, "overage"); err != nil {
			return err
		}

		if out, err = s.CommitmentRepo.Create(txCtx, c); err != nil {
			return err
		}

		inserted = true
		return nil
	})
	if err != nil && req.IdempotencyKey != nil && ierr.IsAlreadyExists(err) {
		// Lost a race with a concurrent retry on the same key. Only recoverable at top level: inside an
		// outer transaction the failed insert has already aborted that transaction.
		replayed, replayErr := s.findReplay(ctx, req)
		if replayErr != nil {
			return nil, replayErr
		}

		if replayed != nil {
			return s.response(ctx, replayed, nil)
		}
	}

	if err != nil {
		return nil, err
	}

	if !inserted {
		return s.response(ctx, out, nil)
	}

	s.Logger.Info(ctx, "commitment.created",
		"operation", "commitment.created",
		"commitment_id", out.ID,
		"subscription_id", out.SubscriptionID,
	)
	s.publishSystemEvent(ctx, types.WebhookEventCommitmentCreated, out)
	return s.response(ctx, out, cc)
}

// findReplay returns the commitment already created under req's idempotency key, or nil when the key is unused.
// A key used on another subscription is rejected rather than replayed.
func (s *commitmentService) findReplay(ctx context.Context, req dto.CreateCommitmentRequest) (*commitment.Commitment, error) {
	if req.IdempotencyKey == nil {
		return nil, nil
	}

	existing, err := s.CommitmentRepo.GetByIdempotencyKey(ctx, *req.IdempotencyKey)
	if ierr.IsNotFound(err) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	if existing.SubscriptionID != req.SubscriptionID {
		return nil, ierr.Validationf(
			"idempotency_key %s belongs to a commitment on subscription %s, not subscription %s",
			*req.IdempotencyKey,
			existing.SubscriptionID,
			req.SubscriptionID,
		)
	}

	return existing, nil
}

// prepare validates the request and builds the commitment and its value (nil when open-ended) without persisting
// anything.
func (s *commitmentService) prepare(
	ctx context.Context,
	req *dto.CreateCommitmentRequest,
	opts commitmentCreateOptions,
) (*commitment.Commitment, *commitmentContext, *decimal.Decimal, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, nil, nil, err
	}

	cc, err := s.loadCommitmentContext(ctx, req.SubscriptionID)
	if err != nil {
		return nil, nil, nil, err
	}

	req.ApplyDefaults(cc.Sub.Timezone)
	if err := req.Validate(); err != nil {
		return nil, nil, nil, err
	}

	if err := s.validateAgainstSubscription(ctx, req, cc, opts); err != nil {
		return nil, nil, nil, err
	}

	c := req.ToCommitment(ctx, cc.Sub)
	for _, rate := range []struct {
		in   *dto.CommitmentPriceInput
		kind string
	}{
		{in: req.CommittedPrice, kind: "committed"},
		{in: req.OveragePrice, kind: "overage"},
	} {
		if rate.in == nil {
			continue
		}

		priceReq := rate.in.ToCreatePriceRequest(c, cc.Sub, scopeMeter(c, cc), rate.kind)
		if err := priceReq.Validate(); err != nil {
			return nil, nil, nil, err
		}
	}

	// Valued from the request so a preview matches the commitment once its price rows exist.
	amountOf := identityAmount
	if req.CommittedPrice != nil {
		amountOf = priceAmountFn(ctx, s.ServiceParams, req.CommittedPrice.ToPrice(c.Currency))
	}

	value, err := termValue(c, cc.Sub, amountOf, bucketFor(c, cc))
	if err != nil {
		return nil, nil, nil, err
	}

	return c, cc, value, nil
}

// scopeMeter returns the meter of the first scoped line at the commitment start, which backs its rate prices.
func scopeMeter(c *commitment.Commitment, cc *commitmentContext) string {
	// Whole-second term edges can sit a fraction below mid-second line starts; resolve from whichever is later.
	lines := resolveScopeLines(c.ScopeEntityType, c.ScopeEntityID, cc, maxTime(c.StartDate, cc.Sub.StartDate))
	if len(lines) == 0 {
		return ""
	}

	return lines[0].MeterID
}

// bucketFor returns the shared bucket size of the scoped prices for window measurement, otherwise "".
func bucketFor(c *commitment.Commitment, cc *commitmentContext) types.WindowSize {
	if c.Measurement != types.CommitmentMeasurementWindow {
		return ""
	}

	lines := resolveScopeLines(c.ScopeEntityType, c.ScopeEntityID, cc, maxTime(c.StartDate, cc.Sub.StartDate))
	if len(lines) == 0 {
		return ""
	}

	return price.ResolveBucketSize(cc.Prices[lines[0].PriceID], cc.Meters[lines[0].MeterID])
}

// createRatePrice creates the subscription-scoped usage price backing a committed or overage rate.
func (s *commitmentService) createRatePrice(
	ctx context.Context,
	c *commitment.Commitment,
	sub *subscription.Subscription,
	meterID string,
	in *dto.CommitmentPriceInput,
	kind string,
) (*string, error) {
	if in == nil {
		return nil, nil
	}

	resp, err := NewPriceService(s.ServiceParams).CreatePrice(ctx, in.ToCreatePriceRequest(c, sub, meterID, kind))
	if err != nil {
		return nil, err
	}

	return lo.ToPtr(resp.ID), nil
}

func (s *commitmentService) Get(ctx context.Context, id string) (*dto.CommitmentResponse, error) {
	c, err := s.CommitmentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.response(ctx, c, nil)
}

func (s *commitmentService) List(ctx context.Context, filter *commitment.Filter) (*dto.ListCommitmentsResponse, error) {
	if filter == nil {
		filter = commitment.NewDefaultFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	items, err := s.CommitmentRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	total, err := s.CommitmentRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	contexts := map[string]*commitmentContext{}
	responses := make([]*dto.CommitmentResponse, 0, len(items))
	for _, c := range items {
		cc, ok := contexts[c.SubscriptionID]
		if !ok {
			if cc, err = s.loadCommitmentContext(ctx, c.SubscriptionID); err != nil {
				return nil, err
			}

			contexts[c.SubscriptionID] = cc
		}

		resp, err := s.response(ctx, c, cc)
		if err != nil {
			return nil, err
		}

		responses = append(responses, resp)
	}

	out := types.NewListResponse(responses, total, filter.GetLimit(), filter.GetOffset())
	return &out, nil
}

// Update edits only the descriptive fields; the economic terms are immutable. It re-reads under the
// commitment lock so a concurrent terminate is never overwritten.
func (s *commitmentService) Update(
	ctx context.Context,
	id string,
	req dto.UpdateCommitmentRequest,
) (*dto.CommitmentResponse, error) {
	if err := validator.ValidateRequest(req); err != nil {
		return nil, err
	}

	if (req.Name != nil && *req.Name == "") || (req.InvoiceLabel != nil && *req.InvoiceLabel == "") {
		return nil, ierr.Validation("name and invoice_label must be 1-255 characters")
	}

	var updated *commitment.Commitment
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockCommitment(txCtx, id); err != nil {
			return err
		}

		c, err := s.CommitmentRepo.Get(txCtx, id)
		if err != nil {
			return err
		}

		b := commitment.NewCommitmentBuilder(c)
		if req.Name != nil {
			b.WithName(*req.Name)
		}

		if req.InvoiceLabel != nil {
			b.WithInvoiceLabel(*req.InvoiceLabel)
		}

		if req.InvoiceDescription != nil {
			b.WithInvoiceDescription(req.InvoiceDescription)
		}

		if req.Metadata != nil {
			b.WithMetadata(req.Metadata)
		}

		updated, err = s.CommitmentRepo.Update(txCtx, b.Build())
		return err
	})
	if err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventCommitmentUpdated, updated)
	return s.response(ctx, updated, nil)
}

// Terminate ends an active commitment at effective_at (default now). A cancellation fee is billed on an immediate
// one-off invoice, created in the same transaction as the status change.
func (s *commitmentService) Terminate(
	ctx context.Context,
	id string,
	req dto.TerminateCommitmentRequest,
) (*dto.CommitmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	effectiveAt := lo.FromPtrOr(req.EffectiveAt, time.Now().UTC()).Truncate(time.Microsecond)
	var out *commitment.Commitment
	var invoiceID *string
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		out, invoiceID, err = s.terminate(txCtx, id, effectiveAt, req.CancellationFee)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "commitment.terminated",
		"operation", "commitment.terminated",
		"commitment_id", out.ID,
		"subscription_id", out.SubscriptionID,
		"effective_at", effectiveAt,
		"termination_invoice_id", lo.FromPtr(invoiceID),
	)
	s.publishSystemEvent(ctx, types.WebhookEventCommitmentTerminated, out)

	resp, err := s.response(ctx, out, nil)
	if err != nil {
		return nil, err
	}

	resp.TerminationInvoiceID = invoiceID
	return resp, nil
}

// TerminateForSubscription terminates every active commitment on the subscription whose term runs past effectiveAt,
// and pulls a termination scheduled after effectiveAt back to it.
func (s *commitmentService) TerminateForSubscription(
	ctx context.Context,
	subscriptionID string,
	effectiveAt time.Time,
) error {
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.DB.LockWithWait(txCtx, postgres.LockRequest{Key: "commitments:" + subscriptionID}); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to lock subscription commitments").
				Mark(ierr.ErrInternal)
		}

		f := commitment.NewNoLimitFilter()
		f.SubscriptionIDs = []string{subscriptionID}
		f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive, types.CommitmentStatusTerminated}
		listed, err := s.CommitmentRepo.List(txCtx, f)
		if err != nil {
			return err
		}

		for _, c := range listed {
			if c.CommitmentStatus == types.CommitmentStatusTerminated {
				if c.TerminatedAt == nil || !c.TerminatedAt.After(effectiveAt) {
					continue
				}

				if err := s.lockCommitment(txCtx, c.ID); err != nil {
					return err
				}

				latest, err := s.CommitmentRepo.Get(txCtx, c.ID)
				if err != nil {
					return err
				}

				if _, err := s.CommitmentRepo.Update(txCtx, commitment.NewCommitmentBuilder(latest).WithTerminatedAt(effectiveAt).Build()); err != nil {
					return err
				}

				s.Logger.Info(ctx, "commitment.termination_rescheduled",
					"operation", "commitment.termination_rescheduled",
					"commitment_id", c.ID,
					"subscription_id", subscriptionID,
					"previous_terminated_at", *c.TerminatedAt,
					"effective_at", effectiveAt,
				)
				continue
			}

			if c.EndedBy(effectiveAt) {
				continue
			}

			out, _, err := s.terminate(txCtx, c.ID, effectiveAt, nil)
			if err != nil {
				return err
			}

			s.publishSystemEvent(txCtx, types.WebhookEventCommitmentTerminated, out)
			s.Logger.Info(ctx, "commitment.terminated",
				"operation", "commitment.terminated",
				"commitment_id", c.ID,
				"subscription_id", subscriptionID,
				"effective_at", effectiveAt,
			)
		}

		return nil
	})
}

// publishSystemEvent sends a commitment webhook once the surrounding transaction commits, so the payload builder
// can read the commitment and a rollback sends nothing. A failure is logged and never fails the write.
func (s *commitmentService) publishSystemEvent(ctx context.Context, eventName types.WebhookEventName, c *commitment.Commitment) {
	event, err := types.NewWebhookEvent(eventName).
		WithIdentityFromContext(ctx).
		WithEntity(types.SystemEntityTypeCommitment, c.ID).
		WithPayload(webhookDto.InternalCommitmentEvent{
			CommitmentID:   c.ID,
			SubscriptionID: c.SubscriptionID,
			TenantID:       types.GetTenantID(ctx),
		}).
		Build()
	if err != nil {
		s.Logger.Error(ctx, "failed to build commitment webhook", "error", err, "commitment_id", c.ID)
		return
	}

	// The publisher records the event in Postgres, which must not run on the finished transaction.
	publishCtx := types.WithoutDBTransaction(ctx)
	publish := func() {
		if err := s.WebhookPublisher.PublishWebhook(publishCtx, event); err != nil {
			s.Logger.Error(publishCtx, "failed to publish commitment webhook",
				"error", err,
				"event_name", eventName,
				"commitment_id", c.ID,
			)
		}
	}

	if !types.RegisterPostCommit(ctx, publish) {
		publish()
	}
}

// lockCommitment serialises writes to one commitment and must run inside a transaction.
func (s *commitmentService) lockCommitment(ctx context.Context, id string) error {
	if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: "commitment:" + id}); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to lock commitment").
			Mark(ierr.ErrInternal)
	}

	return nil
}

// terminate re-reads the commitment under its lock and must run inside a transaction. The fee invoice is created
// before the status change so a billing failure leaves the commitment active.
func (s *commitmentService) terminate(
	ctx context.Context,
	id string,
	effectiveAt time.Time,
	fee *decimal.Decimal,
) (*commitment.Commitment, *string, error) {
	if err := s.lockCommitment(ctx, id); err != nil {
		return nil, nil, err
	}

	c, err := s.CommitmentRepo.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	if c.CommitmentStatus != types.CommitmentStatusActive {
		return nil, nil, ierr.NewErrorf("commitment %s is %s", c.ID, c.CommitmentStatus).
			WithHint("Only active commitments can be terminated").
			Mark(ierr.ErrInvalidOperation)
	}

	if c.EndedBy(effectiveAt) {
		return nil, nil, ierr.Validationf("effective_at must be before the commitment end %s", c.EffectiveEnd().Format(time.RFC3339))
	}

	var invoiceID *string
	if fee != nil {
		if invoiceID, err = s.billCancellationFee(ctx, c, effectiveAt, *fee); err != nil {
			return nil, nil, err
		}
	}

	// Kept as requested, even before start_date, where nothing is earned.
	terminated := commitment.NewCommitmentBuilder(c).
		WithStatus(types.CommitmentStatusTerminated).
		WithTerminatedAt(effectiveAt).
		Build()
	out, err := s.CommitmentRepo.Update(ctx, terminated)
	if err != nil {
		return nil, nil, err
	}

	return out, invoiceID, nil
}

// billCancellationFee bills the fee on a one-off invoice, or returns nil when it rounds to nothing.
func (s *commitmentService) billCancellationFee(
	ctx context.Context,
	c *commitment.Commitment,
	effectiveAt time.Time,
	fee decimal.Decimal,
) (*string, error) {
	if !c.Binding {
		return nil, ierr.Validation("cancellation_fee requires a binding commitment")
	}

	amount := types.RoundToCurrencyPrecision(fee, c.Currency)
	if !amount.IsPositive() {
		return nil, nil
	}

	sub, err := s.SubRepo.Get(ctx, c.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// The fee invoice is dated at effective_at; a closed billing period must not gain charges.
	if effectiveAt.Before(sub.CurrentPeriodStart) {
		return nil, ierr.Validationf(
			"effective_at with a cancellation_fee cannot precede the current billing period start %s",
			sub.CurrentPeriodStart.Format(time.RFC3339),
		)
	}

	// Nor can it fall at or past the subscription end, which bounds the term the fee covers.
	if sub.EndDate != nil && !effectiveAt.Before(*sub.EndDate) {
		return nil, ierr.Validationf(
			"effective_at with a cancellation_fee must be before the subscription end %s",
			sub.EndDate.Format(time.RFC3339),
		)
	}

	metadata := types.Metadata{
		types.MetadataKeyCommitmentLineKind: string(types.CommitmentLineCancellationFee),
		types.MetadataKeyCommitmentID:       c.ID,
	}
	if c.InvoiceDescription != nil {
		metadata[types.MetadataKeyDescription] = *c.InvoiceDescription
	}

	// The fee covers the rest of the term, or of the billing period holding effective_at for one that never ends;
	// either stops at the subscription end.
	periodEnd := c.EffectiveEnd()
	if c.OpenEnded() {
		holding, err := periodAt(sub, effectiveAt)
		if err != nil {
			return nil, err
		}

		periodEnd = &holding.End
	}

	if sub.EndDate != nil {
		periodEnd = lo.ToPtr(minTime(*periodEnd, *sub.EndDate))
	}

	billingPeriod := string(sub.BillingPeriod)
	inv, err := NewInvoiceService(s.ServiceParams).CreateInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     sub.GetInvoicingCustomerID(),
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeOneOff,
		Currency:       c.Currency,
		BillingReason:  types.InvoiceBillingReasonProration,
		AmountDue:      amount,
		Total:          amount,
		Subtotal:       amount,
		PeriodStart:    lo.ToPtr(effectiveAt),
		PeriodEnd:      periodEnd,
		BillingPeriod:  &billingPeriod,
		IdempotencyKey: lo.ToPtr("commitment_termination_" + c.ID),
		LineItems: []dto.CreateInvoiceLineItemRequest{
			{
				DisplayName:  lo.ToPtr(c.InvoiceLabel + " (cancellation fee)"),
				Quantity:     decimal.NewFromInt(1),
				Amount:       amount,
				PeriodStart:  lo.ToPtr(effectiveAt),
				PeriodEnd:    periodEnd,
				CommitmentID: lo.ToPtr(c.ID),
				EntityType:   lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment)),
				EntityID:     lo.ToPtr(c.ID),
				Metadata:     metadata,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return lo.ToPtr(inv.ID), nil
}

// ActiveForPeriod returns commitments whose term overlaps [from, to), including terminated ones,
// which still bill up to terminated_at (callers check IsActiveAt).
func (s *commitmentService) ActiveForPeriod(
	ctx context.Context,
	subscriptionID string,
	from time.Time,
	to time.Time,
) ([]*commitment.Commitment, error) {
	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{subscriptionID}
	f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive, types.CommitmentStatusTerminated}
	f.ActiveFrom, f.ActiveTo = &from, &to
	return s.CommitmentRepo.List(ctx, f)
}

// identityAmount values an amount commitment, whose capacity is already money.
func identityAmount(capacity decimal.Decimal) decimal.Decimal { return capacity }

// priceAmountFn values a quantity by applying p, the way billing prices usage.
func priceAmountFn(ctx context.Context, params ServiceParams, p *price.Price) func(decimal.Decimal) decimal.Decimal {
	prices := NewPriceService(params)
	return func(qty decimal.Decimal) decimal.Decimal { return prices.CalculateCost(ctx, p, qty) }
}

// committedAmountFn values the commitment's capacity: identity for amount commitments, the committed price otherwise.
func (s *commitmentService) committedAmountFn(
	ctx context.Context,
	c *commitment.Commitment,
) (func(decimal.Decimal) decimal.Decimal, error) {
	if c.Type == types.COMMITMENT_TYPE_AMOUNT {
		return identityAmount, nil
	}

	// A quantity commitment without a committed price values to zero.
	var p *price.Price
	if c.CommittedPriceID != nil {
		var err error
		if p, err = s.PriceRepo.Get(ctx, *c.CommittedPriceID); err != nil {
			return nil, err
		}
	}

	return priceAmountFn(ctx, s.ServiceParams, p), nil
}

// response builds the API response, computing the commitment value. cc may be nil; List passes its
// per-subscription context so the value math reuses it.
func (s *commitmentService) response(
	ctx context.Context,
	c *commitment.Commitment,
	cc *commitmentContext,
) (*dto.CommitmentResponse, error) {
	amountOf, err := s.committedAmountFn(ctx, c)
	if err != nil {
		return nil, err
	}

	// Only window measurement needs the scope lines, for its bucket size; otherwise the subscription is enough.
	if cc == nil && c.Measurement == types.CommitmentMeasurementWindow {
		if cc, err = s.loadCommitmentContext(ctx, c.SubscriptionID); err != nil {
			return nil, err
		}
	}

	var sub *subscription.Subscription
	var bucket types.WindowSize
	if cc != nil {
		sub, bucket = cc.Sub, bucketFor(c, cc)
	} else if sub, err = s.SubRepo.Get(ctx, c.SubscriptionID); err != nil {
		return nil, err
	}

	value, err := termValue(c, sub, amountOf, bucket)
	if err != nil {
		return nil, err
	}

	return &dto.CommitmentResponse{Commitment: c, CommitmentValue: value}, nil
}

func (s *commitmentService) CheckPriceAttach(
	ctx context.Context,
	sub *subscription.Subscription,
	p *price.Price,
	at time.Time,
	until *time.Time,
) error {
	_, err := s.checkPriceAttach(ctx, sub, p, at, until)
	return err
}

// checkPriceAttach is CheckPriceAttach that also returns the commitment the price would break.
// It must run inside a transaction.
func (s *commitmentService) checkPriceAttach(
	ctx context.Context,
	sub *subscription.Subscription,
	p *price.Price,
	at time.Time,
	until *time.Time,
) (string, error) {
	if p == nil || p.Type != types.PRICE_TYPE_USAGE {
		return "", nil
	}

	enabled, err := s.IsEnabled(ctx)
	if err != nil || !enabled {
		return "", err
	}

	// Serialise with create before reading, so a concurrent create is seen.
	if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: "commitments:" + sub.ID}); err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to lock subscription commitments").
			Mark(ierr.ErrInternal)
	}

	inScope, err := s.commitmentsInScope(ctx, sub, p, at, until)
	if err != nil || len(inScope) == 0 {
		return "", err
	}

	if meterID, err := s.entitledMeter(ctx, sub, []string{p.MeterID}); err != nil {
		return "", err
	} else if meterID != "" {
		return inScope[0].ID, ierr.Validationf("price %s is on meter %s, which has an active entitlement; it cannot join a commitment's scope", p.ID, meterID)
	}

	if !billsEverySubscriptionPeriod(sub, p.BillingPeriod, p.BillingPeriodCount) {
		return inScope[0].ID, ierr.NewErrorf(
			"commitments require scope prices billed every subscription period; price %s bills on another cadence",
			p.ID,
		).
			WithHint("commitments require scope prices billed every subscription period").
			Mark(ierr.ErrValidation)
	}

	m, err := s.MeterRepo.GetMeter(ctx, p.MeterID)
	if err != nil {
		return "", err
	}

	if err := rejectGroupedBucketedPrice(p, m); err != nil {
		return inScope[0].ID, err
	}

	cc, err := s.loadCommitmentContext(ctx, sub.ID)
	if err != nil {
		return "", err
	}

	for _, c := range inScope {
		if err := s.checkAttachAgainst(ctx, c, cc, p, at); err != nil {
			return c.ID, err
		}
	}

	return s.checkAttachPairs(ctx, sub, inScope)
}

// commitmentsInScope returns commitments covering the new usage price that hold their scope after `at`, through the
// billing period holding their effective end, and start before `until` (nil when open-ended).
func (s *commitmentService) commitmentsInScope(
	ctx context.Context,
	sub *subscription.Subscription,
	p *price.Price,
	at time.Time,
	until *time.Time,
) ([]*commitment.Commitment, error) {
	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{sub.ID}
	f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive, types.CommitmentStatusTerminated}
	listed, err := s.CommitmentRepo.List(ctx, f)
	if err != nil {
		return nil, err
	}

	var inScope []*commitment.Commitment
	for _, c := range listed {
		blockedUntil, err := blocksUntil(sub, c)
		if err != nil {
			return nil, err
		}

		if endReached(blockedUntil, at) || endReached(blockedUntil, c.StartDate) || endReached(until, c.StartDate) {
			continue
		}

		// A price-scoped commitment never includes a newly attached price (new ID).
		switch c.ScopeEntityType {
		case types.CommitmentScopeSubscription:
		case types.CommitmentScopeGroup:
			if p.GroupID != c.ScopeEntityID {
				continue
			}
		case types.CommitmentScopeMeter:
			if p.MeterID != c.ScopeEntityID {
				continue
			}
		default:
			continue
		}

		inScope = append(inScope, c)
	}

	return inScope, nil
}

// checkAttachAgainst checks the meter and bucket-size rules for one commitment that the new price joins.
func (s *commitmentService) checkAttachAgainst(
	ctx context.Context,
	c *commitment.Commitment,
	cc *commitmentContext,
	p *price.Price,
	at time.Time,
) error {
	lines := resolveScopeLines(c.ScopeEntityType, c.ScopeEntityID, cc, maxTime(at, c.StartDate))
	if len(lines) == 0 {
		// Nothing in scope at this instant, e.g. the only line was just replaced: check against the term's lines.
		lines = scopeLinesInPeriod(c.ScopeEntityType, c.ScopeEntityID, cc, c.StartDate, c.EffectiveEnd())
	}

	if len(lines) == 0 {
		return nil
	}

	if (c.Type == types.COMMITMENT_TYPE_QUANTITY || c.OveragePriceID != nil) && p.MeterID != lines[0].MeterID {
		return ierr.Validationf("commitment %s covers a single meter; price %s is on a different meter", c.ID, p.ID)
	}

	if c.Measurement != types.CommitmentMeasurementWindow {
		return nil
	}

	m, err := s.MeterRepo.GetMeter(ctx, p.MeterID)
	if err != nil {
		return err
	}

	want := price.ResolveBucketSize(cc.Prices[lines[0].PriceID], cc.Meters[lines[0].MeterID])
	got := price.ResolveBucketSize(p, m)
	if got != want {
		return ierr.Validationf(
			"commitment %s measures by window with bucket size %q; price %s has bucket size %q",
			c.ID,
			want,
			p.ID,
			got,
		)
	}

	return rejectCalendarBuckets(got)
}

// checkAttachPairs requires commitments that now share the new price and overlap in time to agree on type,
// measurement and overage price, and those still allocating together to differ in priority.
func (s *commitmentService) checkAttachPairs(
	ctx context.Context,
	sub *subscription.Subscription,
	inScope []*commitment.Commitment,
) (string, error) {
	for i, a := range inScope {
		for _, b := range inScope[i+1:] {
			aBlockedUntil, err := blocksUntil(sub, a)
			if err != nil {
				return "", err
			}

			bBlockedUntil, err := blocksUntil(sub, b)
			if err != nil {
				return "", err
			}

			if endReached(bBlockedUntil, a.StartDate) || endReached(aBlockedUntil, b.StartDate) {
				continue
			}

			allocatingTogether := !b.EndedBy(a.StartDate) && !a.EndedBy(b.StartDate)
			if allocatingTogether && a.Priority == b.Priority {
				return a.ID, ierr.Validationf(
					"commitments %s and %s overlap on the new price with the same priority %d",
					a.ID,
					b.ID,
					a.Priority,
				)
			}

			if a.Type != b.Type || a.Measurement != b.Measurement {
				return a.ID, ierr.Validationf(
					"commitments %s and %s overlap on the new price but differ in type or measurement",
					a.ID,
					b.ID,
				)
			}

			same, err := s.sameCommitmentOverage(ctx, a, b)
			if err != nil {
				return "", err
			}

			if !same {
				return a.ID, ierr.Validationf(
					"commitments %s and %s overlap on the new price but have different overage prices; only equal flat overage amounts combine on the same usage",
					a.ID,
					b.ID,
				)
			}
		}
	}

	return "", nil
}

// sameCommitmentOverage reports whether both have no overage price, or flat ones with the same amount.
func (s *commitmentService) sameCommitmentOverage(ctx context.Context, a, b *commitment.Commitment) (bool, error) {
	if a.OveragePriceID == nil || b.OveragePriceID == nil {
		return a.OveragePriceID == nil && b.OveragePriceID == nil, nil
	}

	pa, err := s.PriceRepo.Get(ctx, *a.OveragePriceID)
	if err != nil {
		return false, err
	}

	pb, err := s.PriceRepo.Get(ctx, *b.OveragePriceID)
	if err != nil {
		return false, err
	}

	return pa.BillingModel == types.BILLING_MODEL_FLAT_FEE && pb.BillingModel == types.BILLING_MODEL_FLAT_FEE &&
		pa.Amount.Equal(pb.Amount), nil
}
