package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// planInstalments expands terms into new instalments for what the invoiced ones leave of the total, numbered after
// them; undated terms start at the period holding from, or at the commitment start once from is past its end.
func (s *commitmentService) planInstalments(
	ctx context.Context,
	c *commitment.Commitment,
	cc *commitmentContext,
	amountOf amountFn,
	terms []*billingterm.Term,
	invoiced []*billingterm.Instalment,
	from time.Time,
) ([]*billingterm.Instalment, error) {
	left := c.CollectionTotal().Sub(sumInstalments(invoiced))
	if !left.IsPositive() {
		return nil, ierr.Validationf("billing_terms: the commitment is fully invoiced; nothing is left to plan")
	}

	bucket := bucketFor(c, cc)
	periods, err := commitmentPeriods(c, cc.Sub, amountOf, bucket, c.CollectionPeriod())
	if err != nil {
		return nil, err
	}

	values := billingterm.Periods{
		Billing: lo.Map(periods, func(p commitmentPeriod, _ int) billingterm.PeriodValue {
			return billingterm.PeriodValue{
				Period: types.Period{Start: p.Start, End: p.End},
				Amount: p.Amount,
			}
		}),
	}
	if stepsOnAnniversaries(c, terms) {
		if values.Anniversary, err = anniversaryPeriods(c, cc.Sub, periods, amountOf, bucket); err != nil {
			return nil, err
		}
	}

	term := c.CollectionPeriod()
	owner := plannedOwner{
		Commitment: c,
		left:       left,
		termStart:  term.Start,
	}
	if from.After(term.Start) && from.Before(term.End) {
		owner.termStart = from
	}

	instalments, err := billingterm.Expand(ctx, owner, values, terms)
	if err != nil {
		return nil, err
	}

	if count := len(invoiced) + len(instalments); count > billingterm.MaxInstalments {
		return nil, ierr.Validationf("billing_terms: with the %d invoiced instalments the plan holds %d, more than the %d allowed",
			len(invoiced), count, billingterm.MaxInstalments)
	}

	// Invoiced instalments keep the numbers they were billed with and are billed before any new one, so the new ones
	// are numbered after them, in billing order.
	numbered, err := s.countNumbered(ctx, invoiced)
	if err != nil {
		return nil, err
	}

	planned := slices.Clone(instalments)
	slices.SortStableFunc(planned, func(a, b *billingterm.Instalment) int { return a.BillOn.Compare(b.BillOn) })
	billingterm.NumberLabels(planned, numbered)

	return instalments, nil
}

// countNumbered counts the instalments whose label took a number: those whose term's label template uses one.
func (s *commitmentService) countNumbered(ctx context.Context, instalments []*billingterm.Instalment) (int, error) {
	ids := lo.Uniq(lo.FilterMap(instalments, func(i *billingterm.Instalment, _ int) (string, bool) {
		return lo.FromPtr(i.BillingTermID), i.BillingTermID != nil
	}))
	if len(ids) == 0 {
		return 0, nil
	}

	terms, err := s.BillingTermRepo.ListByIDs(ctx, ids)
	if err != nil {
		return 0, err
	}

	termByID := lo.KeyBy(terms, func(t *billingterm.Term) string { return t.ID })
	return lo.CountBy(instalments, func(i *billingterm.Instalment) bool {
		t, ok := termByID[lo.FromPtr(i.BillingTermID)]
		return ok && billingterm.IsNumbered(t.LabelTemplate())
	}), nil
}

