package service

import (
	"context"
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

func (s *billingScheduleService) CreateOrRecover(ctx context.Context, req core.DueInvoiceRequest) (*core.DueInvoice, error) {
	if err := s.ready(ctx, req.AsOf); err != nil {
		return nil, err
	}
	if !billingDueID(req.ItemID) {
		return nil, ierr.Validation("billing schedule item ID is required")
	}
	ctx = types.WithForceWriter(ctx)
	item, err := s.items.Get(ctx, req.ItemID)
	if ierr.IsNotFound(err) {
		return &core.DueInvoice{DeferredReason: "source_unavailable"}, nil
	}
	if err != nil {
		return nil, err
	}
	if !billingDueItemScope(ctx, item) || item.ID != req.ItemID {
		return nil, ierr.NewError("billing schedule source has invalid scope").Mark(ierr.ErrDatabase)
	}
	if item.ScheduleStatus == types.BillingScheduleInvoiced {
		return s.recoverBillingScheduleInvoice(ctx, item)
	}
	if item.Status != types.StatusPublished || item.ScheduleStatus != types.BillingScheduleScheduled || item.Delivery != types.BillingTermSeparateInvoice || item.BillOn == nil || item.BillOn.After(req.AsOf) {
		return &core.DueInvoice{DeferredReason: "source_ineligible"}, nil
	}
	owner := domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}
	state, err := s.params.BillingTerms.Get(ctx, owner)
	if ierr.IsNotFound(err) {
		return &core.DueInvoice{DeferredReason: "owner_unavailable"}, nil
	}
	if err != nil {
		return nil, err
	}
	if state == nil || state.Owner() == nil || state.Summary() == nil {
		return nil, ierr.NewError("billing collection state is missing").Mark(ierr.ErrDatabase)
	}
	snapshot, summary := state.Owner(), state.Summary()
	if snapshot.Owner != owner || snapshot.TenantID != item.TenantID || snapshot.EnvironmentID != item.EnvironmentID {
		return nil, ierr.NewError("billing collection state has invalid scope").Mark(ierr.ErrDatabase)
	}
	if !snapshot.CanCollect || summary.RequiresReplan {
		reason := lo.FromPtr(summary.DeferredReason)
		if reason == "" {
			reason = "collection_deferred"
		}
		return &core.DueInvoice{DeferredReason: reason}, nil
	}
	invoiceID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE)
	var created *invoice.Invoice
	err = s.params.DB.WithTx(ctx, func(tx context.Context) error {
		tx, err := types.WithInvoiceLockAdmission(tx, nil)
		if err != nil {
			return err
		}
		return s.params.BillingTerms.WithCollection(tx, core.CollectionRequest{
			InvoiceID: invoiceID, CustomerID: snapshot.CustomerID, Currency: snapshot.Currency,
			Delivery: types.BillingTermSeparateInvoice, Owners: []domain.OwnerRef{owner},
			Cutoff: req.AsOf, AsOf: req.AsOf, SelectedItemIDs: []string{item.ID},
		}, func(tx context.Context, selected []*domain.ScheduleItem) ([]core.InvoiceSource, error) {
			if len(selected) != 1 || selected[0].ID != item.ID {
				return nil, ierr.NewError("separate delivery requires exactly one scheduled source").Mark(ierr.ErrDatabase)
			}
			var sources []core.InvoiceSource
			created, sources, err = s.createBillingScheduleDraft(tx, invoiceID, selected[0])
			return sources, err
		})
	})
	if err != nil {
		return nil, err
	}
	NewInvoiceService(s.params).(*invoiceService).afterInvoiceComputation(ctx, created, false, false)
	s.params.Logger.Info(ctx, "billing schedule invoice created", "item_id", item.ID, "invoice_id", invoiceID)
	return &core.DueInvoice{InvoiceID: invoiceID}, nil
}

