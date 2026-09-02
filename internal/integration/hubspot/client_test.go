package hubspot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// fakeConnectionRepo is a minimal in-memory connection.Repository implementation
// scoped to this test file. We can't use internal/testutil here — it imports
// internal/integration (for other test helpers), which imports this hubspot package
// back via the integration factory, creating an import cycle in test builds. This
// mirrors internal/integration/whop/client_test.go's fakeConnectionRepo exactly.
type fakeConnectionRepo struct {
	byProvider map[types.SecretProvider]*connection.Connection
}

func newFakeConnectionRepo() *fakeConnectionRepo {
	return &fakeConnectionRepo{byProvider: make(map[types.SecretProvider]*connection.Connection)}
}

func (r *fakeConnectionRepo) Create(_ context.Context, c *connection.Connection) error {
	r.byProvider[c.ProviderType] = c
	return nil
}

func (r *fakeConnectionRepo) Get(_ context.Context, id string) (*connection.Connection, error) {
	for _, c := range r.byProvider {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, ierr.NewError("connection not found").Mark(ierr.ErrNotFound)
}

func (r *fakeConnectionRepo) GetByProvider(_ context.Context, provider types.SecretProvider) (*connection.Connection, error) {
	c, ok := r.byProvider[provider]
	if !ok {
		return nil, ierr.NewError("connection not found").Mark(ierr.ErrNotFound)
	}
	return c, nil
}

func (r *fakeConnectionRepo) ListPublishedByProvider(_ context.Context, provider types.SecretProvider) ([]*connection.Connection, error) {
	c, ok := r.byProvider[provider]
	if !ok || c.Status != types.StatusPublished {
		return nil, nil
	}
	return []*connection.Connection{c}, nil
}

func (r *fakeConnectionRepo) List(_ context.Context, _ *types.ConnectionFilter) ([]*connection.Connection, error) {
	out := make([]*connection.Connection, 0, len(r.byProvider))
	for _, c := range r.byProvider {
		out = append(out, c)
	}
	return out, nil
}

func (r *fakeConnectionRepo) ListAllPublished(ctx context.Context) ([]*connection.Connection, error) {
	return r.List(ctx, nil)
}

func (r *fakeConnectionRepo) Count(_ context.Context, _ *types.ConnectionFilter) (int, error) {
	return len(r.byProvider), nil
}

func (r *fakeConnectionRepo) Update(_ context.Context, c *connection.Connection) error {
	r.byProvider[c.ProviderType] = c
	return nil
}

func (r *fakeConnectionRepo) Delete(_ context.Context, c *connection.Connection) error {
	delete(r.byProvider, c.ProviderType)
	return nil
}

// fakeHTTPClient is a minimal httpclient.Client double, local to this test file for
// the same import-cycle-avoidance reason as fakeConnectionRepo (avoids internal/testutil).
// Matches routes by URL suffix, same as testutil.MockHTTPClient, just not imported.
type fakeHTTPClient struct {
	mu     sync.RWMutex
	routes map[string]fakeHTTPResponse
}

type fakeHTTPResponse struct {
	statusCode int
	body       []byte
}

func newFakeHTTPClient() *fakeHTTPClient {
	return &fakeHTTPClient{routes: make(map[string]fakeHTTPResponse)}
}

func (f *fakeHTTPClient) registerResponse(urlSuffix string, resp fakeHTTPResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[urlSuffix] = resp
}

func (f *fakeHTTPClient) Send(_ context.Context, req *httpclient.Request) (*httpclient.Response, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for suffix, resp := range f.routes {
		if len(req.URL) >= len(suffix) && req.URL[len(req.URL)-len(suffix):] == suffix {
			return &httpclient.Response{StatusCode: resp.statusCode, Body: resp.body, Headers: map[string]string{}}, nil
		}
	}
	return &httpclient.Response{StatusCode: http.StatusNotFound, Body: []byte("not found"), Headers: map[string]string{}}, nil
}

func mustTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	cfg := &config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	return log
}

