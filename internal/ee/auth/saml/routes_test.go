package saml

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
)

// newTestProvider builds a provider with the deployment-level settings the
// endpoints depend on, without needing the full server graph.
func newTestProvider(baseURL string) *samlProvider {
	cfg := &config.Configuration{}
	cfg.Auth.SAML.BaseURL = baseURL
	cfg.Auth.SAML.DashboardURL = "http://localhost:3000/auth/callback"
	return newSAMLProvider(cfg)
}

func enabledConfig() Config {
	return Config{
		Enabled:        true,
		IDPEntityID:    "http://idp.example.com/metadata",
		IDPSSOUrl:      "https://idp.example.com/sso",
		IDPCertificate: testCertPEM,
		DefaultRole:    "all_reader",
	}
}

// TestMetadataEndpointServesValidSPMetadata exercises what a customer does
// first: fetch our metadata and upload it into their identity provider. If the
// entity ID or ACS URL is wrong here, every later assertion is rejected for
// audience mismatch, which is a confusing way to discover a configuration typo.
func TestMetadataEndpointServesValidSPMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	provider := newTestProvider("http://localhost:8080")
	sp, err := provider.serviceProvider("tenant_abc", enabledConfig())
	if err != nil {
		t.Fatalf("serviceProvider: %v", err)
	}

	router := gin.New()
	router.GET("/v1/auth/saml/:tenant/metadata", func(c *gin.Context) {
		out, err := marshalMetadata(sp)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "application/samlmetadata+xml", out)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/saml/tenant_abc/metadata", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "samlmetadata+xml") {
		t.Errorf("content type = %q, want SAML metadata", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"EntityDescriptor",
		"AssertionConsumerService",
		"http://localhost:8080/v1/auth/saml/tenant_abc/acs",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metadata is missing %q", want)
		}
	}
}

// TestLoginRedirectsToIdentityProvider checks the SP-initiated entry point: the
// browser must be sent to the configured SSO URL carrying a SAMLRequest.
func TestLoginRedirectsToIdentityProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)

	provider := newTestProvider("http://localhost:8080")
	cfg := enabledConfig()
	sp, err := provider.serviceProvider("tenant_abc", cfg)
	if err != nil {
		t.Fatalf("serviceProvider: %v", err)
	}

	authnRequest, err := sp.MakeAuthenticationRequest(
		cfg.IDPSSOUrl,
		"urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect",
		"urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
	)
	if err != nil {
		t.Fatalf("MakeAuthenticationRequest: %v", err)
	}

	redirectURL, err := authnRequest.Redirect("", sp)
	if err != nil {
		t.Fatalf("Redirect: %v", err)
	}

	if got := redirectURL.Host; got != "idp.example.com" {
		t.Errorf("redirect host = %q, want the configured identity provider", got)
	}
	if redirectURL.Query().Get("SAMLRequest") == "" {
		t.Error("redirect carries no SAMLRequest — the identity provider has nothing to answer")
	}
}

// TestRequestTrackerClaimRetiresTheAnsweredID covers the replay defence.
//
// This is a regression test for a real failure: claim() previously returned the
// outstanding IDs without removing the answered one, so posting the same
// assertion twice produced two successful logins. Live testing against
// SimpleSAMLphp caught it — the second post returned 302 instead of 403.
func TestRequestTrackerClaimRetiresTheAnsweredID(t *testing.T) {
	ctx := context.Background()
	tr := newRequestTracker(newFakeRedis(), newFakeLocker())
	tr.remember(ctx, "tenant_a", "id-1")

	ids, consumed := tr.claim(ctx, "tenant_a", "id-1")
	if !consumed || len(ids) != 1 || ids[0] != "id-1" {
		t.Fatalf("first claim = %v (consumed=%v), want [id-1] so the assertion validates", ids, consumed)
	}

	if ids, consumed := tr.claim(ctx, "tenant_a", "id-1"); consumed || len(ids) != 0 {
		t.Errorf("second claim = %v (consumed=%v), want nothing — the replayed assertion would validate again", ids, consumed)
	}
}

