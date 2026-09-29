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
		{"unset falls back to default", ServerConfig{}, 25 * time.Second},
		{"negative falls back to default", ServerConfig{ShutdownTimeout: -1 * time.Second}, 25 * time.Second},
		{"configured value is used", ServerConfig{ShutdownTimeout: 40 * time.Second}, 40 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetShutdownTimeout(); got != tt.want {
				t.Fatalf("GetShutdownTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}