// anniversaryPeriods cuts the commitment's term into billing-period lengths counted from its start, each valued by the
// parts of the commitment periods it spans.
func anniversaryPeriods(
	c *commitment.Commitment,
	sub *subscription.Subscription,
	periods []commitmentPeriod,
	amountOf amountFn,
	bucket types.WindowSize,
) ([]billingterm.PeriodValue, error) {
	term := c.CollectionPeriod()
	steps, err := types.CalculateBillingPeriods(&types.CalculateBillingPeriodsParams{
		InitialPeriodStart: term.Start,
		EndDate:            &term.End,
		Anchor:             term.Start,
		PeriodCount:        sub.BillingPeriodCount,
		BillingPeriod:      sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to compute the commitment's anniversary periods").
			Mark(ierr.ErrInternal)
	}

	out := make([]billingterm.PeriodValue, 0, len(steps))
	for _, step := range steps {
		step.End = minTime(step.End, term.End)
		if !step.Start.Before(step.End) {
			continue
		}

		amount := decimal.Zero
		for _, p := range periods {
			if p.Start.Before(step.End) && p.End.After(step.Start) {
				amount = amount.Add(clipCommitmentPeriod(c, p, step, amountOf, bucket, sub.BillingAnchor).Amount)
			}
		}

		out = append(out, billingterm.PeriodValue{Period: step, Amount: amount})
	}

	return out, nil
}

// stepsOnAnniversaries reports whether a recurring term among terms is invoiced separately, so steps on anniversary
// periods.
func stepsOnAnniversaries(c *commitment.Commitment, terms []*billingterm.Term) bool {
	return lo.SomeBy(terms, func(t *billingterm.Term) bool {
		return t.Kind == types.BillingTermKindRecurring &&
			lo.FromPtrOr(t.InstalmentInvoicing, c.InstalmentDelivery()) == types.InstalmentDeliverySeparateInvoice
	})
}

// plannedOwner is a commitment as a new plan sees it: its instalments collect only what the invoiced ones leave of the
// total, and its undated terms start at the period holding termStart.
type plannedOwner struct {
	*commitment.Commitment
	left      decimal.Decimal
	termStart time.Time
}

func (o plannedOwner) CollectionTotal() decimal.Decimal { return o.left }

func (o plannedOwner) TermStart() time.Time { return o.termStart }

// sumInstalments adds up the amounts of the instalments.
func sumInstalments(instalments []*billingterm.Instalment) decimal.Decimal {
	return decimal.Sum(decimal.Zero, lo.Map(instalments, func(instalment *billingterm.Instalment, _ int) decimal.Decimal {
		return instalment.Amount
	})...)
}

func (s *commitmentService) GetBillingTerms(ctx context.Context, id string) (*dto.BillingTermsResponse, error) {
	c, err := s.CommitmentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	terms, err := s.BillingTermRepo.List(ctx, types.BillingTermOwnerCommitment, c.ID)
	if err != nil {
		return nil, err
	}

	// Cancelled instalments belong to replaced terms or to history; ListInstalments returns them.
	instalments, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		OwnerType: types.BillingTermOwnerCommitment,
		OwnerIDs:  []string{c.ID},
		Statuses:  []types.InstalmentStatus{types.InstalmentStatusScheduled, types.InstalmentStatusInvoiced},
	})
	if err != nil {
		return nil, err
	}

	return &dto.BillingTermsResponse{BillingTerms: terms, Instalments: instalments}, nil
}

func (s *commitmentService) ListInstalments(
	ctx context.Context,
	id string,
	statuses []types.InstalmentStatus,
) (*dto.ListInstalmentsResponse, error) {
	c, err := s.CommitmentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	instalments, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		OwnerType: types.BillingTermOwnerCommitment,
		OwnerIDs:  []string{c.ID},
		Statuses:  statuses,
	})
	if err != nil {
		return nil, err
	}

	return &dto.ListInstalmentsResponse{Items: instalments}, nil
}

func (s *commitmentService) GetInstalment(ctx context.Context, instalmentID string) (*billingterm.Instalment, error) {
	return s.BillingInstalmentRepo.Get(ctx, instalmentID)
}

func (s *commitmentService) PreviewBillingTerms(
	ctx context.Context,
	id string,
	req dto.ReplaceBillingTermsRequest,
) (*dto.BillingTermsResponse, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	c, err := s.CommitmentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := requireActiveBillingTerms(c); err != nil {
		return nil, err
	}

	invoiced, err := s.ListInstalments(ctx, c.ID, []types.InstalmentStatus{types.InstalmentStatusInvoiced})
	if err != nil {
		return nil, err
	}

	terms, instalments, err := s.planReplacement(ctx, c, req, invoiced.Items)
	if err != nil {
		return nil, err
	}

	plan := append(slices.Clone(invoiced.Items), instalments...)
	slices.SortStableFunc(plan, func(a, b *billingterm.Instalment) int { return a.BillOn.Compare(b.BillOn) })

	return &dto.BillingTermsResponse{BillingTerms: terms, Instalments: plan}, nil
}

