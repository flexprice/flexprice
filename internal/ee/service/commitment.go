package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
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
	// GetBillingTerms returns the commitment's published terms and its current instalments.
	GetBillingTerms(ctx context.Context, id string) (*dto.BillingTermsResponse, error)
	// PreviewBillingTerms returns what ReplaceBillingTerms would, writing nothing.
	PreviewBillingTerms(ctx context.Context, id string, req dto.ReplaceBillingTermsRequest) (*dto.BillingTermsResponse, error)
	// ReplaceBillingTerms swaps the whole set of terms and replaces the scheduled instalments with theirs; invoiced
	// instalments are kept.
	ReplaceBillingTerms(ctx context.Context, id string, req dto.ReplaceBillingTermsRequest) (*dto.BillingTermsResponse, error)
	// TriggerInstalment bills a manual instalment of the commitment on the given date or now, and returns it.
	TriggerInstalment(ctx context.Context, id, instalmentID string, req dto.TriggerInstalmentRequest) (*billingterm.Instalment, error)
	// ListInstalments returns the commitment's instalments in billing order; empty statuses list every status, cancelled included.
	ListInstalments(ctx context.Context, id string, statuses []types.InstalmentStatus) (*dto.ListInstalmentsResponse, error)
	// GetInstalment returns one instalment of any commitment in the environment.
	GetInstalment(ctx context.Context, instalmentID string) (*billingterm.Instalment, error)
	// BillDueInstalments issues a one-off invoice for each due separate-invoice instalment in every environment with
	// commitments enabled. A failed instalment is logged and the rest still bill; the failures are returned together.
	BillDueInstalments(ctx context.Context) error
	// ListOverlapping returns the subscription's active and terminated commitments whose term overlaps [from, to); a
	// terminated one still bills up to terminated_at.
	ListOverlapping(ctx context.Context, subscriptionID string, from, to time.Time) ([]*commitment.Commitment, error)
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

func (s *commitmentService) Preview(
	ctx context.Context,
	req dto.CreateCommitmentRequest,
) (*dto.CommitmentResponse, error) {
	preview, _, err := s.prepare(ctx, &req, commitmentCreateOptions{})
	return preview, err
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
	replayed, err := s.findReplay(ctx, req)
	if err != nil {
		return nil, err
	}

	if replayed != nil {
		return s.replayResponse(ctx, replayed)
	}

	var created *dto.CommitmentResponse
	var cc *commitmentContext
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockSubscriptionCommitments(txCtx, req.SubscriptionID); err != nil {
			return err
		}

		var err error
		if replayed, err = s.findReplay(txCtx, req); err != nil || replayed != nil {
			return err
		}

		created, cc, err = s.insert(txCtx, req, opts)
		return err
	})
	if err != nil && req.IdempotencyKey != nil && ierr.IsAlreadyExists(err) {
		// Lost a race with a concurrent retry on the same key. Only recoverable at top level: inside an outer
		// transaction the failed insert has already aborted that transaction.
		var replayErr error
		if replayed, replayErr = s.findReplay(ctx, req); replayErr != nil {
			return nil, replayErr
		}
	}

	if replayed != nil {
		return s.replayResponse(ctx, replayed)
	}

	if err != nil {
		return nil, err
	}

	c := created.Commitment
	s.Logger.Info(ctx, "commitment.created",
		"operation", "commitment.created",
		"commitment_id", c.ID,
		"subscription_id", c.SubscriptionID,
	)
	s.publishSystemEvent(ctx, types.WebhookEventCommitmentCreated, c)
	if len(created.BillingTerms) > 0 {
		publishBillingTermsEvent(ctx, s.ServiceParams, types.WebhookEventBillingTermsCreated, c)
		publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCreated, created.Instalments)
	}

	resp, err := s.valuedResponse(ctx, c, cc.Sub, bucketFor(c, cc))
	if err != nil {
		return nil, err
	}

	resp.BillingTerms, resp.Instalments = created.BillingTerms, created.Instalments
	return resp, nil
}

