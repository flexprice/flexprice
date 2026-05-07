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
	"github.com/flexprice/flexprice/internal/domain/proration"
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
	default:
		return nil, ierr.NewError("unknown modification type: " + string(req.Type)).
			WithHint("Valid values: inheritance, quantity_change").
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
	default:
		return nil, ierr.NewError("unknown modification type: " + string(req.Type)).
			WithHint("Valid values: inheritance, quantity_change").
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
// ─────────────────────────────────────────────

// validateQuantityChangeEffectiveDateWithinLineItemWindow ensures effectiveDate lies in
// [lineItem.StartDate, lineEnd), where lineEnd is lineItem.EndDate when set, otherwise
// sub.CurrentPeriodEnd (open-ended line item). Subscription period bounds are validated separately.
func validateQuantityChangeEffectiveDateWithinLineItemWindow(
	effectiveDate time.Time,
	sub *subscription.Subscription,
	lineItem *subscription.SubscriptionLineItem,
	lineItemID string,
) error {
	if !lineItem.StartDate.IsZero() && effectiveDate.Before(lineItem.StartDate) {
		return ierr.NewError("effective_date cannot be before the line item start date").
			WithHint("Set effective_date to a time when the line item is active").
			WithReportableDetails(map[string]interface{}{
				"effective_date":  effectiveDate,
				"line_item_id":    lineItemID,
				"line_item_start": lineItem.StartDate,
			}).
			Mark(ierr.ErrValidation)
	}
	lineEnd := sub.CurrentPeriodEnd
	if !lineItem.EndDate.IsZero() {
		lineEnd = lineItem.EndDate
	}
	if !effectiveDate.Before(lineEnd) {
		return ierr.NewError("effective_date must be before the line item end date").
			WithHint("Set effective_date to a time before the line item's active window ends").
			WithReportableDetails(map[string]interface{}{
				"effective_date": effectiveDate,
				"line_item_id":   lineItemID,
				"line_item_end":  lineEnd,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (s *subscriptionModificationService) executeQuantityChange(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyQuantityChangeRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Get subscription with line items
	sub, _, err := sp.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Validate subscription is active
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can have quantity changes applied").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	changedLineItems := make([]dto.ChangedLineItem, 0)
	changedInvoices := make([]dto.ChangedInvoice, 0)

	// itemsForProration accumulates pairs of old/new line items + their effective dates.
	// It is populated inside the transaction and consumed after it commits.
	type prorationPair struct {
		old           *subscription.SubscriptionLineItem
		new_          *subscription.SubscriptionLineItem
		effectiveDate time.Time
	}
	var itemsForProration []prorationPair

	// Single transaction: end all old line items and create all new ones atomically.
	// Proration (invoice creation / wallet credits) happens after the transaction commits
	// because those operations have their own side effects.
	err = sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		changedLineItems = nil  // reset for safety
		itemsForProration = nil // reset for safety

		for _, change := range params.LineItems {
			// Resolve effective date: caller-supplied or now.
			// Backdating within the current period is allowed (e.g. to backfill a change);
			// dates before the period start or at/after the period end are rejected.
			effectiveDate := now
			if change.EffectiveDate != nil {
				effectiveDate = change.EffectiveDate.UTC()
			}
			if effectiveDate.Before(sub.CurrentPeriodStart) {
				return ierr.NewError("effective_date cannot be before the current period start").
					WithHint("Set effective_date to a time within the current billing period").
					WithReportableDetails(map[string]interface{}{
						"effective_date":       effectiveDate,
						"current_period_start": sub.CurrentPeriodStart,
					}).
					Mark(ierr.ErrValidation)
			}
			if !effectiveDate.Before(sub.CurrentPeriodEnd) {
				return ierr.NewError("effective_date must be before the current period end").
					WithHint("Set effective_date to a time within the current billing period").
					WithReportableDetails(map[string]interface{}{
						"effective_date":     effectiveDate,
						"current_period_end": sub.CurrentPeriodEnd,
					}).
					Mark(ierr.ErrValidation)
			}

			// Fetch line item
			lineItem, err := sp.SubscriptionLineItemRepo.Get(txCtx, change.ID)
			if err != nil {
				return err
			}

			// Validate it belongs to the subscription
			if lineItem.SubscriptionID != subscriptionID {
				return ierr.NewError("line item does not belong to subscription").
					WithHint("The specified line item ID must belong to the given subscription").
					WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "subscription_id": subscriptionID}).
					Mark(ierr.ErrValidation)
			}

			// Validate it is published (active)
			if lineItem.Status != types.StatusPublished {
				return ierr.NewError("line item is not active").
					WithHint("Only published line items can have their quantity changed").
					WithReportableDetails(map[string]interface{}{"line_item_id": change.ID}).
					Mark(ierr.ErrValidation)
			}

			// Validate it is a fixed-price item
			if lineItem.PriceType != types.PRICE_TYPE_FIXED {
				return ierr.NewError("line item is not a fixed-price item").
					WithHint("Quantity changes are only supported for fixed-price line items").
					WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "price_type": lineItem.PriceType}).
					Mark(ierr.ErrValidation)
			}

			if err := validateQuantityChangeEffectiveDateWithinLineItemWindow(effectiveDate, sub, lineItem, change.ID); err != nil {
				return err
			}

			// Skip no-op: quantity unchanged avoids unnecessary DB writes and a spurious invoice.
			if change.Quantity.Equal(lineItem.Quantity) {
				sp.Logger.Debugw("skipping quantity change: quantity is unchanged",
					"line_item_id", change.ID, "quantity", change.Quantity)
				continue
			}

			// Capture original end date before mutation (used later for changed_resources).
			originalEndDate := lineItem.EndDate

			// End the old line item at effective date
			lineItem.EndDate = effectiveDate
			if err := sp.SubscriptionLineItemRepo.Update(txCtx, lineItem); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to end existing line item").
					Mark(ierr.ErrDatabase)
			}

			// Create new line item (copy with new quantity)
			newItem := &subscription.SubscriptionLineItem{
				ID:                      types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
				SubscriptionID:          lineItem.SubscriptionID,
				CustomerID:              lineItem.CustomerID,
				EntityID:                lineItem.EntityID,
				EntityType:              lineItem.EntityType,
				PlanDisplayName:         lineItem.PlanDisplayName,
				PriceID:                 lineItem.PriceID,
				PriceType:               lineItem.PriceType,
				MeterID:                 lineItem.MeterID,
				MeterDisplayName:        lineItem.MeterDisplayName,
				PriceUnitID:             lineItem.PriceUnitID,
				PriceUnit:               lineItem.PriceUnit,
				DisplayName:             lineItem.DisplayName,
				Quantity:                change.Quantity,
				Currency:                lineItem.Currency,
				BillingPeriod:           lineItem.BillingPeriod,
				BillingPeriodCount:      lineItem.BillingPeriodCount,
				InvoiceCadence:          lineItem.InvoiceCadence,
				StartDate:               effectiveDate,
				CommitmentAmount:        lineItem.CommitmentAmount,
				CommitmentQuantity:      lineItem.CommitmentQuantity,
				CommitmentType:          lineItem.CommitmentType,
				CommitmentOverageFactor: lineItem.CommitmentOverageFactor,
				CommitmentTrueUpEnabled: lineItem.CommitmentTrueUpEnabled,
				CommitmentWindowed:      lineItem.CommitmentWindowed,
				CommitmentDuration:      lineItem.CommitmentDuration,
				EnvironmentID:           lineItem.EnvironmentID,
				BaseModel:               types.GetDefaultBaseModel(txCtx),
			}
			if err := sp.SubscriptionLineItemRepo.Create(txCtx, newItem); err != nil {
				return err
			}

			oldStart := lineItem.StartDate
			endDate := effectiveDate
			startDate := effectiveDate
			// New item runs from effectiveDate until the original item's end (or period end if open-ended).
			newEndDate := sub.CurrentPeriodEnd
			if !originalEndDate.IsZero() {
				newEndDate = originalEndDate
			}
			changedLineItems = append(changedLineItems,
				dto.ChangedLineItem{
					ID:           lineItem.ID,
					PriceID:      lineItem.PriceID,
					Quantity:     lineItem.Quantity,
					StartDate:    &oldStart,
					EndDate:      &endDate,
					ChangeAction: dto.ChangedLineItemActionEnded,
				},
				dto.ChangedLineItem{
					ID:           newItem.ID,
					PriceID:      newItem.PriceID,
					Quantity:     newItem.Quantity,
					StartDate:    &startDate,
					EndDate:      &newEndDate,
					ChangeAction: dto.ChangedLineItemActionCreated,
				},
			)

			// Collect pairs that need proration (handled after the transaction commits).
			// ADVANCE items are billed upfront, so any mid-cycle quantity change requires a proration.
			if lineItem.InvoiceCadence == types.InvoiceCadenceAdvance {
				itemsForProration = append(itemsForProration, prorationPair{old: lineItem, new_: newItem, effectiveDate: effectiveDate})
			}
		}

		// Post-transaction: handle proration outside the DB transaction because invoice creation
		// and wallet top-ups carry their own side effects (payment attempts, credit grants).
		for _, pair := range itemsForProration {
			inv, err := s.handleQuantityChangeProration(ctx, sub, pair.old, pair.new_, pair.effectiveDate)
			if err != nil {
				return err
			}
			if inv != nil {
				changedInvoices = append(changedInvoices, *inv)
			}
		}

		return nil
	})
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

	// Get subscription with line items
	sub, _, err := sp.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Validate subscription is active
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can have quantity changes applied").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	changedLineItems := make([]dto.ChangedLineItem, 0)
	changedInvoices := make([]dto.ChangedInvoice, 0)

	for _, change := range params.LineItems {
		// Resolve effective date: caller-supplied or now.
		// Must be >= now (no backdating) and before the current period end.
		effectiveDate := now
		if change.EffectiveDate != nil {
			effectiveDate = change.EffectiveDate.UTC()
		}
		if effectiveDate.Before(sub.CurrentPeriodStart) {
			return nil, ierr.NewError("effective_date cannot be before the current period start").
				WithHint("Set effective_date to a time within the current billing period").
				WithReportableDetails(map[string]interface{}{
					"effective_date":       effectiveDate,
					"current_period_start": sub.CurrentPeriodStart,
				}).
				Mark(ierr.ErrValidation)
		}
		if !effectiveDate.Before(sub.CurrentPeriodEnd) {
			return nil, ierr.NewError("effective_date must be before the current period end").
				WithHint("Set effective_date to a time within the current billing period").
				WithReportableDetails(map[string]interface{}{
					"effective_date":     effectiveDate,
					"current_period_end": sub.CurrentPeriodEnd,
				}).
				Mark(ierr.ErrValidation)
		}

		lineItem, err := sp.SubscriptionLineItemRepo.Get(ctx, change.ID)
		if err != nil {
			return nil, err
		}

		if lineItem.SubscriptionID != subscriptionID {
			return nil, ierr.NewError("line item does not belong to subscription").
				WithHint("The specified line item ID must belong to the given subscription").
				WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "subscription_id": subscriptionID}).
				Mark(ierr.ErrValidation)
		}

		if lineItem.Status != types.StatusPublished {
			return nil, ierr.NewError("line item is not active").
				WithHint("Only published line items can have their quantity changed").
				WithReportableDetails(map[string]interface{}{"line_item_id": change.ID}).
				Mark(ierr.ErrValidation)
		}

		if lineItem.PriceType != types.PRICE_TYPE_FIXED {
			return nil, ierr.NewError("line item is not a fixed-price item").
				WithHint("Quantity changes are only supported for fixed-price line items").
				WithReportableDetails(map[string]interface{}{"line_item_id": change.ID, "price_type": lineItem.PriceType}).
				Mark(ierr.ErrValidation)
		}

		if err := validateQuantityChangeEffectiveDateWithinLineItemWindow(effectiveDate, sub, lineItem, change.ID); err != nil {
			return nil, err
		}

		// Skip no-op: same quantity as current.
		if change.Quantity.Equal(lineItem.Quantity) {
			continue
		}

		oldStart := lineItem.StartDate
		endDate := effectiveDate
		startDate := effectiveDate
		// New item runs from effectiveDate until the old item's end (or period end if open-ended).
		newEndDate := sub.CurrentPeriodEnd
		if !lineItem.EndDate.IsZero() {
			newEndDate = lineItem.EndDate
		}
		changedLineItems = append(changedLineItems,
			dto.ChangedLineItem{
				ID:           "(preview-ended)",
				PriceID:      lineItem.PriceID,
				Quantity:     lineItem.Quantity,
				StartDate:    &oldStart,
				EndDate:      &endDate,
				ChangeAction: dto.ChangedLineItemActionEnded,
			},
			dto.ChangedLineItem{
				ID:           "(preview-created)",
				PriceID:      lineItem.PriceID,
				Quantity:     change.Quantity,
				StartDate:    &startDate,
				EndDate:      &newEndDate,
				ChangeAction: dto.ChangedLineItemActionCreated,
			},
		)

		// Preview proration for ADVANCE items — calculate only, do NOT create invoices or wallet credits.
		// Always preview for ADVANCE items regardless of proration_behavior (same reasoning as execute).
		if lineItem.InvoiceCadence == types.InvoiceCadenceAdvance {
			previewNewItem := &subscription.SubscriptionLineItem{
				PriceID:  lineItem.PriceID,
				Quantity: change.Quantity,
			}
			inv, err := s.previewQuantityChangeProration(ctx, sub, lineItem, previewNewItem, effectiveDate)
			if err != nil {
				sp.Logger.Warnw("failed to preview proration for quantity change", "error", err, "line_item_id", lineItem.ID)
			} else if inv != nil {
				changedInvoices = append(changedInvoices, *inv)
			}
		}
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

