package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/interfaces"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"

	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

type SubscriptionService = interfaces.SubscriptionService
type subscriptionService struct {
	ServiceParams
}

func NewSubscriptionService(params ServiceParams) SubscriptionService {
	return &subscriptionService{
		ServiceParams: params,
	}
}

func (s *subscriptionService) CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	if req.BillingCycle == "" {
		req.BillingCycle = types.BillingCycleAnniversary
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get and validate customer
	var customer *customer.Customer
	var err error
	if req.CustomerID != "" {
		customer, err = s.CustomerRepo.Get(ctx, req.CustomerID)
	} else {
		customer, err = s.CustomerRepo.GetByLookupKey(ctx, req.ExternalCustomerID)
		if err == nil {
			req.CustomerID = customer.ID
		}
	}
	if err != nil {
		return nil, err
	}
	if customer.Status != types.StatusPublished {
		return nil, ierr.NewError("customer is not active").
			WithHint("The customer must be active to create a subscription").
			WithReportableDetails(map[string]interface{}{"customer_id": req.CustomerID, "status": customer.Status}).
			Mark(ierr.ErrValidation)
	}

	// Handle InvoiceBilling to set InvoicingCustomerID internally
	// The DTO layer ensures InvoiceBilling is always set (defaults to invoice_to_self)
	// For invoice_to_self, we don't need to set InvoicingCustomerID as it defaults to subscription customer
	if lo.FromPtr(req.InvoiceBilling) == types.InvoiceBillingInvoiceToParent {
		if customer.ParentCustomerID == nil {
			return nil, ierr.NewError("customer does not have a parent customer").
				WithHint("The customer must have a parent customer to use invoice_to_parent").
				WithReportableDetails(map[string]interface{}{"customer_id": req.CustomerID}).
				Mark(ierr.ErrValidation)
		}
		req.InvoicingCustomerID = customer.ParentCustomerID
	}

	// Validate that the invoicing customer exists and is active
	if req.InvoicingCustomerID != nil && *req.InvoicingCustomerID != "" {
		invoicingCustomer, err := s.CustomerRepo.Get(ctx, *req.InvoicingCustomerID)
		if err != nil {
			return nil, err
		}
		if invoicingCustomer.Status != types.StatusPublished {
			return nil, ierr.NewError("invoicing customer is not active").
				WithHint("The invoicing customer must be active").
				WithReportableDetails(map[string]interface{}{"invoicing_customer_id": *req.InvoicingCustomerID, "status": invoicingCustomer.Status}).
				Mark(ierr.ErrValidation)
		}
	}

	// Get and validate plan
	plan, err := s.PlanRepo.Get(ctx, req.PlanID)
	if err != nil {
		return nil, err
	}
	if plan.Status != types.StatusPublished {
		return nil, ierr.NewError("plan is not active").
			WithHint("The plan must be active to create a subscription").
			WithReportableDetails(map[string]interface{}{"plan_id": req.PlanID, "status": plan.Status}).
			Mark(ierr.ErrValidation)
	}

	sub := req.ToSubscription(ctx)

	// Validate and filter prices
	validPrices, err := s.ValidateAndFilterPricesForSubscription(ctx, plan.ID, types.PRICE_ENTITY_TYPE_PLAN, sub, req.Workflow)
	if err != nil {
		return nil, err
	}

	// Create price map for line item creation
	priceMap := make(map[string]*dto.PriceResponse, len(validPrices))
	for _, p := range validPrices {
		priceMap[p.Price.ID] = p
	}

	// Create phases array if phases are provided
	var phases []*subscription.SubscriptionPhase
	var firstPhaseID string
	if len(req.Phases) > 0 {
		phases = make([]*subscription.SubscriptionPhase, len(req.Phases))
		for i, phaseReq := range req.Phases {
			phase := phaseReq.ToSubscriptionPhase(ctx, sub.ID)
			phases[i] = phase
			if i == 0 {
				firstPhaseID = phase.ID
			}
		}
	}

	// Setup subscription dates
	if sub.StartDate.IsZero() {
		sub.StartDate = time.Now().UTC()
	} else {
		sub.StartDate = sub.StartDate.UTC()
	}
	if req.BillingAnchor != nil {
		sub.BillingAnchor = *req.BillingAnchor
	} else if sub.BillingCycle == types.BillingCycleCalendar {
		sub.BillingAnchor = types.CalculateCalendarBillingAnchor(sub.StartDate, sub.BillingPeriod)
	} else {
		sub.BillingAnchor = sub.StartDate
	}
	if sub.BillingPeriodCount == 0 {
		sub.BillingPeriodCount = 1
	}
	nextBillingDate, err := types.NextBillingDate(sub.StartDate, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod, sub.EndDate)
	if err != nil {
		return nil, err
	}
	sub.CurrentPeriodStart = sub.StartDate
	sub.CurrentPeriodEnd = nextBillingDate

	// Create line items using DTO method
	subscriptionResponse := &dto.SubscriptionResponse{Subscription: sub}
	planResponse := &dto.PlanResponse{Plan: plan}
	lineItems := make([]*subscription.SubscriptionLineItem, 0, len(validPrices))

	for _, priceResponse := range validPrices {
		lineItemReq := &dto.CreateSubscriptionLineItemRequest{PriceID: priceResponse.Price.ID}
		// Validate with price for MinQuantity checks
		if err := lineItemReq.Validate(priceResponse.Price); err != nil {
			return nil, err
		}
		item := lineItemReq.ToSubscriptionLineItem(ctx, dto.LineItemParams{
			Subscription: subscriptionResponse,
			Price:        priceResponse,
			Plan:         planResponse,
			EntityType:   types.SubscriptionLineItemEntityTypePlan,
		})

		// Convert line items
		for _, item := range lineItems {
			price, ok := priceMap[item.PriceID]
			if !ok {
				return nil, ierr.NewError("failed to get price %s: price not found").
					WithHint("Ensure all prices are valid and available").
					WithReportableDetails(map[string]interface{}{
						"price_id": item.PriceID,
					}).
					Mark(ierr.ErrDatabase)
			}

			if price.Price.Type == types.PRICE_TYPE_USAGE && price.Meter != nil {
				item.MeterID = price.Meter.ID
				item.MeterDisplayName = price.Meter.Name
				item.DisplayName = price.Meter.Name
				item.Quantity = decimal.Zero
			} else {
				item.DisplayName = plan.Name
				if item.Quantity.IsZero() {
					item.Quantity = decimal.NewFromInt(1)
				}
			}

			if item.ID == "" {
				item.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)
			}

			item.SubscriptionID = sub.ID
			item.PriceType = price.Type
			item.EntityID = plan.ID
			item.EntityType = types.SubscriptionLineItemEntityTypePlan
			item.PlanDisplayName = plan.Name
			item.CustomerID = sub.CustomerID
			item.Currency = sub.Currency
			item.BillingPeriod = sub.BillingPeriod
			item.InvoiceCadence = price.InvoiceCadence
			item.TrialPeriod = price.TrialPeriod
			// Set phase ID if phases exist
		}
		if firstPhaseID != "" {
			item.SubscriptionPhaseID = &firstPhaseID
		}
		if len(req.Phases) > 0 && req.Phases[0].EndDate != nil {
			item.EndDate = *req.Phases[0].EndDate
		} else if sub.EndDate != nil {
			item.EndDate = *sub.EndDate
		}
		// Determine start date: max of first phase start, subscription start, and price start
		startDate := sub.StartDate
		if len(req.Phases) > 0 && req.Phases[0].StartDate.After(startDate) {
			startDate = req.Phases[0].StartDate
		}

		// Apply commitment configuration if provided for this price
		if req.LineItemCommitments != nil {
			if commitmentConfig, exists := req.LineItemCommitments[item.PriceID]; exists && commitmentConfig != nil {
				if commitmentConfig.CommitmentAmount != nil {
					item.CommitmentAmount = commitmentConfig.CommitmentAmount
				}
				if commitmentConfig.CommitmentQuantity != nil {
					item.CommitmentQuantity = commitmentConfig.CommitmentQuantity
				}
				if commitmentConfig.CommitmentType != "" {
					item.CommitmentType = commitmentConfig.CommitmentType
				}
				if commitmentConfig.OverageFactor != nil {
					item.CommitmentOverageFactor = commitmentConfig.OverageFactor
				}
				if commitmentConfig.EnableTrueUp != nil {
					item.CommitmentTrueUpEnabled = *commitmentConfig.EnableTrueUp
				}
				if commitmentConfig.IsWindowCommitment != nil {
					item.CommitmentWindowed = *commitmentConfig.IsWindowCommitment
				}
			}
		}

		if priceResponse.Price.StartDate != nil && priceResponse.Price.StartDate.After(startDate) {
			startDate = lo.FromPtr(priceResponse.Price.StartDate)
		}
		item.StartDate = startDate
		lineItems = append(lineItems, item)
	}

	// Build price to line item mapping for overrides
	originalPriceToLineItemMap := make(map[string]string)
	for _, item := range lineItems {
		if item.PriceID != "" && item.ID != "" {
			originalPriceToLineItemMap[item.PriceID] = item.ID
		}
	}

	// Process price overrides
	if len(req.OverrideLineItems) > 0 {
		if err = s.ProcessSubscriptionPriceOverrides(ctx, sub, req.OverrideLineItems, lineItems, priceMap); err != nil {
			return nil, err
		}
	}

	sub.LineItems = lineItems
	sub.EnableTrueUp = req.EnableTrueUp
	if req.SubscriptionStatus != "" {
		sub.SubscriptionStatus = req.SubscriptionStatus
	}

	s.Logger.Infow("creating subscription",
		"customer_id", sub.CustomerID, "plan_id", sub.PlanID, "start_date", sub.StartDate,
		"billing_anchor", sub.BillingAnchor, "current_period_start", sub.CurrentPeriodStart,
		"current_period_end", sub.CurrentPeriodEnd, "valid_prices", len(validPrices),
		"num_line_items", len(sub.LineItems))

	// Process subscription creation in transaction
	var invoice *dto.InvoiceResponse
	var updatedSub *subscription.Subscription
	invoiceService := NewInvoiceService(s.ServiceParams)

	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		if err = s.SubRepo.CreateWithLineItems(ctx, sub, sub.LineItems); err != nil {
			return err
		}
		if len(req.Addons) > 0 {
			if err = s.handleSubscriptionAddons(ctx, sub, req.Addons); err != nil {
				return err
			}
		}
		if len(req.OverrideEntitlements) > 0 {
			if err = s.ProcessSubscriptionEntitlementOverrides(ctx, sub, req.OverrideEntitlements); err != nil {
				return err
			}
		}

		// Prepare credit grants
		var creditGrantRequests []dto.CreateCreditGrantRequest
		if req.CreditGrants != nil {
			creditGrantRequests = req.CreditGrants
		} else {
			creditGrantService := NewCreditGrantService(s.ServiceParams)
			planCreditGrants, err := creditGrantService.GetCreditGrantsByPlan(ctx, plan.ID)
			if err != nil {
				return err
			}
			if len(planCreditGrants.Items) > 0 {
				s.Logger.Infow("plan has credit grants", "plan_id", plan.ID, "credit_grants_count", len(planCreditGrants.Items))
				creditGrantRequests = make([]dto.CreateCreditGrantRequest, 0, len(planCreditGrants.Items))
				for _, cg := range planCreditGrants.Items {
					creditGrantRequests = append(creditGrantRequests, dto.CreateCreditGrantRequest{
						Name:                   cg.Name,
						Scope:                  types.CreditGrantScopeSubscription,
						Credits:                cg.Credits,
						Cadence:                cg.Cadence,
						ExpirationType:         cg.ExpirationType,
						Priority:               cg.Priority,
						SubscriptionID:         lo.ToPtr(sub.ID),
						Period:                 cg.Period,
						PlanID:                 &plan.ID,
						ExpirationDuration:     cg.ExpirationDuration,
						ExpirationDurationUnit: cg.ExpirationDurationUnit,
						Metadata:               cg.Metadata,
						PeriodCount:            cg.PeriodCount,
						ConversionRate:         cg.ConversionRate,
						TopupConversionRate:    cg.TopupConversionRate,
					})
				}
			}
		}
		if err = s.handleCreditGrants(ctx, sub, creditGrantRequests); err != nil {
			return err
		}
		if err = s.handleTaxRateLinking(ctx, sub, req); err != nil {
			return err
		}
		if err = s.handleSubCoupons(ctx, sub, req, originalPriceToLineItemMap); err != nil {
			return err
		}

		// Handle entitlement proration for calendar billing
		if req.ProrationBehavior == types.ProrationBehaviorCreateProrations &&
			sub.BillingCycle == types.BillingCycleCalendar {
			if err = s.handleEntitlementProration(ctx, sub); err != nil {
				// Log error but don't fail subscription creation
				s.Logger.Errorw("failed to create prorated entitlements",
					"error", err,
					"subscription_id", sub.ID)
			}
		}

		// Create invoice for non-draft subscriptions
		if req.SubscriptionStatus != types.SubscriptionStatusDraft {
			paymentParams := dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID).NormalizePaymentParameters()
			invoice, updatedSub, err = invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
				SubscriptionID: sub.ID,
				PeriodStart:    sub.CurrentPeriodStart,
				PeriodEnd:      sub.CurrentPeriodEnd,
				ReferencePoint: types.ReferencePointPeriodStart,
			}, paymentParams, types.InvoiceFlowSubscriptionCreation, false)
			if err != nil {
				return err
			}
			if updatedSub != nil {
				sub = updatedSub
			}
			// Activate subscription if no invoice needed or already paid
			if (req.Workflow == nil || *req.Workflow != types.TemporalStripeIntegrationWorkflow) &&
				sub.SubscriptionStatus == types.SubscriptionStatusIncomplete &&
				(invoice == nil || invoice.PaymentStatus == types.PaymentStatusSucceeded) {
				sub.SubscriptionStatus = types.SubscriptionStatusActive
				if err = s.SubRepo.Update(ctx, sub); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Handle phases (post-transaction)
	if req.SubscriptionStatus != types.SubscriptionStatusDraft && len(phases) > 0 {
		if err = s.handleSubscriptionPhases(ctx, sub, phases, req.Phases, plan, validPrices); err != nil {
			return nil, err
		}
	}

	// Build response
	response := &dto.SubscriptionResponse{Subscription: sub}
	if invoice != nil {
		response.LatestInvoice = invoice
	}

	// Sync to HubSpot and publish webhooks
	isDraft := req.SubscriptionStatus == types.SubscriptionStatusDraft
	if isDraft {
		s.triggerHubSpotQuoteSyncWorkflow(ctx, sub.ID, customer.ID)
		s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionDraftCreated, sub.ID)
	} else {
		s.triggerHubSpotDealSyncWorkflow(ctx, sub.ID, customer.ID)
		s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionCreated, sub.ID)
	}
	return response, nil
}

func (s *subscriptionService) ActivateDraftSubscription(ctx context.Context, subID string, req dto.ActivateDraftSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get subscription with line items
	sub, lineItems, err := s.SubRepo.GetWithLineItems(ctx, subID)
	if err != nil {
		return nil, err
	}
	sub.LineItems = lineItems

	// Validate subscription is in draft status
	if sub.SubscriptionStatus != types.SubscriptionStatusDraft {
		return nil, ierr.NewError("subscription is not in draft status").
			WithHint("Only draft subscriptions can be activated").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subID,
				"current_status":  sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Recalculate all dates with new start date
	newStartDate := req.StartDate.UTC()
	sub.StartDate = newStartDate

	// Calculate billing anchor
	if sub.BillingCycle == types.BillingCycleCalendar {
		sub.BillingAnchor = types.CalculateCalendarBillingAnchor(sub.StartDate, sub.BillingPeriod)
	} else {
		// default to start date for anniversary billing
		sub.BillingAnchor = sub.StartDate
	}

	// Calculate the first billing period end date
	nextBillingDate, err := types.NextBillingDate(sub.StartDate, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod, sub.EndDate)
	if err != nil {
		return nil, err
	}

	sub.CurrentPeriodStart = sub.StartDate
	sub.CurrentPeriodEnd = nextBillingDate

	// Update line item start dates and end dates
	for _, item := range sub.LineItems {
		// Get price to check if it has a start date
		price, err := s.PriceRepo.Get(ctx, item.PriceID)
		if err != nil {
			return nil, err
		}
		// Set start date to the price start date if it is after the subscription start date
		if price.StartDate != nil && price.StartDate.After(sub.StartDate) {
			item.StartDate = lo.FromPtr(price.StartDate)
		} else {
			item.StartDate = sub.StartDate
		}
	}

	invoiceService := NewInvoiceService(s.ServiceParams)
	var invoice *dto.InvoiceResponse
	var updatedSub *subscription.Subscription

	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Update subscription with new dates
		err = s.SubRepo.Update(ctx, sub)
		if err != nil {
			return err
		}

		// Update line items with new start dates
		for _, item := range sub.LineItems {
			err = s.SubscriptionLineItemRepo.Update(ctx, item)
			if err != nil {
				return err
			}
		}

		// Create invoice for the subscription
		paymentParams := dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID)
		// Apply backward compatibility normalization
		paymentParams = paymentParams.NormalizePaymentParameters()
		invoice, updatedSub, err = invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
			SubscriptionID: sub.ID,
			PeriodStart:    sub.CurrentPeriodStart,
			PeriodEnd:      sub.CurrentPeriodEnd,
			ReferencePoint: types.ReferencePointPeriodStart,
		}, paymentParams, types.InvoiceFlowSubscriptionCreation, true) // Pass true for draft activation
		if err != nil {
			return err
		}

		// Use the updated subscription from CreateSubscriptionInvoice to avoid extra DB call
		if updatedSub != nil {
			sub = updatedSub
		}

		// If subscription is still in draft/incomplete status after invoice creation, set appropriate status
		// This applies when:
		// 1. No invoice was created (zero amount) - subscription should be active
		// 2. Invoice payment succeeded - subscription should be active
		// 3. Invoice exists and collection method is charge_automatically - set status based on payment_behavior
		//    - allow_incomplete: set to incomplete when payment is pending
		//    - default_active: set to active when payment is pending
		//    - error_if_incomplete: should not happen in activation, but handle gracefully
		//    - default_incomplete: set to incomplete when payment is pending
		targetStatus := sub.SubscriptionStatus

		if sub.SubscriptionStatus == types.SubscriptionStatusIncomplete || sub.SubscriptionStatus == types.SubscriptionStatusDraft {
			if invoice == nil || invoice.PaymentStatus == types.PaymentStatusSucceeded {
				// No invoice created or payment succeeded - activate subscription
				targetStatus = types.SubscriptionStatusActive
			} else {
				// Set status based on payment_behavior
				paymentBehavior := types.PaymentBehavior(sub.PaymentBehavior)

				switch paymentBehavior {
				case types.PaymentBehaviorAllowIncomplete:
					// Payment pending with allow_incomplete -> set to incomplete
					targetStatus = types.SubscriptionStatusIncomplete
				case types.PaymentBehaviorDefaultIncomplete:
					// Payment pending with default_incomplete -> set to incomplete
					targetStatus = types.SubscriptionStatusIncomplete
				case types.PaymentBehaviorDefaultActive:
					// Payment pending with default_active -> set to active
					targetStatus = types.SubscriptionStatusActive
				case types.PaymentBehaviorErrorIfIncomplete:
					// This shouldn't happen in activation flow, but set to incomplete as fallback
					// The payment processor would have thrown an error during creation
					targetStatus = types.SubscriptionStatusIncomplete
				default:
					// Default to active for backward compatibility
					targetStatus = types.SubscriptionStatusActive
				}
			}
		}

		if sub.SubscriptionStatus != targetStatus {
			sub.SubscriptionStatus = targetStatus
			err = s.SubRepo.Update(ctx, sub)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Create response
	response := &dto.SubscriptionResponse{Subscription: sub}

	// Include latest invoice if created
	if invoice != nil {
		response.LatestInvoice = invoice
	}

	// Publish activation webhook
	s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionActivated, sub.ID)

	return response, nil
}

// triggerHubSpotDealSyncWorkflow triggers the Temporal workflow to sync subscription to HubSpot deal
func (s *subscriptionService) triggerHubSpotDealSyncWorkflow(ctx context.Context, subscriptionID, customerID string) {
	// Copy necessary context values
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	s.Logger.Infow("triggering HubSpot deal sync workflow",
		"subscription_id", subscriptionID,
		"customer_id", customerID,
		"tenant_id", tenantID,
		"environment_id", envID)

	// Check if HubSpot connection exists and deal outbound sync is enabled
	if s.ConnectionRepo == nil {
		s.Logger.Debugw("ConnectionRepo not available, skipping HubSpot deal sync",
			"subscription_id", subscriptionID,
			"customer_id", customerID)
		return
	}

	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderHubSpot)
	if err != nil || conn == nil {
		s.Logger.Debugw("HubSpot connection not found, skipping deal sync",
			"error", err,
			"subscription_id", subscriptionID,
			"customer_id", customerID)
		return
	}

	if !conn.IsDealOutboundEnabled() {
		s.Logger.Debugw("HubSpot deal outbound sync disabled, skipping deal sync",
			"subscription_id", subscriptionID,
			"customer_id", customerID,
			"connection_id", conn.ID)
		return
	}

	// Fetch customer to check for HubSpot deal ID
	cust, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		s.Logger.Errorw("failed to fetch customer for HubSpot deal sync",
			"error", err,
			"customer_id", customerID,
			"subscription_id", subscriptionID)
		return
	}

	// Check if customer has HubSpot deal ID in metadata
	dealID, ok := cust.Metadata["hubspot_deal_id"]
	if !ok || dealID == "" {
		s.Logger.Debugw("customer does not have HubSpot deal ID, skipping sync",
			"customer_id", customerID,
			"subscription_id", subscriptionID)
		return // Not an error - customer might not be from HubSpot
	}

	// Prepare workflow input with all necessary IDs
	input := &models.HubSpotDealSyncWorkflowInput{
		SubscriptionID: subscriptionID,
		CustomerID:     customerID,
		DealID:         dealID,
		TenantID:       tenantID,
		EnvironmentID:  envID,
	}

	// Validate input
	if err := input.Validate(); err != nil {
		s.Logger.Errorw("invalid workflow input for HubSpot deal sync",
			"error", err,
			"subscription_id", subscriptionID,
			"customer_id", customerID,
			"deal_id", dealID)
		return
	}

	// Get global temporal service
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		s.Logger.Warnw("temporal service not available for HubSpot deal sync",
			"subscription_id", subscriptionID)
		return
	}

	// Start workflow - Temporal handles async execution, no need for goroutines
	workflowRun, err := temporalSvc.ExecuteWorkflow(
		ctx,
		types.TemporalHubSpotDealSyncWorkflow,
		input,
	)
	if err != nil {
		s.Logger.Errorw("failed to start HubSpot deal sync workflow",
			"error", err,
			"subscription_id", subscriptionID,
			"customer_id", customerID,
			"deal_id", dealID)
		return
	}

	s.Logger.Infow("HubSpot deal sync workflow started successfully",
		"subscription_id", subscriptionID,
		"workflow_id", workflowRun.GetID())
}

