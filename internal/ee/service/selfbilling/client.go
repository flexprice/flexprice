package selfbilling

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// billingClient is the slice of the Mumbai billing tenant API self-billing uses.
type billingClient interface {
	EnsureCustomer(ctx context.Context, in customerInput) error
	StartTrial(ctx context.Context, extID string) error
	// LatestSubscription returns the newest non-cancelled self-serve subscription, else the newest cancelled one, else nil.
	LatestSubscription(ctx context.Context, extID string) (*remoteSubscription, error)
	UpdateBillingDetails(ctx context.Context, extID string, d billingDetails) error
	// StartTrialEndCheckout gates the trial's end behind a Stripe checkout on Mumbai.
	StartTrialEndCheckout(ctx context.Context, in checkoutInput) (*remoteCheckout, error)
	GetCheckout(ctx context.Context, id string) (*remoteCheckout, error)
	CancelCheckout(ctx context.Context, id string) error
	PortalSession(ctx context.Context, extID string) (*portalSession, error)
	Summary(ctx context.Context, extID string) (*remoteSummary, error)
	IngestBulk(ctx context.Context, events []usageEvent) error
}

type customerInput struct {
	extID, legacyExtID, name, email, tenantID string
}

type billingDetails struct {
	name, email, line1, line2, city, state, country, postalCode, taxID string
}

type remoteSubscription struct {
	id       string
	status   string // trialing, active, incomplete, cancelled, paused, draft
	trialEnd *time.Time
}

type remoteCheckout struct {
	id, status, url, extID string
	terminal               bool
}

type checkoutInput struct {
	extID, subscriptionID, successURL, failureURL string
}

type portalSession struct {
	url       string
	expiresAt time.Time
}

type usageEvent struct {
	id, name, extID string
	timestamp       time.Time
	properties      map[string]string
}

type remoteSummary struct {
	subscription *remoteSubscription
	invoices     []remoteInvoice
	usage        any // SDK usage summary passed through to the dashboard
}

type remoteInvoice struct {
	id, number, status, paymentStatus, currency, pdfURL string
	total                                               decimal.Decimal
	dueDate                                             *time.Time
}

// metaExternalID tags a Mumbai checkout session with the region customer it belongs to.
const metaExternalID = "self_billing_external_id"

func externalID(region, tenantID string) string {
	return fmt.Sprintf("%s:%s", region, tenantID)
}
