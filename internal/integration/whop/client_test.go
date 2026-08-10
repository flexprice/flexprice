package whop

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// signWhopPayload signs payload per Whop's Standard Webhooks scheme, mirroring
// VerifyWebhookSignature: HMAC-SHA256("{id}.{timestamp}.{body}") keyed by the
// base64-decoded secret, base64-encoded, prefixed with the "v1," version tag.
func signWhopPayload(secret, webhookID, timestamp string, payload []byte) string {
	key, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		panic(err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(fmt.Sprintf("%s.%s.%s", webhookID, timestamp, payload)))
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

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

// newTestEncryptionKey generates a random key rather than using a literal, so this
// file carries no credential-shaped constant for secret scanners to flag. Defined
// locally instead of using testutil.NewEncryptionKey for the import-cycle reason
// described on fakeConnectionRepo above.
func newTestEncryptionKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return hex.EncodeToString(key)
}

func mustTestEncryptionService(t *testing.T) security.EncryptionService {
	t.Helper()
	cfg := &config.Configuration{
		Secrets: config.SecretsConfig{EncryptionKey: newTestEncryptionKey(t)},
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

// testWebhookSecretB64 mimics the base64-encoded secret Whop issues from its
// dashboard (the raw bytes here are arbitrary).
var testWebhookSecretB64 = base64.StdEncoding.EncodeToString([]byte("test_webhook_secret"))

func TestVerifyWebhookSignature_ValidSignature(t *testing.T) {
	payload := []byte(`{"type":"payment.succeeded","data":{}}`)
	webhookID := "msg_test123"
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	validSig := signWhopPayload(testWebhookSecretB64, webhookID, timestamp, payload)

	client, conn := newTestWhopClientWithSecret(t, testWebhookSecretB64)
	err := client.VerifyWebhookSignature(testCtx(), payload, webhookID, timestamp, validSig)
	require.NoError(t, err)
	_ = conn
}

func TestVerifyWebhookSignature_InvalidSignature(t *testing.T) {
	client, _ := newTestWhopClientWithSecret(t, testWebhookSecretB64)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	err := client.VerifyWebhookSignature(testCtx(), []byte(`{"type":"payment.succeeded"}`), "msg_test123", timestamp, "v1,deadbeef")
	require.Error(t, err)
}

func TestVerifyWebhookSignature_MissingSignatureHeader(t *testing.T) {
	client, _ := newTestWhopClientWithSecret(t, testWebhookSecretB64)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	err := client.VerifyWebhookSignature(testCtx(), []byte(`{}`), "msg_test123", timestamp, "")
	require.Error(t, err)
}

func TestVerifyWebhookSignature_StaleTimestampRejected(t *testing.T) {
	client, _ := newTestWhopClientWithSecret(t, testWebhookSecretB64)
	payload := []byte(`{"type":"payment.succeeded"}`)
	webhookID := "msg_test123"
	staleTimestamp := fmt.Sprintf("%d", time.Now().Add(-10*time.Minute).Unix())
	sig := signWhopPayload(testWebhookSecretB64, webhookID, staleTimestamp, payload)

	err := client.VerifyWebhookSignature(testCtx(), payload, webhookID, staleTimestamp, sig)
	require.Error(t, err)
}

func TestVerifyWebhookSignature_MissingConfiguredSecret(t *testing.T) {
	client, _ := newTestWhopClientWithSecret(t, "")

	payload := []byte(`{"type":"payment.succeeded"}`)
	webhookID := "msg_test123"
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sig := signWhopPayload(testWebhookSecretB64, webhookID, timestamp, payload)

	err := client.VerifyWebhookSignature(testCtx(), payload, webhookID, timestamp, sig)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}
