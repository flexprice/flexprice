package types

import "testing"

func resetEESettings(t *testing.T) {
	t.Helper()
	eeSettingsMu.Lock()
	saved := eeSettingDefs
	eeSettingDefs = map[SettingKey]EESettingDefinition{}
	eeSettingsMu.Unlock()
	t.Cleanup(func() {
		eeSettingsMu.Lock()
		eeSettingDefs = saved
		eeSettingsMu.Unlock()
	})
}

func TestRegisterAndLookupEESetting(t *testing.T) {
	resetEESettings(t)
	RegisterEESetting(EESettingDefinition{Key: "ee_demo_key", DefaultValue: map[string]interface{}{}})
	if _, ok := LookupEESetting("ee_demo_key"); !ok {
		t.Fatal("registered EE setting not found")
	}
	if EESettingCount() != 1 {
		t.Fatalf("expected 1, got %d", EESettingCount())
	}
}

func TestLookupEESetting_CommunityEmpty(t *testing.T) {
	resetEESettings(t)
	if _, ok := LookupEESetting("anything"); ok {
		t.Fatal("community build must have no EE settings")
	}
}

func TestRegisterEESetting_RejectsCoreKey(t *testing.T) {
	resetEESettings(t)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic overriding core key")
		}
	}()
	RegisterEESetting(EESettingDefinition{Key: SettingKeyTenantConfig, DefaultValue: map[string]interface{}{}})
}
