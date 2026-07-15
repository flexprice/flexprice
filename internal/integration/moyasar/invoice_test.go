package moyasar

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	return log
}

// stubMoyasarClient overrides only GetConnection; buildInvoiceRequest never calls any
// other MoyasarClient method, so the embedded nil interface is safe.
type stubMoyasarClient struct {
	MoyasarClient
	conn *connection.Connection
	err  error
}

func (s *stubMoyasarClient) GetConnection(ctx context.Context) (*connection.Connection, error) {
	return s.conn, s.err
}

func TestBuildInvoiceRequest_SetsSuccessAndBackURLFromConnectionMetadata(t *testing.T) {
	svc := &InvoiceSyncService{
		client: &stubMoyasarClient{
			conn: &connection.Connection{
				Metadata: map[string]interface{}{
					ConnKeySuccessURL: "https://example.com/success",
					ConnKeyCancelURL:  "https://example.com/cancel",
				},
			},
		},
		logger: mustTestLogger(t),
	}

	inv := &invoice.Invoice{
		ID:         "inv_test_1",
		CustomerID: "cust_test_1",
		Total:      decimal.NewFromInt(100),
		Currency:   "usd",
	}

	req, err := svc.buildInvoiceRequest(context.Background(), inv)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/success", req.SuccessURL)
	assert.Equal(t, "https://example.com/cancel", req.BackURL)
	assert.Empty(t, req.CallbackURL)
}

func TestBuildInvoiceRequest_NoConnectionMetadata_URLsEmpty(t *testing.T) {
	svc := &InvoiceSyncService{
		client: &stubMoyasarClient{
			conn: &connection.Connection{}, // Metadata is nil
		},
		logger: mustTestLogger(t),
	}

	inv := &invoice.Invoice{
		ID:         "inv_test_2",
		CustomerID: "cust_test_2",
		Total:      decimal.NewFromInt(50),
		Currency:   "usd",
	}

	req, err := svc.buildInvoiceRequest(context.Background(), inv)
	require.NoError(t, err)
	assert.Empty(t, req.SuccessURL)
	assert.Empty(t, req.BackURL)
	assert.Empty(t, req.CallbackURL)
}

func TestBuildInvoiceRequest_GetConnectionErrors_URLsEmpty(t *testing.T) {
	svc := &InvoiceSyncService{
		client: &stubMoyasarClient{
			err: errors.New("connection lookup failed"),
		},
		logger: mustTestLogger(t),
	}

	inv := &invoice.Invoice{
		ID:         "inv_test_3",
		CustomerID: "cust_test_3",
		Total:      decimal.NewFromInt(75),
		Currency:   "usd",
	}

	req, err := svc.buildInvoiceRequest(context.Background(), inv)
	require.NoError(t, err, "a connection lookup failure must not fail the whole invoice-request build")
	assert.Empty(t, req.SuccessURL)
	assert.Empty(t, req.BackURL)
}
