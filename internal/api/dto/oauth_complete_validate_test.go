package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCompleteOAuthRequest_Validate_Identifiers(t *testing.T) {
	base := CompleteOAuthRequest{SessionID: "s", Code: "c", State: "st"}
	tests := []struct {
		name     string
		provider types.OAuthProvider
		realm    string
		org      string
		wantErr  bool
	}{
		{"qb numeric", types.OAuthProviderQuickBooks, "9130350000000000", "", false},
		{"qb traversal", types.OAuthProviderQuickBooks, "../x", "", true},
		{"qb query", types.OAuthProviderQuickBooks, "1?a=b", "", true},
		{"qb empty", types.OAuthProviderQuickBooks, "", "", true},
		{"zoho numeric", types.OAuthProviderZohoBooks, "", "123456", false},
		{"zoho traversal", types.OAuthProviderZohoBooks, "", "../x", true},
		{"zoho userinfo", types.OAuthProviderZohoBooks, "", "1@evil", true},
		{"zoho empty", types.OAuthProviderZohoBooks, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base
			r.Provider = tt.provider
			r.RealmID = tt.realm
			r.OrganizationID = tt.org
			err := r.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