// ReplaceBillingTerms swaps the commitment's whole set of terms for req's. The scheduled instalments are cancelled and
// the new terms expand against what the invoiced ones, which are kept, leave of the total.
func (s *commitmentService) ReplaceBillingTerms(
	ctx context.Context,
	id string,
	req dto.ReplaceBillingTermsRequest,
) (*dto.BillingTermsResponse, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	var c *commitment.Commitment
	var cancelled, created []*billingterm.Instalment
	var resp *dto.BillingTermsResponse
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockCommitment(txCtx, id); err != nil {
			return err
		}

		var err error
		if c, err = s.CommitmentRepo.Get(txCtx, id); err != nil {
			return err
		}

		if err := requireActiveBillingTerms(c); err != nil {
			return err
		}

		if err := NewInvoiceService(s.ServiceParams).(*invoiceService).requireNoDraftPayment(txCtx, c.ID); err != nil {
			return err
		}

		// Cancelling first fixes what is invoiced: a finalize cannot claim a cancelled instalment.
		scheduled := []types.InstalmentStatus{types.InstalmentStatusScheduled}
		if cancelled, err = s.BillingInstalmentRepo.Cancel(txCtx, types.BillingTermOwnerCommitment, c.ID, scheduled); err != nil {
			return err
		}

		invoiced, err := s.ListInstalments(txCtx, c.ID, []types.InstalmentStatus{types.InstalmentStatusInvoiced})
		if err != nil {
			return err
		}

		terms, instalments, err := s.planReplacement(txCtx, c, req, invoiced.Items)
		if err != nil {
			return err
		}

		if _, err := s.BillingTermRepo.Archive(txCtx, types.BillingTermOwnerCommitment, c.ID); err != nil {
			return err
		}

		resp = &dto.BillingTermsResponse{}
		if resp.BillingTerms, err = s.BillingTermRepo.CreateMany(txCtx, terms); err != nil {
			return err
		}

		if created, err = s.BillingInstalmentRepo.CreateMany(txCtx, instalments); err != nil {
			return err
		}

		plan, err := s.ListInstalments(txCtx, c.ID, []types.InstalmentStatus{
			types.InstalmentStatusScheduled,
			types.InstalmentStatusInvoiced,
		})
		if err != nil {
			return err
		}

		resp.Instalments = plan.Items
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "commitment.billing_terms_replaced",
		"operation", "commitment.billing_terms_replaced",
		"commitment_id", id,
		"terms", len(resp.BillingTerms),
		"cancelled", len(cancelled),
		"created", len(created),
	)

	publishBillingTermsEvent(ctx, s.ServiceParams, types.WebhookEventBillingTermsUpdated, c)
	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCancelled, cancelled)
	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCreated, created)

	return resp, nil
}

// planReplacement builds new terms from req and expands them against what the invoiced instalments leave of c's
// total; terms without dates start at the current billing period. It writes nothing.
func (s *commitmentService) planReplacement(
	ctx context.Context,
	c *commitment.Commitment,
	req dto.ReplaceBillingTermsRequest,
	invoiced []*billingterm.Instalment,
) ([]*billingterm.Term, []*billingterm.Instalment, error) {
	cc, err := s.loadCommitmentContext(ctx, c.SubscriptionID)
	if err != nil {
		return nil, nil, err
	}

	amountOf, err := s.committedAmountFn(ctx, c)
	if err != nil {
		return nil, nil, err
	}

	if err := alignTermDates(cc.Sub, req.BillingTerms); err != nil {
		return nil, nil, err
	}

	terms := lo.Map(req.BillingTerms, func(in dto.BillingTermInput, _ int) *billingterm.Term {
		return in.ToTerm(ctx, c.ID)
	})

	instalments, err := s.planInstalments(ctx, c, cc, amountOf, terms, invoiced, clock.Now())
	if err != nil {
		return nil, nil, err
	}

	return terms, instalments, nil
}

// requireActiveBillingTerms rejects a commitment whose plan cannot change: one that is not active or does not collect
// through billing terms.
func requireActiveBillingTerms(c *commitment.Commitment) error {
	if c.CommitmentStatus != types.CommitmentStatusActive {
		return ierr.NewErrorf("commitment %s is %s", c.ID, c.CommitmentStatus).
			WithHint("Only active commitments can have their billing terms or instalments changed").
			Mark(ierr.ErrInvalidOperation)
	}

	if lo.FromPtr(c.CollectionMethod) != types.CommitmentCollectionBillingTerms {
		return ierr.NewErrorf("commitment %s does not collect through billing terms", c.ID).
			WithHint("Only commitments with collection_method billing_terms have billing terms or instalments").
			Mark(ierr.ErrInvalidOperation)
	}

	return nil
}

