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
	// CheckPriceAttach rejects a usage price that would join an active commitment's scope but break its rules:
	// a single-meter commitment gaining a second meter, a bucket-size mismatch, or overlapping commitments that disagree.
	CheckPriceAttach(ctx context.Context, sub *subscription.Subscription, p *price.Price, at time.Time) error
}

type commitmentService struct {
	ServiceParams
}

func NewCommitmentService(params ServiceParams) CommitmentService {
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

	return &dto.CommitmentResponse{Commitment: c, CommitmentValue: &value}, nil
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

// createInline creates a commitment sent with a new subscription, which may not be active yet.
func (s *commitmentService) createInline(
	ctx context.Context,
	req dto.CreateCommitmentRequest,
) (*dto.CommitmentResponse, error) {
	return s.create(ctx, req, commitmentCreateOptions{allowPendingSubscription: true})
}

func (s *commitmentService) create(
	ctx context.Context,
	req dto.CreateCommitmentRequest,
	opts commitmentCreateOptions,
) (*dto.CommitmentResponse, error) {
	if req.IdempotencyKey != nil {
		existing, err := s.CommitmentRepo.GetByIdempotencyKey(ctx, *req.IdempotencyKey)
		if err == nil {
			return s.toResponse(ctx, existing)
		}

		if !ierr.IsNotFound(err) {
			return nil, err
		}
	}

	var out *commitment.Commitment
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		out, err = s.createLocked(txCtx, &req, opts)
		return err
	})
	if err != nil && req.IdempotencyKey != nil && ierr.IsAlreadyExists(err) {
		// Lost a race with a concurrent retry on the same key. Only recoverable at top level: inside an
		// outer transaction the failed insert has already aborted that transaction.
		existing, getErr := s.CommitmentRepo.GetByIdempotencyKey(ctx, *req.IdempotencyKey)
		if getErr != nil {
			return nil, getErr
		}

		return s.toResponse(ctx, existing)
	}

	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "commitment created",
		"commitment_id", out.ID,
		"subscription_id", out.SubscriptionID,
	)
	return s.toResponse(ctx, out)
}

// createLocked serialises creates per subscription so the overlap checks see committed rows,
// and must run inside a transaction. A replayed idempotency key returns the original commitment (first write wins).
func (s *commitmentService) createLocked(
	ctx context.Context,
	req *dto.CreateCommitmentRequest,
	opts commitmentCreateOptions,
) (*commitment.Commitment, error) {
	if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: "commitments:" + req.SubscriptionID}); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to lock subscription commitments").
			Mark(ierr.ErrInternal)
	}

	if req.IdempotencyKey != nil {
		existing, err := s.CommitmentRepo.GetByIdempotencyKey(ctx, *req.IdempotencyKey)
		if err == nil {
			return existing, nil
		}

		if !ierr.IsNotFound(err) {
			return nil, err
		}
	}

	c, cc, _, err := s.prepare(ctx, req, opts)
	if err != nil {
		return nil, err
	}

	meterID := ""
	if lines := resolveScopeLines(c.ScopeEntityType, c.ScopeEntityID, cc, c.StartDate); len(lines) > 0 {
		meterID = lines[0].MeterID
	}

	if c.CommittedPriceID, err = s.createRatePrice(ctx, c, cc.Sub, meterID, req.CommittedRate, "committed"); err != nil {
		return nil, err
	}

	if c.OveragePriceID, err = s.createRatePrice(ctx, c, cc.Sub, meterID, req.OverageRate, "overage"); err != nil {
		return nil, err
	}

	return s.CommitmentRepo.Create(ctx, c)
}

