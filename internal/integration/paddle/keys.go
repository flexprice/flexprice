package paddle

// Metadata keys used across the Paddle integration.
// Always use these constants instead of raw string literals to prevent typos.
const (
	// Connection metadata keys (stored on the Paddle connection record)
	ConnKeyRedirectURL         = "redirect_url"
	ConnKeyZeroDollarProductID = "paddle_zero_dollar_product_id"
	ConnKeyZeroDollarPriceID   = "paddle_zero_dollar_price_id"

	// Customer / address mapping metadata keys
	MetaKeyPaddleCustomerID    = "paddle_customer_id"
	MetaKeyPaddleAddressID     = "paddle_address_id"
	MetaKeyPaddleCustomerEmail = "paddle_customer_email"

	// Invoice / transaction mapping metadata keys
	MetaKeyPaddleTransactionID = "paddle_transaction_id"
	MetaKeyPaddleCheckoutURL   = "paddle_checkout_url"
	MetaKeyPaddleSubtotal      = "paddle_subtotal"
	MetaKeyPaddleTaxAmount     = "paddle_tax_amount"
	MetaKeyPaddleTaxRate       = "paddle_tax_rate"
	MetaKeyPaddleGrandTotal    = "paddle_grand_total"
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
	MetaKeyPaddlePaymentSource     = "paddle_external"

	// Shared mapping metadata keys
	MetaKeyCreatedVia = "created_via"
	MetaKeySyncedAt   = "synced_at"

	// Values for MetaKeyCreatedVia
	CreatedViaFlexpriceToProvider         = "flexprice_to_provider"
	CreatedViaFlexpriceToProviderBackfill = "flexprice_to_provider_backfill"
	CreatedViaProviderToFlexprice         = "provider_to_flexprice"
)
