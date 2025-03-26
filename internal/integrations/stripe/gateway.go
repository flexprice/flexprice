package stripe

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integrations"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/client"
)

// StripeGateway implements the IntegrationGateway interface for Stripe
type StripeGateway struct {
	logger *logger.Logger
	client *client.API
	apiKey string
}

// NewStripeGateway creates a new Stripe gateway
func NewStripeGateway(apiKey string, logger *logger.Logger) (integrations.IntegrationGateway, error) {
	if apiKey == "" {
		return nil, ierr.NewError("Stripe API key is required").
			WithHint("Please provide a valid Stripe API key").
			Mark(ierr.ErrValidation)
	}

	sc := &client.API{}
	sc.Init(apiKey, nil)

	return &StripeGateway{
		logger: logger,
		client: sc,
		apiKey: apiKey,
	}, nil
}

// GetProviderName returns the name of the provider
func (g *StripeGateway) GetProviderName() string {
	return "stripe"
}

// GetCapabilities returns the capabilities supported by this gateway
func (g *StripeGateway) GetCapabilities() []types.IntegrationCapability {
	return []types.IntegrationCapability{
		types.CapabilityCustomer,
		types.CapabilityPaymentMethod,
		types.CapabilityPayment,
	}
}

// SupportsCapability checks if a capability is supported
func (g *StripeGateway) SupportsCapability(capability types.IntegrationCapability) bool {
	for _, c := range g.GetCapabilities() {
		if c == capability {
			return true
		}
	}
	return false
}

// CreateCustomer creates a customer in Stripe
func (g *StripeGateway) CreateCustomer(ctx context.Context, cust *customer.Customer) (string, error) {
	g.logger.Infow("creating customer in Stripe", "customer_id", cust.ID)

	// Create customer params
	params := &stripe.CustomerParams{
		Name:  stripe.String(cust.Name),
		Email: stripe.String(cust.Email),
		Metadata: map[string]string{
			"flexprice_customer_id": cust.ID,
			"flexprice_tenant_id":   cust.TenantID,
		},
	}

	// Add address if available
	if cust.AddressLine1 != "" {
		params.Address = &stripe.AddressParams{
			Line1:      stripe.String(cust.AddressLine1),
			Line2:      stripe.String(cust.AddressLine2),
			City:       stripe.String(cust.AddressCity),
			State:      stripe.String(cust.AddressState),
			PostalCode: stripe.String(cust.AddressPostalCode),
			Country:    stripe.String(cust.AddressCountry),
		}
	}

	// Create customer in Stripe
	stripeCustomer, err := g.client.Customers.New(params)
	if err != nil {
		return "", g.convertStripeError(err)
	}

	g.logger.Infow("customer created in Stripe",
		"customer_id", cust.ID,
		"stripe_customer_id", stripeCustomer.ID)

	return stripeCustomer.ID, nil
}

// UpdateCustomer updates a customer in Stripe
func (g *StripeGateway) UpdateCustomer(ctx context.Context, cust *customer.Customer, providerID string) error {
	g.logger.Infow("updating customer in Stripe",
		"customer_id", cust.ID,
		"stripe_customer_id", providerID)

	// Create customer update params
	params := &stripe.CustomerParams{
		Name:  stripe.String(cust.Name),
		Email: stripe.String(cust.Email),
	}

	// Add address if available
	if cust.AddressLine1 != "" {
		params.Address = &stripe.AddressParams{
			Line1:      stripe.String(cust.AddressLine1),
			Line2:      stripe.String(cust.AddressLine2),
			City:       stripe.String(cust.AddressCity),
			State:      stripe.String(cust.AddressState),
			PostalCode: stripe.String(cust.AddressPostalCode),
			Country:    stripe.String(cust.AddressCountry),
		}
	}

	// Update customer in Stripe
	_, err := g.client.Customers.Update(providerID, params)
	if err != nil {
		return g.convertStripeError(err)
	}

	g.logger.Infow("customer updated in Stripe",
		"customer_id", cust.ID,
		"stripe_customer_id", providerID)

	return nil
}

// ListPaymentMethods lists payment methods for a customer in Stripe
func (g *StripeGateway) ListPaymentMethods(ctx context.Context, customerProviderID string) ([]integrations.PaymentMethodInfo, error) {
	g.logger.Infow("listing payment methods in Stripe", "stripe_customer_id", customerProviderID)

	params := &stripe.PaymentMethodListParams{
		Customer: stripe.String(customerProviderID),
		Type:     stripe.String("card"),
	}

	iter := g.client.PaymentMethods.List(params)
	var paymentMethods []integrations.PaymentMethodInfo

	for iter.Next() {
		pm := iter.PaymentMethod()

		// Skip if card details not available
		if pm.Card == nil {
			continue
		}

		paymentMethod := integrations.PaymentMethodInfo{
			ID:          pm.ID,
			Type:        string(pm.Type),
			Last4:       pm.Card.Last4,
			ExpiryMonth: int(pm.Card.ExpMonth),
			ExpiryYear:  int(pm.Card.ExpYear),
			Brand:       string(pm.Card.Brand),
			Metadata:    pm.Metadata,
		}

		paymentMethods = append(paymentMethods, paymentMethod)
	}

	if err := iter.Err(); err != nil {
		return nil, g.convertStripeError(err)
	}

	g.logger.Infow("payment methods retrieved from Stripe",
		"stripe_customer_id", customerProviderID,
		"count", len(paymentMethods))

	return paymentMethods, nil
}