// TestRequestTrackerIsScopedByTenant stops one tenant's outstanding request from
// satisfying another tenant's assertion.
func TestRequestTrackerIsScopedByTenant(t *testing.T) {
	ctx := context.Background()
	tr := newRequestTracker(newFakeRedis(), newFakeLocker())
	tr.remember(ctx, "tenant_a", "id-1")

	if _, consumed := tr.claim(ctx, "tenant_b", "id-1"); consumed {
		t.Error("a request issued for one tenant was consumed by another tenant's assertion")
	}
	if _, consumed := tr.claim(ctx, "tenant_a", "id-1"); !consumed {
		t.Error("the owning tenant's request was disturbed by another tenant's attempt")
	}
}

// TestRequestTrackerClaimKeepsOtherRequests makes sure retiring one request does
// not disturb another browser's outstanding login.
func TestRequestTrackerClaimKeepsOtherRequests(t *testing.T) {
	ctx := context.Background()
	tr := newRequestTracker(newFakeRedis(), newFakeLocker())
	tr.remember(ctx, "tenant_a", "id-1")
	tr.remember(ctx, "tenant_a", "id-2")

	tr.claim(ctx, "tenant_a", "id-1")

	if _, consumed := tr.claim(ctx, "tenant_a", "id-2"); !consumed {
		t.Error("retiring one request also retired another browser's outstanding login")
	}
}

// TestRequestTrackerClaimIsFinal pins a deliberate choice: a consumed request
// stays consumed even when the assertion that consumed it was then rejected.
//
// Reopening it would mean releasing a marker taken atomically on whichever
// replica served the request, and no replica can release another's — so the
// alternative is a window in which the same assertion is claimable twice. A
// genuine retry starts a fresh login instead.
func TestRequestTrackerClaimIsFinal(t *testing.T) {
	ctx := context.Background()
	tr := newRequestTracker(newFakeRedis(), newFakeLocker())
	tr.remember(ctx, "tenant_a", "id-1")

	if _, consumed := tr.claim(ctx, "tenant_a", "id-1"); !consumed {
		t.Fatal("the first claim must succeed")
	}
	if _, consumed := tr.claim(ctx, "tenant_a", "id-1"); consumed {
		t.Error("a consumed request was claimable again — the assertion could be replayed")
	}
}

// TestRequestTrackerIgnoresAnEmptyID covers an assertion carrying no
// InResponseTo at all: there is nothing to consume, and it must not be treated
// as answering an outstanding request.
func TestRequestTrackerIgnoresAnEmptyID(t *testing.T) {
	ctx := context.Background()
	tr := newRequestTracker(newFakeRedis(), newFakeLocker())
	tr.remember(ctx, "tenant_a", "id-1")

	if ids, consumed := tr.claim(ctx, "tenant_a", ""); consumed || len(ids) != 0 {
		t.Errorf("claim(\"\") = %v (consumed=%v), want nothing", ids, consumed)
	}
}

// fakeRedis is the slice of cache.RedisCache the tracker uses. Only Get, Set and
// Delete are exercised; the rest satisfy the interface.
type fakeRedis struct {
	mu     sync.Mutex
	values map[string]interface{}
}

func newFakeRedis() *fakeRedis { return &fakeRedis{values: map[string]interface{}{}} }

func (f *fakeRedis) Get(_ context.Context, key string) (interface{}, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.values[key]
	return v, ok
}

func (f *fakeRedis) Set(_ context.Context, key string, value interface{}, _ time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[key] = value
}

func (f *fakeRedis) Delete(_ context.Context, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.values, key)
}

