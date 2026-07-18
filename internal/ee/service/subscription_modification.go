package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// SubscriptionModificationService handles mid-cycle subscription modifications.
type SubscriptionModificationService interface {
	// Execute performs the modification and persists all changes.
	Execute(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error)

	// Preview returns what would happen without committing any changes.
	Preview(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error)
}

type subscriptionModificationService struct {
	serviceParams ServiceParams
}

// NewSubscriptionModificationService creates a new SubscriptionModificationService.
func NewSubscriptionModificationService(serviceParams ServiceParams) SubscriptionModificationService {
	return &subscriptionModificationService{
		serviceParams: serviceParams,
	}
}

// Execute performs the modification and persists all changes.
func (s *subscriptionModificationService) Execute(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	switch req.Type {
	case dto.SubscriptionModifyTypeInheritance:
		return s.executeInheritance(ctx, subscriptionID, req.InheritanceParams)
	case dto.SubscriptionModifyTypeQuantityChange:
		return s.executeQuantityChange(ctx, subscriptionID, req.QuantityChangeParams)
	case dto.SubscriptionModifyTypeGroupedInvoicing:
		return s.executeGroupedInvoicingMembership(ctx, req.GroupedInvoicingParams)
	case dto.SubscriptionModifyTypeTrialEnd:
		return s.executeTrialEnd(ctx, subscriptionID, req.TrialEndParams)
	case dto.SubscriptionModifyTypeCoupon:
		return s.executeCouponModification(ctx, subscriptionID, req.CouponParams)
	case dto.SubscriptionModifyTypeTax:
		return s.executeTaxModification(ctx, subscriptionID, req.TaxParams)
	default:
		return nil, ierr.NewError("unknown modification type: " + string(req.Type)).
			WithHint("Valid values: inheritance, quantity_change, grouped_invoicing, trial_end, coupon, tax").
			Mark(ierr.ErrValidation)
	}
}

// Preview returns what would happen without committing any changes.
func (s *subscriptionModificationService) Preview(ctx context.Context, subscriptionID string, req dto.ExecuteSubscriptionModifyRequest) (*dto.SubscriptionModifyResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	switch req.Type {
	case dto.SubscriptionModifyTypeInheritance:
		return s.previewInheritance(ctx, subscriptionID, req.InheritanceParams)
	case dto.SubscriptionModifyTypeQuantityChange:
		return s.previewQuantityChange(ctx, subscriptionID, req.QuantityChangeParams)
	case dto.SubscriptionModifyTypeGroupedInvoicing:
		return s.previewGroupedInvoicingMembership(ctx, req.GroupedInvoicingParams)
	case dto.SubscriptionModifyTypeTrialEnd:
		return s.previewTrialEnd(ctx, subscriptionID, req.TrialEndParams)
	case dto.SubscriptionModifyTypeCoupon:
		return s.previewCouponModification(ctx, subscriptionID, req.CouponParams)
	case dto.SubscriptionModifyTypeTax:
		return s.previewTaxModification(ctx, subscriptionID, req.TaxParams)
	default:
		return nil, ierr.NewError("unknown modification type: " + string(req.Type)).
			WithHint("Valid values: inheritance, quantity_change, grouped_invoicing, trial_end, coupon, tax").
			Mark(ierr.ErrValidation)
	}
}

