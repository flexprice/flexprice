package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	flexCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/price"
	"github.com/stripe/stripe-go/v82/product"
	"github.com/stripe/stripe-go/v82/webhook"
)

// StripeService handles Stripe integration operations
type StripeService struct {
	ServiceParams
	encryptionService security.EncryptionService
}

// NewStripeService creates a new Stripe service instance
func NewStripeService(params ServiceParams) *StripeService {
	encryptionService, err := security.NewEncryptionService(params.Config, params.Logger)
	if err != nil {
		params.Logger.Fatalw("failed to create encryption service", "error", err)
	}

	return &StripeService{
		ServiceParams:     params,
		encryptionService: encryptionService,
	}
}

// mergeCustomerMetadata merges new metadata with existing customer metadata
func (s *StripeService) mergeCustomerMetadata(existingMetadata map[string]string, newMetadata map[string]string) map[string]string {
	merged := make(map[string]string)

	// Copy existing metadata
	for k, v := range existingMetadata {
		merged[k] = v
	}

	// Add/override with new metadata
	for k, v := range newMetadata {
		merged[k] = v
	}

	return merged
}

// decryptConnectionMetadata decrypts the connection encrypted secret data if it's encrypted
func (s *StripeService) decryptConnectionMetadata(encryptedSecretData types.ConnectionMetadata, providerType types.SecretProvider) (types.ConnectionMetadata, error) {
	decryptedMetadata := encryptedSecretData

	switch providerType {
	case types.SecretProviderStripe:
		if encryptedSecretData.Stripe != nil {
			decryptedPublishableKey, err := s.encryptionService.Decrypt(encryptedSecretData.Stripe.PublishableKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			decryptedSecretKey, err := s.encryptionService.Decrypt(encryptedSecretData.Stripe.SecretKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			decryptedWebhookSecret, err := s.encryptionService.Decrypt(encryptedSecretData.Stripe.WebhookSecret)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}

			decryptedMetadata.Stripe = &types.StripeConnectionMetadata{
				PublishableKey: decryptedPublishableKey,
				SecretKey:      decryptedSecretKey,
				WebhookSecret:  decryptedWebhookSecret,
				AccountID:      encryptedSecretData.Stripe.AccountID, // Account ID is not sensitive
			}
		}

	default:
		// For other providers or unknown types, use generic format
		if encryptedSecretData.Generic != nil {
			decryptedData := make(map[string]interface{})
			for key, value := range encryptedSecretData.Generic.Data {
				if strValue, ok := value.(string); ok {
					decryptedValue, err := s.encryptionService.Decrypt(strValue)
					if err != nil {
						return types.ConnectionMetadata{}, err
					}
					decryptedData[key] = decryptedValue
				} else {
					decryptedData[key] = value
				}
			}
			decryptedMetadata.Generic = &types.GenericConnectionMetadata{
				Data: decryptedData,
			}
		}
	}

	return decryptedMetadata, nil
}

// GetDecryptedStripeConfig gets the decrypted Stripe configuration from a connection
func (s *StripeService) GetDecryptedStripeConfig(conn *connection.Connection) (*connection.StripeConnection, error) {
	// Decrypt metadata if needed
	decryptedMetadata, err := s.decryptConnectionMetadata(conn.EncryptedSecretData, conn.ProviderType)
	if err != nil {
		return nil, err
	}

	// Create a temporary connection with decrypted encrypted secret data
	tempConn := &connection.Connection{
		ID:                  conn.ID,
		Name:                conn.Name,
		ProviderType:        conn.ProviderType,
		EncryptedSecretData: decryptedMetadata,
		EnvironmentID:       conn.EnvironmentID,
		BaseModel:           conn.BaseModel,
	}

	// Now call GetStripeConfig on the decrypted connection
	return tempConn.GetStripeConfig()
}

// EnsureCustomerSyncedToStripe checks if customer is synced to Stripe and syncs if needed

func (s *StripeService) EnsureCustomerSyncedToStripe(ctx context.Context, customerID string) (*dto.CustomerResponse, error) {
	// Get our customer
	customerService := NewCustomerService(s.ServiceParams)
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	ourCustomer := ourCustomerResp.Customer

	// Check if customer already has Stripe ID in metadata
	if stripeID, exists := ourCustomer.Metadata["stripe_customer_id"]; exists && stripeID != "" {
		s.Logger.Infow("customer already synced to Stripe",
			"customer_id", customerID,
			"stripe_customer_id", stripeID)
		return ourCustomerResp, nil
	}

	// Check if customer is synced via integration mapping table
	if s.EntityIntegrationMappingRepo != nil {
		filter := &types.EntityIntegrationMappingFilter{
			EntityID:      customerID,
			EntityType:    types.IntegrationEntityTypeCustomer,
			ProviderTypes: []string{string(types.SecretProviderStripe)},
		}

		entityMappingService := NewEntityIntegrationMappingService(s.ServiceParams)
		existingMappings, err := entityMappingService.GetEntityIntegrationMappings(ctx, filter)
		if err == nil && existingMappings != nil && len(existingMappings.Items) > 0 {
			existingMapping := existingMappings.Items[0]
			s.Logger.Infow("customer already mapped to Stripe via integration mapping",
				"customer_id", customerID,
				"stripe_customer_id", existingMapping.ProviderEntityID)

			// Update customer metadata with Stripe ID for faster future lookups
			updateReq := dto.UpdateCustomerRequest{
				Metadata: s.mergeCustomerMetadata(ourCustomer.Metadata, map[string]string{
					"stripe_customer_id": existingMapping.ProviderEntityID,
				}),
			}
			updatedCustomerResp, err := customerService.UpdateCustomer(ctx, ourCustomer.ID, updateReq)
			if err != nil {
				s.Logger.Warnw("failed to update customer metadata with Stripe ID",
					"customer_id", customerID,
					"error", err)
				// Return original customer info if update fails
				return ourCustomerResp, nil
			}
			return updatedCustomerResp, nil
		}
	}

	// Customer is not synced, create in Stripe
	s.Logger.Infow("customer not synced to Stripe, creating in Stripe",
		"customer_id", customerID)
	err = s.CreateCustomerInStripe(ctx, customerID)
	if err != nil {
		return nil, err
	}

	// Get updated customer after sync
	updatedCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}

	return updatedCustomerResp, nil
}

// CreateCustomerInStripe creates a customer in Stripe and updates our customer with Stripe ID
func (s *StripeService) CreateCustomerInStripe(ctx context.Context, customerID string) error {
	// Get our customer
	customerService := NewCustomerService(s.ServiceParams)
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	ourCustomer := ourCustomerResp.Customer

	// Get Stripe connection for this environment
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	sc := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Check if customer already has Stripe ID
	if stripeID, exists := ourCustomer.Metadata["stripe_customer_id"]; exists && stripeID != "" {
		return ierr.NewError("customer already has Stripe ID").
			WithHint("Customer is already synced with Stripe").
			Mark(ierr.ErrAlreadyExists)
	}

	// Create customer in Stripe
	params := &stripe.CustomerCreateParams{
		Name:  stripe.String(ourCustomer.Name),
		Email: stripe.String(ourCustomer.Email),
		Metadata: map[string]string{
			"flexprice_customer_id": ourCustomer.ID,
			"flexprice_environment": ourCustomer.EnvironmentID,
			"external_id":           ourCustomer.ExternalID,
		},
	}

	// Add address if available
	if ourCustomer.AddressLine1 != "" || ourCustomer.AddressCity != "" {
		params.Address = &stripe.AddressParams{
			Line1:      stripe.String(ourCustomer.AddressLine1),
			Line2:      stripe.String(ourCustomer.AddressLine2),
			City:       stripe.String(ourCustomer.AddressCity),
			State:      stripe.String(ourCustomer.AddressState),
			PostalCode: stripe.String(ourCustomer.AddressPostalCode),
			Country:    stripe.String(ourCustomer.AddressCountry),
		}
	}

	stripeCustomer, err := sc.V1Customers.Create(context.Background(), params)
	if err != nil {
		return ierr.NewError("failed to create customer in Stripe").
			WithHint("Stripe API error").
			Mark(ierr.ErrHTTPClient)
	}

	// Update our customer with Stripe ID
	updateReq := dto.UpdateCustomerRequest{
		Metadata: s.mergeCustomerMetadata(ourCustomer.Metadata, map[string]string{
			"stripe_customer_id": stripeCustomer.ID,
		}),
	}

	_, err = customerService.UpdateCustomer(ctx, ourCustomer.ID, updateReq)
	if err != nil {
		return err
	}

	return nil
}

