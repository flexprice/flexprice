package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStorage is an in-memory Storage used to verify the interface contract
// itself is usable by callers — real backend tests live in s3backend/gcsbackend.
type fakeStorage struct {
	objects  map[string][]byte
	provider storage.Provider
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{objects: map[string][]byte{}, provider: storage.ProviderS3}
}

func (f *fakeStorage) Upload(ctx context.Context, req *storage.UploadRequest) (*storage.UploadResponse, error) {
	f.objects[req.Key] = req.Data
	return &storage.UploadResponse{
		FileURL:       f.FileURL(req.Key),
		Key:           req.Key,
		FileSizeBytes: int64(len(req.Data)),
		UploadedAt:    time.Now(),
	}, nil
}

func (f *fakeStorage) Download(ctx context.Context, key string) ([]byte, error) {
	data, ok := f.objects[key]
	if !ok {
		return nil, assert.AnError
	}
	return data, nil
}

func (f *fakeStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := f.objects[key]
	return ok, nil
}

func (f *fakeStorage) PresignGet(ctx context.Context, key string, d time.Duration) (string, error) {
	return "https://example.com/" + key, nil
}

func (f *fakeStorage) ValidateConnection(ctx context.Context) error { return nil }

func (f *fakeStorage) FileURL(key string) string { return "s3://fake-bucket/" + key }

func (f *fakeStorage) Provider() storage.Provider { return f.provider }

func TestStorageInterface_UploadDownloadRoundTrip(t *testing.T) {
	var s storage.Storage = newFakeStorage()

	resp, err := s.Upload(context.Background(), &storage.UploadRequest{
		Key:    "invoices/inv_123.pdf",
		Data:   []byte("pdf-bytes"),
		Format: storage.UploadFormatPDF,
	})
	require.NoError(t, err)
	assert.Equal(t, "invoices/inv_123.pdf", resp.Key)
	assert.Equal(t, int64(len("pdf-bytes")), resp.FileSizeBytes)

	data, err := s.Download(context.Background(), "invoices/inv_123.pdf")
	require.NoError(t, err)
	assert.Equal(t, []byte("pdf-bytes"), data)

	exists, err := s.Exists(context.Background(), "invoices/inv_123.pdf")
	require.NoError(t, err)
	assert.True(t, exists)

	missing, err := s.Exists(context.Background(), "does/not/exist.pdf")
	require.NoError(t, err)
	assert.False(t, missing)
}
