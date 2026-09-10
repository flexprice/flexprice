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

func TestRegisterEESetting_RejectsEmptyKey(t *testing.T) {
	resetEESettings(t)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty key")
		}
	}()
	RegisterEESetting(EESettingDefinition{Key: "", DefaultValue: map[string]interface{}{}})
}

func TestRegisterEESetting_RejectsDuplicate(t *testing.T) {
	resetEESettings(t)
	RegisterEESetting(EESettingDefinition{Key: "ee_dup", DefaultValue: map[string]interface{}{}})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	RegisterEESetting(EESettingDefinition{Key: "ee_dup", DefaultValue: map[string]interface{}{}})
}

func TestSettingKeyValidate_AcceptsRegisteredEEKey(t *testing.T) {
	resetEESettings(t)
	key := SettingKey("ee_registered_key")
	if err := key.Validate(); err == nil {
		t.Fatal("unregistered EE key must fail Validate")
	}
	RegisterEESetting(EESettingDefinition{Key: key, DefaultValue: map[string]interface{}{}})
	if err := key.Validate(); err != nil {
		t.Fatalf("registered EE key must pass Validate, got %v", err)
	}
}
