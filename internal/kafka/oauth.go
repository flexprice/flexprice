package kafka

import (
	"context"
	"fmt"

	"github.com/Shopify/sarama"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// gcpTokenProvider implements sarama.AccessTokenProvider for SASL/OAUTHBEARER
// against GCP Managed Kafka. It pulls credentials from Application Default
// Credentials, which resolves to:
//   - the GKE metadata server when running with Workload Identity, or
//   - gcloud user credentials when running locally.
//
// oauth2.ReuseTokenSource caches the token until expiry, so we don't hit the
// metadata server on every broker connect.
type gcpTokenProvider struct {
	src oauth2.TokenSource
}

func newGCPTokenProvider(ctx context.Context, scopes []string) (*gcpTokenProvider, error) {
	if len(scopes) == 0 {
		scopes = []string{"https://www.googleapis.com/auth/cloud-platform"}
	}
	src, err := google.DefaultTokenSource(ctx, scopes...)
	if err != nil {
		return nil, fmt.Errorf("kafka oauthbearer: resolve GCP default token source: %w", err)
	}
	return &gcpTokenProvider{src: oauth2.ReuseTokenSource(nil, src)}, nil
}

func (p *gcpTokenProvider) Token() (*sarama.AccessToken, error) {
	tok, err := p.src.Token()
	if err != nil {
		return nil, fmt.Errorf("kafka oauthbearer: fetch GCP access token: %w", err)
	}
	return &sarama.AccessToken{Token: tok.AccessToken}, nil
}
