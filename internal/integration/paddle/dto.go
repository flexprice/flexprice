package paddle

import "github.com/shopspring/decimal"

// PaddleInvoiceSyncRequest represents a request to sync FlexPrice invoice to Paddle
type PaddleInvoiceSyncRequest struct {
	InvoiceID string // FlexPrice invoice ID to sync
}

// PaddleInvoiceSyncResponse represents the response after syncing invoice to Paddle
type PaddleInvoiceSyncResponse struct {
	PaddleTransactionID string          // Paddle transaction ID (txn_xxx)
	InvoiceNumber       string          // Invoice number from Paddle
	Status              string          // Transaction status (billed, etc.)
	CheckoutURL         string          // Payment URL if enable_checkout is true
	Amount              decimal.Decimal // Pre-tax subtotal (sum of line items before tax)
	Currency            string          // Currency code
	TaxAmount           decimal.Decimal // Tax calculated by Paddle (Paddle is Merchant of Record)
	GrandTotal          decimal.Decimal // Grand total = subtotal + tax (what Paddle charges the customer)
}

// EnsureCustomerSyncedRequest is the input to PaddleSyncService.EnsureCustomerSynced.
type EnsureCustomerSyncedRequest struct {
	CustomerID string
}

// EnsureCustomerSyncedResponse is returned by PaddleSyncService.EnsureCustomerSynced.
type EnsureCustomerSyncedResponse struct {
	PaddleCustomerID string
	PaddleAddressID  string
	// Created is true when the Paddle customer was newly created (false = already existed).
	Created bool
}

// EnsureProductSyncedRequest is the input to PaddleSyncService.EnsureProductSynced.
type EnsureProductSyncedRequest struct {
	// PriceID is the FlexPrice price entity ID used as the mapping key.
	PriceID  string
	Name     string
	Amount   decimal.Decimal
	Currency string
}

// EnsureProductSyncedResponse is returned by PaddleSyncService.EnsureProductSynced.
type EnsureProductSyncedResponse struct {
	PaddleProductID string
	// Created is true when the Paddle product+price were newly created.
	Created bool
}

// EnsureProductsSyncedRequest is the bulk input to PaddleSyncService.EnsureProductsSynced.
type EnsureProductsSyncedRequest struct {
	Items []EnsureProductSyncedRequest
}

// EnsureProductsSyncedResponse is returned by PaddleSyncService.EnsureProductsSynced.
type EnsureProductsSyncedResponse struct {
	// PriceIDToPaddleProductID maps FlexPrice priceID → Paddle product ID.
	PriceIDToPaddleProductID map[string]string
}

// EnsureSubscriptionSyncedRequest is the input to PaddleSyncService.EnsureSubscriptionSynced.
type EnsureSubscriptionSyncedRequest struct {
	// SubscriptionID is the FlexPrice subscription entity ID.
	SubscriptionID string
	// CustomerID is the FlexPrice customer entity ID (used to ensure customer exists in Paddle first).
	CustomerID string
}

// EnsureSubscriptionSyncedResponse is returned by PaddleSyncService.EnsureSubscriptionSynced.
type EnsureSubscriptionSyncedResponse struct {
	PaddleSubscriptionID string
	// Created is true when the Paddle subscription was newly created.
	Created bool
}

// SyncInvoiceRequest is the input to PaddleSyncService.SyncInvoice.
type SyncInvoiceRequest struct {
	InvoiceID string
}

// SyncInvoiceResponse is returned by PaddleSyncService.SyncInvoice.
type SyncInvoiceResponse struct {
	PaddleTransactionID string
	CheckoutURL         string
	// AlreadySynced is true when the invoice was already synced (no-op path taken).
	AlreadySynced bool
}
