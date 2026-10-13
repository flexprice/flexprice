package config

import (
	"testing"
	"time"
)

func TestServerConfigGetShutdownTimeout(t *testing.T) {
	tests := []struct {
		name string
		cfg  ServerConfig
		want time.Duration
	}{
		{"unset falls back to default", ServerConfig{}, DefaultServerShutdownTimeout},
		{"negative falls back to default", ServerConfig{ShutdownTimeout: -1 * time.Second}, DefaultServerShutdownTimeout},
		{"configured value is used", ServerConfig{ShutdownTimeout: 40 * time.Second}, 40 * time.Second},
		{"value above ceiling is clamped", ServerConfig{ShutdownTimeout: 5 * time.Minute}, MaxServerShutdownTimeout},
	}

	if MaxServerShutdownTimeout >= ServerStopTimeout {
		t.Fatalf("drain ceiling %v must stay below fx stop budget %v", MaxServerShutdownTimeout, ServerStopTimeout)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetShutdownTimeout(); got != tt.want {
				t.Fatalf("GetShutdownTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}
