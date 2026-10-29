package invoice

import (
	"context"
	"testing"

	domain "github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	models "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type retryInvoiceRepository struct {
	domain.Repository
	invoice *domain.Invoice
}

func (r *retryInvoiceRepository) Get(ctx context.Context, id string) (*domain.Invoice, error) {
	return r.invoice, nil
}

func TestFinalizedInvoiceRetryReachesPayment(t *testing.T) {
	for _, status := range []types.PaymentStatus{types.PaymentStatusPending, types.PaymentStatusSucceeded} {
		t.Run(string(status), func(t *testing.T) {
			repository := &retryInvoiceRepository{invoice: &domain.Invoice{ID: "inv", InvoiceStatus: types.InvoiceStatusFinalized, PaymentStatus: status, BaseModel: types.BaseModel{Status: types.StatusPublished}}}
			activities := NewInvoiceActivities(service.ServiceParams{InvoiceRepo: repository}, logger.NewNoopLogger())
			for range 2 {
				result, err := activities.FinalizeInvoiceActivity(context.Background(), models.FinalizeInvoiceActivityInput{InvoiceID: "inv", TenantID: "tenant", EnvironmentID: "env"})
				require.NoError(t, err)
				require.True(t, result.Success)
				require.False(t, result.Skipped)
			}
		})
	}
}

func TestInvoiceActivityDoesNotPayVoidSkippedOrPaid(t *testing.T) {
	for _, status := range []types.InvoiceStatus{types.InvoiceStatusVoided, types.InvoiceStatusSkipped, types.InvoiceStatusFinalized} {
		t.Run(string(status), func(t *testing.T) {
			repository := &retryInvoiceRepository{invoice: &domain.Invoice{ID: "inv", InvoiceStatus: status, PaymentStatus: types.PaymentStatusSucceeded, BaseModel: types.BaseModel{Status: types.StatusPublished}}}
			activities := NewInvoiceActivities(service.ServiceParams{InvoiceRepo: repository}, logger.NewNoopLogger())
			for range 2 {
				result, err := activities.AttemptInvoicePaymentActivity(context.Background(), models.PaymentActivityInput{InvoiceID: "inv", TenantID: "tenant", EnvironmentID: "env"})
				require.NoError(t, err)
				require.True(t, result.Success)
			}
			finalized, err := activities.FinalizeInvoiceActivity(context.Background(), models.FinalizeInvoiceActivityInput{InvoiceID: "inv", TenantID: "tenant", EnvironmentID: "env"})
			require.NoError(t, err)
			require.Equal(t, status != types.InvoiceStatusFinalized, finalized.Skipped)
		})
	}
}
