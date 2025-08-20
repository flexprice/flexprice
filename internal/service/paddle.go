package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PaddleService handles Paddle integration operations
type PaddleService struct {
	ServiceParams
	encryptionService security.EncryptionService
}

// NewPaddleService creates a new Paddle service instance
func NewPaddleService(params ServiceParams) *PaddleService {
	encryptionService, err := security.NewEncryptionService(params.Config, params.Logger)
	if err != nil {
		params.Logger.Fatalw("failed to create encryption service", "error", err)
	}

	return &PaddleService{
		ServiceParams:     params,
		encryptionService: encryptionService,
	}
}

// decryptConnectionMetadata decrypts the connection encrypted secret data if it's encrypted
func (s *PaddleService) decryptConnectionMetadata(encryptedSecretData types.ConnectionMetadata, providerType types.SecretProvider) (types.ConnectionMetadata, error) {
	decryptedMetadata := encryptedSecretData

	switch providerType {
	case types.SecretProviderPaddle:
		if encryptedSecretData.Paddle != nil {
			decryptedAPIKey, err := s.encryptionService.Decrypt(encryptedSecretData.Paddle.APIKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			decryptedWebhookSecret, err := s.encryptionService.Decrypt(encryptedSecretData.Paddle.WebhookSecret)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}

			decryptedMetadata.Paddle = &types.PaddleConnectionMetadata{
				APIKey:        decryptedAPIKey,
				WebhookSecret: decryptedWebhookSecret,
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

// GetDecryptedPaddleConfig gets the decrypted Paddle configuration from a connection
func (s *PaddleService) GetDecryptedPaddleConfig(conn *connection.Connection) (*connection.PaddleConnection, error) {
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
	}

	// Now call GetPaddleConfig on the decrypted connection
	return tempConn.GetPaddleConfig()
}

// CreatePaddleClient creates and configures a Paddle API client
func (s *PaddleService) CreatePaddleClient(ctx context.Context) (*paddle.SDK, error) {
	// Get Paddle connection for this environment
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		return nil, ierr.NewError("failed to get Paddle connection").
			WithHint("Paddle connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	paddleConfig, err := s.GetDecryptedPaddleConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Paddle configuration").
			WithHint("Invalid Paddle configuration").
			Mark(ierr.ErrValidation)
	}

	// Create Paddle client - determine environment based on API key prefix
	var client *paddle.SDK
	var clientErr error

	// Handle different API key formats
	if strings.HasPrefix(paddleConfig.APIKey, "test_") || strings.HasPrefix(paddleConfig.APIKey, "pdl_s") {
		// Sandbox environment
		s.Logger.Infow("using Paddle Sandbox environment", "api_key_prefix", paddleConfig.APIKey[:5])
		client, clientErr = paddle.New(paddleConfig.APIKey, paddle.WithBaseURL(paddle.SandboxBaseURL))
	} else if strings.HasPrefix(paddleConfig.APIKey, "live_") || strings.HasPrefix(paddleConfig.APIKey, "pdl_l") || strings.HasPrefix(paddleConfig.APIKey, "pdl_") {
		// Production environment
		s.Logger.Infow("using Paddle Production environment", "api_key_prefix", paddleConfig.APIKey[:5])
		client, clientErr = paddle.New(paddleConfig.APIKey, paddle.WithBaseURL(paddle.ProductionBaseURL))
	} else {
		// Debug: Log the first few characters of API key to verify decryption
		apiKeyPrefix := "unknown"
		if len(paddleConfig.APIKey) >= 5 {
			apiKeyPrefix = paddleConfig.APIKey[:5]
		}
		return nil, ierr.NewError("invalid Paddle API key format").
			WithHint(fmt.Sprintf("API key should start with 'test_', 'live_', or 'pdl_', got: %s", apiKeyPrefix)).
			Mark(ierr.ErrValidation)
	}

	if clientErr != nil {
		return nil, ierr.NewError("failed to create Paddle client").
			WithHint("Invalid Paddle API key or configuration").
			Mark(ierr.ErrValidation)
	}

	return client, nil
}

// CreateCustomerInPaddle creates a customer in Paddle and updates our customer with Paddle ID
func (s *PaddleService) CreateCustomerInPaddle(ctx context.Context, customerID string) error {
	// Get our customer
	customerService := NewCustomerService(s.ServiceParams)
	ourCustomerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return err
	}
	ourCustomer := ourCustomerResp.Customer

	// Create Paddle client
	paddleClient, err := s.CreatePaddleClient(ctx)
	if err != nil {
		return err
	}

	// Check if customer already has Paddle ID
	if paddleID, exists := ourCustomer.Metadata["paddle_customer_id"]; exists && paddleID != "" {
		return ierr.NewError("customer already has Paddle ID").
			WithHint("Customer is already synced with Paddle").
			Mark(ierr.ErrAlreadyExists)
	}

	// Validate email format
	if ourCustomer.Email == "" {
		return ierr.NewError("customer email is required").
			WithHint("Paddle requires a valid email address").
			Mark(ierr.ErrValidation)
	}

	// Create customer in Paddle
	createReq := &paddle.CreateCustomerRequest{
		Email: ourCustomer.Email,
	}

	if ourCustomer.Name != "" {
		createReq.Name = &ourCustomer.Name
	}

	// Add custom data if needed
	createReq.CustomData = map[string]interface{}{
		"flexprice_customer_id": ourCustomer.ID,
		"flexprice_environment": ourCustomer.EnvironmentID,
	}

	// Debug: Log the request being sent (without sensitive data)
	s.Logger.Infow("creating Paddle customer",
		"email", createReq.Email,
		"name", createReq.Name,
		"flexprice_customer_id", ourCustomer.ID,
	)

	paddleCustomer, err := paddleClient.CreateCustomer(ctx, createReq)
	if err != nil {
		return ierr.NewError("failed to create customer in Paddle").
			WithHint(fmt.Sprintf("Paddle API error: %v", err)).
			Mark(ierr.ErrHTTPClient)
	}

	// Log the created customer details for debugging
	s.Logger.Infow("Paddle customer created successfully",
		"paddle_customer_id", paddleCustomer.ID,
		"customer_email", paddleCustomer.Email,
		"customer_name", paddleCustomer.Name,
		"customer_status", paddleCustomer.Status,
		"created_at", paddleCustomer.CreatedAt,
	)

	// Update our customer with Paddle ID
	updateReq := dto.UpdateCustomerRequest{
		Metadata: map[string]string{
			"paddle_customer_id": paddleCustomer.ID,
		},
	}
	// Merge with existing metadata
	if ourCustomer.Metadata != nil {
		for k, v := range ourCustomer.Metadata {
			updateReq.Metadata[k] = v
		}
	}

	_, err = customerService.UpdateCustomer(ctx, ourCustomer.ID, updateReq)
	if err != nil {
		return err
	}

	return nil
}

// CreateCustomerFromPaddle creates a customer in our system from Paddle webhook data
func (s *PaddleService) CreateCustomerFromPaddle(ctx context.Context, paddleCustomer map[string]interface{}, environmentID string) error {
	// Create customer service instance
	customerService := NewCustomerService(s.ServiceParams)

	// Extract customer data from Paddle webhook
	email, _ := paddleCustomer["email"].(string)
	name, _ := paddleCustomer["name"].(string)
	paddleCustomerID, _ := paddleCustomer["id"].(string)

	if email == "" || paddleCustomerID == "" {
		return ierr.NewError("invalid paddle customer data").
			WithHint("Email and customer ID are required").
			Mark(ierr.ErrValidation)
	}

	// Check for existing customer by external ID if flexprice_customer_id is present
	var externalID string
	if customData, ok := paddleCustomer["custom_data"].(map[string]interface{}); ok {
		if flexpriceID, exists := customData["flexprice_customer_id"].(string); exists {
			externalID = flexpriceID
			// Check if customer with this external ID already exists
			existing, err := customerService.GetCustomerByLookupKey(ctx, externalID)
			if err == nil && existing != nil {
				// Customer exists with this external ID, update with Paddle ID
				updateReq := dto.UpdateCustomerRequest{
					Metadata: map[string]string{
						"paddle_customer_id": paddleCustomerID,
					},
				}
				// Merge with existing metadata
				if existing.Customer.Metadata != nil {
					for k, v := range existing.Customer.Metadata {
						updateReq.Metadata[k] = v
					}
				}
				_, err = customerService.UpdateCustomer(ctx, existing.Customer.ID, updateReq)
				return err
			}
		}
	}

	if externalID == "" {
		// Generate external ID if not present
		externalID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER)
	}

	// Create new customer using DTO
	createReq := dto.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       name,
		Email:      email,
		Metadata: map[string]string{
			"paddle_customer_id": paddleCustomerID,
		},
	}

	_, err := customerService.CreateCustomer(ctx, createReq)
	return err
}