// prepare validates the request and builds the commitment and its value without persisting anything.
func (s *commitmentService) prepare(
	ctx context.Context,
	req *dto.CreateCommitmentRequest,
	opts commitmentCreateOptions,
) (*commitment.Commitment, *commitmentContext, decimal.Decimal, error) {
	enabled, err := s.IsEnabled(ctx)
	if err != nil {
		return nil, nil, decimal.Zero, err
	}

	if !enabled {
		return nil, nil, decimal.Zero, ierr.NewError("commitments are not enabled for this environment").
			WithHint("Enable the commitments_v2 setting").
			Mark(ierr.ErrInvalidOperation)
	}

	cc, err := s.loadCommitmentContext(ctx, req.SubscriptionID)
	if err != nil {
		return nil, nil, decimal.Zero, err
	}

	req.ApplyDefaults(cc.Sub.Timezone)
	if err := req.Validate(); err != nil {
		return nil, nil, decimal.Zero, err
	}

	if err := s.validateAgainstSubscription(ctx, req, cc, opts); err != nil {
		return nil, nil, decimal.Zero, err
	}

	c := req.ToCommitment(ctx, cc.Sub)
	periods, err := commitmentPeriods(c, cc.Sub, lo.FromPtr(req.CommittedRate), bucketFor(c, cc))
	if err != nil {
		return nil, nil, decimal.Zero, err
	}

	return c, cc, commitmentValue(periods), nil
}

// bucketFor returns the shared bucket size of the scoped prices for window measurement, otherwise "".
func bucketFor(c *commitment.Commitment, cc *commitmentContext) types.WindowSize {
	if c.Measurement != types.CommitmentMeasurementWindow {
		return ""
	}

	lines := resolveScopeLines(c.ScopeEntityType, c.ScopeEntityID, cc, c.StartDate)
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
	rate *decimal.Decimal,
	kind string,
) (*string, error) {
	if rate == nil {
		return nil, nil
	}

	resp, err := NewPriceService(s.ServiceParams).CreatePrice(ctx, dto.CreatePriceRequest{
		Amount:               rate,
		Currency:             c.Currency,
		EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:             c.SubscriptionID,
		Type:                 types.PRICE_TYPE_USAGE,
		PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:        sub.BillingPeriod,
		BillingPeriodCount:   sub.BillingPeriodCount,
		BillingModel:         types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:       types.InvoiceCadenceArrear,
		MeterID:              meterID,
		DisplayName:          c.Name + " (" + kind + ")",
		SkipEntityValidation: true,
		Metadata: map[string]string{
			"commitment_id":   c.ID,
			"commitment_rate": kind,
		},
	})
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

	return s.toResponse(ctx, c)
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

		resp, err := s.responseWithContext(ctx, c, cc)
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
		return nil, invalid("name and invoice_label must be 1-255 characters")
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

	return s.toResponse(ctx, updated)
}

// Terminate ends an active commitment at effective_at (default now).
// Settlement and fee lines are added by the final invoice, not here.
func (s *commitmentService) Terminate(
	ctx context.Context,
	id string,
	req dto.TerminateCommitmentRequest,
) (*dto.CommitmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	effectiveAt := lo.FromPtrOr(req.EffectiveAt, time.Now().UTC())
	var out *commitment.Commitment
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		out, err = s.terminate(txCtx, id, effectiveAt)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "commitment terminated",
		"commitment_id", out.ID,
		"subscription_id", out.SubscriptionID,
		"effective_at", effectiveAt,
	)
	return s.toResponse(ctx, out)
}

// TerminateForSubscription terminates every active commitment on the subscription whose term runs past effectiveAt.
func (s *commitmentService) TerminateForSubscription(
	ctx context.Context,
	subscriptionID string,
	effectiveAt time.Time,
) error {
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		f := commitment.NewNoLimitFilter()
		f.SubscriptionIDs = []string{subscriptionID}
		f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive}
		active, err := s.CommitmentRepo.List(txCtx, f)
		if err != nil {
			return err
		}

		for _, c := range active {
			if !effectiveAt.Before(c.EndDate) {
				continue
			}

			if _, err := s.terminate(txCtx, c.ID, effectiveAt); err != nil {
				return err
			}

			s.Logger.Info(ctx, "commitment terminated",
				"commitment_id", c.ID,
				"subscription_id", subscriptionID,
				"effective_at", effectiveAt,
			)
		}

		return nil
	})
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

