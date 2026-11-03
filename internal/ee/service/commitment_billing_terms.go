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
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// planInstalments expands terms into the commitment's instalments, valuing each commitment period with amountOf.
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

// settlementLabel is the invoice label of the instalment that settles a terminated commitment.
const settlementLabel = "Settlement"

// settle cancels the commitment's scheduled instalments and, when it earned more by effectiveAt than its invoiced
// instalments bill, creates a settlement instalment for the difference and returns it. Nothing is refunded when they
// bill more. It must run inside the terminate transaction, under the commitment lock.
func (s *commitmentService) settle(
	ctx context.Context,
	c *commitment.Commitment,
	effectiveAt time.Time,
) (*billingterm.Instalment, error) {
	cc, err := s.loadCommitmentContext(ctx, c.SubscriptionID)
	if err != nil {
		return nil, err
	}

	amountOf, err := s.committedAmountFn(ctx, c)
	if err != nil {
		return nil, err
	}

	earned, err := earnedAmount(c, cc.Sub, amountOf, bucketFor(c, cc), effectiveAt)
	if err != nil {
		return nil, err
	}

	// Cancelling first fixes what is invoiced: a finalize cannot claim a cancelled instalment.
	scheduled := []types.InstalmentStatus{types.InstalmentStatusScheduled}
	cancelled, err := s.BillingInstalmentRepo.Cancel(ctx, types.BillingTermOwnerCommitment, c.ID, scheduled)
	if err != nil {
		return nil, err
	}

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentCancelled, cancelled)

	invoiced, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		OwnerType: types.BillingTermOwnerCommitment,
		OwnerIDs:  []string{c.ID},
		Statuses:  []types.InstalmentStatus{types.InstalmentStatusInvoiced},
	})
	if err != nil {
		return nil, err
	}

	unbilled := earned.Sub(sumInstalments(invoiced))
	if !unbilled.IsPositive() {
		return nil, nil
	}

	settlement := billingterm.InstalmentTemplate(ctx, c)
	settlement.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_INSTALMENT)
	settlement.Kind = types.InstalmentKindSettlement
	settlement.BillTrigger = types.InstalmentBillTriggerDate
	settlement.BillOn = effectiveAt
	settlement.Amount = unbilled
	settlement.InvoiceLabel = settlementLabel
	settlement.ServicePeriodStart = c.StartDate
	settlement.ServicePeriodEnd = effectiveAt
	settlement.Delivery = types.InstalmentDeliverySeparateInvoice

	created, err := s.BillingInstalmentRepo.CreateMany(ctx, []*billingterm.Instalment{&settlement})
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

	_, err = s.billTermination(ctx, c, end, nil, settlement)
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
			s.Logger.Info(ctx, "skipping instalment whose subscription no longer exists",
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
			s.Logger.Info(ctx, "skipping instalment whose commitment no longer exists",
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
