package webhook_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/integration/stripe/webhook"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	stripeapi "github.com/stripe/stripe-go/v82"
)

type fakeStripeConnectionRepo struct {
	connection.Repository
	conn *connection.Connection
}

func (r *fakeStripeConnectionRepo) GetByProvider(_ context.Context, _ types.SecretProvider) (*connection.Connection, error) {
	return r.conn, nil
}

func TestHandleInvoicePaymentFailed_AlreadyPaidInvoiceIsSkipped(t *testing.T) {
	ctx := testutil.SetupContext()
	log := logger.NewNoopLogger()

	invoiceRepo := testutil.NewInMemoryInvoiceStore()
	require.NoError(t, invoiceRepo.Create(ctx, &invoice.Invoice{
		ID:            "inv_paid",
		CustomerID:    "cust_1",
		PaymentStatus: types.PaymentStatusSucceeded,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))

	mappingRepo := testutil.NewInMemoryEntityIntegrationMappingStore()
	require.NoError(t, mappingRepo.Create(ctx, &entityintegrationmapping.EntityIntegrationMapping{
		ID:               "eim_1",
		EntityID:         "inv_paid",
		EntityType:       types.IntegrationEntityTypeInvoice,
		ProviderType:     "stripe",
		ProviderEntityID: "in_stripe_1",
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}))

	invoiceSyncSvc := stripe.NewInvoiceSyncService(nil, nil, invoiceRepo, mappingRepo, nil, log)
	paymentSvc := stripe.NewPaymentService(nil, nil, invoiceSyncSvc, nil, invoiceRepo, nil, nil, log)
	connRepo := &fakeStripeConnectionRepo{conn: &connection.Connection{
		SyncConfig: &types.SyncConfig{Invoice: &types.EntitySyncConfig{Outbound: true}},
	}}
	h := webhook.NewHandler(nil, nil, paymentSvc, invoiceSyncSvc, nil, nil, mappingRepo, connRepo, log)

	raw, err := json.Marshal(map[string]string{"id": "in_stripe_1", "object": "invoice"})
	require.NoError(t, err)
	event := &stripeapi.Event{ID: "evt_1", Type: "invoice.payment_failed", Data: &stripeapi.EventData{Raw: raw}}

	// Services are nil: reaching MarkSubscriptionIncomplete or the payment record would panic.
	require.NoError(t, h.HandleWebhookEvent(ctx, event, "env_sandbox", &webhook.ServiceDependencies{}))
}
