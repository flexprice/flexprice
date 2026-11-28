package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

// A key is either read-only or has immutable fields, never both: a read-only key never reaches the field merge.
func TestReadOnlySettingsHaveNoImmutableFields(t *testing.T) {
	defaults, err := types.GetDefaultSettings()
	if err != nil {
		t.Fatal(err)
	}
	for key := range defaults {
		if rejectAPIReadOnlySetting(key) != nil && len(apiImmutableSettingFields(key)) > 0 {
			t.Errorf("%s is read-only and also lists immutable fields", key)
		}
	}
}