// triggerHubSpotQuoteSyncWorkflow triggers the Temporal workflow to sync subscription to HubSpot quote
func (s *subscriptionService) triggerHubSpotQuoteSyncWorkflow(ctx context.Context, subscriptionID, customerID string) {
	// Copy necessary context values
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	s.Logger.Infow("triggering HubSpot quote sync workflow",
		"subscription_id", subscriptionID,
		"customer_id", customerID,
		"tenant_id", tenantID,
		"environment_id", envID)

	// Check if HubSpot connection exists and quote outbound sync is enabled
	if s.ConnectionRepo == nil {
		s.Logger.Debugw("ConnectionRepo not available, skipping HubSpot quote sync",
			"subscription_id", subscriptionID,
			"customer_id", customerID)
		return
	}

	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderHubSpot)
	if err != nil || conn == nil {
		s.Logger.Debugw("HubSpot connection not found, skipping quote sync",
			"error", err,
			"subscription_id", subscriptionID,
			"customer_id", customerID)
		return
	}

	if !conn.IsQuoteOutboundEnabled() {
		s.Logger.Debugw("HubSpot quote outbound sync disabled, skipping quote sync",
			"subscription_id", subscriptionID,
			"customer_id", customerID,
			"connection_id", conn.ID)
		return
	}

	// Fetch customer to check for HubSpot deal ID
	cust, err := s.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		s.Logger.Errorw("failed to fetch customer for HubSpot quote sync",
			"error", err,
			"customer_id", customerID,
			"subscription_id", subscriptionID)
		return
	}

	// Check if customer has HubSpot deal ID in metadata
	dealID, ok := cust.Metadata["hubspot_deal_id"]
	if !ok || dealID == "" {
		s.Logger.Debugw("customer does not have HubSpot deal ID, skipping quote sync",
			"customer_id", customerID,
			"subscription_id", subscriptionID)
		return // Not an error - customer might not be from HubSpot
	}

	// Prepare workflow input with all necessary IDs
	input := &models.HubSpotQuoteSyncWorkflowInput{
		SubscriptionID: subscriptionID,
		CustomerID:     customerID,
		DealID:         dealID,
		TenantID:       tenantID,
		EnvironmentID:  envID,
	}

	// Validate input
	if err := input.Validate(); err != nil {
		s.Logger.Errorw("invalid workflow input for HubSpot quote sync",
			"error", err,
			"subscription_id", subscriptionID,
			"customer_id", customerID,
			"deal_id", dealID)
		return
	}

	// Get global temporal service
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		s.Logger.Warnw("temporal service not available for HubSpot quote sync",
			"subscription_id", subscriptionID)
		return
	}

	// Start workflow - Temporal handles async execution, no need for goroutines
	workflowRun, err := temporalSvc.ExecuteWorkflow(
		ctx,
		types.TemporalHubSpotQuoteSyncWorkflow,
		input,
	)
	if err != nil {
		s.Logger.Errorw("failed to start HubSpot quote sync workflow",
			"error", err,
			"subscription_id", subscriptionID,
			"customer_id", customerID,
			"deal_id", dealID)
		return
	}

	s.Logger.Infow("HubSpot quote sync workflow started successfully",
		"subscription_id", subscriptionID,
		"customer_id", customerID,
		"deal_id", dealID,
		"workflow_id", workflowRun.GetID(),
		"run_id", workflowRun.GetRunID())
}

func (s *subscriptionService) handleTaxRateLinking(ctx context.Context, sub *subscription.Subscription, req dto.CreateSubscriptionRequest) error {
	taxService := NewTaxService(s.ServiceParams)

	// if tax overrides are provided, link them to the subscription
	if len(req.TaxRateOverrides) > 0 {
		err := taxService.LinkTaxRatesToEntity(ctx, dto.LinkTaxRateToEntityRequest{
			EntityType:       types.TaxRateEntityTypeSubscription,
			EntityID:         sub.ID,
			TaxRateOverrides: req.TaxRateOverrides,
		})
		if err != nil {
			return err
		}
	}

	// If no tax rate overrides are provided, link the customer's tax association to the subscription
	if req.TaxRateOverrides == nil {
		filter := types.NewNoLimitTaxAssociationFilter()
		filter.EntityType = types.TaxRateEntityTypeCustomer
		filter.EntityID = sub.CustomerID
		filter.AutoApply = lo.ToPtr(true)
		tenantTaxAssociations, err := taxService.ListTaxAssociations(ctx, filter)
		if err != nil {
			return err
		}

		err = taxService.LinkTaxRatesToEntity(ctx, dto.LinkTaxRateToEntityRequest{
			EntityType:              types.TaxRateEntityTypeSubscription,
			EntityID:                sub.ID,
			ExistingTaxAssociations: tenantTaxAssociations.Items,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// handleSubscriptionPhases processes subscription phases and creates phase-specific line items
func (s *subscriptionService) handleSubscriptionPhases(
	ctx context.Context,
	sub *subscription.Subscription,
	phases []*subscription.SubscriptionPhase,
	phaseRequests []dto.SubscriptionPhaseCreateRequest,
	plan *plan.Plan,
	validPrices []*dto.PriceResponse,
) error {
	if len(phases) == 0 {
		return nil
	}

	// Create a map from price ID to price for quick lookup
	priceMap := lo.SliceToMap(validPrices, func(p *dto.PriceResponse) (string, *dto.PriceResponse) {
		return p.Price.ID, p
	})

	// Create plan response wrapper (reusing DTO conversion pattern from AddSubscriptionLineItem)
	planResponse := &dto.PlanResponse{Plan: plan}

	// Process each phase
	for i, phase := range phases {
		// Create the phase in database
		if err := s.SubscriptionPhaseRepo.Create(ctx, phase); err != nil {
			return err
		}

		// Skip creating line items for the first phase since they're already created with the subscription
		if i == 0 {
			continue
		}

		// Get corresponding phase request for additional data
		phaseReq := phaseRequests[i]

		// Validate all line item requests before creating them
		for _, priceResp := range validPrices {
			startDate := phaseReq.StartDate
			if priceResp.Price.StartDate != nil && priceResp.Price.StartDate.After(phaseReq.StartDate) {
				startDate = lo.FromPtr(priceResp.Price.StartDate)
			}
			req := dto.CreateSubscriptionLineItemRequest{
				PriceID:             priceResp.Price.ID,
				SubscriptionPhaseID: lo.ToPtr(phase.ID),
				StartDate:           lo.ToPtr(startDate),
				EndDate:             phaseReq.EndDate,
			}
			if err := req.Validate(priceResp.Price); err != nil {
				return err
			}
		}

		// Create line items from plan prices - reusing DTO's ToSubscriptionLineItem logic (same as AddSubscriptionLineItem)
		phaseLineItems := lo.Map(validPrices, func(priceResp *dto.PriceResponse, _ int) *subscription.SubscriptionLineItem {
			// Build line item params
			params := dto.LineItemParams{
				Subscription: &dto.SubscriptionResponse{Subscription: sub},
				Price:        priceResp,
				Plan:         planResponse,
				EntityType:   types.SubscriptionLineItemEntityTypePlan,
			}

			// Create request with phase dates
			startDate := phaseReq.StartDate
			if priceResp.Price.StartDate != nil && priceResp.Price.StartDate.After(phaseReq.StartDate) {
				startDate = lo.FromPtr(priceResp.Price.StartDate)
			}

			req := dto.CreateSubscriptionLineItemRequest{
				PriceID:             priceResp.Price.ID,
				SubscriptionPhaseID: lo.ToPtr(phase.ID),
				StartDate:           lo.ToPtr(startDate),
				EndDate:             phaseReq.EndDate,
			}

			lineItem := req.ToSubscriptionLineItem(ctx, params)
			return lineItem
		})

		// Create original price to line item mapping before processing overrides
		// This captures the mapping before any price overrides change the PriceID
		phasePriceToLineItemMap := make(map[string]string)
		for _, item := range phaseLineItems {
			if item.PriceID != "" && item.ID != "" {
				phasePriceToLineItemMap[item.PriceID] = item.ID
			}
		}

		// Process phase-specific price overrides if present
		if len(phaseReq.OverrideLineItems) > 0 {
			if err := s.ProcessSubscriptionPriceOverrides(ctx, sub, phaseReq.OverrideLineItems, phaseLineItems, priceMap); err != nil {
				return err
			}
		}

		// Create phase line items in database
		for _, lineItem := range phaseLineItems {
			if err := s.SubscriptionLineItemRepo.Create(ctx, lineItem); err != nil {
				return err
			}
		}

		// Handle phase coupons - transform simple coupons to SubscriptionCouponRequest format
		couponAssociationService := NewCouponAssociationService(s.ServiceParams)
		phaseCoupons := s.normalizePhaseCoupons(phaseReq, phase.ID, phasePriceToLineItemMap)
		if len(phaseCoupons) > 0 {
			err := couponAssociationService.ApplyCouponsToSubscription(ctx, sub, phaseCoupons)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// normalizePhaseCoupons converts simple Coupons and LineItemCoupons from phase request to SubscriptionCouponRequest format
// Sets start/end dates from the phase dates
func (s *subscriptionService) normalizePhaseCoupons(
	phaseReq dto.SubscriptionPhaseCreateRequest,
	phaseID string,
	phasePriceToLineItemMap map[string]string,
) []dto.SubscriptionCouponRequest {
	var subscriptionCoupons []dto.SubscriptionCouponRequest

	// Convert subscription-level coupons
	for _, couponID := range phaseReq.Coupons {
		if couponID != "" {
			subscriptionCoupons = append(subscriptionCoupons, dto.SubscriptionCouponRequest{
				CouponID:            couponID,
				SubscriptionPhaseID: lo.ToPtr(phaseID),
				StartDate:           phaseReq.StartDate,
				EndDate:             phaseReq.EndDate,
			})
		}
	}

	// Convert line item coupons - use phasePriceToLineItemMap to convert priceID to lineItemID
	for priceID, couponIDs := range phaseReq.LineItemCoupons {
		for _, couponID := range couponIDs {
			if couponID != "" {
				// Get lineItemID from the phase price mapping
				if lineItemID, exists := phasePriceToLineItemMap[priceID]; exists {
					subscriptionCoupons = append(subscriptionCoupons, dto.SubscriptionCouponRequest{
						CouponID:            couponID,
						LineItemID:          lo.ToPtr(lineItemID),
						SubscriptionPhaseID: lo.ToPtr(phaseID),
						StartDate:           phaseReq.StartDate,
						EndDate:             phaseReq.EndDate,
					})
				} else {
					// Log warning but continue processing other coupons
					s.Logger.Warnw("phase coupon priceID not found in phase line items, skipping",
						"price_id", priceID,
						"coupon_id", couponID,
						"phase_id", phaseID)
				}
			}
		}
	}

	return subscriptionCoupons
}

// processSubscriptionPriceOverrides handles creating subscription-scoped prices for overrides
func (s *subscriptionService) ProcessSubscriptionPriceOverrides(
	ctx context.Context,
	sub *subscription.Subscription,
	overrideRequests []dto.OverrideLineItemRequest,
	lineItems []*subscription.SubscriptionLineItem,
	priceMap map[string]*dto.PriceResponse,
) error {
	if len(overrideRequests) == 0 {
		return nil
	}

	s.Logger.Infow("processing price overrides for subscription",
		"subscription_id", sub.ID,
		"override_count", len(overrideRequests))

	// Create a map from price ID to line item for quick lookup
	lineItemsByPriceID := make(map[string]*subscription.SubscriptionLineItem)
	for _, item := range lineItems {
		lineItemsByPriceID[item.PriceID] = item
	}

	// Create price service instance
	priceService := NewPriceService(s.ServiceParams)

	// Process each override request
	for _, override := range overrideRequests {
		// Validate the override request with context
		if err := override.Validate(priceMap, lineItemsByPriceID, sub.PlanID); err != nil {
			return err
		}

		// Get the original price and line item
		originalPrice := priceMap[override.PriceID]
		lineItem := lineItemsByPriceID[override.PriceID]

		// Determine target billing model (use override if provided, otherwise original)
		targetBillingModel := originalPrice.BillingModel
		if override.BillingModel != "" {
			targetBillingModel = override.BillingModel
		}

		// Create subscription-scoped price using price service
		// Always preserve the original price's display name and price unit type
		createPriceReq := dto.CreatePriceRequest{
			Currency:             originalPrice.Currency,
			EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
			EntityID:             sub.ID,
			Type:                 originalPrice.Type,
			BillingPeriod:        originalPrice.BillingPeriod,
			BillingPeriodCount:   originalPrice.BillingPeriodCount,
			BillingModel:         targetBillingModel,
			BillingCadence:       originalPrice.BillingCadence,
			InvoiceCadence:       originalPrice.InvoiceCadence,
			TrialPeriod:          originalPrice.TrialPeriod,
			TierMode:             originalPrice.TierMode,
			MeterID:              originalPrice.MeterID,
			Description:          originalPrice.Description,
			Metadata:             originalPrice.Metadata,
			ParentPriceID:        originalPrice.GetRootPriceID(), // Always point to the root price ID
			DisplayName:          originalPrice.DisplayName,      // Preserve original price display name
			PriceUnitType:        originalPrice.PriceUnitType,    // Always copy from original (cannot be changed)
			SkipEntityValidation: true,
		}

		// Handle PriceUnitConfig construction for CUSTOM price unit type
		var priceUnitConfig *dto.PriceUnitConfig
		if originalPrice.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
			priceUnitConfig = &dto.PriceUnitConfig{
				PriceUnit: lo.FromPtr(originalPrice.PriceUnit), // Always use original price unit (cannot be changed)
			}
		}

		// Handle billing model-specific fields based on target billing model and price unit type
		switch targetBillingModel {
		case types.BILLING_MODEL_FLAT_FEE, types.BILLING_MODEL_PACKAGE:
			// Handle amount based on price unit type
			if originalPrice.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
				// For CUSTOM price unit, amount is handled via PriceUnitConfig
				if override.PriceUnitAmount != nil {
					priceUnitConfig.Amount = override.PriceUnitAmount
				} else if originalPrice.PriceUnitAmount != nil {
					priceUnitConfig.Amount = originalPrice.PriceUnitAmount
				}
				createPriceReq.PriceUnitConfig = priceUnitConfig
			} else {
				// For FIAT price unit, use Amount
				if override.Amount != nil {
					createPriceReq.Amount = override.Amount
				} else {
					createPriceReq.Amount = lo.ToPtr(originalPrice.Amount)
				}
			}

			// Handle TransformQuantity for PACKAGE (applies to both FIAT and CUSTOM)
			if targetBillingModel == types.BILLING_MODEL_PACKAGE {
				if override.TransformQuantity != nil {
					createPriceReq.TransformQuantity = override.TransformQuantity
				} else if originalPrice.TransformQuantity != (price.JSONBTransformQuantity{}) {
					transformQuantity := price.TransformQuantity(originalPrice.TransformQuantity)
					createPriceReq.TransformQuantity = &transformQuantity
				}
			}

		case types.BILLING_MODEL_TIERED:
			// Handle tiers based on price unit type
			if originalPrice.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
				// For CUSTOM price unit, tiers are handled via PriceUnitConfig
				if len(override.PriceUnitTiers) > 0 {
					priceUnitConfig.PriceUnitTiers = override.PriceUnitTiers
				} else if len(originalPrice.PriceUnitTiers) > 0 {
					priceUnitConfig.PriceUnitTiers = make([]dto.CreatePriceTier, len(originalPrice.PriceUnitTiers))
					for i, tier := range originalPrice.PriceUnitTiers {
						priceUnitConfig.PriceUnitTiers[i] = dto.CreatePriceTier{
							UpTo:       tier.UpTo,
							UnitAmount: tier.UnitAmount,
						}
						priceUnitConfig.PriceUnitTiers[i].FlatAmount = tier.FlatAmount
					}
				}
				createPriceReq.PriceUnitConfig = priceUnitConfig
			} else {
				// For FIAT price unit, use Tiers
				if len(override.Tiers) > 0 {
					createPriceReq.Tiers = override.Tiers
				} else if len(originalPrice.Tiers) > 0 {
					createPriceReq.Tiers = make([]dto.CreatePriceTier, len(originalPrice.Tiers))
					for i, tier := range originalPrice.Tiers {
						createPriceReq.Tiers[i] = dto.CreatePriceTier{
							UpTo:       tier.UpTo,
							UnitAmount: tier.UnitAmount,
						}
						createPriceReq.Tiers[i].FlatAmount = tier.FlatAmount
					}
				}
			}

			// Handle TierMode for both types
			if override.TierMode != "" {
				createPriceReq.TierMode = override.TierMode
			} else {
				createPriceReq.TierMode = originalPrice.TierMode
			}
		}

		// Create the subscription-scoped price using price service
		overriddenPriceResp, err := priceService.CreatePrice(ctx, createPriceReq)
		if err != nil {
			return err
		}

		// Update line item quantity if specified
		if override.Quantity != nil {
			lineItem.Quantity = *override.Quantity
		}

		// Update the line item to reference the new subscription-scoped price
		// Also update display name to match the new price (which preserves the original display name)
		lineItem.PriceID = overriddenPriceResp.ID
		if overriddenPriceResp.DisplayName != "" {
			lineItem.DisplayName = overriddenPriceResp.DisplayName
		}

		s.Logger.Infow("created subscription-scoped price override",
			"subscription_id", sub.ID,
			"original_price_id", override.PriceID,
			"override_price_id", overriddenPriceResp.ID,
			"amount_override", override.Amount != nil,
			"quantity_override", override.Quantity != nil,
			"billing_model_override", override.BillingModel != "",
			"tier_mode_override", override.TierMode != "",
			"tiers_override", len(override.Tiers) > 0,
			"transform_quantity_override", override.TransformQuantity != nil,
			"price_unit_amount_override", override.PriceUnitAmount != nil,
			"price_unit_tiers_override", len(override.PriceUnitTiers) > 0)
	}

	return nil
}

// handleEntitlementProration calculates and creates prorated entitlements for calendar billing
func (s *subscriptionService) handleEntitlementProration(
	ctx context.Context,
	sub *subscription.Subscription,
) error {
	s.Logger.Infow("handling entitlement proration",
		"subscription_id", sub.ID,
		"plan_id", sub.PlanID,
		"billing_cycle", sub.BillingCycle,
		"start_date", sub.StartDate,
		"period_end", sub.CurrentPeriodEnd)

	// Get proration service
	prorationService := NewProrationService(s.ServiceParams)

	// Calculate entitlement proration
	prorationResult, err := prorationService.CalculateEntitlementProration(
		ctx,
		sub.PlanID,
		sub.CurrentPeriodStart,
		sub.CurrentPeriodEnd,
		sub.StartDate, // Proration date is the start date
		sub.CustomerTimezone,
		sub.BillingCycle,
		sub.BillingAnchor,
		sub.BillingPeriod,
		sub.BillingPeriodCount,
	)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to calculate entitlement proration").
			Mark(ierr.ErrSystem)
	}

	// Create prorated entitlements
	// For prorated subscriptions: start_date = current_period_start, end_date = current_period_end
	if err = prorationService.CreateProratedEntitlements(ctx, sub.ID, prorationResult, sub.CurrentPeriodStart, sub.CurrentPeriodEnd); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create prorated entitlements").
			Mark(ierr.ErrSystem)
	}

	s.Logger.Infow("entitlement proration completed",
		"subscription_id", sub.ID,
		"prorated_count", len(prorationResult.ProratedLimits),
		"coefficient", prorationResult.ProrationCoefficient.String())

	return nil
}

// handleCreditGrants handles creating and applying credit grants for a subscription
func (s *subscriptionService) handleCreditGrants(
	ctx context.Context,
	subscription *subscription.Subscription,
	creditGrantRequests []dto.CreateCreditGrantRequest,
) error {
	if len(creditGrantRequests) == 0 {
		return nil
	}

	creditGrantService := NewCreditGrantService(s.ServiceParams)

	s.Logger.Infow("processing credit grants for subscription",
		"subscription_id", subscription.ID,
		"credit_grants_count", len(creditGrantRequests))

	// Validate that all credit grants have the same conversion rates
	if len(creditGrantRequests) > 1 {
		conversionRate := creditGrantRequests[0].ConversionRate
		topupConversionRate := creditGrantRequests[0].TopupConversionRate

		validationError := ierr.NewError("all credit grants must have the same conversion_rate and topup_conversion_rate").
			WithHint("All credit grants must have the same conversion rates").
			Mark(ierr.ErrValidation)

		for i := 1; i < len(creditGrantRequests); i++ {
			grantReq := creditGrantRequests[i]

			// If first is nil, all must be nil. If first is not nil, all must match that value.
			if conversionRate == nil {
				if grantReq.ConversionRate != nil {
					return validationError
				}
			} else {
				if grantReq.ConversionRate == nil || !conversionRate.Equal(lo.FromPtr(grantReq.ConversionRate)) {
					return validationError
				}
			}

			if topupConversionRate == nil {
				if grantReq.TopupConversionRate != nil {
					return validationError
				}
			} else {
				if grantReq.TopupConversionRate == nil || !topupConversionRate.Equal(lo.FromPtr(grantReq.TopupConversionRate)) {
					return validationError
				}
			}
		}
	}

	// Create and apply credit grants
	startDate := subscription.StartDate
	if subscription.TrialEnd != nil {
		startDate = lo.FromPtr(subscription.TrialEnd)
	}

	for _, grantReq := range creditGrantRequests {
		// Ensure subscription ID is set and scope is SUBSCRIPTION
		grantReq.SubscriptionID = lo.ToPtr(subscription.ID)
		grantReq.Scope = types.CreditGrantScopeSubscription
		grantReq.StartDate = lo.ToPtr(startDate)
		grantReq.EndDate = subscription.EndDate

		// Use subscription start date as the anchor for the credit grant chain
		grantReq.CreditGrantAnchor = lo.ToPtr(startDate)

		// Create credit grant: this now triggers initializeCreditGrantWorkflow
		// which handles creation, anchor calculation, and eager application
		_, err := creditGrantService.CreateCreditGrant(ctx, grantReq)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *subscriptionService) GetSubscription(ctx context.Context, id string) (*dto.SubscriptionResponse, error) {
	// Get sub with line items
	sub, lineItems, err := s.SubRepo.GetWithLineItems(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &dto.SubscriptionResponse{
		Subscription: sub,
	}

	// if subscription pause status is not none, get all pauses
	if sub.PauseStatus != types.PauseStatusNone {
		pauses, err := s.SubRepo.ListPauses(ctx, id)
		if err != nil {
			return nil, err
		}
		response.Pauses = pauses
	}

	// expand plan
	planService := NewPlanService(s.ServiceParams)

	plan, err := planService.GetPlan(ctx, sub.PlanID)
	if err != nil {
		return nil, err
	}
	response.Plan = plan

	// expand customer
	customerService := NewCustomerService(s.ServiceParams)
	customer, err := customerService.GetCustomer(ctx, sub.CustomerID)
	if err != nil {
		return nil, err
	}
	response.Customer = customer

	// expand coupon associations
	couponAssociationService := NewCouponAssociationService(s.ServiceParams)
	couponFilter := types.NewCouponAssociationFilter()
	couponFilter.SubscriptionIDs = []string{id}
	couponAssociationsResponse, err := couponAssociationService.ListCouponAssociations(ctx, couponFilter)
	if err != nil {
		s.Logger.Errorw("failed to get coupon associations for subscription",
			"subscription_id", id,
			"error", err)
	} else {
		response.CouponAssociations = couponAssociationsResponse.Items
	}

	// expand subscription phases
	subscriptionPhaseService := NewSubscriptionPhaseService(s.ServiceParams)
	phaseFilter := types.NewNoLimitSubscriptionPhaseFilter()
	phaseFilter.SubscriptionIDs = []string{id}
	phasesResponse, err := subscriptionPhaseService.GetSubscriptionPhases(ctx, phaseFilter)
	if err != nil {
		s.Logger.Errorw("failed to get subscription phases for subscription",
			"subscription_id", id,
			"error", err)
	} else {
		response.Phases = phasesResponse.Items
	}

	// expand price for subscription line items
	priceIds := lo.Map(lineItems, func(item *subscription.SubscriptionLineItem, _ int) string {
		return item.PriceID
	})
	priceService := NewPriceService(s.ServiceParams)
	priceFilter := types.NewNoLimitPriceFilter().
		WithPriceIDs(priceIds).
		WithAllowExpiredPrices(true)
	prices, err := priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	priceMap := make(map[string]*price.Price)
	for _, price := range prices.Items {
		priceMap[price.ID] = price.Price
	}

	for _, lineItem := range sub.LineItems {
		lineItem.Price = priceMap[lineItem.PriceID]
	}

	// expand credit grants
	creditGrantService := NewCreditGrantService(s.ServiceParams)
	creditGrantsResponse, err := creditGrantService.GetCreditGrantsBySubscription(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get credit grants for subscription",
			"subscription_id", id,
			"error", err)
		return nil, err
	}
	response.CreditGrants = creditGrantsResponse.Items

	return response, nil
}

// UpdateSubscription updates a subscription with the provided request
func (s *subscriptionService) UpdateSubscription(ctx context.Context, subscriptionID string, req dto.UpdateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	logger := s.Logger.With(
		zap.String("subscription_id", subscriptionID),
	)

	logger.Info("updating subscription")

	// Get the current subscription
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription").
			Mark(ierr.ErrDatabase)
	}

	// Update fields from request
	if req.Status != "" {
		subscription.SubscriptionStatus = req.Status
	}

	if req.CancelAt != nil {
		subscription.CancelAt = req.CancelAt
		subscription.EndDate = req.CancelAt
	}

	subscription.CancelAtPeriodEnd = req.CancelAtPeriodEnd

	// Update the subscription in the database
	err = s.SubRepo.Update(ctx, subscription)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to update subscription").
			Mark(ierr.ErrDatabase)
	}

	logger.Info("successfully updated subscription")

	s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionUpdated, subscription.ID)

	// Return the updated subscription
	return s.GetSubscription(ctx, subscriptionID)
}

