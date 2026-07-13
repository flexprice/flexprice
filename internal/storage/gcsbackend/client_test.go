package gcsbackend_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/gcsbackend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_ReturnsStorage(t *testing.T) {
	cfg := &gcsbackend.Config{
		Bucket: "test-bucket",
	}

	s, err := gcsbackend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)

	var _ storage.Storage = s
	assert.Equal(t, storage.ProviderGCS, s.Provider())
	assert.Equal(t, "gs://test-bucket/a/b.pdf", s.FileURL("a/b.pdf"))
}

func TestClient_FileURL_MatchesProviderScheme(t *testing.T) {
	cfg := &gcsbackend.Config{Bucket: "test-bucket"}
	s, err := gcsbackend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	got := s.FileURL("exports/report.csv")
	want := storage.FileURL(storage.ProviderGCS, "test-bucket", "exports/report.csv")
	assert.Equal(t, want, got)
}

// newFakeGCSServer starts an httptest.Server that fakes just enough of the
// GCS JSON/XML API for Upload/Exists/Download to round-trip against, without
// any real GCP network access. The GCS client library supports pointing at a
// custom endpoint via option.WithEndpoint + option.WithoutAuthentication,
// mirroring how s3backend's tests point the AWS SDK at an httptest.Server via
// Config.EndpointURL.
func newFakeGCSServer(t *testing.T, handler http.HandlerFunc) (*gcsbackend.Config, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)

	cfg := &gcsbackend.Config{
		Bucket:      "test-bucket",
		EndpointURL: srv.URL,
	}
	return cfg, srv
}

func TestClient_Upload_RoundTrip(t *testing.T) {
	var gotMethod, gotPath string
	cfg, srv := newFakeGCSServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"exports/report.csv","bucket":"test-bucket"}`))
	})
	defer srv.Close()

	s, err := gcsbackend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	resp, err := s.Upload(context.Background(), &storage.UploadRequest{
		Key:    "exports/report.csv",
		Data:   []byte("a,b,c\n1,2,3"),
		Format: storage.UploadFormatCSV,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotEmpty(t, gotMethod)
	assert.Contains(t, gotPath, "test-bucket")
	assert.Equal(t, "exports/report.csv", resp.Key)
	assert.Equal(t, "test-bucket", resp.Bucket)
}

func TestClient_Exists_ReturnsFalseForMissingKey(t *testing.T) {
	cfg, srv := newFakeGCSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer srv.Close()

	s, err := gcsbackend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	exists, err := s.Exists(context.Background(), "missing/key.csv")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestClient_Exists_ReturnsTrueForFoundKey(t *testing.T) {
	cfg, srv := newFakeGCSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"found/key.csv","bucket":"test-bucket"}`))
	})
	defer srv.Close()

	s, err := gcsbackend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	exists, err := s.Exists(context.Background(), "found/key.csv")
	require.NoError(t, err)
	assert.True(t, exists)
}
