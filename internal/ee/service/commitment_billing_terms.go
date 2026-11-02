package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// broughtForwardDescription labels the instalment a bring forward adds.
const broughtForwardDescription = "Payment brought forward"

// planInstalments expands terms into the commitment's instalments, valuing each commitment period with amountOf.
// terms must be new: expanding fills the amount of a percentage term.
func (s *commitmentService) planInstalments(
	ctx context.Context,
	c *commitment.Commitment,
	cc *commitmentContext,
	amountOf func(capacity decimal.Decimal) decimal.Decimal,
	terms []*billingterm.Term,
) ([]*billingterm.Instalment, error) {
	periods, err := commitmentPeriods(c, cc.Sub, amountOf, bucketFor(c, cc), c.StartDate, *c.EndDate)
	if err != nil {
		return nil, err
	}

	values := lo.Map(periods, func(p commitmentPeriod, _ int) billingterm.PeriodValue {
		return billingterm.PeriodValue{
			Period: types.Period{Start: p.Start, End: p.End},
			Amount: p.Amount,
		}
	})

	return billingterm.Expand(ctx, c, values, terms)
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

	req.ApplyDefaults()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	c, err := s.CommitmentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	terms, instalments, err := s.planReplacement(ctx, c, req)
	if err != nil {
		return nil, err
	}

	invoiced, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		OwnerType: types.BillingTermOwnerCommitment,
		OwnerIDs:  []string{c.ID},
		Statuses:  []types.InstalmentStatus{types.InstalmentStatusInvoiced},
	})
	if err != nil {
		return nil, err
	}

	return &dto.BillingTermsResponse{
		BillingTerms:     terms,
		Instalments:      instalments,
		InvoicesToReview: invoicesToReview(invoiced),
	}, nil
}

// ReplaceBillingTerms swaps the commitment's whole set of terms for req's. Every current instalment is cancelled,
// invoiced ones included, and the new terms expand against the unchanged total; invoices are left as they are.
func (s *commitmentService) ReplaceBillingTerms(
	ctx context.Context,
	id string,
	req dto.ReplaceBillingTermsRequest,
) (*dto.BillingTermsResponse, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	req.ApplyDefaults()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var c *commitment.Commitment
	var cancelled []*billingterm.Instalment
	var resp *dto.BillingTermsResponse
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockCommitment(txCtx, id); err != nil {
			return err
		}

		var err error
		if c, err = s.CommitmentRepo.Get(txCtx, id); err != nil {
			return err
		}

		terms, instalments, err := s.planReplacement(txCtx, c, req)
		if err != nil {
			return err
		}

		if _, err := s.BillingTermRepo.Archive(txCtx, types.BillingTermOwnerCommitment, c.ID); err != nil {
			return err
		}

		cancelled, err = s.BillingInstalmentRepo.Cancel(txCtx, types.BillingTermOwnerCommitment, c.ID, []types.InstalmentStatus{
			types.InstalmentStatusScheduled,
			types.InstalmentStatusInvoiced,
		})
		if err != nil {
			return err
		}

		resp = &dto.BillingTermsResponse{InvoicesToReview: invoicesToReview(cancelled)}
		if resp.BillingTerms, err = s.BillingTermRepo.CreateMany(txCtx, terms); err != nil {
			return err
		}

		resp.Instalments, err = s.BillingInstalmentRepo.CreateMany(txCtx, instalments)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "commitment.billing_terms_replaced",
		"operation", "commitment.billing_terms_replaced",
		"commitment_id", id,
		"terms", len(resp.BillingTerms),
		"instalments", len(resp.Instalments),
		"invoices_to_review", len(resp.InvoicesToReview),
	)

	publishBillingTermsEvent(ctx, s.ServiceParams, types.WebhookEventBillingTermsUpdated, c)
	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCancelled, cancelled)
	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCreated, resp.Instalments)

	return resp, nil
}