// GetDefaultPaymentMethod gets the default payment method for a customer in Stripe
func (g *StripeGateway) GetDefaultPaymentMethod(ctx context.Context, customerProviderID string) (string, error) {
	g.logger.Infow("getting default payment method in Stripe", "stripe_customer_id", customerProviderID)

	// Get the customer to check for default payment method
	customer, err := g.client.Customers.Get(customerProviderID, nil)
	if err != nil {
		return "", g.convertStripeError(err)
	}

	// Check if customer has a default payment method
	if customer.InvoiceSettings != nil && customer.InvoiceSettings.DefaultPaymentMethod != nil {
		defaultPMID := customer.InvoiceSettings.DefaultPaymentMethod.ID
		g.logger.Infow("found default payment method from customer invoice settings",
			"stripe_customer_id", customerProviderID,
			"payment_method_id", defaultPMID)
		return defaultPMID, nil
	}

	// Fallback: get first payment method if no default is set
	params := &stripe.PaymentMethodListParams{
		Customer: stripe.String(customerProviderID),
		Type:     stripe.String("card"),
	}
	// Set limit to 1 to reduce API overhead
	params.Limit = stripe.Int64(1)

	iter := g.client.PaymentMethods.List(params)
	if iter.Next() {
		pm := iter.PaymentMethod()
		g.logger.Infow("using first available payment method as default",
			"stripe_customer_id", customerProviderID,
			"payment_method_id", pm.ID)
		return pm.ID, nil
	}

	if err := iter.Err(); err != nil {
		return "", g.convertStripeError(err)
	}

	// No payment methods found
	return "", ierr.NewError("no payment methods found for customer").
		WithHint("Please add a payment method for this customer in Stripe").
		Mark(ierr.ErrNotFound)
}

// CreatePayment creates a payment in Stripe
func (g *StripeGateway) CreatePayment(ctx context.Context, payment *payment.Payment, options map[string]interface{}) (string, error) {
	g.logger.Infow("creating payment in Stripe", "payment_id", payment.ID)

	// Generate idempotency key to prevent duplicate payments
	idempotencyKey := fmt.Sprintf("payment_%s", payment.ID)

	// Validate payment
	if payment.PaymentMethodID == "" {
		return "", ierr.NewError("payment method ID is required").
			WithHint("Please provide a valid payment method ID").
			Mark(ierr.ErrValidation)
	}

	// Get the customer ID from options or lookup based on payment
	var customerID string
	if options != nil {
		if custID, ok := options["customer_id"].(string); ok && custID != "" {
			customerID = custID
		}
	}

	if customerID == "" {
		return "", ierr.NewError("customer ID is required").
			WithHint("Please provide a valid customer ID").
			Mark(ierr.ErrValidation)
	}

	// Create payment intent params
	params := &stripe.PaymentIntentParams{
		Amount:             stripe.Int64(payment.Amount.IntPart()),
		Currency:           stripe.String(string(payment.Currency)),
		Customer:           stripe.String(customerID),
		PaymentMethod:      stripe.String(payment.PaymentMethodID),
		ConfirmationMethod: stripe.String(string(stripe.PaymentIntentConfirmationMethodAutomatic)),
		Confirm:            stripe.Bool(true),
		Metadata: map[string]string{
			"flexprice_payment_id": payment.ID,
			"flexprice_tenant_id":  payment.TenantID,
		},
	}

	// Use metadata as description if available
	if description, ok := payment.Metadata["description"]; ok {
		params.Description = stripe.String(description)
		// Also use as statement descriptor but truncate to 22 chars max
		if len(description) > 22 {
			params.StatementDescriptor = stripe.String(description[:22])
		} else {
			params.StatementDescriptor = stripe.String(description)
		}
	}

	// Use the idempotency key
	params.IdempotencyKey = stripe.String(idempotencyKey)

	// Create payment intent in Stripe
	paymentIntent, err := g.client.PaymentIntents.New(params)
	if err != nil {
		return "", g.convertStripeError(err)
	}

	g.logger.Infow("payment created in Stripe",
		"payment_id", payment.ID,
		"stripe_payment_intent_id", paymentIntent.ID,
		"status", paymentIntent.Status)

	return paymentIntent.ID, nil
}

// convertStripeError converts Stripe errors to internal errors
func (g *StripeGateway) convertStripeError(err error) error {
	if stripeErr, ok := err.(*stripe.Error); ok {
		message := stripeErr.Msg
		hint := stripeErr.DocURL

		// Create proper error based on the error type
		internalErr := ierr.NewError(fmt.Sprintf("Stripe error: %s", message)).
			WithHint(hint)

		switch stripeErr.Type {
		case stripe.ErrorTypeCard:
			// Card errors - like declined charges
			return internalErr.Mark(ierr.ErrValidation)
		case stripe.ErrorTypeInvalidRequest:
			// Invalid parameters were supplied to Stripe's API
			return internalErr.Mark(ierr.ErrValidation)
		case stripe.ErrorTypeAPI:
			// Stripe API errors - network communication, rate limits, etc.
			return internalErr.Mark(ierr.ErrHTTPClient)
		default:
			// Unknown error type
			return internalErr.Mark(ierr.ErrSystem)
		}
	}

	// Not a Stripe error
	return ierr.NewError(fmt.Sprintf("Stripe error: %s", err.Error())).
		Mark(ierr.ErrSystem)
}