// VerifyWebhookSignature verifies the Paddle webhook signature using Paddle SDK
func (s *PaddleService) VerifyWebhookSignature(ctx context.Context, req *http.Request) error {
	// Get Paddle connection to retrieve webhook secret
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		s.Logger.Errorw("failed to get Paddle connection for webhook verification",
			"error", err,
			"tenant_id", types.GetTenantID(ctx),
			"environment_id", types.GetEnvironmentID(ctx))
		return ierr.NewError("failed to get Paddle connection for webhook verification").
			WithHint("Paddle connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	paddleConfig, err := s.GetDecryptedPaddleConfig(conn)
	if err != nil {
		s.Logger.Errorw("failed to get Paddle configuration", "error", err)
		return ierr.NewError("failed to get Paddle configuration").
			WithHint("Invalid Paddle configuration").
			Mark(ierr.ErrValidation)
	}

	// Verify webhook secret is configured
	if paddleConfig.WebhookSecret == "" {
		s.Logger.Errorw("webhook secret not configured for Paddle connection")
		return ierr.NewError("webhook secret not configured for Paddle connection").
			WithHint("Paddle webhook secret is required").
			Mark(ierr.ErrValidation)
	}

	// Log webhook secret format for debugging (first 10 chars only for security)
	secretPrefix := paddleConfig.WebhookSecret
	if len(secretPrefix) > 10 {
		secretPrefix = secretPrefix[:10] + "..."
	}
	s.Logger.Debugw("verifying Paddle webhook signature",
		"secret_prefix", secretPrefix,
		"signature_header", req.Header.Get("Paddle-Signature"))

	// Create Paddle webhook verifier
	verifier := paddle.NewWebhookVerifier(paddleConfig.WebhookSecret)

	// Verify the webhook signature
	verified, err := verifier.Verify(req)
	if err != nil {
		s.Logger.Errorw("error during Paddle webhook signature verification",
			"error", err,
			"signature_header", req.Header.Get("Paddle-Signature"))
		return ierr.NewError("failed to verify Paddle webhook signature").
			WithHint("Error during webhook verification").
			Mark(ierr.ErrValidation)
	}

	if !verified {
		s.Logger.Errorw("Paddle webhook signature verification failed",
			"signature_header", req.Header.Get("Paddle-Signature"))
		return ierr.NewError("invalid Paddle webhook signature").
			WithHint("Webhook signature verification failed").
			Mark(ierr.ErrValidation)
	}

	s.Logger.Debugw("Paddle webhook signature verified successfully")
	return nil
}

// ParseWebhookEvent parses a Paddle webhook event
func (s *PaddleService) ParseWebhookEvent(payload []byte) (map[string]interface{}, error) {
	// Parse JSON payload
	var event map[string]interface{}
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, ierr.NewError("failed to parse webhook payload").
			WithHint("Invalid JSON payload").
			Mark(ierr.ErrValidation)
	}

	return event, nil
}

