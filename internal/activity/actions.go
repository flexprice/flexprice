package activity

// Action names what happened to an entity, as <entity>.<verb>.
type Action string

// Declared actions, one block per entity. created, updated and deleted come from the hook and are not listed.

// Customer
const (
	CustomerArchived Action = "customer.archived"
)

// Subscription
const (
	SubscriptionArchived    Action = "subscription.archived"
	SubscriptionCancelled   Action = "subscription.cancelled"
	SubscriptionPlanChanged Action = "subscription.plan_changed"
	SubscriptionActivated   Action = "subscription.activated"
)

// Invoice
const (
	InvoiceDeleted   Action = "invoice.deleted"
	InvoiceVoided    Action = "invoice.voided"
	InvoiceFinalized Action = "invoice.finalized"
	InvoiceRefunded  Action = "invoice.refunded"
	InvoicePaid      Action = "invoice.paid"
)

// CreditNote
const (
	CreditNoteVoided    Action = "credit_note.voided"
	CreditNoteFinalized Action = "credit_note.finalized"
)

// Wallet
const (
	WalletTerminated Action = "wallet.terminated"
	WalletToppedUp   Action = "wallet.topped_up"
)

// Payment
const (
	PaymentArchived  Action = "payment.archived"
	PaymentVoided    Action = "payment.voided"
	PaymentRefunded  Action = "payment.refunded"
	PaymentFailed    Action = "payment.failed"
	PaymentSucceeded Action = "payment.succeeded"
)

// Refund
const (
	RefundFailed    Action = "refund.failed"
	RefundSucceeded Action = "refund.succeeded"
)

// CheckoutSession
const (
	CheckoutSessionArchived  Action = "checkout_session.archived"
	CheckoutSessionCompleted Action = "checkout_session.completed"
	CheckoutSessionFailed    Action = "checkout_session.failed"
	CheckoutSessionExpired   Action = "checkout_session.expired"
)

// Plan
const (
	PlanArchived Action = "plan.archived"
)

// Price
const (
	PriceTerminated Action = "price.terminated"
)

// Feature
const (
	FeatureArchived Action = "feature.archived"
)
