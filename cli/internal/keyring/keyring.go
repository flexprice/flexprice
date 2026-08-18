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

// probeTimeout bounds how long Open() waits on the OS keychain probe. Some
// backends (a partially configured Linux secret service with D-Bus reachable
// but no prompt agent to answer an unlock request) block indefinitely instead
// of failing fast, which would otherwise freeze the CLI at startup.
const probeTimeout = 2 * time.Second

// Store is the credential backend. Name() is surfaced by whoami so the user can
// always tell where their key actually lives.
type Store interface {
	Set(profile, key string) error
	Get(profile string) (string, error)
	Delete(profile string) error
	Name() string
}

type OSKeyring struct{}

func (OSKeyring) Name() string { return "OS keychain" }

func (OSKeyring) Set(profile, key string) error {
	return oskeyring.Set(service, profile, key)
}

func (OSKeyring) Get(profile string) (string, error) {
	return oskeyring.Get(service, profile)
}

func (OSKeyring) Delete(profile string) error {
	return oskeyring.Delete(service, profile)
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
	probeErr := probeKeychain(os_)
	if probeErr == nil {
		return os_, "", nil
	}

	return &FileStore{Dir: fileDir},
		fmt.Sprintf("No OS keychain available (%v).\n"+
			"Storing your key in %s with mode 0600 instead.\n"+
			"Set FLEXPRICE_KEY_BACKEND=file to silence this warning.", probeErr, fileDir),
		nil
}

// probeKeychain is the only reliable availability check: on Linux the keychain
// fails at call time when libsecret or D-Bus is absent. The call runs on a
// separate goroutine so a hung backend cannot block startup past probeTimeout;
// if it does time out the goroutine is abandoned (and, best-effort, its probe
// entry may be left behind) rather than the CLI hanging indefinitely.
func probeKeychain(s OSKeyring) error {
	done := make(chan error, 1)
	go func() {
		if err := s.Set(service+".probe", "probe"); err != nil {
			done <- err
			return
		}
		done <- s.Delete(service + ".probe")
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(probeTimeout):
		return fmt.Errorf("keychain probe timed out after %s", probeTimeout)
	}
}