// insert validates req and writes the commitment with its rate prices, billing terms and instalments. It must run
// under the subscription's commitments lock.
func (s *commitmentService) insert(
	ctx context.Context,
	req dto.CreateCommitmentRequest,
	opts commitmentCreateOptions,
) (*dto.CommitmentResponse, *commitmentContext, error) {
	granted, err := (&entitlementGrantService{ServiceParams: s.ServiceParams}).
		hasSubscriptionScopedEntitlement(ctx, req.SubscriptionID)
	if err != nil {
		return nil, nil, err
	}

	if granted {
		return nil, nil, ierr.NewError("subscription has a subscription entitlement grant").
			WithHint("A subscription entitlement grant cannot be combined with a commitment; delete the grant first.").
			WithReportableDetails(map[string]any{"subscription_id": req.SubscriptionID}).
			Mark(ierr.ErrValidation)
	}

	prepared, cc, err := s.prepare(ctx, &req, opts)
	if err != nil {
		return nil, nil, err
	}

	c := prepared.Commitment
	meterID := scopeMeter(c, cc)
	if c.CommittedPriceID, err = s.createRatePrice(ctx, c, cc.Sub, meterID, req.CommittedPrice, types.CommitmentRateCommitted); err != nil {
		return nil, nil, err
	}

	if c.OveragePriceID, err = s.createRatePrice(ctx, c, cc.Sub, meterID, req.OveragePrice, types.CommitmentRateOverage); err != nil {
		return nil, nil, err
	}

	created := &dto.CommitmentResponse{}
	if created.Commitment, err = s.CommitmentRepo.Create(ctx, c); err != nil {
		return nil, nil, err
	}

	if len(prepared.BillingTerms) == 0 {
		return created, cc, nil
	}

	if created.BillingTerms, err = s.BillingTermRepo.CreateMany(ctx, prepared.BillingTerms); err != nil {
		return nil, nil, err
	}

	if created.Instalments, err = s.BillingInstalmentRepo.CreateMany(ctx, prepared.Instalments); err != nil {
		return nil, nil, err
	}

	return created, cc, nil
}

// replayResponse returns a replayed create's commitment with its current billing terms and instalments.
func (s *commitmentService) replayResponse(ctx context.Context, c *commitment.Commitment) (*dto.CommitmentResponse, error) {
	resp, err := s.response(ctx, c)
	if err != nil || lo.FromPtr(c.CollectionMethod) != types.CommitmentCollectionBillingTerms {
		return resp, err
	}

	terms, err := s.GetBillingTerms(ctx, c.ID)
	if err != nil {
		return nil, err
	}

	resp.BillingTerms, resp.Instalments = terms.BillingTerms, terms.Instalments
	return resp, nil
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

// prepare validates the request and builds the commitment, its value (nil when open-ended) and, under billing
// terms, the terms and instalments, without persisting anything.
func (s *commitmentService) prepare(
	ctx context.Context,
	req *dto.CreateCommitmentRequest,
	opts commitmentCreateOptions,
) (*dto.CommitmentResponse, *commitmentContext, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, nil, err
	}

	cc, err := s.loadCommitmentContext(ctx, req.SubscriptionID)
	if err != nil {
		return nil, nil, err
	}

	req.ApplyDefaults(cc.Sub.Timezone)

	// A date within a second after a billing period edge means the edge; validation sees the aligned dates.
	if req.StartDate, err = atPeriodEdge(cc.Sub, req.StartDate); err != nil {
		return nil, nil, err
	}

	if req.EndDate, err = atPeriodEdgePtr(cc.Sub, req.EndDate); err != nil {
		return nil, nil, err
	}

	if err := alignTermDates(cc.Sub, req.BillingTerms); err != nil {
		return nil, nil, err
	}

	if err := req.Validate(); err != nil {
		return nil, nil, err
	}

	c := req.ToCommitment(ctx, cc.Sub)
	if err := s.validateAgainstSubscription(ctx, c, req.OveragePrice, cc, opts); err != nil {
		return nil, nil, err
	}

	meterID := scopeMeter(c, cc)
	rates := []struct {
		rate types.CommitmentRate
		in   *dto.CommitmentPriceInput
	}{
		{rate: types.CommitmentRateCommitted, in: req.CommittedPrice},
		{rate: types.CommitmentRateOverage, in: req.OveragePrice},
	}
	for _, r := range rates {
		if r.in == nil {
			continue
		}

		priceReq := r.in.ToCreatePriceRequest(c, cc.Sub, meterID, r.rate)
		if err := priceReq.Validate(); err != nil {
			return nil, nil, err
		}
	}

	// Valued from the request so a preview matches the commitment once its price rows exist.
	amountOf := identityAmount
	if req.CommittedPrice != nil {
		amountOf = priceAmountFn(ctx, s.ServiceParams, req.CommittedPrice.ToPrice(c.Currency))
	}

	value, err := termValue(c, cc.Sub, amountOf, bucketFor(c, cc))
	if err != nil {
		return nil, nil, err
	}

	if lo.FromPtr(c.CollectionMethod) != types.CommitmentCollectionBillingTerms {
		return &dto.CommitmentResponse{Commitment: c, CommitmentValue: value}, cc, nil
	}

	// Expansion splits the commitment's total, so the total is set first.
	c = commitment.NewCommitmentBuilder(c).
		WithTotalAmount(types.RoundToCurrencyPrecision(lo.FromPtr(value), c.Currency)).
		WithInstalmentInvoicing(lo.FromPtr(req.InstalmentInvoicing)).
		Build()
	terms := lo.Map(req.BillingTerms, func(in dto.BillingTermInput, _ int) *billingterm.Term {
		return in.ToTerm(ctx, c.ID)
	})

	instalments, err := s.planInstalments(ctx, c, cc, amountOf, terms, nil, c.StartDate)
	if err != nil {
		return nil, nil, err
	}

	return &dto.CommitmentResponse{
		Commitment:      c,
		CommitmentValue: value,
		BillingTerms:    terms,
		Instalments:     instalments,
	}, cc, nil
}