// ─────────────────────────────────────────────
// Sub-feature 1: Inheritance
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) executeInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	if params.Action == dto.InheritanceActionRemove {
		return s.executeRemoveInheritance(ctx, subscriptionID, params)
	}

	sp := s.serviceParams

	// 1. Get subscription
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// 2. Validate: not inherited, is active
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil, ierr.NewError("cannot modify inherited subscription").
			WithHint("Inheritance can only be applied to standalone or parent subscriptions").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can be modified for inheritance").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}
	if sub.HasPositiveAutoInvoiceThreshold() {
		return nil, ierr.NewError("cannot add inherited subscriptions while auto_invoice_threshold is set").
			WithHint("Remove subscription-level auto_invoice_threshold first; it applies only to standalone subscriptions").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}

	// 3. Resolve external customers for inheritance
	childCustomerIDs, err := s.resolveExternalCustomersForInheritance(ctx, sub.CustomerID, params.ExternalCustomerIDsToInheritSubscription)
	if err != nil {
		return nil, err
	}

	// 4. Check for duplicate inherited subscriptions
	existingInherited, err := s.getInheritedSubscriptions(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	existingChildIDs := make(map[string]bool, len(existingInherited))
	for _, inh := range existingInherited {
		existingChildIDs[inh.CustomerID] = true
	}
	for _, childID := range childCustomerIDs {
		if existingChildIDs[childID] {
			return nil, ierr.NewError("duplicate inherited subscription").
				WithHint("A child customer already has an inherited subscription for this parent").
				WithReportableDetails(map[string]interface{}{"child_customer_id": childID, "subscription_id": subscriptionID}).
				Mark(ierr.ErrValidation)
		}
	}

	// 5. Transaction: update parent type and create inherited subscriptions
	changedSubs := make([]dto.ChangedSubscription, 0)
	err = sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		changedSubs = nil // reset for safety in case of retry
		// If standalone, promote to parent
		if sub.SubscriptionType == types.SubscriptionTypeStandalone {
			sub.SubscriptionType = types.SubscriptionTypeParent
			if err := sp.SubRepo.Update(txCtx, sub); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to update subscription type to parent").
					Mark(ierr.ErrDatabase)
			}
			changedSubs = append(changedSubs, dto.ChangedSubscription{
				ID:     sub.ID,
				Action: dto.ChangedSubscriptionActionUpdated,
				Status: sub.SubscriptionStatus,
			})
		}

		// Create inherited subscriptions for each child customer
		for _, childCustomerID := range childCustomerIDs {
			inheritedSub, err := s.createInheritedSubscription(txCtx, sub, childCustomerID)
			if err != nil {
				return err
			}
			changedSubs = append(changedSubs, dto.ChangedSubscription{
				ID:     inheritedSub.ID,
				Action: dto.ChangedSubscriptionActionCreated,
				Status: inheritedSub.SubscriptionStatus,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 6. Publish webhook event
	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	// 7. Return response with updated subscription
	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}

func (s *subscriptionModificationService) previewInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	if params.Action == dto.InheritanceActionRemove {
		return s.previewRemoveInheritance(ctx, subscriptionID, params)
	}

	sp := s.serviceParams

	// Get subscription (read-only)
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Validate
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil, ierr.NewError("cannot modify inherited subscription").
			WithHint("Inheritance can only be applied to standalone or parent subscriptions").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can be modified for inheritance").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}
	if sub.HasPositiveAutoInvoiceThreshold() {
		return nil, ierr.NewError("cannot add inherited subscriptions while auto_invoice_threshold is set").
			WithHint("Remove subscription-level auto_invoice_threshold first; it applies only to standalone subscriptions").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}

	// Resolve external customers
	childCustomerIDs, err := s.resolveExternalCustomersForInheritance(ctx, sub.CustomerID, params.ExternalCustomerIDsToInheritSubscription)
	if err != nil {
		return nil, err
	}

	// Check for duplicates
	existingInherited, err := s.getInheritedSubscriptions(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	existingChildIDs := make(map[string]bool, len(existingInherited))
	for _, inh := range existingInherited {
		existingChildIDs[inh.CustomerID] = true
	}
	for _, childID := range childCustomerIDs {
		if existingChildIDs[childID] {
			return nil, ierr.NewError("duplicate inherited subscription").
				WithHint("A child customer already has an inherited subscription for this parent").
				WithReportableDetails(map[string]interface{}{"child_customer_id": childID, "subscription_id": subscriptionID}).
				Mark(ierr.ErrValidation)
		}
	}

	// Build preview response (no DB mutations)
	changedSubs := make([]dto.ChangedSubscription, 0)
	if sub.SubscriptionType == types.SubscriptionTypeStandalone {
		changedSubs = append(changedSubs, dto.ChangedSubscription{
			ID:     sub.ID,
			Action: dto.ChangedSubscriptionActionUpdated,
			Status: sub.SubscriptionStatus,
		})
	}
	for range childCustomerIDs {
		changedSubs = append(changedSubs, dto.ChangedSubscription{
			ID:     "(preview-created)",
			Action: dto.ChangedSubscriptionActionCreated,
			Status: types.SubscriptionStatusActive,
		})
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}

// ─────────────────────────────────────────────
// Sub-feature 2: Quantity Change
// Plan (A) + proration (B) + apply (C) + settlePayLater (D1) in subscription_modification_quantity.go.
// Pay-first checkout is Module D3+.
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) executeQuantityChange(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyQuantityChangeRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	plan, err := s.buildQuantityChangePlan(ctx, subscriptionID, params)
	if err != nil {
		return nil, err
	}

	prorationResult, err := s.calculateProrationForPlan(ctx, plan)
	if err != nil {
		return nil, err
	}

	changedLineItems, changedInvoices, err := s.settlePayLater(ctx, plan, prorationResult)
	if err != nil {
		return nil, err
	}

	// Publish webhook event
	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	// Build response
	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems: changedLineItems,
			Invoices:  changedInvoices,
		},
	}, nil
}

