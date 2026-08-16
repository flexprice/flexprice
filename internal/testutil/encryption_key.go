package testutil

import (
	"crypto/rand"
	"encoding/hex"
)

// NewEncryptionKey returns a hex-encoded key drawn from 32 random bytes, for
// tests that need to build a security.EncryptionService.
//
// Generated rather than written as a literal so no test file carries a
// credential-shaped constant: secret scanners flag those on every pull request,
// and a real key that reaches a test file is indistinguishable from a fake one.
// The value only has to round-trip within a single test run, so a fresh key per
// call is sufficient.
func NewEncryptionKey() string {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("testutil: could not generate encryption key: " + err.Error())
	}
	return hex.EncodeToString(key)
}
