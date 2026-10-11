package activity

// Declared actions, one block per entity. created, updated and deleted come from the hook and are not listed.

// Customer
const (
	CustomerArchived = "customer.archived"
)

// Subscription
const (
	SubscriptionArchived    = "subscription.archived"
	SubscriptionCancelled   = "subscription.cancelled"
	SubscriptionPlanChanged = "subscription.plan_changed"
	SubscriptionActivated   = "subscription.activated"
)

// Invoice
const (
	InvoiceDeleted   = "invoice.deleted"
	InvoiceVoided    = "invoice.voided"
	InvoiceFinalized = "invoice.finalized"
	InvoiceRefunded  = "invoice.refunded"
	InvoicePaid      = "invoice.paid"
)

// CreditNote
const (
	CreditNoteVoided    = "credit_note.voided"
	CreditNoteFinalized = "credit_note.finalized"
)

// Wallet
const (
	WalletTerminated = "wallet.terminated"
	WalletToppedUp   = "wallet.topped_up"
)

// Payment
const (
	PaymentArchived  = "payment.archived"
	PaymentVoided    = "payment.voided"
	PaymentRefunded  = "payment.refunded"
	PaymentFailed    = "payment.failed"
	PaymentSucceeded = "payment.succeeded"
)

// Refund
const (
	RefundFailed    = "refund.failed"
	RefundSucceeded = "refund.succeeded"
)

// CheckoutSession
const (
	CheckoutSessionArchived  = "checkout_session.archived"
	CheckoutSessionCompleted = "checkout_session.completed"
	CheckoutSessionFailed    = "checkout_session.failed"
	CheckoutSessionExpired   = "checkout_session.expired"
)

// Plan
const (
	PlanArchived = "plan.archived"
)

// Price
const (
	PriceTerminated = "price.terminated"
)

// Feature
const (
	FeatureArchived = "feature.archived"
)
