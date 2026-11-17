package quickbooks

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

type realmRepo struct {
	connection.Repository
	conn *connection.Connection
}

func (r *realmRepo) GetByProvider(context.Context, types.SecretProvider) (*connection.Connection, error) {
	return r.conn, nil
}

type realmEnc struct{ security.EncryptionService }

func (realmEnc) Decrypt(s string) (string, error) { return s, nil }

type realmRT struct{ urls []string }

func (c *realmRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.urls = append(c.urls, r.URL.String())
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
}

func TestRealmIDEscapedInRequestPaths(t *testing.T) {
	tests := []struct {
		name  string
		realm string
		want  string
	}{
		{"numeric", "9130350000000000", "/v3/company/9130350000000000/"},
		{"traversal", "../x", "/v3/company/..%2Fx/"},
		{"query", "a?b=c", "/v3/company/a%3Fb=c/"},
		{"userinfo", "a@b", "/v3/company/a@b/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &realmRT{}
			conn := &connection.Connection{ProviderType: types.SecretProviderQuickBooks, EncryptedSecretData: types.ConnectionMetadata{
				QuickBooks: &types.QuickBooksConnectionMetadata{
					ClientID: "id", ClientSecret: "s", AccessToken: "t",
					RealmID: tt.realm, Environment: "sandbox",
				},
			}}
			c := &Client{
				connectionRepo:    &realmRepo{conn: conn},
				encryptionService: realmEnc{},
				httpClient:        &http.Client{Transport: rt},
				logger:            logger.NewNoopLogger(),
				minorVersion:      "70",
			}
			resp, err := c.makeRequest(context.Background(), http.MethodGet, "customer", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			_, _ = c.queryEntitiesWithRetry(context.Background(), "Customer", "select * from Customer", 0)
			if len(rt.urls) == 0 {
				t.Fatal("no request")
			}
			for _, u := range rt.urls {
				if !strings.Contains(u, tt.want) {
					t.Fatalf("url %s lacks %s", u, tt.want)
				}
			}
		})
	}
}
