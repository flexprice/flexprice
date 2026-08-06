package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// structKeys walks the Configuration struct and returns (scalar leaf paths, map-field
// prefixes). Scalars/slices are exact dotted keys; map fields are prefixes under which any
// sub-key is allowed (e.g. webhook.tenants.<id>.*, auth.api_key.keys.<hash>.*). Mirrors the
// path logic in bindEnvs so the contract and the loader agree by construction.
func structKeys(t reflect.Type, parts []string, scalars map[string]bool, mapPrefixes map[string]bool) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := strings.Split(f.Tag.Get("mapstructure"), ",")[0]
		if tag == "-" {
			continue
		}
		if tag == "" {
			tag = strings.ToLower(f.Name)
		}
		path := append(append([]string{}, parts...), tag)
		key := strings.Join(path, ".")

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.Struct:
			structKeys(ft, path, scalars, mapPrefixes)
		case reflect.Map:
			mapPrefixes[key] = true
		default:
			scalars[key] = true
		}
	}
}

// TestConfigYAMLMatchesStruct is the config contract: every key in config.yaml must map to a
// field in the Configuration struct. A key that doesn't (a typo, a stale key, or a key added
// to the yaml but not the struct) fails the build at PR time instead of silently resolving to
// nothing at runtime. This is the guard that catches the drift class found during the
// autobind refactor (e.g. ECS FLEXPRICE_ENCRYPTION_KEY vs the canonical name).
func TestConfigYAMLMatchesStruct(t *testing.T) {
	scalars := map[string]bool{}
	mapPrefixes := map[string]bool{}
	structKeys(reflect.TypeOf(Configuration{}), nil, scalars, mapPrefixes)

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}

	// reservedPrefixes are config.yaml keys deliberately kept for FUTURE use but not yet
	// wired to the Configuration struct (nothing reads them at runtime). They are allowed
	// here so the contract still catches ACCIDENTAL drift/typos while permitting these
	// DELIBERATE placeholders. When one is actually wired, add the struct field and drop it
	// from this list.
	reservedPrefixes := []string{
		"temporal.client_name",
		"temporal.retry",      // temporal.retry.* — reserved temporal tuning
		"temporal.connection", // temporal.connection.* — reserved temporal tuning
	}

	allowed := func(key string) bool {
		if scalars[key] {
			return true
		}
		for p := range mapPrefixes {
			if key == p || strings.HasPrefix(key, p+".") {
				return true
			}
		}
		for _, p := range reservedPrefixes {
			if key == p || strings.HasPrefix(key, p+".") {
				return true
			}
		}
		return false
	}

	var orphans []string
	for _, key := range v.AllKeys() {
		if !allowed(key) {
			orphans = append(orphans, key)
		}
	}
	if len(orphans) > 0 {
		t.Errorf("config.yaml has %d key(s) that map to no Configuration struct field (typo or stale key):\n  %s",
			len(orphans), strings.Join(orphans, "\n  "))
	}
}
