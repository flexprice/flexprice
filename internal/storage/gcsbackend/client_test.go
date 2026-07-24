package gcsbackend_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/gcsbackend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew_ReturnsStorage proves construction succeeds without explicit
// ServiceAccountJSON, mirroring the production path where a real GKE pod
// relies on ambient Workload Identity / Application Default Credentials.
// It must NOT actually reach real ADC resolution — real cloud.google.com/go
// storage.NewClient() resolves ADC eagerly at construction time (unlike the
// AWS SDK, which resolves credentials lazily on first API call), so on a
// machine/CI runner with no ADC configured this would fail with "could not
// find default credentials". Pointing at a fake endpoint via EndpointURL
// (same technique newFakeGCSServer/TestClient_Upload_RoundTrip use) makes
// gcsbackend.New use option.WithoutAuthentication() instead, so this test's
// result never depends on whatever credentials happen to be ambient on the
// machine running it.
func TestNew_ReturnsStorage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &gcsbackend.Config{
		Bucket:      "test-bucket",
		EndpointURL: srv.URL,
	}

	s, err := gcsbackend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)

	var _ storage.Storage = s
	assert.Equal(t, storage.ProviderGCS, s.Provider())
	assert.Equal(t, "gs://test-bucket/a/b.pdf", s.FileURL("a/b.pdf"))
}

// TestClient_FileURL_MatchesProviderScheme: see TestNew_ReturnsStorage doc
// comment for why EndpointURL is required here to avoid real ADC resolution.
func TestClient_FileURL_MatchesProviderScheme(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &gcsbackend.Config{Bucket: "test-bucket", EndpointURL: srv.URL}
	s, err := gcsbackend.New(context.Background(), cfg, logger.NewNoopLogger())
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

	s, err := gcsbackend.New(context.Background(), cfg, logger.NewNoopLogger())
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

// TestClient_Upload_SetsContentTypeByFormat proves contentTypeFor's
// format-to-content-type mapping (including the Parquet case) actually
// reaches the wire: it inspects the JSON metadata part of the multipart
// upload body the GCS client sends, which carries contentType for GCS
// (unlike S3, which sends it as a Content-Type HTTP header).
func TestClient_Upload_SetsContentTypeByFormat(t *testing.T) {
	tests := []struct {
		name            string
		req             *storage.UploadRequest
		wantContentType string
	}{
		{
			name: "csv format infers text/csv content type",
			req: &storage.UploadRequest{
				Key:    "exports/report.csv",
				Data:   []byte("a,b,c\n1,2,3"),
				Format: storage.UploadFormatCSV,
			},
			wantContentType: "text/csv",
		},
		{
			name: "parquet format infers application/vnd.apache.parquet content type",
			req: &storage.UploadRequest{
				Key:    "exports/report.parquet",
				Data:   []byte("fake-parquet-bytes"),
				Format: storage.UploadFormatParquet,
			},
			wantContentType: "application/vnd.apache.parquet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotContentType string
			var extractErr error
			cfg, srv := newFakeGCSServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotContentType, extractErr = extractUploadedContentType(r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"name":"` + tt.req.Key + `","bucket":"test-bucket"}`))
			})
			defer srv.Close()

			s, err := gcsbackend.New(context.Background(), cfg, logger.NewNoopLogger())
			require.NoError(t, err)

			resp, err := s.Upload(context.Background(), tt.req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			require.NoError(t, extractErr, "failed to extract uploaded content type from request")
			assert.Equal(t, tt.wantContentType, gotContentType)
		})
	}
}

// extractUploadedContentType parses the multipart/related body the GCS JSON
// API client sends and returns the "contentType" field of the JSON metadata
// part (the first part).
//
// This runs inside the httptest.Server's request-handling goroutine, not the
// test goroutine, so it returns an error instead of calling require.*
// directly — t.FailNow() (which require.* calls internally) must be invoked
// from the goroutine running the test, per the testing package's docs.
// Calling it from the server goroutine would stop only that goroutine while
// the test function continued as if nothing had failed, silently passing.
// Callers must check the returned error with require.NoError in the actual
// test goroutine.
func extractUploadedContentType(r *http.Request) (string, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return "", fmt.Errorf("parsing Content-Type header: %w", err)
	}
	if !strings.Contains(mediaType, "multipart/") {
		return "", fmt.Errorf("expected multipart/* content type, got %q", mediaType)
	}

	mr := multipart.NewReader(r.Body, params["boundary"])

	metadataPart, err := mr.NextPart()
	if err != nil {
		return "", fmt.Errorf("reading metadata part: %w", err)
	}
	metadataBytes, err := io.ReadAll(metadataPart)
	if err != nil {
		return "", fmt.Errorf("reading metadata part body: %w", err)
	}

	var metadata struct {
		ContentType string `json:"contentType"`
	}
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return "", fmt.Errorf("unmarshaling metadata JSON: %w", err)
	}
	return metadata.ContentType, nil
}

// extractUploadedObjectBytes parses the multipart/related body the GCS JSON
// API client sends for a "simple"/multipart upload (as used by small
// payloads in these tests) and returns the raw bytes of the object's data
// part (the second part, after the JSON metadata part).
//
// See extractUploadedContentType's doc comment for why this returns an error
// instead of calling require.* directly: it runs inside the httptest.Server's
// request-handling goroutine, not the test goroutine.
func extractUploadedObjectBytes(r *http.Request) ([]byte, error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("parsing Content-Type header: %w", err)
	}
	if !strings.Contains(mediaType, "multipart/") {
		return nil, fmt.Errorf("expected multipart/* content type, got %q", mediaType)
	}

	mr := multipart.NewReader(r.Body, params["boundary"])

	// First part: JSON metadata (bucket/name/contentType/etc).
	if _, err = mr.NextPart(); err != nil {
		return nil, fmt.Errorf("reading metadata part: %w", err)
	}

	// Second part: the actual object data.
	dataPart, err := mr.NextPart()
	if err != nil {
		return nil, fmt.Errorf("reading data part: %w", err)
	}
	data, err := io.ReadAll(dataPart)
	if err != nil {
		return nil, fmt.Errorf("reading data part body: %w", err)
	}
	return data, nil
}

func TestClient_Upload_CompressesWhenRequestedAndConfigured(t *testing.T) {
	original := []byte("a,b,c\n1,2,3\n4,5,6\n")
	var gotBody []byte
	var extractErr error

	cfg, srv := newFakeGCSServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, extractErr = extractUploadedObjectBytes(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"exports/report.csv.gz","bucket":"test-bucket"}`))
	})
	defer srv.Close()
	cfg.CompressionGzip = true

	s, err := gcsbackend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	resp, err := s.Upload(context.Background(), &storage.UploadRequest{
		Key:      "exports/report.csv.gz",
		Data:     original,
		Format:   storage.UploadFormatCSV,
		Compress: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.NoError(t, extractErr, "failed to extract uploaded object bytes from request")
	require.NotEmpty(t, gotBody, "expected the fake GCS server to receive object bytes")

	// The bytes actually sent over the wire must be genuinely gzip-compressed
	// (not the raw original bytes with a misleading .gz key name).
	gzReader, err := gzip.NewReader(bytes.NewReader(gotBody))
	require.NoError(t, err, "uploaded bytes should be valid gzip data")
	decompressed, err := io.ReadAll(gzReader)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed, "decompressed uploaded bytes should match the original input")

	// The wire bytes should differ from the raw input (i.e. compression
	// actually happened, this isn't just a no-op pass-through).
	assert.NotEqual(t, original, gotBody)
}