// newTestEncryptionKey generates a random key rather than using a literal, so this
// file carries no credential-shaped constant for secret scanners to flag.
func newTestEncryptionKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return hex.EncodeToString(key)
}

func mustTestEncryptionService(t *testing.T) security.EncryptionService {
	t.Helper()
	cfg := &config.Configuration{Secrets: config.SecretsConfig{EncryptionKey: newTestEncryptionKey(t)}}
	svc, err := security.NewEncryptionService(cfg, mustTestLogger(t))
	require.NoError(t, err)
	return svc
}

// newTestHubSpotClient builds a hubspot.Client backed by an in-memory connection
// store with a single published HubSpot connection, and a fakeHTTPClient the test
// can register responses on. Returns the concrete *Client (not the HubSpotClient
// interface) so tests can call unexported methods if ever needed, and the
// fakeHTTPClient so tests can register per-case responses.
func newTestHubSpotClient(t *testing.T) (*Client, *fakeHTTPClient) {
	t.Helper()

	encryptionSvc := mustTestEncryptionService(t)
	connRepo := newFakeConnectionRepo()

	encryptedAccessToken, err := encryptionSvc.Encrypt("test-access-token")
	require.NoError(t, err)
	encryptedClientSecret, err := encryptionSvc.Encrypt("test-client-secret")
	require.NoError(t, err)

	conn := &connection.Connection{
		ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONNECTION),
		Name:         "test-hubspot-connection",
		ProviderType: types.SecretProviderHubSpot,
		EncryptedSecretData: types.ConnectionMetadata{
			HubSpot: &types.HubSpotConnectionMetadata{
				AccessToken:  encryptedAccessToken,
				ClientSecret: encryptedClientSecret,
			},
		},
		EnvironmentID: "env_test",
		BaseModel: types.BaseModel{
			TenantID: "tenant_test",
			Status:   types.StatusPublished,
		},
	}

	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_test")
	ctx = types.SetEnvironmentID(ctx, "env_test")
	require.NoError(t, connRepo.Create(ctx, conn))

	httpClient := newFakeHTTPClient()
	client := &Client{
		connectionRepo:    connRepo,
		encryptionService: encryptionSvc,
		logger:            mustTestLogger(t),
		httpClient:        httpClient,
	}
	return client, httpClient
}

func testCtx() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_test")
	ctx = types.SetEnvironmentID(ctx, "env_test")
	return ctx
}

func TestDeleteDealLineItem_Success(t *testing.T) {
	client, httpClient := newTestHubSpotClient(t)
	httpClient.registerResponse("/crm/v3/objects/line_items/li_123", fakeHTTPResponse{
		statusCode: http.StatusNoContent,
	})

	err := client.DeleteDealLineItem(testCtx(), "li_123")
	require.NoError(t, err)
}

func TestDeleteDealLineItem_NotFound(t *testing.T) {
	client, httpClient := newTestHubSpotClient(t)
	httpClient.registerResponse("/crm/v3/objects/line_items/li_missing", fakeHTTPResponse{
		statusCode: http.StatusNotFound,
		body:       []byte(`{"message":"line item not found"}`),
	})

	err := client.DeleteDealLineItem(testCtx(), "li_missing")
	require.Error(t, err)
	require.True(t, ierr.IsNotFound(err), "expected ErrNotFound, got: %v", err)
}

func TestDeleteDealLineItem_UnexpectedStatus(t *testing.T) {
	client, httpClient := newTestHubSpotClient(t)
	httpClient.registerResponse("/crm/v3/objects/line_items/li_500", fakeHTTPResponse{
		statusCode: http.StatusInternalServerError,
		body:       []byte(`{"message":"internal error"}`),
	})

	err := client.DeleteDealLineItem(testCtx(), "li_500")
	require.Error(t, err)
	require.False(t, ierr.IsNotFound(err))
}