// CancelSubscription provides enhanced cancellation with proration support
func (s *subscriptionService) CancelSubscription(
	ctx context.Context,
	subscriptionID string,
	req *dto.CancelSubscriptionRequest,
) (*dto.CancelSubscriptionResponse, error) {
	logger := s.Logger.With(
		zap.String("subscription_id", subscriptionID),
		zap.String("cancellation_type", string(req.CancellationType)),
		zap.String("reason", req.Reason),
	)

	logger.Info("processing enhanced subscription cancellation")

	// Step 1: Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Step 2: Get subscription with line items
	subscription, lineItems, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription").
			Mark(ierr.ErrDatabase)
	}

	// Step 3: Validate subscription state
	// Reject cancellation of draft subscriptions
	if err := s.validateNotDraftSubscription(subscription, "cancellation"); err != nil {
		return nil, err
	}

	if subscription.SubscriptionStatus == types.SubscriptionStatusCancelled {
		return nil, ierr.NewError("subscription is already cancelled").
			WithHint("The subscription is already cancelled").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Step 4: Determine effective cancellation date
	effectiveDate, err := s.determineEffectiveDate(subscription, req.CancellationType)
	if err != nil {
		return nil, err
	}

	var prorationDetails []dto.ProrationDetail
	totalCreditAmount := decimal.Zero

	// Step 5: Execute in transaction
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {

		// Step 6: Calculate proration using unified function
		if req.ProrationBehavior == types.ProrationBehaviorCreateProrations {
			prorationService := NewProrationService(s.ServiceParams)
			prorationResult, err := prorationService.CalculateSubscriptionCancellationProration(
				ctx, subscription, lineItems, req.CancellationType, effectiveDate, req.Reason, req.ProrationBehavior)
			if err != nil {
				return err
			}

			// Convert proration result to response format
			prorationDetails, totalCreditAmount = s.convertProrationResultToDetails(prorationResult)
		}

		if req.CancellationType != types.CancellationTypeEndOfPeriod {
			invoiceService := NewInvoiceService(s.ServiceParams)
			paymentParams := dto.NewPaymentParametersFromSubscription(subscription.CollectionMethod, subscription.PaymentBehavior, subscription.GatewayPaymentMethodID)
			paymentParams = paymentParams.NormalizePaymentParameters()
			inv, _, err := invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
				SubscriptionID: subscription.ID,
				PeriodStart:    subscription.CurrentPeriodStart,
				PeriodEnd:      effectiveDate,
				ReferencePoint: types.ReferencePointCancel,
			}, paymentParams, types.InvoiceFlowCancel, false)
			if err != nil {
				return err
			}

			if inv != nil {
				s.Logger.Infow("created invoice for subscription",
					"subscription_id", subscription.ID,
					"invoice_id", inv.ID)
			}

		}
		// Step 7: Update subscription status
		err = s.updateSubscriptionForCancellation(ctx, subscription, req.CancellationType, effectiveDate, req.Reason)
		if err != nil {
			return err
		}

		// Step 8: Void future credit grants
		creditGrantService := NewCreditGrantService(s.ServiceParams)
		err = creditGrantService.CancelFutureSubscriptionGrants(ctx, subscription.ID)
		if err != nil {
			return err
		}

		// Step 9: Top up wallet for proration credit (only if there's a credit amount)
		if totalCreditAmount.GreaterThan(decimal.Zero) {
			walletService := NewWalletService(s.ServiceParams)
			err = walletService.TopUpWalletForProratedCharge(ctx, subscription.CustomerID, totalCreditAmount.Abs(), subscription.Currency)
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		logger.Errorw("failed to process cancellation", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to process subscription cancellation").
			Mark(ierr.ErrDatabase)
	}

	if !req.SuppressWebhook {
		// Step 10: Publish events
		s.publishCancellationEvents(ctx, subscription)
	}

	// Step 11: Build response
	response := &dto.CancelSubscriptionResponse{
		SubscriptionID:    subscription.ID,
		CancellationType:  req.CancellationType,
		EffectiveDate:     effectiveDate,
		Status:            subscription.SubscriptionStatus,
		Reason:            req.Reason,
		ProrationDetails:  prorationDetails,
		TotalCreditAmount: totalCreditAmount,
		ProcessedAt:       time.Now().UTC(),
	}

	// Note: Proration invoice is no longer created during cancellation

	// Generate user-friendly message
	response.Message = s.generateCancellationMessage(req.CancellationType, effectiveDate, totalCreditAmount)

	logger.Infow("subscription cancellation completed successfully",
		"effective_date", effectiveDate,
		"total_credit_amount", totalCreditAmount.String(),
		"proration_items", len(prorationDetails))

	return response, nil
}

func (s *subscriptionService) ListSubscriptions(ctx context.Context, filter *types.SubscriptionFilter) (*dto.ListSubscriptionsResponse, error) {
	s.Logger.Debugw("starting ListSubscriptions",
		"filter", filter,
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx))

	planService := NewPlanService(s.ServiceParams)

	if filter == nil {
		s.Logger.Debugw("filter is nil, creating new subscription filter")
		filter = types.NewSubscriptionFilter()
	}

	if filter.GetLimit() == 0 {
		s.Logger.Debugw("filter limit is 0, setting default limit", "default_limit", types.GetDefaultFilter().Limit)
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	if filter.QueryFilter == nil {
		s.Logger.Debugw("filter.QueryFilter is nil, creating default query filter")
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Validate expand fields
	if err := filter.GetExpand().Validate(types.SubscriptionExpandConfig); err != nil {
		return nil, err
	}

	// Resolve external customer ID to internal customer ID if provided
	if filter.ExternalCustomerID != "" {
		s.Logger.Debugw("resolving external customer ID",
			"external_customer_id", filter.ExternalCustomerID)

		customer, err := s.CustomerRepo.GetByLookupKey(ctx, filter.ExternalCustomerID)
		if err != nil {
			s.Logger.Errorw("failed to resolve external customer ID",
				"error", err,
				"external_customer_id", filter.ExternalCustomerID)
			return nil, ierr.WithError(err).
				WithHintf("Customer with external ID '%s' not found", filter.ExternalCustomerID).
				WithReportableDetails(map[string]interface{}{
					"external_customer_id": filter.ExternalCustomerID,
				}).
				Mark(ierr.ErrNotFound)
		}

		// Set the resolved customer ID and clear the external customer ID
		filter.CustomerID = customer.ID
		filter.ExternalCustomerID = "" // Clear to avoid confusion

		s.Logger.Debugw("resolved external customer ID to internal customer ID",
			"external_customer_id", filter.ExternalCustomerID,
			"customer_id", customer.ID)
	}

	s.Logger.Debugw("calling SubRepo.List",
		"final_filter", filter,
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset())

	subscriptions, err := s.SubRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to list subscriptions from repository", "error", err, "filter", filter)
		return nil, err
	}

	count, err := s.SubRepo.Count(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to count subscriptions from repository", "error", err, "filter", filter)
		return nil, err
	}

	response := &dto.ListSubscriptionsResponse{
		Items: make([]*dto.SubscriptionResponse, len(subscriptions)),
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	// Collect unique plan IDs
	planIDMap := make(map[string]*dto.PlanResponse, 0)
	for _, sub := range subscriptions {
		if sub.PlanID == "" {
			s.Logger.Warnw("subscription has empty plan_id", "subscription_id", sub.ID)
		}
		planIDMap[sub.PlanID] = nil
	}

	uniquePlanIDs := lo.Keys(planIDMap)
	s.Logger.Debugw("collected unique plan IDs",
		"unique_plan_count", len(uniquePlanIDs),
		"plan_ids", uniquePlanIDs)

	// Get plans in bulk
	planFilter := types.NewNoLimitPlanFilter()
	planFilter.PlanIDs = uniquePlanIDs
	if filter != nil && filter.Expand != nil {
		s.Logger.Debugw("passing expand filters to plan service", "expand", filter.Expand)
		planFilter.Expand = filter.Expand // pass on the filters to next layer
	}

	planResponse, err := planService.GetPlans(ctx, planFilter)
	if err != nil {
		s.Logger.Errorw("failed to get plans from plan service",
			"error", err,
			"plan_filter", planFilter,
			"plan_ids", uniquePlanIDs)
		return nil, err
	}

	// Build plan map for quick lookup
	for _, plan := range planResponse.Items {
		if plan.Plan == nil {
			s.Logger.Warnw("plan response has nil Plan field", "plan_response", plan)
			continue
		}
		planIDMap[plan.Plan.ID] = plan
	}

	// Get customers in bulk if customer expansion is requested
	var customerIDMap map[string]*dto.CustomerResponse
	if filter.Expand != nil && filter.GetExpand().Has(types.ExpandCustomer) {
		customerIDMap = make(map[string]*dto.CustomerResponse, 0)
		for _, sub := range subscriptions {
			if sub.CustomerID == "" {
				s.Logger.Warnw("subscription has empty customer_id", "subscription_id", sub.ID)
			}
			customerIDMap[sub.CustomerID] = nil
		}

		uniqueCustomerIDs := lo.Keys(customerIDMap)
		s.Logger.Debugw("collected unique customer IDs",
			"unique_customer_count", len(uniqueCustomerIDs),
			"customer_ids", uniqueCustomerIDs)

		// Get customers in bulk
		customerService := NewCustomerService(s.ServiceParams)
		customerFilter := types.NewNoLimitCustomerFilter()
		customerFilter.CustomerIDs = uniqueCustomerIDs

		customerResponse, err := customerService.GetCustomers(ctx, customerFilter)
		if err != nil {
			s.Logger.Errorw("failed to get customers from customer service",
				"error", err,
				"customer_filter", customerFilter,
				"customer_ids", uniqueCustomerIDs)
			return nil, err
		}

		// Build customer map for quick lookup
		for _, customer := range customerResponse.Items {
			if customer.Customer == nil {
				s.Logger.Warnw("customer response has nil Customer field", "customer_response", customer)
				continue
			}
			customerIDMap[customer.Customer.ID] = customer
		}

		s.Logger.Debugw("built customer map", "customer_map_size", len(customerIDMap))
	}

	// Build response with plans and customers
	for i, sub := range subscriptions {
		planResp := planIDMap[sub.PlanID]
		if planResp == nil {
			s.Logger.Warnw("no plan found for subscription",
				"subscription_id", sub.ID,
				"plan_id", sub.PlanID,
				"available_plan_ids", lo.Keys(planIDMap))
		}

		var customerResp *dto.CustomerResponse
		if customerIDMap != nil {
			customerResp = customerIDMap[sub.CustomerID]
			if customerResp == nil {
				s.Logger.Warnw("no customer found for subscription",
					"subscription_id", sub.ID,
					"customer_id", sub.CustomerID,
					"available_customer_ids", lo.Keys(customerIDMap))
			}
		}

		response.Items[i] = &dto.SubscriptionResponse{
			Subscription: sub,
			Plan:         planResp,
			Customer:     customerResp,
		}
	}

	s.Logger.Debugw("built subscription responses", "response_count", len(response.Items))

	s.Logger.Debugw("completed ListSubscriptions successfully",
		"total_items", len(response.Items),
		"total_count", count,
		"pagination", response.Pagination)

	return response, nil
}

func (s *subscriptionService) GetUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error) {
	response := &dto.GetUsageBySubscriptionResponse{}

	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)
	priceService := NewPriceService(s.ServiceParams)

	// Get subscription with line items
	subscription, lineItems, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Get customer
	customer, err := s.CustomerRepo.Get(ctx, subscription.CustomerID)
	if err != nil {
		return nil, err
	}

	usageStartTime := req.StartTime
	if usageStartTime.IsZero() {
		usageStartTime = subscription.CurrentPeriodStart
	}

	// TODO: handle this to honour line item level end time
	usageEndTime := req.EndTime
	if usageEndTime.IsZero() {
		usageEndTime = subscription.CurrentPeriodEnd
	}

	if req.LifetimeUsage {
		usageStartTime = time.Time{}
		usageEndTime = time.Now().UTC()
	}

	// Collect all price IDs
	priceIDs := make([]string, 0, len(lineItems))
	for _, item := range lineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}
		if item.MeterID == "" {
			continue
		}
		priceIDs = append(priceIDs, item.PriceID)
	}

	// Fetch all prices in one call
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.PriceIDs = priceIDs
	priceFilter.Expand = lo.ToPtr(string(types.ExpandMeters))
	priceFilter.AllowExpiredPrices = true
	pricesList, err := priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	// Build price map for quick lookup
	priceMap := make(map[string]*price.Price, len(pricesList.Items))
	meterMap := make(map[string]*dto.MeterResponse, len(pricesList.Items))
	// Pre-fetch all meter display names
	meterDisplayNames := make(map[string]string)

	for _, p := range pricesList.Items {
		priceMap[p.ID] = p.Price
		meterMap[p.Price.MeterID] = p.Meter
		if p.Meter != nil {
			meterDisplayNames[p.Price.MeterID] = p.Meter.Name
		}
	}

	totalCost := decimal.Zero

	s.Logger.Debugw("calculating usage for subscription",
		"subscription_id", req.SubscriptionID,
		"start_time", usageStartTime,
		"end_time", usageEndTime,
		"metered_line_items", len(priceIDs))

	// Performance optimization: Get distinct event names for this customer
	// to filter out meters that have no events, reducing processing from potentially
	// 400-500 meters down to only 5-7 that have actual usage
	distinctEventNames, err := s.EventRepo.GetDistinctEventNames(ctx, customer.ExternalID, usageStartTime, usageEndTime)
	if err != nil {
		s.Logger.Warnw("failed to get distinct event names, proceeding without optimization",
			"error", err,
			"external_customer_id", customer.ExternalID)
		distinctEventNames = nil // Fallback: process all meters if optimization fails
	}

	// Create a map for fast event name lookup
	eventNameExists := make(map[string]bool, len(distinctEventNames))
	for _, eventName := range distinctEventNames {
		eventNameExists[eventName] = true
	}

	s.Logger.Debugw("distinct event names optimization",
		"external_customer_id", customer.ExternalID,
		"total_distinct_events", len(distinctEventNames),
		"total_line_items", len(lineItems),
		"distinct_event_names", distinctEventNames)

	meterUsageRequests := make([]*dto.GetUsageByMeterRequest, 0, len(lineItems))
	for _, lineItem := range lineItems {
		if lineItem.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}

		if lineItem.MeterID == "" {
			continue
		}

		meter := meterMap[lineItem.MeterID]
		if meter == nil {
			continue
		}

		if len(distinctEventNames) == 0 {
			// skip all usage items if distinct event names is nil
			// which means there is no event data in the database
			// this is a fallback to ensure that we don't process all meters
			// if the event data is not available

			s.Logger.Debugw("skipping meter as there are no events",
				"meter_id", lineItem.MeterID,
				"event_name", meter.EventName,
				"customer_id", customer.ID,
				"external_customer_id", customer.ExternalID,
				"subscription_id", req.SubscriptionID)
			continue
		}

		// Performance optimization: Skip meters that don't have any events for this customer
		// Only skip if we successfully got distinct event names (not nil) and the event doesn't exist
		if distinctEventNames != nil && !eventNameExists[meter.EventName] {
			s.Logger.Debugw("skipping meter with no events",
				"meter_id", lineItem.MeterID,
				"event_name", meter.EventName,
				"customer_id", customer.ID,
				"external_customer_id", customer.ExternalID,
				"subscription_id", req.SubscriptionID)
			continue
		}

		meterID := lineItem.MeterID
		usageRequest := &dto.GetUsageByMeterRequest{
			MeterID:            meterID,
			PriceID:            lineItem.PriceID,
			Meter:              meter.ToMeter(),
			ExternalCustomerID: customer.ExternalID,
			StartTime:          lineItem.GetPeriodStart(usageStartTime),
			EndTime:            lineItem.GetPeriodEnd(usageEndTime),
			Filters:            make(map[string][]string),
		}

		for _, filter := range meter.Filters {
			usageRequest.Filters[filter.Key] = filter.Values
		}
		meterUsageRequests = append(meterUsageRequests, usageRequest)
	}

	s.Logger.Infow("performance optimization results",
		"subscription_id", req.SubscriptionID,
		"external_customer_id", customer.ExternalID,
		"total_line_items", len(lineItems),
		"total_usage_line_items", len(priceIDs),
		"meters_with_events", len(meterUsageRequests),
		"optimization_enabled", distinctEventNames != nil,
		"meters_skipped", len(priceIDs)-len(meterUsageRequests))

	usageMap, err := eventService.BulkGetUsageByMeter(ctx, meterUsageRequests)
	if err != nil {
		return nil, err
	}

	s.Logger.Debugw("fetched usage for meters",
		"meter_ids", lo.Keys(usageMap),
		"total_usage_count", len(usageMap),
		"subscription_id", req.SubscriptionID)

	// Store usage charges for later sorting and processing
	var usageCharges []*dto.SubscriptionUsageByMetersResponse

	// First pass: calculate normal costs and build initial charge objects
	// Note: we are iterating over the meterUsageRequests and not the usageMap
	// This is because the usageMap is a map of meterID to usage and we want to iterate over the meterUsageRequests
	// as there can be multiple requests for the same meterID with different priceIDs
	// Ideally this will not be the case and we will have a single request per meterID
	// TODO: should add validation to ensure that same subscription does not have multiple line items with the same meterID
	for _, request := range meterUsageRequests {
		meterID := request.MeterID
		priceID := request.PriceID
		usage, ok := usageMap[priceID]

		if !ok {
			continue
		}

		// Get price by price ID and check if it exists
		priceObj, priceExists := priceMap[usage.PriceID]
		if !priceExists || priceObj == nil {
			return nil, ierr.NewError("price not found").
				WithHint("The price for the meter was not found").
				WithReportableDetails(map[string]interface{}{
					"meter_id":        meterID,
					"price_id":        usage.PriceID,
					"subscription_id": req.SubscriptionID,
				}).
				Mark(ierr.ErrNotFound)
		}

		meterDisplayName := ""
		if meter, ok := meterDisplayNames[meterID]; ok {
			meterDisplayName = meter
		}

		// For bucketed max, we need to handle array of values
		var cost decimal.Decimal
		var quantity decimal.Decimal

		// Get meter info
		meterInfo := meterMap[meterID]
		if priceObj.MeterID != "" && meterInfo != nil && (meterInfo.ToMeter().IsBucketedMaxMeter() || meterInfo.ToMeter().IsBucketedSumMeter()) {
			// For bucketed max, use the array of values
			bucketedValues := make([]decimal.Decimal, len(usage.Results))
			for i, result := range usage.Results {
				bucketedValues[i] = result.Value
			}
			cost = priceService.CalculateBucketedCost(ctx, priceObj, bucketedValues)

			// Calculate quantity as sum of all bucket maxes
			quantity = decimal.Zero
			for _, bucketValue := range bucketedValues {
				quantity = quantity.Add(bucketValue)
			}
		} else {
			// For all other cases, use the single value
			quantity = usage.Value
			cost = priceService.CalculateCost(ctx, priceObj, quantity)
		}

		s.Logger.Debugw("calculated usage for meter",
			"meter_id", meterID,
			"quantity", quantity,
			"cost", cost,
			"meter_display_name", meterDisplayName,
			"subscription_id", req.SubscriptionID,
			"usage", usage,
			"price", priceObj,
		)

		charge := createChargeResponse(
			priceObj,
			quantity,
			cost,
			meterDisplayName,
		)

		if charge == nil {
			continue
		}

		usageCharges = append(usageCharges, charge)
		totalCost = totalCost.Add(cost)
	}

	// Apply commitment logic if set on the subscription
	hasCommitment := false

	commitmentAmount := lo.FromPtr(subscription.CommitmentAmount)
	overageFactor := lo.FromPtr(subscription.OverageFactor)

	// Check if commitment amount is greater than zero
	if commitmentAmount.GreaterThan(decimal.Zero) {
		// Check if overage factor is greater than 1.0
		oneDecimal := decimal.NewFromInt(1)
		hasCommitment = overageFactor.GreaterThan(oneDecimal)
	}

	// Default values assuming no commitment/overage
	commitmentFloat, _ := commitmentAmount.Float64()
	overageFactorFloat, _ := overageFactor.Float64()
	response.CommitmentAmount = commitmentFloat
	response.OverageFactor = overageFactorFloat
	response.HasOverage = false

	// Initialize charges list with enough capacity
	response.Charges = make([]*dto.SubscriptionUsageByMetersResponse, 0, len(usageCharges)*2)

	// If using commitment-based pricing, process charges with overage logic
	if hasCommitment {
		// First, filter charges to only include usage-based charges for commitment calculations
		// Fixed charges are not subject to commitment/overage
		var usageOnlyCharges []*dto.SubscriptionUsageByMetersResponse
		var fixedCharges []*dto.SubscriptionUsageByMetersResponse

		for _, charge := range usageCharges {
			if charge.Price != nil && charge.Price.Type == types.PRICE_TYPE_USAGE {
				usageOnlyCharges = append(usageOnlyCharges, charge)
			} else {
				// Add fixed charges directly to the response without overage calculation
				fixedCharges = append(fixedCharges, charge)
			}
		}

		// Add all fixed charges directly to the response
		response.Charges = append(response.Charges, fixedCharges...)

		// Track remaining commitment and process each usage charge
		remainingCommitment := commitmentAmount
		totalOverageAmount := decimal.Zero

		for _, charge := range usageOnlyCharges {
			// Get charge amount as decimal for precise calculations
			chargeAmount := decimal.NewFromFloat(charge.Amount)
			pricePerUnit := decimal.Zero
			if charge.Price != nil && charge.Price.BillingModel == types.BILLING_MODEL_FLAT_FEE {
				pricePerUnit = charge.Price.Amount
			} else if charge.Quantity > 0 {
				pricePerUnit = chargeAmount.Div(decimal.NewFromFloat(charge.Quantity))
			}

			// Normal price covers all of this charge
			if remainingCommitment.GreaterThanOrEqual(chargeAmount) {
				charge.IsOverage = false
				remainingCommitment = remainingCommitment.Sub(chargeAmount)
				response.Charges = append(response.Charges, charge)
				continue
			}

			// Charge needs to be split between normal and overage
			if remainingCommitment.GreaterThan(decimal.Zero) {
				// Calculate exact quantity that can be covered by remaining commitment
				var normalQuantityDecimal decimal.Decimal

				if !pricePerUnit.IsZero() {
					normalQuantityDecimal = remainingCommitment.Div(pricePerUnit)

					// Round down to ensure we don't exceed commitment
					normalQuantityDecimal = normalQuantityDecimal.Floor()
				}

				// Calculate the normal amount based on the normal quantity
				normalAmountDecimal := normalQuantityDecimal.Mul(pricePerUnit)

				// Create the normal charge
				if normalQuantityDecimal.GreaterThan(decimal.Zero) {
					normalCharge := *charge // Create a copy
					normalCharge.Quantity = normalQuantityDecimal.InexactFloat64()
					normalCharge.Amount = price.FormatAmountToFloat64WithPrecision(normalAmountDecimal, subscription.Currency)
					normalCharge.DisplayAmount = price.FormatAmountToStringWithPrecision(normalAmountDecimal, subscription.Currency)
					normalCharge.IsOverage = false
					response.Charges = append(response.Charges, &normalCharge)
				}

				// Calculate overage quantity and amount
				overageQuantityDecimal := decimal.NewFromFloat(charge.Quantity).Sub(normalQuantityDecimal)

				// Create the overage charge only if there's actual overage
				if overageQuantityDecimal.GreaterThan(decimal.Zero) {
					overageAmountDecimal := overageQuantityDecimal.Mul(pricePerUnit).Mul(overageFactor)
					totalOverageAmount = totalOverageAmount.Add(overageAmountDecimal)

					overageCharge := *charge // Create a copy
					overageCharge.Quantity = overageQuantityDecimal.InexactFloat64()
					overageCharge.Amount = price.FormatAmountToFloat64WithPrecision(overageAmountDecimal, subscription.Currency)
					overageCharge.DisplayAmount = price.FormatAmountToStringWithPrecision(overageAmountDecimal, subscription.Currency)
					overageCharge.IsOverage = true
					overageCharge.OverageFactor = overageFactorFloat
					response.Charges = append(response.Charges, &overageCharge)
					response.HasOverage = true
				}

				// Update remaining commitment (should be zero or very close to it due to rounding)
				remainingCommitment = remainingCommitment.Sub(normalAmountDecimal)
				continue
			}

			// Charge is entirely in overage
			overageAmountDecimal := chargeAmount.Mul(overageFactor)
			totalOverageAmount = totalOverageAmount.Add(overageAmountDecimal)

			charge.Amount = price.FormatAmountToFloat64WithPrecision(overageAmountDecimal, subscription.Currency)
			charge.DisplayAmount = overageAmountDecimal.StringFixed(6)
			charge.IsOverage = true
			charge.OverageFactor = overageFactorFloat
			response.Charges = append(response.Charges, charge)
			response.HasOverage = true
		}

		// Calculate final amounts for response
		commitmentUtilized := commitmentAmount.Sub(remainingCommitment)
		commitmentUtilizedFloat, _ := commitmentUtilized.Float64()
		overageAmountFloat, _ := totalOverageAmount.Float64()
		response.CommitmentUtilized = commitmentUtilizedFloat
		response.OverageAmount = overageAmountFloat

		// Update total cost with commitment + overage calculation
		totalCost = commitmentUtilized.Add(totalOverageAmount)
	} else {
		// Without commitment, just use the original charges
		response.Charges = usageCharges
	}

	response.StartTime = usageStartTime
	response.EndTime = usageEndTime
	response.Amount = price.FormatAmountToFloat64WithPrecision(totalCost, subscription.Currency)
	response.Currency = subscription.Currency
	response.DisplayAmount = price.GetDisplayAmountWithPrecision(totalCost, subscription.Currency)
	return response, nil
}

