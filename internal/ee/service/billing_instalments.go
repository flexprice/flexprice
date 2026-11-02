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

// dueInstalmentLines returns the subscription's scheduled subscription-invoice instalments due by billAt as fixed lines.
func (s *billingService) dueInstalmentLines(
	ctx context.Context,
	sub *subscription.Subscription,
	billAt time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	enabled, err := NewCommitmentService(s.ServiceParams).IsEnabled(ctx)
	if err != nil || !enabled {
		return nil, decimal.Zero, err
	}

	instalments, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		OwnerType:       types.BillingTermOwnerCommitment,
		SubscriptionIDs: []string{sub.ID},
		Statuses:        []types.InstalmentStatus{types.InstalmentStatusScheduled},
		Deliveries:      []types.InstalmentDelivery{types.InstalmentDeliverySubscriptionInvoice},
		BillOnOrBefore:  &billAt,
	})
	if err != nil || len(instalments) == 0 {
		return nil, decimal.Zero, err
	}

	filter := commitment.NewNoLimitFilter()
	filter.IDs = lo.Uniq(lo.Map(instalments, func(i *billingterm.Instalment, _ int) string { return i.OwnerID }))
	owners, err := s.CommitmentRepo.List(ctx, filter)
	if err != nil {
		return nil, decimal.Zero, err
	}

	ownerByID := lo.KeyBy(owners, func(c *commitment.Commitment) string { return c.ID })
	lines := make([]dto.CreateInvoiceLineItemRequest, 0, len(instalments))
	total := decimal.Zero
	for _, i := range instalments {
		c, ok := ownerByID[i.OwnerID]
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

	return dto.CreateInvoiceLineItemRequest{
		DisplayName: lo.ToPtr(c.InvoiceLabel + " - " + i.Description),
		Amount:      i.Amount,
		Quantity:    decimal.NewFromInt(1),
		PeriodStart: lo.ToPtr(i.ServicePeriodStart),
		PeriodEnd:   lo.ToPtr(i.ServicePeriodEnd),
		PriceType:   lo.ToPtr(string(types.PRICE_TYPE_FIXED)),

		CommitmentID:        lo.ToPtr(c.ID),
		EntityType:          lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment)),
		EntityID:            lo.ToPtr(c.ID),
		BillingInstalmentID: lo.ToPtr(i.ID),

		Metadata: types.Metadata{
			types.MetadataKeyCommitmentLineKind: string(kind),
			types.MetadataKeyCommitmentID:       c.ID,
		},
	}
}
