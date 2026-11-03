package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

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

		// A draft computed since may already bill it at its current bill_on; an edit cancels it so that draft recomputes.
		if billOn.After(instalment.BillOn) {
			return ierr.Validationf("bill_on: cannot move past the instalment's bill_on %s; edit the billing terms to bill it later",
				instalment.BillOn.Format(time.RFC3339))
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
