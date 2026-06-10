package kafka

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// staticTokenSource returns a fixed token for deterministic testing.
type staticTokenSource struct{ tok *oauth2.Token }

func (s staticTokenSource) Token() (*oauth2.Token, error) { return s.tok, nil }

// TestGMKTokenFormat verifies the OAUTHBEARER token matches Google's reference
// wire format: base64url(header).base64url(payload).base64url(accessToken),
// where header is {"typ":"JWT","alg":"GOOG_OAUTH2_TOKEN"} and payload carries
// exp/iss/iat/sub. See:
// https://github.com/googleapis/managedkafka/blob/main/kafka-auth-local-server/kafka_gcp_credentials_server.py
func TestGMKTokenFormat(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	p := &gcpTokenProvider{
		src:       oauth2.ReuseTokenSource(nil, staticTokenSource{&oauth2.Token{AccessToken: "raw-access-token-xyz", Expiry: expiry}}),
		principal: "flexprice-app@flexprice-422103.iam.gserviceaccount.com", // skips metadata lookup
	}

	at, err := p.Token()
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}

	segs := strings.Split(at.Token, ".")
	if len(segs) != 3 {
		t.Fatalf("expected 3 dot-separated segments, got %d: %q", len(segs), at.Token)
	}

	// Header segment.
	var header map[string]string
	decodeSeg(t, segs[0], &header)
	if header["typ"] != "JWT" || header["alg"] != "GOOG_OAUTH2_TOKEN" {
		t.Errorf("unexpected header: %+v", header)
	}

	// Payload segment.
	var payload map[string]any
	decodeSeg(t, segs[1], &payload)
	if payload["iss"] != "Google" {
		t.Errorf("expected iss=Google, got %v", payload["iss"])
	}
	if payload["sub"] != "flexprice-app@flexprice-422103.iam.gserviceaccount.com" {
		t.Errorf("unexpected sub: %v", payload["sub"])
	}
	if _, ok := payload["exp"]; !ok {
		t.Error("payload missing exp")
	}
	if _, ok := payload["iat"]; !ok {
		t.Error("payload missing iat")
	}

	// Token segment must be the base64url-encoded raw access token.
	raw, err := base64.RawURLEncoding.DecodeString(segs[2])
	if err != nil {
		t.Fatalf("token segment not valid base64url: %v", err)
	}
	if string(raw) != "raw-access-token-xyz" {
		t.Errorf("expected raw access token in 3rd segment, got %q", string(raw))
	}
}

func decodeSeg(t *testing.T, seg string, v any) {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		t.Fatalf("segment not valid base64url: %v", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("segment not valid JSON: %v", err)
	}
}
