package hubspot

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// DealSyncService handles synchronization of subscription data with HubSpot deals
type DealSyncService struct {
	client           HubSpotClient
	customerRepo     customer.Repository
	subscriptionRepo subscription.Repository
	priceRepo        price.Repository
	logger           *logger.Logger
}

// NewDealSyncService creates a new HubSpot deal sync service
func NewDealSyncService(
	client HubSpotClient,
	customerRepo customer.Repository,
	subscriptionRepo subscription.Repository,
	priceRepo price.Repository,
	logger *logger.Logger,
) *DealSyncService {
	return &DealSyncService{
		client:           client,
		customerRepo:     customerRepo,
		subscriptionRepo: subscriptionRepo,
		priceRepo:        priceRepo,
		logger:           logger,
	}
}

// SyncSubscriptionToDeal creates HubSpot line items from subscription and associates them with a deal
func (s *DealSyncService) SyncSubscriptionToDeal(ctx context.Context, subscriptionID string) error {
	s.logger.Infow("fetching subscription for deal sync",
		"subscription_id", subscriptionID)

	// Fetch subscription with line items
	sub, lineItems, err := s.subscriptionRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		s.logger.Errorw("failed to fetch subscription with line items",
			"error", err,
			"subscription_id", subscriptionID)
		return ierr.WithError(err).
			WithHint("Failed to fetch subscription").
			Mark(ierr.ErrInternal)
	}

	// Assign line items to the subscription
	sub.LineItems = lineItems

	s.logger.Infow("fetching customer for deal sync",
		"customer_id", sub.CustomerID,
		"subscription_id", subscriptionID,
		"line_items_count", len(lineItems))

	// Fetch customer to get deal ID from metadata
	cust, err := s.customerRepo.Get(ctx, sub.CustomerID)
	if err != nil {
		s.logger.Errorw("failed to fetch customer",
			"error", err,
			"customer_id", sub.CustomerID,
			"subscription_id", subscriptionID)
		return ierr.WithError(err).
			WithHint("Failed to fetch customer").
			Mark(ierr.ErrInternal)
	}

	// Get deal ID from customer metadata
	dealID, ok := cust.Metadata["hubspot_deal_id"]
	if !ok || dealID == "" {
		s.logger.Warnw("no HubSpot deal ID found in customer metadata",
			"customer_id", cust.ID,
			"subscription_id", subscriptionID,
			"metadata", cust.Metadata)
		return nil // Not an error - customer might not be from HubSpot
	}

	s.logger.Infow("found HubSpot deal ID, creating line items",
		"deal_id", dealID,
		"subscription_id", subscriptionID,
		"line_items_count", len(sub.LineItems))

	// Filter for ACTIVE and FIXED pricing only (flat rate)
	var flatRateLineItems []*subscription.SubscriptionLineItem
	now := time.Now()
	for _, lineItem := range sub.LineItems {
		if lineItem.PriceType == types.PRICE_TYPE_FIXED && lineItem.IsActive(now) {
			flatRateLineItems = append(flatRateLineItems, lineItem)
		}
	}

	if len(flatRateLineItems) == 0 {
		s.logger.Warnw("no active flat rate line items to sync",
			"subscription_id", subscriptionID,
			"deal_id", dealID,
			"line_items_count", len(sub.LineItems))
		return nil
	}

	// Create HubSpot line items for each flat rate subscription line item
	for _, lineItem := range flatRateLineItems {
		if err := s.createHubSpotLineItem(ctx, lineItem, sub, dealID); err != nil {
			s.logger.Errorw("failed to create HubSpot line item",
				"error", err,
				"line_item_id", lineItem.ID,
				"deal_id", dealID)
			// Continue with other line items even if one fails
			continue
		}
	}

	s.logger.Infow("successfully synced subscription line items to HubSpot deal",
		"subscription_id", subscriptionID,
		"deal_id", dealID,
		"synced_items", len(flatRateLineItems))

	return nil
}

// UpdateDealAmountFromACV updates the deal amount based on HubSpot's calculated ACV
// This should be called after line items are created and HubSpot has recalculated ACV
func (s *DealSyncService) UpdateDealAmountFromACV(ctx context.Context, customerID, dealID string) error {
	s.logger.Infow("updating deal amount from ACV",
		"customer_id", customerID,
		"deal_id", dealID)

	// Update deal amount based on ACV - just fetch and update, don't calculate
	if err := s.updateDealAmountFromHubSpot(ctx, dealID); err != nil {
		s.logger.Errorw("failed to update deal amount",
			"error", err,
			"deal_id", dealID,
			"customer_id", customerID)
		return err
	}

	return nil
}

