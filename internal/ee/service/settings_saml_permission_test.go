package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// samlEnabledService builds the minimum service needed to exercise the
// deployment switch.
func samlEnabledService(enabled bool) *settingsService {
	cfg := &config.Configuration{}
	cfg.Auth.SAML.Enabled = enabled
	return &settingsService{ServiceParams: ServiceParams{Config: cfg}}
}

// TestRequireSAMLEnabled covers the deployment-level kill switch. Who may reach
// the key is the router's business — every settings route requires a super_admin
// user — but the router cannot know whether the feature exists at all, and a
// configuration stored on a deployment serving no SAML routes would only sit
// there looking as though SSO were set up.
func TestRequireSAMLEnabled(t *testing.T) {
	if err := samlEnabledService(true).requireSAMLEnabled(types.SettingKeySAMLConfig); err != nil {
		t.Fatalf("a deployment offering SAML must accept the key: %v", err)
	}

	err := samlEnabledService(false).requireSAMLEnabled(types.SettingKeySAMLConfig)
	if err == nil {
		t.Fatal("a deployment with SAML disabled must refuse the key")
	}
	// Reported as absent rather than forbidden: the feature does not exist here.
	if !ierr.IsNotFound(err) {
		t.Errorf("expected a not-found error, got %v", err)
	}

	// Every other setting is untouched by the SAML switch.
	for _, key := range []types.SettingKey{types.SettingKeyInvoiceConfig, types.SettingKeyTenantConfig} {
		if err := samlEnabledService(false).requireSAMLEnabled(key); err != nil {
			t.Errorf("setting %q must not be gated on the SAML switch: %v", key, err)
		}
	}
}

// TestActiveIsNotSettableThroughTheAPI is the test that matters for the ops
// gate: "active" is Flexprice's approval that a tenant may serve SSO, so a
// tenant able to set it in an ordinary settings write would be approving itself.
func TestActiveIsNotSettableThroughTheAPI(t *testing.T) {
	protected := apiImmutableSettingFields(types.SettingKeySAMLConfig)
	if !lo.Contains(protected, "active") {
		t.Fatalf(`"active" must be protected from API writes, got %v`, protected)
	}

	stored := map[string]interface{}{"enabled": true, "active": false, "enforce_sso": false}
	request := map[string]interface{}{"active": true, "enforce_sso": true}

	merged := mergePreservingImmutableFields(types.SettingKeySAMLConfig, stored, request)

	if merged["active"] != false {
		t.Error("a request setting active:true must not grant approval")
	}
	// The tenant's own fields still apply — only the approval is pinned.
	if merged["enforce_sso"] != true {
		t.Error("enforce_sso is the tenant's own setting and must remain writable")
	}
	if merged["enabled"] != true {
		t.Error("untouched fields must survive the merge")
	}
}

// TestMergePreservingImmutableFieldsLeavesOtherKeysAlone confirms the merge is
// unchanged for every setting that has no protected fields.
func TestMergePreservingImmutableFieldsLeavesOtherKeysAlone(t *testing.T) {
	stored := map[string]interface{}{"a": 1, "b": 2}
	request := map[string]interface{}{"b": 3, "c": 4}

	merged := mergePreservingImmutableFields(types.SettingKeyInvoiceConfig, stored, request)

	for field, want := range map[string]interface{}{"a": 1, "b": 3, "c": 4} {
		if merged[field] != want {
			t.Errorf("field %q = %v, want %v", field, merged[field], want)
		}
	}
}
