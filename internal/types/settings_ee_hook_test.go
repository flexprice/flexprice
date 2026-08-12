package types

import "testing"

func withNoEESettings(t *testing.T) {
	t.Helper()
	eeSettingsMu.Lock()
	original := eeSettingDefs
	eeSettingDefs = map[SettingKey]EESettingDefinition{}
	eeSettingsMu.Unlock()

	t.Cleanup(func() {
		eeSettingsMu.Lock()
		eeSettingDefs = original
		eeSettingsMu.Unlock()
	})
}

// TestCommunityBuildRejectsUnknownKeys pins the behaviour a community build must
// keep: with no enterprise registrations, an unregistered key is still invalid.
func TestCommunityBuildRejectsUnknownKeys(t *testing.T) {
	withNoEESettings(t)

	key := SettingKey("enterprise_example_config")
	if err := key.Validate(); err == nil {
		t.Error("an unregistered key must be rejected in a community build")
	}

	valid := SettingKeyTenantConfig
	if err := valid.Validate(); err != nil {
		t.Errorf("built-in key rejected: %v", err)
	}
}

// TestEESettingBecomesValidOnceRegistered covers the whole point of the hook:
// an enterprise key passes validation and carries its default and scope.
func TestEESettingBecomesValidOnceRegistered(t *testing.T) {
	withNoEESettings(t)

	key := SettingKey("enterprise_example_config")
	RegisterEESetting(EESettingDefinition{
		Key:          key,
		DefaultValue: map[string]interface{}{"enabled": false},
		TenantLevel:  true,
	})

	if err := key.Validate(); err != nil {
		t.Fatalf("registered ee key rejected: %v", err)
	}

	def, ok := LookupEESetting(key)
	if !ok {
		t.Fatal("registered ee key did not resolve")
	}
	if !def.TenantLevel {
		t.Error("TenantLevel not carried through — the setting would be scoped per environment")
	}

	defaults, err := GetDefaultSettings()
	if err != nil {
		t.Fatalf("GetDefaultSettings: %v", err)
	}
	if _, ok := defaults[key]; !ok {
		t.Error("ee default missing from GetDefaultSettings — GetSetting would error on first read")
	}
	if _, ok := defaults[SettingKeyTenantConfig]; !ok {
		t.Error("ee registration displaced a built-in default")
	}
}

// TestEESettingRejectsInvalidRegistrations covers the cases that stay
// programming errors: an empty key, shadowing a built-in, and a duplicate.
func TestEESettingRejectsInvalidRegistrations(t *testing.T) {
	cases := []struct {
		name string
		run  func()
	}{
		{"empty key", func() { RegisterEESetting(EESettingDefinition{}) }},
		{"shadows built-in", func() {
			RegisterEESetting(EESettingDefinition{Key: SettingKeyTenantConfig})
		}},
		{"duplicate", func() {
			RegisterEESetting(EESettingDefinition{Key: "dup"})
			RegisterEESetting(EESettingDefinition{Key: "dup"})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withNoEESettings(t)
			defer func() {
				if recover() == nil {
					t.Errorf("%s should panic", tc.name)
				}
			}()
			tc.run()
		})
	}
}
