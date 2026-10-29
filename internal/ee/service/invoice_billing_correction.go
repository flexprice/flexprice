package service

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	ca "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type invoiceCorrection struct {
	original    *invoice.Invoice
	replacement *invoice.Invoice
	owners      []domain.OwnerRef
	currency    string
	delivery    types.BillingTermDelivery
}

func (s *invoiceService) ValidateInvoiceCorrection(ctx context.Context, id string, req dto.RecalculateInvoiceRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if len(req.BillingScheduleItemIDs) == 0 {
		inv, err := s.InvoiceRepo.Get(ctx, id)
		if err != nil {
			return err
		}
		if hasBillingScheduleLines(inv) {
			return billingScheduleCorrectionRequired(inv)
		}
		return nil
	}
	_, err := s.loadInvoiceCorrection(types.WithForceWriter(ctx), id, req)
	return err
}

func (s *invoiceService) RecalculateInvoiceWithSchedule(ctx context.Context, id string, req dto.RecalculateInvoiceRequest) (*dto.InvoiceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if len(req.BillingScheduleItemIDs) == 0 {
		return nil, ierr.Validation("scheduled invoice correction requires reviewed replacement items")
	}
	ctx = types.WithForceWriter(ctx)
	plan, err := s.loadInvoiceCorrection(ctx, id, req)
	if err != nil {
		return nil, err
	}
	if plan.replacement != nil {
		return dto.NewInvoiceResponse(plan.replacement), nil
	}
	prepared, err := s.prepareInvoiceCorrection(ctx, plan)
	if err != nil {
		return nil, err
	}
	if prepared.CustomerID != plan.original.CustomerID || !strings.EqualFold(prepared.Currency, plan.currency) {
		return nil, correctionConflict("replacement recipient or currency changed", plan.original.ID, "")
	}
	invoiceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE)
	var replacement *invoice.Invoice
	asOf := time.Now().UTC()
	err = s.DB.WithTx(ctx, func(tx context.Context) error {
		original, err := s.InvoiceRepo.GetForUpdate(tx, id)
		if err != nil {
			return err
		}
		if original.RecalculatedInvoiceID != nil || original.InvoiceStatus != types.InvoiceStatusVoided || !sameInvoiceComputationContext(plan.original, original) {
			return correctionConflict("source invoice changed; retry the correction", id, lo.FromPtr(original.RecalculatedInvoiceID))
		}
		tx, err = types.WithInvoiceLockAdmission(tx, []string{id})
		if err != nil {
			return err
		}
		return s.BillingTerms.WithCollection(tx, core.CollectionRequest{
			InvoiceID: invoiceID, CustomerID: original.CustomerID, Currency: plan.currency, Delivery: plan.delivery,
			Owners: plan.owners, SelectedItemIDs: req.BillingScheduleItemIDs, Cutoff: asOf, AsOf: asOf,
		}, func(tx context.Context, items []*domain.ScheduleItem) ([]core.InvoiceSource, error) {
			draft := prepared.ToDraftRequest()
			draft.SubscriptionCustomerID = original.SubscriptionCustomerID
			created, err := draft.ToDraftInvoice(tx)
			if err != nil {
				return nil, err
			}
			created = invoice.NewInvoiceBuilder(created).WithID(invoiceID).WithIdempotencyKey(lo.ToPtr("billing_correction:" + original.ID)).Build()
			if err := s.InvoiceRepo.Create(tx, created); err != nil {
				return nil, err
			}
			bindings, err := s.appendBillingScheduleLines(tx, created, items)
			if err != nil {
				return nil, err
			}
			compute := prepared.ToComputeRequest()
			if _, err := s.applyInvoiceComputation(tx, created, &compute); err != nil {
				return nil, err
			}
			linked := invoice.NewInvoiceBuilder(original).WithRecalculatedInvoiceID(&created.ID).Build()
			if err := s.InvoiceRepo.Update(tx, linked); err != nil {
				return nil, err
			}
			replacement = created
			return bindings, nil
		})
	})
	if err != nil {
		if ierr.IsVersionConflict(err) {
			latest, reloadErr := s.loadInvoiceCorrection(ctx, id, req)
			if reloadErr != nil {
				return nil, reloadErr
			}
			if latest.replacement != nil {
				return dto.NewInvoiceResponse(latest.replacement), nil
			}
		}
		return nil, err
	}
	s.afterInvoiceComputation(ctx, replacement, false, false)
	s.Logger.Info(ctx, "scheduled invoice correction created", "original_invoice_id", id, "invoice_id", replacement.ID)
	return dto.NewInvoiceResponse(replacement), nil
}

