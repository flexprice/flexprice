package kafka

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/Shopify/sarama"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// gcpTokenProvider implements sarama.AccessTokenProvider for SASL/OAUTHBEARER
// against GCP Managed Service for Apache Kafka (GMK). It pulls credentials from
// Application Default Credentials, which resolves to:
//   - the GKE metadata server when running with Workload Identity, or
//   - gcloud user credentials when running locally.
//
// IMPORTANT: GMK does NOT accept a bare OAuth2 access token over OAUTHBEARER.
// The broker expects a Google-specific token: three base64url segments joined
// with "." — a JWT-style header {"typ":"JWT","alg":"GOOG_OAUTH2_TOKEN"}, a
// payload {"exp","iss":"Google","iat","sub":<principal email>}, and the raw
// access token. Sending the plain access token yields:
//
//	kafka server: SASL Authentication failed: ... invalid credentials with
//	SASL mechanism OAUTHBEARER
//
// This format is defined by Google's reference implementation:
// https://github.com/googleapis/managedkafka/blob/main/kafka-auth-local-server/kafka_gcp_credentials_server.py
//
// oauth2.ReuseTokenSource caches the underlying access token until expiry, so we
// don't hit the metadata server on every broker connect.
type gcpTokenProvider struct {
	src oauth2.TokenSource
	// principal is the service-account email used as the JWT `sub`. When empty
	// it is resolved lazily from the metadata server on first Token() call.
	principal string
}

// gmkJWTHeader is the fixed OAUTHBEARER header GMK requires. The alg value is a
// Google sentinel, not a real signing algorithm — the third segment carries the
// already-minted access token rather than a signature.
var gmkJWTHeader = mustB64JSON(map[string]string{"typ": "JWT", "alg": "GOOG_OAUTH2_TOKEN"})

func newGCPTokenProvider(ctx context.Context, scopes []string) (*gcpTokenProvider, error) {
	if len(scopes) == 0 {
		scopes = []string{"https://www.googleapis.com/auth/cloud-platform"}
	}
	src, err := google.DefaultTokenSource(ctx, scopes...)
	if err != nil {
		return nil, fmt.Errorf("kafka oauthbearer: resolve GCP default token source: %w", err)
	}
	// Allow an explicit principal override (matches the Google reference's
	// GOOGLE_MANAGED_KAFKA_AUTH_PRINCIPAL env var). Otherwise it is resolved
	// from the metadata server on first use.
	principal := os.Getenv("GOOGLE_MANAGED_KAFKA_AUTH_PRINCIPAL")
	return &gcpTokenProvider{
		src:       oauth2.ReuseTokenSource(nil, src),
		principal: principal,
	}, nil
}

func (p *gcpTokenProvider) Token() (*sarama.AccessToken, error) {
	tok, err := p.src.Token()
	if err != nil {
		return nil, fmt.Errorf("kafka oauthbearer: fetch GCP access token: %w", err)
	}

	principal, err := p.resolvePrincipal()
	if err != nil {
		return nil, fmt.Errorf("kafka oauthbearer: resolve principal email: %w", err)
	}

	payload := mustB64JSON(map[string]any{
		"exp": tok.Expiry.UTC().Unix(),
		"iss": "Google",
		"iat": time.Now().UTC().Unix(),
		"sub": principal,
	})
	encodedToken := base64.RawURLEncoding.EncodeToString([]byte(tok.AccessToken))

	gmkToken := strings.Join([]string{gmkJWTHeader, payload, encodedToken}, ".")
	return &sarama.AccessToken{Token: gmkToken}, nil
}

// resolvePrincipal returns the service-account email for the JWT `sub`. It is
// cached after the first successful lookup.
func (p *gcpTokenProvider) resolvePrincipal() (string, error) {
	if p.principal != "" {
		return p.principal, nil
	}
	// Under Workload Identity the access token does not carry the SA email, so
	// fetch it from the metadata server (the impersonated GSA's default SA).
	email, err := metadata.Email("default")
	if err != nil {
		return "", fmt.Errorf("query metadata server for SA email (set GOOGLE_MANAGED_KAFKA_AUTH_PRINCIPAL to override): %w", err)
	}
	p.principal = email
	return email, nil
}

func mustB64JSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// The inputs are fixed maps of primitives; marshaling cannot fail.
		panic(fmt.Sprintf("kafka oauthbearer: marshal token segment: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