// settlementLabel is the invoice label of the instalment that settles a terminated commitment.
const settlementLabel = "Settlement"

// settle cancels the commitment's scheduled instalments and returns a settlement instalment billing those already due
// by effectiveAt, or nil when none were. Later instalments are not billed and nothing is refunded; anything more is the
// cancellation fee. It must run inside the terminate transaction, under the commitment lock.
func (s *commitmentService) settle(
	ctx context.Context,
	c *commitment.Commitment,
	effectiveAt time.Time,
) (*billingterm.Instalment, error) {
	// Cancelling first fixes what is invoiced: a finalize cannot claim a cancelled instalment.
	scheduled := []types.InstalmentStatus{types.InstalmentStatusScheduled}
	cancelled, err := s.BillingInstalmentRepo.Cancel(ctx, types.BillingTermOwnerCommitment, c.ID, scheduled)
	if err != nil {
		return nil, err
	}

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCancelled, cancelled)

	due := lo.Filter(cancelled, func(inst *billingterm.Instalment, _ int) bool { return !inst.BillOn.After(effectiveAt) })
	if len(due) == 0 {
		return nil, nil
	}

	first := lo.MinBy(due, func(a, b *billingterm.Instalment) bool { return a.ServicePeriodStart.Before(b.ServicePeriodStart) })
	last := lo.MaxBy(due, func(a, b *billingterm.Instalment) bool { return a.ServicePeriodEnd.After(b.ServicePeriodEnd) })
	settlement := billingterm.NewOwnerInstalment(ctx, c).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_INSTALMENT)).
		WithKind(types.InstalmentKindSettlement).
		WithDelivery(types.InstalmentDeliverySeparateInvoice).
		WithBillOn(effectiveAt).
		WithAmount(sumInstalments(due)).
		WithInvoiceLabel(settlementLabel).
		WithServicePeriod(types.Period{Start: first.ServicePeriodStart, End: last.ServicePeriodEnd}).
		Build()

	created, err := s.BillingInstalmentRepo.CreateMany(ctx, []*billingterm.Instalment{settlement})
	if err != nil {
		return nil, err
	}

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCreated, created)
	return created[0], nil
}

// settleEnded bills the instalments still scheduled on a billing-terms commitment that ended before its subscription
// was cancelled, since no later subscription invoice will carry them. It must run inside the cancel transaction.
func (s *commitmentService) settleEnded(ctx context.Context, c *commitment.Commitment) error {
	if lo.FromPtr(c.CollectionMethod) != types.CommitmentCollectionBillingTerms {
		return nil
	}

	if err := s.lockCommitment(ctx, c.ID); err != nil {
		return err
	}

	scheduled, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		OwnerType: types.BillingTermOwnerCommitment,
		OwnerIDs:  []string{c.ID},
		Statuses:  []types.InstalmentStatus{types.InstalmentStatusScheduled},
	})
	if err != nil || len(scheduled) == 0 {
		return err
	}

	end := *c.EffectiveEnd()
	settlement, err := s.settle(ctx, c, end)
	if err != nil || settlement == nil {
		return err
	}

	sub, err := s.SubRepo.Get(ctx, c.SubscriptionID)
	if err != nil {
		return err
	}

	_, err = s.billTermination(ctx, c, sub, end, decimal.Zero, settlement)
	return err
}

func (s *commitmentService) BillDueInstalments(ctx context.Context) error {
	configs, err := s.SettingsRepo.ListAllTenantEnvSettingsByKey(ctx, types.SettingKeyCommitmentsV2)
	if err != nil {
		return err
	}

	var errs []error
	for _, env := range configs {
		cfg, err := utils.ToStruct[types.CommitmentsV2Config](env.Config)
		if err != nil {
			errs = append(errs, fmt.Errorf("decoding commitments_v2 setting for environment %s: %w", env.EnvironmentID, err))
			continue
		}
		if !cfg.Enabled {
			continue
		}

		envCtx := types.SetEnvironmentID(types.SetTenantID(ctx, env.TenantID), env.EnvironmentID)
		errs = append(errs, s.billEnvironmentInstalments(envCtx))
	}

	return errors.Join(errs...)
}

