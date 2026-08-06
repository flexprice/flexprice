package storage

import (
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
)

// FileURL formats a bucket+key pair as a scheme-prefixed URL for the given
// provider. The implementation lives in storagetypes so backend packages
// (s3backend, gcsbackend) can call it without importing this package — see
// storagetypes.FileURL doc comment for why.
func FileURL(provider Provider, bucket, key string) string {
	return storagetypes.FileURL(provider, bucket, key)
}

// ParseFileURL parses a "s3://bucket/key" or "gs://bucket/key" URL into its parts.
func ParseFileURL(fileURL string) (provider Provider, bucket string, key string, err error) {
	var scheme, rest string
	switch {
	case strings.HasPrefix(fileURL, "s3://"):
		scheme, rest = "s3", strings.TrimPrefix(fileURL, "s3://")
	case strings.HasPrefix(fileURL, "gs://"):
		scheme, rest = "gs", strings.TrimPrefix(fileURL, "gs://")
	default:
		return "", "", "", ierr.NewErrorf("unrecognized storage URL scheme: %s", fileURL).
			WithHint("File URL must start with s3:// or gs://").
			Mark(ierr.ErrValidation)
	}

	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", ierr.NewErrorf("invalid storage URL format: %s", fileURL).
			WithHintf("File URL must be in format %s://bucket/key", scheme).
			Mark(ierr.ErrValidation)
	}

	if scheme == "gs" {
		return ProviderGCS, parts[0], parts[1], nil
	}
	return ProviderS3, parts[0], parts[1], nil
}
