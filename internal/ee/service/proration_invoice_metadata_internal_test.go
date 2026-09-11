package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func prorationMetadataTestSubscription() *subscription.Subscription {
	return &subscription.Subscription{
		ID:               "sub_1",
		CustomerID:       "cust_1",
		Currency:         "usd",
		BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
		CurrentPeriodEnd: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC),
	}
}

func TestBuildAggregatedProrationChargeInvoiceRequest_SetsCollapsedInvoiceDisplayName(t *testing.T) {
	req := buildAggregatedProrationChargeInvoiceRequest(prorationMetadataTestSubscription(), nil)
	assert.Equal(t, "Quantity change", types.CollapsedInvoiceDisplayName(req.Metadata))
}

func TestBuildLineItemProrationChargeInvoiceRequest_SetsCollapsedInvoiceDisplayName(t *testing.T) {
	req := buildLineItemProrationChargeInvoiceRequest(
		prorationMetadataTestSubscription(),
		&LineItemProrationSummary{},
		time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		"idemp",
	)
	assert.Equal(t, "Subscription update", types.CollapsedInvoiceDisplayName(req.Metadata))
}
