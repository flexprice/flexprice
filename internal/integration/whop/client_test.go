package whop

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// fakeConnectionRepo is a minimal in-memory connection.Repository implementation
// scoped to this test file. We can't use internal/testutil here — it imports
// internal/integration (for other test helpers), which imports this whop package
// back via the integration factory, creating an import cycle in test builds.
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

func mustTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	cfg := &config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	return log
}

func mustTestEncryptionService(t *testing.T) security.EncryptionService {
	t.Helper()
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: "test-encryption-key-32-bytes!!!"},
	}
	svc, err := security.NewEncryptionService(cfg, mustTestLogger(t))
	require.NoError(t, err)
	return svc
}

// newTestWhopClientWithSecret builds a Whop client backed by an in-memory connection
// store with a single published Whop connection whose webhook_secret is set to secret.
// If secret is empty, the connection is created without a webhook secret at all
// (mirrors a pre-existing Whop connection that predates this feature).
func newTestWhopClientWithSecret(t *testing.T, secret string) (WhopClient, *connection.Connection) {
	t.Helper()

	encryptionSvc := mustTestEncryptionService(t)
	connRepo := newFakeConnectionRepo()

	encryptedAPIKey, err := encryptionSvc.Encrypt("test-api-key")
	require.NoError(t, err)
	encryptedCompanyID, err := encryptionSvc.Encrypt("biz_test123")
	require.NoError(t, err)

	whopMetadata := &types.WhopConnectionMetadata{
		APIKey:    encryptedAPIKey,
		CompanyID: encryptedCompanyID,
	}
	if secret != "" {
		encryptedWebhookSecret, err := encryptionSvc.Encrypt(secret)
		require.NoError(t, err)
		whopMetadata.WebhookSecret = encryptedWebhookSecret
	}

	conn := &connection.Connection{
		ID:           types.GenerateUUIDWithPrefix("conn"),
		Name:         "test-whop-connection",
		ProviderType: types.SecretProviderWhop,
		EncryptedSecretData: types.ConnectionMetadata{
			Whop: whopMetadata,
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

	client := NewClient(connRepo, encryptionSvc, mustTestLogger(t), &config.Configuration{})
	return client, conn
}

func testCtx() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_test")
	ctx = types.SetEnvironmentID(ctx, "env_test")
	return ctx
}

func TestVerifyWebhookSignature_ValidSignature(t *testing.T) {
	secret := "test_webhook_secret"
	payload := []byte(`{"type":"payment.succeeded","data":{}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := hex.EncodeToString(mac.Sum(nil))

	client, conn := newTestWhopClientWithSecret(t, secret)
	err := client.VerifyWebhookSignature(testCtx(), payload, validSig)
	require.NoError(t, err)
	_ = conn
}

func TestVerifyWebhookSignature_InvalidSignature(t *testing.T) {
	client, _ := newTestWhopClientWithSecret(t, "test_webhook_secret")
	err := client.VerifyWebhookSignature(testCtx(), []byte(`{"type":"payment.succeeded"}`), "deadbeef")
	require.Error(t, err)
}

func TestVerifyWebhookSignature_MissingSignatureHeader(t *testing.T) {
	client, _ := newTestWhopClientWithSecret(t, "test_webhook_secret")
	err := client.VerifyWebhookSignature(testCtx(), []byte(`{}`), "")
	require.Error(t, err)
}

func TestVerifyWebhookSignature_MissingConfiguredSecret(t *testing.T) {
	client, _ := newTestWhopClientWithSecret(t, "")

	payload := []byte(`{"type":"payment.succeeded"}`)
	mac := hmac.New(sha256.New, []byte("irrelevant"))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))

	err := client.VerifyWebhookSignature(testCtx(), payload, sig)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}