// CreateCustomerFromStripe creates a customer in our system from Stripe webhook data
func (s *StripeService) CreateCustomerFromStripe(ctx context.Context, stripeCustomer *stripe.Customer, environmentID string) error {
	// Create customer service instance
	customerService := NewCustomerService(s.ServiceParams)

	// Check for existing customer by external ID if flexprice_customer_id is present
	var externalID string
	if flexpriceID, exists := stripeCustomer.Metadata["flexprice_customer_id"]; exists {
		externalID = flexpriceID
		// Check if customer with this external ID already exists
		existing, err := customerService.GetCustomerByLookupKey(ctx, externalID)
		if err == nil && existing != nil {
			// Customer exists with this external ID, update with Stripe ID
			updateReq := dto.UpdateCustomerRequest{
				Metadata: s.mergeCustomerMetadata(existing.Customer.Metadata, map[string]string{
					"stripe_customer_id": stripeCustomer.ID,
				}),
			}
			_, err = customerService.UpdateCustomer(ctx, existing.Customer.ID, updateReq)
			return err
		}
	} else {
		// When syncing from Stripe webhook, set external_id as stripe_customer_id
		externalID = stripeCustomer.ID
	}

	// Create new customer using DTO
	createReq := dto.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       stripeCustomer.Name,
		Email:      stripeCustomer.Email,
		Metadata: map[string]string{
			"stripe_customer_id": stripeCustomer.ID,
		},
	}

	// Add address if available
	if stripeCustomer.Address != nil {
		createReq.AddressLine1 = stripeCustomer.Address.Line1
		createReq.AddressLine2 = stripeCustomer.Address.Line2
		createReq.AddressCity = stripeCustomer.Address.City
		createReq.AddressState = stripeCustomer.Address.State
		createReq.AddressPostalCode = stripeCustomer.Address.PostalCode
		createReq.AddressCountry = stripeCustomer.Address.Country
	}

	_, err := customerService.CreateCustomer(ctx, createReq)
	return err
}

// CreateMeterFromStripe creates a meter in our system from Stripe webhook data
func (s *StripeService) CreateMeterFromStripe(ctx context.Context, meterData map[string]interface{}, environmentID string) error {
	// Create meter service instance
	meterService := NewMeterServiceWithParams(s.ServiceParams)

	// Extract meter fields
	meterID, _ := meterData["id"].(string)
	displayName, _ := meterData["display_name"].(string)
	eventName, _ := meterData["event_name"].(string)

	// Check for existing meter by external ID if flexprice_meter_id is present
	var externalID string
	if metadata, ok := meterData["metadata"].(map[string]interface{}); ok {
		if flexpriceID, exists := metadata["flexprice_meter_id"]; exists {
			if flexpriceIDStr, ok := flexpriceID.(string); ok {
				externalID = flexpriceIDStr
				// Check if meter with this external ID already exists by searching
				filter := &types.MeterFilter{
					QueryFilter: types.NewNoLimitQueryFilter(),
				}
				existingMeters, err := meterService.GetMeters(ctx, filter)
				if err == nil && existingMeters != nil {
					for _, meterResp := range existingMeters.Items {
						// Check if this meter has the same external ID in metadata or name
						if meterResp.Name == externalID || meterResp.EventName == eventName {
							s.Logger.Infow("meter already exists with external ID",
								"meter_id", meterResp.ID,
								"stripe_meter_id", meterID,
								"external_id", externalID)
							return nil
						}
					}
				}
			}
		}
	}

	if externalID == "" {
		// When syncing from Stripe webhook, set external_id as stripe_meter_id
		externalID = meterID
	}

	// Map Stripe meter aggregation to FlexPrice aggregation
	var aggregation meter.Aggregation
	aggregation.Type = types.AggregationCount // Default to count (doesn't require field)

	if defaultAgg, ok := meterData["default_aggregation"].(map[string]interface{}); ok {
		if formula, ok := defaultAgg["formula"].(string); ok {
			switch formula {
			case "sum":
				aggregation.Type = types.AggregationSum
				aggregation.Field = "value" // Default field for sum aggregation
			case "count":
				aggregation.Type = types.AggregationCount
				// Count doesn't require a field
			case "max":
				aggregation.Type = types.AggregationMax
				aggregation.Field = "value" // Default field for max aggregation
			default:
				aggregation.Type = types.AggregationCount // Default to count
			}
		}
	}

	// Extract field from value_settings if available
	if valueSettings, ok := meterData["value_settings"].(map[string]interface{}); ok {
		if eventPayloadKey, ok := valueSettings["event_payload_key"].(string); ok && eventPayloadKey != "" {
			aggregation.Field = eventPayloadKey
		}
	}

	// Create new meter using DTO
	createReq := dto.CreateMeterRequest{
		Name:        displayName,
		EventName:   eventName,
		Aggregation: aggregation,
		Filters:     []meter.Filter{},              // Empty filters array
		ResetUsage:  types.ResetUsageBillingPeriod, // Default reset usage
	}

	_, err := meterService.CreateMeter(ctx, &createReq)
	if err != nil {
		s.Logger.Errorw("failed to create meter in FlexPrice",
			"error", err,
			"meter_id", meterID,
			"display_name", displayName,
			"event_name", eventName,
			"aggregation_type", aggregation.Type)
		return ierr.WithError(err).
			WithHint("Failed to create meter in FlexPrice").
			Mark(ierr.ErrInternal)
	}

	s.Logger.Infow("successfully created meter from Stripe",
		"stripe_meter_id", meterID,
		"display_name", displayName,
		"event_name", eventName,
		"environment_id", environmentID)

	return nil
}

