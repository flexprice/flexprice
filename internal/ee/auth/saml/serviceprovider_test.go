package saml

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
)

// TestServiceProviderMetadataIsWellFormed guards the misconfiguration that is
// hardest to diagnose in the field: if base_url disagrees with what the identity
// provider is told to call, the assertion is rejected for audience mismatch,
// which reads like a signature failure but is not.
func TestServiceProviderMetadataIsWellFormed(t *testing.T) {
	p := &samlProvider{cfg: &config.Configuration{}}
	p.cfg.Auth.SAML.BaseURL = "http://localhost:8080"

	cfg := Config{
		Enabled:        true,
		IDPEntityID:    "http://idp.example.com/metadata",
		IDPSSOUrl:      "https://idp.example.com/sso",
		IDPCertificate: testCertPEM,
		DefaultRole:    "all_reader",
	}

	sp, err := p.serviceProvider("tenant_abc", cfg)
	if err != nil {
		t.Fatalf("serviceProvider: %v", err)
	}

	out, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}

	s := string(out)
	for _, want := range []string{
		"http://localhost:8080/v1/auth/saml/tenant_abc/acs",
		"http://localhost:8080/v1/auth/saml/tenant_abc/metadata",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("metadata missing %q — the IdP would call the wrong endpoint", want)
		}
	}
}