// createHubSpotLineItem creates a single HubSpot line item and associates it with a deal
func (s *DealSyncService) createHubSpotLineItem(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	sub *subscription.Subscription,
	dealID string,
) error {
	// Fetch the price to get the actual amount
	priceObj, err := s.priceRepo.Get(ctx, lineItem.PriceID)
	if err != nil {
		s.logger.Errorw("failed to fetch price for line item; cannot create accurate HubSpot line item",
			"error", err,
			"price_id", lineItem.PriceID,
			"line_item_id", lineItem.ID,
			"deal_id", dealID)
		return ierr.WithError(err).
			WithHint("Price not found; cannot create accurate HubSpot line item").
			Mark(ierr.ErrInternal)
	}

	// Calculate unit price and total amount
	unitPrice := priceObj.Amount
	totalAmount := unitPrice.Mul(lineItem.Quantity)

	// Calculate discount (default to 0 if not applicable)
	discount := "0"

	// Map billing frequency to HubSpot format
	billingFreq := s.mapBillingFrequency(sub.BillingPeriod)

	// Build description with pricing model
	description := string(lineItem.PriceType) + " pricing"
	if lineItem.DisplayName != "" {
		description = lineItem.DisplayName + " (" + string(lineItem.PriceType) + " pricing)"
	}

	// Create line item request with all fields
	lineItemReq := &DealLineItemCreateRequest{
		Properties: DealLineItemProperties{
			Name:                 lineItem.DisplayName,
			Price:                unitPrice.String(),         // Unit price
			Quantity:             lineItem.Quantity.String(), // Quantity
			Amount:               totalAmount.String(),       // Total amount
			Discount:             discount,                   // Discount
			RecurringBillingFreq: billingFreq,                // Billing frequency
			Description:          description,                // Pricing model in description
		},
		Associations: []LineItemAssociation{
			{
				To: AssociationTarget{
					ID: dealID,
				},
				Types: []AssociationType{
					{
						AssociationCategory: string(AssociationCategoryHubSpotDefined),
						AssociationTypeID:   AssociationTypeLineItemToDeal,
					},
				},
			},
		},
	}

	s.logger.Infow("creating HubSpot line item",
		"deal_id", dealID,
		"line_item_name", lineItem.DisplayName,
		"quantity", lineItem.Quantity.String(),
		"unit_price", unitPrice.String(),
		"total_amount", totalAmount.String(),
		"discount", discount,
		"billing_frequency", billingFreq,
		"pricing_model", lineItem.PriceType)

	// Create the line item in HubSpot
	_, err = s.client.CreateDealLineItem(ctx, lineItemReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create HubSpot line item").
			Mark(ierr.ErrHTTPClient)
	}

	return nil
}

// mapBillingFrequency converts FlexPrice billing period to HubSpot billing frequency
func (s *DealSyncService) mapBillingFrequency(period types.BillingPeriod) string {
	switch period {
	case types.BILLING_PERIOD_MONTHLY:
		return "monthly"
	case types.BILLING_PERIOD_ANNUAL:
		return "annually"
	case types.BILLING_PERIOD_WEEKLY:
		return "weekly"
	case types.BILLING_PERIOD_QUARTER:
		return "quarterly"
	default:
		return string(period)
	}
}

// updateDealAmountFromHubSpot fetches the deal's ACV from HubSpot and updates the deal amount
// This function only reads ACV calculated by HubSpot, never calculates manually
func (s *DealSyncService) updateDealAmountFromHubSpot(ctx context.Context, dealID string) error {
	s.logger.Infow("fetching deal to get ACV",
		"deal_id", dealID)

	// Get the deal to read its ACV
	deal, err := s.client.GetDeal(ctx, dealID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch deal from HubSpot").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Infow("fetched deal properties",
		"deal_id", dealID,
		"acv", deal.Properties.ACV,
		"mrr", deal.Properties.MRR,
		"arr", deal.Properties.ARR)

	// Extract ACV from deal properties (already a string)
	acv := deal.Properties.ACV
	if acv == "" {
		s.logger.Warnw("hs_acv property not found or empty",
			"deal_id", dealID,
			"deal_name", deal.Properties.DealName,
			"current_amount", deal.Properties.Amount)
		return ierr.NewError("ACV not found in HubSpot deal").
			WithHint("HubSpot has not calculated ACV yet or line items were not synced").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Infow("updating deal amount with ACV",
		"deal_id", dealID,
		"acv", acv)

	// Update the deal amount
	updateProps := map[string]string{
		"amount": acv,
	}

	_, err = s.client.UpdateDeal(ctx, dealID, updateProps)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update deal amount").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Infow("successfully updated deal amount",
		"deal_id", dealID,
		"amount", acv)

	return nil
}
