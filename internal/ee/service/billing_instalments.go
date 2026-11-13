package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// instalmentBillAt is the bill_on up to which an invoice computed at referencePoint bills instalments: the invoice
// issued at a boundary bills those due by it, and ok is false for the other reference points, which bill none.
func instalmentBillAt(referencePoint types.InvoiceReferencePoint, periodStart, periodEnd time.Time) (time.Time, bool) {
	switch referencePoint {
	case types.ReferencePointPeriodStart:
		return periodStart, true
	case types.ReferencePointPeriodEnd, types.ReferencePointPreview, types.ReferencePointInternalPreview:
		return periodEnd, true
	default:
		return time.Time{}, false
	}
}

// dueInstalments returns the subscriptions' scheduled subscription-invoice instalments due by billAt that no termination
// cancelled, in billing order, with their commitments by ID.
func dueInstalments(
	ctx context.Context,
	params ServiceParams,
	subscriptionIDs []string,
	billAt time.Time,
) ([]*billingterm.Instalment, map[string]*commitment.Commitment, error) {
	scheduled, err := params.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		OwnerType:       types.BillingTermOwnerCommitment,
		SubscriptionIDs: subscriptionIDs,
		Statuses:        []types.InstalmentStatus{types.InstalmentStatusScheduled},
		Deliveries:      []types.InstalmentDelivery{types.InstalmentDeliverySubscriptionInvoice},
		BillOnOrBefore:  &billAt,
	})
	if err != nil || len(scheduled) == 0 {
		return nil, nil, err
	}

	commitments, err := instalmentCommitments(ctx, params.CommitmentRepo, scheduled)
	if err != nil {
		return nil, nil, err
	}

	billable := make([]*billingterm.Instalment, 0, len(scheduled))
	for _, inst := range scheduled {
		if !cancelledByTermination(commitments[inst.OwnerID], inst) {
			billable = append(billable, inst)
		}
	}

	return billable, commitments, nil
}

// instalmentCommitments returns the commitments owning the instalments, by ID.
func instalmentCommitments(
	ctx context.Context,
	repo commitment.Repository,
	instalments []*billingterm.Instalment,
) (map[string]*commitment.Commitment, error) {
	if len(instalments) == 0 {
		return map[string]*commitment.Commitment{}, nil
	}

	ownerIDs := make([]string, 0, len(instalments))
	for _, inst := range instalments {
		ownerIDs = append(ownerIDs, inst.OwnerID)
	}

	filter := commitment.NewNoLimitFilter()
	filter.IDs = lo.Uniq(ownerIDs)
	owners, err := repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*commitment.Commitment, len(owners))
	for _, c := range owners {
		byID[c.ID] = c
	}

	return byID, nil
}

// dueInstalmentLines returns the subscription's scheduled subscription-invoice instalments due by billAt as fixed lines.
func (s *billingService) dueInstalmentLines(
	ctx context.Context,
	sub *subscription.Subscription,
	billAt time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	instalments, commitments, err := dueInstalments(ctx, s.ServiceParams, []string{sub.ID}, billAt)
	if err != nil || len(instalments) == 0 {
		return nil, decimal.Zero, err
	}

	lines := make([]dto.CreateInvoiceLineItemRequest, 0, len(instalments))
	total := decimal.Zero
	for _, i := range instalments {
		c, ok := commitments[i.OwnerID]
		if !ok {
			return nil, decimal.Zero, ierr.NewErrorf("commitment %s of instalment %s not found", i.OwnerID, i.ID).
				WithHint("A scheduled instalment must belong to a commitment").
				Mark(ierr.ErrNotFound)
		}

		lines = append(lines, instalmentLine(c, i))
		total = total.Add(i.Amount)
	}

	return lines, total, nil
}

// instalmentLine is the fixed invoice line that bills instalment i of commitment c.
func instalmentLine(c *commitment.Commitment, i *billingterm.Instalment) dto.CreateInvoiceLineItemRequest {
	kind := types.CommitmentLineInstalment
	if i.Kind == types.InstalmentKindSettlement {
		kind = types.CommitmentLineSettlement
	}

	metadata := types.Metadata{
		types.MetadataKeyCommitmentLineKind: string(kind),
		types.MetadataKeyCommitmentID:       c.ID,
	}
	if c.InvoiceDescription != nil {
		metadata[types.MetadataKeyDescription] = *c.InvoiceDescription
	}

	return dto.CreateInvoiceLineItemRequest{
		DisplayName: lo.ToPtr(c.InvoiceLabel + " - " + i.InvoiceLabel),
		Amount:      i.Amount,
		Quantity:    decimal.NewFromInt(1),
		PeriodStart: lo.ToPtr(i.ServicePeriodStart),
		PeriodEnd:   lo.ToPtr(i.ServicePeriodEnd),
		PriceType:   lo.ToPtr(string(types.PRICE_TYPE_FIXED)),

		CommitmentID:        lo.ToPtr(c.ID),
		EntityType:          lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment)),
		EntityID:            lo.ToPtr(c.ID),
		BillingInstalmentID: lo.ToPtr(i.ID),

		Metadata: metadata,
	}
}