// CreatePaymentLink creates a Stripe checkout session for payment
func (s *StripeService) CreatePaymentLink(ctx context.Context, req *dto.CreateStripePaymentLinkRequest) (*dto.StripePaymentLinkResponse, error) {
	s.Logger.Infow("creating stripe payment link",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
		"environment_id", req.EnvironmentID,
	)

	// Get Stripe connection for this environment
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			WithReportableDetails(map[string]interface{}{
				"environment_id": req.EnvironmentID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Validate invoice and check payment eligibility
	invoiceService := NewInvoiceService(s.ServiceParams)
	invoiceResp, err := invoiceService.GetInvoice(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.NewError("failed to get invoice").
			WithHint("Invoice not found").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Validate invoice payment status
	if invoiceResp.PaymentStatus == types.PaymentStatusSucceeded {
		return nil, ierr.NewError("invoice is already paid").
			WithHint("Cannot create payment link for an already paid invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     req.InvoiceID,
				"payment_status": invoiceResp.PaymentStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	if invoiceResp.InvoiceStatus == types.InvoiceStatusVoided {
		return nil, ierr.NewError("invoice is voided").
			WithHint("Cannot create payment link for a voided invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     req.InvoiceID,
				"invoice_status": invoiceResp.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate payment amount against invoice remaining balance
	if req.Amount.GreaterThan(invoiceResp.AmountRemaining) {
		return nil, ierr.NewError("payment amount exceeds invoice remaining balance").
			WithHint("Payment amount cannot be greater than the remaining balance on the invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":        req.InvoiceID,
				"payment_amount":    req.Amount.String(),
				"invoice_remaining": invoiceResp.AmountRemaining.String(),
				"invoice_total":     invoiceResp.AmountDue.String(),
				"invoice_paid":      invoiceResp.AmountPaid.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate currency matches invoice currency
	if req.Currency != invoiceResp.Currency {
		return nil, ierr.NewError("payment currency does not match invoice currency").
			WithHint("Payment currency must match the invoice currency").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":       req.InvoiceID,
				"payment_currency": req.Currency,
				"invoice_currency": invoiceResp.Currency,
			}).
			Mark(ierr.ErrValidation)
	}

	// Ensure customer is synced to Stripe before creating payment link
	customerResp, err := s.EnsureCustomerSyncedToStripe(ctx, req.CustomerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get Stripe customer ID (should exist after sync)
	stripeCustomerID, exists := customerResp.Customer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return nil, ierr.NewError("customer does not have Stripe customer ID after sync").
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get Stripe configuration
	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Convert amount to cents (Stripe expects amounts in smallest currency unit)
	amountCents := req.Amount.Mul(decimal.NewFromInt(100)).IntPart()

	// Build comprehensive product name with all information
	productName := fmt.Sprintf(customerResp.Customer.Name)

	// Build detailed description with all invoice information
	var descriptionParts []string

	// Add invoice information
	invoiceInfo := fmt.Sprintf("Invoice: %s", lo.FromPtrOr(invoiceResp.InvoiceNumber, req.InvoiceID))
	descriptionParts = append(descriptionParts, invoiceInfo)

	// Add invoice total
	totalInfo := fmt.Sprintf("Invoice Total: %s %s", invoiceResp.Total.String(), invoiceResp.Currency)
	descriptionParts = append(descriptionParts, totalInfo)

	// Add items details
	if len(invoiceResp.LineItems) > 0 {
		var itemDetails []string
		for _, lineItem := range invoiceResp.LineItems {
			if lineItem.Amount.IsZero() {
				continue // Skip zero-amount items
			}

			var entityType string
			var itemName string

			// Determine entity type and name using enums
			if lineItem.EntityType != nil {
				switch *lineItem.EntityType {
				case string(types.InvoiceLineItemEntityTypePlan):
					entityType = "Plan"
					itemName = lo.FromPtrOr(lineItem.DisplayName, "")
					if itemName == "" {
						itemName = lo.FromPtrOr(lineItem.PlanDisplayName, "Plan")
					}
				case string(types.InvoiceLineItemEntityTypeAddon):
					entityType = "Add-on"
					itemName = lo.FromPtrOr(lineItem.DisplayName, "Add-on")
				default:
					entityType = "Item"
					itemName = lo.FromPtrOr(lineItem.DisplayName, "Service")
				}
			}
			// Format as "Entity: Name ($Amount)"
			itemDetail := fmt.Sprintf("%s: %s ($%s)", entityType, itemName, lineItem.Amount.String())
			itemDetails = append(itemDetails, itemDetail)
		}

		if len(itemDetails) > 0 {
			descriptionParts = append(descriptionParts, itemDetails...)
		}
	}

	// Join all parts with separators for better readability
	productDescription := strings.Join(descriptionParts, " • ")

	// Create a single line item for the exact payment amount requested
	lineItems := []*stripe.CheckoutSessionCreateLineItemParams{
		{
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency: stripe.String(req.Currency),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name:        stripe.String(productName),
					Description: stripe.String(productDescription),
				},
				UnitAmount: stripe.Int64(amountCents),
			},
			Quantity: stripe.Int64(1),
		},
	}

	// Build metadata for the session
	metadata := map[string]string{
		"invoice_id":     req.InvoiceID,
		"customer_id":    req.CustomerID,
		"environment_id": req.EnvironmentID,
	}

	// Add custom metadata if provided
	for k, v := range req.Metadata {
		metadata[k] = v
	}

	// Provide default URLs if not provided
	successURL := req.SuccessURL
	if successURL == "" {
		successURL = "https://admin-dev.flexprice.io/customer-management/invoices?page=1"
	}

	cancelURL := req.CancelURL
	if cancelURL == "" {
		cancelURL = "https://admin-dev.flexprice.io/customer-management/invoices?page=1"
	}

	// Create checkout session parameters
	params := &stripe.CheckoutSessionCreateParams{
		LineItems:           lineItems,
		Mode:                stripe.String("payment"),
		AllowPromotionCodes: stripe.Bool(true),
		SuccessURL:          stripe.String(successURL),
		CancelURL:           stripe.String(cancelURL),
		Metadata:            metadata,
		Customer:            stripe.String(stripeCustomerID),
	}

	// Only save payment method for future use if SaveCardAndMakeDefault is true
	if req.SaveCardAndMakeDefault {
		params.PaymentIntentData = &stripe.CheckoutSessionCreatePaymentIntentDataParams{
			SetupFutureUsage: stripe.String("off_session"),
		}
		s.Logger.Infow("payment link configured to save card and make default",
			"invoice_id", req.InvoiceID,
			"customer_id", req.CustomerID,
		)
	} else {
		s.Logger.Infow("payment link configured for one-time payment only",
			"invoice_id", req.InvoiceID,
			"customer_id", req.CustomerID,
		)
	}

	// Create the checkout session
	session, err := stripeClient.V1CheckoutSessions.Create(context.Background(), params)
	if err != nil {
		s.Logger.Errorw("failed to create Stripe checkout session",
			"error", err,
			"invoice_id", req.InvoiceID)
		return nil, ierr.NewError("failed to create payment link").
			WithHint("Unable to create Stripe checkout session").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
				"error":      err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	response := &dto.StripePaymentLinkResponse{
		ID:         session.ID,
		PaymentURL: session.URL,
		PaymentIntentID: func() string {
			if session.PaymentIntent != nil {
				return session.PaymentIntent.ID
			}
			return ""
		}(),
		Amount:    req.Amount,
		Currency:  req.Currency,
		Status:    string(session.Status),
		CreatedAt: session.Created,
		PaymentID: "", // Payment ID will be set by the calling code
	}

	s.Logger.Infow("successfully created stripe payment link",
		"payment_id", response.PaymentID,
		"session_id", session.ID,
		"payment_url", session.URL,
		"invoice_id", req.InvoiceID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
	)

	return response, nil
}

// GetCustomerPaymentMethods retrieves saved payment methods for a customer
func (s *StripeService) GetCustomerPaymentMethods(ctx context.Context, req *dto.GetCustomerPaymentMethodsRequest) ([]*dto.PaymentMethodResponse, error) {
	// Get Stripe connection
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Get our customer to find Stripe customer ID
	customerService := NewCustomerService(s.ServiceParams)
	ourCustomerResp, err := customerService.GetCustomer(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}
	ourCustomer := ourCustomerResp.Customer

	stripeCustomerID, exists := ourCustomer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		// No Stripe customer ID means no saved payment methods
		s.Logger.Warnw("customer has no stripe_customer_id in metadata",
			"customer_id", req.CustomerID,
			"customer_metadata", ourCustomer.Metadata,
		)
		return []*dto.PaymentMethodResponse{}, nil
	}

	s.Logger.Infow("retrieving payment methods for stripe customer",
		"customer_id", req.CustomerID,
		"stripe_customer_id", stripeCustomerID,
	)

	// List payment methods for the customer
	params := &stripe.PaymentMethodListParams{
		Customer: stripe.String(stripeCustomerID),
		Type:     stripe.String("card"),
	}

	paymentMethods := stripeClient.V1PaymentMethods.List(context.Background(), params)
	var responses []*dto.PaymentMethodResponse

	paymentMethods(func(pm *stripe.PaymentMethod, err error) bool {
		if err != nil {
			s.Logger.Errorw("failed to list payment methods",
				"error", err,
				"customer_id", req.CustomerID,
				"stripe_customer_id", stripeCustomerID)
			return false // Stop iteration on error
		}

		response := &dto.PaymentMethodResponse{
			ID:       pm.ID,
			Type:     string(pm.Type),
			Customer: pm.Customer.ID,
			Created:  pm.Created,
			Metadata: make(map[string]interface{}),
		}

		// Convert metadata from map[string]string to map[string]interface{}
		for k, v := range pm.Metadata {
			response.Metadata[k] = v
		}

		if pm.Card != nil {
			response.Card = &dto.CardDetails{
				Brand:       string(pm.Card.Brand),
				Last4:       pm.Card.Last4,
				ExpMonth:    int(pm.Card.ExpMonth),
				ExpYear:     int(pm.Card.ExpYear),
				Fingerprint: pm.Card.Fingerprint,
			}
		}

		responses = append(responses, response)
		return true // Continue iteration
	})

	if len(responses) == 0 {
		s.Logger.Warnw("no payment methods found for customer",
			"customer_id", req.CustomerID,
			"stripe_customer_id", stripeCustomerID)
		return responses, nil // Return empty list instead of error
	}

	s.Logger.Infow("successfully retrieved payment methods",
		"customer_id", req.CustomerID,
		"stripe_customer_id", stripeCustomerID,
		"payment_methods_count", len(responses),
	)

	return responses, nil
}

// SetDefaultPaymentMethod sets a payment method as default in Stripe
func (s *StripeService) SetDefaultPaymentMethod(ctx context.Context, customerID, paymentMethodID string) error {
	// Get Stripe connection
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Get our customer to find Stripe customer ID
	customerService := NewCustomerService(s.ServiceParams)
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	ourCustomer := ourCustomerResp.Customer

	stripeCustomerID, exists := ourCustomer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return ierr.NewError("customer not found in Stripe").
			WithHint("Customer must have a Stripe account").
			Mark(ierr.ErrNotFound)
	}

	s.Logger.Infow("setting default payment method in Stripe",
		"customer_id", customerID,
		"stripe_customer_id", stripeCustomerID,
		"payment_method_id", paymentMethodID,
	)

	// Update customer's default payment method in Stripe
	params := &stripe.CustomerUpdateParams{
		InvoiceSettings: &stripe.CustomerUpdateInvoiceSettingsParams{
			DefaultPaymentMethod: stripe.String(paymentMethodID),
		},
	}

	_, err = stripeClient.V1Customers.Update(context.Background(), stripeCustomerID, params)
	if err != nil {
		s.Logger.Errorw("failed to set default payment method in Stripe",
			"error", err,
			"customer_id", customerID,
			"stripe_customer_id", stripeCustomerID,
			"payment_method_id", paymentMethodID,
		)
		return ierr.NewError("failed to set default payment method").
			WithHint("Could not update default payment method in Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id":       customerID,
				"payment_method_id": paymentMethodID,
			}).
			Mark(ierr.ErrSystem)
	}

	s.Logger.Infow("successfully set default payment method in Stripe",
		"customer_id", customerID,
		"stripe_customer_id", stripeCustomerID,
		"payment_method_id", paymentMethodID,
	)

	return nil
}

// GetDefaultPaymentMethod retrieves the default payment method from Stripe
func (s *StripeService) GetDefaultPaymentMethod(ctx context.Context, customerID string) (*dto.PaymentMethodResponse, error) {
	// Get Stripe connection
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Get our customer to find Stripe customer ID
	customerService := NewCustomerService(s.ServiceParams)
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	ourCustomer := ourCustomerResp.Customer

	stripeCustomerID, exists := ourCustomer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return nil, ierr.NewError("customer not found in Stripe").
			WithHint("Customer must have a Stripe account").
			Mark(ierr.ErrNotFound)
	}

	// Get customer from Stripe to find default payment method
	customer, err := stripeClient.V1Customers.Retrieve(context.Background(), stripeCustomerID, nil)
	if err != nil {
		s.Logger.Errorw("failed to get customer from Stripe",
			"error", err,
			"customer_id", customerID,
			"stripe_customer_id", stripeCustomerID,
		)
		return nil, ierr.NewError("failed to get customer from Stripe").
			WithHint("Could not retrieve customer information from Stripe").
			Mark(ierr.ErrSystem)
	}

	// Check if customer has a default payment method
	if customer.InvoiceSettings == nil || customer.InvoiceSettings.DefaultPaymentMethod == nil {
		return nil, ierr.NewError("no default payment method").
			WithHint("Customer does not have a default payment method set in Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrNotFound)
	}

	defaultPaymentMethodID := customer.InvoiceSettings.DefaultPaymentMethod.ID

	// Get the payment method details
	paymentMethod, err := stripeClient.V1PaymentMethods.Retrieve(context.Background(), defaultPaymentMethodID, nil)
	if err != nil {
		s.Logger.Errorw("failed to get default payment method from Stripe",
			"error", err,
			"customer_id", customerID,
			"payment_method_id", defaultPaymentMethodID,
		)
		return nil, ierr.NewError("failed to get payment method").
			WithHint("Could not retrieve payment method details from Stripe").
			Mark(ierr.ErrSystem)
	}

	// Convert to our DTO format
	response := &dto.PaymentMethodResponse{
		ID:       paymentMethod.ID,
		Type:     string(paymentMethod.Type),
		Customer: paymentMethod.Customer.ID,
		Created:  paymentMethod.Created,
		Metadata: make(map[string]interface{}),
	}

	// Convert metadata
	for k, v := range paymentMethod.Metadata {
		response.Metadata[k] = v
	}

	// Add card details if it's a card
	if paymentMethod.Type == stripe.PaymentMethodTypeCard && paymentMethod.Card != nil {
		response.Card = &dto.CardDetails{
			Brand:       string(paymentMethod.Card.Brand),
			Last4:       paymentMethod.Card.Last4,
			ExpMonth:    int(paymentMethod.Card.ExpMonth),
			ExpYear:     int(paymentMethod.Card.ExpYear),
			Fingerprint: paymentMethod.Card.Fingerprint,
		}
	}

	s.Logger.Infow("successfully retrieved default payment method",
		"customer_id", customerID,
		"stripe_customer_id", stripeCustomerID,
		"payment_method_id", defaultPaymentMethodID,
	)

	return response, nil
}

// ChargeSavedPaymentMethod charges a customer using their saved payment method
func (s *StripeService) ChargeSavedPaymentMethod(ctx context.Context, req *dto.ChargeSavedPaymentMethodRequest) (*dto.PaymentIntentResponse, error) {
	// Get Stripe connection
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Ensure customer is synced to Stripe before charging saved payment method
	ourCustomerResp, err := s.EnsureCustomerSyncedToStripe(ctx, req.CustomerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}
	ourCustomer := ourCustomerResp.Customer

	stripeCustomerID, exists := ourCustomer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return nil, ierr.NewError("customer not found in Stripe after sync").
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get invoice to validate payment amount
	invoiceService := NewInvoiceService(s.ServiceParams)
	invoiceResp, err := invoiceService.GetInvoice(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get invoice for payment validation").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
			}).
			Mark(ierr.ErrNotFound)
	}
	// Validate payment amount against invoice remaining balance
	if req.Amount.GreaterThan(invoiceResp.AmountRemaining) {
		return nil, ierr.NewError("payment amount exceeds invoice remaining balance").
			WithHint("Payment amount cannot be greater than the remaining balance on the invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":        invoiceResp.ID,
				"payment_amount":    req.Amount.String(),
				"invoice_remaining": invoiceResp.AmountRemaining.String(),
				"invoice_total":     invoiceResp.AmountDue.String(),
				"invoice_paid":      invoiceResp.AmountPaid.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Create PaymentIntent with saved payment method
	amountInCents := req.Amount.Mul(decimal.NewFromInt(100)).IntPart()
	params := &stripe.PaymentIntentCreateParams{
		Amount:        stripe.Int64(amountInCents),
		Currency:      stripe.String(req.Currency),
		Customer:      stripe.String(stripeCustomerID),
		PaymentMethod: stripe.String(req.PaymentMethodID),
		OffSession:    stripe.Bool(true), // Important: indicates off-session payment
		Confirm:       stripe.Bool(true), // Confirm immediately
		Metadata: map[string]string{
			"flexprice_customer_id": req.CustomerID,
			"environment_id":        types.GetEnvironmentID(ctx),
			"invoice_id":            req.InvoiceID,
		},
	}

	paymentIntent, err := stripeClient.V1PaymentIntents.Create(context.Background(), params)
	if err != nil {
		// Handle specific error cases
		if stripeErr, ok := err.(*stripe.Error); ok {
			switch stripeErr.Code {
			case stripe.ErrorCodeAuthenticationRequired:
				// Payment requires authentication - customer needs to return to complete
				return nil, ierr.NewError("payment requires authentication").
					WithHint("Customer must return to complete payment authentication").
					WithReportableDetails(map[string]interface{}{
						"customer_id":       req.CustomerID,
						"payment_method_id": req.PaymentMethodID,
						"stripe_error_code": stripeErr.Code,
						"payment_intent_id": stripeErr.PaymentIntent.ID,
					}).
					Mark(ierr.ErrInvalidOperation)
			case stripe.ErrorCodeCardDeclined:
				// Card was declined
				return nil, ierr.NewError("payment method declined").
					WithHint("The saved payment method was declined").
					WithReportableDetails(map[string]interface{}{
						"customer_id":       req.CustomerID,
						"payment_method_id": req.PaymentMethodID,
						"stripe_error_code": stripeErr.Code,
					}).
					Mark(ierr.ErrInvalidOperation)
			}
		}

		s.Logger.Errorw("failed to create PaymentIntent with saved payment method",
			"error", err,
			"customer_id", req.CustomerID,
			"payment_method_id", req.PaymentMethodID,
			"amount", req.Amount.String(),
		)
		return nil, ierr.NewError("failed to charge saved payment method").
			WithHint("Unable to process payment with saved payment method").
			WithReportableDetails(map[string]interface{}{
				"customer_id":       req.CustomerID,
				"payment_method_id": req.PaymentMethodID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	response := &dto.PaymentIntentResponse{
		ID:            paymentIntent.ID,
		Status:        string(paymentIntent.Status),
		Amount:        req.Amount,
		Currency:      req.Currency,
		CustomerID:    stripeCustomerID,
		PaymentMethod: req.PaymentMethodID,
		CreatedAt:     paymentIntent.Created,
	}

	s.Logger.Infow("successfully charged saved payment method",
		"payment_intent_id", paymentIntent.ID,
		"customer_id", req.CustomerID,
		"payment_method_id", req.PaymentMethodID,
		"amount", req.Amount.String(),
		"status", paymentIntent.Status,
	)

	return response, nil
}

// HasSavedPaymentMethods checks if a customer has any saved payment methods
func (s *StripeService) HasSavedPaymentMethods(ctx context.Context, customerID string) (bool, error) {
	req := &dto.GetCustomerPaymentMethodsRequest{
		CustomerID: customerID,
	}

	paymentMethods, err := s.GetCustomerPaymentMethods(ctx, req)
	if err != nil {
		return false, err
	}

	return len(paymentMethods) > 0, nil
}

// HasCustomerStripeMapping checks if the customer has a Stripe entity mapping
func (s *StripeService) HasCustomerStripeMapping(ctx context.Context, customerID string) bool {
	entityMappingService := NewEntityIntegrationMappingService(s.ServiceParams)

	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      customerID,
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderStripe)},
	}

	mappings, err := entityMappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		s.Logger.Debugw("failed to check customer Stripe mapping",
			"customer_id", customerID,
			"error", err,
		)
		return false
	}

	if mappings == nil || len(mappings.Items) == 0 {
		s.Logger.Debugw("no Stripe mapping found for customer",
			"customer_id", customerID,
		)
		return false
	}

	// Check if any mapping has a valid provider entity ID
	for _, mapping := range mappings.Items {
		if mapping.ProviderEntityID != "" {
			s.Logger.Debugw("customer has Stripe mapping",
				"customer_id", customerID,
				"provider_entity_id", mapping.ProviderEntityID,
			)
			return true
		}
	}

	s.Logger.Debugw("customer Stripe mapping found but no provider entity ID",
		"customer_id", customerID,
	)
	return false
}

// ReconcilePaymentWithInvoice updates the invoice payment status and amounts when a payment succeeds
func (s *StripeService) ReconcilePaymentWithInvoice(ctx context.Context, paymentID string, paymentAmount decimal.Decimal) error {
	s.Logger.Infow("starting payment reconciliation with invoice",
		"payment_id", paymentID,
		"payment_amount", paymentAmount.String(),
	)

	// Get the payment record
	paymentService := NewPaymentService(s.ServiceParams)
	payment, err := paymentService.GetPayment(ctx, paymentID)
	if err != nil {
		s.Logger.Errorw("failed to get payment record for reconciliation",
			"error", err,
			"payment_id", paymentID,
		)
		return err
	}

	s.Logger.Infow("got payment record for reconciliation",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"payment_amount", payment.Amount.String(),
	)

	// Get the invoice
	invoiceService := NewInvoiceService(s.ServiceParams)
	invoiceResp, err := invoiceService.GetInvoice(ctx, payment.DestinationID)
	if err != nil {
		s.Logger.Errorw("failed to get invoice for payment reconciliation",
			"error", err,
			"payment_id", paymentID,
			"invoice_id", payment.DestinationID,
		)
		return ierr.WithError(err).
			WithHint("Failed to get invoice for payment reconciliation").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentID,
				"invoice_id": payment.DestinationID,
			}).
			Mark(ierr.ErrSystem)
	}

	s.Logger.Infow("got invoice for reconciliation",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"invoice_amount_due", invoiceResp.AmountDue.String(),
		"invoice_amount_paid", invoiceResp.AmountPaid.String(),
		"invoice_amount_remaining", invoiceResp.AmountRemaining.String(),
		"invoice_payment_status", invoiceResp.PaymentStatus,
		"invoice_status", invoiceResp.InvoiceStatus,
	)

	// Calculate new amounts
	newAmountPaid := invoiceResp.AmountPaid.Add(paymentAmount)
	newAmountRemaining := invoiceResp.AmountDue.Sub(newAmountPaid)

	// Determine payment status
	var newPaymentStatus types.PaymentStatus
	if newAmountRemaining.IsZero() {
		newPaymentStatus = types.PaymentStatusSucceeded
	} else if newAmountRemaining.IsNegative() {
		// Invoice is overpaid
		newPaymentStatus = types.PaymentStatusOverpaid
		// For overpaid invoices, amount_remaining should be 0
		newAmountRemaining = decimal.Zero
	} else {
		newPaymentStatus = types.PaymentStatusPending
	}

	s.Logger.Infow("calculated new amounts for reconciliation",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"payment_amount", paymentAmount.String(),
		"new_amount_paid", newAmountPaid.String(),
		"new_amount_remaining", newAmountRemaining.String(),
		"new_payment_status", newPaymentStatus,
	)

	// Update invoice payment status and amounts using reconciliation method
	s.Logger.Infow("calling invoice reconciliation",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"payment_amount", paymentAmount.String(),
		"new_payment_status", newPaymentStatus,
	)

	err = invoiceService.ReconcilePaymentStatus(ctx, payment.DestinationID, newPaymentStatus, &paymentAmount)
	if err != nil {
		s.Logger.Errorw("failed to update invoice payment status during reconciliation",
			"error", err,
			"payment_id", paymentID,
			"invoice_id", payment.DestinationID,
			"payment_amount", paymentAmount.String(),
			"new_payment_status", newPaymentStatus,
		)
		return ierr.WithError(err).
			WithHint("Failed to update invoice payment status").
			WithReportableDetails(map[string]interface{}{
				"payment_id":         paymentID,
				"invoice_id":         payment.DestinationID,
				"payment_amount":     paymentAmount.String(),
				"new_payment_status": newPaymentStatus,
			}).
			Mark(ierr.ErrSystem)
	}

	s.Logger.Infow("successfully reconciled payment with invoice",
		"payment_id", paymentID,
		"invoice_id", payment.DestinationID,
		"payment_amount", paymentAmount.String(),
		"new_payment_status", newPaymentStatus,
		"new_amount_paid", newAmountPaid.String(),
		"new_amount_remaining", newAmountRemaining.String(),
	)

	return nil
}

// ParseWebhookEvent parses a Stripe webhook event with signature verification
func (s *StripeService) ParseWebhookEvent(payload []byte, signature string, webhookSecret string) (*stripe.Event, error) {
	// Verify the webhook signature, ignoring API version mismatch
	options := webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	}
	event, err := webhook.ConstructEventWithOptions(payload, signature, webhookSecret, options)
	if err != nil {
		// Log the error using structured logging
		s.Logger.Errorw("Stripe webhook verification failed", "error", err)
		return nil, ierr.NewError("failed to verify webhook signature").
			WithHint("Invalid webhook signature or payload").
			Mark(ierr.ErrValidation)
	}
	return &event, nil
}