// UpdateBillingPeriods updates the current billing periods for all active subscriptions
// This should be run every 15 minutes to ensure billing periods are up to date
// TODO: move to billing service
func (s *subscriptionService) UpdateBillingPeriods(ctx context.Context) (*dto.SubscriptionUpdatePeriodResponse, error) {
	const batchSize = 100
	now := time.Now().UTC()

	s.Logger.Infow("starting billing period updates",
		"current_time", now)

	response := &dto.SubscriptionUpdatePeriodResponse{
		Items:        make([]*dto.SubscriptionUpdatePeriodResponseItem, 0),
		TotalFailed:  0,
		TotalSuccess: 0,
		StartAt:      now,
	}

	offset := 0
	for {
		filter := &types.SubscriptionFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(batchSize),
				Offset: lo.ToPtr(offset),
				Status: lo.ToPtr(types.StatusPublished),
			},
			SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive},
			TimeRangeFilter: &types.TimeRangeFilter{
				EndTime: &now,
			},
		}

		subs, err := s.SubRepo.ListAllTenant(ctx, filter)
		if err != nil {
			return response, err
		}

		s.Logger.Infow("processing subscription batch",
			"batch_size", len(subs),
			"offset", offset)

		if len(subs) == 0 {
			break // No more subscriptions to process
		}

		// Process each subscription in the batch
		for _, sub := range subs {
			// update context to include the tenant id
			ctx = context.WithValue(ctx, types.CtxTenantID, sub.TenantID)
			ctx = context.WithValue(ctx, types.CtxEnvironmentID, sub.EnvironmentID)
			ctx = context.WithValue(ctx, types.CtxUserID, sub.CreatedBy)

			item := &dto.SubscriptionUpdatePeriodResponseItem{
				SubscriptionID: sub.ID,
				PeriodStart:    sub.CurrentPeriodStart,
				PeriodEnd:      sub.CurrentPeriodEnd,
			}
			err = s.processSubscriptionPeriod(ctx, sub, now)
			if err != nil {
				s.Logger.Errorw("failed to process subscription period",
					"subscription_id", sub.ID,
					"error", err)

				response.TotalFailed++
				item.Error = err.Error()
			} else {
				item.Success = true
				response.TotalSuccess++
			}

			response.Items = append(response.Items, item)
		}

		offset += len(subs)
		if len(subs) < batchSize {
			break // No more subscriptions to fetch
		}
	}

	return response, nil
}

/// Helpers

// isDraftSubscription checks if a subscription is in draft status
func (s *subscriptionService) isDraftSubscription(sub *subscription.Subscription) bool {
	return sub.SubscriptionStatus == types.SubscriptionStatusDraft
}

// validateNotDraftSubscription validates that a subscription is not in draft status
// Returns an error if the subscription is draft, nil otherwise
func (s *subscriptionService) validateNotDraftSubscription(sub *subscription.Subscription, operation string) error {
	if s.isDraftSubscription(sub) {
		return ierr.NewError("cannot perform operation on draft subscription").
			WithHint(fmt.Sprintf("Draft subscriptions must be activated before %s.", operation)).
			WithReportableDetails(map[string]interface{}{
				"subscription_id":     sub.ID,
				"subscription_status": sub.SubscriptionStatus,
				"operation":           operation,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// we get each subscription picked by the cron where the current period end is before now
// and we process the subscription period to create invoices for the passed period
// and decide next period start and end or cancel the subscription if it has ended
func (s *subscriptionService) processSubscriptionPeriod(ctx context.Context, sub *subscription.Subscription, now time.Time) error {
	// Skip processing for draft subscriptions
	if s.isDraftSubscription(sub) {
		s.Logger.Infow("skipping period processing for draft subscription",
			"subscription_id", sub.ID)
		return nil
	}

	// Skip processing for paused subscriptions
	if sub.SubscriptionStatus == types.SubscriptionStatusPaused {
		s.Logger.Infow("skipping period processing for paused subscription",
			"subscription_id", sub.ID)
		return nil
	}

	// Check for scheduled pauses that should be activated
	if sub.PauseStatus == types.PauseStatusScheduled && sub.ActivePauseID != nil {
		pause, err := s.SubRepo.GetPause(ctx, *sub.ActivePauseID)
		if err != nil {
			return err
		}

		// If this is a period-end pause and we're at period end, activate it
		if pause.PauseMode == types.PauseModePeriodEnd && !now.Before(sub.CurrentPeriodEnd) {
			sub.SubscriptionStatus = types.SubscriptionStatusPaused
			pause.PauseStatus = types.PauseStatusActive

			// Update the subscription and pause
			if err := s.SubRepo.Update(ctx, sub); err != nil {
				return err
			}

			if err := s.SubRepo.UpdatePause(ctx, pause); err != nil {
				return err
			}

			s.Logger.Infow("activated period-end pause",
				"subscription_id", sub.ID,
				"pause_id", pause.ID)

			// Skip further processing
			return nil
		}

		// If this is a scheduled pause and we've reached the start date, activate it
		if pause.PauseMode == types.PauseModeScheduled && !now.Before(pause.PauseStart) {
			sub.SubscriptionStatus = types.SubscriptionStatusPaused
			pause.PauseStatus = types.PauseStatusActive

			// Update the subscription and pause
			if err := s.SubRepo.Update(ctx, sub); err != nil {
				return err
			}

			if err := s.SubRepo.UpdatePause(ctx, pause); err != nil {
				return err
			}

			s.Logger.Infow("activated scheduled pause",
				"subscription_id", sub.ID,
				"pause_id", pause.ID)

			// Skip further processing
			return nil
		}
	}

	// Check for auto-resume based on pause end date
	if sub.SubscriptionStatus == types.SubscriptionStatusPaused && sub.ActivePauseID != nil {
		pause, err := s.SubRepo.GetPause(ctx, *sub.ActivePauseID)
		if err != nil {
			return err
		}

		// If this pause has an end date and we've reached it, auto-resume
		if pause.PauseEnd != nil && !now.Before(*pause.PauseEnd) {
			// Calculate the pause duration
			pauseDuration := now.Sub(pause.PauseStart)

			// Update the pause record
			pause.PauseStatus = types.PauseStatusCompleted
			pause.ResumedAt = &now

			// Update the subscription
			sub.SubscriptionStatus = types.SubscriptionStatusActive
			sub.PauseStatus = types.PauseStatusNone
			sub.ActivePauseID = nil

			// Adjust the billing period by the pause duration
			sub.CurrentPeriodEnd = sub.CurrentPeriodEnd.Add(pauseDuration)

			// Update the subscription and pause
			if err := s.SubRepo.Update(ctx, sub); err != nil {
				return err
			}

			if err := s.SubRepo.UpdatePause(ctx, pause); err != nil {
				return err
			}

			s.Logger.Infow("auto-resumed subscription",
				"subscription_id", sub.ID,
				"pause_id", pause.ID,
				"pause_duration", pauseDuration)

			// Continue with normal processing
		} else {
			// Still paused, skip processing
			s.Logger.Infow("skipping period processing for paused subscription",
				"subscription_id", sub.ID)
			return nil
		}
	}

	// TODO: Check if subscription has ended and should be cancelled

	// Initialize services
	invoiceService := NewInvoiceService(s.ServiceParams)

	currentStart := sub.CurrentPeriodStart
	currentEnd := sub.CurrentPeriodEnd

	// Start with current period
	var periods []struct {
		start time.Time
		end   time.Time
	}
	periods = append(periods, struct {
		start time.Time
		end   time.Time
	}{
		start: currentStart,
		end:   currentEnd,
	})

	// isLastPeriod := false
	// if sub.EndDate != nil && currentEnd.Equal(*sub.EndDate) {
	// 	isLastPeriod = true
	// }

	// Generate periods but respect subscription end date
	for currentEnd.Before(now) {
		nextStart := currentEnd
		nextEnd, err := types.NextBillingDate(nextStart, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod, sub.EndDate)
		if err != nil {
			s.Logger.Errorw("failed to calculate next billing date",
				"subscription_id", sub.ID,
				"current_end", currentEnd,
				"process_up_to", now,
				"error", err)
			return err
		}

		periods = append(periods, struct {
			start time.Time
			end   time.Time
		}{
			start: nextStart,
			end:   nextEnd,
		})

		// in case of end date reached or next end is equal to current end, we break the loop
		// nextEnd will be equal to currentEnd in case of end date reached
		if nextEnd.Equal(currentEnd) {
			s.Logger.Infow("stopped period generation - reached subscription end date",
				"subscription_id", sub.ID,
				"end_date", sub.EndDate,
				"final_period_end", currentEnd)
			break
		}

		currentEnd = nextEnd
	}

	if len(periods) == 1 {
		s.Logger.Debugw("no transitions needed for subscription",
			"subscription_id", sub.ID,
			"current_period_start", sub.CurrentPeriodStart,
			"current_period_end", sub.CurrentPeriodEnd,
			"process_up_to", now)
		return nil
	}

	// Use db's WithTx for atomic operations
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Process all periods except the last one (which becomes the new current period)
		for i := 0; i < len(periods)-1; i++ {
			period := periods[i]

			// Create a single invoice for both arrear and advance charges at period end
			paymentParams := dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID)
			// Apply backward compatibility normalization
			paymentParams = paymentParams.NormalizePaymentParameters()
			inv, updatedSub, err := invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
				SubscriptionID: sub.ID,
				PeriodStart:    period.start,
				PeriodEnd:      period.end,
				ReferencePoint: types.ReferencePointPeriodEnd,
			}, paymentParams, types.InvoiceFlowRenewal, false)
			if err != nil {
				return err
			}

			// Use the updated subscription from CreateSubscriptionInvoice to avoid extra DB call
			if updatedSub != nil {
				sub = updatedSub
			}

			// Check for cancellation at this period end
			if sub.CancelAtPeriodEnd && sub.CancelAt != nil && !sub.CancelAt.After(period.end) {
				sub.SubscriptionStatus = types.SubscriptionStatusCancelled
				sub.EndDate = sub.CancelAt
				break
			}

			// Check if this period end matches the subscription end date
			if sub.EndDate != nil && period.end.Equal(*sub.EndDate) {
				sub.SubscriptionStatus = types.SubscriptionStatusCancelled
				sub.CancelledAt = sub.EndDate
				s.Logger.Infow("will cancel subscription at end of this period",
					"subscription_id", sub.ID,
					"period_end", period.end,
					"end_date", *sub.EndDate)
				break
			}

			if inv == nil {
				s.Logger.Infow("no invoice was created for period",
					"subscription_id", sub.ID,
					"period_start", period.start,
					"period_end", period.end,
					"period_index", i)
				continue
			}

			s.Logger.Infow("created invoice for period",
				"subscription_id", sub.ID,
				"invoice_id", inv.ID,
				"period_start", period.start,
				"period_end", period.end,
				"period_index", i)
		}

		// Update to the new current period (last period)
		newPeriod := periods[len(periods)-1]
		sub.CurrentPeriodStart = newPeriod.start
		sub.CurrentPeriodEnd = newPeriod.end

		// Final cancellation check
		if sub.CancelAtPeriodEnd && sub.CancelAt != nil && !sub.CancelAt.After(newPeriod.end) {
			sub.SubscriptionStatus = types.SubscriptionStatusCancelled
		}

		// Check if the new period end matches the subscription end date
		if sub.EndDate != nil && newPeriod.end.Equal(*sub.EndDate) {
			sub.SubscriptionStatus = types.SubscriptionStatusCancelled
			sub.CancelledAt = sub.EndDate
			s.Logger.Infow("subscription will be cancelled at new period end (end date reached)",
				"subscription_id", sub.ID,
				"new_period_end", newPeriod.end,
				"end_date", *sub.EndDate)
		}

		// Update the subscription
		if err := s.SubRepo.Update(ctx, sub); err != nil {
			return err
		}

		s.Logger.Infow("completed subscription period processing",
			"subscription_id", sub.ID,
			"original_period_start", periods[0].start,
			"original_period_end", periods[0].end,
			"new_period_start", sub.CurrentPeriodStart,
			"new_period_end", sub.CurrentPeriodEnd,
			"process_up_to", now,
			"periods_processed", len(periods)-1,
			"has_end_date", sub.EndDate != nil)

		return nil
	})

	if err != nil {
		s.Logger.Errorw("failed to process subscription period",
			"subscription_id", sub.ID,
			"error", err)
		return err
	}

	return nil
}

func createChargeResponse(priceObj *price.Price, quantity decimal.Decimal, cost decimal.Decimal, meterDisplayName string) *dto.SubscriptionUsageByMetersResponse {
	if priceObj == nil {
		return nil
	}

	finalAmount := price.FormatAmountToFloat64WithPrecision(cost, priceObj.Currency)

	return &dto.SubscriptionUsageByMetersResponse{
		Amount:           finalAmount,
		Currency:         priceObj.Currency,
		DisplayAmount:    price.GetDisplayAmountWithPrecision(cost, priceObj.Currency),
		Quantity:         quantity.InexactFloat64(),
		MeterID:          priceObj.MeterID,
		MeterDisplayName: meterDisplayName,
		Price:            priceObj,
	}
}

// filterValidPricesForSubscription filters prices that are valid for a subscription
// This utility function can be used for both plans and addons
func filterValidPricesForSubscription(prices []*dto.PriceResponse, subscription *subscription.Subscription) []*dto.PriceResponse {
	var validPrices []*dto.PriceResponse
	for _, p := range prices {
		if types.IsMatchingCurrency(p.Price.Currency, subscription.Currency) &&
			p.Price.BillingPeriod == subscription.BillingPeriod &&
			p.Price.BillingPeriodCount == subscription.BillingPeriodCount {
			validPrices = append(validPrices, p)
		}
	}
	return validPrices
}

