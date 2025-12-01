package webhook

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/hubspot"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
)

// Handler handles HubSpot webhook events
type Handler struct {
	client         hubspot.HubSpotClient
	customerSvc    hubspot.HubSpotCustomerService
	connectionRepo connection.Repository
	logger         *logger.Logger
}

// NewHandler creates a new HubSpot webhook handler
func NewHandler(
	client hubspot.HubSpotClient,
	customerSvc hubspot.HubSpotCustomerService,
	connectionRepo connection.Repository,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		client:         client,
		customerSvc:    customerSvc,
		connectionRepo: connectionRepo,
		logger:         logger,
	}
}

// ServiceDependencies contains all service dependencies needed by webhook handlers
type ServiceDependencies = interfaces.ServiceDependencies

// HandleWebhookEvent processes a HubSpot webhook event
func (h *Handler) HandleWebhookEvent(
	ctx context.Context,
	events []hubspot.WebhookEvent,
	environmentID string,
	services *ServiceDependencies,
) error {
	h.logger.Infow("processing HubSpot webhook events",
		"event_count", len(events),
		"environment_id", environmentID)

	for _, event := range events {
		h.logger.Infow("processing HubSpot webhook event",
			"subscription_type", event.SubscriptionType,
			"object_id", event.ObjectID,
			"property_name", event.PropertyName,
			"property_value", event.PropertyValue,
			"environment_id", environmentID)

		// Process deal.creation events
		if event.SubscriptionType == string(hubspot.SubscriptionTypeDealCreation) {
			// Process the deal creation (convert ObjectID to string)
			dealIDStr := strconv.FormatInt(event.ObjectID, 10)
			if err := h.handleDealCreated(ctx, dealIDStr, services); err != nil {
				h.logger.Errorw("failed to handle deal created event",
					"error", err,
					"deal_id", event.ObjectID)
				// Continue processing other events even if one fails
				continue
			}
			continue
		}

		// Process deal.propertyChange events
		if event.SubscriptionType == string(hubspot.SubscriptionTypeDealPropertyChange) {
		// Only process dealstage property changes to "closedwon"
		if event.PropertyName != hubspot.PropertyNameDealStage || event.PropertyValue != string(hubspot.DealStageClosedWon) {
			h.logger.Infow("skipping event - not a closed won deal",
				"property_name", event.PropertyName,
				"property_value", event.PropertyValue)
			continue
		}

		// Process the closed won deal (convert ObjectID to string)
		dealIDStr := strconv.FormatInt(event.ObjectID, 10)
		if err := h.handleDealClosedWon(ctx, dealIDStr, services); err != nil {
			h.logger.Errorw("failed to handle deal closed won event",
				"error", err,
				"deal_id", event.ObjectID)
			// Continue processing other events even if one fails
			continue
		}
			continue
		}

		// Skip unsupported event types
		h.logger.Infow("skipping unsupported event type", "subscription_type", event.SubscriptionType)
	}

	return nil
}

// processDealContacts is a shared function that processes a deal by fetching contacts and creating customers
func (h *Handler) processDealContacts(
	ctx context.Context,
	dealID string,
	services *ServiceDependencies,
) error {
	// Step 1: Fetch deal details from HubSpot
	deal, err := h.client.GetDeal(ctx, dealID)
	if err != nil {
		h.logger.Errorw("failed to fetch deal from HubSpot",
			"error", err,
			"deal_id", dealID)
		return ierr.WithError(err).
			WithHint("Failed to fetch deal from HubSpot").
			Mark(ierr.ErrHTTPClient)
	}

	h.logger.Infow("fetched deal from HubSpot",
		"deal_id", deal.ID)

	// Step 2: Fetch associated contacts for the deal
	associations, err := h.client.GetDealAssociations(ctx, dealID)
	if err != nil {
		h.logger.Errorw("failed to fetch deal associations from HubSpot",
			"error", err,
			"deal_id", dealID)
		return ierr.WithError(err).
			WithHint("Failed to fetch deal associations").
			Mark(ierr.ErrHTTPClient)
	}

	if len(associations.Results) == 0 {
		h.logger.Warnw("no contacts associated with deal", "deal_id", dealID)
		return nil
	}

	h.logger.Infow("found associated contacts",
		"deal_id", dealID,
		"contact_count", len(associations.Results))

	// Step 3: Fetch and create customers for each associated contact
	for _, assoc := range associations.Results {
		contactID := assoc.ID

		// Fetch contact details
		contact, err := h.client.GetContact(ctx, contactID)
		if err != nil {
			h.logger.Errorw("failed to fetch contact from HubSpot",
				"error", err,
				"contact_id", contactID,
				"deal_id", dealID)
			// Continue with other contacts even if one fails
			continue
		}

		h.logger.Infow("fetched contact from HubSpot",
			"contact_id", contact.ID,
			"deal_id", dealID)

		// Create customer in FlexPrice
		if err := h.customerSvc.CreateCustomerFromHubSpot(ctx, contact, dealID, services.CustomerService); err != nil {
			h.logger.Errorw("failed to create customer from HubSpot contact",
				"error", err,
				"contact_id", contactID,
				"deal_id", dealID)
			// Continue with other contacts even if one fails
			continue
		}

		h.logger.Infow("successfully created customer from HubSpot contact",
			"contact_id", contactID,
			"deal_id", dealID)
	}

	return nil
}

// handleDealClosedWon processes a deal that was marked as closed won
func (h *Handler) handleDealClosedWon(
	ctx context.Context,
	dealID string,
	services *ServiceDependencies,
) error {
	h.logger.Infow("handling deal closed won event", "deal_id", dealID)

	if err := h.processDealContacts(ctx, dealID, services); err != nil {
		return err
	}

	h.logger.Infow("successfully processed deal closed won event", "deal_id", dealID)
	return nil
}

// handleDealCreated processes a deal that was created
func (h *Handler) handleDealCreated(
	ctx context.Context,
	dealID string,
	services *ServiceDependencies,
) error {
	h.logger.Infow("handling deal created event", "deal_id", dealID)

	if err := h.processDealContacts(ctx, dealID, services); err != nil {
		return err
	}

	h.logger.Infow("successfully processed deal created event", "deal_id", dealID)
	return nil
}

// ParseWebhookPayload parses the HubSpot webhook payload
func (h *Handler) ParseWebhookPayload(body []byte) ([]hubspot.WebhookEvent, error) {
	var events []hubspot.WebhookEvent
	if err := json.Unmarshal(body, &events); err != nil {
		h.logger.Errorw("failed to parse webhook payload", "error", err)
		return nil, ierr.NewError("failed to parse webhook payload").
			WithHint("Invalid webhook payload format").
			Mark(ierr.ErrValidation)
	}

	return events, nil
}
