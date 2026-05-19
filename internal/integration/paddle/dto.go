package paddle

import (
	"github.com/flexprice/flexprice/internal/domain/subscription"
)

// EnsureCustomerSyncedRequest is the input to PaddleSyncService.EnsureCustomerSynced.
type EnsureCustomerSyncedRequest struct {
	CustomerID string `json:"customer_id"`
}

// EnsureCustomerSyncedResponse is returned by PaddleSyncService.EnsureCustomerSynced.
type EnsureCustomerSyncedResponse struct {
	PaddleCustomerID string `json:"paddle_customer_id"`
	PaddleAddressID  string `json:"paddle_address_id"`
	// Created is true when the Paddle customer was newly created (false = already existed).
	Created bool `json:"created"`
}

// EnsureBulkProductSyncedRequest is the bulk input to PaddleSyncService.EnsureBulkProductSynced.
type EnsureBulkProductSyncedRequest struct {
	Items []EnsureBulkProductSyncedItem `json:"items"`
}

// EnsureBulkProductSyncedItem is a single item in EnsureBulkProductSyncedRequest.
type EnsureBulkProductSyncedItem struct {
	PriceID string `json:"price_id"`
	Name    string `json:"name"`
}

// EnsureBulkProductSyncedResponse is returned by PaddleSyncService.EnsureBulkProductSynced.
type EnsureBulkProductSyncedResponse struct {
	// PriceIDToPaddleProductID maps FlexPrice priceID → Paddle product ID.
	PriceIDToPaddleProductID map[string]string `json:"price_id_to_paddle_product_id"`
}

// EnsureSubscriptionSyncedRequest is the input to PaddleSyncService.EnsureSubscriptionSynced.
type EnsureSubscriptionSyncedRequest struct {
	// Subscription is the full FlexPrice subscription object.
	// Used for: ID, CustomerID, Currency, CollectionMethod, BillingPeriod, BillingPeriodCount.
	Subscription *subscription.Subscription `json:"subscription"`
	// PriceIDToProductID maps FlexPrice priceIDs → Paddle product IDs (from EnsureBulkProductSynced).
	// Used to build $0 recurring items for the bootstrap transaction.
	PriceIDToProductID map[string]string `json:"price_id_to_product_id"`
}

// EnsureSubscriptionSyncedResponse is returned by PaddleSyncService.EnsureSubscriptionSynced.
type EnsureSubscriptionSyncedResponse struct {
	PaddleSubscriptionID string `json:"paddle_subscription_id"`
	// Created is true when the Paddle subscription was newly created.
	Created bool `json:"created"`
}

// SyncInvoiceRequest is the input to PaddleSyncService.SyncInvoice.
type SyncInvoiceRequest struct {
	InvoiceID string `json:"invoice_id"`
}

// SyncInvoiceResponse is returned by PaddleSyncService.SyncInvoice.
type SyncInvoiceResponse struct {
	PaddleTransactionID  string `json:"paddle_transaction_id"`
	PaddleSubscriptionID string `json:"paddle_subscription_id"`
	CheckoutURL          string `json:"checkout_url"`
	// AlreadySynced is true when the invoice was already synced (no-op path taken).
	AlreadySynced bool `json:"already_synced"`
}
