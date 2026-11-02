package service

import (
	"context"
	"slices"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

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

	var resp *dto.BillingTermsResponse
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockCommitment(txCtx, id); err != nil {
			return err
		}

		c, err := s.CommitmentRepo.Get(txCtx, id)
		if err != nil {
			return err
		}

		terms, instalments, err := s.planReplacement(txCtx, c, req)
		if err != nil {
			return err
		}

		if _, err := s.BillingTermRepo.Archive(txCtx, types.BillingTermOwnerCommitment, c.ID); err != nil {
			return err
		}

		cancelled, err := s.BillingInstalmentRepo.Cancel(txCtx, types.BillingTermOwnerCommitment, c.ID, []types.InstalmentStatus{
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

	return resp, nil
}

// planReplacement builds new terms from req and expands them against c's unchanged total. It writes nothing.
func (s *commitmentService) planReplacement(
	ctx context.Context,
	c *commitment.Commitment,
	req dto.ReplaceBillingTermsRequest,
) ([]*billingterm.Term, []*billingterm.Instalment, error) {
	if c.CommitmentStatus != types.CommitmentStatusActive {
		return nil, nil, ierr.NewErrorf("commitment %s is %s", c.ID, c.CommitmentStatus).
			WithHint("Only active commitments can have their billing terms edited").
			Mark(ierr.ErrInvalidOperation)
	}

	if lo.FromPtr(c.CollectionMethod) != types.CommitmentCollectionBillingTerms {
		return nil, nil, ierr.NewErrorf("commitment %s does not collect through billing terms", c.ID).
			WithHint("Only commitments with collection_method billing_terms have billing terms").
			Mark(ierr.ErrInvalidOperation)
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

// invoicesToReview returns the distinct invoices the instalments sit on, in order.
func invoicesToReview(instalments []*billingterm.Instalment) []string {
	ids := lo.Uniq(lo.FilterMap(instalments, func(i *billingterm.Instalment, _ int) (string, bool) {
		return lo.FromPtr(i.InvoiceID), i.InvoiceID != nil
	}))
	slices.Sort(ids)

	return ids
}
