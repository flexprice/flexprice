package types

import "testing"

func TestValidateProviderIdentifiers(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"numeric", "9130350000000000", false},
		{"short numeric", "123", false},
		{"empty", "", true},
		{"traversal", "../x", true},
		{"query", "a?b=c", true},
		{"userinfo", "a@b", true},
		{"non numeric", "abc", true},
		{"slash", "1/2", true},
		{"space", "1 2", true},
		{"newline suffix", "123\n", true},
		{"too long", "123456789012345678901234567890123", true},
	}
	validators := map[string]func(string) error{
		"realm_id":        ValidateQuickBooksRealmID,
		"organization_id": ValidateZohoOrganizationID,
	}
	for field, fn := range validators {
		for _, tt := range tests {
			t.Run(field+"/"+tt.name, func(t *testing.T) {
				err := fn(tt.in)
				if (err != nil) != tt.wantErr {
					t.Fatalf("%q: err=%v wantErr=%v", tt.in, err, tt.wantErr)
				}
			})
		}
	}
}

func TestQuickBooksMetadataValidate_RealmID(t *testing.T) {
	base := QuickBooksConnectionMetadata{ClientID: "id", ClientSecret: "s", Environment: "sandbox"}
	for _, realm := range []string{"", "../x", "a?b=c"} {
		m := base
		m.RealmID = realm
		if err := m.Validate(); err == nil {
			t.Fatalf("realm %q accepted", realm)
		}
	}
	m := base
	m.RealmID = "123456"
	if err := m.Validate(); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	m.Environment = "evil"
	if err := m.Validate(); err == nil {
		t.Fatal("bad environment accepted")
	}
}

func TestZohoMetadataValidate_OrganizationID(t *testing.T) {
	m := ZohoBooksConnectionMetadata{ClientID: "id", ClientSecret: "s", RefreshToken: "r", OrganizationID: "../x"}
	if err := m.Validate(); err == nil {
		t.Fatal("bad org accepted")
	}
	m.OrganizationID = "20012345678"
	if err := m.Validate(); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	m.APIDomain = "https://evil.example.com"
	if err := m.Validate(); err == nil {
		t.Fatal("non-zoho api_domain accepted")
	}
}

func TestZohoMetadataValidate_Endpoints(t *testing.T) {
	tests := []struct {
		name     string
		accounts string
		api      string
		wantErr  bool
	}{
		{"empty before oauth", "", "", false},
		{"bare origins", "https://accounts.zoho.com", "https://www.zohoapis.com", false},
		{"trailing slash tolerated", "https://accounts.zoho.eu/", "https://www.zohoapis.eu/", false},
		{"accounts path rejected", "https://accounts.zoho.com/oauth", "", true},
		{"accounts query rejected", "https://accounts.zoho.com?x=1", "", true},
		{"accounts userinfo rejected", "https://u:p@accounts.zoho.com", "", true},
		{"api fragment rejected", "", "https://www.zohoapis.com#f", true},
		{"api path rejected", "", "https://www.zohoapis.com/books", true},
		{"api non-zoho rejected", "", "https://evil.example.com", true},
		{"accounts http rejected", "http://accounts.zoho.com", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ZohoBooksConnectionMetadata{
				ClientID: "id", ClientSecret: "s", RefreshToken: "r", OrganizationID: "1",
				AccountsURL: tt.accounts, APIDomain: tt.api,
			}
			if err := m.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