func (f *fakeRedis) IsEnabled() bool                        { return true }
func (f *fakeRedis) IsRedisCache() bool                     { return true }
func (f *fakeRedis) DeleteByPrefix(context.Context, string) {}
func (f *fakeRedis) Flush(context.Context) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values = map[string]interface{}{}
}
func (f *fakeRedis) ForceCacheGetWithTTL(context.Context, string) (interface{}, time.Duration, bool) {
	return nil, 0, false
}
func (f *fakeRedis) ForceCacheDelete(ctx context.Context, key string) { f.Delete(ctx, key) }
func (f *fakeRedis) ForceCacheGet(ctx context.Context, key string) (interface{}, bool) {
	return f.Get(ctx, key)
}
func (f *fakeRedis) ForceCacheSet(ctx context.Context, key string, value interface{}, ttl time.Duration) {
	f.Set(ctx, key, value, ttl)
}

// TestDashboardRedirectCarriesToken checks the browser hand-back: the token has
// to reach the dashboard, and a missing configuration must not produce a
// redirect to an attacker-controlled default.
func TestDashboardRedirectCarriesToken(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.Auth.SAML.DashboardURL = "http://localhost:3000/auth/callback"

	got := dashboardRedirect(cfg, "tenant_abc", "the-token", "nonce-123")
	if !strings.HasPrefix(got, "http://localhost:3000/auth/callback#") {
		t.Errorf("redirect = %q, want the configured dashboard URL with a fragment", got)
	}
	if !strings.Contains(got, "token=the-token") {
		t.Errorf("redirect = %q, want it to carry the token", got)
	}

	// The tenant must travel with the token. The dashboard compares it against
	// the tenant it recorded when the login began and refuses a mismatch, and it
	// treats a missing value as a refusal — so omitting it here would break every
	// login rather than merely weakening the check.
	fragment, err := url.ParseQuery(strings.TrimPrefix(got[strings.Index(got, "#"):], "#"))
	if err != nil {
		t.Fatalf("fragment is not parseable: %v", err)
	}
	if fragment.Get("tenant_id") != "tenant_abc" {
		t.Errorf("fragment tenant_id = %q, want the tenant the login was served for", fragment.Get("tenant_id"))
	}

	// The token must be in the fragment, never the query. A fragment is not sent
	// to a server, so it stays out of proxy and CDN access logs and out of the
	// Referer header of every request the dashboard makes after it loads; a
	// query parameter reaches all of those, and this token lasts thirty days.
	if u, err := url.Parse(got); err != nil {
		t.Fatalf("redirect is not a URL: %v", err)
	} else if u.Query().Get("token") != "" {
		t.Errorf("redirect = %q, want the token out of the query string", got)
	}

	empty := &config.Configuration{}
	if got := dashboardRedirect(empty, "tenant_abc", "t", ""); !strings.HasPrefix(got, "/") {
		t.Errorf("unconfigured redirect = %q, want a relative path rather than an absolute URL", got)
	}
}

// marshalMetadata mirrors what the metadata handler serves.
func marshalMetadata(sp *saml.ServiceProvider) ([]byte, error) {
	return xml.MarshalIndent(sp.Metadata(), "", "  ")
}

// TestMetadataWorksBeforeConfiguration pins the onboarding order. The customer's
// administrator needs our ACS URL and entity ID to create the application in
// their identity provider, and only then can they hand back the entity ID, SSO
// URL, and certificate. Gating metadata on a configured provider deadlocks
// onboarding, so this must keep working with no configuration at all.
func TestMetadataWorksBeforeConfiguration(t *testing.T) {
	provider := newTestProvider("https://billing.acme.com")

	sp, err := provider.metadataOnlyServiceProvider("00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("metadata must be available before configuration: %v", err)
	}

	out, err := marshalMetadata(sp)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}

	body := string(out)
	for _, want := range []string{
		"https://billing.acme.com/v1/auth/saml/00000000-0000-0000-0000-000000000000/acs",
		"https://billing.acme.com/v1/auth/saml/00000000-0000-0000-0000-000000000000/metadata",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metadata missing %q — the identity provider would be configured to call the wrong endpoint", want)
		}
	}
}

