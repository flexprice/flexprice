package storage_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileURL(t *testing.T) {
	assert.Equal(t, "s3://my-bucket/a/b.csv", storage.FileURL(storage.ProviderS3, "my-bucket", "a/b.csv"))
	assert.Equal(t, "gs://my-bucket/a/b.csv", storage.FileURL(storage.ProviderGCS, "my-bucket", "a/b.csv"))
}

func TestParseFileURL(t *testing.T) {
	provider, bucket, key, err := storage.ParseFileURL("s3://my-bucket/a/b.csv")
	require.NoError(t, err)
	assert.Equal(t, storage.ProviderS3, provider)
	assert.Equal(t, "my-bucket", bucket)
	assert.Equal(t, "a/b.csv", key)

	provider, bucket, key, err = storage.ParseFileURL("gs://other-bucket/x/y.pdf")
	require.NoError(t, err)
	assert.Equal(t, storage.ProviderGCS, provider)
	assert.Equal(t, "other-bucket", bucket)
	assert.Equal(t, "x/y.pdf", key)

	_, _, _, err = storage.ParseFileURL("https://example.com/not-a-storage-url")
	assert.Error(t, err)

	_, _, _, err = storage.ParseFileURL("s3://bucket-only-no-key")
	assert.Error(t, err)
}