// scopeMeter returns the meter of the first scoped line at the commitment start, which backs its rate prices.
func scopeMeter(c *commitment.Commitment, cc *commitmentContext) string {
	lines := scopeLinesAtStart(c, cc)
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

	lines := scopeLinesAtStart(c, cc)
	if len(lines) == 0 {
		return ""
	}

	return cc.bucketSize(lines[0])
}

// createRatePrice creates the subscription-scoped usage price backing a committed or overage rate.
func (s *commitmentService) createRatePrice(
	ctx context.Context,
	c *commitment.Commitment,
	sub *subscription.Subscription,
	meterID string,
	in *dto.CommitmentPriceInput,
	rate types.CommitmentRate,
) (*string, error) {
	if in == nil {
		return nil, nil
	}

	resp, err := NewPriceService(s.ServiceParams).CreatePrice(ctx, in.ToCreatePriceRequest(c, sub, meterID, rate))
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

	return s.response(ctx, c)
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

		resp, err := s.valuedResponse(ctx, c, cc.Sub, bucketFor(c, cc))
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
	if err := req.Validate(); err != nil {
		return nil, err
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
	return s.response(ctx, updated)
}

// Terminate ends an active commitment at effective_at, which defaults to now and may not be in the future. A
// cancellation fee and, under billing terms, a settlement are billed on one immediate one-off invoice, created in the
// same transaction as the status change.
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

	now := clock.Now()
	effectiveAt := lo.FromPtrOr(req.EffectiveAt, now)
	if effectiveAt.After(now) {
		return nil, ierr.Validation("future-dated termination is not supported yet")
	}

	fee := decimal.Zero
	if req.CancellationFee != nil {
		fee = req.CancellationFee.Value
	}

	var out *commitment.Commitment
	var invoiceID *string
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		out, invoiceID, err = s.terminate(txCtx, id, effectiveAt, fee)
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

	resp, err := s.response(ctx, out)
	if err != nil {
		return nil, err
	}

	resp.TerminationInvoiceID = invoiceID
	return resp, nil
}

// TerminateForSubscription terminates every active commitment on the subscription whose term runs past effectiveAt.
func (s *commitmentService) TerminateForSubscription(
	ctx context.Context,
	subscriptionID string,
	effectiveAt time.Time,
) error {
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockSubscriptionCommitments(txCtx, subscriptionID); err != nil {
			return err
		}

		listed, err := s.CommitmentRepo.List(txCtx, commitment.NewSubscriptionFilter(subscriptionID, types.CommitmentStatusActive))
		if err != nil {
			return err
		}

		for _, c := range listed {
			if c.EndedBy(effectiveAt) {
				if err := s.settleEnded(txCtx, c); err != nil {
					return err
				}
				continue
			}

			out, _, err := s.terminate(txCtx, c.ID, effectiveAt, decimal.Zero)
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

	publishAfterCommit(ctx, s.ServiceParams, event)
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

// lockSubscriptionCommitments serialises commitment writes and scope changes on one subscription and must run inside
// a transaction.
func (s *commitmentService) lockSubscriptionCommitments(ctx context.Context, subscriptionID string) error {
	if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: "commitments:" + subscriptionID}); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to lock subscription commitments").
			Mark(ierr.ErrInternal)
	}

	return nil
}

