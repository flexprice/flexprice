package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// recalculateDiscountOnInvoice mutates inv's discount/tax/totals in place. Caller must
// hold the row lock (GetForUpdate) and persist inv afterward.
func (s *invoiceService) recalculateDiscountOnInvoice(ctx context.Context, inv *invoice.Invoice) error {
	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return ierr.NewError("invoice is not in draft status").
			WithHint("Only draft invoices can have their discount recalculated").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     inv.ID,
				"current_status": inv.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	if err := s.wipeCouponApplications(ctx, inv); err != nil {
		return err
	}

	return s.applyCurrentDiscountToDraft(ctx, inv, inv)
}

// applyCurrentDiscountToDraft re-derives discount, tax, and totals from current coupon
// associations; assumes no stale coupon applications (recalculateDiscountOnInvoice wipes first).
func (s *invoiceService) applyCurrentDiscountToDraft(ctx context.Context, sourceInvoice, destinationInvoice *invoice.Invoice) error {
	if destinationInvoice.InvoiceStatus != types.InvoiceStatusDraft {
		return ierr.NewError("invoice is not in draft status").
			WithHint("Only draft invoices can have their discount recalculated").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     destinationInvoice.ID,
				"current_status": destinationInvoice.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	invoiceCoupons, lineItemCoupons, err := s.resolveCurrentInvoiceCoupons(ctx, sourceInvoice)
	if err != nil {
		return err
	}

	couponApplicationService := NewCouponApplicationService(s.ServiceParams)
	couponResult, err := couponApplicationService.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice:         destinationInvoice,
		InvoiceCoupons:  invoiceCoupons,
		LineItemCoupons: lineItemCoupons,
	})
	if err != nil {
		return err
	}
	destinationInvoice.TotalDiscount = couponResult.TotalDiscountAmount

	// Tax depends on discount (taxableAmount = Subtotal - TotalDiscount); refresh it via real
	// TaxApplied records rather than TotalTax==0, which a full discount can zero legitimately.
	taxFilter := types.NewNoLimitTaxAppliedFilter()
	taxFilter.EntityType = types.TaxRateEntityTypeInvoice
	taxFilter.EntityID = destinationInvoice.ID
	taxAppliedCount, err := s.TaxAppliedRepo.Count(ctx, taxFilter)
	if err != nil {
		return err
	}
	if taxAppliedCount > 0 {
		// Reset first: applyTaxesToInvoice no-ops (leaving TotalTax stale) when the current
		// subscription resolves to no active tax rates, e.g. the association was removed.
		destinationInvoice.TotalTax = decimal.Zero
		if err := s.applyTaxesToInvoice(ctx, destinationInvoice, dto.InvoiceComputeRequest{}); err != nil {
			return err
		}
	}

	destinationInvoice.Total = decimal.Max(destinationInvoice.Subtotal.Sub(destinationInvoice.TotalPrepaidCreditsApplied).Sub(destinationInvoice.TotalDiscount).Add(destinationInvoice.TotalTax), decimal.Zero)
	destinationInvoice.AmountDue = destinationInvoice.Total
	destinationInvoice.AmountRemaining = decimal.Max(destinationInvoice.Total.Sub(destinationInvoice.AmountPaid), decimal.Zero)

	s.Logger.Info(ctx, "recalculated discount on invoice",
		"invoice_id", destinationInvoice.ID, "total_discount", destinationInvoice.TotalDiscount, "new_total", destinationInvoice.Total)

	return nil
}

// wipeCouponApplications deletes existing CouponApplication rows and persists a zeroed
// discount reset — ApplyCouponsToInvoice only adds to these fields, never resets them.
func (s *invoiceService) wipeCouponApplications(ctx context.Context, inv *invoice.Invoice) error {
	filter := types.NewNoLimitCouponApplicationFilter()
	filter.InvoiceIDs = []string{inv.ID}
	existing, err := s.CouponApplicationRepo.List(ctx, filter)
	if err != nil {
		return ierr.WithError(err).WithHint("failed to list existing coupon applications").Mark(ierr.ErrDatabase)
	}

	for _, app := range existing {
		if err := s.CouponApplicationRepo.Delete(ctx, app.ID); err != nil {
			return ierr.WithError(err).WithHint("failed to delete existing coupon application").Mark(ierr.ErrDatabase)
		}
	}

	for _, li := range inv.LineItems {
		li.LineItemDiscount = decimal.Zero
		li.InvoiceLevelDiscount = decimal.Zero
		if err := s.InvoiceLineItemRepo.Update(ctx, li); err != nil {
			return ierr.WithError(err).WithHint("failed to reset line item discount").Mark(ierr.ErrDatabase)
		}
	}

	return nil
}

// resolveCurrentInvoiceCoupons resolves coupons currently standing against this invoice's
// subscription, scoped to its billing period and filtered to price IDs on its line items.
func (s *invoiceService) resolveCurrentInvoiceCoupons(ctx context.Context, inv *invoice.Invoice) ([]dto.InvoiceCoupon, []dto.InvoiceLineItemCoupon, error) {
	if inv.SubscriptionID == nil || inv.PeriodStart == nil || inv.PeriodEnd == nil {
		return nil, nil, nil
	}

	sub, _, err := s.SubRepo.GetWithLineItems(ctx, *inv.SubscriptionID)
	if err != nil {
		return nil, nil, err
	}

	children, err := getGroupedInvoicingChildren(ctx, s.ServiceParams, sub, true)
	if err != nil {
		return nil, nil, err
	}

	subs := append([]*subscription.Subscription{sub}, children...)
	subByID := lo.SliceToMap(subs, func(x *subscription.Subscription) (string, *subscription.Subscription) {
		return x.ID, x
	})

	couponValidationService := NewCouponValidationService(s.ServiceParams)
	filter := func(c *coupon.Coupon, a *coupon_association.CouponAssociation) bool {
		owner, ok := subByID[a.SubscriptionID]
		if !ok {
			return false
		}
		return couponValidationService.ValidateCoupon(ctx, *c, owner) == nil
	}

	sel, err := selectSubscriptionCoupons(ctx, s.ServiceParams, subs, *inv.PeriodStart, *inv.PeriodEnd, filter)
	if err != nil {
		return nil, nil, err
	}

	priceIDs := lo.SliceToMap(
		lo.Filter(inv.LineItems, func(li *invoice.InvoiceLineItem, _ int) bool { return li.PriceID != nil }),
		func(li *invoice.InvoiceLineItem) (string, bool) { return *li.PriceID, true },
	)

	// Only the parent's subscription-level coupons.
	invoiceCoupons := lo.Map(sel.SubLevel[sub.ID], func(a *coupon_association.CouponAssociation, _ int) dto.InvoiceCoupon {
		return dto.InvoiceCoupon{CouponID: a.CouponID, CouponAssociationID: &a.ID}
	})

	lineItemCoupons := make([]dto.InvoiceLineItemCoupon, 0)
	for sliID, assocs := range sel.LineLevel {
		priceID := sel.SubLineItemIDToPriceID[sliID]
		if priceID == "" || !priceIDs[priceID] {
			continue
		}
		lineItemCoupons = append(lineItemCoupons, lo.Map(assocs, func(a *coupon_association.CouponAssociation, _ int) dto.InvoiceLineItemCoupon {
			return dto.InvoiceLineItemCoupon{
				LineItemID:             priceID,
				SubscriptionLineItemID: lo.ToPtr(sliID),
				CouponID:               a.CouponID,
				CouponAssociationID:    &a.ID,
			}
		})...)
	}

	return invoiceCoupons, lineItemCoupons, nil
}
