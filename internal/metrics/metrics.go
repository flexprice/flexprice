package metrics

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MeterName scopes app metrics so tracing views can target them.
const MeterName = "github.com/flexprice/flexprice/internal/metrics"

var meter = otel.Meter(MeterName)

// Name is a metric name; every name needs an entry in catalog.
type Name string

const (
	CheckoutSessions   Name = "checkout.sessions"
	PaymentTransitions Name = "payment.transitions"
	PaymentAttempts    Name = "payment.attempts"
	WebhookDeliveries  Name = "webhook.outbound.deliveries"
	InvoiceTransitions Name = "invoice.transitions"
	RefundTransitions  Name = "refund.transitions"
	GatewayWebhooks    Name = "gateway.webhooks"
)

type definition struct {
	description string
	unit        string // UCUM annotation naming what is counted
}

var catalog = map[Name]definition{
	CheckoutSessions:   {"Checkout sessions by status reached", "{session}"},
	PaymentTransitions: {"Payment status changes by new status, including creation", "{transition}"},
	PaymentAttempts:    {"Gateway charge attempts by status", "{attempt}"},
	WebhookDeliveries:  {"Outbound deliveries of subscribed webhook events by outcome", "{delivery}"},
	InvoiceTransitions: {"Invoice status changes by new status, including draft creation", "{transition}"},
	RefundTransitions:  {"Refund status changes by new status, including creation", "{transition}"},
	GatewayWebhooks:    {"Inbound gateway webhooks by processing outcome; unhandled types are labelled other", "{webhook}"},
}

// counters is built once at init and only read afterwards, so concurrent lookups are safe.
var counters = lo.MapValues(catalog, func(def definition, name Name) metric.Int64Counter {
	c, _ := meter.Int64Counter(string(name), metric.WithDescription(def.description), metric.WithUnit(def.unit))
	return c
})

type LabelKey string

const (
	KeyTenantID      LabelKey = "tenant_id"
	KeyEnvironmentID LabelKey = "environment_id"
	KeyProvider      LabelKey = "provider"
	KeyOutcome       LabelKey = "outcome"
	KeyStatus        LabelKey = "status"
	KeyMethodType    LabelKey = "method_type"
	KeyEventType     LabelKey = "event_type"
	KeyTransport     LabelKey = "transport"
	KeyAction        LabelKey = "action"
	KeyChargeMode    LabelKey = "charge_mode"
	KeyCheckout      LabelKey = "checkout"
	KeyInvoiceType   LabelKey = "invoice_type"
	KeyBillingReason LabelKey = "billing_reason"
	KeyDestination   LabelKey = "destination"
	KeyReason        LabelKey = "reason"
)

type Label struct {
	key   LabelKey
	value string
}

func L(key LabelKey, value string) Label { return Label{key: key, value: value} }

// RecordCounter increments the counter name by n; names missing from catalog are ignored.
func RecordCounter(ctx context.Context, name Name, n int64, labels ...Label) {
	c, ok := counters[name]
	if !ok {
		return
	}

	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if !tenantAllowed(tenantID, environmentID) {
		return
	}

	attrs := make([]attribute.KeyValue, 0, len(labels)+2)
	for _, l := range labels {
		attrs = append(attrs, attribute.String(string(l.key), l.value))
	}

	// Identity labels go last: OTel keeps the last value for a repeated key, so callers cannot override them.
	if tenantID != "" {
		attrs = append(attrs, attribute.String(string(KeyTenantID), tenantID), attribute.String(string(KeyEnvironmentID), environmentID))
	}

	// Inside a DB transaction, record only once it commits.
	add := func() { c.Add(ctx, n, metric.WithAttributes(attrs...)) }
	if !types.RegisterPostCommit(ctx, add) {
		add()
	}
}

var allowedTenants atomic.Pointer[map[string]struct{}]

// SetTenantAllowlist limits records to "tenant_id:environment_id" or "tenant_id" entries; empty allows all.
func SetTenantAllowlist(entries []string) {
	allowed := lo.Keyify(lo.FilterMap(entries, func(entry string, _ int) (string, bool) {
		entry = strings.TrimSpace(entry)
		if !strings.Contains(entry, ":") {
			entry += ":*"
		}
		return entry, entry != ":*"
	}))
	allowedTenants.Store(&allowed)
}

func tenantAllowed(tenantID, environmentID string) bool {
	allowed := allowedTenants.Load()
	if allowed == nil || len(*allowed) == 0 {
		return true
	}
	_, tenant := (*allowed)[tenantID+":*"]
	_, pair := (*allowed)[tenantID+":"+environmentID]
	return tenant || pair
}
