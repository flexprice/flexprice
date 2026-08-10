package dto

import (
	"net"
	"testing"

	"github.com/flexprice/flexprice/internal/validator"
)

// Regression for SSRF-VULN-01: file_url is fetched server-side, so
// CreateTaskRequest.Validate must reject internal / non-https targets while
// still accepting legitimate public https file hosts.
func TestCreateTaskRequest_Validate_FileURL_SSRF(t *testing.T) {
	// Resolve hostnames to a fixed public address so the public-host case does
	// not depend on live DNS. Literal-IP cases below bypass resolution entirely
	// and still exercise the address classification.
	validator.StubResolverForTest(t, net.ParseIP("93.184.216.34"))

	base := func(url string) *CreateTaskRequest {
		return &CreateTaskRequest{
			TaskType:   "IMPORT",
			EntityType: "EVENTS",
			FileURL:    url,
			FileType:   "CSV",
		}
	}
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"cloud metadata endpoint", "http://169.254.169.254/latest/meta-data/iam/security-credentials/", true},
		{"localhost", "http://127.0.0.1/secret", true},
		{"private RFC1918", "https://10.0.0.5/internal", true},
		{"plain http public", "http://files.example.com/data.csv", true},
		{"legit public https", "https://drive.google.com/file/d/123/view", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := base(tc.url).Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected rejection for %q, got nil", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected %q to pass, got %v", tc.url, err)
			}
		})
	}
}
