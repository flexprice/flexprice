package stripe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockConnectionRepo struct {
	mock.Mock
}

func (m *mockConnectionRepo) Create(ctx context.Context, c *connection.Connection) error {
	args := m.Called(ctx, c)
	return args.Error(0)
}

func (m *mockConnectionRepo) Get(ctx context.Context, id string) (*connection.Connection, error) {
	args := m.Called(ctx, id)
	if conn, ok := args.Get(0).(*connection.Connection); ok {
		return conn, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockConnectionRepo) GetByProvider(ctx context.Context, provider types.SecretProvider) (*connection.Connection, error) {
	args := m.Called(ctx, provider)
	if conn, ok := args.Get(0).(*connection.Connection); ok {
		return conn, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockConnectionRepo) ListPublishedByProvider(ctx context.Context, provider types.SecretProvider) ([]*connection.Connection, error) {
	args := m.Called(ctx, provider)
	if conns, ok := args.Get(0).([]*connection.Connection); ok {
		return conns, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockConnectionRepo) List(ctx context.Context, filter *types.ConnectionFilter) ([]*connection.Connection, error) {
	args := m.Called(ctx, filter)
	if conns, ok := args.Get(0).([]*connection.Connection); ok {
		return conns, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockConnectionRepo) Count(ctx context.Context, filter *types.ConnectionFilter) (int, error) {
	args := m.Called(ctx, filter)
	return args.Int(0), args.Error(1)
}

func (m *mockConnectionRepo) Update(ctx context.Context, c *connection.Connection) error {
	args := m.Called(ctx, c)
	return args.Error(0)
}

func (m *mockConnectionRepo) Delete(ctx context.Context, c *connection.Connection) error {
	args := m.Called(ctx, c)
	return args.Error(0)
}

type mockEncryptionService struct{}

func (m *mockEncryptionService) Encrypt(text string) (string, error) { return text, nil }
func (m *mockEncryptionService) Decrypt(text string) (string, error) { return text, nil }
func (m *mockEncryptionService) Hash(value string) string            { return value }

func setupTestStripeClient(repo connection.Repository) *stripe.Client {
	log := logger.NewNoopLogger()
	return stripe.NewClient(repo, &mockEncryptionService{}, log)
}

func createDummyConnection(baseURL string) *connection.Connection {
	return &connection.Connection{
		ID:           "conn_123",
		ProviderType: types.SecretProviderStripe,
		EncryptedSecretData: types.ConnectionMetadata{
			Stripe: &types.StripeConnectionMetadata{
				SecretKey:      "sk_test_123",
				PublishableKey: "pk_test_123",
				WebhookSecret:  "whsec_123",
				BaseURL:        baseURL,
			},
		},
		EnvironmentID: "env_test",
		BaseModel: types.BaseModel{
			TenantID:  "tenant_test",
			Status:    types.StatusPublished,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
}

func TestGetStripeClient_DefaultURL(t *testing.T) {
	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection(""), nil)

	client := setupTestStripeClient(repo)
	stripeSDKClient, config, err := client.GetStripeClient(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, stripeSDKClient)
	assert.Equal(t, "sk_test_123", config.SecretKey)
	assert.Empty(t, config.BaseURL)
}

func TestGetStripeClient_CustomURLNoAllowlistConfigured(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("https://stripe-proxy.internal"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom Stripe base URL is not enabled")
}

func TestGetStripeClient_AllowedHTTPSSEndpoint(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "https://stripe-proxy.internal:8443")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("https://stripe-proxy.internal:8443"), nil)

	client := setupTestStripeClient(repo)
	stripeSDKClient, config, err := client.GetStripeClient(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, stripeSDKClient)
	assert.Equal(t, "https://stripe-proxy.internal:8443", config.BaseURL)
}

func TestGetStripeClient_AllowedLocalhostPrivateHTTPEndpoint(t *testing.T) {
	var requestedPath string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"cus_123","object":"customer"}`))
	}))
	defer mockServer.Close()

	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", mockServer.URL)

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection(mockServer.URL), nil)

	client := setupTestStripeClient(repo)
	stripeSDKClient, config, err := client.GetStripeClient(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, stripeSDKClient)
	assert.Equal(t, mockServer.URL, config.BaseURL)

	cust, err := stripeSDKClient.V1Customers.Retrieve(context.Background(), "cus_123", nil)
	require.NoError(t, err)
	assert.Equal(t, "cus_123", cust.ID)
	assert.Equal(t, "/v1/customers/cus_123", requestedPath)
}

func TestGetStripeClient_NonAllowlistedEndpointRejected(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "https://stripe-proxy.internal")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("https://unauthorized-proxy.com"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in the operator allowlist")
}

func TestGetStripeClient_LookalikeHostnameRejected(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "https://stripe-proxy.internal")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("https://stripe-proxy.internal.evil.com"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in the operator allowlist")
}

func TestGetStripeClient_RedirectToAnotherOriginRejected(t *testing.T) {
	secondServerCalled := false
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondServerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"cus_123","object":"customer"}`))
	}))
	defer secondServer.Close()

	firstServerCalled := false
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstServerCalled = true
		http.Redirect(w, r, secondServer.URL+"/v1/customers/cus_123", http.StatusFound)
	}))
	defer firstServer.Close()

	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", firstServer.URL)

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection(firstServer.URL), nil)

	client := setupTestStripeClient(repo)
	stripeSDKClient, _, err := client.GetStripeClient(context.Background())
	require.NoError(t, err)

	cust, err := stripeSDKClient.V1Customers.Retrieve(context.Background(), "cus_123", nil)
	require.Error(t, err, "Stripe SDK must return an error for 302 response without following redirect")
	assert.True(t, firstServerCalled, "request must reach the configured first server backend")
	assert.False(t, secondServerCalled, "request must not reach redirected target origin")
	if cust != nil {
		assert.NotEqual(t, "cus_123", cust.ID)
	}
}