func (s *invoiceService) loadInvoiceCorrection(ctx context.Context, id string, req dto.RecalculateInvoiceRequest) (*invoiceCorrection, error) {
	if ctx == nil || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" || !billingDueID(id) {
		return nil, ierr.Validation("invoice correction requires a scoped invoice")
	}
	if s.BillingTerms == nil || s.BillingTermsStore == nil || s.BillingScheduleItems == nil || s.DB == nil {
		return nil, ierr.NewError("invoice correction is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	if s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx) {
		return nil, ierr.Validation("invoice correction requires its own transaction")
	}
	original, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if original == nil || original.ID != id || original.TenantID != types.GetTenantID(ctx) || original.EnvironmentID != types.GetEnvironmentID(ctx) {
		return nil, ierr.NewError("invoice correction has invalid source scope").Mark(ierr.ErrDatabase)
	}
	if original.InvoiceStatus != types.InvoiceStatusVoided {
		return nil, correctionConflict("void the source invoice and review the replacement schedule first", id, "")
	}
	plan := &invoiceCorrection{original: original, delivery: types.BillingTermSeparateInvoice}
	switch original.InvoiceType {
	case types.InvoiceTypeSubscription:
		plan.delivery = types.BillingTermSubscriptionInvoice
	case types.InvoiceTypeOneOff:
	default:
		return nil, ierr.Validation("scheduled correction supports subscription or one-off invoices")
	}
	if original.CustomCurrency != nil || !finiteScheduleDate(original.PeriodStart) || !finiteScheduleDate(original.PeriodEnd) {
		return nil, ierr.Validation("scheduled correction requires supported currency and invoice period context")
	}
	history, err := s.BillingScheduleItems.List(ctx, &domain.ScheduleItemFilter{InvoiceIDs: []string{id}, IncludeArchived: true, Limit: core.MaxItems})
	if err != nil {
		return nil, err
	}
	if len(history) == 0 {
		return nil, ierr.Validation("source invoice has no scheduled charges")
	}
	allowed := make(map[domain.OwnerRef]*domain.ScheduleItem)
	for _, item := range history {
		if !billingDueItemScope(ctx, item) || item.OwnerType != types.BillingTermOwnerCommitment || lo.FromPtr(item.InvoiceID) != id || item.InvoiceLineItemID == nil || item.CustomerID != original.CustomerID {
			return nil, ierr.NewError("original invoice source history is inconsistent").Mark(ierr.ErrInvalidOperation)
		}
		allowed[domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}] = item
	}
	states, err := s.BillingTermsStore.BindingStates(ctx, history)
	if err != nil {
		return nil, err
	}
	if len(states) != len(history) || lo.SomeBy(states, func(state domain.BindingState) bool {
		return state.InvoiceID != id || state.InvoiceStatus != types.InvoiceStatusVoided
	}) {
		return nil, ierr.NewError("original invoice bindings are not voided").Mark(ierr.ErrInvalidOperation)
	}
	if original.RecalculatedInvoiceID != nil {
		replacement, err := s.InvoiceRepo.Get(ctx, *original.RecalculatedInvoiceID)
		if err != nil {
			return nil, err
		}
		if replacement == nil || replacement.ID != *original.RecalculatedInvoiceID || replacement.TenantID != original.TenantID || replacement.EnvironmentID != original.EnvironmentID || replacement.CustomerID != original.CustomerID || replacement.InvoiceStatus == types.InvoiceStatusVoided || replacement.Status != types.StatusPublished {
			return nil, correctionConflict("the existing replacement is no longer live", id, *original.RecalculatedInvoiceID)
		}
		sources, err := s.BillingScheduleItems.List(ctx, &domain.ScheduleItemFilter{InvoiceIDs: []string{replacement.ID}, IncludeArchived: true, Limit: core.MaxItems})
		if err != nil {
			return nil, err
		}
		if len(sources) != len(req.BillingScheduleItemIDs) {
			return nil, correctionConflict("replacement source selection changed", id, replacement.ID)
		}
		for _, source := range sources {
			if !billingDueItemScope(ctx, source) || allowed[domain.OwnerRef{Type: source.OwnerType, ID: source.OwnerID}] == nil || lo.FromPtr(source.InvoiceID) != replacement.ID || !slices.Contains(req.BillingScheduleItemIDs, source.ID) {
				return nil, correctionConflict("replacement source selection changed", id, replacement.ID)
			}
		}
		bindings, err := s.BillingTermsStore.BindingStates(ctx, sources)
		if err != nil {
			return nil, err
		}
		if len(bindings) != len(sources) || lo.SomeBy(bindings, func(binding domain.BindingState) bool {
			return binding.InvoiceID != replacement.ID || binding.InvoiceStatus != replacement.InvoiceStatus || binding.InvoiceArchived
		}) {
			return nil, correctionConflict("replacement source bindings changed", id, replacement.ID)
		}
		ids := []string{}
		for _, line := range replacement.LineItems {
			if line != nil && line.BillingScheduleItemID != nil {
				ids = append(ids, *line.BillingScheduleItemID)
			}
		}
		slices.Sort(ids)
		selected := slices.Clone(req.BillingScheduleItemIDs)
		slices.Sort(selected)
		if !slices.Equal(ids, selected) {
			return nil, correctionConflict("source invoice already has another replacement selection", id, replacement.ID)
		}
		plan.replacement = replacement
		return plan, nil
	}
	seenOwners := make(map[domain.OwnerRef]bool)
	asOf := time.Now().UTC()
	for _, itemID := range req.BillingScheduleItemIDs {
		item, err := s.BillingScheduleItems.Get(ctx, itemID)
		if err != nil {
			return nil, err
		}
		if !billingDueItemScope(ctx, item) || item.ID != itemID {
			return nil, ierr.NewError("replacement item has invalid scope").Mark(ierr.ErrDatabase)
		}
		owner := domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}
		old := allowed[owner]
		if old == nil {
			return nil, ierr.NewError("replacement item is outside the source invoice owners").Mark(ierr.ErrNotFound)
		}
		if item.CustomerID != original.CustomerID || !sameScheduleString(item.SubscriptionID, old.SubscriptionID) || !strings.EqualFold(item.Currency, old.Currency) || item.Delivery != plan.delivery {
			return nil, correctionConflict("replacement item has an incompatible invoice destination", id, lo.FromPtr(item.InvoiceID))
		}
		if item.Status != types.StatusPublished || item.ScheduleStatus != types.BillingScheduleScheduled || item.InvoiceID != nil || item.InvoiceLineItemID != nil || item.BillOn == nil || item.BillOn.After(asOf) {
			return nil, correctionConflict("replacement item is no longer an eligible due charge", id, lo.FromPtr(item.InvoiceID))
		}
		if plan.currency != "" && !strings.EqualFold(plan.currency, item.Currency) {
			return nil, ierr.Validation("replacement items must share one obligation currency")
		}
		plan.currency = item.Currency
		if seenOwners[owner] {
			continue
		}
		state, err := s.BillingTerms.Get(ctx, owner)
		if err != nil {
			return nil, err
		}
		if state == nil || state.Owner() == nil || state.Summary() == nil || !state.Owner().CanCollect || state.Owner().CustomerID != original.CustomerID || state.Summary().RequiresReplan {
			return nil, correctionConflict("replacement owner is deferred or requires a complete billing plan", id, "")
		}
		seenOwners[owner] = true
		plan.owners = append(plan.owners, owner)
	}
	sort.Slice(plan.owners, func(i, j int) bool { return plan.owners[i].ID < plan.owners[j].ID })
	return plan, nil
}