// terminate re-reads the commitment under its lock and must run inside a transaction. Under billing terms it settles
// the instalments first. The termination invoice is created before the status change so a billing failure leaves
// the commitment active.
func (s *commitmentService) terminate(
	ctx context.Context,
	id string,
	effectiveAt time.Time,
	fee decimal.Decimal,
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

	sub, err := s.SubRepo.Get(ctx, c.SubscriptionID)
	if err != nil {
		return nil, nil, err
	}

	if effectiveAt, err = atPeriodEdge(sub, effectiveAt); err != nil {
		return nil, nil, err
	}

	if c.EndedBy(effectiveAt) {
		return nil, nil, ierr.Validationf("effective_at must be before the commitment end %s", c.EffectiveEnd().Format(time.RFC3339))
	}

	if fee.IsPositive() && !c.Binding {
		return nil, nil, ierr.Validation("cancellation_fee requires a binding commitment")
	}

	var settlement *billingterm.Instalment
	if lo.FromPtr(c.CollectionMethod) == types.CommitmentCollectionBillingTerms {
		if settlement, err = s.settle(ctx, c, effectiveAt); err != nil {
			return nil, nil, err
		}
	}

	invoiceID, err := s.billTermination(ctx, c, sub, effectiveAt, fee, settlement)
	if err != nil {
		return nil, nil, err
	}

	// terminated_at keeps the requested date, even one before start_date.
	terminated := commitment.NewCommitmentBuilder(c).
		WithCommitmentStatus(types.CommitmentStatusTerminated).
		WithTerminatedAt(effectiveAt).
		Build()
	out, err := s.CommitmentRepo.Update(ctx, terminated)
	if err != nil {
		return nil, nil, err
	}

	return out, invoiceID, nil
}

// billTermination issues the termination invoice carrying the settlement and the cancellation fee, either optional.
// It returns nil when there is no settlement and the fee rounds to nothing.
func (s *commitmentService) billTermination(
	ctx context.Context,
	c *commitment.Commitment,
	sub *subscription.Subscription,
	effectiveAt time.Time,
	fee decimal.Decimal,
	settlement *billingterm.Instalment,
) (*string, error) {
	fee = types.RoundToCurrencyPrecision(fee, c.Currency)
	if settlement == nil && !fee.IsPositive() {
		return nil, nil
	}

	var lines []dto.CreateInvoiceLineItemRequest
	if settlement != nil {
		lines = append(lines, instalmentLine(c, settlement))
	}

	if fee.IsPositive() {
		line, err := cancellationFeeLine(c, sub, effectiveAt, fee)
		if err != nil {
			return nil, err
		}

		lines = append(lines, line)
	}

	total := sumAmounts(lines)

	// The invoice period spans its lines: the settled term before effective_at, the rest the fee covers, or both.
	first := lo.MinBy(lines, func(a, b dto.CreateInvoiceLineItemRequest) bool { return a.PeriodStart.Before(*b.PeriodStart) })
	last := lo.MaxBy(lines, func(a, b dto.CreateInvoiceLineItemRequest) bool { return a.PeriodEnd.After(*b.PeriodEnd) })

	billingPeriod := string(sub.BillingPeriod)
	inv, err := NewInvoiceService(s.ServiceParams).CreateInvoice(ctx, dto.CreateInvoiceRequest{
		CustomerID:     sub.GetInvoicingCustomerID(),
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeOneOff,
		Currency:       c.Currency,
		BillingReason:  types.InvoiceBillingReasonCommitmentTermination,
		AmountDue:      total,
		Total:          total,
		Subtotal:       total,
		PeriodStart:    lo.ToPtr(*first.PeriodStart),
		PeriodEnd:      lo.ToPtr(*last.PeriodEnd),
		BillingPeriod:  &billingPeriod,
		IdempotencyKey: lo.ToPtr("commitment_termination_" + c.ID),
		LineItems:      lines,
	})
	if err != nil {
		return nil, err
	}

	return lo.ToPtr(inv.ID), nil
}