// TestTenantComesOnlyFromTheURL pins the removal of the deployment-wide default
// tenant. SSO is configured per tenant, so there is nothing for a URL that does
// not name its tenant to resolve against, and "default" is now an ordinary
// segment that will match no tenant rather than silently selecting one.
func TestTenantComesOnlyFromTheURL(t *testing.T) {
	provider := newTestProvider("https://billing.acme.com")

	for _, segment := range []string{"tenant_xyz", "00000000-0000-0000-0000-000000000000", "default"} {
		if got := provider.resolveTenant(segment); got != segment {
			t.Errorf("resolveTenant(%q) = %q, want it passed through unchanged", segment, got)
		}
	}

	// An empty segment stays empty so the handler rejects the request rather
	// than falling back to some tenant.
	if got := provider.resolveTenant(""); got != "" {
		t.Errorf("resolveTenant(\"\") = %q, want empty so the request is rejected", got)
	}
}

// TestMetadataCarriesTheTenantFromTheURL guards the trap that mattered when the
// alias existed and still matters now: metadata must be published under the same
// tenant the ACS endpoint validates against, or every assertion fails audience
// validation with an error that points at signatures rather than the URL.
func TestMetadataCarriesTheTenantFromTheURL(t *testing.T) {
	const tenantID = "tenant_acme"

	provider := newTestProvider("https://billing.acme.com")
	sp, err := provider.metadataOnlyServiceProvider(provider.resolveTenant(tenantID))
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}

	xml, err := marshalMetadata(sp)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if !strings.Contains(string(xml), tenantID) {
		t.Error("metadata does not carry the tenant ID from the URL")
	}
}

