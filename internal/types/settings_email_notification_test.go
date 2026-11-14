package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailNotificationConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     EmailNotificationConfig
		wantErr bool
	}{
		{name: "auto-send off ignores entities", cfg: EmailNotificationConfig{AutoSend: EmailAutoSendConfig{Entities: []SystemEntityType{"wallet"}}}},
		{name: "invoice", cfg: EmailNotificationConfig{Enabled: true, AutoSend: EmailAutoSendConfig{Enabled: true, Entities: []SystemEntityType{SystemEntityTypeInvoice}}}},
		{name: "no entities", cfg: EmailNotificationConfig{Enabled: true, AutoSend: EmailAutoSendConfig{Enabled: true}}, wantErr: true},
		{name: "emails off", cfg: EmailNotificationConfig{AutoSend: EmailAutoSendConfig{Enabled: true, Entities: []SystemEntityType{SystemEntityTypeInvoice}}}, wantErr: true},
		{name: "unsupported entity", cfg: EmailNotificationConfig{Enabled: true, AutoSend: EmailAutoSendConfig{Enabled: true, Entities: []SystemEntityType{"wallet"}}}, wantErr: true},
		{name: "empty entity", cfg: EmailNotificationConfig{Enabled: true, AutoSend: EmailAutoSendConfig{Enabled: true, Entities: []SystemEntityType{""}}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantErr, tt.cfg.Validate() != nil)
		})
	}
}
