package paddle

// Metadata keys used across the Paddle integration.
// Always use these constants instead of raw string literals to prevent typos.
const (
	// Connection metadata keys (stored on the Paddle connection record)
	ConnKeyRedirectURL = "redirect_url"
	ConnKeyCheckoutURL = "checkout_url" // base URL matching the Paddle default payment link, e.g. https://example.com/checkout

	// Customer / address mapping metadata keys
	MetaKeyPaddleCustomerID    = "paddle_customer_id"
	MetaKeyPaddleAddressID     = "paddle_address_id"
	MetaKeyPaddleCustomerEmail = "paddle_customer_email"

	// Invoice / transaction mapping metadata keys
	MetaKeyPaddleTransactionID = "paddle_transaction_id"
	MetaKeyPaddleCheckoutURL   = "paddle_checkout_url"
	MetaKeyInvoiceNumber       = "invoice_number"

	// Subscription mapping metadata keys
	MetaKeyPaddleSubscriptionID = "paddle_subscription_id"

	// Price / product mapping metadata keys
	MetaKeyPaddlePriceID   = "paddle_price_id"
	MetaKeyPaddleProductID = "paddle_product_id"

	// Payment metadata keys
	MetaKeyPaddlePaymentAttemptID  = "paddle_payment_attempt_id"
	MetaKeyPaddleCardLast4         = "paddle_card_last4"
	MetaKeyPaddlePaymentMethodType = "paddle_payment_method_type"
	MetaKeyPaddlePaymentSource     = "payment_source"

	// Shared mapping metadata keys
	MetaKeyCreatedVia = "created_via"
	MetaKeySyncedAt   = "synced_at"

	// Values for MetaKeyCreatedVia
	CreatedViaFlexpriceToProvider         = "flexprice_to_provider"
	CreatedViaFlexpriceToProviderBackfill = "flexprice_to_provider_backfill"
	CreatedViaProviderToFlexprice         = "provider_to_flexprice"
)
