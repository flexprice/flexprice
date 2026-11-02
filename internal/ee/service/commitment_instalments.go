package service

import (
	"context"
	"slices"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// broughtForwardDescription labels the instalment a bring forward adds.
const broughtForwardDescription = "Payment brought forward"

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

// TriggerInstalment sets when a manual instalment is billed: req.BillOn, now by default. The instalment stays scheduled
// and goes on the first invoice at or after that date.
func (s *commitmentService) TriggerInstalment(
	ctx context.Context,
	id, instalmentID string,
	req dto.TriggerInstalmentRequest,
) (*billingterm.Instalment, error) {
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	req.ApplyDefaults()

	var triggered *billingterm.Instalment
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

		instalment, err := s.BillingInstalmentRepo.Get(txCtx, instalmentID)
		if err != nil {
			return err
		}

		if instalment.OwnerType != types.BillingTermOwnerCommitment || instalment.OwnerID != c.ID {
			return ierr.NewErrorf("instalment %s does not belong to commitment %s", instalmentID, c.ID).
				WithHintf("Billing instalment %s not found", instalmentID).
				Mark(ierr.ErrNotFound)
		}

		if instalment.BillTrigger != types.InstalmentBillTriggerManual {
			return ierr.NewErrorf("instalment %s is billed by %s", instalment.ID, instalment.BillTrigger).
				WithHint("Only a manual instalment can be triggered").
				Mark(ierr.ErrInvalidOperation)
		}

		if instalment.InstalmentStatus != types.InstalmentStatusScheduled {
			return ierr.NewErrorf("instalment %s is %s", instalment.ID, instalment.InstalmentStatus).
				WithHint("Only a scheduled instalment can be triggered").
				Mark(ierr.ErrInvalidOperation)
		}

		term := c.CollectionPeriod()
		billOn := lo.FromPtrOr(req.BillOn, clock.Now())
		if billOn.Before(term.Start) || billOn.After(term.End) {
			return ierr.Validation("bill_on: must fall within the commitment term")
		}

		triggered, err = s.BillingInstalmentRepo.Update(txCtx,
			billingterm.NewInstalmentBuilder(instalment).WithBillOn(billOn).WithTriggeredAt(clock.Now()).Build())
		return err
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "commitment.instalment_triggered",
		"operation", "commitment.instalment_triggered",
		"commitment_id", id,
		"instalment_id", triggered.ID,
		"bill_on", triggered.BillOn,
	)

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentUpdated, []*billingterm.Instalment{triggered})

	return triggered, nil
}

// sumInstalments adds up the amounts of the instalments.
func sumInstalments(instalments []*billingterm.Instalment) decimal.Decimal {
	return decimal.Sum(decimal.Zero, lo.Map(instalments, func(instalment *billingterm.Instalment, _ int) decimal.Decimal {
		return instalment.Amount
	})...)
}