func (s *subscriptionModificationService) previewQuantityChange(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyQuantityChangeRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	plan, err := s.buildQuantityChangePlan(ctx, subscriptionID, params)
	if err != nil {
		return nil, err
	}

	prorationResult, err := s.calculateProrationForPlan(ctx, plan)
	if err != nil {
		return nil, err
	}

	changedLineItems := plan.previewChangedLineItems()
	changedInvoices, err := s.toPreviewChangedInvoices(ctx, plan.GetSubscription(), prorationResult)
	if err != nil {
		return nil, err
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems: changedLineItems,
			Invoices:  changedInvoices,
		},
	}, nil
}

// toPreviewChangedInvoices maps Module B calc results to synthetic ChangedInvoice entries.
func (s *subscriptionModificationService) toPreviewChangedInvoices(
	ctx context.Context,
	sub *subscription.Subscription,
	prorationResult *quantityChangeProration,
) ([]dto.ChangedInvoice, error) {
	if sub == nil || prorationResult == nil {
		return nil, nil
	}
	out := make([]dto.ChangedInvoice, 0, len(prorationResult.getItems()))
	for _, item := range prorationResult.getItems() {
		if item == nil {
			continue
		}
		mod := item.getMod()
		oldItem := mod.getOldLineItem()
		price := item.getPrice()
		netAmount := item.getNetAmount()
		if mod == nil || oldItem == nil || price == nil || netAmount.IsZero() {
			continue
		}
		newItem := &subscription.SubscriptionLineItem{
			PriceID:  oldItem.PriceID,
			Quantity: mod.getQuantity(),
		}
		if netAmount.GreaterThan(decimal.Zero) {
			invResp := previewProrationQuantityChangeInvoiceResponse(ctx, sub, oldItem, newItem, mod.getEffectiveDate(), price, netAmount)
			out = append(out, dto.ChangedInvoice{
				ID:      "(preview-invoice)",
				Action:  dto.ChangedInvoiceActionCreated,
				Status:  dto.ChangedInvoiceStatusPreview,
				Invoice: invResp,
			})
			continue
		}
		walletTx, err := s.previewProrationWalletTransactionResponse(ctx, sub, netAmount.Abs())
		if err != nil {
			return nil, err
		}
		out = append(out, dto.ChangedInvoice{
			ID:                "(preview-wallet-credit)",
			Action:            dto.ChangedInvoiceActionWalletCredit,
			Status:            dto.ChangedInvoiceStatusPreview,
			WalletTransaction: walletTx,
		})
	}
	return out, nil
}

