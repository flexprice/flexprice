// Package config manages ~/.flexprice/config.toml. It holds no secrets: keys live
// in the keyring and are referenced by KeyRef.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// The atomic auth unit: a key is scoped to one environment, so region, base URL
// and key move together. Deliberately carries no environment name or live flag —
// no endpoint reveals which environment a key belongs to, so either would be a
// guess, and users label profiles themselves (ADR 0003).
type Profile struct {
	Region  string `toml:"region"`
	BaseURL string `toml:"base_url"`
	Label   string `toml:"label"` // free text, set by the user; purely informational
	KeyRef  string `toml:"key_ref"`
}

type Config struct {
	DefaultProfile string             `toml:"default_profile"`
	Profiles       map[string]Profile `toml:"profiles"`
}

// DefaultPath is ~/.flexprice/config.toml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".flexprice", "config.toml"), nil
}

func Load(path string) (*Config, error) {
	cfg := &Config{Profiles: map[string]Profile{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return cfg, nil
}

// Save writes cfg to path atomically: it encodes to a temp file in the same
// directory and renames it into place, so a crash or interrupt mid-write
// cannot leave a truncated config on disk.
func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	// Belt-and-suspenders: MkdirAll already requests 0700 and umask can only
	// clear bits, never add them, so this is a no-op under any umask that
	// (like every umask in practice) leaves the owner bits alone. It only
	// matters if MkdirAll's mode is ever loosened later.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup: once the rename below succeeds this is a no-op
	// (the path no longer exists), so the error is not worth surfacing.
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temp file: %w", err)
	}
	if err := toml.NewEncoder(tmp).Encode(cfg); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Resolve returns the named profile, or the default when name is empty.
func (c *Config) Resolve(name string) (string, Profile, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		return "", Profile{}, fmt.Errorf("no profile configured — run: flexprice init")
	}
	p, ok := c.Profiles[name]
	if !ok {
		return "", Profile{}, fmt.Errorf("profile %q not found — see: flexprice config list", name)
	}
	return name, p, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugifies a user-supplied label. Input with no ASCII alphanumeric character
// at all (punctuation-only, or pure unicode like "üöä") collapses to "", which
// falls back to "default" rather than an unusable TOML key.
func ProfileName(label string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if slug == "" {
		return "default"
	}
	return slug
}
