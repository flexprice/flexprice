package paddle_test

import (
	"testing"

	paddleactivities "github.com/flexprice/flexprice/internal/temporal/activities/paddle"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

// TestPullAndUpdatePaddleInvoice_NoPaddleConnection verifies that when GetPaddleIntegration
// returns ErrNotFound (no connection seeded), the activity returns a NonRetryableApplicationError.
func TestPullAndUpdatePaddleInvoice_NoPaddleConnection(t *testing.T) {
	ctx := buildActivityTestContext()

	connectionStore := testutil.NewInMemoryConnectionStore()
	mappingStore := testutil.NewInMemoryEntityIntegrationMappingStore()
	invoiceStore := testutil.NewInMemoryInvoiceStore()
	subStore := testutil.NewInMemorySubscriptionStore()

	factory := buildActivityFactory(connectionStore, mappingStore, invoiceStore, subStore)
	act := paddleactivities.NewInvoiceSyncActivities(factory, nil, buildTestActivityLogger())

	input := models.PaddleInvoicePullSyncWorkflowInput{
		InvoiceID:     "inv_no_conn",
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
	}

	err := act.PullAndUpdatePaddleInvoice(ctx, input)
	require.Error(t, err)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.True(t, appErr.NonRetryable(), "error must be non-retryable")
	assert.Equal(t, "ConnectionNotFound", appErr.Type())
}
