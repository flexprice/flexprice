package keyring

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStore_RoundTrips(t *testing.T) {
	s := &FileStore{Dir: t.TempDir()}

	if err := s.Set("acme-production", "sk_test_abc"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get("acme-production")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "sk_test_abc" {
		t.Errorf("Get = %q, want %q", got, "sk_test_abc")
	}

	if err := s.Delete("acme-production"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("acme-production"); err == nil {
		t.Error("Get after Delete returned no error")
	}
}

func TestFileStore_WritesRestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	if err := s.Set("p", "sk_1"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dir, "p.key"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestFileStore_StoredBytesAreNotPlaintext(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	if err := s.Set("p", "sk_supersecret"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "p.key"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) == "sk_supersecret" {
		t.Error("key stored as plaintext")
	}
}

func TestFileStore_BackendNameIsReportable(t *testing.T) {
	s := &FileStore{Dir: t.TempDir()}
	if s.Name() == "" {
		t.Error("Name() is empty; whoami needs to report the active backend")
	}
}
