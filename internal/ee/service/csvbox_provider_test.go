package service

import (
	"context"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func csvboxTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)
	return log
}

func TestNewCSVBoxProvider_DisabledReturnsNil(t *testing.T) {
	log := csvboxTestLogger(t)
	require.Nil(t, NewCSVBoxProvider(config.FlexpriceS3ImportsConfig{Enabled: false}, log))
}

func TestNewCSVBoxProvider_MissingCredsReturnsNil(t *testing.T) {
	log := csvboxTestLogger(t)
	require.Nil(t, NewCSVBoxProvider(config.FlexpriceS3ImportsConfig{
		Enabled: true,
		Bucket:  "b",
	}, log))
}

// GetDownloadURL must produce a presigned https URL against the configured
// bucket/key. Static credentials are enough for presigning — no network call.
func TestCSVBoxProvider_PresignsConfiguredBucket(t *testing.T) {
	log := csvboxTestLogger(t)
	p := NewCSVBoxProvider(config.FlexpriceS3ImportsConfig{
		Enabled:            true,
		Bucket:             "flexprice-imports-test",
		Region:             "us-east-1",
		AWSAccessKeyID:     "AKIATESTTESTTESTTEST",
		AWSSecretAccessKey: "secretsecretsecretsecretsecretsecret",
	}, log)
	require.NotNil(t, p)

	url, err := p.GetDownloadURL(context.Background(), "s3://flexprice-imports-test/csvbox/abc123.csv")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(url, "https://"), "presigned URL should be https, got %q", url)
	require.Contains(t, url, "flexprice-imports-test")
	require.Contains(t, url, "csvbox/abc123.csv")
	require.Contains(t, url, "X-Amz-Signature")
}

// The provider is the trust boundary: it must refuse to sign a URL whose
// bucket does not match its configured bucket. Otherwise an attacker who
// could stuff a task record's FileURL could get imports creds presigned
// against a bucket they control.
func TestCSVBoxProvider_RejectsForeignBucket(t *testing.T) {
	log := csvboxTestLogger(t)
	p := NewCSVBoxProvider(config.FlexpriceS3ImportsConfig{
		Enabled:            true,
		Bucket:             "flexprice-imports-test",
		Region:             "us-east-1",
		AWSAccessKeyID:     "AKIATESTTESTTESTTEST",
		AWSSecretAccessKey: "secretsecretsecretsecretsecretsecret",
	}, log)
	require.NotNil(t, p)

	_, err := p.GetDownloadURL(context.Background(), "s3://not-our-bucket/csvbox/abc123.csv")
	require.Error(t, err)
}

func TestCSVBoxProvider_RejectsNonS3URL(t *testing.T) {
	log := csvboxTestLogger(t)
	p := NewCSVBoxProvider(config.FlexpriceS3ImportsConfig{
		Enabled:            true,
		Bucket:             "flexprice-imports-test",
		Region:             "us-east-1",
		AWSAccessKeyID:     "AKIATESTTESTTESTTEST",
		AWSSecretAccessKey: "secretsecretsecretsecretsecretsecret",
	}, log)
	_, err := p.GetDownloadURL(context.Background(), "https://example.com/foo.csv")
	require.Error(t, err)
}

// The registry routes s3:// URLs to CSVBox only when the bucket matches the
// configured imports bucket. An unrelated s3:// URL (e.g. an export FileURL)
// must fall through to the default s3 provider so it doesn't accidentally get
// signed with imports credentials.
func TestFileProviderRegistry_RoutesOnlyMatchingBucketToCSVBox(t *testing.T) {
	log := csvboxTestLogger(t)
	reg := NewFileProviderRegistry()
	csvbox := NewCSVBoxProvider(config.FlexpriceS3ImportsConfig{
		Enabled:            true,
		Bucket:             "flexprice-imports-test",
		Region:             "us-east-1",
		AWSAccessKeyID:     "AKIATESTTESTTESTTEST",
		AWSSecretAccessKey: "secretsecretsecretsecretsecretsecret",
	}, log)
	reg.RegisterCSVBoxProvider(csvbox)

	require.Equal(t, FileProviderTypeCSVBox,
		reg.GetProvider("s3://flexprice-imports-test/csvbox/abc.csv").GetProviderName())
	require.NotEqual(t, FileProviderTypeCSVBox,
		reg.GetProvider("s3://some-other-bucket/exports/foo.csv").GetProviderName())
	require.Equal(t, FileProviderTypeDirect,
		reg.GetProvider("https://example.com/foo.csv").GetProviderName())
}