// ValidateAndFilterPricesForSubscription validates and filters prices for a subscription
// This method follows the same validation pattern as plans and can be reused for addons
func (s *subscriptionService) ValidateAndFilterPricesForSubscription(
	ctx context.Context,
	entityID string,
	entityType types.PriceEntityType,
	subscription *subscription.Subscription,
	workflowType *types.TemporalWorkflowType,
) ([]*dto.PriceResponse, error) {
	// Get prices for the entity (plan or addon)
	priceService := NewPriceService(s.ServiceParams)

	var pricesResponse *dto.ListPricesResponse
	var err error

	if entityType == types.PRICE_ENTITY_TYPE_PLAN {
		pricesResponse, err = priceService.GetPricesByPlanID(ctx, dto.GetPricesByPlanRequest{
			PlanID:       entityID,
			AllowExpired: false,
		})
	} else if entityType == types.PRICE_ENTITY_TYPE_ADDON {
		pricesResponse, err = priceService.GetPricesByAddonID(ctx, entityID)
	}

	if err != nil {
		return nil, err
	}

	// Check if empty prices are allowed for this workflow type
	if !s.allowsEmptyPrices(workflowType) {
		if len(pricesResponse.Items) == 0 {
			return nil, ierr.NewError("no prices found for entity").
				WithHint("The entity must have at least one price to create a subscription").
				WithReportableDetails(map[string]interface{}{
					"entity_id":   entityID,
					"entity_type": entityType,
				}).
				Mark(ierr.ErrValidation)
		}

		// Filter prices for subscription that are valid for the entity
		validPrices := filterValidPricesForSubscription(pricesResponse.Items, subscription)
		if len(validPrices) == 0 {
			return nil, ierr.NewError("no valid prices found for subscription").
				WithHint("No prices match the subscription criteria").
				WithReportableDetails(map[string]interface{}{
					"entity_id":   entityID,
					"entity_type": entityType,
				}).
				Mark(ierr.ErrValidation)
		}
		return validPrices, nil
	}

	// For workflows that allow empty prices, filter and return (even if empty)
	validPrices := filterValidPricesForSubscription(pricesResponse.Items, subscription)

	return validPrices, nil
}

// allowsEmptyPrices checks if the given workflow type allows empty prices
func (s *subscriptionService) allowsEmptyPrices(workflowType *types.TemporalWorkflowType) bool {
	if workflowType == nil {
		return false
	}

	// Define workflow types that allow empty prices
	emptyPricesAllowedWorkflows := []types.TemporalWorkflowType{
		types.TemporalStripeIntegrationWorkflow,
		// Add more workflow types here as needed
	}

	return lo.Contains(emptyPricesAllowedWorkflows, *workflowType)
}

// PauseSubscription pauses a subscription
func (s *subscriptionService) PauseSubscription(
	ctx context.Context,
	subscriptionID string,
	req *dto.PauseSubscriptionRequest,
) (*dto.PauseSubscriptionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the subscription
	sub, lineItems, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription for pausing").
			Mark(ierr.ErrNotFound)
	}
	sub.LineItems = lineItems

	// Validate subscription can be paused
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("invalid subscription status").
			WithHint("Subscription is not active").
			WithReportableDetails(map[string]any{
				"status": sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Calculate pause start and end
	pauseStart, pauseEnd, err := s.calculatePauseStartEnd(req, sub)
	if err != nil {
		return nil, err
	}

	// Use the unified billing impact calculator
	impact, err := s.calculateBillingImpact(ctx, sub, lineItems, *pauseStart, pauseEnd, false, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to calculate billing impact").
			Mark(ierr.ErrValidation)
	}

	// If this is a dry run, return the impact without making changes
	if req.DryRun {
		return &dto.PauseSubscriptionResponse{
			BillingImpact: impact,
			DryRun:        true,
		}, nil
	}

	// Create the pause record and update the subscription
	sub, pause, err := s.executePause(ctx, sub, req, pauseStart, pauseEnd)
	if err != nil {
		return nil, err
	}

	response := dto.NewSubscriptionPauseResponse(sub, pause)
	response.BillingImpact = impact

	// Return the response
	// Publish webhook event
	s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)
	s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionPaused, subscriptionID)
	return response, nil
}

// executePause creates the pause record and updates the subscription
func (s *subscriptionService) executePause(
	ctx context.Context,
	sub *subscription.Subscription,
	req *dto.PauseSubscriptionRequest,
	pauseStart *time.Time,
	pauseEnd *time.Time,
) (*subscription.Subscription, *subscription.SubscriptionPause, error) {
	// Set pause status based on mode
	pauseStatus := types.PauseStatusActive
	if req.PauseMode == types.PauseModeScheduled || req.PauseMode == types.PauseModePeriodEnd {
		pauseStatus = types.PauseStatusScheduled
	}

	// Create the pause record
	pause := &subscription.SubscriptionPause{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PAUSE),
		SubscriptionID:      sub.ID,
		PauseStatus:         pauseStatus,
		PauseMode:           req.PauseMode,
		ResumeMode:          types.ResumeModeAuto, // Default to auto resume if pause end is set
		PauseStart:          *pauseStart,
		PauseEnd:            pauseEnd,
		ResumedAt:           nil,
		OriginalPeriodStart: sub.CurrentPeriodStart,
		OriginalPeriodEnd:   sub.CurrentPeriodEnd,
		Reason:              req.Reason,
		Metadata:            req.Metadata,
		EnvironmentID:       sub.EnvironmentID,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}

	// Update the subscription
	sub.PauseStatus = pauseStatus
	sub.ActivePauseID = lo.ToPtr(pause.ID)

	// Only change subscription status to paused for immediate pauses
	if req.PauseMode == types.PauseModeImmediate {
		sub.SubscriptionStatus = types.SubscriptionStatusPaused
	}

	// Execute the transaction
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Create the pause record
		if err := s.SubRepo.CreatePause(txCtx, pause); err != nil {
			return err
		}

		// Update the subscription
		if err := s.SubRepo.Update(txCtx, sub); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	return sub, pause, nil
}