func (s *invoiceService) prepareInvoiceCorrection(ctx context.Context, plan *invoiceCorrection) (*dto.CreateInvoiceRequest, error) {
	original := plan.original
	if original.SubscriptionID == nil {
		return nil, ierr.Validation("commitment invoice correction requires its subscription")
	}
	sub, _, err := s.SubRepo.GetWithLineItems(ctx, *original.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if !subscriptionUpdateScoped(ctx, sub, *original.SubscriptionID) || !strings.EqualFold(sub.Currency, plan.currency) || original.InvoiceType == types.InvoiceTypeSubscription && sub.GetInvoicingCustomerID() != original.CustomerID {
		return nil, correctionConflict("source subscription destination changed", original.ID, "")
	}
	if original.InvoiceType == types.InvoiceTypeSubscription {
		reference := types.ReferencePointPeriodEnd
		switch types.InvoiceBillingReason(original.BillingReason) {
		case types.InvoiceBillingReasonSubscriptionCreate, types.InvoiceBillingReasonSubscriptionTrialEnd, types.InvoiceBillingReasonSubscriptionUpdate, types.InvoiceBillingReasonSubscriptionResume:
			reference = types.ReferencePointPeriodStart
		case types.InvoiceBillingReasonProration:
			reference = types.ReferencePointCancel
		}
		request, err := NewBillingService(s.ServiceParams).PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
			Subscription: sub, PeriodStart: *original.PeriodStart, PeriodEnd: *original.PeriodEnd, ReferencePoint: reference, ExcludeInvoiceID: original.ID,
		})
		if err != nil {
			return nil, err
		}
		request.BillingReason = types.InvoiceBillingReason(original.BillingReason)
		return request, nil
	}
	request := &dto.CreateInvoiceRequest{
		CustomerID: original.CustomerID, SubscriptionID: original.SubscriptionID, InvoiceType: types.InvoiceTypeOneOff,
		Currency: plan.currency, Description: original.Description, BillingReason: types.InvoiceBillingReasonManual,
		PeriodStart: original.PeriodStart, PeriodEnd: original.PeriodEnd, InvoiceStatus: lo.ToPtr(types.InvoiceStatusDraft),
	}
	validation := NewCouponValidationService(s.ServiceParams)
	selected, err := selectSubscriptionCoupons(ctx, s.ServiceParams, []*subscription.Subscription{sub}, *original.PeriodStart, *original.PeriodEnd,
		func(c *coupon.Coupon, _ *ca.CouponAssociation) bool {
			return validation.ValidateCoupon(ctx, *c, sub) == nil
		})
	if err != nil {
		return nil, err
	}
	for _, association := range selected.SubLevel[sub.ID] {
		request.InvoiceCoupons = append(request.InvoiceCoupons, dto.InvoiceCoupon{CouponID: association.CouponID, CouponAssociationID: &association.ID})
	}
	request.PreparedTaxRates, err = NewTaxService(s.ServiceParams).PrepareTaxRatesForInvoice(ctx, *request)
	if err != nil {
		return nil, err
	}
	settings := NewSettingsService(s.ServiceParams).(*settingsService)
	configuration, err := GetSetting[types.InvoiceConfig](settings, ctx, types.SettingKeyInvoiceConfig)
	if err != nil {
		return nil, err
	}
	request.DueDate = lo.ToPtr(subscriptionInvoiceDueDate(sub, configuration, time.Now().UTC()))
	return request, nil
}

func correctionConflict(message, originalID, invoiceID string) error {
	return ierr.NewError(message).WithReportableDetails(map[string]any{"original_invoice_id": originalID, "invoice_id": invoiceID}).Mark(ierr.ErrVersionConflict)
}
