package keyring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests guard against the bug where OSKeyring's real Set/Get/Delete
// calls — as used by login/logout/whoami once Open has already picked
// OSKeyring as the backend — were direct, unwrapped calls into the OS
// keychain library with no bound at all. A keychain session expiring, or a
// fresh unlock prompt appearing, between the startup probe and the real
// operation could then hang the CLI forever.
//
// Each test substitutes a fake backend via the keychainSet/keychainGet/
// keychainDelete indirections and shrinks keychainOpTimeout so the timeout
// path is exercised in milliseconds rather than seconds; the real OS
// keychain is never touched. The `started` channel synchronizes with the
// fake actually being invoked before the deferred restore runs, so the
// restore's write to the package var can't race with the abandoned
// goroutine's read of it.

func TestOSKeyring_Set_TimesOutWhenKeychainNeverResponds(t *testing.T) {
	restoreSet, restoreTimeout := keychainSet, keychainOpTimeout
	defer func() { keychainSet, keychainOpTimeout = restoreSet, restoreTimeout }()
	keychainOpTimeout = 50 * time.Millisecond

	started := make(chan struct{})
	block := make(chan struct{}) // never closed: simulates a keychain call that never returns
	defer close(block)           // unstick the abandoned goroutine once the test is done

	keychainSet = func(service, user, password string) error {
		close(started)
		<-block
		return nil
	}

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- OSKeyring{}.Set("acme", "sk_test") }()
	<-started

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Set returned nil for a keychain call that never responds; expected a timeout error")
		}
		if elapsed := time.Since(start); elapsed > 1*time.Second {
			t.Errorf("Set took %s to time out; expected roughly keychainOpTimeout (%s)", elapsed, keychainOpTimeout)
		}
		if !strings.Contains(err.Error(), "FLEXPRICE_KEY_BACKEND=file") {
			t.Errorf("timeout error %q does not mention the file-backend workaround", err.Error())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Set blocked past a 1s grace period with no timeout error; the real keychain call is unbounded")
	}
}

func TestOSKeyring_Get_TimesOutWhenKeychainNeverResponds(t *testing.T) {
	restoreGet, restoreTimeout := keychainGet, keychainOpTimeout
	defer func() { keychainGet, keychainOpTimeout = restoreGet, restoreTimeout }()
	keychainOpTimeout = 50 * time.Millisecond

	started := make(chan struct{})
	block := make(chan struct{})
	defer close(block)

	keychainGet = func(service, user string) (string, error) {
		close(started)
		<-block
		return "", nil
	}

	type result struct {
		val string
		err error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() { v, err := OSKeyring{}.Get("acme"); done <- result{v, err} }()
	<-started

	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("Get returned nil error for a keychain call that never responds; expected a timeout error")
		}
		if elapsed := time.Since(start); elapsed > 1*time.Second {
			t.Errorf("Get took %s to time out; expected roughly keychainOpTimeout (%s)", elapsed, keychainOpTimeout)
		}
		if !strings.Contains(r.err.Error(), "FLEXPRICE_KEY_BACKEND=file") {
			t.Errorf("timeout error %q does not mention the file-backend workaround", r.err.Error())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Get blocked past a 1s grace period with no timeout error; the real keychain call is unbounded")
	}
}

func TestOSKeyring_Delete_TimesOutWhenKeychainNeverResponds(t *testing.T) {
	restoreDelete, restoreTimeout := keychainDelete, keychainOpTimeout
	defer func() { keychainDelete, keychainOpTimeout = restoreDelete, restoreTimeout }()
	keychainOpTimeout = 50 * time.Millisecond

	started := make(chan struct{})
	block := make(chan struct{})
	defer close(block)

	keychainDelete = func(service, user string) error {
		close(started)
		<-block
		return nil
	}

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- OSKeyring{}.Delete("acme") }()
	<-started

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Delete returned nil for a keychain call that never responds; expected a timeout error")
		}
		if elapsed := time.Since(start); elapsed > 1*time.Second {
			t.Errorf("Delete took %s to time out; expected roughly keychainOpTimeout (%s)", elapsed, keychainOpTimeout)
		}
		if !strings.Contains(err.Error(), "FLEXPRICE_KEY_BACKEND=file") {
			t.Errorf("timeout error %q does not mention the file-backend workaround", err.Error())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Delete blocked past a 1s grace period with no timeout error; the real keychain call is unbounded")
	}
}

// TestOSKeyring_Set_SucceedsWithoutWaitingForTimeout confirms the timeout
// wrapping doesn't penalize the normal case: a fast-returning backend should
// resolve immediately, not wait out keychainOpTimeout.
func TestOSKeyring_Set_SucceedsWithoutWaitingForTimeout(t *testing.T) {
	restoreSet, restoreTimeout := keychainSet, keychainOpTimeout
	defer func() { keychainSet, keychainOpTimeout = restoreSet, restoreTimeout }()
	keychainOpTimeout = 5 * time.Second

	var gotService, gotUser, gotPassword string
	keychainSet = func(service, user, password string) error {
		gotService, gotUser, gotPassword = service, user, password
		return nil
	}

	start := time.Now()
	if err := (OSKeyring{}).Set("acme-production", "sk_test_abc"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Errorf("Set took %s for a fast backend; timeout wrapping should not add latency", elapsed)
	}
	if gotService != service || gotUser != "acme-production" || gotPassword != "sk_test_abc" {
		t.Errorf("Set forwarded (%q, %q, %q), want (%q, %q, %q)",
			gotService, gotUser, gotPassword, service, "acme-production", "sk_test_abc")
	}
}

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