// previewProrationQuantityChangeInvoiceResponse builds a non-persisted invoice shaped like the execute-path delta invoice.
func previewProrationQuantityChangeInvoiceResponse(
	ctx context.Context,
	sub *subscription.Subscription,
	oldItem *subscription.SubscriptionLineItem,
	newItem *subscription.SubscriptionLineItem,
	effectiveDate time.Time,
	priceResp *dto.PriceResponse,
	netAmount decimal.Decimal,
) *dto.InvoiceResponse {
	billingCustomer := sub.GetInvoicingCustomerID()
	periodEnd := sub.CurrentPeriodEnd
	qtyDelta := newItem.Quantity.Sub(oldItem.Quantity)
	displayName := fmt.Sprintf("%s — Quantity Change Proration (%s – %s)",
		oldItem.PlanDisplayName,
		effectiveDate.Format("2 Jan 2006"),
		periodEnd.Format("2 Jan 2006"))
	priceID := oldItem.PriceID
	priceType := string(priceResp.Price.Type)
	planDisplayName := oldItem.PlanDisplayName
	lineItemDescription := fmt.Sprintf("Proration for quantity change: %s → %s units × %s %s/unit (%s – %s)",
		oldItem.Quantity.String(), newItem.Quantity.String(),
		strings.ToUpper(sub.Currency), priceResp.Price.Amount.String(),
		effectiveDate.Format("2 Jan 2006"), periodEnd.Format("2 Jan 2006"))

	subscriptionID := sub.ID
	subscriptionCustomerID := sub.CustomerID
	envID := types.GetEnvironmentID(ctx)
	bm := types.GetDefaultBaseModel(ctx)

	invLine := &invoice.InvoiceLineItem{
		ID:                    "(preview-line)",
		InvoiceID:             "(preview-invoice)",
		CustomerID:            billingCustomer,
		SubscriptionID:        &subscriptionID,
		PlanDisplayName:       &planDisplayName,
		PriceID:               &priceID,
		PriceType:             &priceType,
		DisplayName:           &displayName,
		Amount:                netAmount,
		Quantity:              qtyDelta,
		Currency:              sub.Currency,
		PeriodStart:           &effectiveDate,
		PeriodEnd:             &periodEnd,
		Metadata:              types.Metadata{"description": lineItemDescription},
		EnvironmentID:         envID,
		PrepaidCreditsApplied: decimal.Zero,
		LineItemDiscount:      decimal.Zero,
		InvoiceLevelDiscount:  decimal.Zero,
		BaseModel:             bm,
	}

	inv := &invoice.Invoice{
		ID:                         "(preview-invoice)",
		CustomerID:                 billingCustomer,
		SubscriptionID:             &subscriptionID,
		SubscriptionCustomerID:     &subscriptionCustomerID,
		InvoiceType:                types.InvoiceTypeOneOff,
		InvoiceStatus:              types.InvoiceStatusDraft,
		PaymentStatus:              types.PaymentStatusPending,
		Currency:                   sub.Currency,
		AmountDue:                  netAmount,
		AmountPaid:                 decimal.Zero,
		Subtotal:                   netAmount,
		Total:                      netAmount,
		TotalDiscount:              decimal.Zero,
		AmountRemaining:            netAmount,
		PeriodStart:                &effectiveDate,
		PeriodEnd:                  &periodEnd,
		BillingReason:              string(types.InvoiceBillingReasonSubscriptionUpdate),
		LineItems:                  []*invoice.InvoiceLineItem{invLine},
		Version:                    1,
		EnvironmentID:              envID,
		AdjustmentAmount:           decimal.Zero,
		RefundedAmount:             decimal.Zero,
		TotalTax:                   decimal.Zero,
		TotalPrepaidCreditsApplied: decimal.Zero,
		BaseModel:                  bm,
	}
	return dto.NewInvoiceResponse(inv)
}