// planReplacement builds new terms from req and expands them against c's unchanged total. It writes nothing.
func (s *commitmentService) planReplacement(
	ctx context.Context,
	c *commitment.Commitment,
	req dto.ReplaceBillingTermsRequest,
) ([]*billingterm.Term, []*billingterm.Instalment, error) {
	if err := requireActiveBillingTerms(c); err != nil {
		return nil, nil, err
	}

	cc, err := s.loadCommitmentContext(ctx, c.SubscriptionID)
	if err != nil {
		return nil, nil, err
	}

	amountOf, err := s.committedAmountFn(ctx, c)
	if err != nil {
		return nil, nil, err
	}

	terms := lo.Map(req.BillingTerms, func(in dto.BillingTermInput, _ int) *billingterm.Term {
		return in.ToTerm(ctx, c.ID)
	})

	instalments, err := s.planInstalments(ctx, c, cc, amountOf, terms)
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

// invoicesToReview returns the distinct invoices the instalments sit on, in order.
func invoicesToReview(instalments []*billingterm.Instalment) []string {
	ids := lo.Uniq(lo.FilterMap(instalments, func(i *billingterm.Instalment, _ int) (string, bool) {
		return lo.FromPtr(i.InvoiceID), i.InvoiceID != nil
	}))
	slices.Sort(ids)

	return ids
}

// ReplanInstalments replaces the commitment's scheduled instalments with req's, which must add up to what the invoiced
// ones leave of the commitment's total. The response is the resulting plan.
func (s *commitmentService) ReplanInstalments(
	ctx context.Context,
	id string,
	req dto.ReplanInstalmentsRequest,
) (*dto.ListInstalmentsResponse, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	req.ApplyDefaults()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var cancelled, created []*billingterm.Instalment
	var plan *dto.ListInstalmentsResponse
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockCommitment(txCtx, id); err != nil {
			return err
		}

		c, err := s.CommitmentRepo.Get(txCtx, id)
		if err != nil {
			return err
		}

		if err := requireActiveBillingTerms(c); err != nil {
			return err
		}

		replanned, err := replannedInstalments(txCtx, c, req.Instalments)
		if err != nil {
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

		billed, planned, total := sumInstalments(invoiced.Items), sumInstalments(replanned), c.CollectionTotal()
		if !billed.Add(planned).Equal(total) {
			return ierr.Validationf(
				"instalments: the invoiced %s and the new %s add up to %s, not the commitment total of %s",
				billed, planned, billed.Add(planned), total,
			)
		}

		if created, err = s.BillingInstalmentRepo.CreateMany(txCtx, replanned); err != nil {
			return err
		}

		plan, err = s.ListInstalments(txCtx, c.ID, []types.InstalmentStatus{
			types.InstalmentStatusScheduled,
			types.InstalmentStatusInvoiced,
		})
		return err
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "commitment.instalments_replanned",
		"operation", "commitment.instalments_replanned",
		"commitment_id", id,
		"cancelled", len(cancelled),
		"created", len(created),
	)

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCancelled, cancelled)
	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCreated, created)

	return plan, nil
}

// replannedInstalments builds the instalments a re-plan creates, in billing order. Each covers the whole commitment
// term; a manual one waits at the end of it.
func replannedInstalments(
	ctx context.Context,
	c *commitment.Commitment,
	inputs []dto.ReplanInstalmentInput,
) ([]*billingterm.Instalment, error) {
	term := c.CollectionPeriod()
	instalments := make([]*billingterm.Instalment, 0, len(inputs))
	for n, in := range inputs {
		if !in.Amount.Equal(types.RoundToCurrencyPrecision(in.Amount, c.Currency)) {
			return nil, ierr.Validationf("instalments[%d].amount: must be rounded to %s precision", n, c.Currency)
		}

		billOn := lo.FromPtrOr(in.BillOn, term.End)
		if billOn.Before(term.Start) || billOn.After(term.End) {
			return nil, ierr.Validationf("instalments[%d].bill_on: must fall within the commitment term", n)
		}

		instalment := billingterm.InstalmentTemplate(ctx, c)
		instalment.BillTrigger = in.Trigger()
		instalment.BillOn = billOn
		instalment.Amount = in.Amount
		instalment.Description = lo.CoalesceOrEmpty(lo.FromPtr(in.Description), billingterm.DefaultOnceDescription)
		instalment.ServicePeriodStart = term.Start
		instalment.ServicePeriodEnd = term.End
		instalments = append(instalments, &instalment)
	}

	// IDs are time-sortable, so generating them after a stable sort keeps the request's order among equal dates.
	slices.SortStableFunc(instalments, func(a, b *billingterm.Instalment) int { return a.BillOn.Compare(b.BillOn) })
	for _, instalment := range instalments {
		instalment.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_INSTALMENT)
	}

	return instalments, nil
}