// ResumeSubscription resumes a paused subscription
func (s *subscriptionService) ResumeSubscription(
	ctx context.Context,
	subscriptionID string,
	req *dto.ResumeSubscriptionRequest,
) (*dto.ResumeSubscriptionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the subscription with its pauses
	_, pauses, err := s.SubRepo.GetWithPauses(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	// get the line items
	sub, lineItems, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	sub.LineItems = lineItems
	sub.Pauses = pauses

	// Reject resume of draft subscriptions
	if err := s.validateNotDraftSubscription(sub, "resume"); err != nil {
		return nil, err
	}

	// Validate subscription can be resumed
	if sub.SubscriptionStatus != types.SubscriptionStatusPaused &&
		sub.PauseStatus != types.PauseStatusScheduled {
		return nil, ierr.NewError("invalid subscription status").
			WithHint("Subscription is not paused").
			WithReportableDetails(map[string]any{
				"status": sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	if sub.ActivePauseID == nil {
		return nil, ierr.NewError("invalid subscription status").
			WithHint("Subscription has no active pause").
			Mark(ierr.ErrValidation)
	}

	// Find the active pause
	var activePause *subscription.SubscriptionPause
	for _, p := range pauses {
		if p.ID == *sub.ActivePauseID {
			activePause = p
			break
		}
	}

	if activePause == nil {
		return nil, ierr.NewError("invalid subscription status").
			WithHint("Active pause not found").
			Mark(ierr.ErrValidation)
	}

	// Use the unified billing impact calculator
	impact, err := s.calculateBillingImpact(ctx, sub, lineItems, activePause.PauseStart, activePause.PauseEnd, true, activePause)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to calculate billing impact").
			Mark(ierr.ErrValidation)
	}

	// If this is a dry run, return the impact without making changes
	if req.DryRun {
		return &dto.ResumeSubscriptionResponse{
			BillingImpact: impact,
			DryRun:        true,
		}, nil
	}

	// Resume the subscription
	sub, activePause, err = s.executeResume(ctx, sub, activePause, req)
	if err != nil {
		return nil, err
	}

	// Publish webhook event
	s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)
	s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionResumed, subscriptionID)

	// Return the response
	return &dto.ResumeSubscriptionResponse{
		Subscription: &dto.SubscriptionResponse{
			Subscription: sub,
		},
		Pause: &dto.SubscriptionPauseResponse{
			SubscriptionPause: activePause,
		},
		BillingImpact: impact,
		DryRun:        false,
	}, nil
}

// executeResume updates the subscription and pause record for a resume operation
func (s *subscriptionService) executeResume(
	ctx context.Context,
	sub *subscription.Subscription,
	activePause *subscription.SubscriptionPause,
	req *dto.ResumeSubscriptionRequest,
) (*subscription.Subscription, *subscription.SubscriptionPause, error) {
	// Update the pause record
	now := time.Now()
	activePause.PauseStatus = types.PauseStatusCompleted
	activePause.ResumeMode = req.ResumeMode
	activePause.ResumedAt = &now
	activePause.Metadata = req.Metadata
	activePause.UpdatedBy = types.GetUserID(ctx)

	// Calculate the pause duration
	pauseDuration := now.Sub(activePause.PauseStart)

	// Update the subscription
	sub.PauseStatus = types.PauseStatusNone
	sub.ActivePauseID = nil

	// Only change subscription status if it was paused
	if sub.SubscriptionStatus == types.SubscriptionStatusPaused {
		sub.SubscriptionStatus = types.SubscriptionStatusActive
	}

	// Adjust the billing period by the pause duration
	sub.CurrentPeriodEnd = sub.CurrentPeriodEnd.Add(pauseDuration)

	// Execute the transaction
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Update the pause record
		if err := s.SubRepo.UpdatePause(txCtx, activePause); err != nil {
			return err
		}

		// Update the subscription
		if err := s.SubRepo.Update(txCtx, sub); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	return sub, activePause, nil
}

// GetPause gets a subscription pause by ID
func (s *subscriptionService) GetPause(ctx context.Context, pauseID string) (*subscription.SubscriptionPause, error) {
	pause, err := s.SubRepo.GetPause(ctx, pauseID)
	if err != nil {
		return nil, err
	}
	return pause, nil
}

// ListPauses lists all pauses for a subscription
func (s *subscriptionService) ListPauses(ctx context.Context, subscriptionID string) (*dto.ListSubscriptionPausesResponse, error) {
	pauses, err := s.SubRepo.ListPauses(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	return dto.NewListSubscriptionPausesResponse(pauses), nil
}

// CalculatePauseImpact calculates the billing impact of pausing a subscription
func (s *subscriptionService) CalculatePauseImpact(
	ctx context.Context,
	subscriptionID string,
	req *dto.PauseSubscriptionRequest,
) (*types.BillingImpactDetails, error) {
	// Get the subscription
	sub, lineItems, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Validate subscription can be paused
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("invalid subscription status").
			WithHint("Subscription is not active").
			WithReportableDetails(map[string]any{
				"status": sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Calculate pause start and end
	pauseStart, pauseEnd, err := s.calculatePauseStartEnd(req, sub)
	if err != nil {
		return nil, err
	}

	// Use the unified billing impact calculator
	return s.calculateBillingImpact(ctx, sub, lineItems, *pauseStart, pauseEnd, false, nil)
}

// CalculateResumeImpact calculates the billing impact of resuming a subscription
func (s *subscriptionService) CalculateResumeImpact(
	ctx context.Context,
	subscriptionID string,
	req *dto.ResumeSubscriptionRequest,
) (*types.BillingImpactDetails, error) {
	// Get the subscription with its pauses
	_, pauses, err := s.SubRepo.GetWithPauses(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// get the line items
	sub, lineItems, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	sub.LineItems = lineItems
	sub.Pauses = pauses

	// Validate subscription can be resumed
	if sub.SubscriptionStatus != types.SubscriptionStatusPaused &&
		sub.PauseStatus != types.PauseStatusScheduled {
		return nil, ierr.NewError("invalid subscription status").
			WithHint("Subscription is not paused").
			WithReportableDetails(map[string]any{
				"status": sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	if sub.ActivePauseID == nil {
		return nil, ierr.NewError("invalid subscription status").
			WithHint("Subscription has no active pause").
			Mark(ierr.ErrValidation)
	}

	// Find the active pause
	var activePause *subscription.SubscriptionPause
	for _, p := range pauses {
		if p.ID == *sub.ActivePauseID {
			activePause = p
			break
		}
	}

	if activePause == nil {
		return nil, ierr.NewError("invalid subscription status").
			WithHint("Active pause not found").
			Mark(ierr.ErrValidation)
	}

	// Use the unified billing impact calculator
	return s.calculateBillingImpact(ctx, sub, lineItems, activePause.PauseStart, activePause.PauseEnd, true, activePause)
}

// Pause subscription helper methods

// calculatePauseStartEnd calculates the pause start and end dates based on the pause mode
// requested input and the subscription's current period end date.
// TODO: add a config check for max pause duration and make it configurable for each tenant
func (s *subscriptionService) calculatePauseStartEnd(req *dto.PauseSubscriptionRequest, sub *subscription.Subscription) (*time.Time, *time.Time, error) {
	now := time.Now().UTC()

	// First lets handle pause_start date based on pause mode
	var pauseStart, pauseEnd *time.Time
	switch req.PauseMode {
	case types.PauseModeImmediate:
		pauseStart = &now
	case types.PauseModeScheduled:
		pauseStart = req.PauseStart
	case types.PauseModePeriodEnd:
		pauseStart = lo.ToPtr(sub.CurrentPeriodEnd)
	default:
		return nil, nil, ierr.NewError("invalid pause mode").
			WithHint("Invalid pause mode").
			WithReportableDetails(map[string]any{
				"pauseMode": req.PauseMode,
			}).
			Mark(ierr.ErrValidation)
	}

	if pauseStart == nil || pauseStart.IsZero() {
		return nil, nil, ierr.NewError("invalid pause start date").
			WithHint("Pause start date is required").
			Mark(ierr.ErrValidation)
	}

	if req.PauseDays != nil {
		pauseEnd = lo.ToPtr(pauseStart.AddDate(0, 0, *req.PauseDays))
	} else if req.PauseEnd != nil {
		pauseEnd = req.PauseEnd
	}

	if pauseEnd == nil || pauseEnd.IsZero() || pauseEnd.Before(*pauseStart) {
		return nil, nil, ierr.NewError("invalid pause end date").
			WithHint("Pause end date is not valid").
			WithReportableDetails(map[string]any{
				"pauseStart": pauseStart,
				"pauseEnd":   pauseEnd,
			}).
			Mark(ierr.ErrValidation)
	}

	return pauseStart, pauseEnd, nil
}

// calculateBillingImpact calculates the billing impact of pause/resume operations
func (s *subscriptionService) calculateBillingImpact(
	_ context.Context,
	sub *subscription.Subscription,
	lineItems []*subscription.SubscriptionLineItem,
	pauseStart time.Time,
	pauseEnd *time.Time,
	isResume bool,
	activePause *subscription.SubscriptionPause,
) (*types.BillingImpactDetails, error) {
	// Initialize impact details
	impact := &types.BillingImpactDetails{}

	// Get subscription configuration for billing model (advance vs. arrears)
	// TODO: handle this when we implement add ons with one time charges
	var invoiceCadence types.InvoiceCadence
	for _, li := range lineItems {
		if li.PriceType == types.PRICE_TYPE_FIXED {
			invoiceCadence = li.InvoiceCadence
			break
		}
	}

	// TODO: need to handle this better for cases with no fixed prices
	if invoiceCadence == "" {
		invoiceCadence = types.InvoiceCadenceArrear
	}

	// Set original period information
	if isResume && activePause != nil {
		impact.OriginalPeriodStart = &activePause.OriginalPeriodStart
		impact.OriginalPeriodEnd = &activePause.OriginalPeriodEnd
	} else {
		impact.OriginalPeriodStart = &sub.CurrentPeriodStart
		impact.OriginalPeriodEnd = &sub.CurrentPeriodEnd
	}

	now := time.Now()

	if isResume {
		// Resume impact calculation
		if activePause == nil {
			return nil, ierr.NewError("missing active pause").
				WithHint("Cannot calculate resume impact without active pause").
				Mark(ierr.ErrValidation)
		}

		// Calculate pause duration
		pauseDuration := now.Sub(activePause.PauseStart)
		impact.PauseDurationDays = int(pauseDuration.Hours() / 24)

		// Set next billing date to now for immediate resumes
		impact.NextBillingDate = &now

		// Calculate adjusted period dates
		adjustedStart := now
		adjustedEnd := activePause.OriginalPeriodEnd.Add(pauseDuration)
		impact.AdjustedPeriodStart = &adjustedStart
		impact.AdjustedPeriodEnd = &adjustedEnd

		// Calculate next billing amount based on billing model
		if invoiceCadence == types.InvoiceCadenceAdvance {
			// For advance billing, calculate the prorated amount for the resumed period
			// This is a simplified calculation - in a real implementation, you would
			// need to consider the subscription's line items, pricing, etc.
			totalPeriodDuration := activePause.OriginalPeriodEnd.Sub(activePause.OriginalPeriodStart)
			remainingDuration := adjustedEnd.Sub(now)
			if totalPeriodDuration > 0 {
				remainingRatio := float64(remainingDuration) / float64(totalPeriodDuration)
				impact.NextBillingAmount = decimal.NewFromFloat(100.00 * remainingRatio) // Placeholder value
			}
		} else {
			// For arrears billing, no immediate charge on resume
			impact.NextBillingAmount = decimal.Zero
		}
	} else {
		// Pause impact calculation

		// Calculate the current period adjustment (credit for unused time)
		if invoiceCadence == types.InvoiceCadenceAdvance {
			// For advance billing, calculate credit for unused portion
			totalPeriodDuration := sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart)
			unusedDuration := sub.CurrentPeriodEnd.Sub(pauseStart)
			if totalPeriodDuration > 0 {
				unusedRatio := float64(unusedDuration) / float64(totalPeriodDuration)
				// Negative value indicates a credit to the customer
				impact.PeriodAdjustmentAmount = decimal.NewFromFloat(-100.00 * unusedRatio) // Placeholder value
			}
		} else {
			// For arrears billing, calculate charge for used portion
			totalPeriodDuration := sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart)
			usedDuration := pauseStart.Sub(sub.CurrentPeriodStart)
			if totalPeriodDuration > 0 {
				usedRatio := float64(usedDuration) / float64(totalPeriodDuration)
				impact.PeriodAdjustmentAmount = decimal.NewFromFloat(100.00 * usedRatio) // Placeholder value
			}
		}

		// Calculate pause duration and next billing date
		if pauseEnd != nil {
			pauseDuration := pauseEnd.Sub(pauseStart)
			impact.PauseDurationDays = int(pauseDuration.Hours() / 24)
			impact.NextBillingDate = pauseEnd

			// Calculate adjusted period dates
			adjustedStart := pauseStart
			adjustedEnd := sub.CurrentPeriodEnd.Add(pauseDuration)
			impact.AdjustedPeriodStart = &adjustedStart
			impact.AdjustedPeriodEnd = &adjustedEnd
		} else {
			// For indefinite pauses, use a default of 30 days for estimation
			defaultPauseDays := 30
			impact.PauseDurationDays = defaultPauseDays
			estimatedEnd := pauseStart.AddDate(0, 0, defaultPauseDays)
			impact.NextBillingDate = &estimatedEnd

			// Calculate adjusted period dates
			adjustedStart := pauseStart
			adjustedEnd := sub.CurrentPeriodEnd.AddDate(0, 0, defaultPauseDays)
			impact.AdjustedPeriodStart = &adjustedStart
			impact.AdjustedPeriodEnd = &adjustedEnd
		}
	}

	return impact, nil
}

func (s *subscriptionService) publishInternalWebhookEvent(ctx context.Context, eventName string, subscriptionID string) {

	eventPayload := webhookDto.InternalSubscriptionEvent{
		SubscriptionID: subscriptionID,
		TenantID:       types.GetTenantID(ctx),
	}

	webhookPayload, err := json.Marshal(eventPayload)

	if err != nil {
		s.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}

// ProcessSubscriptionRenewalDueAlert processes subscriptions that are due for renewal in 24 hours
func (s *subscriptionService) ProcessSubscriptionRenewalDueAlert(ctx context.Context) error {
	subscriptions, err := s.SubRepo.ListSubscriptionsDueForRenewal(ctx)
	if err != nil {
		s.Logger.Errorw("failed to list subscriptions due for renewal", "error", err)
		return err
	}

	if len(subscriptions) == 0 {
		s.Logger.Infow("no subscriptions due for renewal found")
		return nil
	}

	s.Logger.Infow("found subscriptions due for renewal", "count", len(subscriptions))

	for _, sub := range subscriptions {
		ctx = context.WithValue(ctx, types.CtxTenantID, sub.TenantID)
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, sub.EnvironmentID)
		s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionRenewalDue, sub.ID)
	}

	return nil
}

// handleSubCoupons processes coupons for a subscription
// Converts deprecated Coupons and LineItemCoupons fields to SubscriptionCouponRequest format and applies them
func (s *subscriptionService) handleSubCoupons(
	ctx context.Context,
	sub *subscription.Subscription,
	req dto.CreateSubscriptionRequest,
	originalPriceToLineItemMap map[string]string,
) error {
	// Convert deprecated fields to SubscriptionCouponRequest format
	var subscriptionCoupons []dto.SubscriptionCouponRequest
	for _, couponID := range req.Coupons {
		if couponID != "" {
			subscriptionCoupons = append(subscriptionCoupons, dto.SubscriptionCouponRequest{
				CouponID:  couponID,
				StartDate: sub.StartDate,
			})
		}
	}

	// Process LineItemCoupons - use originalPriceToLineItemMap to convert priceID to lineItemID
	for priceID, couponIDs := range req.LineItemCoupons {
		for _, couponID := range couponIDs {
			if couponID != "" {
				// Get lineItemID from the original price mapping
				if lineItemID, exists := originalPriceToLineItemMap[priceID]; exists {
					subscriptionCoupons = append(subscriptionCoupons, dto.SubscriptionCouponRequest{
						CouponID:   couponID,
						LineItemID: lo.ToPtr(lineItemID),
						StartDate:  sub.StartDate,
					})
				} else {
					// Log warning but continue processing other coupons
					s.Logger.Warnw("coupon priceID not found in subscription, skipping",
						"price_id", priceID,
						"coupon_id", couponID,
						"subscription_id", sub.ID)
				}
			}
		}
	}

	if len(subscriptionCoupons) == 0 {
		return nil
	}

	s.Logger.Infow("handling subscription and line item coupon associations",
		"subscription_id", sub.ID,
		"coupon_count", len(subscriptionCoupons))

	couponAssociationService := NewCouponAssociationService(s.ServiceParams)
	err := couponAssociationService.ApplyCouponsToSubscription(ctx, sub, subscriptionCoupons)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to apply coupons to subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"coupon_count":    len(subscriptionCoupons),
			}).
			Mark(ierr.ErrInternal)
	}

	s.Logger.Infow("successfully applied all coupons to subscription",
		"subscription_id", sub.ID,
		"coupon_count", len(subscriptionCoupons))

	return nil
}

// handleSubscriptionAddons processes addons for a subscription
func (s *subscriptionService) handleSubscriptionAddons(
	ctx context.Context,
	subscription *subscription.Subscription,
	addonRequests []dto.AddAddonToSubscriptionRequest,
) error {
	if len(addonRequests) == 0 {
		return nil
	}

	s.Logger.Infow("processing addons for subscription",
		"subscription_id", subscription.ID,
		"addons_count", len(addonRequests))

	// Process each addon request
	for _, addonReq := range addonRequests {

		// check if start date is given else mark it as subscription start date
		if addonReq.StartDate == nil {
			addonReq.StartDate = &subscription.StartDate
		}

		_, err := s.addAddonToSubscription(ctx, subscription, lo.ToPtr(addonReq))
		if err != nil {
			return err
		}
	}

	return nil
}

// AddAddonToSubscription adds an addon to a subscription
// This is the public facing method for adding an addon to a subscription
func (s *subscriptionService) AddAddonToSubscription(
	ctx context.Context,
	subID string,
	req *dto.AddAddonToSubscriptionRequest,
) (*addonassociation.AddonAssociation, error) {

	sub, lineItems, err := s.SubRepo.GetWithLineItems(ctx, subID)
	if err != nil {
		return nil, err
	}
	sub.LineItems = lineItems

	return s.addAddonToSubscription(ctx, sub, req)
}

// addAddonToSubscription adds an addon to a subscription
func (s *subscriptionService) addAddonToSubscription(
	ctx context.Context,
	sub *subscription.Subscription,
	req *dto.AddAddonToSubscriptionRequest,
) (*addonassociation.AddonAssociation, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get addon via addon service to reuse validations
	addonService := NewAddonService(s.ServiceParams)
	a, err := addonService.GetAddon(ctx, req.AddonID)
	if err != nil {
		return nil, err
	}

	if a.Addon.Status != types.StatusPublished {
		return nil, ierr.NewError("addon is not published").
			WithHint("Cannot add inactive addon to subscription").
			Mark(ierr.ErrValidation)
	}

	// Check if sub exists and is active
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Cannot add addon to inactive subscription").
			Mark(ierr.ErrValidation)
	}

	// Check if addon is already active on this subscription
	activeAddons, err := addonService.GetActiveAddonAssociation(ctx, dto.GetActiveAddonAssociationRequest{
		EntityID:   sub.ID,
		EntityType: types.AddonAssociationEntityTypeSubscription,
		StartDate:  req.StartDate,
		AddonIds:   []string{req.AddonID},
	})
	if err != nil {
		return nil, err
	}

	if activeAddons != nil && len(activeAddons.Items) > 0 {
		return nil, ierr.NewError("addon is already added to subscription").
			WithHint("Cannot add addon to subscription that already has an active instance").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"addon_id":        req.AddonID,
				"active_addons":   activeAddons,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate entitlement compatibility if check is not skipped
	if !req.SkipEntityValidation {
		if err := s.validateEntitlementCompatibility(ctx, sub.ID, req.AddonID); err != nil {
			return nil, err
		}
	}

	// Validate and filter prices for the addon
	validPrices, err := s.ValidateAndFilterPricesForSubscription(ctx, req.AddonID, types.PRICE_ENTITY_TYPE_ADDON, sub, nil)
	if err != nil {
		return nil, err
	}

	// Create subscription addon association
	addonAssociation := req.ToAddonAssociation(
		ctx,
		sub.ID,
		types.AddonAssociationEntityTypeSubscription,
	)

	// Create line items for addon prices
	lineItems := make([]*subscription.SubscriptionLineItem, 0, len(validPrices))
	for _, priceResponse := range validPrices {
		lineItem := s.createLineItemFromPrice(ctx, priceResponse, sub, req.AddonID, a.Addon.Name)
		lineItems = append(lineItems, lineItem)
	}

	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Create subscription addon association
		err = s.AddonAssociationRepo.Create(ctx, addonAssociation)
		if err != nil {
			return err
		}

		// Create line items
		for _, lineItem := range lineItems {
			err = s.SubscriptionLineItemRepo.Create(ctx, lineItem)
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return addonAssociation, nil
}

// validateEntitlementCompatibility checks if addon entitlements are compatible with existing subscription entitlements
// It ensures that metered features with the same feature ID have the same usage reset period
func (s *subscriptionService) validateEntitlementCompatibility(ctx context.Context, subscriptionID, addonID string) error {
	// Get entitlements for the addon we're trying to add
	entitlementService := NewEntitlementService(s.ServiceParams)
	addonEntitlements, err := entitlementService.GetAddonEntitlements(ctx, addonID)
	if err != nil {
		return err
	}

	// Filter to metered features only (only metered features have usage reset periods that matter)
	meteredAddonEntitlements := make([]*dto.EntitlementResponse, 0)
	for _, addonEnt := range addonEntitlements.Items {
		if addonEnt.FeatureType == types.FeatureTypeMetered {
			meteredAddonEntitlements = append(meteredAddonEntitlements, addonEnt)
		}
	}

	// Early return if no metered entitlements to check
	if len(meteredAddonEntitlements) == 0 {
		return nil
	}

	// Fetch subscription entitlements
	subscriptionEntitlements, err := s.GetSubscriptionEntitlements(ctx, subscriptionID)
	if err != nil {
		return err
	}

	// Build map of feature_id to usage_reset_period for metered features in subscription
	featureResetMap := make(map[string]types.EntitlementUsageResetPeriod)
	for _, ent := range subscriptionEntitlements {
		if ent.FeatureType == types.FeatureTypeMetered {
			featureResetMap[ent.FeatureID] = ent.UsageResetPeriod
		}
	}

	// Check for conflicts
	for _, addonEnt := range meteredAddonEntitlements {

		existingResetPeriod, exists := featureResetMap[addonEnt.FeatureID]

		if exists && existingResetPeriod != addonEnt.UsageResetPeriod {

			return ierr.NewError("metered feature usage reset period conflict").
				WithHint(fmt.Sprintf("Feature '%s' has conflicting reset periods: %s vs %s", addonEnt.FeatureID, existingResetPeriod, addonEnt.UsageResetPeriod)).
				WithReportableDetails(map[string]interface{}{
					"subscription_id": subscriptionID,
					"addon_id":        addonID,
					"feature_id":      addonEnt.FeatureID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// RemoveAddonFromSubscription removes an addon from a subscription by addon association ID
func (s *subscriptionService) RemoveAddonFromSubscription(ctx context.Context, req *dto.RemoveAddonRequest) error {
	// Validate request
	if err := req.Validate(); err != nil {
		return err
	}

	// Get addon association
	association, err := s.AddonAssociationRepo.GetByID(ctx, req.AddonAssociationID)
	if err != nil {
		return err
	}

	// check if association already has end date i.e. scheduled to be removed
	if association.EndDate != nil {
		return ierr.NewError("addon is already scheduled to be removed").
			WithHint("This addon is already marked for removal").
			WithReportableDetails(map[string]interface{}{
				"addon_association_id": association.ID,
				"end_date":             association.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	// get cancel at date from subscription
	var effectiveEndDate *time.Time

	if association.EntityType == types.AddonAssociationEntityTypeSubscription {
		sub, err := s.SubRepo.Get(ctx, association.EntityID)
		if err != nil {
			return err
		}

		effectiveEndDate = lo.ToPtr(sub.CurrentPeriodEnd)
	}

	endReason := "Cancelled by API"
	if req.Reason != "" {
		endReason = req.Reason
	}

	association.CancellationReason = endReason
	association.CancelledAt = effectiveEndDate
	association.EndDate = effectiveEndDate

	// Get line items to terminate
	lineItemFilter := types.NewSubscriptionLineItemFilter()
	lineItemFilter.SubscriptionIDs = []string{association.EntityID}
	lineItemFilter.EntityIDs = []string{association.AddonID}
	lineItemFilter.EntityType = lo.ToPtr(types.SubscriptionLineItemEntityTypeAddon)

	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, lineItemFilter)
	if err != nil {
		return err
	}

	return s.DB.WithTx(ctx, func(ctx context.Context) error {
		if err := s.AddonAssociationRepo.Update(ctx, association); err != nil {
			return err
		}

		deleteReq := dto.DeleteSubscriptionLineItemRequest{EffectiveFrom: effectiveEndDate}
		for _, lineItem := range lineItems {
			if _, err := s.DeleteSubscriptionLineItem(ctx, lineItem.ID, deleteReq); err != nil {
				return err
			}
		}

		return nil
	})
}

// createLineItemFromPrice creates a subscription line item from a price for addon additions
func (s *subscriptionService) createLineItemFromPrice(ctx context.Context, priceResponse *dto.PriceResponse, sub *subscription.Subscription, addonID, addonName string) *subscription.SubscriptionLineItem {
	price := priceResponse.Price

	lineItem := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: sub.ID,
		CustomerID:     sub.CustomerID,
		EntityID:       addonID,
		EntityType:     types.SubscriptionLineItemEntityTypeAddon,
		PriceID:        price.ID,
		PriceType:      price.Type,
		Currency:       sub.Currency,
		BillingPeriod:  price.BillingPeriod,
		InvoiceCadence: price.InvoiceCadence,
		TrialPeriod:    0,
		StartDate:      time.Now(),
		EndDate:        time.Time{},
		Metadata: map[string]string{
			"addon_id":        addonID,
			"subscription_id": sub.ID,
			"addon_quantity":  "1",
			"addon_status":    string(types.AddonStatusActive),
		},
		EnvironmentID: sub.EnvironmentID,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	// Set display name from price (always use price display name)
	if price.DisplayName != "" {
		lineItem.DisplayName = price.DisplayName
	} else {
		// Fallback to addon name if price display name is not set
		lineItem.DisplayName = addonName
	}

	// Set price-related fields
	if price.Type == types.PRICE_TYPE_USAGE && price.MeterID != "" && priceResponse.Meter != nil {
		lineItem.MeterID = price.MeterID
		lineItem.MeterDisplayName = priceResponse.Meter.Name
		lineItem.Quantity = decimal.Zero
	} else {
		lineItem.Quantity = decimal.NewFromInt(1)
	}

	// Copy price unit fields from price to line item
	lineItem.PriceUnitID = price.PriceUnitID
	lineItem.PriceUnit = price.PriceUnit

	return lineItem
}

// ActivateIncompleteSubscription activates a subscription that is in incomplete status
// after the first invoice has been successfully paid
func (s *subscriptionService) ActivateIncompleteSubscription(ctx context.Context, subscriptionID string) error {
	s.Logger.Infow("activating incomplete subscription", "subscription_id", subscriptionID)

	// Get the subscription
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Check if subscription is in incomplete status
	if sub.SubscriptionStatus != types.SubscriptionStatusIncomplete {
		// If the subscription is not in incomplete status, do nothing
		return nil
	}

	// Update subscription status to active
	sub.SubscriptionStatus = types.SubscriptionStatusActive

	// Update the subscription in database
	err = s.SubRepo.Update(ctx, sub)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update subscription status").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("successfully activated incomplete subscription",
		"subscription_id", subscriptionID,
		"previous_status", types.SubscriptionStatusIncomplete,
		"new_status", types.SubscriptionStatusActive)

	// Process any pending credit grant applications for this subscription
	// This ensures credit grants are applied immediately when subscription becomes active
	// The cron job serves as a backup in case this fails
	err = s.processPendingCreditGrantsForSubscription(ctx, sub)
	if err != nil {
		// Log the error but don't fail the activation
		// The cron job will pick up these CGAs as a backup
		s.Logger.Errorw("failed to process pending credit grants during subscription activation",
			"subscription_id", subscriptionID,
			"error", err,
			"note", "cron job will process these as backup")
	}

	// Publish webhook event for subscription activation
	s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionActivated, subscriptionID)

	return nil
}

// processPendingCreditGrantsForSubscription finds and processes pending CGAs for a subscription
// This is called when a subscription becomes active to immediately apply deferred credit grants
func (s *subscriptionService) processPendingCreditGrantsForSubscription(ctx context.Context, sub *subscription.Subscription) error {
	// Get credit grant service
	creditGrantService := NewCreditGrantService(s.ServiceParams)

	// Find pending credit grant applications for this subscription
	filter := &types.CreditGrantApplicationFilter{
		SubscriptionIDs: []string{sub.ID},
		ApplicationStatuses: []types.ApplicationStatus{
			types.ApplicationStatusPending,
			types.ApplicationStatusFailed,
		},
		QueryFilter: types.NewNoLimitQueryFilter(),
	}

	applications, err := s.CreditGrantApplicationRepo.List(ctx, filter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get pending credit grant applications").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	if len(applications) == 0 {
		s.Logger.Infow("no pending credit grant applications found for subscription",
			"subscription_id", sub.ID)
		return nil
	}

	s.Logger.Infow("found pending credit grant applications to process",
		"subscription_id", sub.ID,
		"count", len(applications))

	// Process each application
	successCount := 0
	failureCount := 0
	for _, cga := range applications {
		// Get the credit grant
		creditGrant, err := creditGrantService.GetCreditGrant(ctx, cga.CreditGrantID)
		if err != nil {
			s.Logger.Errorw("failed to get credit grant for application",
				"application_id", cga.ID,
				"grant_id", cga.CreditGrantID,
				"error", err)
			failureCount++
			continue
		}

		// Check subscription state and determine action
		stateHandler := NewSubscriptionStateHandler(sub, creditGrant.CreditGrant)
		action, err := stateHandler.DetermineCreditGrantAction()
		if err != nil {
			s.Logger.Errorw("failed to determine credit grant action",
				"application_id", cga.ID,
				"grant_id", cga.CreditGrantID,
				"error", err)
			failureCount++
			continue
		}

		// Only apply if action is APPLY (subscription is now active)
		if action != StateActionApply {
			s.Logger.Infow("skipping credit grant application - action not APPLY",
				"application_id", cga.ID,
				"grant_id", cga.CreditGrantID,
				"action", action,
				"subscription_status", sub.SubscriptionStatus)
			continue
		}

		// Apply the credit grant to wallet
		err = creditGrantService.ProcessCreditGrantApplication(ctx, cga.ID)
		if err != nil {
			s.Logger.Errorw("failed to apply credit grant to wallet",
				"application_id", cga.ID,
				"grant_id", cga.CreditGrantID,
				"error", err)
			failureCount++
			continue
		}

		s.Logger.Infow("successfully applied credit grant during subscription activation",
			"application_id", cga.ID,
			"grant_id", cga.CreditGrantID,
			"subscription_id", sub.ID,
			"credits", cga.Credits)
		successCount++
	}

	s.Logger.Infow("completed processing pending credit grants",
		"subscription_id", sub.ID,
		"total", len(applications),
		"success", successCount,
		"failed", failureCount)

	if failureCount > 0 {
		return ierr.NewError("some credit grant applications failed to process").
			WithHint("Some credit grants could not be applied. The cron job will retry these.").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"total":           len(applications),
				"success":         successCount,
				"failed":          failureCount,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	return nil
}

// ProcessAutoCancellationSubscriptions processes subscriptions that are eligible for auto-cancellation
func (s *subscriptionService) ProcessAutoCancellationSubscriptions(ctx context.Context) error {
	s.Logger.Infow("starting auto-cancellation processing")

	// Get all tenant x environment combinations that have auto-cancellation enabled
	enabledConfigs, err := s.SettingsRepo.GetAllTenantEnvSubscriptionSettings(ctx)
	if err != nil {
		s.Logger.Errorw("failed to list subscription configs", "error", err)
		return err
	}

	if len(enabledConfigs) == 0 {
		s.Logger.Infow("no tenants have auto-cancellation enabled, skipping processing")
		return nil
	}

	s.Logger.Infow("found tenants with auto-cancellation enabled",
		"tenant_count", len(enabledConfigs))

	totalCanceledCount := 0
	totalFailedCount := 0

	// Process each tenant x environment combination
	for _, tenantConfig := range enabledConfigs {
		// Skip if auto-cancellation is not enabled
		if !tenantConfig.AutoCancellationEnabled {
			s.Logger.Debugw("auto-cancellation not enabled for tenant",
				"tenant_id", tenantConfig.TenantID,
				"environment_id", tenantConfig.EnvironmentID)
			continue
		}

		// Create a new context with tenant and environment IDs
		tenantCtx := context.WithValue(ctx, types.CtxTenantID, tenantConfig.TenantID)
		tenantCtx = context.WithValue(tenantCtx, types.CtxEnvironmentID, tenantConfig.EnvironmentID)

		s.Logger.Debugw("processing tenant",
			"tenant_id", tenantConfig.TenantID,
			"environment_id", tenantConfig.EnvironmentID,
			"grace_period_days", tenantConfig.GracePeriodDays)

		// Get all past due invoices for this tenant x environment
		invoicesFilter := &types.InvoiceFilter{
			InvoiceType:       types.InvoiceTypeSubscription,
			InvoiceStatus:     []types.InvoiceStatus{types.InvoiceStatusFinalized},
			PaymentStatus:     []types.PaymentStatus{types.PaymentStatusFailed, types.PaymentStatusPending},
			AmountRemainingGt: lo.ToPtr(decimal.NewFromInt(0)),
			SkipLineItems:     true,
			QueryFilter:       types.NewNoLimitQueryFilter(),
		}

		invoices, err := s.InvoiceRepo.List(tenantCtx, invoicesFilter)
		if err != nil {
			s.Logger.Errorw("failed to get invoices for tenant",
				"tenant_id", tenantConfig.TenantID,
				"environment_id", tenantConfig.EnvironmentID,
				"error", err)
			continue // Skip this tenant but continue with others
		}

		s.Logger.Debugw("found unpaid invoices for tenant",
			"tenant_id", tenantConfig.TenantID,
			"environment_id", tenantConfig.EnvironmentID,
			"invoice_count", len(invoices))

		// Filter invoices that are past grace period
		now := time.Now().UTC()
		eligibleInvoices := lo.Filter(invoices, func(inv *invoice.Invoice, _ int) bool {
			// Must have a subscription ID
			if inv.SubscriptionID == nil {
				return false
			}

			// Must have a valid due date
			if inv.DueDate == nil {
				s.Logger.Warnw("invoice has invalid due date, skipping",
					"invoice_id", inv.ID,
					"subscription_id", *inv.SubscriptionID)
				return false
			}

			// Calculate grace period end time: due_date + grace_period_days
			gracePeriodEndTime := inv.DueDate.AddDate(0, 0, tenantConfig.GracePeriodDays)

			// Check if current time is past grace period end
			isPastGracePeriod := now.After(gracePeriodEndTime)

			if isPastGracePeriod {
				s.Logger.Debugw("found invoice past grace period",
					"invoice_id", inv.ID,
					"subscription_id", *inv.SubscriptionID,
					"due_date", inv.DueDate,
					"grace_period_end_time", gracePeriodEndTime,
					"amount_remaining", inv.AmountRemaining,
					"current_time", now)
			}

			return isPastGracePeriod
		})

		// Extract unique subscription IDs from eligible invoices
		subscriptionIDs := lo.Uniq(lo.FilterMap(eligibleInvoices, func(inv *invoice.Invoice, _ int) (string, bool) {
			return lo.FromPtr(inv.SubscriptionID), inv.SubscriptionID != nil
		}))

		s.Logger.Debugw("found subscriptions with invoices past grace period",
			"tenant_id", tenantConfig.TenantID,
			"environment_id", tenantConfig.EnvironmentID,
			"total_invoices", len(invoices),
			"eligible_invoices", len(eligibleInvoices),
			"subscription_count", len(subscriptionIDs))

		if len(subscriptionIDs) == 0 {
			s.Logger.Debugw("no subscriptions eligible for auto-cancellation",
				"tenant_id", tenantConfig.TenantID,
				"environment_id", tenantConfig.EnvironmentID)
			continue
		}

		// Get ONLY ACTIVE subscriptions for this tenant x environment
		filter := &types.SubscriptionFilter{
			SubscriptionIDs:    subscriptionIDs,
			SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive},
		}

		subscriptions, err := s.SubRepo.List(tenantCtx, filter)
		if err != nil {
			s.Logger.Errorw("failed to get subscriptions for tenant",
				"tenant_id", tenantConfig.TenantID,
				"environment_id", tenantConfig.EnvironmentID,
				"error", err)
			continue // Skip this tenant but continue with others
		}

		s.Logger.Debugw("found active subscriptions to cancel",
			"tenant_id", tenantConfig.TenantID,
			"environment_id", tenantConfig.EnvironmentID,
			"subscription_count", len(subscriptions))

		canceledCount := 0
		failedCount := 0

		// Cancel all subscriptions - they've already been filtered for eligibility
		for _, sub := range subscriptions {
			s.Logger.Infow("auto-cancelling subscription",
				"subscription_id", sub.ID,
				"tenant_id", tenantConfig.TenantID,
				"environment_id", tenantConfig.EnvironmentID,
				"grace_period_days", tenantConfig.GracePeriodDays,
				"reason", "grace_period_expired",
			)

			// Cancel the subscription
			if _, err := s.CancelSubscription(tenantCtx, sub.ID, &dto.CancelSubscriptionRequest{
				CancellationType: types.CancellationTypeImmediate,
			}); err != nil {
				s.Logger.Errorw("failed to auto-cancel subscription",
					"subscription_id", sub.ID,
					"tenant_id", tenantConfig.TenantID,
					"environment_id", tenantConfig.EnvironmentID,
					"error", err)
				failedCount++
				continue
			}

			canceledCount++

			// Log audit trail
			s.Logger.Infow("successfully auto-canceled subscription",
				"subscription_id", sub.ID,
				"reason", "grace_period_expired",
				"grace_period_days", tenantConfig.GracePeriodDays,
				"canceled_by", "auto_cancellation_system",
				"tenant_id", tenantConfig.TenantID,
				"environment_id", tenantConfig.EnvironmentID)
		}

		s.Logger.Infow("completed processing for tenant",
			"tenant_id", tenantConfig.TenantID,
			"environment_id", tenantConfig.EnvironmentID,
			"total_subscriptions", len(subscriptions),
			"canceled_count", canceledCount,
			"failed_count", failedCount)

		totalCanceledCount += canceledCount
		totalFailedCount += failedCount
	}

	s.Logger.Infow("completed auto-cancellation processing for all tenants",
		"total_tenants_processed", len(enabledConfigs),
		"total_canceled", totalCanceledCount,
		"total_failed", totalFailedCount)

	return nil
}

// Helper functions for enhanced cancellation

// determineEffectiveDate calculates the actual effective date based on cancellation type
func (s *subscriptionService) determineEffectiveDate(
	subscription *subscription.Subscription,
	cancellationType types.CancellationType,
) (time.Time, error) {
	now := time.Now().UTC()

	switch cancellationType {
	case types.CancellationTypeImmediate:
		return now, nil

	case types.CancellationTypeEndOfPeriod:
		return subscription.CurrentPeriodEnd, nil
	default:
		return time.Time{}, ierr.NewError("invalid cancellation type").
			WithHintf("Unsupported cancellation type: %s", cancellationType).
			Mark(ierr.ErrValidation)
	}
}

// validateCancellationTiming ensures the cancellation timing is valid
func (s *subscriptionService) validateCancellationTiming(
	subscription *subscription.Subscription,
	cancellationType types.CancellationType,
	effectiveDate time.Time,
) error {
	switch cancellationType {
	case types.CancellationTypeImmediate:
		// Immediate cancellation should be within current period for proration
		if effectiveDate.After(subscription.CurrentPeriodEnd) {
			return ierr.NewError("immediate cancellation date is after current period end").
				WithHintf("Current period ends at %s, cancellation date is %s",
					subscription.CurrentPeriodEnd.Format("2006-01-02"),
					effectiveDate.Format("2006-01-02")).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// convertProrationResultToDetails converts SubscriptionProrationResult to response format
func (s *subscriptionService) convertProrationResultToDetails(
	result *proration.SubscriptionProrationResult,
) ([]dto.ProrationDetail, decimal.Decimal) {
	var prorationDetails []dto.ProrationDetail
	totalCreditAmount := decimal.Zero

	for lineItemID, lineResult := range result.LineItemResults {
		// Calculate amounts for this line item
		creditAmount := s.calculateCreditAmount(lineResult.CreditItems)
		chargeAmount := s.calculateChargeAmount(lineResult.ChargeItems)
		totalCreditAmount = totalCreditAmount.Add(creditAmount)

		// Calculate proration days
		prorationDays := s.calculateProrationDaysFromResult(lineResult)

		// Generate description
		description := s.generateProrationDescriptionFromResult(lineResult, creditAmount)

		// Get original amount from line items (we'll need to fetch this)
		originalAmount := decimal.Zero
		planName := ""
		priceID := ""

		// Extract from credit/charge items if available
		if len(lineResult.CreditItems) > 0 {
			priceID = lineResult.CreditItems[0].PriceID
			originalAmount = lineResult.CreditItems[0].Amount.Abs()
		} else if len(lineResult.ChargeItems) > 0 {
			priceID = lineResult.ChargeItems[0].PriceID
			originalAmount = lineResult.ChargeItems[0].Amount
		}

		prorationDetails = append(prorationDetails, dto.ProrationDetail{
			LineItemID:     lineItemID,
			PriceID:        priceID,
			PlanName:       planName, // TODO: Get from line item
			OriginalAmount: originalAmount,
			CreditAmount:   creditAmount,
			ChargeAmount:   chargeAmount,
			ProrationDays:  prorationDays,
			Description:    description,
		})
	}

	return prorationDetails, totalCreditAmount
}

// updateSubscriptionForCancellation updates the subscription with cancellation details
func (s *subscriptionService) updateSubscriptionForCancellation(
	ctx context.Context,
	subscription *subscription.Subscription,
	cancellationType types.CancellationType,
	effectiveDate time.Time,
	reason string,
) error {
	now := time.Now().UTC()

	// Update cancellation fields
	subscription.CancelledAt = &now

	// Add cancellation metadata
	if subscription.Metadata == nil {
		subscription.Metadata = make(map[string]string)
	}
	subscription.Metadata["cancellation_type"] = string(cancellationType)
	subscription.Metadata["cancellation_reason"] = reason
	subscription.Metadata["effective_date"] = effectiveDate.Format(time.RFC3339)

	// Set status and dates based on cancellation type
	switch cancellationType {
	case types.CancellationTypeImmediate:
		subscription.SubscriptionStatus = types.SubscriptionStatusCancelled
		subscription.CancelAt = &effectiveDate
		subscription.CancelAtPeriodEnd = false
		subscription.EndDate = &effectiveDate

	case types.CancellationTypeEndOfPeriod:
		// Don't change status immediately - will be cancelled at period end
		subscription.CancelAtPeriodEnd = true
		subscription.CancelAt = &effectiveDate
		subscription.EndDate = &effectiveDate
	default:
		return ierr.NewError("invalid cancellation type").
			WithHintf("Unsupported cancellation type: %s", cancellationType).
			Mark(ierr.ErrValidation)
	}

	// Update subscription in database
	err := s.SubRepo.Update(ctx, subscription)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update subscription with cancellation details").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// publishCancellationEvents publishes webhook events for cancellation
func (s *subscriptionService) publishCancellationEvents(
	ctx context.Context,
	subscription *subscription.Subscription,
) {
	// Publish standard subscription events
	s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionUpdated, subscription.ID)
	s.publishInternalWebhookEvent(ctx, types.WebhookEventSubscriptionCancelled, subscription.ID)

	s.Logger.Debugw("subscription cancellation events published",
		"subscription_id", subscription.ID)
}

// generateCancellationMessage creates a user-friendly message for the response
func (s *subscriptionService) generateCancellationMessage(
	cancellationType types.CancellationType,
	effectiveDate time.Time,
	totalCreditAmount decimal.Decimal,
) string {
	switch cancellationType {
	case types.CancellationTypeImmediate:
		if totalCreditAmount.IsNegative() {
			return fmt.Sprintf("Subscription cancelled immediately with %s credit for unused time",
				totalCreditAmount.Abs().String())
		}
		return "Subscription cancelled immediately"

	case types.CancellationTypeEndOfPeriod:
		return fmt.Sprintf("Subscription will be cancelled at the end of the current period (%s)",
			effectiveDate.Format("2006-01-02"))

	default:
		return "Subscription cancelled successfully"
	}
}

// Helper functions for proration calculations

func (s *subscriptionService) calculateCreditAmount(creditItems []proration.ProrationLineItem) decimal.Decimal {
	total := decimal.Zero
	for _, item := range creditItems {
		if item.IsCredit {
			total = total.Add(item.Amount.Abs())
		}
	}
	return total
}

func (s *subscriptionService) calculateChargeAmount(chargeItems []proration.ProrationLineItem) decimal.Decimal {
	total := decimal.Zero
	for _, item := range chargeItems {
		if !item.IsCredit {
			total = total.Add(item.Amount)
		}
	}
	return total
}

func (s *subscriptionService) calculateProrationDaysFromResult(result *proration.ProrationResult) int {
	if result.ProrationDate.After(result.CurrentPeriodEnd) {
		return 0
	}

	totalDays := int(result.CurrentPeriodEnd.Sub(result.CurrentPeriodStart).Hours() / 24)
	usedDays := int(result.ProrationDate.Sub(result.CurrentPeriodStart).Hours() / 24)
	remainingDays := totalDays - usedDays

	if remainingDays < 0 {
		return 0
	}
	return remainingDays
}

func (s *subscriptionService) generateProrationDescriptionFromResult(
	result *proration.ProrationResult,
	creditAmount decimal.Decimal,
) string {
	effectiveDate := result.ProrationDate

	switch result.Action {
	case types.ProrationActionCancellation:
		if creditAmount.IsNegative() {
			return fmt.Sprintf("Credit for unused time (cancelled %s)", effectiveDate.Format("2006-01-02"))
		}
		return fmt.Sprintf("Cancellation (%s)", effectiveDate.Format("2006-01-02"))
	default:
		return fmt.Sprintf("Proration (%s)", effectiveDate.Format("2006-01-02"))
	}
}

func (s *subscriptionService) GetFeatureUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error) {
	response := &dto.GetUsageBySubscriptionResponse{}
	priceService := NewPriceService(s.ServiceParams)

	// Get subscription with line items
	subscription, lineItems, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	customer, err := s.CustomerRepo.Get(ctx, subscription.CustomerID)
	if err != nil {
		return nil, err
	}

	usageStartTime := req.StartTime
	if usageStartTime.IsZero() {
		usageStartTime = subscription.CurrentPeriodStart
	}

	// TODO: Handle line item level end time - use the earliest end time among all line items
	usageEndTime := req.EndTime
	if usageEndTime.IsZero() {
		usageEndTime = subscription.CurrentPeriodEnd
	}

	if req.LifetimeUsage {
		usageStartTime = time.Time{}
		usageEndTime = time.Now().UTC()
	}

	// Collect all price IDs and build meter to price mapping
	priceIDs := make([]string, 0, len(lineItems))

	for _, item := range lineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}
		if item.MeterID == "" {
			continue
		}
		priceIDs = append(priceIDs, item.PriceID)
	}

	// Fetch all prices in one call
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.PriceIDs = priceIDs
	priceFilter.Expand = lo.ToPtr(string(types.ExpandMeters))
	priceFilter.AllowExpiredPrices = true
	pricesList, err := priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	// Build price map for quick lookup
	priceMap := make(map[string]*price.Price, len(pricesList.Items))
	meterMap := make(map[string]*dto.MeterResponse, len(pricesList.Items))
	meterDisplayNames := make(map[string]string)

	for _, p := range pricesList.Items {
		priceMap[p.ID] = p.Price
		meterMap[p.Price.MeterID] = p.Meter
		if p.Meter != nil {
			meterDisplayNames[p.Price.MeterID] = p.Meter.Name
		}
	}

	s.Logger.Debugw("calculating usage for subscription V2",
		"subscription_id", req.SubscriptionID,
		"start_time", usageStartTime,
		"end_time", usageEndTime,
		"metered_line_items", len(priceIDs))

	// Use the optimized single query
	usageResults, err := s.FeatureUsageRepo.GetFeatureUsageBySubscription(ctx, req.SubscriptionID, customer.ExternalID, usageStartTime, usageEndTime)
	if err != nil {
		return nil, err
	}

	s.Logger.Debugw("fetched usage for features using V2 query",
		"feature_ids", lo.Keys(usageResults),
		"total_usage_count", len(usageResults),
		"subscription_id", req.SubscriptionID)

	// Store usage charges for later sorting and processing
	var usageCharges []*dto.SubscriptionUsageByMetersResponse
	totalCost := decimal.Zero

	// Track which line items have been processed
	processedLineItems := make(map[string]bool)

	// Process each feature result - now we have meter_id directly from ClickHouse
	for subLineItemID, usageResult := range usageResults {
		meterID := usageResult.MeterID
		if meterID == "" {
			s.Logger.Warnw("meter_id not found in usage result, skipping",
				"sub_line_item_id", subLineItemID,
				"subscription_id", req.SubscriptionID)
			continue
		}

		priceID := usageResult.PriceID

		priceObj, priceExists := priceMap[priceID]
		if !priceExists || priceObj == nil {
			s.Logger.Warnw("price object not found, skipping",
				"price_id", priceID,
				"subscription_id", req.SubscriptionID)
			continue
		}

		meter := meterMap[meterID]
		if meter == nil {
			s.Logger.Warnw("meter not found, skipping",
				"sub_line_item_id", subLineItemID,
				"meter_id", meterID,
				"subscription_id", req.SubscriptionID)
			continue
		}

		// Calculate quantity based on meter aggregation type
		var quantity decimal.Decimal
		switch meter.Aggregation.Type {
		case types.AggregationSum, types.AggregationSumWithMultiplier, types.AggregationWeightedSum:
			quantity = usageResult.SumTotal
		case types.AggregationMax:
			quantity = usageResult.MaxTotal
		case types.AggregationCount:
			quantity = decimal.NewFromInt(int64(usageResult.CountDistinctIDs))
		case types.AggregationCountUnique:
			quantity = decimal.NewFromInt(int64(usageResult.CountUniqueQty))
		case types.AggregationLatest:
			quantity = usageResult.LatestQty
		default:
			quantity = usageResult.SumTotal // Default to sum
		}

		// Calculate cost using the price service
		cost := priceService.CalculateCost(ctx, priceObj, quantity)
		totalCost = totalCost.Add(cost)

		// Create charge response
		charge := &dto.SubscriptionUsageByMetersResponse{
			Amount:           cost.InexactFloat64(),
			Currency:         priceObj.Currency,
			DisplayAmount:    fmt.Sprintf("%.2f %s", cost.InexactFloat64(), priceObj.Currency),
			Quantity:         quantity.InexactFloat64(),
			FilterValues:     make(price.JSONBFilters),
			MeterID:          meterID,
			MeterDisplayName: meterDisplayNames[meterID],
			Price:            priceObj,
			IsOverage:        false,
		}

		// Add filter values from meter
		for _, filter := range meter.Filters {
			charge.FilterValues[filter.Key] = filter.Values
		}

		usageCharges = append(usageCharges, charge)
		processedLineItems[subLineItemID] = true
	}

	// Add zero-quantity, zero-cost charges for line items not found in usage results
	for _, item := range lineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}
		if item.MeterID == "" {
			continue
		}

		// Skip if this line item was already processed
		if processedLineItems[item.ID] {
			continue
		}

		priceObj, priceExists := priceMap[item.PriceID]
		if !priceExists || priceObj == nil {
			s.Logger.Warnw("price object not found for line item, skipping zero charge",
				"line_item_id", item.ID,
				"price_id", item.PriceID,
				"subscription_id", req.SubscriptionID)
			continue
		}

		meter := meterMap[item.MeterID]
		if meter == nil {
			s.Logger.Warnw("meter not found for line item, skipping zero charge",
				"line_item_id", item.ID,
				"meter_id", item.MeterID,
				"subscription_id", req.SubscriptionID)
			continue
		}

		// Create zero-quantity, zero-cost charge for this line item
		charge := &dto.SubscriptionUsageByMetersResponse{
			Amount:           0.0,
			Currency:         priceObj.Currency,
			DisplayAmount:    fmt.Sprintf("0.00 %s", priceObj.Currency),
			Quantity:         0.0,
			FilterValues:     make(price.JSONBFilters),
			MeterID:          item.MeterID,
			MeterDisplayName: meterDisplayNames[item.MeterID],
			Price:            priceObj,
			IsOverage:        false,
		}

		// Add filter values from meter
		for _, filter := range meter.Filters {
			charge.FilterValues[filter.Key] = filter.Values
		}

		usageCharges = append(usageCharges, charge)
	}

	// Apply commitment-based overage logic if configured
	commitmentAmount := lo.FromPtr(subscription.CommitmentAmount)
	overageFactor := lo.FromPtr(subscription.OverageFactor)
	hasCommitment := commitmentAmount.GreaterThan(decimal.Zero) && overageFactor.GreaterThan(decimal.NewFromInt(1))

	// Default values assuming no commitment/overage
	commitmentFloat, _ := commitmentAmount.Float64()
	overageFactorFloat, _ := overageFactor.Float64()
	response.CommitmentAmount = commitmentFloat
	response.OverageFactor = overageFactorFloat
	response.HasOverage = false

	// Initialize charges list with enough capacity for potential overage splits
	finalCharges := make([]*dto.SubscriptionUsageByMetersResponse, 0, len(usageCharges)*2)

	// If using commitment-based pricing, process charges with overage logic
	if hasCommitment {
		// First, filter charges to only include usage-based charges for commitment calculations
		// Fixed charges are not subject to commitment/overage
		var usageOnlyCharges []*dto.SubscriptionUsageByMetersResponse
		var fixedCharges []*dto.SubscriptionUsageByMetersResponse

		for _, charge := range usageCharges {
			if charge.Price != nil && charge.Price.Type == types.PRICE_TYPE_USAGE {
				usageOnlyCharges = append(usageOnlyCharges, charge)
			} else {
				// Add fixed charges directly to the response without overage calculation
				fixedCharges = append(fixedCharges, charge)
			}
		}

		// Add all fixed charges directly to the response
		finalCharges = append(finalCharges, fixedCharges...)

		// Track remaining commitment and process each usage charge
		remainingCommitment := commitmentAmount
		totalOverageAmount := decimal.Zero

		for _, charge := range usageOnlyCharges {
			// Get charge amount as decimal for precise calculations
			chargeAmount := decimal.NewFromFloat(charge.Amount)
			pricePerUnit := decimal.Zero
			if charge.Price != nil && charge.Price.BillingModel == types.BILLING_MODEL_FLAT_FEE {
				pricePerUnit = charge.Price.Amount
			} else if charge.Quantity > 0 {
				pricePerUnit = chargeAmount.Div(decimal.NewFromFloat(charge.Quantity))
			}

			// Normal price covers all of this charge
			if remainingCommitment.GreaterThanOrEqual(chargeAmount) {
				charge.IsOverage = false
				remainingCommitment = remainingCommitment.Sub(chargeAmount)
				finalCharges = append(finalCharges, charge)
				continue
			}

			// Charge needs to be split between normal and overage
			if remainingCommitment.GreaterThan(decimal.Zero) {
				// Calculate exact quantity that can be covered by remaining commitment
				var normalQuantityDecimal decimal.Decimal

				if !pricePerUnit.IsZero() {
					normalQuantityDecimal = remainingCommitment.Div(pricePerUnit)
					// Round down to ensure we don't exceed commitment
					normalQuantityDecimal = normalQuantityDecimal.Floor()
				}

				// Calculate the normal amount based on the normal quantity
				normalAmountDecimal := normalQuantityDecimal.Mul(pricePerUnit)

				// Create the normal charge
				if normalQuantityDecimal.GreaterThan(decimal.Zero) {
					normalCharge := *charge // Create a copy
					normalCharge.Quantity = normalQuantityDecimal.InexactFloat64()
					normalCharge.Amount = price.FormatAmountToFloat64WithPrecision(normalAmountDecimal, subscription.Currency)
					normalCharge.DisplayAmount = price.FormatAmountToStringWithPrecision(normalAmountDecimal, subscription.Currency)
					normalCharge.IsOverage = false
					finalCharges = append(finalCharges, &normalCharge)
				}

				// Calculate overage quantity and amount
				overageQuantityDecimal := decimal.NewFromFloat(charge.Quantity).Sub(normalQuantityDecimal)

				// Create the overage charge only if there's actual overage
				if overageQuantityDecimal.GreaterThan(decimal.Zero) {
					overageAmountDecimal := overageQuantityDecimal.Mul(pricePerUnit).Mul(overageFactor)
					totalOverageAmount = totalOverageAmount.Add(overageAmountDecimal)

					overageCharge := *charge // Create a copy
					overageCharge.Quantity = overageQuantityDecimal.InexactFloat64()
					overageCharge.Amount = price.FormatAmountToFloat64WithPrecision(overageAmountDecimal, subscription.Currency)
					overageCharge.DisplayAmount = price.GetDisplayAmountWithPrecision(overageAmountDecimal, subscription.Currency)
					overageCharge.IsOverage = true
					overageCharge.OverageFactor = overageFactorFloat
					finalCharges = append(finalCharges, &overageCharge)
					response.HasOverage = true
				}

				// Update remaining commitment (should be zero or very close to it due to rounding)
				remainingCommitment = remainingCommitment.Sub(normalAmountDecimal)
				continue
			}

			// Charge is entirely in overage
			overageAmountDecimal := chargeAmount.Mul(overageFactor)
			totalOverageAmount = totalOverageAmount.Add(overageAmountDecimal)

			charge.Amount = price.FormatAmountToFloat64WithPrecision(overageAmountDecimal, subscription.Currency)
			charge.DisplayAmount = overageAmountDecimal.StringFixed(6)
			charge.IsOverage = true
			charge.OverageFactor = overageFactorFloat
			finalCharges = append(finalCharges, charge)
			response.HasOverage = true
		}

		// Calculate final amounts for response
		commitmentUtilized := commitmentAmount.Sub(remainingCommitment)
		commitmentUtilizedFloat, _ := commitmentUtilized.Float64()
		overageAmountFloat, _ := totalOverageAmount.Float64()
		response.CommitmentUtilized = commitmentUtilizedFloat
		response.OverageAmount = overageAmountFloat

		// Update total cost with commitment + overage calculation
		totalCost = commitmentUtilized.Add(totalOverageAmount)
	} else {
		// Without commitment, just use the original charges
		finalCharges = usageCharges
	}

	// Sort charges by meter display name for consistent ordering
	sort.Slice(finalCharges, func(i, j int) bool {
		return finalCharges[i].MeterDisplayName < finalCharges[j].MeterDisplayName
	})

	// Build response
	response.Amount = price.FormatAmountToFloat64WithPrecision(totalCost, subscription.Currency)
	response.Currency = subscription.Currency
	response.DisplayAmount = price.GetDisplayAmountWithPrecision(totalCost, subscription.Currency)
	response.StartTime = usageStartTime
	response.EndTime = usageEndTime
	response.Charges = finalCharges

	s.Logger.Infow("subscription usage calculation completed V2",
		"subscription_id", req.SubscriptionID,
		"total_cost", totalCost.InexactFloat64(),
		"charge_count", len(finalCharges),
		"currency", response.Currency)

	return response, nil
}

// GetSubscriptionEntitlements retrieves all entitlements associated with a subscription
// This includes entitlements from:
// 1. The subscription's plan
// 2. Active addon associations (one-time addons counted once, multiple addons counted per occurrence)
// 3. Subscription-scoped entitlement overrides
// Note: If a plan/addon entitlement has been overridden at the subscription level,
// only the subscription-scoped override is returned, not the original.
func (s *subscriptionService) GetSubscriptionEntitlements(ctx context.Context, subscriptionID string) ([]*dto.EntitlementResponse, error) {
	// Get the subscription
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Initialize entitlement service
	entitlementService := NewEntitlementService(s.ServiceParams)

	// Step 1: Get plan entitlements
	planEntitlements, err := entitlementService.GetPlanEntitlements(ctx, sub.PlanID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get plan entitlements").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"plan_id":         sub.PlanID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Step 2: Get active addon associations using current period start
	addonService := NewAddonService(s.ServiceParams)
	activeAddons, err := addonService.GetActiveAddonAssociation(ctx, dto.GetActiveAddonAssociationRequest{
		EntityID:   subscriptionID,
		EntityType: types.AddonAssociationEntityTypeSubscription,
		StartDate:  &sub.CurrentPeriodStart,
		EndDate:    &sub.CurrentPeriodEnd,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get active addon associations").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Step 3: Extract unique addon IDs
	addonIDs := lo.Uniq(lo.Map(activeAddons.Items, func(assoc *dto.AddonAssociationResponse, _ int) string {
		if assoc != nil {
			return assoc.AddonID
		}
		return ""
	}))

	// Step 4: Fetch addon entitlements if any addons exist
	var addonEntitlements []*dto.EntitlementResponse
	if len(addonIDs) > 0 {
		// Create filter for bulk fetching addon entitlements
		addonEntFilter := types.NewNoLimitEntitlementFilter().
			WithEntityIDs(addonIDs).
			WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_ADDON).
			WithStatus(types.StatusPublished).
			WithExpand(fmt.Sprintf("%s,%s", types.ExpandFeatures, types.ExpandMeters))

		addonEntResp, err := entitlementService.ListEntitlements(ctx, addonEntFilter)
		if err != nil {
			return nil, err
		}
		addonEntitlements = addonEntResp.Items
	}

	// Step 5: Fetch subscription-scoped entitlement overrides
	subscriptionEntFilter := types.NewNoLimitEntitlementFilter().
		WithEntityIDs([]string{subscriptionID}).
		WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION).
		WithStatus(types.StatusPublished).
		WithExpand(fmt.Sprintf("%s,%s,%s", types.ExpandFeatures, types.ExpandMeters, types.ExpandAddons))

	subscriptionEntResp, err := entitlementService.ListEntitlements(ctx, subscriptionEntFilter)
	if err != nil {
		return nil, err
	}
	subscriptionEntitlements := subscriptionEntResp.Items

	// Step 6: Filter out overridden entitlements and combine results
	finalEntitlements := s.filterOverriddenEntitlements(
		planEntitlements.Items,
		addonEntitlements,
		subscriptionEntitlements,
		subscriptionID,
	)

	return finalEntitlements, nil
}

// filterOverriddenEntitlements removes plan/addon entitlements that have been overridden
// by subscription-scoped entitlements and returns the combined final list
func (s *subscriptionService) filterOverriddenEntitlements(
	planEntitlements []*dto.EntitlementResponse,
	addonEntitlements []*dto.EntitlementResponse,
	subscriptionEntitlements []*dto.EntitlementResponse,
	subscriptionID string,
) []*dto.EntitlementResponse {
	// Build a map of parent_entitlement_id -> true for quick lookup
	// Only include subscription entitlements that are currently active (time-based check)
	s.Logger.Infow("total plan entitlements", "count", len(planEntitlements))
	s.Logger.Infow("total addon entitlements", "count", len(addonEntitlements))
	s.Logger.Infow("total subscription entitlements", "count", len(subscriptionEntitlements))

	now := time.Now().UTC()
	overriddenIDs := make(map[string]bool)
	activeSubEntitlements := make([]*dto.EntitlementResponse, 0, len(subscriptionEntitlements))

	for _, subEnt := range subscriptionEntitlements {
		// Check if this subscription entitlement is currently active
		isActive := true

		// Check start_date: must be <= now (or NULL)
		if subEnt.StartDate != nil && subEnt.StartDate.After(now) {
			isActive = false
			s.Logger.Debugw("subscription entitlement not yet active",
				"entitlement_id", subEnt.ID,
				"start_date", subEnt.StartDate,
				"now", now)
		}

		// Check end_date: must be > now (or NULL)
		if isActive && subEnt.EndDate != nil && !subEnt.EndDate.After(now) {
			isActive = false
			s.Logger.Debugw("subscription entitlement expired",
				"entitlement_id", subEnt.ID,
				"end_date", subEnt.EndDate,
				"now", now)
		}

		// Only use active subscription entitlements for overriding
		if isActive {
			activeSubEntitlements = append(activeSubEntitlements, subEnt)
			if subEnt.ParentEntitlementID != nil && *subEnt.ParentEntitlementID != "" {
				overriddenIDs[*subEnt.ParentEntitlementID] = true
			}
		} else {
			s.Logger.Infow("skipping inactive subscription entitlement, will use plan entitlement instead",
				"entitlement_id", subEnt.ID,
				"parent_entitlement_id", subEnt.ParentEntitlementID,
				"start_date", subEnt.StartDate,
				"end_date", subEnt.EndDate)
		}
	}

	// Replace subscriptionEntitlements with only active ones
	subscriptionEntitlements = activeSubEntitlements

	// If no overrides exist, just combine all entitlements
	if len(overriddenIDs) == 0 {
		allEntitlements := make([]*dto.EntitlementResponse, 0, len(planEntitlements)+len(addonEntitlements)+len(subscriptionEntitlements))
		allEntitlements = append(allEntitlements, planEntitlements...)
		allEntitlements = append(allEntitlements, addonEntitlements...)
		allEntitlements = append(allEntitlements, subscriptionEntitlements...)
		return allEntitlements
	}

	// Filter plan entitlements - exclude overridden ones
	filteredPlanEnts := make([]*dto.EntitlementResponse, 0, len(planEntitlements))
	planOverrideCount := 0
	for _, planEnt := range planEntitlements {
		if !overriddenIDs[planEnt.ID] {
			filteredPlanEnts = append(filteredPlanEnts, planEnt)
		} else {
			planOverrideCount++
		}
	}

	// Filter addon entitlements - exclude overridden ones
	filteredAddonEnts := make([]*dto.EntitlementResponse, 0, len(addonEntitlements))
	addonOverrideCount := 0
	for _, addonEnt := range addonEntitlements {
		if !overriddenIDs[addonEnt.ID] {
			filteredAddonEnts = append(filteredAddonEnts, addonEnt)
		} else {
			addonOverrideCount++
		}
	}

	// Log override statistics
	if planOverrideCount > 0 || addonOverrideCount > 0 {
		s.Logger.Infow("filtered overridden entitlements",
			"subscription_id", subscriptionID,
			"plan_overrides", planOverrideCount,
			"addon_overrides", addonOverrideCount,
			"total_subscription_entitlements", len(subscriptionEntitlements))
	}

	// Combine filtered plan entitlements, filtered addon entitlements, and all subscription overrides
	finalEntitlements := make([]*dto.EntitlementResponse, 0,
		len(filteredPlanEnts)+len(filteredAddonEnts)+len(subscriptionEntitlements))
	finalEntitlements = append(finalEntitlements, filteredPlanEnts...)
	finalEntitlements = append(finalEntitlements, filteredAddonEnts...)
	finalEntitlements = append(finalEntitlements, subscriptionEntitlements...)

	return finalEntitlements
}

// GetAggregatedSubscriptionEntitlements retrieves and aggregates all entitlements for a subscription
// and returns them in a structured response format with aggregated features
func (s *subscriptionService) GetAggregatedSubscriptionEntitlements(ctx context.Context, subscriptionID string, req *dto.GetSubscriptionEntitlementsRequest) (*dto.SubscriptionEntitlementsResponse, error) {
	// Validate request if provided
	if req != nil {
		if err := req.Validate(); err != nil {
			return nil, err
		}
	} else {
		// Initialize with empty request if none provided
		req = &dto.GetSubscriptionEntitlementsRequest{}
	}

	// Get the subscription
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Get all entitlements for the subscription
	entitlements, err := s.GetSubscriptionEntitlements(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Filter by feature IDs if specified
	if len(req.FeatureIDs) > 0 {
		filteredEntitlements := make([]*dto.EntitlementResponse, 0)
		for _, ent := range entitlements {
			if lo.Contains(req.FeatureIDs, ent.FeatureID) {
				filteredEntitlements = append(filteredEntitlements, ent)
			}
		}
		entitlements = filteredEntitlements
	}

	// Use the generic aggregation function from billing service
	billingService := NewBillingService(s.ServiceParams)
	aggregatedFeatures := billingService.AggregateEntitlements(entitlements, subscriptionID)

	// Ensure subscription ID is set in all sources
	for _, feature := range aggregatedFeatures {
		for _, source := range feature.Sources {
			if source.SubscriptionID == "" {
				source.SubscriptionID = subscriptionID
			}
		}
	}

	// Build final response
	response := &dto.SubscriptionEntitlementsResponse{
		SubscriptionID: subscriptionID,
		PlanID:         sub.PlanID,
		Features:       aggregatedFeatures,
	}

	return response, nil
}

// ProcessSubscriptionEntitlementOverrides creates subscription-scoped entitlement overrides
// Only plan entitlements can be overridden, not addon entitlements
func (s *subscriptionService) ProcessSubscriptionEntitlementOverrides(
	ctx context.Context,
	sub *subscription.Subscription,
	overrideRequests []dto.OverrideEntitlementRequest,
) error {
	if len(overrideRequests) == 0 {
		return nil
	}

	s.Logger.Infow("processing entitlement overrides",
		"subscription_id", sub.ID,
		"override_count", len(overrideRequests))

	// Get plan entitlements to validate and copy from
	planEntitlements, err := s.EntitlementRepo.ListByPlanIDs(ctx, []string{sub.PlanID})
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch plan entitlements").
			Mark(ierr.ErrDatabase)
	}

	// Create a map for quick lookup
	entitlementMap := make(map[string]*entitlement.Entitlement)
	for _, ent := range planEntitlements {
		entitlementMap[ent.ID] = ent
	}

	// Validate that ONLY plan entitlements are being overridden (no addon entitlements)
	for _, override := range overrideRequests {
		// Validate the override request
		if err := override.Validate(); err != nil {
			return err
		}

		// Check if the entitlement exists in plan
		parentEnt, existsInPlan := entitlementMap[override.EntitlementID]

		if !existsInPlan {
			// The entitlement is not in the plan, check if it might be an addon entitlement
			// by fetching the entitlement directly to give a better error message
			checkEnt, err := s.EntitlementRepo.Get(ctx, override.EntitlementID)
			if err == nil && checkEnt != nil {
				// Entitlement exists but is not a plan entitlement
				if checkEnt.EntityType == types.ENTITLEMENT_ENTITY_TYPE_ADDON {
					return ierr.NewError("only plan entitlements can be overridden").
						WithHint("Addon entitlements cannot be overridden at subscription level. Only plan entitlements can be overridden.").
						WithReportableDetails(map[string]interface{}{
							"entitlement_id": override.EntitlementID,
							"entity_type":    checkEnt.EntityType,
							"entity_id":      checkEnt.EntityID,
							"plan_id":        sub.PlanID,
						}).
						Mark(ierr.ErrValidation)
				}
			}

			// Either entitlement doesn't exist or belongs to a different plan
			return ierr.NewError("entitlement not found").
				WithHint(fmt.Sprintf("Entitlement %s does not belong to plan %s", override.EntitlementID, sub.PlanID)).
				WithReportableDetails(map[string]interface{}{
					"entitlement_id":  override.EntitlementID,
					"subscription_id": sub.ID,
					"plan_id":         sub.PlanID,
				}).
				Mark(ierr.ErrNotFound)
		}

		// Double-check entity type (should always be PLAN at this point, but defensive programming)
		if parentEnt.EntityType != types.ENTITLEMENT_ENTITY_TYPE_PLAN {
			return ierr.NewError("only plan entitlements can be overridden").
				WithHint("Only plan entitlements can be overridden at subscription level.").
				WithReportableDetails(map[string]interface{}{
					"entitlement_id": override.EntitlementID,
					"entity_type":    parentEnt.EntityType,
					"plan_id":        sub.PlanID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Process each override request
	for _, override := range overrideRequests {
		// Get the parent entitlement (already validated above)
		parentEnt := entitlementMap[override.EntitlementID]

		// Create subscription-scoped entitlement with overrides
		newEnt := &entitlement.Entitlement{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT),
			EntityType:          types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
			EntityID:            sub.ID,
			FeatureID:           parentEnt.FeatureID,
			FeatureType:         parentEnt.FeatureType,
			UsageResetPeriod:    parentEnt.UsageResetPeriod,
			IsSoftLimit:         parentEnt.IsSoftLimit,
			DisplayOrder:        parentEnt.DisplayOrder,
			ParentEntitlementID: &parentEnt.ID,
			StartDate:           &sub.StartDate, // Set start date to subscription start
			EndDate:             nil,            // No end date - persists across billing periods
			EnvironmentID:       parentEnt.EnvironmentID,
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}

		// Apply overrides - ONLY these 3 fields can be overridden
		// Filter based on feature type since for metered features, nil is also a valid value
		switch parentEnt.FeatureType {
		case types.FeatureTypeMetered:
			// For metered features, UsageLimit can be overridden (including nil for unlimited)
			// Simply use whatever value is provided (even if nil)
			newEnt.UsageLimit = override.UsageLimit
		default:
			// For non-metered features, UsageLimit is not relevant, leave as nil
			newEnt.UsageLimit = nil
		}

		if override.IsEnabled != nil {
			newEnt.IsEnabled = *override.IsEnabled
		} else {
			newEnt.IsEnabled = parentEnt.IsEnabled
		}

		if override.StaticValue != nil {
			newEnt.StaticValue = *override.StaticValue
		} else {
			newEnt.StaticValue = parentEnt.StaticValue
		}

		// Validate based on feature type
		switch parentEnt.FeatureType {
		case types.FeatureTypeMetered:
			// For metered features, usage_limit and is_enabled are relevant
			if override.StaticValue != nil {
				return ierr.NewError("static_value cannot be set for metered features").
					WithHint("Only usage_limit and is_enabled can be overridden for metered features").
					Mark(ierr.ErrValidation)
			}
			// Ensure UsageResetPeriod is set for metered features
			// If parent has empty reset period, default to MONTHLY
			if newEnt.UsageResetPeriod == "" {
				newEnt.UsageResetPeriod = types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY
				s.Logger.Warnw("subscription entitlement override: parent entitlement had empty usage_reset_period, defaulting to MONTHLY",
					"subscription_id", sub.ID,
					"parent_entitlement_id", parentEnt.ID,
					"feature_id", parentEnt.FeatureID)
			}
		case types.FeatureTypeStatic:
			// For static features, static_value is required
			if newEnt.StaticValue == "" {
				return ierr.NewError("static_value is required for static features").
					WithHint("Please provide static_value for this feature type").
					Mark(ierr.ErrValidation)
			}
			if override.UsageLimit != nil {
				return ierr.NewError("usage_limit cannot be set for static features").
					WithHint("Only static_value and is_enabled can be overridden for static features").
					Mark(ierr.ErrValidation)
			}
		}

		// Create the subscription-scoped entitlement
		_, err := s.EntitlementRepo.Create(ctx, newEnt)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create subscription entitlement override").
				WithReportableDetails(map[string]interface{}{
					"subscription_id":       sub.ID,
					"parent_entitlement_id": parentEnt.ID,
					"feature_id":            parentEnt.FeatureID,
				}).
				Mark(ierr.ErrDatabase)
		}

		s.Logger.Infow("created subscription-scoped entitlement override",
			"subscription_id", sub.ID,
			"entitlement_id", newEnt.ID,
			"parent_entitlement_id", parentEnt.ID,
			"feature_id", parentEnt.FeatureID,
			"usage_limit_override", override.UsageLimit != nil,
			"is_enabled_override", override.IsEnabled != nil,
			"static_value_override", override.StaticValue != nil)
	}

	return nil
}

func (s *subscriptionService) ListAllTenantSubscriptions(ctx context.Context, filter *types.SubscriptionFilter) (*dto.ListSubscriptionsResponse, error) {
	if filter == nil {
		filter = types.NewNoLimitSubscriptionFilter()
	}
	subs, err := s.SubRepo.ListAllTenant(ctx, filter)
	if err != nil {
		return nil, err
	}
	response := &dto.ListSubscriptionsResponse{
		Items: make([]*dto.SubscriptionResponse, len(subs)),
	}
	for i, sub := range subs {
		response.Items[i] = &dto.SubscriptionResponse{
			Subscription: sub,
		}
	}
	return response, nil
}
func (s *subscriptionService) GetUpcomingCreditGrantApplications(ctx context.Context, req *dto.GetUpcomingCreditGrantApplicationsRequest) (*dto.ListCreditGrantApplicationsResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Verify each subscription exists
	subFilter := types.NewNoLimitSubscriptionFilter()
	subFilter.SubscriptionIDs = req.SubscriptionIDs
	subscriptions, err := s.SubRepo.List(ctx, subFilter)
	if err != nil {
		return nil, err
	}

	subscriptionIDToSubscriptionMap := make(map[string]*subscription.Subscription, len(subscriptions))
	for _, sub := range subscriptions {
		subscriptionIDToSubscriptionMap[sub.ID] = sub
	}

	for _, subscriptionID := range req.SubscriptionIDs {
		if _, exists := subscriptionIDToSubscriptionMap[subscriptionID]; !exists {
			return nil, ierr.NewError("subscription not found").
				WithHint("Please verify the subscription ID is correct").
				WithReportableDetails(map[string]interface{}{
					"subscription_id":  subscriptionID,
					"subscription_ids": req.SubscriptionIDs,
				}).
				Mark(ierr.ErrNotFound)
		}
	}

	// Get credit grant service
	creditGrantService := NewCreditGrantService(s.ServiceParams)

	// Create filter for upcoming grants
	// Include pending and failed statuses (failed ones can be retried)
	now := time.Now().UTC()
	filter := types.NewNoLimitCreditGrantApplicationFilter()
	filter.SubscriptionIDs = req.SubscriptionIDs
	filter.ApplicationStatuses = []types.ApplicationStatus{
		types.ApplicationStatusPending,
		types.ApplicationStatusFailed,
	}

	// Get all applications for the specified subscriptions with pending/failed status
	response, err := creditGrantService.ListCreditGrantApplications(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list credit grant applications").
			WithReportableDetails(map[string]interface{}{
				"subscription_ids": req.SubscriptionIDs,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Filter to only include upcoming grants (scheduled_for > now)
	upcomingItems := make([]*dto.CreditGrantApplicationResponse, 0)
	for _, item := range response.Items {
		if item.CreditGrantApplication != nil && item.ScheduledFor.After(now) {
			upcomingItems = append(upcomingItems, item)
		}
	}

	// Sort by scheduled_for date (ascending - earliest first)
	sort.Slice(upcomingItems, func(i, j int) bool {
		return upcomingItems[i].ScheduledFor.Before(upcomingItems[j].ScheduledFor)
	})

	// Update response with filtered items
	response.Items = upcomingItems
	response.Pagination.Total = len(upcomingItems)

	return response, nil
}

// ListByCustomerID retrieves all active subscriptions for a customer
// This method returns subscriptions with Active or Trialing status and includes line items
func (s *subscriptionService) ListByCustomerID(ctx context.Context, customerID string) ([]*subscription.Subscription, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer ID is required").
			WithHint("Please provide a valid customer ID").
			Mark(ierr.ErrValidation)
	}

	subscriptions, err := s.SubRepo.ListByCustomerID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	return subscriptions, nil
}

func (s *subscriptionService) GetActiveAddonAssociations(ctx context.Context, subscriptionID string) (*dto.ListAddonAssociationsResponse, error) {
	addonService := NewAddonService(s.ServiceParams)
	associations, err := addonService.GetActiveAddonAssociation(ctx, dto.GetActiveAddonAssociationRequest{
		EntityID:   subscriptionID,
		EntityType: types.AddonAssociationEntityTypeSubscription,
	})
	if err != nil {
		return nil, err
	}
	return associations, nil
}

// Calculate Billing Periods
func (s *subscriptionService) CalculateBillingPeriods(ctx context.Context, subscriptionID string) ([]dto.Period, error) {
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	currentStart := sub.CurrentPeriodStart
	currentEnd := sub.CurrentPeriodEnd

	periods := make([]dto.Period, 0)

	periods = append(periods, dto.Period{
		Start: currentStart,
		End:   currentEnd,
	})

	now := time.Now().UTC()

	for currentEnd.Before(now) {
		nextStart := currentEnd
		nextEnd, err := types.NextBillingDate(nextStart, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod, sub.EndDate)
		if err != nil {
			return nil, err
		}
		periods = append(periods, dto.Period{
			Start: nextStart,
			End:   nextEnd,
		})

		if sub.CancelAtPeriodEnd && sub.CancelAt != nil && !sub.CancelAt.After(nextEnd) {
			s.Logger.Infow("subscription cancelled at period end",
				"subscription_id", sub.ID,
				"cancel_at", sub.CancelAt,
				"next_end", nextEnd)
			break
		}

		// in case of end date reached or next end is equal to current end, we break the loop
		// nextEnd will be equal to currentEnd in case of end date reached
		if nextEnd.Equal(currentEnd) {
			s.Logger.Infow("stopped period generation - reached subscription end date",
				"subscription_id", sub.ID,
				"end_date", sub.EndDate,
				"final_period_end", currentEnd)
			break
		}

		currentEnd = nextEnd
	}

	return periods, nil
}

// Create Draft Invoice for Subscription
func (s *subscriptionService) CreateDraftInvoiceForSubscription(ctx context.Context, subscriptionID string, period dto.Period) (*dto.InvoiceResponse, error) {
	sub, _, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	billingService := NewBillingService(s.ServiceParams)
	invoiceService := NewInvoiceService(s.ServiceParams)

	// Prepare Invoice Request
	invoiceReq, err := billingService.PrepareSubscriptionInvoiceRequest(
		ctx,
		sub,
		period.Start,
		period.End,
		types.ReferencePointPeriodEnd,
	)
	if err != nil {
		return nil, err
	}
	// Check if the invoice is zeroAmountInvoice
	if invoiceReq.Subtotal.IsZero() {
		return nil, nil
	}

	// Create Invoice
	inv, err := invoiceService.CreateInvoice(ctx, *invoiceReq)
	if err != nil {
		return nil, err
	}

	return inv, nil
}
