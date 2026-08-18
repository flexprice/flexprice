// Package keyring stores API keys. It prefers the OS keychain and falls back to
// an obfuscated file when no keychain is available — common in containers and WSL.
package keyring

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	oskeyring "github.com/zalando/go-keyring"
)

const service = "flexprice"

// Some backends (a Linux secret service with no prompt agent) block
// indefinitely instead of failing fast, which would freeze the CLI at startup.
const probeTimeout = 2 * time.Second

// Longer than probeTimeout: a real operation can involve the user answering an
// OS unlock prompt, which 2s would false-timeout on.
var keychainOpTimeout = 8 * time.Second

// Store is the credential backend. Name() is surfaced by whoami so the user can
// always tell where their key actually lives.
type Store interface {
	Set(profile, key string) error
	Get(profile string) (string, error)
	Delete(profile string) error
	Name() string
}

// Indirections so tests can substitute a fake backend without touching the
// real OS keychain.
var (
	keychainSet    = oskeyring.Set
	keychainGet    = oskeyring.Get
	keychainDelete = oskeyring.Delete
)

type OSKeyring struct{}

func (OSKeyring) Name() string { return "OS keychain" }

func (OSKeyring) Set(profile, key string) error {
	_, err := withTimeout(keychainOpTimeout, "set", func() (struct{}, error) {
		return struct{}{}, keychainSet(service, profile, key)
	})
	return err
}

func (OSKeyring) Get(profile string) (string, error) {
	return withTimeout(keychainOpTimeout, "get", func() (string, error) {
		return keychainGet(service, profile)
	})
}

func (OSKeyring) Delete(profile string) error {
	_, err := withTimeout(keychainOpTimeout, "delete", func() (struct{}, error) {
		return struct{}{}, keychainDelete(service, profile)
	})
	return err
}

// Open returns the OS keychain when it works, otherwise the file fallback.
// warn is non-empty when the fallback was selected and the caller has not opted
// in via FLEXPRICE_KEY_BACKEND=file; callers print it once.
func Open() (store Store, warn string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("locate home directory: %w", err)
	}
	fileDir := filepath.Join(home, ".flexprice", "keys")

	if os.Getenv("FLEXPRICE_KEY_BACKEND") == "file" {
		return &FileStore{Dir: fileDir}, "", nil
	}

	os_ := OSKeyring{}
	probeErr := probeKeychain()
	if probeErr == nil {
		return os_, "", nil
	}

	return &FileStore{Dir: fileDir},
		fmt.Sprintf("No OS keychain available (%v).\n"+
			"Storing your key in %s with mode 0600 instead.\n"+
			"Set FLEXPRICE_KEY_BACKEND=file to silence this warning.", probeErr, fileDir),
		nil
}

// Calls the raw functions rather than OSKeyring's methods so probeTimeout
// applies instead of keychainOpTimeout.
func probeKeychain() error {
	_, err := withTimeout(probeTimeout, "probe", func() (struct{}, error) {
		if err := keychainSet(service, service+".probe", "probe"); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, keychainDelete(service, service+".probe")
	})
	return err
}

// A hung OS keychain call must never block the CLI forever. On timeout the
// goroutine is abandoned — best-effort, it may complete later unobserved.
func withTimeout[T any](timeout time.Duration, op string, fn func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := fn()
		done <- result{v, err}
	}()

	select {
	case r := <-done:
		return r.val, r.err
	case <-time.After(timeout):
		var zero T
		return zero, fmt.Errorf(
			"OS keychain %s timed out after %s; it may be waiting on an OS unlock/consent prompt.\n"+
				"Set FLEXPRICE_KEY_BACKEND=file to bypass the OS keychain.", op, timeout)
	}
}