// BringForwardInstalments adds one instalment of req.Amount and takes the same amount off the earliest scheduled
// ones, which it cancels, re-creating the part of each that is left. The response is the resulting plan.
func (s *commitmentService) BringForwardInstalments(
	ctx context.Context,
	id string,
	req dto.BringForwardInstalmentsRequest,
) (*dto.ListInstalmentsResponse, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	req.ApplyDefaults()
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var cancelled, created []*billingterm.Instalment
	var plan *dto.ListInstalmentsResponse
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockCommitment(txCtx, id); err != nil {
			return err
		}

		c, err := s.CommitmentRepo.Get(txCtx, id)
		if err != nil {
			return err
		}

		if err := requireActiveBillingTerms(c); err != nil {
			return err
		}

		if !req.Amount.Equal(types.RoundToCurrencyPrecision(req.Amount, c.Currency)) {
			return ierr.Validationf("amount: must be rounded to %s precision", c.Currency)
		}

		term := c.CollectionPeriod()
		billOn := lo.FromPtrOr(req.BillOn, clock.Now())
		if billOn.Before(term.Start) || billOn.After(term.End) {
			return ierr.Validation("bill_on: must fall within the commitment term")
		}

		scheduled, err := s.ListInstalments(txCtx, c.ID, []types.InstalmentStatus{types.InstalmentStatusScheduled})
		if err != nil {
			return err
		}

		if available := sumInstalments(scheduled.Items); req.Amount.GreaterThan(available) {
			return ierr.Validationf("amount: %s is more than the %s still scheduled", req.Amount, available)
		}

		var touched, replacements []*billingterm.Instalment
		remaining := req.Amount
		for _, instalment := range scheduled.Items {
			if !remaining.IsPositive() {
				break
			}

			taken := decimal.Min(instalment.Amount, remaining)
			remaining = remaining.Sub(taken)

			// An amount is never changed in place: a draft invoice computed earlier may still carry the instalment,
			// and finalizing it must fail instead of billing the old amount. A cancelled instalment keeps its amount.
			cancelledRow, err := s.BillingInstalmentRepo.Update(txCtx,
				billingterm.NewInstalmentBuilder(instalment).WithStatus(types.InstalmentStatusCancelled).Build())
			if err != nil {
				return err
			}

			touched = append(touched, instalment)
			cancelled = append(cancelled, cancelledRow)

			if left := instalment.Amount.Sub(taken); left.IsPositive() {
				replacement := billingterm.InstalmentTemplate(txCtx, c)
				replacement.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_INSTALMENT)
				replacement.BillingTermID = instalment.BillingTermID
				replacement.BillTrigger = instalment.BillTrigger
				replacement.BillOn = instalment.BillOn
				replacement.TriggeredAt = instalment.TriggeredAt
				replacement.Amount = left
				replacement.Description = instalment.Description
				replacement.ServicePeriodStart = instalment.ServicePeriodStart
				replacement.ServicePeriodEnd = instalment.ServicePeriodEnd
				replacement.Metadata = instalment.Metadata
				replacements = append(replacements, &replacement)
			}
		}

		broughtForward := billingterm.InstalmentTemplate(txCtx, c)
		broughtForward.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_INSTALMENT)
		broughtForward.BillTrigger = types.InstalmentBillTriggerDate
		broughtForward.BillOn = billOn
		broughtForward.Amount = req.Amount
		broughtForward.Description = broughtForwardDescription
		broughtForward.ServicePeriodStart = lo.MinBy(touched, func(a, b *billingterm.Instalment) bool {
			return a.ServicePeriodStart.Before(b.ServicePeriodStart)
		}).ServicePeriodStart
		broughtForward.ServicePeriodEnd = lo.MaxBy(touched, func(a, b *billingterm.Instalment) bool {
			return a.ServicePeriodEnd.After(b.ServicePeriodEnd)
		}).ServicePeriodEnd

		if created, err = s.BillingInstalmentRepo.CreateMany(txCtx, append(replacements, &broughtForward)); err != nil {
			return err
		}

		plan, err = s.ListInstalments(txCtx, c.ID, []types.InstalmentStatus{
			types.InstalmentStatusScheduled,
			types.InstalmentStatusInvoiced,
		})
		return err
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "commitment.instalments_brought_forward",
		"operation", "commitment.instalments_brought_forward",
		"commitment_id", id,
		"amount", req.Amount.String(),
		"cancelled", len(cancelled),
		"created", len(created),
	)

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCancelled, cancelled)
	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCreated, created)

	return plan, nil
}

// sumInstalments adds up the amounts of the instalments.
func sumInstalments(instalments []*billingterm.Instalment) decimal.Decimal {
	return decimal.Sum(decimal.Zero, lo.Map(instalments, func(instalment *billingterm.Instalment, _ int) decimal.Decimal {
		return instalment.Amount
	})...)
}

func (s *commitmentService) BillDueInstalments(ctx context.Context) error {
	configs, err := s.SettingsRepo.ListAllTenantEnvSettingsByKey(ctx, types.SettingKeyCommitmentsV2)
	if err != nil {
		return err
	}

	var errs []error
	for _, env := range configs {
		cfg, err := utils.ToStruct[types.CommitmentsV2Config](env.Config)
		if err != nil || !cfg.Enabled {
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
				"subscription_id", instalment.SubscriptionID,
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

// billInstalment issues the instalment's one-off invoice under its commitment's lock. Finalizing the invoice claims
// the instalment, so it returns nil when the instalment was claimed or cancelled since it was listed, or when its
// subscription is not active (draft, incomplete, trialing, paused) and the instalment waits for a later run.
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

		sub, err := s.SubRepo.Get(txCtx, instalment.SubscriptionID)
		if err != nil {
			return err
		}

		if sub.SubscriptionStatus != types.SubscriptionStatusActive {
			return nil
		}

		c, err := s.CommitmentRepo.Get(txCtx, instalment.OwnerID)
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
