package paddle

import (
	"github.com/flexprice/flexprice/internal/domain/subscription"
)

// EnsureCustomerSyncedRequest is the input to PaddleSyncService.EnsureCustomerSynced.
type EnsureCustomerSyncedRequest struct {
	CustomerID string
}

// EnsureCustomerSyncedResponse is returned by PaddleSyncService.EnsureCustomerSynced.
type EnsureCustomerSyncedResponse struct {
	PaddleCustomerID string
	PaddleAddressID  string
	Created          bool
}

// EnsureBulkProductSyncedRequest is the bulk input to PaddleSyncService.EnsureBulkProductSynced.
type EnsureBulkProductSyncedRequest struct {
	Items []EnsureBulkProductSyncedItem
}

// EnsureBulkProductSyncedItem is a single item in EnsureBulkProductSyncedRequest.
type EnsureBulkProductSyncedItem struct {
	PriceID string
	Name    string
}

// EnsureBulkProductSyncedResponse is returned by PaddleSyncService.EnsureBulkProductSynced.
type EnsureBulkProductSyncedResponse struct {
	// PriceIDToPaddleProductID maps FlexPrice priceID → Paddle product ID.
	PriceIDToPaddleProductID map[string]string
}

// EnsureSubscriptionSyncedRequest is the input to PaddleSyncService.EnsureSubscriptionSynced.
type EnsureSubscriptionSyncedRequest struct {
	// Subscription is the full FlexPrice subscription object.
	// Used for: ID, CustomerID, Currency, CollectionMethod, BillingPeriod, BillingPeriodCount.
	Subscription *subscription.Subscription
	// PriceIDToProductID maps FlexPrice priceIDs → Paddle product IDs (from EnsureBulkProductSynced).
	// Used to build $0 recurring items for the bootstrap transaction.
	PriceIDToProductID map[string]string
}

// EnsureSubscriptionSyncedResponse is returned by PaddleSyncService.EnsureSubscriptionSynced.
type EnsureSubscriptionSyncedResponse struct {
	PaddleSubscriptionID string
	Created              bool
}

// SyncInvoiceRequest is the input to PaddleSyncService.SyncInvoice.
type SyncInvoiceRequest struct {
	InvoiceID string
}

// SyncInvoiceResponse is returned by PaddleSyncService.SyncInvoice.
type SyncInvoiceResponse struct {
	PaddleTransactionID string
	CheckoutURL         string
	AlreadySynced       bool
}
