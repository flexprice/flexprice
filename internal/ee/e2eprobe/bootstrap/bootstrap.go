// Package bootstrap provisions an API key for the probe from an email and
// password. It exists for airgapped deployments, which have no dashboard or
// existing key to mint one with.
//
// Every call is raw net/http: go-sdk v2.0.24 exposes no auth methods, and its
// Environments type has only CloneEnvironment. This mirrors the raw-HTTP
// precedent in client.go (EntitlementOps.CreateWithGrant / GetRaw).
package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Doer is the subset of *http.Client the flow needs, so tests can inject one.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Credentials carries what bootstrap provisioned. Fields stay private so
// callers cannot mutate a live credential; read them through the getters.
type Credentials struct {
	apiKey        string
	tenantID      string
	environmentID string
	secretID      string
}

func (c *Credentials) APIKey() string        { return c.apiKey }
func (c *Credentials) TenantID() string      { return c.tenantID }
func (c *Credentials) EnvironmentID() string { return c.environmentID }
func (c *Credentials) SecretID() string      { return c.secretID }

type authResponse struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

// environmentsResponse mirrors dto.ListEnvironmentsResponse. The array key is
// "environments" — an "items"-keyed decode silently yields nothing.
type environmentsResponse struct {
	Environments []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"environments"`
	Total int `json:"total"`
}

type mintResponse struct {
	Secret struct {
		ID string `json:"id"`
	} `json:"secret"`
	APIKey string `json:"api_key"`
}

// Run executes signup (falling back to login when the user already exists),
// resolves the environment, and mints a private API key.
func Run(ctx context.Context, doer Doer, apiHost, email, password, keyName string) (*Credentials, error) {
	host := strings.TrimRight(apiHost, "/")

	auth, err := signUpOrLogIn(ctx, doer, host, email, password)
	if err != nil {
		return nil, err
	}

	envID, err := resolveEnvironment(ctx, doer, host, auth.Token)
	if err != nil {
		return nil, err
	}

	body, status, err := do(ctx, doer, http.MethodPost, host+"/secrets/api/keys",
		map[string]string{"name": keyName, "type": "private_key"},
		map[string]string{
			"Authorization":    "Bearer " + auth.Token,
			"X-Environment-ID": envID,
		})
	if err != nil {
		return nil, fmt.Errorf("bootstrap step=mint_key: %w", err)
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return nil, fmt.Errorf("bootstrap step=mint_key: unexpected status %d: %s", status, truncate(body))
	}

	var mint mintResponse
	if err := json.Unmarshal(body, &mint); err != nil {
		return nil, fmt.Errorf("bootstrap step=mint_key: decode: %w", err)
	}
	if mint.APIKey == "" {
		return nil, fmt.Errorf("bootstrap step=mint_key: response carried no api_key")
	}

	return &Credentials{
		apiKey:        mint.APIKey,
		tenantID:      auth.TenantID,
		environmentID: envID,
		secretID:      mint.Secret.ID,
	}, nil
}

// signUpOrLogIn tries signup first. A 409 means the user exists already —
// expected on any restart before the key was persisted — so it logs in instead.
func signUpOrLogIn(ctx context.Context, doer Doer, host, email, password string) (*authResponse, error) {
	body, status, err := do(ctx, doer, http.MethodPost, host+"/auth/signup",
		map[string]string{"email": email, "password": password, "tenant_name": "e2eprobe"}, nil)
	if err != nil {
		return nil, fmt.Errorf("bootstrap step=signup: %w", err)
	}

	// Signup returns 200, not 201.
	if status == http.StatusOK || status == http.StatusCreated {
		return decodeAuth(body, "signup")
	}

	if !isAlreadyExists(status, body) {
		return nil, fmt.Errorf(
			"bootstrap step=signup: the auth provider rejected signup (status %d: %s); "+
				"bootstrap requires the flexprice-native auth provider with signups enabled — "+
				"Supabase-backed and SSO-enforced deployments cannot use it",
			status, truncate(body))
	}

	body, status, err = do(ctx, doer, http.MethodPost, host+"/auth/login",
		map[string]string{"email": email, "password": password}, nil)
	if err != nil {
		return nil, fmt.Errorf("bootstrap step=login: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf(
			"bootstrap step=login: the account exists but login failed (status %d: %s); "+
				"check the password, or whether the tenant enforces SSO",
			status, truncate(body))
	}
	return decodeAuth(body, "login")
}

func decodeAuth(body []byte, step string) (*authResponse, error) {
	var out authResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("bootstrap step=%s: decode: %w", step, err)
	}
	if out.Token == "" {
		return nil, fmt.Errorf("bootstrap step=%s: response carried no token", step)
	}
	return &out, nil
}

// resolveEnvironment always lists. E2EPROBE_ENVIRONMENT_ID is an observability
// label, and reusing it as the mint target would let a stale value put the key
// in the wrong environment. Signup provisions exactly one environment.
func resolveEnvironment(ctx context.Context, doer Doer, host, token string) (string, error) {
	body, status, err := do(ctx, doer, http.MethodGet, host+"/environments", nil,
		map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		return "", fmt.Errorf("bootstrap step=list_environments: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("bootstrap step=list_environments: unexpected status %d: %s", status, truncate(body))
	}

	var out environmentsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("bootstrap step=list_environments: decode: %w", err)
	}
	if len(out.Environments) == 0 {
		return "", fmt.Errorf("bootstrap step=list_environments: tenant has no environments")
	}
	return out.Environments[0].ID, nil
}

// isAlreadyExists detects a duplicate signup. The probe's SDK-based
// isAlreadyExists cannot be reused: it matches go-sdk error types, and this
// flow speaks raw HTTP.
func isAlreadyExists(status int, body []byte) bool {
	return status == http.StatusConflict || bytes.Contains(body, []byte(`"already_exists"`))
}

func do(ctx context.Context, doer Doer, method, url string, payload any, headers map[string]string) ([]byte, int, error) {
	var rdr io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("encode request: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := doer.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return body, resp.StatusCode, nil
}

// truncate bounds an upstream error body so a huge HTML error page cannot
// flood the logs.
func truncate(body []byte) string {
	const max = 200
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// NewCredentialsForTest builds a Credentials outside this package. Test-only:
// production code obtains one from Run.
func NewCredentialsForTest(apiKey, tenantID, environmentID, secretID string) *Credentials {
	return &Credentials{
		apiKey:        apiKey,
		tenantID:      tenantID,
		environmentID: environmentID,
		secretID:      secretID,
	}
}
