package types

import "testing"

func TestValidateZohoEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"empty is allowed before oauth completes", "", false},
		{"zoho com accounts", "https://accounts.zoho.com", false},
		{"zoho eu accounts", "https://accounts.zoho.eu", false},
		{"zoho api domain default", "https://www.zohoapis.com", false},
		{"zoho in api domain", "https://www.zohoapis.in", false},
		{"zoho com au", "https://accounts.zoho.com.au", false},
		{"bare zoho domain", "https://zoho.com", false},

		// A host chosen at connection creation would otherwise receive this
		// connection's client_id and client_secret on every token refresh.
		{"attacker host rejected", "https://evil.example.com", true},
		{"lookalike suffix rejected", "https://zoho.com.evil.example", true},
		{"http rejected", "http://accounts.zoho.com", true},
		{"relative rejected", "/oauth/v2/token", true},
		{"not a url rejected", "not a url", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateZohoEndpoint(tt.url, "accounts_server")
			if tt.wantErr && err == nil {
				t.Fatalf("expected %q to be rejected, got nil error", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected %q to be allowed, got: %v", tt.url, err)
			}
		})
	}
}

func TestValidateGoogleEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"sts endpoint", "https://sts.googleapis.com/v1/token", false},
		{"iam credentials endpoint", "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/x:generateAccessToken", false},

		{"attacker host rejected", "https://evil.example.com/token", true},
		{"lookalike suffix rejected", "https://googleapis.com.evil.example/token", true},
		// Serves tenant-controlled content, so it must not be reachable even
		// though it is a genuine Google domain.
		{"other google host rejected", "https://storage.googleapis.com/token", true},
		{"metadata service rejected", "https://169.254.169.254/token", true},
		{"http rejected", "http://sts.googleapis.com/v1/token", true},
		{"empty rejected", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGoogleEndpoint(tt.url, "token_url")
			if tt.wantErr && err == nil {
				t.Fatalf("expected %q to be rejected, got nil error", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected %q to be allowed, got: %v", tt.url, err)
			}
		})
	}
}

func TestIsValidChargebeeSite(t *testing.T) {
	tests := []struct {
		site string
		want bool
	}{
		{"acme", true},
		{"acme-test", true},
		{"acme123", true},

		// The SDK appends .chargebee.com, so URL syntax here changes which host
		// is contacted rather than which site is addressed.
		{"acme#evil.com", false},
		{"acme.evil.com", false},
		{"acme/path", false},
		{"acme:8080", false},
		{"acme?a=b", false},
		{"-acme", false},
		{"acme-", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.site, func(t *testing.T) {
			if got := isValidChargebeeSite(tt.site); got != tt.want {
				t.Fatalf("isValidChargebeeSite(%q) = %v, want %v", tt.site, got, tt.want)
			}
		})
	}
}