// terminate re-reads the commitment under its lock and must run inside a transaction.
func (s *commitmentService) terminate(
	ctx context.Context,
	id string,
	effectiveAt time.Time,
) (*commitment.Commitment, error) {
	if err := s.lockCommitment(ctx, id); err != nil {
		return nil, err
	}

	c, err := s.CommitmentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if c.CommitmentStatus != types.CommitmentStatusActive {
		return nil, ierr.NewErrorf("commitment %s is %s", c.ID, c.CommitmentStatus).
			WithHint("Only active commitments can be terminated").
			Mark(ierr.ErrInvalidOperation)
	}

	if !effectiveAt.Before(c.EndDate) {
		return nil, invalidf("effective_at must be before the commitment end %s", c.EndDate.Format(time.RFC3339))
	}

	// Kept as requested, even before start_date: the settlement invoice is chosen from it and nothing is earned.
	terminated := commitment.NewCommitmentBuilder(c).
		WithStatus(types.CommitmentStatusTerminated).
		WithTerminatedAt(effectiveAt).
		Build()
	return s.CommitmentRepo.Update(ctx, terminated)
}

// cancellationFeeAmount prices the fee in the commitment currency; a percent fee applies to the value not yet
// earned at terminatedAt.
func (s *commitmentService) cancellationFeeAmount(
	ctx context.Context,
	c *commitment.Commitment,
	fee *dto.CommitmentCancellationFee,
	terminatedAt time.Time,
) (decimal.Decimal, error) {
	if fee.Type == types.CommitmentCancellationFeeFlat {
		return types.RoundToCurrencyPrecision(fee.Value, c.Currency), nil
	}

	cc, err := s.loadCommitmentContext(ctx, c.SubscriptionID)
	if err != nil {
		return decimal.Zero, err
	}

	total, err := s.responseWithContext(ctx, c, cc)
	if err != nil {
		return decimal.Zero, err
	}

	remaining := *total.CommitmentValue
	if terminatedAt.After(c.StartDate) {
		// The value of a term cut at terminatedAt is what has been earned.
		earnedTerm := *c
		earnedTerm.EndDate = terminatedAt
		earned, err := s.responseWithContext(ctx, &earnedTerm, cc)
		if err != nil {
			return decimal.Zero, err
		}

		remaining = remaining.Sub(*earned.CommitmentValue)
	}

	amount := remaining.
		Mul(fee.Value).
		Div(decimal.NewFromInt(100))
	return types.RoundToCurrencyPrecision(amount, c.Currency), nil
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

func (s *commitmentService) toResponse(ctx context.Context, c *commitment.Commitment) (*dto.CommitmentResponse, error) {
	cc, err := s.loadCommitmentContext(ctx, c.SubscriptionID)
	if err != nil {
		return nil, err
	}

	return s.responseWithContext(ctx, c, cc)
}

// responseWithContext recomputes the commitment value; the committed rate comes from the committed price.
func (s *commitmentService) responseWithContext(
	ctx context.Context,
	c *commitment.Commitment,
	cc *commitmentContext,
) (*dto.CommitmentResponse, error) {
	committedRate := decimal.Zero
	if c.CommittedPriceID != nil {
		p, err := s.PriceRepo.Get(ctx, *c.CommittedPriceID)
		if err != nil {
			return nil, err
		}

		committedRate = p.Amount
	}

	periods, err := commitmentPeriods(c, cc.Sub, committedRate, bucketFor(c, cc))
	if err != nil {
		return nil, err
	}

	value := commitmentValue(periods)
	return &dto.CommitmentResponse{Commitment: c, CommitmentValue: &value}, nil
}

func (s *commitmentService) CheckPriceAttach(
	ctx context.Context,
	sub *subscription.Subscription,
	p *price.Price,
	at time.Time,
) error {
	_, err := s.checkPriceAttach(ctx, sub, p, at)
	return err
}

// checkPriceAttach is CheckPriceAttach that also returns the commitment the price would break.
// It must run inside a transaction.
func (s *commitmentService) checkPriceAttach(
	ctx context.Context,
	sub *subscription.Subscription,
	p *price.Price,
	at time.Time,
) (string, error) {
	if p == nil || p.Type != types.PRICE_TYPE_USAGE {
		return "", nil
	}

	enabled, err := s.IsEnabled(ctx)
	if err != nil || !enabled {
		return "", err
	}

	inScope, err := s.commitmentsInScope(ctx, sub.ID, p, at)
	if err != nil || len(inScope) == 0 {
		return "", err
	}

	// Serialise with createLocked, then re-read so a concurrent create is seen.
	if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: "commitments:" + sub.ID}); err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to lock subscription commitments").
			Mark(ierr.ErrInternal)
	}

	if inScope, err = s.commitmentsInScope(ctx, sub.ID, p, at); err != nil || len(inScope) == 0 {
		return "", err
	}

	if !billsEverySubscriptionPeriod(sub, p.BillingPeriod, p.BillingPeriodCount) {
		return inScope[0].ID, errScopeCadence(p.ID)
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

	return s.checkAttachPairs(ctx, inScope)
}