// handleQuantityChangeProration handles the proration logic for quantity changes on in-advance line items.
func (s *subscriptionModificationService) handleQuantityChangeProration(
	ctx context.Context,
	sub *subscription.Subscription,
	oldItem *subscription.SubscriptionLineItem,
	newItem *subscription.SubscriptionLineItem,
	effectiveDate time.Time,
) (*dto.ChangedInvoice, error) {
	sp := s.serviceParams
	prorationSvc := NewProrationService(sp)
	priceSvc := NewPriceService(sp)

	price, err := priceSvc.GetPrice(ctx, oldItem.PriceID)
	if err != nil {
		return nil, err
	}

	customerTimezone := sub.CustomerTimezone
	if customerTimezone == "" {
		customerTimezone = "UTC"
	}

	prorationParams := proration.ProrationParams{
		SubscriptionID:     sub.ID,
		LineItemID:         oldItem.ID,
		PlanPayInAdvance:   price.Price.InvoiceCadence == types.InvoiceCadenceAdvance,
		CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd:   sub.CurrentPeriodEnd.Add(-time.Second),
		Action:             types.ProrationActionQuantityChange,
		NewPriceID:         newItem.PriceID,
		OldQuantity:        oldItem.Quantity,
		NewQuantity:        newItem.Quantity,
		NewPricePerUnit:    price.Price.Amount,
		OldPricePerUnit:    price.Price.Amount,
		ProrationDate:      effectiveDate,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		ProrationStrategy:  types.StrategySecondBased,
		Currency:           sub.Currency,
		PlanDisplayName:    oldItem.PlanDisplayName,
		CustomerTimezone:   customerTimezone,
	}

	result, err := prorationSvc.CalculateProration(ctx, prorationParams)
	if err != nil {
		return nil, err
	}

	if result.NetAmount.IsZero() {
		return nil, nil
	}

	if result.NetAmount.GreaterThan(decimal.Zero) {
		// Upgrade: create a delta-only invoice for exactly the prorated amount (Stripe-style).
		// We do NOT re-bill the full remaining period — only the incremental charge.
		invoiceSvc := NewInvoiceService(sp)
		periodEnd := sub.CurrentPeriodEnd
		billingPeriod := string(sub.BillingPeriod)
		billingCustomer := sub.GetInvoicingCustomerID()

		// Build a descriptive line item for the delta charge.
		qtyDelta := newItem.Quantity.Sub(oldItem.Quantity)
		displayName := fmt.Sprintf("%s — Quantity Change Proration (%s – %s)",
			oldItem.PlanDisplayName,
			effectiveDate.Format("2 Jan 2006"),
			periodEnd.Format("2 Jan 2006"))
		priceID := oldItem.PriceID
		priceType := string(price.Price.Type)
		planDisplayName := oldItem.PlanDisplayName
		lineItemDescription := fmt.Sprintf("Proration for quantity change: %s → %s units × %s %s/unit (%s – %s)",
			oldItem.Quantity.String(), newItem.Quantity.String(),
			strings.ToUpper(sub.Currency), price.Price.Amount.String(),
			effectiveDate.Format("2 Jan 2006"), periodEnd.Format("2 Jan 2006"))
		lineItems := []dto.CreateInvoiceLineItemRequest{
			{
				PriceID:         &priceID,
				PriceType:       &priceType,
				PlanDisplayName: &planDisplayName,
				DisplayName:     &displayName,
				Amount:          result.NetAmount,
				Quantity:        qtyDelta,
				PeriodStart:     &effectiveDate,
				PeriodEnd:       &periodEnd,
				Metadata:        types.Metadata{"description": lineItemDescription},
			},
		}

		// Use InvoiceTypeOneOff so ComputeInvoice uses the explicit delta amount rather
		// than recomputing from the subscription's (now-updated) line items.
		// SubscriptionID is set for reference/traceability only.
		inv, err := invoiceSvc.CreateInvoice(ctx, dto.CreateInvoiceRequest{
			CustomerID:     billingCustomer,
			SubscriptionID: &sub.ID,
			InvoiceType:    types.InvoiceTypeOneOff,
			Currency:       sub.Currency,
			BillingReason:  types.InvoiceBillingReasonSubscriptionUpdate,
			AmountDue:      result.NetAmount,
			Total:          result.NetAmount,
			Subtotal:       result.NetAmount,
			PeriodStart:    &effectiveDate,
			PeriodEnd:      &periodEnd,
			BillingPeriod:  &billingPeriod,
			LineItems:      lineItems,
		})
		if err != nil {
			sp.Logger.Errorw("failed to create delta proration invoice for quantity change", "error", err)
			return nil, err
		}
		// CreateInvoice with InvoiceTypeOneOff already finalizes the invoice internally.
		// Attempt payment (credits + payment method charge).
		if err := invoiceSvc.AttemptPayment(ctx, inv.ID); err != nil {
			sp.Logger.Warnw("failed to attempt payment for delta proration invoice", "error", err, "invoice_id", inv.ID)
		}
		// Re-fetch to get latest payment status after finalize+payment attempt.
		latest, fetchErr := invoiceSvc.GetInvoice(ctx, inv.ID)
		if fetchErr != nil {
			latest = inv
		}
		return &dto.ChangedInvoice{
			ID:      latest.ID,
			Action:  dto.ChangedInvoiceActionCreated,
			Status:  dto.ChangedInvoiceStatusFromPaymentStatus(latest.PaymentStatus),
			Invoice: latest,
		}, nil
	}

	// Downgrade: wallet credit
	walletSvc := NewWalletService(sp)
	creditAmount := result.NetAmount.Abs()
	billingCustomer := sub.GetInvoicingCustomerID()
	// Stable idempotency key: prevents duplicate credits if this call is retried.
	idempotencyKey := fmt.Sprintf("proration_credit_%s_%s_%s", sub.ID, oldItem.ID, effectiveDate.Format(time.RFC3339))
	walletTx, err := walletSvc.TopUpWalletForProratedCharge(ctx, billingCustomer, creditAmount, sub.Currency, idempotencyKey)
	if err != nil {
		sp.Logger.Errorw("failed to top up wallet for downgrade proration", "error", err)
		return nil, err
	}
	changedID := "(wallet_credit)"
	if walletTx != nil && walletTx.Transaction != nil && walletTx.ID != "" {
		changedID = walletTx.ID
	}
	return &dto.ChangedInvoice{
		ID:                changedID,
		Action:            dto.ChangedInvoiceActionWalletCredit,
		Status:            dto.ChangedInvoiceStatusWalletIssued,
		WalletTransaction: walletTx,
	}, nil
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

// previewQuantityChangeProration calculates what proration would occur without creating any
// invoices, wallet credits, or other side effects. Safe to call from the preview endpoint.
func (s *subscriptionModificationService) previewQuantityChangeProration(
	ctx context.Context,
	sub *subscription.Subscription,
	oldItem *subscription.SubscriptionLineItem,
	newItem *subscription.SubscriptionLineItem,
	effectiveDate time.Time,
) (*dto.ChangedInvoice, error) {
	sp := s.serviceParams
	prorationSvc := NewProrationService(sp)
	priceSvc := NewPriceService(sp)

	price, err := priceSvc.GetPrice(ctx, oldItem.PriceID)
	if err != nil {
		return nil, err
	}

	customerTimezone := sub.CustomerTimezone
	if customerTimezone == "" {
		customerTimezone = "UTC"
	}

	prorationParams := proration.ProrationParams{
		SubscriptionID:     sub.ID,
		LineItemID:         oldItem.ID,
		PlanPayInAdvance:   price.Price.InvoiceCadence == types.InvoiceCadenceAdvance,
		CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd:   sub.CurrentPeriodEnd.Add(-time.Second),
		Action:             types.ProrationActionQuantityChange,
		NewPriceID:         newItem.PriceID,
		OldQuantity:        oldItem.Quantity,
		NewQuantity:        newItem.Quantity,
		NewPricePerUnit:    price.Price.Amount,
		OldPricePerUnit:    price.Price.Amount,
		ProrationDate:      effectiveDate,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		ProrationStrategy:  types.StrategySecondBased,
		Currency:           sub.Currency,
		PlanDisplayName:    oldItem.PlanDisplayName,
		CustomerTimezone:   customerTimezone,
	}

	result, err := prorationSvc.CalculateProration(ctx, prorationParams)
	if err != nil {
		return nil, err
	}

	if result.NetAmount.IsZero() {
		return nil, nil
	}

	// Return a preview-only ChangedInvoice — no invoice is created, no payment attempted.
	if result.NetAmount.GreaterThan(decimal.Zero) {
		invResp := previewProrationQuantityChangeInvoiceResponse(ctx, sub, oldItem, newItem, effectiveDate, price, result.NetAmount)
		return &dto.ChangedInvoice{
			ID:      "(preview-invoice)",
			Action:  dto.ChangedInvoiceActionCreated,
			Status:  dto.ChangedInvoiceStatusPreview,
			Invoice: invResp,
		}, nil
	}
	walletTx, err := s.previewProrationWalletTransactionResponse(ctx, sub, result.NetAmount.Abs())
	if err != nil {
		return nil, err
	}
	return &dto.ChangedInvoice{
		ID:                "(preview-wallet-credit)",
		Action:            dto.ChangedInvoiceActionWalletCredit,
		Status:            dto.ChangedInvoiceStatusPreview,
		WalletTransaction: walletTx,
	}, nil
}

// ─────────────────────────────────────────────
// Helper methods
// ─────────────────────────────────────────────

// resolveExternalCustomersForInheritance resolves published customers by external ID and validates
// they may receive an inherited subscription.
func (s *subscriptionModificationService) resolveExternalCustomersForInheritance(ctx context.Context, subscriberCustomerID string, externalIDs []string) ([]string, error) {
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
		if cust.ID == subscriberCustomerID {
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
		CustomerTimezone:       parent.CustomerTimezone,
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
		s.serviceParams.Logger.ErrorwCtx(ctx, "failed to marshal webhook payload", "error", err)
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
		s.serviceParams.Logger.ErrorfCtx(ctx, "failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}
