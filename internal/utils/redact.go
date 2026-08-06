package utils

import (
	"sort"
	"strings"
)

// redactedPlaceholder is what a removed secret is replaced with, so a redacted message still shows
// that something was removed rather than silently reading as if the field were absent.
const redactedPlaceholder = "[redacted]"

// RedactSecrets redacts each non-empty secret out of msg, leaving everything else intact.
//
// Use it on any third-party error message before it reaches a log or an API response: the message
// stays useful for debugging (status code, request id, which action was denied, why), while the
// tenant's own credentials or resource identifiers embedded in it are removed.
//
// Secrets are redacted longest-first: a shorter secret that is also a substring of a longer one
// would otherwise get replaced first and leave the longer secret's remainder exposed.
func RedactSecrets(msg string, secrets ...string) string {
	sort.SliceStable(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, redactedPlaceholder)
	}
	return msg
}