// billEnvironmentInstalments bills the environment's due separate-invoice instalments one at a time, so a failure
// leaves the others billed and its own instalment scheduled for the next run.
func (s *commitmentService) billEnvironmentInstalments(ctx context.Context) error {
	dueBy := clock.Now()
	due, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		OwnerType:      types.BillingTermOwnerCommitment,
		Statuses:       []types.InstalmentStatus{types.InstalmentStatusScheduled},
		Deliveries:     []types.InstalmentDelivery{types.InstalmentDeliverySeparateInvoice},
		BillOnOrBefore: &dueBy,
	})
	if err != nil {
		return err
	}

	var errs []error
	for _, instalment := range due {
		inv, err := s.billInstalment(ctx, instalment)
		if err != nil {
			s.Logger.Error(ctx, "failed to bill separate-invoice instalment",
				"error", err,
				"instalment_id", instalment.ID,
				"commitment_id", instalment.OwnerID,
				"subscription_id", lo.FromPtr(instalment.SubscriptionID),
			)
			errs = append(errs, fmt.Errorf("billing instalment %s: %w", instalment.ID, err))
			continue
		}

		if inv != nil {
			s.Logger.Info(ctx, "commitment.instalment_invoiced",
				"operation", "commitment.instalment_invoiced",
				"instalment_id", instalment.ID,
				"commitment_id", instalment.OwnerID,
				"invoice_id", inv.ID,
			)
		}
	}

	return errors.Join(errs...)
}

// billInstalment issues the instalment's one-off invoice under its commitment's lock; finalizing it claims the
// instalment. It returns nil when the instalment was claimed or cancelled since it was listed, when its subscription
// or commitment no longer exists, or when it is not a settlement and its subscription is not active (draft,
// incomplete, trialing, paused), so it waits for a later run.
func (s *commitmentService) billInstalment(ctx context.Context, listed *billingterm.Instalment) (*dto.InvoiceResponse, error) {
	var inv *dto.InvoiceResponse
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockCommitment(txCtx, listed.OwnerID); err != nil {
			return err
		}

		instalment, err := s.BillingInstalmentRepo.Get(txCtx, listed.ID)
		if err != nil {
			return err
		}

		if instalment.InstalmentStatus != types.InstalmentStatusScheduled {
			return nil
		}

		sub, err := s.SubRepo.Get(txCtx, lo.FromPtr(instalment.SubscriptionID))
		if ierr.IsNotFound(err) {
			s.Logger.Info(ctx, "commitment.instalment_skipped",
				"operation", "commitment.instalment_skipped",
				"reason", "subscription no longer exists",
				"instalment_id", instalment.ID,
				"subscription_id", lo.FromPtr(instalment.SubscriptionID),
			)
			return nil
		}
		if err != nil {
			return err
		}

		if instalment.Kind == types.InstalmentKindInstalment && sub.SubscriptionStatus != types.SubscriptionStatusActive {
			return nil
		}

		c, err := s.CommitmentRepo.Get(txCtx, instalment.OwnerID)
		if ierr.IsNotFound(err) {
			s.Logger.Info(ctx, "commitment.instalment_skipped",
				"operation", "commitment.instalment_skipped",
				"reason", "commitment no longer exists",
				"instalment_id", instalment.ID,
				"commitment_id", instalment.OwnerID,
			)
			return nil
		}
		if err != nil {
			return err
		}

		billingPeriod := string(sub.BillingPeriod)
		inv, err = NewInvoiceService(s.ServiceParams).CreateInvoice(txCtx, dto.CreateInvoiceRequest{
			CustomerID:     sub.GetInvoicingCustomerID(),
			SubscriptionID: lo.ToPtr(sub.ID),
			InvoiceType:    types.InvoiceTypeOneOff,
			Currency:       instalment.Currency,
			BillingReason:  types.InvoiceBillingReasonBillingTerms,
			AmountDue:      instalment.Amount,
			Total:          instalment.Amount,
			Subtotal:       instalment.Amount,
			PeriodStart:    lo.ToPtr(instalment.ServicePeriodStart),
			PeriodEnd:      lo.ToPtr(instalment.ServicePeriodEnd),
			BillingPeriod:  &billingPeriod,
			// A reschedule changes updated_at, so a voided instalment bills again under a new key.
			IdempotencyKey: lo.ToPtr(fmt.Sprintf("billing_instalment_%s_%d", instalment.ID, instalment.UpdatedAt.UnixMicro())),
			LineItems:      []dto.CreateInvoiceLineItemRequest{instalmentLine(c, instalment)},
		})
		return err
	})

	return inv, err
}
