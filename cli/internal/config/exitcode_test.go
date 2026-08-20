package config

import (
	"errors"
	"testing"

	"github.com/flexprice/cli/internal/exitcode"
)

// Errors raised before any HTTP call previously all exited 1, so a script could
// not tell "needs login" from any other failure. Found by cli/scripts/smoke.sh.
func TestResolveContext_ErrorsCarryExitCodes(t *testing.T) {
	cases := []struct {
		name string
		cfg  *Config
		over Overrides
		want int
	}{
		{
			name: "no credentials at all",
			cfg:  &Config{Profiles: map[string]Profile{}},
			over: Overrides{},
			want: exitcode.Auth,
		},
		{
			name: "key with no region",
			cfg:  &Config{Profiles: map[string]Profile{}},
			over: Overrides{APIKey: "sk_test"},
			want: exitcode.Usage,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveContext(tc.cfg, nil, tc.over)
			if err == nil {
				t.Fatal("expected an error")
			}
			var coded interface{ ExitCode() int }
			if !errors.As(err, &coded) {
				t.Fatalf("error carries no exit code, so main exits 1: %v", err)
			}
			if got := coded.ExitCode(); got != tc.want {
				t.Errorf("ExitCode() = %d, want %d — %v", got, tc.want, err)
			}
		})
	}
}
