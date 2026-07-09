package razorpay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// testLogger returns a no-op logger, matching the convention used across the
// integration package (see internal/integration/events/handler_test.go).
func testLogger() *logger.Logger {
	return &logger.Logger{SugaredLogger: zap.NewNop().Sugar()}
}

// fakeConnectionRepo is a hand-rolled connection.Repository test double that
// always returns a fixed Razorpay connection. This package has no generated
// mocks, so we follow the same "write a small fake struct" convention used
// elsewhere in internal/integration (e.g. events/handler_test.go).
type fakeConnectionRepo struct {
	conn *connection.Connection
}

func (f *fakeConnectionRepo) Create(ctx context.Context, c *connection.Connection) error { return nil }
func (f *fakeConnectionRepo) Get(ctx context.Context, id string) (*connection.Connection, error) {
	return f.conn, nil
}
func (f *fakeConnectionRepo) GetByProvider(ctx context.Context, provider types.SecretProvider) (*connection.Connection, error) {
	return f.conn, nil
}
func (f *fakeConnectionRepo) List(ctx context.Context, filter *types.ConnectionFilter) ([]*connection.Connection, error) {
	return []*connection.Connection{f.conn}, nil
}
func (f *fakeConnectionRepo) Count(ctx context.Context, filter *types.ConnectionFilter) (int, error) {
	return 1, nil
}
func (f *fakeConnectionRepo) Update(ctx context.Context, c *connection.Connection) error { return nil }
func (f *fakeConnectionRepo) Delete(ctx context.Context, c *connection.Connection) error { return nil }

// fakeEncryptionService is an identity "encryption" service test double —
// decrypt just returns the input unchanged so we can seed plaintext
// credentials directly on the fake connection.
type fakeEncryptionService struct{}

func (f *fakeEncryptionService) Encrypt(plaintext string) (string, error)  { return plaintext, nil }
func (f *fakeEncryptionService) Decrypt(ciphertext string) (string, error) { return ciphertext, nil }
func (f *fakeEncryptionService) Hash(value string) string                  { return value }

// newTestClientPointingAt builds a *Client wired to fake connection/encryption
// dependencies, redirecting the underlying Razorpay SDK client's HTTP calls at
// baseURL via the package-internal baseURLOverride test seam.
func newTestClientPointingAt(t *testing.T, baseURL string) *Client {
	t.Helper()

	conn := &connection.Connection{
		ID:           "conn_test123",
		ProviderType: types.SecretProviderRazorpay,
		EncryptedSecretData: types.ConnectionMetadata{
			Razorpay: &types.RazorpayConnectionMetadata{
				KeyID:     "rzp_test_key",
				SecretKey: "rzp_test_secret",
			},
		},
	}

	return &Client{
		connectionRepo:    &fakeConnectionRepo{conn: conn},
		encryptionService: &fakeEncryptionService{},
		logger:            testLogger(),
		baseURLOverride:   baseURL,
	}
}

func TestClient_GetCustomerTokens_ParsesTokenList(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/customers/cust_test123/tokens", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"entity": "collection",
			"count":  1,
			"items": []map[string]interface{}{
				{
					"id":                "token_test123",
					"method":            "upi",
					"max_amount":        1500000,
					"expired_at":        nil,
					"recurring_details": map[string]interface{}{"status": "confirmed"},
					"created_at":        1751328000,
				},
			},
		})
	}))
	defer server.Close()

	c := newTestClientPointingAt(t, server.URL)

	result, err := c.GetCustomerTokens(context.Background(), "cust_test123")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "token_test123", result[0]["id"])
}

func TestClient_CreateAuthorizationLink_PostsToRegistrationEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/subscription_registration/auth_links", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":        "auth_link_test123",
			"short_url": "https://rzp.io/i/testlink",
			"status":    "created",
		})
	}))
	defer server.Close()

	c := newTestClientPointingAt(t, server.URL)

	result, err := c.CreateAuthorizationLink(context.Background(), map[string]interface{}{
		"customer":                  map[string]interface{}{"name": "Test", "contact": "9999999999", "email": "t@example.com"},
		"type":                      "link",
		"amount":                    100000,
		"currency":                  "INR",
		"subscription_registration": map[string]interface{}{"method": "upi", "max_amount": 1500000},
	})
	require.NoError(t, err)
	require.Equal(t, "https://rzp.io/i/testlink", result["short_url"])
}

func TestClient_CreateOrder_PostsToOrdersEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/orders", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":       "order_test123",
			"amount":   50000,
			"currency": "INR",
			"status":   "created",
		})
	}))
	defer server.Close()

	c := newTestClientPointingAt(t, server.URL)

	result, err := c.CreateOrder(context.Background(), map[string]interface{}{
		"amount":          50000,
		"currency":        "INR",
		"payment_capture": true,
	})
	require.NoError(t, err)
	require.Equal(t, "order_test123", result["id"])
}

func TestClient_CreateRecurringPayment_PostsToRecurringPaymentEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/payments/create/recurring", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "pay_test123",
			"status": "created",
		})
	}))
	defer server.Close()

	c := newTestClientPointingAt(t, server.URL)

	result, err := c.CreateRecurringPayment(context.Background(), map[string]interface{}{
		"email":       "t@example.com",
		"contact":     "9999999999",
		"amount":      50000,
		"currency":    "INR",
		"order_id":    "order_test123",
		"customer_id": "cust_test123",
		"token":       "token_test123",
		"recurring":   true,
	})
	require.NoError(t, err)
	require.Equal(t, "pay_test123", result["id"])
}