func TestGetStripeClient_CustomURLWithTrailingSlash(t *testing.T) {
	var requestedPath string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"cus_456","object":"customer"}`))
	}))
	defer mockServer.Close()

	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", mockServer.URL)

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection(mockServer.URL+"/"), nil)

	client := setupTestStripeClient(repo)
	stripeSDKClient, config, err := client.GetStripeClient(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, stripeSDKClient)
	assert.Equal(t, mockServer.URL+"/", config.BaseURL)

	cust, err := stripeSDKClient.V1Customers.Retrieve(context.Background(), "cus_456", nil)
	require.NoError(t, err)
	assert.Equal(t, "cus_456", cust.ID)
	assert.Equal(t, "/v1/customers/cus_456", requestedPath)
}

func TestGetStripeClient_InvalidURL(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "http://localhost")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("not-a-valid-url"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Stripe base URL")
}

func TestGetStripeClient_NonHTTPScheme(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "ftp://localhost:8080")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("ftp://localhost:8080"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Stripe base URL origin")
}

func TestGetStripeClient_CredentialsUserinfoRejected(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "https://payments.example.com")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("https://user:pass@payments.example.com"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials are forbidden")
}

func TestGetStripeClient_QueryStringRejected(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "https://payments.example.com")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("https://payments.example.com?token=x"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "query string is forbidden")
}

func TestGetStripeClient_FragmentRejected(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "https://payments.example.com")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("https://payments.example.com#fragment"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fragment is forbidden")
}

func TestGetStripeClient_NonRootPathRejected(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "https://payments.example.com")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("https://payments.example.com/foo"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-root path is forbidden")
}

func TestGetStripeClient_PortOnlyHostRejected(t *testing.T) {
	t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", "http://:8080")

	repo := new(mockConnectionRepo)
	repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection("http://:8080"), nil)

	client := setupTestStripeClient(repo)
	_, _, err := client.GetStripeClient(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname is required")
}

func TestGetStripeClient_ValidHostnamesIPv4IPv6Accepted(t *testing.T) {
	testCases := []struct {
		name      string
		url       string
		allowlist string
	}{
		{
			name:      "domain with port",
			url:       "http://example.com:8080",
			allowlist: "http://example.com:8080",
		},
		{
			name:      "ipv4 with port",
			url:       "http://127.0.0.1:8080",
			allowlist: "http://127.0.0.1:8080",
		},
		{
			name:      "ipv6 literal with port",
			url:       "http://[::1]:8080",
			allowlist: "http://[::1]:8080",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FLEXPRICE_STRIPE_ALLOWED_BASE_URLS", tc.allowlist)

			repo := new(mockConnectionRepo)
			repo.On("GetByProvider", mock.Anything, types.SecretProviderStripe).Return(createDummyConnection(tc.url), nil)

			client := setupTestStripeClient(repo)
			stripeSDKClient, config, err := client.GetStripeClient(context.Background())

			require.NoError(t, err)
			assert.NotNil(t, stripeSDKClient)
			assert.Equal(t, tc.url, config.BaseURL)
		})
	}
}

func TestConvertFlatMetadataToStructured_StripeBaseURL(t *testing.T) {
	flatData := map[string]interface{}{
		"publishable_key": "pk_test_123",
		"secret_key":      "sk_test_123",
		"webhook_secret":  "whsec_123",
		"account_id":      "acct_123",
		"base_url":        "http://localhost:12111",
	}

	res := dto.ConvertFlatMetadataToStructured(flatData, types.SecretProviderStripe)
	require.NotNil(t, res.Stripe)
	assert.Equal(t, "http://localhost:12111", res.Stripe.BaseURL)
	assert.Equal(t, "sk_test_123", res.Stripe.SecretKey)

	// Verify StripeConnectionMetadata.Validate() validates BaseURL
	err := res.Stripe.Validate()
	require.NoError(t, err)

	invalidMetadata := &types.StripeConnectionMetadata{
		PublishableKey: "pk_test_123",
		SecretKey:      "sk_test_123",
		WebhookSecret:  "whsec_123",
		BaseURL:        "https://user:pass@payments.example.com",
	}
	err = invalidMetadata.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base_url")
}
