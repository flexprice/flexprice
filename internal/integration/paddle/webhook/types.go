package webhook

// PaddleEventType represents Paddle webhook event types
type PaddleEventType string

const (
	// EventTransactionCompleted occurs when a transaction is completed
	EventTransactionCompleted PaddleEventType = "transaction.completed"
	// EventCustomerCreated occurs when a customer is created
	EventCustomerCreated PaddleEventType = "customer.created"
	// EventAddressCreated occurs when an address is created
	EventAddressCreated PaddleEventType = "address.created"
	// EventSubscriptionActivated occurs when a Paddle subscription is activated
	// (customer has completed checkout and payment method is saved).
	EventSubscriptionActivated PaddleEventType = "subscription.activated"
)