// cancellationFeeLine bills the fee from effectiveAt to the term end, or for an open-ended commitment to the end of
// the billing period holding effectiveAt, cut at the subscription end.
func cancellationFeeLine(
	c *commitment.Commitment,
	sub *subscription.Subscription,
	effectiveAt time.Time,
	fee decimal.Decimal,
) (dto.CreateInvoiceLineItemRequest, error) {
	// The fee is dated at effective_at, so a closed billing period must not gain it.
	if effectiveAt.Before(sub.CurrentPeriodStart) {
		return dto.CreateInvoiceLineItemRequest{}, ierr.Validationf(
			"effective_at with a cancellation_fee cannot precede the current billing period start %s",
			sub.CurrentPeriodStart.Format(time.RFC3339),
		)
	}

	// Nor can it fall at or past the subscription end, which bounds the term the fee covers.
	if sub.EndDate != nil && !effectiveAt.Before(*sub.EndDate) {
		return dto.CreateInvoiceLineItemRequest{}, ierr.Validationf(
			"effective_at with a cancellation_fee must be before the subscription end %s",
			sub.EndDate.Format(time.RFC3339),
		)
	}

	end := c.EffectiveEnd()
	if c.OpenEnded() {
		holding, err := periodAt(sub, effectiveAt)
		if err != nil {
			return dto.CreateInvoiceLineItemRequest{}, err
		}

		end = &holding.End
	}

	if sub.EndDate != nil {
		end = lo.ToPtr(minTime(*end, *sub.EndDate))
	}

	metadata := types.Metadata{
		types.MetadataKeyCommitmentLineKind: string(types.CommitmentLineCancellationFee),
		types.MetadataKeyCommitmentID:       c.ID,
	}
	if c.InvoiceDescription != nil {
		metadata[types.MetadataKeyDescription] = *c.InvoiceDescription
	}

	return dto.CreateInvoiceLineItemRequest{
		DisplayName:  lo.ToPtr(c.InvoiceLabel + " (cancellation fee)"),
		Quantity:     decimal.NewFromInt(1),
		Amount:       fee,
		PeriodStart:  lo.ToPtr(effectiveAt),
		PeriodEnd:    end,
		CommitmentID: lo.ToPtr(c.ID),
		EntityType:   lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment)),
		EntityID:     lo.ToPtr(c.ID),
		Metadata:     metadata,
	}, nil
}

func (s *commitmentService) ListOverlapping(
	ctx context.Context,
	subscriptionID string,
	from time.Time,
	to time.Time,
) ([]*commitment.Commitment, error) {
	f := commitment.NewSubscriptionFilter(subscriptionID, types.CommitmentStatusActive, types.CommitmentStatusTerminated)
	f.ActiveFrom, f.ActiveTo = &from, &to
	return s.CommitmentRepo.List(ctx, f)
}

// identityAmount values an amount commitment, whose capacity is already money.
func identityAmount(capacity decimal.Decimal) decimal.Decimal { return capacity }

// priceAmountFn values a quantity by applying p, the way billing prices usage.
func priceAmountFn(ctx context.Context, params ServiceParams, p *price.Price) amountFn {
	prices := NewPriceService(params)
	return func(qty decimal.Decimal) decimal.Decimal { return prices.CalculateCost(ctx, p, qty) }
}

// committedAmountFn values the commitment's capacity: identity for amount commitments, the committed price otherwise.
func (s *commitmentService) committedAmountFn(
	ctx context.Context,
	c *commitment.Commitment,
) (amountFn, error) {
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

// response builds the API response with the commitment's value. Only window measurement loads the scope, for its
// bucket size.
func (s *commitmentService) response(ctx context.Context, c *commitment.Commitment) (*dto.CommitmentResponse, error) {
	if c.Measurement == types.CommitmentMeasurementWindow {
		cc, err := s.loadCommitmentContext(ctx, c.SubscriptionID)
		if err != nil {
			return nil, err
		}

		return s.valuedResponse(ctx, c, cc.Sub, bucketFor(c, cc))
	}

	sub, err := s.SubRepo.Get(ctx, c.SubscriptionID)
	if err != nil {
		return nil, err
	}

	return s.valuedResponse(ctx, c, sub, "")
}

// valuedResponse builds the API response with the commitment's value over sub, counting windows in bucket.
func (s *commitmentService) valuedResponse(
	ctx context.Context,
	c *commitment.Commitment,
	sub *subscription.Subscription,
	bucket types.WindowSize,
) (*dto.CommitmentResponse, error) {
	amountOf, err := s.committedAmountFn(ctx, c)
	if err != nil {
		return nil, err
	}

	value, err := termValue(c, sub, amountOf, bucket)
	if err != nil {
		return nil, err
	}

	return &dto.CommitmentResponse{Commitment: c, CommitmentValue: value}, nil
}
