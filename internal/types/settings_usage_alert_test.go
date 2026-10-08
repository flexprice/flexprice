package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsageAlertConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     UsageAlertConfig
		wantErr bool
	}{
		{name: "unset", cfg: UsageAlertConfig{}},
		{name: "in range", cfg: UsageAlertConfig{ScheduleDelaySeconds: 120, StaleAfterSeconds: 1800}},
		{name: "delay below min", cfg: UsageAlertConfig{ScheduleDelaySeconds: 29}, wantErr: true},
		{name: "delay above max", cfg: UsageAlertConfig{ScheduleDelaySeconds: 3601}, wantErr: true},
		{name: "negative delay", cfg: UsageAlertConfig{ScheduleDelaySeconds: -1}, wantErr: true},
		{name: "stale above max", cfg: UsageAlertConfig{StaleAfterSeconds: 86401}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantErr, tt.cfg.Validate() != nil)
		})
	}
}