func (s *subscriptionModificationService) previewProrationWalletTransactionResponse(
	ctx context.Context,
	sub *subscription.Subscription,
	currencyTopUpAmount decimal.Decimal,
) (*dto.WalletTransactionResponse, error) {
	billingCustomer := sub.GetInvoicingCustomerID()
	currency := sub.Currency

	topupRate := decimal.NewFromInt(1)
	walletSvc := NewWalletService(s.serviceParams)
	if billingCustomer != "" {
		existingWallets, err := walletSvc.GetWalletsByCustomerID(ctx, billingCustomer)
		if err != nil {
			return nil, err
		}
		var selected *dto.WalletResponse
		for _, w := range existingWallets {
			if w.WalletStatus == types.WalletStatusActive &&
				types.IsMatchingCurrency(w.Currency, currency) &&
				w.WalletType == types.WalletTypePrePaid {
				selected = w
				break
			}
		}
		if selected != nil && !selected.TopupConversionRate.IsZero() && selected.TopupConversionRate.GreaterThan(decimal.Zero) {
			topupRate = selected.TopupConversionRate
		}
	}

	creditAmount := walletSvc.GetCreditsFromCurrencyAmount(currencyTopUpAmount, topupRate)

	envID := types.GetEnvironmentID(ctx)
	bm := types.GetDefaultBaseModel(ctx)
	tx := &wallet.Transaction{
		ID:                  "(preview-wallet-credit)",
		CustomerID:          billingCustomer,
		Type:                types.TransactionTypeCredit,
		Amount:              currencyTopUpAmount,
		CreditAmount:        creditAmount,
		CreditBalanceBefore: decimal.Zero,
		CreditBalanceAfter:  creditAmount,
		TxStatus:            types.TransactionStatusCompleted,
		ReferenceType:       types.WalletTxReferenceTypeExternal,
		ReferenceID:         "preview",
		Description:         "Proration credit from subscription change (preview)",
		TransactionReason:   types.TransactionReasonSubscriptionCredit,
		Currency:            sub.Currency,
		EnvironmentID:       envID,
		BaseModel:           bm,
		TopupConversionRate: lo.ToPtr(topupRate),
	}
	return dto.FromWalletTransaction(tx), nil
}

// ─────────────────────────────────────────────
// Helper methods
// ─────────────────────────────────────────────

// resolveExternalCustomersForInheritance resolves published customers by external ID and validates
// they may receive an inherited subscription.
func (s *subscriptionModificationService) resolveExternalCustomersForInheritance(ctx context.Context, parentCustomerID string, externalIDs []string) ([]string, error) {
	// Step 1: fetch all subscription IDs belonging to the parent customer.
	// These are used to distinguish "already under this parent" (allowed) from
	// "under a different parent" (blocked).
	parentSubFilter := types.NewNoLimitSubscriptionFilter()
	parentSubFilter.CustomerID = parentCustomerID
	parentSubFilter.Status = lo.ToPtr(types.StatusPublished)
	parentSubFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusDraft,
		types.SubscriptionStatusTrialing,
	}
	parentSubFilter.WithLineItems = false
	parentSubs, err := s.serviceParams.SubRepo.List(ctx, parentSubFilter)
	if err != nil {
		return nil, err
	}
	parentSubIDs := make(map[string]bool, len(parentSubs))
	for _, sub := range parentSubs {
		parentSubIDs[sub.ID] = true
	}

	// Step 2: resolve child customers by external ID.
	childFilter := types.NewNoLimitCustomerFilter()
	childFilter.ExternalIDs = externalIDs
	childFilter.Status = lo.ToPtr(types.StatusPublished)
	customers, err := s.serviceParams.CustomerRepo.ListAll(ctx, childFilter)
	if err != nil {
		return nil, err
	}

	byExternalID := make(map[string]*customer.Customer, len(customers))
	for _, cust := range customers {
		byExternalID[cust.ExternalID] = cust
	}

	childCustomerIDs := make([]string, 0, len(externalIDs))
	for _, extID := range externalIDs {
		cust, ok := byExternalID[extID]
		if !ok {
			return nil, ierr.NewError("customer not found").
				WithHint("No customer exists for the given external id in this environment").
				WithReportableDetails(map[string]interface{}{"external_id": extID}).
				Mark(ierr.ErrNotFound)
		}
		if cust.ID == parentCustomerID {
			return nil, ierr.NewError("cannot inherit onto itself").
				WithHint("The subscriber cannot appear in external_customer_ids_to_inherit_subscription").
				WithReportableDetails(map[string]interface{}{"external_id": extID, "customer_id": cust.ID}).
				Mark(ierr.ErrValidation)
		}
		if cust.Status != types.StatusPublished {
			return nil, ierr.NewError("customer is not active").
				WithHint("Only active/published customers can receive inherited subscriptions").
				WithReportableDetails(map[string]interface{}{"external_id": extID, "customer_id": cust.ID}).
				Mark(ierr.ErrValidation)
		}

		// Step 3: fetch all active/draft/trialing published subscriptions for the child.
		childSubFilter := types.NewNoLimitSubscriptionFilter()
		childSubFilter.CustomerID = cust.ID
		childSubFilter.Status = lo.ToPtr(types.StatusPublished)
		childSubFilter.SubscriptionStatus = []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusDraft,
			types.SubscriptionStatusTrialing,
		}
		childSubFilter.WithLineItems = false
		childSubs, err := s.serviceParams.SubRepo.List(ctx, childSubFilter)
		if err != nil {
			return nil, err
		}

		// Step 4: check each subscription of the child.
		// Block if:
		//   - subscription has no parent (child has their own standalone/parent subscription), OR
		//   - subscription's parent belongs to a different parent customer (not parentSubIDs)
		for _, childSub := range childSubs {
			if childSub.ParentSubscriptionID == nil {
				// Child has a standalone or parent subscription of their own
				return nil, ierr.NewError("child customer has standalone or parent subscriptions").
					WithHint("The child customer cannot have standalone or parent subscriptions").
					WithReportableDetails(map[string]interface{}{"external_id": extID, "customer_id": cust.ID}).
					Mark(ierr.ErrValidation)
			}
			if !parentSubIDs[*childSub.ParentSubscriptionID] {
				// Child is already inherited under a different parent
				return nil, ierr.NewError("child customer already has a parent subscription").
					WithHint("A customer can only be inherited under one parent").
					WithReportableDetails(map[string]interface{}{"external_id": extID, "customer_id": cust.ID}).
					Mark(ierr.ErrValidation)
			}
		}

		childCustomerIDs = append(childCustomerIDs, cust.ID)
	}
	return childCustomerIDs, nil
}