// TestInResponseToAcceptsEveryValidSerialisation covers the replay defence
// against identity providers that serialise XML differently from ours.
//
// The extracted ID decides which outstanding request is retired. Failing to
// find it does not fail loudly — claim("") retires nothing, so the assertion
// stays replayable for the request's whole lifetime. A pattern matching only
// double-quoted attributes silently lost that defence against any provider
// emitting single quotes or whitespace around "=", both valid XML.
func TestInResponseToAcceptsEveryValidSerialisation(t *testing.T) {
	cases := []struct {
		name string
		xml  string
		want string
	}{
		{"double quotes", `<Response InResponseTo="id-abc" Version="2.0"></Response>`, "id-abc"},
		{"single quotes", `<Response InResponseTo='id-abc' Version='2.0'></Response>`, "id-abc"},
		{"whitespace around equals", `<Response InResponseTo = "id-abc"></Response>`, "id-abc"},
		{"namespaced root", `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" InResponseTo="id-abc"></samlp:Response>`, "id-abc"},
		{"xml declaration first", `<?xml version="1.0"?><Response InResponseTo="id-abc"></Response>`, "id-abc"},
		{"absent attribute", `<Response Version="2.0"></Response>`, ""},
		{"not xml at all", `this is not xml`, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := url.Values{"SAMLResponse": {base64.StdEncoding.EncodeToString([]byte(tc.xml))}}
			req := httptest.NewRequest(http.MethodPost, "/acs", strings.NewReader(body.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			if got := inResponseTo(req); got != tc.want {
				t.Errorf("inResponseTo() = %q, want %q — a missed ID leaves the assertion replayable", got, tc.want)
			}
		})
	}
}

// TestInResponseToReadsTheRootElementOnly guards against retiring the wrong
// request. SubjectConfirmationData also carries InResponseTo, and in a document
// where it appears before the Response attribute a scan of the whole payload
// would take that value instead, leaving the real request outstanding.
func TestInResponseToReadsTheRootElementOnly(t *testing.T) {
	doc := `<Response InResponseTo="id-correct"><Assertion><Subject><SubjectConfirmation>` +
		`<SubjectConfirmationData InResponseTo="id-nested"/></SubjectConfirmation></Subject></Assertion></Response>`

	body := url.Values{"SAMLResponse": {base64.StdEncoding.EncodeToString([]byte(doc))}}
	req := httptest.NewRequest(http.MethodPost, "/acs", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if got := inResponseTo(req); got != "id-correct" {
		t.Errorf("inResponseTo() = %q, want the root element's value", got)
	}
}

// fakeLocker reproduces the property the tracker depends on: AcquireLock is a
// SETNX, so exactly one caller can take a given key. A fake that always
// succeeded would hide the race this replaces.
type fakeLocker struct {
	mu   sync.Mutex
	held map[string]bool
}

func newFakeLocker() *fakeLocker { return &fakeLocker{held: map[string]bool{}} }

func (l *fakeLocker) AcquireLock(_ context.Context, key string, _ time.Duration) (cache.Lock, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[key] {
		return &fakeLock{acquired: false}, nil
	}
	l.held[key] = true
	return &fakeLock{acquired: true, locker: l, key: key}, nil
}

type fakeLock struct {
	acquired bool
	locker   *fakeLocker
	key      string
}

func (k *fakeLock) AcquiredSuccessfully() bool { return k.acquired }
func (k *fakeLock) Release(context.Context) error {
	if k.locker != nil {
		k.locker.mu.Lock()
		delete(k.locker.held, k.key)
		k.locker.mu.Unlock()
	}
	return nil
}

// TestRequestTrackerClaimIsAtomicUnderConcurrency is the regression test for a
// real race: claim previously read the outstanding key and then deleted it in a
// second round trip, so two concurrent posts of the same assertion could both
// observe it as present before either removed it, and both logins succeeded.
//
// Exactly one caller must win, however many arrive at once.
func TestRequestTrackerClaimIsAtomicUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	tr := newRequestTracker(newFakeRedis(), newFakeLocker())
	tr.remember(ctx, "tenant_a", "id-1")

	const attempts = 32
	var wg sync.WaitGroup
	results := make([]bool, attempts)
	start := make(chan struct{})

	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release them together to maximise overlap
			_, consumed := tr.claim(ctx, "tenant_a", "id-1")
			results[i] = consumed
		}(i)
	}
	close(start)
	wg.Wait()

	won := 0
	for _, ok := range results {
		if ok {
			won++
		}
	}
	if won != 1 {
		t.Errorf("%d of %d concurrent claims succeeded, want exactly 1 — a replayed assertion would mint that many sessions", won, attempts)
	}
}

// TestDashboardRedirectCarriesRelayState covers the per-login nonce. The tenant
// binds a callback to a tenant, but every login to that tenant carries the same
// value; the nonce is what ties a response to one specific attempt. It travels
// as SAML RelayState and must reach the dashboard unchanged.
func TestDashboardRedirectCarriesRelayState(t *testing.T) {
	cfg := &config.Configuration{}
	cfg.Auth.SAML.DashboardURL = "http://localhost:3000/auth/callback"

	got := dashboardRedirect(cfg, "tenant_abc", "the-token", "nonce-abc123")
	fragment, err := url.ParseQuery(strings.TrimPrefix(got[strings.Index(got, "#"):], "#"))
	if err != nil {
		t.Fatalf("fragment is not parseable: %v", err)
	}
	if fragment.Get("state") != "nonce-abc123" {
		t.Errorf("state = %q, want the nonce the browser generated", fragment.Get("state"))
	}

	// An identity provider that drops RelayState must not produce a literal
	// empty state, which the dashboard would compare against its stored nonce
	// and reject anyway — the absence is clearer than an empty string.
	plain := dashboardRedirect(cfg, "tenant_abc", "the-token", "")
	plainFragment, err := url.ParseQuery(strings.TrimPrefix(plain[strings.Index(plain, "#"):], "#"))
	if err != nil {
		t.Fatalf("fragment is not parseable: %v", err)
	}
	if _, present := plainFragment["state"]; present {
		t.Error("an empty RelayState should be omitted rather than sent as an empty value")
	}
}
