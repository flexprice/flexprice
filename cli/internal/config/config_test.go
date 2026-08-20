package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestSaveLoad_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	in := &Config{
		DefaultProfile: "sandbox",
		Profiles: map[string]Profile{
			"sandbox": {
				Region:  "in",
				BaseURL: "https://api.cloud.flexprice.io/v1",
				Label:   "Sandbox",
				KeyRef:  "keychain:flexprice/sandbox",
			},
		},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := out.Profiles["sandbox"]
	if !ok {
		t.Fatal("profile missing after round trip")
	}
	if got.Region != "in" || got.Label != "Sandbox" {
		t.Errorf("profile = %+v, want region in and label Sandbox", got)
	}
}

func TestSave_UsesRestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.toml")

	if err := Save(path, &Config{Profiles: map[string]Profile{}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}
}

func TestLoad_MissingFileReturnsEmptyConfig(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("Profiles = %v, want empty", cfg.Profiles)
	}
}

func TestProfileName_SlugifiesUserInput(t *testing.T) {
	if got := ProfileName("My Sandbox"); got != "my-sandbox" {
		t.Errorf("ProfileName = %q, want %q", got, "my-sandbox")
	}
	if got := ProfileName(""); got != "default" {
		t.Errorf("ProfileName(\"\") = %q, want %q", got, "default")
	}
}

// Punctuation-only, separator-heavy, and unicode input must always slugify to
// something non-empty and usable as a bare TOML key.
func TestProfileName_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  string
	}{
		{"punctuation only", "!!!", "default"},
		{"separators only", "   ---   ", "default"},
		{"leading and trailing separators", "--My Sandbox--", "my-sandbox"},
		{"leading and trailing spaces", "  prod  ", "prod"},
		// "ü" is outside [a-z0-9], so it becomes a separator; the surrounding
		// letters survive and the result is still a valid, non-empty slug.
		{"unicode letters mixed with ascii", "Prodüktion", "prod-ktion"},
		// A label with no ASCII alphanumeric character at all has nothing to
		// build a slug from and must fall back rather than yield "".
		{"unicode only", "Üöä", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProfileName(tt.label)
			if got != tt.want {
				t.Errorf("ProfileName(%q) = %q, want %q", tt.label, got, tt.want)
			}
			if got == "" {
				t.Errorf("ProfileName(%q) returned empty string", tt.label)
			}
			// A bare TOML key permits [A-Za-z0-9_-]; the slugifier's alphabet
			// is a subset, so it must round-trip without quoting.
			cfg := &Config{Profiles: map[string]Profile{got: {Label: tt.label}}}
			var buf strings.Builder
			if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
				t.Fatalf("encode with key %q: %v", got, err)
			}
			var decoded Config
			if err := toml.Unmarshal([]byte(buf.String()), &decoded); err != nil {
				t.Fatalf("decode with key %q: %v", got, err)
			}
			if _, ok := decoded.Profiles[got]; !ok {
				t.Errorf("key %q did not round-trip through TOML: %s", got, buf.String())
			}
		})
	}
}

// BurntSushi/toml merges into a non-empty map rather than replacing it; reusing
// a *Config would silently resurrect stale profiles.
func TestUnmarshal_MergesIntoExistingMap(t *testing.T) {
	cfg := &Config{Profiles: map[string]Profile{
		"stale":   {Region: "stale-region"},
		"sandbox": {Region: "old-value"},
	}}
	data := []byte(`
[profiles.sandbox]
region = "in"
`)
	if err := toml.Unmarshal(data, cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := cfg.Profiles["stale"]; !ok {
		t.Error("Unmarshal removed a map key absent from the source; expected it to survive (merge, not replace)")
	}
	if got := cfg.Profiles["sandbox"].Region; got != "in" {
		t.Errorf("sandbox.Region = %q, want %q (value from source should win)", got, "in")
	}
}

// If the commit step fails, the existing file must be intact. Forced by making
// path a directory so os.Rename fails after the temp file is fully written.
func TestSave_FailurePreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir marker: %v", err)
	}
	marker := filepath.Join(path, "marker")
	if err := os.WriteFile(marker, []byte("keep-me"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	err := Save(path, &Config{Profiles: map[string]Profile{"x": {Region: "us"}}})
	if err == nil {
		t.Fatal("expected Save to fail when path collides with an existing directory")
	}

	if _, err := os.Stat(marker); err != nil {
		t.Errorf("existing directory content did not survive a failed Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("leftover temp file after failed Save: %s", e.Name())
		}
	}
}