// getInheritedSubscriptions retrieves all INHERITED child subscriptions for a parent subscription.
func (s *subscriptionModificationService) getInheritedSubscriptions(ctx context.Context, parentSubID string) ([]*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.ParentSubscriptionIDs = []string{parentSubID}
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusDraft,
		types.SubscriptionStatusPaused,
	}
	return s.serviceParams.SubRepo.List(ctx, filter)
}

// createInheritedSubscription creates a child inherited subscription from a parent.
func (s *subscriptionModificationService) createInheritedSubscription(ctx context.Context, parent *subscription.Subscription, childCustomerID string) (*subscription.Subscription, error) {
	inheritedSub := &subscription.Subscription{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:             childCustomerID,
		PlanID:                 parent.PlanID,
		Currency:               parent.Currency,
		LookupKey:              "",
		SubscriptionStatus:     parent.SubscriptionStatus,
		BillingAnchor:          parent.BillingAnchor,
		BillingCycle:           parent.BillingCycle,
		StartDate:              parent.StartDate,
		EndDate:                parent.EndDate,
		CurrentPeriodStart:     parent.CurrentPeriodStart,
		CurrentPeriodEnd:       parent.CurrentPeriodEnd,
		BillingCadence:         parent.BillingCadence,
		BillingPeriod:          parent.BillingPeriod,
		BillingPeriodCount:     parent.BillingPeriodCount,
		Version:                1,
		EnvironmentID:          parent.EnvironmentID,
		PauseStatus:            parent.PauseStatus,
		PaymentBehavior:        parent.PaymentBehavior,
		CollectionMethod:       parent.CollectionMethod,
		GatewayPaymentMethodID: parent.GatewayPaymentMethodID,
		Timezone:               parent.Timezone,
		ProrationBehavior:      parent.ProrationBehavior,
		ParentSubscriptionID:   &parent.ID,
		SubscriptionType:       types.SubscriptionTypeInherited,
		PaymentTerms:           parent.PaymentTerms,
		EnableTrueUp:           parent.EnableTrueUp,
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
	if err := s.serviceParams.SubRepo.Create(ctx, inheritedSub); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create inherited subscription for child customer").
			WithReportableDetails(map[string]interface{}{
				"parent_subscription_id": parent.ID,
				"child_customer_id":      childCustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return inheritedSub, nil
}

// publishSystemEvent publishes a webhook event for a subscription change.
func (s *subscriptionModificationService) publishSystemEvent(ctx context.Context, eventName types.WebhookEventName, subscriptionID string) {
	eventPayload := webhookDto.InternalSubscriptionEvent{
		SubscriptionID: subscriptionID,
		TenantID:       types.GetTenantID(ctx),
	}

	webhookPayload, err := json.Marshal(eventPayload)
	if err != nil {
		s.serviceParams.Logger.Error(ctx, "failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
		EntityType:    types.SystemEntityTypeSubscription,
		EntityID:      subscriptionID,
	}
	if err := s.serviceParams.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.serviceParams.Logger.Error(ctx, "failed to publish webhook event", "event_name", webhookEvent.EventName, "error", err)
	}
}

// ─────────────────────────────────────────────
// Sub-feature: Remove Inheritance
// ─────────────────────────────────────────────

// resolveCustomersByExternalIDs converts external customer IDs to internal IDs.
// Unlike resolveExternalCustomersForInheritance, this does not require StatusPublished
// since we are removing (not adding) children.
func (s *subscriptionModificationService) resolveCustomersByExternalIDs(ctx context.Context, externalIDs []string) ([]string, error) {
	childFilter := types.NewNoLimitCustomerFilter()
	childFilter.ExternalIDs = externalIDs
	childFilter.Status = lo.ToPtr(types.StatusPublished)
	customers, err := s.serviceParams.CustomerRepo.List(ctx, childFilter)
	if err != nil {
		return nil, err
	}

	byExternalID := make(map[string]*customer.Customer, len(customers))
	for _, c := range customers {
		byExternalID[c.ExternalID] = c
	}

	result := make([]string, 0, len(externalIDs))
	for _, extID := range externalIDs {
		c, ok := byExternalID[extID]
		if !ok {
			return nil, ierr.NewError("customer not found").
				WithHint("No customer exists for the given external ID").
				WithReportableDetails(map[string]interface{}{"external_id": extID}).
				Mark(ierr.ErrNotFound)
		}
		result = append(result, c.ID)
	}
	return result, nil
}

// getInheritedSubscriptionsForChildCustomer returns active, trialing, or paused inherited
// subscriptions for a child customer under the specified parent subscription.
func (s *subscriptionModificationService) getInheritedSubscriptionsForChildCustomer(ctx context.Context, parentSubID, childCustomerID string) ([]*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.ParentSubscriptionIDs = []string{parentSubID}
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusPaused,
	}
	filter.CustomerID = childCustomerID

	subs, err := s.serviceParams.SubRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, ierr.NewError("inherited subscription not found for child customer").
			WithHint("No active inherited subscription exists for this child customer under the given parent").
			WithReportableDetails(map[string]interface{}{
				"parent_subscription_id": parentSubID,
				"child_customer_id":      childCustomerID,
			}).
			Mark(ierr.ErrNotFound)
	}
	return subs, nil
}

func (s *subscriptionModificationService) getInheritedSubscriptionsForChildCustomers(
	ctx context.Context,
	parentSubID string,
	childCustomerIDs []string,
) ([]*subscription.Subscription, error) {
	childSubs := make([]*subscription.Subscription, 0, len(childCustomerIDs))
	for _, childCustomerID := range childCustomerIDs {
		subs, err := s.getInheritedSubscriptionsForChildCustomer(ctx, parentSubID, childCustomerID)
		if err != nil {
			return nil, err
		}
		childSubs = append(childSubs, subs...)
	}
	return childSubs, nil
}

func validateInheritedSubscriptionsNotScheduledForRemoval(subs []*subscription.Subscription) error {
	if sub, found := lo.Find(subs, func(sub *subscription.Subscription) bool {
		return sub.CancelAt != nil
	}); found {
		return ierr.NewError("inherited subscription is already scheduled for removal").
			WithHint("The inherited subscription already has a scheduled cancellation").
			WithReportableDetails(map[string]interface{}{
				"child_subscription_id": sub.ID,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (s *subscriptionModificationService) executeRemoveInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// 1. Fetch and validate parent
	parentSub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil, ierr.NewError("subscription is not a parent subscription").
			WithHint("Only parent subscriptions can have inherited children removed").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":   subscriptionID,
				"subscription_type": parentSub.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}
	if parentSub.SubscriptionStatus != types.SubscriptionStatusActive &&
		parentSub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return nil, ierr.NewError("parent subscription is not active or trialing").
			WithHint("The parent subscription must be active or trialing to remove inherited children").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"status":          parentSub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// 2. Resolve external customer IDs → internal IDs (no status check for remove)
	externalIDs := lo.Uniq(params.ExternalCustomerIDsToRemove)
	childCustomerIDs, err := s.resolveCustomersByExternalIDs(ctx, externalIDs)
	if err != nil {
		return nil, err
	}

	// 3. Find each child's inherited sub and guard against double-scheduling
	childSubs, err := s.getInheritedSubscriptionsForChildCustomers(ctx, subscriptionID, childCustomerIDs)
	if err != nil {
		return nil, err
	}
	if err := validateInheritedSubscriptionsNotScheduledForRemoval(childSubs); err != nil {
		return nil, err
	}

	// 4. Effective date = parent's current period end
	effectiveDate := parentSub.CurrentPeriodEnd

	// 5. Transaction: schedule each inherited sub for cancellation at period end
	changedSubs := make([]dto.ChangedSubscription, 0, len(childSubs))
	err = sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		changedSubs = nil
		for _, childSub := range childSubs {
			childSub.CancelAt = lo.ToPtr(effectiveDate)
			childSub.CancelAtPeriodEnd = true
			if err := sp.SubRepo.Update(txCtx, childSub); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to schedule inherited subscription for removal").
					WithReportableDetails(map[string]interface{}{
						"child_subscription_id": childSub.ID,
					}).
					Mark(ierr.ErrDatabase)
			}
			changedSubs = append(changedSubs, dto.ChangedSubscription{
				ID:               childSub.ID,
				Action:           dto.ChangedSubscriptionActionUpdated,
				Status:           childSub.SubscriptionStatus,
				CurrentPeriodEnd: lo.ToPtr(effectiveDate),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 6. Publish webhook event
	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	// 7. Return response with parent subscription and changed children
	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}

func (s *subscriptionModificationService) previewRemoveInheritance(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyInheritanceRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Validate parent (same as execute, no DB writes)
	parentSub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil, ierr.NewError("subscription is not a parent subscription").
			WithHint("Only parent subscriptions can have inherited children removed").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":   subscriptionID,
				"subscription_type": parentSub.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}
	if parentSub.SubscriptionStatus != types.SubscriptionStatusActive &&
		parentSub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return nil, ierr.NewError("parent subscription is not active or trialing").
			WithHint("The parent subscription must be active or trialing to remove inherited children").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"status":          parentSub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Resolve customers
	externalIDs := lo.Uniq(params.ExternalCustomerIDsToRemove)
	childCustomerIDs, err := s.resolveCustomersByExternalIDs(ctx, externalIDs)
	if err != nil {
		return nil, err
	}

	// Validate children and build preview response (no DB mutations)
	effectiveDate := parentSub.CurrentPeriodEnd
	childSubs, err := s.getInheritedSubscriptionsForChildCustomers(ctx, subscriptionID, childCustomerIDs)
	if err != nil {
		return nil, err
	}
	if err := validateInheritedSubscriptionsNotScheduledForRemoval(childSubs); err != nil {
		return nil, err
	}
	changedSubs := lo.Map(childSubs, func(childSub *subscription.Subscription, _ int) dto.ChangedSubscription {
		return dto.ChangedSubscription{
			ID:               childSub.ID,
			Action:           dto.ChangedSubscriptionActionUpdated,
			Status:           childSub.SubscriptionStatus,
			CurrentPeriodEnd: lo.ToPtr(effectiveDate),
		}
	})

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}
