package s3backend_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/s3backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_WithStaticCredentials_ReturnsStorage(t *testing.T) {
	cfg := &s3backend.Config{
		Bucket:             "test-bucket",
		Region:             "ap-south-1",
		AWSAccessKeyID:     "AKIAEXAMPLE",
		AWSSecretAccessKey: "secretexample",
	}

	s, err := s3backend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)

	var _ storage.Storage = s
	assert.Equal(t, storage.ProviderS3, s.Provider())
}

func TestNew_NoCredentialsConfigured_FallsBackToAmbientChain(t *testing.T) {
	cfg := &s3backend.Config{
		Bucket: "test-bucket",
		Region: "ap-south-1",
	}

	// Ambient chain resolution is lazy (SDK resolves creds on first call, not
	// at construction), so New() must still succeed here — no credentials
	// error until an actual API call is made.
	s, err := s3backend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
}

// newFakeS3Server starts an httptest.Server that fakes just enough of the S3
// REST API for Upload/Exists/Download to round-trip against, without any real
// AWS network access. handler receives the raw HTTP request/writer so each
// test can assert on what the SDK actually sent (method, path, headers) and
// script the response.
func newFakeS3Server(t *testing.T, handler http.HandlerFunc) (*s3backend.Config, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)

	cfg := &s3backend.Config{
		Bucket:             "test-bucket",
		Region:             "us-east-1",
		EndpointURL:        srv.URL,
		UsePathStyle:       true,
		AWSAccessKeyID:     "AKIAEXAMPLE",
		AWSSecretAccessKey: "secretexample",
	}
	return cfg, srv.Close
}

func TestClient_Upload_SetsContentTypeAndKey(t *testing.T) {
	tests := []struct {
		name            string
		req             *storage.UploadRequest
		wantContentType string
		wantKeyInPath   string
	}{
		{
			name: "csv format infers text/csv content type",
			req: &storage.UploadRequest{
				Key:    "exports/report.csv",
				Data:   []byte("a,b,c\n1,2,3"),
				Format: storage.UploadFormatCSV,
			},
			wantContentType: "text/csv",
			wantKeyInPath:   "/test-bucket/exports/report.csv",
		},
		{
			name: "json format infers application/json content type",
			req: &storage.UploadRequest{
				Key:    "exports/data.json",
				Data:   []byte(`{"a":1}`),
				Format: storage.UploadFormatJSON,
			},
			wantContentType: "application/json",
			wantKeyInPath:   "/test-bucket/exports/data.json",
		},
		{
			name: "explicit content type overrides format inference",
			req: &storage.UploadRequest{
				Key:         "exports/custom.bin",
				Data:        []byte("raw"),
				Format:      storage.UploadFormatCSV,
				ContentType: "application/octet-stream",
			},
			wantContentType: "application/octet-stream",
			wantKeyInPath:   "/test-bucket/exports/custom.bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotContentType, gotMethod string
			cfg, closeSrv := newFakeS3Server(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotContentType = r.Header.Get("Content-Type")
				gotMethod = r.Method
				w.WriteHeader(http.StatusOK)
			})
			defer closeSrv()

			s, err := s3backend.New(cfg, logger.NewNoopLogger())
			require.NoError(t, err)

			resp, err := s.Upload(context.Background(), tt.req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			assert.Equal(t, http.MethodPut, gotMethod)
			assert.Equal(t, tt.wantKeyInPath, gotPath)
			assert.Equal(t, tt.wantContentType, gotContentType)
			assert.Equal(t, tt.req.Key, resp.Key)
			assert.Equal(t, "test-bucket", resp.Bucket)
		})
	}
}

func TestClient_Exists_ReturnsFalseForMissingKey(t *testing.T) {
	cfg, closeSrv := newFakeS3Server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer closeSrv()

	s, err := s3backend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	exists, err := s.Exists(context.Background(), "missing/key.csv")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestClient_Exists_ReturnsTrueForFoundKey(t *testing.T) {
	cfg, closeSrv := newFakeS3Server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer closeSrv()

	s, err := s3backend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	exists, err := s.Exists(context.Background(), "found/key.csv")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestClient_FileURL_MatchesProviderScheme(t *testing.T) {
	cfg := &s3backend.Config{
		Bucket:             "test-bucket",
		Region:             "ap-south-1",
		AWSAccessKeyID:     "AKIAEXAMPLE",
		AWSSecretAccessKey: "secretexample",
	}
	s, err := s3backend.New(cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	got := s.FileURL("exports/report.csv")
	want := storage.FileURL(storage.ProviderS3, "test-bucket", "exports/report.csv")
	assert.Equal(t, want, got)
}
