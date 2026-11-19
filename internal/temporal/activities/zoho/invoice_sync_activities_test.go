package zoho_test

import (
	"context"
	"errors"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	zohoactivities "github.com/flexprice/flexprice/internal/temporal/activities/zoho"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func buildZohoActivityTestContext() context.Context {
	return testutil.SetupContext()
}

func buildZohoActivityFactory(
	connectionRepo *testutil.InMemoryConnectionStore,
	mappingRepo entityintegrationmapping.Repository,
	invoiceRepo invoice.Repository,
	subscriptionRepo subscription.Repository,
) *integration.Factory {
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{
			EncryptionKey: "test-encryption-key-for-unit-tests-only",
		},
	}
	log := logger.NewNoopLogger()

	encSvc, err := security.NewEncryptionService(cfg, log)
	if err != nil {
		panic("failed to create test encryption service: " + err.Error())
	}

	return integration.NewFactory(
		cfg,
		log,
		connectionRepo,
		testutil.NewInMemoryCustomerStore(),
		subscriptionRepo,
		testutil.NewInMemoryPlanStore(),
		invoiceRepo,
		testutil.NewInMemoryPaymentStore(),
		nil, // paymentMethodRepo — not needed in activity unit tests
		testutil.NewInMemoryPriceStore(),
		mappingRepo,
		testutil.NewInMemoryMeterStore(),
		testutil.NewInMemoryFeatureStore(),
		encSvc,
		nil, // TemporalService — not needed in activity unit tests
		testutil.NewInMemoryRedisLocker(nil),
		nil,
		nil,
	)
}

// TestMarkZohoBooksInvoicePaid_NoZohoConnection verifies that when GetZohoBooksIntegration
// returns ErrNotFound (no connection seeded), the activity returns a NonRetryableApplicationError.
func TestMarkZohoBooksInvoicePaid_NoZohoConnection(t *testing.T) {
	ctx := buildZohoActivityTestContext()

	connectionStore := testutil.NewInMemoryConnectionStore()
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	invoiceStore := testutil.NewInMemoryInvoiceStore()
	subStore := testutil.NewInMemorySubscriptionStore()

	factory := buildZohoActivityFactory(connectionStore, mappingStore, invoiceStore, subStore)
	act := zohoactivities.NewInvoiceSyncActivities(factory, nil, logger.NewNoopLogger())

	input := models.NewZohoBooksInvoiceMarkPaidWorkflowInput("inv_no_conn", types.GetTenantID(ctx), types.GetEnvironmentID(ctx))

	err := act.MarkZohoBooksInvoicePaid(ctx, input)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "error must be non-retryable")
	assert.Equal(t, ierr.ErrConnectionNotFound, appErr.Type())
}

type cnActivityEntityScope struct {
	interfaces.BillingEntityScope
	err        error
	invoiceIDs []string
}

func (f *cnActivityEntityScope) ScopeToInvoice(ctx context.Context, invoiceID string) (context.Context, error) {
	f.invoiceIDs = append(f.invoiceIDs, invoiceID)
	if f.err != nil {
		return nil, f.err
	}
	return types.SetBillingEntityID(ctx, "be_"+invoiceID), nil
}

func newCreditNoteSyncActivities(t *testing.T, ctx context.Context, scope interfaces.BillingEntityScope, seed *creditnote.CreditNote) *zohoactivities.CreditNoteSyncActivities {
	t.Helper()
	creditNoteStore := testutil.NewInMemoryCreditNoteStore()
	if seed != nil {
		require.NoError(t, creditNoteStore.Create(ctx, seed))
	}
	factory := buildZohoActivityFactory(
		testutil.NewInMemoryConnectionStore(),
		testutil.NewInMemoryEntityIntegrationMappingStore(),
		testutil.NewInMemoryInvoiceStore(),
		testutil.NewInMemorySubscriptionStore(),
	)
	return zohoactivities.NewCreditNoteSyncActivities(factory, scope, creditNoteStore, testutil.NewInMemoryRefundStore(), logger.NewNoopLogger())
}

func seedCreditNote(ctx context.Context) *creditnote.CreditNote {
	return &creditnote.CreditNote{
		ID:               "cn_1",
		InvoiceID:        "inv_1",
		CreditNoteType:   types.CreditNoteTypeRefund,
		CreditNoteStatus: types.CreditNoteStatusFinalized,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
}

func TestSyncCreditNoteToZoho_CreditNoteNotFound(t *testing.T) {
	ctx := buildZohoActivityTestContext()
	act := newCreditNoteSyncActivities(t, ctx, nil, nil)

	err := act.SyncCreditNoteToZoho(ctx, models.ZohoBooksCreditNoteSyncWorkflowInput{
		CreditNoteID: "cn_missing", TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
	})

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "a missing credit note must not be retried")
	assert.Equal(t, "CreditNoteNotFound", appErr.Type())
}

func TestSyncCreditNoteToZoho_NoZohoConnection(t *testing.T) {
	ctx := buildZohoActivityTestContext()
	scope := &cnActivityEntityScope{}
	act := newCreditNoteSyncActivities(t, ctx, scope, seedCreditNote(ctx))

	err := act.SyncCreditNoteToZoho(ctx, models.ZohoBooksCreditNoteSyncWorkflowInput{
		CreditNoteID: "cn_1", TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
	})

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "a missing connection must not be retried")
	assert.Equal(t, ierr.ErrConnectionNotFound, appErr.Type())
	assert.Equal(t, []string{"inv_1"}, scope.invoiceIDs, "the connection is looked up in the credit note invoice's billing entity")
}

func TestSyncCreditNoteToZoho_BillingEntityScopeErrorIsRetried(t *testing.T) {
	ctx := buildZohoActivityTestContext()
	scopeErr := errors.New("billing entity lookup failed")
	act := newCreditNoteSyncActivities(t, ctx, &cnActivityEntityScope{err: scopeErr}, seedCreditNote(ctx))

	err := act.SyncCreditNoteToZoho(ctx, models.ZohoBooksCreditNoteSyncWorkflowInput{
		CreditNoteID: "cn_1", TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
	})

	require.ErrorIs(t, err, scopeErr)
	var appErr *temporal.ApplicationError
	assert.False(t, errors.As(err, &appErr), "a scope failure stays retryable")
}