// VerifyWebhookSignature verifies the Stripe webhook signature
func (s *StripeService) VerifyWebhookSignature(payload []byte, signature string, webhookSecret string) error {
	_, err := webhook.ConstructEvent(payload, signature, webhookSecret)
	if err != nil {
		return ierr.NewError("failed to verify webhook signature").
			WithHint("Invalid webhook signature or payload").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// GetPaymentStatus gets the payment status from Stripe checkout session
func (s *StripeService) GetPaymentStatus(ctx context.Context, sessionID string, environmentID string) (*dto.PaymentStatusResponse, error) {
	// Get Stripe connection for this environment
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			WithReportableDetails(map[string]interface{}{
				"environment_id": environmentID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Get Stripe configuration
	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Get the checkout session with expanded fields
	params := &stripe.CheckoutSessionRetrieveParams{
		Expand: []*string{
			stripe.String("payment_intent"),
			stripe.String("line_items"),
			stripe.String("customer"),
		},
	}
	session, err := stripeClient.V1CheckoutSessions.Retrieve(context.Background(), sessionID, params)
	if err != nil {
		s.Logger.Errorw("failed to get Stripe checkout session",
			"error", err,
			"session_id", sessionID)
		return nil, ierr.NewError("failed to get payment status").
			WithHint("Unable to retrieve Stripe checkout session").
			WithReportableDetails(map[string]interface{}{
				"session_id": sessionID,
				"error":      err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	// Log session details for debugging
	s.Logger.Debugw("retrieved Stripe checkout session",
		"session_id", session.ID,
		"status", session.Status,
		"has_payment_intent", session.PaymentIntent != nil,
		"has_line_items", session.LineItems != nil,
		"line_items_count", func() int {
			if session.LineItems != nil {
				return len(session.LineItems.Data)
			}
			return 0
		}(),
		"has_customer", session.Customer != nil,
	)

	// Get payment intent if available
	var paymentIntentID string
	var paymentStatus string
	var amount decimal.Decimal
	var currency string
	var paymentMethodID string

	// First try to get data from payment intent
	if session.PaymentIntent != nil {
		paymentIntentID = session.PaymentIntent.ID
		paymentStatus = string(session.PaymentIntent.Status)
		if session.PaymentIntent.Amount > 0 {
			amount = decimal.NewFromInt(session.PaymentIntent.Amount).Div(decimal.NewFromInt(100))
		}
		if session.PaymentIntent.Currency != "" {
			currency = string(session.PaymentIntent.Currency)
		}

		// Get payment method ID from payment intent
		if paymentIntentID != "" {
			paymentIntent, err := stripeClient.V1PaymentIntents.Retrieve(context.Background(), paymentIntentID, nil)
			if err != nil {
				s.Logger.Warnw("failed to get payment intent details",
					"error", err,
					"payment_intent_id", paymentIntentID)
				// Don't fail the entire request if we can't get payment intent details
			} else {
				// Get the payment method ID from the payment intent
				if paymentIntent.PaymentMethod != nil {
					paymentMethodID = paymentIntent.PaymentMethod.ID
				}
			}
		}
	}

	// If payment intent data is incomplete, try to get from session
	if paymentStatus == "" {
		paymentStatus = string(session.Status)
	}

	// If amount is still 0, try to get from line items
	if amount.IsZero() && session.LineItems != nil && len(session.LineItems.Data) > 0 {
		item := session.LineItems.Data[0]
		if item.AmountTotal > 0 {
			amount = decimal.NewFromInt(item.AmountTotal).Div(decimal.NewFromInt(100))
		}
		if item.Currency != "" && currency == "" {
			currency = string(item.Currency)
		}
	}

	// If currency is still empty, try to get from session metadata or default
	if currency == "" {
		// Check if currency is in metadata
		if session.Metadata != nil {
			if curr, exists := session.Metadata["currency"]; exists {
				currency = curr
			}
		}
		// Default to USD if still empty
		if currency == "" {
			currency = "usd"
		}
	}

	// Log extracted values for debugging
	s.Logger.Debugw("extracted payment status values",
		"session_id", session.ID,
		"payment_intent_id", paymentIntentID,
		"status", paymentStatus,
		"amount", amount.String(),
		"currency", currency,
		"customer_id", func() string {
			if session.Customer != nil {
				return session.Customer.ID
			}
			return ""
		}(),
	)

	return &dto.PaymentStatusResponse{
		SessionID:       session.ID,
		PaymentIntentID: paymentIntentID,
		PaymentMethodID: paymentMethodID,
		Status:          paymentStatus,
		Amount:          amount,
		Currency:        currency,
		CustomerID: func() string {
			if session.Customer != nil {
				return session.Customer.ID
			}
			return ""
		}(),
		CreatedAt: session.Created,
		ExpiresAt: session.ExpiresAt,
		Metadata:  session.Metadata,
	}, nil
}

// GetPaymentStatusByPaymentIntent gets payment status directly from a payment intent ID
func (s *StripeService) GetPaymentStatusByPaymentIntent(ctx context.Context, paymentIntentID string, environmentID string) (*dto.PaymentStatusResponse, error) {
	// Get Stripe connection for this environment
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			WithReportableDetails(map[string]interface{}{
				"environment_id": environmentID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Get Stripe configuration
	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Get the payment intent with expanded fields
	params := &stripe.PaymentIntentRetrieveParams{
		Expand: []*string{
			stripe.String("payment_method"),
			stripe.String("customer"),
		},
	}
	paymentIntent, err := stripeClient.V1PaymentIntents.Retrieve(context.Background(), paymentIntentID, params)
	if err != nil {
		s.Logger.Errorw("failed to get Stripe payment intent",
			"error", err,
			"payment_intent_id", paymentIntentID)
		return nil, ierr.NewError("failed to get payment status").
			WithHint("Unable to retrieve Stripe payment intent").
			WithReportableDetails(map[string]interface{}{
				"payment_intent_id": paymentIntentID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	// Log payment intent details for debugging
	s.Logger.Debugw("retrieved Stripe payment intent",
		"payment_intent_id", paymentIntent.ID,
		"status", paymentIntent.Status,
		"has_payment_method", paymentIntent.PaymentMethod != nil,
		"has_customer", paymentIntent.Customer != nil,
	)

	// Extract payment method ID
	var paymentMethodID string
	if paymentIntent.PaymentMethod != nil {
		paymentMethodID = paymentIntent.PaymentMethod.ID
	}

	// Convert amount from cents to decimal
	var amount decimal.Decimal
	if paymentIntent.Amount > 0 {
		amount = decimal.NewFromInt(paymentIntent.Amount).Div(decimal.NewFromInt(100))
	}

	// Get currency
	currency := string(paymentIntent.Currency)
	if currency == "" {
		currency = "usd" // Default to USD
	}

	// Log extracted values for debugging
	s.Logger.Debugw("extracted payment intent status values",
		"payment_intent_id", paymentIntent.ID,
		"status", string(paymentIntent.Status),
		"amount", amount.String(),
		"currency", currency,
		"payment_method_id", paymentMethodID,
		"customer_id", func() string {
			if paymentIntent.Customer != nil {
				return paymentIntent.Customer.ID
			}
			return ""
		}(),
	)

	return &dto.PaymentStatusResponse{
		SessionID:       "", // No session ID for direct payment intent
		PaymentIntentID: paymentIntent.ID,
		PaymentMethodID: paymentMethodID,
		Status:          string(paymentIntent.Status),
		Amount:          amount,
		Currency:        currency,
		CustomerID: func() string {
			if paymentIntent.Customer != nil {
				return paymentIntent.Customer.ID
			}
			return ""
		}(),
		CreatedAt: paymentIntent.Created,
		ExpiresAt: 0, // Payment intents don't have expires_at
		Metadata:  paymentIntent.Metadata,
	}, nil
}

// UpdateStripeCustomerMetadata updates the Stripe customer metadata with FlexPrice information
func (s *StripeService) UpdateStripeCustomerMetadata(ctx context.Context, stripeCustomerID string, cust interface{}) error {
	// Type assertion to get customer data
	var customerID, environmentID, externalID string
	switch customer := cust.(type) {
	case *flexCustomer.Customer:
		customerID = customer.ID
		environmentID = customer.EnvironmentID
		externalID = customer.ExternalID
	default:
		return ierr.NewError("invalid customer type").
			WithHint("Expected customer.Customer type").
			Mark(ierr.ErrValidation)
	}

	// Get Stripe connection
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get Stripe connection").
			Mark(ierr.ErrInternal)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get Stripe configuration").
			Mark(ierr.ErrInternal)
	}

	// Initialize Stripe client
	sc := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Create update parameters
	params := &stripe.CustomerUpdateParams{}
	params.AddMetadata("flexprice_customer_id", customerID)
	params.AddMetadata("flexprice_environment", environmentID)
	params.AddMetadata("external_id", externalID)

	// Update the Stripe customer
	_, err = sc.V1Customers.Update(context.Background(), stripeCustomerID, params)
	if err != nil {
		s.Logger.Errorw("failed to update Stripe customer metadata",
			"stripe_customer_id", stripeCustomerID,
			"flexprice_customer_id", customerID,
			"error", err)
		return ierr.WithError(err).
			WithHint("Failed to update Stripe customer metadata").
			Mark(ierr.ErrInternal)
	}

	s.Logger.Infow("successfully updated Stripe customer metadata",
		"stripe_customer_id", stripeCustomerID,
		"flexprice_customer_id", customerID)

	return nil
}

// CreateMeterInStripe creates a meter in Stripe and returns the Stripe meter ID
func (s *StripeService) CreateMeterInStripe(ctx context.Context, meterID string) (string, error) {
	// Get our meter
	meterService := NewMeterService(s.MeterRepo)
	ourMeter, err := meterService.GetMeter(ctx, meterID)
	if err != nil {
		return "", err
	}

	// Get Stripe connection for this environment
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return "", ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return "", ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	sc := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Map FlexPrice aggregation to Stripe formula
	var stripeFormula string
	switch ourMeter.Aggregation.Type {
	case types.AggregationSum:
		stripeFormula = "sum"
	case types.AggregationCount:
		stripeFormula = "count"
	case types.AggregationMax:
		stripeFormula = "last" // Stripe doesn't have max, use last as closest
	default:
		stripeFormula = "sum" // Default to sum
	}

	// Create meter in Stripe
	params := &stripe.BillingMeterCreateParams{
		DisplayName: stripe.String(ourMeter.Name),
		EventName:   stripe.String(ourMeter.EventName),
		DefaultAggregation: &stripe.BillingMeterCreateDefaultAggregationParams{
			Formula: stripe.String(stripeFormula),
		},
	}

	// Add value settings if aggregation has a field (for sum aggregation)
	if ourMeter.Aggregation.Field != "" && ourMeter.Aggregation.Type == types.AggregationSum {
		params.ValueSettings = &stripe.BillingMeterCreateValueSettingsParams{
			EventPayloadKey: stripe.String(ourMeter.Aggregation.Field),
		}
	}

	// Add customer mapping - standard mapping by customer ID
	params.CustomerMapping = &stripe.BillingMeterCreateCustomerMappingParams{
		Type:            stripe.String("by_id"),
		EventPayloadKey: stripe.String("customer_id"),
	}

	stripeMeter, err := sc.V1BillingMeters.Create(context.Background(), params)
	if err != nil {
		return "", ierr.NewError("failed to create meter in Stripe").
			WithHint("Stripe API error").
			WithReportableDetails(map[string]interface{}{
				"meter_id":     meterID,
				"event_name":   ourMeter.EventName,
				"display_name": ourMeter.Name,
			}).
			Mark(ierr.ErrHTTPClient)
	}

	s.Logger.Infow("successfully created meter in Stripe",
		"meter_id", meterID,
		"stripe_meter_id", stripeMeter.ID,
		"event_name", ourMeter.EventName,
		"display_name", ourMeter.Name,
	)

	return stripeMeter.ID, nil
}

// CreateProductInStripe creates a product in Stripe and returns the Stripe product ID
func (s *StripeService) CreateProductInStripe(ctx context.Context, planID string) (string, error) {
	// Get our plan
	ourPlan, err := s.PlanRepo.Get(ctx, planID)
	if err != nil {
		return "", err
	}

	// Get Stripe connection for this environment
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return "", ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return "", ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Set Stripe API key
	stripe.Key = stripeConfig.SecretKey

	// Create product in Stripe
	params := &stripe.ProductParams{
		Name: stripe.String(ourPlan.Name),
	}

	// Add description if available
	if ourPlan.Description != "" {
		params.Description = stripe.String(ourPlan.Description)
	}

	// Add metadata if lookup key is available
	if ourPlan.LookupKey != "" {
		params.Metadata = map[string]string{
			"flexprice_plan_id":     ourPlan.ID,
			"flexprice_lookup_key":  ourPlan.LookupKey,
			"flexprice_tenant_id":   types.GetTenantID(ctx),
			"flexprice_environment": types.GetEnvironmentID(ctx),
		}
		// Note: Stripe doesn't have a direct lookup_key for products like it does for prices
		// We store the original lookup_key in metadata for reference
	}

	// Set product as active
	params.Active = stripe.Bool(true)

	stripeProduct, err := product.New(params)
	if err != nil {
		return "", ierr.NewError("failed to create product in Stripe").
			WithHint("Stripe API error").
			Mark(ierr.ErrHTTPClient)
	}

	s.Logger.Infow("successfully created product in Stripe",
		"plan_id", planID,
		"stripe_product_id", stripeProduct.ID,
		"name", ourPlan.Name,
		"lookup_key", ourPlan.LookupKey)

	return stripeProduct.ID, nil
}

// CreatePriceInStripe creates a price in Stripe and returns the Stripe price ID
func (s *StripeService) CreatePriceInStripe(ctx context.Context, priceID string) (string, error) {
	// Get our price
	ourPrice, err := s.PriceRepo.Get(ctx, priceID)
	if err != nil {
		return "", ierr.NewError("failed to get price").
			WithHint("Price not found").
			Mark(ierr.ErrNotFound)
	}

	// Get Stripe connection for this environment
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return "", ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.GetDecryptedStripeConfig(conn)
	if err != nil {
		return "", ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Set Stripe API key
	stripe.Key = stripeConfig.SecretKey

	// Get the Stripe product ID for this price's plan
	stripeProductID, err := s.getStripeProductID(ctx, ourPrice.EntityID)
	if err != nil {
		return "", ierr.NewError("failed to get Stripe product ID").
			WithHint("Plan must be synced to Stripe before syncing prices").
			Mark(ierr.ErrValidation)
	}

	// Create price parameters
	params := &stripe.PriceParams{
		Currency: stripe.String(strings.ToLower(ourPrice.Currency)),
		Product:  stripe.String(stripeProductID),
		Active:   stripe.Bool(true),
	}

	// Add lookup key if available
	if ourPrice.LookupKey != "" {
		params.LookupKey = stripe.String(ourPrice.LookupKey)
	}

	// Add metadata
	params.Metadata = map[string]string{
		"flexprice_price_id":      ourPrice.ID,
		"flexprice_tenant_id":     types.GetTenantID(ctx),
		"flexprice_environment":   types.GetEnvironmentID(ctx),
		"flexprice_price_type":    string(ourPrice.Type),
		"flexprice_billing_model": string(ourPrice.BillingModel),
	}

	// Map billing cadence (type)
	if ourPrice.BillingCadence == types.BILLING_CADENCE_RECURRING {
		// Set up recurring parameters
		recurringParams := &stripe.PriceRecurringParams{
			Interval:      stripe.String(s.mapBillingPeriodToStripe(ourPrice.BillingPeriod)),
			IntervalCount: stripe.Int64(int64(ourPrice.BillingPeriodCount)),
		}

		// Map usage type based on price type
		if ourPrice.Type == types.PRICE_TYPE_USAGE && ourPrice.MeterID != "" {
			recurringParams.UsageType = stripe.String("metered")

			// Get synced meter ID from integration mapping
			stripeMeterID, err := s.getStripeMeterID(ctx, ourPrice.MeterID)
			if err != nil {
				return "", ierr.NewError("failed to get Stripe meter ID").
					WithHint("Meter must be synced to Stripe before creating usage-based prices").
					Mark(ierr.ErrValidation)
			}
			recurringParams.Meter = stripe.String(stripeMeterID)
		} else {
			recurringParams.UsageType = stripe.String("licensed")
		}

		params.Recurring = recurringParams
	}
	// Note: For one-time prices, we don't set recurring params

	// Map billing model
	switch ourPrice.BillingModel {
	case types.BILLING_MODEL_FLAT_FEE:
		params.BillingScheme = stripe.String("per_unit")
		// Convert amount to cents (Stripe expects cents)
		amountInCents := ourPrice.Amount.Mul(decimal.NewFromInt(100)).IntPart()
		params.UnitAmount = stripe.Int64(amountInCents)

	case types.BILLING_MODEL_PACKAGE:
		// Package pricing maps to per_unit with calculated unit amount
		params.BillingScheme = stripe.String("per_unit")
		// For package pricing, use the amount as-is (already calculated per package)
		amountInCents := ourPrice.Amount.Mul(decimal.NewFromInt(100)).IntPart()
		params.UnitAmount = stripe.Int64(amountInCents)

	case types.BILLING_MODEL_TIERED:
		params.BillingScheme = stripe.String("tiered")

		// Map tier mode
		if ourPrice.TierMode == types.BILLING_TIER_VOLUME {
			params.TiersMode = stripe.String("volume")
		} else if ourPrice.TierMode == types.BILLING_TIER_SLAB {
			params.TiersMode = stripe.String("graduated")
		}

		// Map tiers
		if len(ourPrice.Tiers) > 0 {
			// Convert domain tiers to types tiers
			var typeTiers []types.PriceTier
			for _, tier := range ourPrice.Tiers {
				typeTiers = append(typeTiers, types.PriceTier{
					UpTo:       tier.UpTo,
					UnitAmount: tier.UnitAmount,
					FlatAmount: tier.FlatAmount,
				})
			}
			stripeTiers := s.mapPriceTiersToStripe(typeTiers)
			params.Tiers = stripeTiers
		}

	default:
		return "", ierr.NewError("unsupported billing model").
			WithHint(fmt.Sprintf("Billing model %s is not supported for Stripe sync", ourPrice.BillingModel)).
			Mark(ierr.ErrValidation)
	}

	// Handle transform quantity if present
	if ourPrice.TransformQuantity.DivideBy > 0 {
		params.TransformQuantity = &stripe.PriceTransformQuantityParams{
			DivideBy: stripe.Int64(int64(ourPrice.TransformQuantity.DivideBy)),
			Round:    stripe.String(strings.ToLower(ourPrice.TransformQuantity.Round)),
		}
	}

	// Create price in Stripe
	stripePrice, err := price.New(params)
	if err != nil {
		return "", ierr.NewError("failed to create price in Stripe").
			WithHint("Stripe API error").
			Mark(ierr.ErrHTTPClient)
	}

	s.Logger.Infow("successfully created price in Stripe",
		"price_id", priceID,
		"stripe_price_id", stripePrice.ID,
		"type", ourPrice.Type,
		"billing_model", ourPrice.BillingModel,
		"amount", ourPrice.Amount,
		"currency", ourPrice.Currency)

	return stripePrice.ID, nil
}

// mapBillingPeriodToStripe maps FlexPrice billing periods to Stripe intervals
func (s *StripeService) mapBillingPeriodToStripe(period types.BillingPeriod) string {
	switch period {
	case types.BILLING_PERIOD_DAILY:
		return "day"
	case types.BILLING_PERIOD_WEEKLY:
		return "week"
	case types.BILLING_PERIOD_MONTHLY:
		return "month"
	case types.BILLING_PERIOD_ANNUAL:
		return "year"
	case types.BILLING_PERIOD_QUARTER:
		return "month" // Will use interval_count=3
	case types.BILLING_PERIOD_HALF_YEAR:
		return "month" // Will use interval_count=6
	default:
		return "month"
	}
}

// mapPriceTiersToStripe maps FlexPrice tiers to Stripe tier parameters
func (s *StripeService) mapPriceTiersToStripe(flexTiers []types.PriceTier) []*stripe.PriceTierParams {
	var stripeTiers []*stripe.PriceTierParams

	for _, tier := range flexTiers {
		stripeTier := &stripe.PriceTierParams{}

		// Map up_to value
		if tier.UpTo == nil {
			// For the last tier, Stripe expects "inf" as a string
			stripeTier.UpToInf = stripe.Bool(true)
		} else {
			stripeTier.UpTo = stripe.Int64(int64(*tier.UpTo))
		}

		// Map flat amount (convert to cents)
		if tier.FlatAmount != nil && !tier.FlatAmount.IsZero() {
			flatAmountInCents := tier.FlatAmount.Mul(decimal.NewFromInt(100)).IntPart()
			stripeTier.FlatAmount = stripe.Int64(flatAmountInCents)
		}

		// Map unit amount (convert to cents)
		if !tier.UnitAmount.IsZero() {
			unitAmountInCents := tier.UnitAmount.Mul(decimal.NewFromInt(100)).IntPart()
			stripeTier.UnitAmount = stripe.Int64(unitAmountInCents)
		}

		stripeTiers = append(stripeTiers, stripeTier)
	}

	return stripeTiers
}

// getStripeProductID gets the Stripe product ID for a FlexPrice plan
func (s *StripeService) getStripeProductID(ctx context.Context, planID string) (string, error) {
	entityMappingService := NewEntityIntegrationMappingService(s.ServiceParams)
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypePlan,
		EntityID:      planID,
		ProviderTypes: []string{string(types.SecretProviderStripe)},
	}

	mappings, err := entityMappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return "", err
	}

	if len(mappings.Items) == 0 {
		return "", ierr.NewError("plan not synced to Stripe").
			WithHint("Plan must be synced to Stripe before creating prices").
			Mark(ierr.ErrNotFound)
	}

	return mappings.Items[0].ProviderEntityID, nil
}

// getStripeMeterID gets the Stripe meter ID for a FlexPrice meter
func (s *StripeService) getStripeMeterID(ctx context.Context, meterID string) (string, error) {
	entityMappingService := NewEntityIntegrationMappingService(s.ServiceParams)
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeMeter,
		EntityID:      meterID,
		ProviderTypes: []string{string(types.SecretProviderStripe)},
	}

	mappings, err := entityMappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return "", err
	}

	if len(mappings.Items) == 0 {
		return "", ierr.NewError("meter not synced to Stripe").
			WithHint("Meter must be synced to Stripe before creating usage-based prices").
			Mark(ierr.ErrNotFound)
	}

	return mappings.Items[0].ProviderEntityID, nil
}
