package hubspot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// QuoteSyncService handles synchronization of subscription data with HubSpot quotes
type QuoteSyncService struct {
	client           HubSpotClient
	customerRepo     customer.Repository
	subscriptionRepo subscription.Repository
	priceRepo        price.Repository
	logger           *logger.Logger
}

// NewQuoteSyncService creates a new HubSpot quote sync service
func NewQuoteSyncService(
	client HubSpotClient,
	customerRepo customer.Repository,
	subscriptionRepo subscription.Repository,
	priceRepo price.Repository,
	logger *logger.Logger,
) *QuoteSyncService {
	return &QuoteSyncService{
		client:           client,
		customerRepo:     customerRepo,
		subscriptionRepo: subscriptionRepo,
		priceRepo:        priceRepo,
		logger:           logger,
	}
}

// SyncSubscriptionToQuote creates HubSpot quote from subscription and associates line items with it
func (s *QuoteSyncService) SyncSubscriptionToQuote(ctx context.Context, subscriptionID string) error {
	s.logger.Infow("fetching subscription for quote sync",
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

	s.logger.Infow("fetching customer for quote sync",
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

	// Get contact ID from customer metadata (optional - for quote association)
	contactID := cust.Metadata["hubspot_contact_id"]

	s.logger.Infow("found HubSpot deal ID, creating quote",
		"deal_id", dealID,
		"contact_id", contactID,
		"subscription_id", subscriptionID,
		"line_items_count", len(sub.LineItems))

	// Create quote title
	quoteTitle := fmt.Sprintf("Quote for Subscription %s", subscriptionID)

	// Set expiration date to 30 days from now
	// Per HubSpot docs: use date format "YYYY-MM-DD" (not RFC3339 timestamp)
	expirationDate := time.Now().Add(30 * 24 * time.Hour).Format("2006-01-02")

	// Step 1: Create minimal quote (title + expiry only)
	// Per HubSpot docs: Create quote with only required properties first
	// Do NOT set hs_status or hs_esign_enabled during creation
	quoteReq := &QuoteCreateRequest{
		Properties: QuoteProperties{
			Title:          quoteTitle,
			ExpirationDate: expirationDate,
			// Note: hs_status and hs_esign_enabled will be set after associations
		},
	}

	quote, err := s.client.CreateQuote(ctx, quoteReq)
	if err != nil {
		s.logger.Errorw("failed to create quote in HubSpot",
			"error", err,
			"subscription_id", subscriptionID,
			"deal_id", dealID)
		return ierr.WithError(err).
			WithHint("Failed to create quote in HubSpot").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Infow("created quote in HubSpot",
		"quote_id", quote.ID,
		"subscription_id", subscriptionID,
		"deal_id", dealID)

	// Associate quote with deal (REQUIRED for publishable quotes per HubSpot docs)
	if err := s.client.AssociateQuoteToDeal(ctx, quote.ID, dealID); err != nil {
		s.logger.Errorw("failed to associate quote with deal",
			"error", err,
			"quote_id", quote.ID,
			"deal_id", dealID,
			"subscription_id", subscriptionID)
		// Don't fail the entire sync if association fails - quote was created successfully
		// Log warning and continue
		s.logger.Warnw("quote created but association with deal failed",
			"quote_id", quote.ID,
			"deal_id", dealID)
	} else {
		s.logger.Infow("associated quote with deal",
			"quote_id", quote.ID,
			"deal_id", dealID)
	}

	// Associate quote with contact (if contact ID is available)
	if contactID != "" {
		if err := s.client.AssociateQuoteToContact(ctx, quote.ID, contactID); err != nil {
			s.logger.Warnw("failed to associate quote with contact",
				"error", err,
				"quote_id", quote.ID,
				"contact_id", contactID,
				"subscription_id", subscriptionID)
			// Don't fail - contact association is optional
		} else {
			s.logger.Infow("associated quote with contact",
				"quote_id", quote.ID,
				"contact_id", contactID)
		}
	} else {
		s.logger.Debugw("no HubSpot contact ID found in customer metadata, skipping contact association",
			"customer_id", cust.ID,
			"subscription_id", subscriptionID)
	}

	// Associate quote with quote template (REQUIRED for publishable quotes per HubSpot docs)
	// Per docs: "For a quote to be publishable, it must have an associated deal and quote template"
	templates, err := s.client.GetQuoteTemplates(ctx)
	if err != nil {
		s.logger.Warnw("failed to fetch quote templates - quote will not be publishable without template",
			"error", err,
			"quote_id", quote.ID,
			"subscription_id", subscriptionID)
	} else if len(templates) == 0 {
		s.logger.Warnw("no quote templates found in HubSpot - quote will not be publishable without template",
			"quote_id", quote.ID,
			"subscription_id", subscriptionID)
	} else {
		// Use the first available template (typically there's a default template)
		templateID := templates[0].ID
		if err := s.client.AssociateQuoteToTemplate(ctx, quote.ID, templateID); err != nil {
			s.logger.Warnw("failed to associate quote with template - quote will not be publishable",
				"error", err,
				"quote_id", quote.ID,
				"template_id", templateID,
				"subscription_id", subscriptionID)
		} else {
			s.logger.Infow("associated quote with template",
				"quote_id", quote.ID,
				"template_id", templateID)
		}
	}

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
			"quote_id", quote.ID,
			"line_items_count", len(sub.LineItems))
		return nil
	}

	// Create HubSpot line items for each flat rate subscription line item
	for _, lineItem := range flatRateLineItems {
		if err := s.createHubSpotQuoteLineItem(ctx, lineItem, sub, quote.ID); err != nil {
			s.logger.Errorw("failed to create HubSpot quote line item",
				"error", err,
				"line_item_id", lineItem.ID,
				"quote_id", quote.ID)
			// Continue with other line items even if one fails
			continue
		}
	}

	s.logger.Infow("successfully synced subscription line items to HubSpot quote",
		"subscription_id", subscriptionID,
		"quote_id", quote.ID,
		"synced_items", len(flatRateLineItems))

	// Step 3: Set quote to DRAFT state to make it editable in HubSpot
	// Per HubSpot docs: hs_status must be set or quote cannot be edited
	// Per HubSpot docs line 384: DRAFT enables editing in HubSpot
	if err := s.client.UpdateQuote(ctx, quote.ID, QuoteProperties{
		Status: "DRAFT",
	}); err != nil {
		s.logger.Warnw("failed to set quote to DRAFT state",
			"error", err,
			"quote_id", quote.ID,
			"subscription_id", subscriptionID)
		// Continue - quote exists but may not be editable via API
	} else {
		s.logger.Infow("set quote to DRAFT state - now editable in HubSpot",
			"quote_id", quote.ID,
			"subscription_id", subscriptionID)
	}

	return nil
}

// createHubSpotQuoteLineItem creates a single HubSpot line item and associates it with a quote
func (s *QuoteSyncService) createHubSpotQuoteLineItem(
	ctx context.Context,
	lineItem *subscription.SubscriptionLineItem,
	sub *subscription.Subscription,
	quoteID string,
) error {
	// Fetch the price to get the actual amount
	priceObj, err := s.priceRepo.Get(ctx, lineItem.PriceID)
	if err != nil {
		s.logger.Errorw("failed to fetch price for line item; cannot create accurate HubSpot line item",
			"error", err,
			"price_id", lineItem.PriceID,
			"line_item_id", lineItem.ID,
			"quote_id", quoteID)
		return ierr.WithError(err).
			WithHint("Price not found; cannot create accurate HubSpot line item").
			Mark(ierr.ErrInternal)
	}

	// Calculate unit price and total amount
	unitPrice := priceObj.Amount
	totalAmount := unitPrice.Mul(lineItem.Quantity)

	// Map billing frequency to HubSpot format (same as deal sync)
	billingFreq := s.mapBillingFrequency(sub.BillingPeriod)

	// Build description with pricing model
	description := string(lineItem.PriceType) + " pricing"
	if lineItem.DisplayName != "" {
		description = lineItem.DisplayName + " (" + string(lineItem.PriceType) + " pricing)"
	}

	// Create line item request (without associations - we'll associate separately like invoices)
	lineItemReq := &LineItemCreateRequest{
		Properties: LineItemProperties{
			Name:                 lineItem.DisplayName,
			Price:                unitPrice.String(),         // Unit price
			Quantity:             lineItem.Quantity.String(), // Quantity
			Amount:               totalAmount.String(),       // Total amount
			RecurringBillingFreq: billingFreq,                // Billing frequency (monthly, annually, etc.)
			Description:          description,                // Pricing model in description
		},
	}

	// Log the request for debugging
	reqJSON, _ := json.Marshal(lineItemReq)
	s.logger.Infow("creating HubSpot line item for quote",
		"quote_id", quoteID,
		"line_item_name", lineItem.DisplayName,
		"quantity", lineItem.Quantity.String(),
		"unit_price", unitPrice.String(),
		"total_amount", totalAmount.String(),
		"billing_frequency", billingFreq,
		"pricing_model", lineItem.PriceType,
		"request_body", string(reqJSON))

	// Create the line item in HubSpot (without associations)
	hubspotLineItem, err := s.client.CreateLineItem(ctx, lineItemReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create HubSpot line item").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Infow("created HubSpot line item, associating with quote",
		"line_item_id", hubspotLineItem.ID,
		"quote_id", quoteID)

	// Associate line item to quote (similar to how invoices do it)
	if err := s.client.AssociateLineItemToQuote(ctx, hubspotLineItem.ID, quoteID); err != nil {
		return ierr.WithError(err).
			WithHint(fmt.Sprintf("Failed to associate line item %s to quote", hubspotLineItem.ID)).
			Mark(ierr.ErrHTTPClient)
	}

	return nil
}

// mapBillingFrequency converts FlexPrice billing period to HubSpot billing frequency
func (s *QuoteSyncService) mapBillingFrequency(period types.BillingPeriod) string {
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
