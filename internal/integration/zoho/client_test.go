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
			var hits []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits = append(hits, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(secretBody))
			}))
			defer srv.Close()

			// Real Zoho hosts pass endpoint validation; the transport sends them to srv.
			origTransport := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				r = r.Clone(r.Context())
				r.URL.Scheme = "http"
				r.URL.Host = strings.TrimPrefix(srv.URL, "http://")
				return origTransport.RoundTrip(r)
			})
			t.Cleanup(func() { http.DefaultTransport = origTransport })

			md := &types.ZohoBooksConnectionMetadata{
				ClientID: "id", ClientSecret: "secret", RefreshToken: "rt",
				AccountsURL: "https://accounts.zoho.in", APIDomain: "https://www.zohoapis.in", OrganizationID: "org",
			}
			if tc.path != "/oauth/v2/token" {
				md.AccessToken = "tok"
				md.AccessTokenExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			}
			c, _, ctx := newClient(t, md)
			_, err := c.GetInvoice(ctx, "inv_1")
			require.Error(t, err)
			require.Equal(t, []string{tc.path}, hits, "request must reach the upstream")
			assert.NotContains(t, detailsOf(err), secretBody)
		})
	}
}
