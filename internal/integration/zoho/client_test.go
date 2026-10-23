package zoho_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/integration/zoho"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type plainCrypto struct{}

func (plainCrypto) Encrypt(s string) (string, error) { return s, nil }
func (plainCrypto) Decrypt(s string) (string, error) { return s, nil }
func (plainCrypto) Hash(s string) string             { return s }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const secretBody = "internal-upstream-detail"

func newClient(t *testing.T, md *types.ZohoBooksConnectionMetadata) (zoho.ZohoClient, *testutil.InMemoryConnectionStore, context.Context) {
	t.Helper()
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")
	store := testutil.NewInMemoryConnectionStore()
	conn := &connection.Connection{
		ID:                  "conn_1",
		ProviderType:        types.SecretProviderZohoBooks,
		EnvironmentID:       "env_1",
		EncryptedSecretData: types.ConnectionMetadata{ZohoBooks: md},
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	conn.TenantID = "tenant_1"
	require.NoError(t, store.Create(ctx, conn))
	return zoho.NewClient(store, plainCrypto{}, logger.NewNoopLogger()), store, ctx
}

func detailsOf(err error) string {
	return fmt.Sprint(errors.GetAllSafeDetails(err)) + err.Error()
}

func TestTokenRefreshAPIDomainValidation(t *testing.T) {
	tests := []struct {
		name      string
		apiDomain string
		wantErr   bool
	}{
		{"zoho api domain", "https://www.zohoapis.in", false},
		{"attacker host", "https://evil.example.com", true},
		{"http scheme", "http://www.zohoapis.com", true},
		{"zoho lookalike suffix", "https://zohoapis.com.evil.com", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth/v2/token" {
					_, _ = w.Write([]byte(fmt.Sprintf(`{"access_token":"tok","api_domain":%q,"expires_in":3600}`, tc.apiDomain)))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer srv.Close()

			origTransport := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				r = r.Clone(r.Context())
				r.URL.Scheme = "http"
				r.URL.Host = strings.TrimPrefix(srv.URL, "http://")
				return origTransport.RoundTrip(r)
			})
			t.Cleanup(func() { http.DefaultTransport = origTransport })

			c, store, ctx := newClient(t, &types.ZohoBooksConnectionMetadata{
				ClientID: "id", ClientSecret: "secret", RefreshToken: "rt",
				AccountsURL: srv.URL, OrganizationID: "org",
			})
			_, err := c.GetInvoice(ctx, "inv_1")

			stored, getErr := store.GetByProvider(ctx, types.SecretProviderZohoBooks)
			require.NoError(t, getErr)
			if tc.wantErr {
				assert.Empty(t, stored.EncryptedSecretData.ZohoBooks.APIDomain)
				assert.Empty(t, stored.EncryptedSecretData.ZohoBooks.AccessToken)
				assert.Error(t, err)
				return
			}
			assert.Equal(t, tc.apiDomain, stored.EncryptedSecretData.ZohoBooks.APIDomain)
		})
	}
}

func TestUpstreamBodyNotReturned(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"token refresh failure", "/oauth/v2/token"},
		{"api call failure", "/books/v3/invoices/inv_1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(secretBody))
			}))
			defer srv.Close()

			md := &types.ZohoBooksConnectionMetadata{
				ClientID: "id", ClientSecret: "secret", RefreshToken: "rt",
				AccountsURL: srv.URL, APIDomain: srv.URL, OrganizationID: "org",
			}
			if tc.path != "/oauth/v2/token" {
				md.AccessToken = "tok"
				md.AccessTokenExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			}
			c, _, ctx := newClient(t, md)
			_, err := c.GetInvoice(ctx, "inv_1")
			require.Error(t, err)
			assert.NotContains(t, detailsOf(err), secretBody)
		})
	}
}
