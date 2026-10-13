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

func TestBuildInvoiceRequest(t *testing.T) {
	tests := []struct {
		name               string
		conn               *connection.Connection
		connErr            error
		expectedSuccessURL string
		expectedBackURL    string
	}{
		{
			name: "SetsSuccessAndBackURLFromConnectionMetadata",
			conn: &connection.Connection{
				Metadata: map[string]interface{}{
					ConnKeySuccessURL: "https://example.com/success",
					ConnKeyCancelURL:  "https://example.com/cancel",
				},
			},
			expectedSuccessURL: "https://example.com/success",
			expectedBackURL:    "https://example.com/cancel",
		},
		{
			name: "NoConnectionMetadata_URLsEmpty",
			conn: &connection.Connection{}, // Metadata is nil
		},
		{
			name:    "GetConnectionErrors_URLsEmpty",
			connErr: errors.New("connection lookup failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &InvoiceSyncService{
				client: &stubMoyasarClient{
					conn: tt.conn,
					err:  tt.connErr,
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
			require.NoError(t, err, "a connection lookup failure must not fail the whole invoice-request build")
			assert.Equal(t, tt.expectedSuccessURL, req.SuccessURL)
			assert.Equal(t, tt.expectedBackURL, req.BackURL)
			assert.Empty(t, req.CallbackURL)
		})
	}
}
