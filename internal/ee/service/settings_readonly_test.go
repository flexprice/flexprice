package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

// revenue_recognition_config is changed by Flexprice operators only, never through the public settings API.
func TestRevenueRecognitionConfigIsNotWritableThroughTheAPI(t *testing.T) {
	s := samlEnabledService(false)
	ctx := context.Background()

	_, err := s.UpdateSettingByKey(ctx, types.SettingKeyRevenueRecognitionConfig, &dto.UpdateSettingRequest{Value: map[string]interface{}{"enabled": true}})
	if !ierr.IsValidation(err) {
		t.Errorf("update: expected a validation error, got %v", err)
	}
	if err := s.DeleteSettingByKey(ctx, types.SettingKeyRevenueRecognitionConfig); !ierr.IsValidation(err) {
		t.Errorf("delete: expected a validation error, got %v", err)
	}
}