// CreatePaymentLink creates a Paddle checkout session for payment
func (s *PaddleService) CreatePaymentLink(ctx context.Context, req *dto.CreatePaddlePaymentLinkRequest) (*dto.PaddlePaymentLinkResponse, error) {
	s.Logger.Infow("creating paddle payment link",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
		"environment_id", req.EnvironmentID,
	)

	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get customer to verify it exists and check for Paddle customer ID
	customerService := NewCustomerService(s.ServiceParams)
	customerResp, err := customerService.GetCustomer(ctx, req.CustomerID)
	if err != nil {
		return nil, ierr.NewError("failed to get customer").
			WithHint("Customer not found").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
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

	// Create Paddle client
	_, err = s.CreatePaddleClient(ctx)
	if err != nil {
		return nil, err
	}

	// Ensure customer exists in Paddle
	paddleCustomerID := ""
	if customerResp.Customer.Metadata != nil {
		if id, exists := customerResp.Customer.Metadata["paddle_customer_id"]; exists && id != "" {
			paddleCustomerID = id
		}
	}

	// If no Paddle customer ID, create customer in Paddle
	if paddleCustomerID == "" {
		err := s.CreateCustomerInPaddle(ctx, req.CustomerID)
		if err != nil {
			return nil, ierr.NewError("failed to create customer in Paddle").
				WithHint("Unable to sync customer with Paddle").
				Mark(ierr.ErrHTTPClient)
		}

		// Refresh customer data to get Paddle ID
		customerResp, err = customerService.GetCustomer(ctx, req.CustomerID)
		if err != nil {
			return nil, err
		}

		if customerResp.Customer.Metadata != nil {
			if id, exists := customerResp.Customer.Metadata["paddle_customer_id"]; exists && id != "" {
				paddleCustomerID = id
			}
		}

		if paddleCustomerID == "" {
			return nil, ierr.NewError("failed to get Paddle customer ID").
				WithHint("Customer was created in Paddle but ID not found").
				Mark(ierr.ErrSystem)
		}
	}

	// Create Paddle client
	paddleClient, err := s.CreatePaddleClient(ctx)
	if err != nil {
		return nil, err
	}

	// Provide default URLs if not provided
	successURL := req.SuccessURL
	if successURL == "" {
		successURL = "https://admin-dev.flexprice.io/customer-management/invoices?page=1&status=success"
	}

	cancelURL := req.CancelURL
	if cancelURL == "" {
		cancelURL = "https://admin-dev.flexprice.io/customer-management/invoices?page=1&status=canceled"
	}

	// Create transaction using your exact working pattern
	item := &paddle.TransactionItemCreateWithProduct{
		Quantity: 1,
		Price: paddle.TransactionPriceCreateWithProduct{
			Name:         paddle.PtrTo("Custom Service Payment"),
			Description:  fmt.Sprintf("For: Invoice #%s", req.InvoiceID),
			BillingCycle: nil, // null for one-time payments
			UnitPrice: paddle.Money{
				Amount:       req.Amount.Mul(decimal.NewFromInt(100)).String(),
				CurrencyCode: paddle.CurrencyCode(strings.ToUpper(req.Currency)),
			},
			TaxMode: paddle.TaxModeExternal,
			Quantity: paddle.PriceQuantity{
				Minimum: 1,
				Maximum: 1,
			},
			Product: paddle.TransactionSubscriptionProductCreate{
				Name:        fmt.Sprintf("Flexprice Service Invoice #%s", req.InvoiceID),
				Description: paddle.PtrTo("One-time custom service"),
				TaxCategory: paddle.TaxCategoryStandard,
			},
		},
	}

	// Now wrap this in a CreateTransactionItems
	txnItem := paddle.NewCreateTransactionItemsTransactionItemCreateWithProduct(item)

	createTxnReq := &paddle.CreateTransactionRequest{
		Items:          []paddle.CreateTransactionItems{*txnItem},
		CustomerID:     &paddleCustomerID,
		CollectionMode: paddle.PtrTo(paddle.CollectionModeAutomatic),                     // Explicit!
		CurrencyCode:   paddle.PtrTo(paddle.CurrencyCode(strings.ToUpper(req.Currency))), // Explicit currency

		CustomData: paddle.CustomData{
			"invoice_id":     req.InvoiceID,
			"customer_id":    req.CustomerID,
			"environment_id": req.EnvironmentID,
			"success_url":    successURL,
			"cancel_url":     cancelURL,
		},
	}

	// Add custom metadata if provided
	if req.Metadata != nil {
		for k, v := range req.Metadata {
			createTxnReq.CustomData[k] = v
		}
	}

	// Log the transaction request for debugging
	s.Logger.Infow("creating Paddle transaction",
		"customer_id", paddleCustomerID,
		"amount_dollars", req.Amount.String(),
		"amount_cents", item.Price.UnitPrice.Amount,
		"currency", strings.ToUpper(req.Currency),
		"invoice_id", req.InvoiceID,
		"item_quantity", item.Quantity,
		"price_name", *item.Price.Name,
		"price_description", item.Price.Description,
		"unit_price_cents", item.Price.UnitPrice.Amount,
		"currency_code", item.Price.UnitPrice.CurrencyCode,
		"tax_mode", item.Price.TaxMode,
		"quantity_min", item.Price.Quantity.Minimum,
		"quantity_max", item.Price.Quantity.Maximum,
		"product_name", item.Price.Product.Name,
		"product_tax_category", item.Price.Product.TaxCategory,
		"collection_mode", "automatic",
		"billing_cycle", item.Price.BillingCycle,
	)

	// Create the transaction
	transaction, err := paddleClient.CreateTransaction(ctx, createTxnReq)
	if err != nil {
		s.Logger.Errorw("failed to create Paddle transaction",
			"error", err,
			"invoice_id", req.InvoiceID,
			"customer_id", paddleCustomerID,
			"amount", req.Amount.String(),
			"currency", req.Currency)
		return nil, ierr.NewError("failed to create payment link").
			WithHint("Unable to create Paddle transaction").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": req.InvoiceID,
				"error":      err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	// Log successful transaction creation
	s.Logger.Infow("Paddle transaction created successfully",
		"transaction_id", transaction.ID,
		"transaction_status", transaction.Status,
		"customer_id", transaction.CustomerID,
		"collection_mode", transaction.CollectionMode,
		"created_at", transaction.CreatedAt,
		"invoice_id", req.InvoiceID,
	)

	// Get the hosted checkout URL from the response
	checkoutURL := ""
	if transaction.Checkout != nil && transaction.Checkout.URL != nil {
		checkoutURL = *transaction.Checkout.URL
		s.Logger.Infow("Paddle checkout URL generated",
			"checkout_url", checkoutURL,
			"transaction_id", transaction.ID,
			"transaction_status", transaction.Status,
		)
	} else {
		s.Logger.Warnw("No checkout URL in Paddle response",
			"transaction_id", transaction.ID,
			"transaction_status", transaction.Status,
			"checkout_object", transaction.Checkout,
		)
	}

	response := &dto.PaddlePaymentLinkResponse{
		ID:            transaction.ID,
		PaymentURL:    checkoutURL,
		TransactionID: transaction.ID,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Status:        string(transaction.Status),
		CreatedAt:     parseTimestamp(transaction.CreatedAt),
		PaymentID:     "", // Payment ID will be set by the calling code
	}

	s.Logger.Infow("successfully created paddle payment link",
		"payment_id", response.PaymentID,
		"transaction_id", transaction.ID,
		"payment_url", checkoutURL,
		"invoice_id", req.InvoiceID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
	)

	return response, nil
}

// GetPaymentStatus retrieves payment status from Paddle by transaction ID
func (s *PaddleService) GetPaymentStatus(ctx context.Context, transactionID string, environmentID string) (*dto.PaddlePaymentStatusResponse, error) {
	s.Logger.Infow("getting paddle payment status",
		"transaction_id", transactionID,
		"environment_id", environmentID,
	)

	// Create Paddle client
	paddleClient, err := s.CreatePaddleClient(ctx)
	if err != nil {
		return nil, err
	}

	// Get transaction from Paddle
	transaction, err := paddleClient.GetTransaction(ctx, &paddle.GetTransactionRequest{
		TransactionID: transactionID,
	})
	if err != nil {
		return nil, ierr.NewError("failed to get transaction from Paddle").
			WithHint("Unable to retrieve transaction status").
			WithReportableDetails(map[string]interface{}{
				"transaction_id": transactionID,
				"error":          err.Error(),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	// Convert amount from cents to dollars
	amount := decimal.Zero
	if transaction.Details.Totals.Total != "" {
		if parsedAmount, parseErr := decimal.NewFromString(transaction.Details.Totals.Total); parseErr == nil {
			// Paddle returns amounts in cents, so divide by 100 to get dollars
			amount = parsedAmount.Div(decimal.NewFromInt(100))
		}
	}

	// Get currency
	currency := string(transaction.Details.Totals.CurrencyCode)

	// Get payment method and paid timestamp
	var paymentMethod string
	var paidAt *int64
	if len(transaction.Payments) > 0 {
		payment := transaction.Payments[0]
		paymentMethod = string(payment.MethodDetails.Type)
		if payment.CapturedAt != nil {
			timestamp := parseTimestamp(*payment.CapturedAt)
			paidAt = &timestamp
		}
	}

	response := &dto.PaddlePaymentStatusResponse{
		TransactionID: transaction.ID,
		Status:        string(transaction.Status),
		Amount:        amount,
		Currency:      currency,
		PaymentMethod: paymentMethod,
		PaidAt:        paidAt,
		CreatedAt:     parseTimestamp(transaction.CreatedAt),
		UpdatedAt:     parseTimestamp(transaction.UpdatedAt),
	}

	s.Logger.Infow("retrieved paddle payment status",
		"transaction_id", transactionID,
		"status", response.Status,
		"amount", response.Amount.String(),
		"currency", response.Currency,
	)

	return response, nil
}

// parseTimestamp parses a timestamp string and returns Unix timestamp
func parseTimestamp(timestampStr string) int64 {
	if timestampStr == "" {
		return 0
	}

	// Try parsing RFC3339 format first
	if t, err := time.Parse(time.RFC3339, timestampStr); err == nil {
		return t.Unix()
	}

	// Try parsing RFC3339Nano format
	if t, err := time.Parse(time.RFC3339Nano, timestampStr); err == nil {
		return t.Unix()
	}

	// If parsing fails, return 0
	return 0
}