func TestClient_Upload_DoesNotCompressWhenGzipNotConfigured(t *testing.T) {
	original := []byte("a,b,c\n1,2,3\n4,5,6\n")
	var gotBody []byte
	var extractErr error

	cfg, srv := newFakeGCSServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, extractErr = extractUploadedObjectBytes(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"exports/report.csv","bucket":"test-bucket"}`))
	})
	defer srv.Close()
	// cfg.CompressionGzip left false (default/unset) — compression must be
	// explicitly enabled per-connection, so Compress:true alone must not
	// trigger gzip.

	s, err := gcsbackend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	resp, err := s.Upload(context.Background(), &storage.UploadRequest{
		Key:      "exports/report.csv",
		Data:     original,
		Format:   storage.UploadFormatCSV,
		Compress: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	require.NoError(t, extractErr, "failed to extract uploaded object bytes from request")
	require.NotEmpty(t, gotBody)
	assert.Equal(t, original, gotBody, "bytes should be uploaded uncompressed when CompressionGzip is not configured")

	// Confirm it's NOT valid gzip data.
	_, err = gzip.NewReader(bytes.NewReader(gotBody))
	assert.Error(t, err, "uploaded bytes should not be gzip-compressed")
}

func TestClient_Exists_ReturnsFalseForMissingKey(t *testing.T) {
	cfg, srv := newFakeGCSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer srv.Close()

	s, err := gcsbackend.New(context.Background(), cfg, logger.NewNoopLogger())
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

	s, err := gcsbackend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	exists, err := s.Exists(context.Background(), "found/key.csv")
	require.NoError(t, err)
	assert.True(t, exists)
}