func (s *billingScheduleService) recoverBillingScheduleInvoice(ctx context.Context, item *domain.ScheduleItem) (*core.DueInvoice, error) {
	if item.Delivery != types.BillingTermSeparateInvoice || item.InvoiceID == nil || !billingDueID(*item.InvoiceID) {
		return nil, ierr.NewError("assigned schedule item has an invalid invoice destination").Mark(ierr.ErrDatabase)
	}
	result := &core.DueInvoice{}
	err := s.params.DB.WithTx(ctx, func(tx context.Context) error {
		inv, err := s.params.InvoiceRepo.GetForUpdate(tx, *item.InvoiceID)
		if err != nil {
			return err
		}
		fresh, err := s.items.Get(tx, item.ID)
		if err != nil {
			return err
		}
		if !billingDueItemScope(tx, fresh) || fresh.ID != item.ID || inv == nil || inv.ID != *item.InvoiceID || inv.TenantID != item.TenantID || inv.EnvironmentID != item.EnvironmentID {
			return ierr.NewError("billing schedule recovery returned invalid scope").Mark(ierr.ErrDatabase)
		}
		if inv.Status == types.StatusArchived || inv.InvoiceStatus == types.InvoiceStatusVoided || inv.InvoiceStatus == types.InvoiceStatusSkipped || fresh.ScheduleStatus == types.BillingScheduleCanceled {
			result.DeferredReason = "invoice_ineligible"
			return nil
		}
		states, err := s.params.BillingTermsStore.BindingStates(tx, []*domain.ScheduleItem{fresh})
		if err != nil {
			return err
		}
		if len(states) != 1 || states[0].ItemID != item.ID || states[0].InvoiceID != inv.ID || states[0].InvoiceLineItemID != lo.FromPtr(fresh.InvoiceLineItemID) || states[0].TenantID != item.TenantID || states[0].EnvironmentID != item.EnvironmentID || states[0].InvoiceArchived || states[0].InvoiceStatus != inv.InvoiceStatus {
			return ierr.NewError("billing schedule recovery source is inconsistent").Mark(ierr.ErrDatabase)
		}
		if inv.InvoiceStatus == types.InvoiceStatusDraft {
			due, err := NewInvoiceService(s.params).IsInvoiceFinalizationDue(tx, inv)
			if err != nil {
				return err
			}
			if !due {
				result.DeferredReason = "finalization_pending"
				return nil
			}
		}
		result.InvoiceID = inv.ID
		return nil
	})
	return result, err
}

func (s *billingScheduleService) createBillingScheduleDraft(ctx context.Context, invoiceID string, item *domain.ScheduleItem) (*invoice.Invoice, []core.InvoiceSource, error) {
	invoiceService := NewInvoiceService(s.params).(*invoiceService)
	req := dto.CreateInvoiceRequest{
		CustomerID: item.CustomerID, SubscriptionID: item.SubscriptionID, InvoiceType: types.InvoiceTypeOneOff,
		Currency: item.Currency, Description: item.Description, BillingReason: types.InvoiceBillingReasonManual,
		PeriodStart: &item.ServicePeriodStart, PeriodEnd: &item.ServicePeriodEnd,
		InvoiceStatus: lo.ToPtr(types.InvoiceStatusDraft), IdempotencyKey: lo.ToPtr("billing_schedule:" + item.ID),
	}
	if err := invoiceService.validateInvoiceBillingCurrency(ctx, req); err != nil {
		return nil, nil, err
	}
	draftReq := req.ToDraftRequest()
	if item.SubscriptionID != nil {
		sub, err := s.params.SubRepo.Get(ctx, *item.SubscriptionID)
		if err != nil {
			return nil, nil, err
		}
		if !subscriptionUpdateScoped(ctx, sub, *item.SubscriptionID) || !strings.EqualFold(sub.Currency, item.Currency) {
			return nil, nil, ierr.NewError("schedule subscription destination changed").Mark(ierr.ErrVersionConflict)
		}
		settings := NewSettingsService(s.params).(*settingsService)
		configuration, err := GetSetting[types.InvoiceConfig](settings, ctx, types.SettingKeyInvoiceConfig)
		if err != nil {
			return nil, nil, err
		}
		req.DueDate = lo.ToPtr(subscriptionInvoiceDueDate(sub, configuration, time.Now().UTC()))
		draftReq.DueDate = req.DueDate
		draftReq.SubscriptionCustomerID = &sub.CustomerID
		validation := NewCouponValidationService(s.params)
		selected, err := selectSubscriptionCoupons(ctx, s.params, []*subscription.Subscription{sub}, item.ServicePeriodStart, item.ServicePeriodEnd,
			func(c *coupon.Coupon, _ *ca.CouponAssociation) bool {
				return validation.ValidateCoupon(ctx, *c, sub) == nil
			})
		if err != nil {
			return nil, nil, err
		}
		for _, association := range selected.SubLevel[sub.ID] {
			req.InvoiceCoupons = append(req.InvoiceCoupons, dto.InvoiceCoupon{CouponID: association.CouponID, CouponAssociationID: &association.ID})
		}
	}
	prepared, err := NewTaxService(s.params).PrepareTaxRatesForInvoice(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	req.PreparedTaxRates = prepared
	inv, err := draftReq.ToDraftInvoice(ctx)
	if err != nil {
		return nil, nil, err
	}
	inv = invoice.NewInvoiceBuilder(inv).WithID(invoiceID).WithIdempotencyKey(req.IdempotencyKey).Build()
	if err := s.params.InvoiceRepo.Create(ctx, inv); err != nil {
		return nil, nil, err
	}
	bindings, err := invoiceService.appendBillingScheduleLines(ctx, inv, []*domain.ScheduleItem{item})
	if err != nil {
		return nil, nil, err
	}
	compute := req.ToComputeRequest()
	if _, err := invoiceService.applyInvoiceComputation(ctx, inv, &compute); err != nil {
		return nil, nil, err
	}
	return inv, bindings, nil
}
