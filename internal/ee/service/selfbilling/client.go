package selfbilling

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// billingClient is the slice of the Flexprice Cloud API self-billing uses.
type billingClient interface {
	// EnsureCustomer finds or creates the tenant's customer on Flexprice Cloud.
	EnsureCustomer(ctx context.Context, in customerInput) error

	// StartTrial subscribes the customer to the self-serve plan unless it already has a subscription on any plan; created reports a new subscription.
	StartTrial(ctx context.Context, extID string) (created bool, err error)

	// LatestSubscription returns the newest non-cancelled subscription on any plan, else the newest cancelled one, else nil.
	LatestSubscription(ctx context.Context, extID string) (*remoteSubscription, error)

	// UpdateBillingDetails sets the customer's address and tax ID, keeping its other metadata.
	UpdateBillingDetails(ctx context.Context, extID string, details billingDetails) error

	// StartTrialEndCheckout gates the trial's end behind a configured checkout provider on Flexprice Cloud, resuming an open one.
	StartTrialEndCheckout(ctx context.Context, in checkoutInput) (*remoteCheckout, error)

	// GetCheckout returns a checkout session by ID.
	GetCheckout(ctx context.Context, id string) (*remoteCheckout, error)

	// CancelCheckout cancels an open checkout session.
	CancelCheckout(ctx context.Context, id string) error

	// PortalSession opens a short-lived customer portal session.
	PortalSession(ctx context.Context, extID string) (*portalSession, error)

	// Summary returns the customer's subscription, recent invoices and usage.
	Summary(ctx context.Context, extID string) (*remoteSummary, error)

	// IngestBulk sends usage events; Flexprice Cloud drops a re-sent event ID as a duplicate.
	IngestBulk(ctx context.Context, events []usageEvent) error
}

type customerInput struct {
	extID       string
	legacyExtID string
	name        string
	email       string
	tenantID    string
}

type billingDetails struct {
	name       string
	email      string
	line1      string
	line2      string
	city       string
	state      string
	country    string
	postalCode string
	taxID      string
}

func (d billingDetails) hasAddress() bool {
	return d.line1 != "" || d.line2 != "" || d.city != "" || d.state != "" || d.country != "" || d.postalCode != ""
}

type remoteSubscription struct {
	id       string
	status   types.SubscriptionStatus
	trialEnd *time.Time
}

type checkoutInput struct {
	extID          string
	subscriptionID string
	successURL     string
	failureURL     string
}

type remoteCheckout struct {
	id       string
	status   types.CheckoutStatus
	url      string
	extID    string
	terminal bool
}

type portalSession struct {
	url       string
	expiresAt time.Time
}

type remoteSummary struct {
	subscription *remoteSubscription
	plan         *dto.SelfBillingPlanResponse
	invoices     []remoteInvoice
	usage        *dto.CustomerUsageSummaryResponse
}

type remoteInvoice struct {
	id            string
	number        string
	status        types.InvoiceStatus
	paymentStatus types.PaymentStatus
	total         decimal.Decimal
	currency      string
	dueDate       *time.Time
	pdfURL        string
}

type usageEvent struct {
	id         string
	name       string
	extID      string
	timestamp  time.Time
	properties map[string]string
}

func externalID(region, tenantID string) string {
	return fmt.Sprintf("%s:%s", region, tenantID)
}
