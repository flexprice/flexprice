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
