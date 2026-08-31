package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	"github.com/stretchr/testify/require"
)

// fakeStorage is a minimal storagetypes.Storage that returns a stub URL from
// PresignGet. Tests only ever exercise PresignGet, so the other methods are
// stubbed to satisfy the interface.
type fakeStorage struct{ presignErr error }

func (f *fakeStorage) Upload(context.Context, *storagetypes.UploadRequest) (*storagetypes.UploadResponse, error) {
	return nil, nil
}
func (f *fakeStorage) Download(context.Context, string) ([]byte, error) { return nil, nil }
func (f *fakeStorage) Exists(context.Context, string) (bool, error)     { return false, nil }
func (f *fakeStorage) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	if f.presignErr != nil {
		return "", f.presignErr
	}
	return "https://presigned.example/" + key, nil
}
func (f *fakeStorage) ValidateConnection(context.Context) error { return nil }
func (f *fakeStorage) FileURL(key string) string                { return "s3://fake/" + key }
func (f *fakeStorage) Provider() storagetypes.Provider          { return storagetypes.ProviderS3 }

// fakeResolver is a minimal storage.Resolver for provider tests. Only
// PurposeImport is exercised; other purposes return an error to catch any
// accidental broadening of the trust boundary.
type fakeResolver struct {
	bucket    string
	store     storagetypes.Storage
	forErr    error
	bucketErr error
}

func (r *fakeResolver) ForPlatform(_ context.Context, purpose storage.Purpose) (storagetypes.Storage, error) {
	if purpose != storage.PurposeImport {
		return nil, errUnexpectedPurpose(purpose)
	}
	if r.forErr != nil {
		return nil, r.forErr
	}
	return r.store, nil
}
func (r *fakeResolver) ForConnection(context.Context, string) (storagetypes.Storage, error) {
	return nil, nil
}
func (r *fakeResolver) Provider() storage.Provider { return storage.ProviderS3 }
func (r *fakeResolver) BucketConfigFor(purpose storage.Purpose) (config.BucketConfig, error) {
	if purpose != storage.PurposeImport {
		return config.BucketConfig{}, errUnexpectedPurpose(purpose)
	}
	if r.bucketErr != nil {
		return config.BucketConfig{}, r.bucketErr
	}
	return config.BucketConfig{Bucket: r.bucket, PresignExpiryDuration: "5m"}, nil
}

type unexpectedPurposeErr struct{ purpose storage.Purpose }

func (e *unexpectedPurposeErr) Error() string { return "unexpected purpose: " + string(e.purpose) }

func errUnexpectedPurpose(p storage.Purpose) error { return &unexpectedPurposeErr{purpose: p} }

func TestNewCSVBoxProvider_NilResolverReturnsNil(t *testing.T) {
	require.Nil(t, NewCSVBoxProvider(nil))
}

// GetDownloadURL routes through storage.Resolver → Storage.PresignGet. Test
// exercises the whole chain without touching AWS.
func TestCSVBoxProvider_PresignsConfiguredBucket(t *testing.T) {
	r := &fakeResolver{bucket: "flexprice-imports-test", store: &fakeStorage{}}
	p := NewCSVBoxProvider(r)
	require.NotNil(t, p)

	url, err := p.GetDownloadURL(context.Background(), "s3://flexprice-imports-test/csvbox/abc123.csv")
	require.NoError(t, err)
	require.Equal(t, "https://presigned.example/csvbox/abc123.csv", url)
}

// The provider is the trust boundary: it must refuse to sign a URL whose
// bucket does not match the resolver's imports bucket. Without this check,
// a task row with a foreign FileURL could get imports creds pointed at
// someone else's bucket.
func TestCSVBoxProvider_RejectsForeignBucket(t *testing.T) {
	r := &fakeResolver{bucket: "flexprice-imports-test", store: &fakeStorage{}}
	p := NewCSVBoxProvider(r)
	require.NotNil(t, p)

	_, err := p.GetDownloadURL(context.Background(), "s3://not-our-bucket/csvbox/abc123.csv")
	require.Error(t, err)
}

func TestCSVBoxProvider_RejectsNonS3URL(t *testing.T) {
	r := &fakeResolver{bucket: "flexprice-imports-test", store: &fakeStorage{}}
	p := NewCSVBoxProvider(r)
	_, err := p.GetDownloadURL(context.Background(), "https://example.com/foo.csv")
	require.Error(t, err)
}

// The registry routes s3:// URLs to CSVBox only when the bucket matches the
// resolver's imports bucket. An unrelated s3:// URL (e.g. an export FileURL)
// must fall through to the default s3 provider so it doesn't accidentally get
// signed with imports credentials.
func TestFileProviderRegistry_RoutesOnlyMatchingBucketToCSVBox(t *testing.T) {
	reg := NewFileProviderRegistry()
	reg.RegisterCSVBoxProvider(NewCSVBoxProvider(&fakeResolver{
		bucket: "flexprice-imports-test", store: &fakeStorage{},
	}))

	require.Equal(t, FileProviderTypeCSVBox,
		reg.GetProvider("s3://flexprice-imports-test/csvbox/abc.csv").GetProviderName())
	require.NotEqual(t, FileProviderTypeCSVBox,
		reg.GetProvider("s3://some-other-bucket/exports/foo.csv").GetProviderName())
	require.Equal(t, FileProviderTypeDirect,
		reg.GetProvider("https://example.com/foo.csv").GetProviderName())
}

// A resolver whose imports bucket is empty (imports disabled) must not
// register the CSVBox provider — otherwise s3:// URL routing would silently
// swallow requests instead of falling through.
func TestFileProviderRegistry_ImportsDisabledSkipsRegistration(t *testing.T) {
	reg := NewFileProviderRegistry()
	reg.RegisterCSVBoxProvider(NewCSVBoxProvider(&fakeResolver{bucket: "", store: &fakeStorage{}}))
	require.NotEqual(t, FileProviderTypeCSVBox,
		reg.GetProvider("s3://flexprice-imports-test/csvbox/abc.csv").GetProviderName())
}