// commitmentsInScope returns active commitments, still running at `at`, whose scope includes the new usage price.
func (s *commitmentService) commitmentsInScope(
	ctx context.Context,
	subscriptionID string,
	p *price.Price,
	at time.Time,
) ([]*commitment.Commitment, error) {
	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{subscriptionID}
	f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive}
	active, err := s.CommitmentRepo.List(ctx, f)
	if err != nil {
		return nil, err
	}

	// A price-scoped commitment never includes a newly attached price (new ID).
	return lo.Filter(active, func(c *commitment.Commitment, _ int) bool {
		if !at.Before(c.EndDate) {
			return false
		}

		switch c.ScopeEntityType {
		case types.CommitmentScopeSubscription:
			return true
		case types.CommitmentScopeGroup:
			return p.GroupID == c.ScopeEntityID
		case types.CommitmentScopeMeter:
			return p.MeterID == c.ScopeEntityID
		}

		return false
	}), nil
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
		return nil
	}

	if (c.Type == types.COMMITMENT_TYPE_QUANTITY || c.OveragePriceID != nil) && p.MeterID != lines[0].MeterID {
		return invalidf("commitment %s covers a single meter; price %s is on a different meter", c.ID, p.ID)
	}

	if c.Measurement != types.CommitmentMeasurementWindow {
		return nil
	}

	m, err := s.MeterRepo.GetMeter(ctx, p.MeterID)
	if err != nil {
		return err
	}

	want := price.ResolveBucketSize(cc.Prices[lines[0].PriceID], cc.Meters[lines[0].MeterID])
	if got := price.ResolveBucketSize(p, m); got != want {
		return invalidf(
			"commitment %s measures by window with bucket size %q; price %s has bucket size %q",
			c.ID,
			want,
			p.ID,
			got,
		)
	}

	return nil
}

// checkAttachPairs requires commitments that now share the new price and overlap in time to agree on
// type, measurement and overage rate.
func (s *commitmentService) checkAttachPairs(ctx context.Context, inScope []*commitment.Commitment) (string, error) {
	for i, a := range inScope {
		for _, b := range inScope[i+1:] {
			if !a.StartDate.Before(b.EndDate) || !b.StartDate.Before(a.EndDate) {
				continue
			}

			if a.Type != b.Type || a.Measurement != b.Measurement {
				return a.ID, invalidf(
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
				return a.ID, invalidf(
					"commitments %s and %s overlap on the new price but have different overage rates",
					a.ID,
					b.ID,
				)
			}
		}
	}

	return "", nil
}

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

	return pa.Amount.Equal(pb.Amount), nil
}
