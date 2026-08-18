package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The fallback when no OS keychain exists. Encrypted with a host-derived key to
// stop casual disclosure; file mode 0600 is the real control against an
// attacker with read access as this user.
type FileStore struct {
	Dir string
}

func (f *FileStore) Name() string { return "encrypted file (" + f.Dir + ")" }

func (f *FileStore) path(profile string) string {
	return filepath.Join(f.Dir, profile+".key")
}

// Sensitive to the hostname on purpose: a rename makes stored keys
// undecryptable, which Get() below turns into an actionable message.
func (f *FileStore) derive() []byte {
	host, _ := os.Hostname()
	sum := sha256.Sum256([]byte("flexprice-cli|" + host + "|" + f.Dir))
	return sum[:]
}

func (f *FileStore) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(f.derive())
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func (f *FileStore) Set(profile, key string) error {
	gcm, err := f.aead()
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(key), nil)

	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return fmt.Errorf("create key directory: %w", err)
	}
	if err := os.Chmod(f.Dir, 0o700); err != nil {
		return fmt.Errorf("secure key directory: %w", err)
	}

	enc := []byte(base64.StdEncoding.EncodeToString(sealed))
	return f.writeAtomic(f.path(profile), enc)
}

// Temp file plus rename, which is atomic on the same filesystem. Two processes
// opening the destination with O_TRUNC directly could leave a short write with
// a leftover tail from the longer one.
func (f *FileStore) writeAtomic(dest string, data []byte) error {
	tmp, err := os.CreateTemp(f.Dir, filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp key file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write key file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("secure key file: %w", err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}
	return nil
}

func (f *FileStore) Get(profile string) (string, error) {
	raw, err := os.ReadFile(f.path(profile))
	if err != nil {
		return "", fmt.Errorf("no stored key for profile %q: %w", profile, err)
	}
	sealed, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return "", fmt.Errorf("stored key for %q is corrupt: %w", profile, err)
	}
	gcm, err := f.aead()
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("stored key for %q is truncated; run `flexprice login` again", profile)
	}
	nonce, body := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf(
			"cannot decrypt stored key for %q: it was likely saved on a different host, "+
				"or this host's name has since changed; run `flexprice login` again to fix it: %w",
			profile, err)
	}
	return string(plain), nil
}

func (f *FileStore) Delete(profile string) error {
	if err := os.Remove(f.path(profile)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove key file: %w", err)
	}
	return nil
}
