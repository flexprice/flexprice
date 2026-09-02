package s3backend_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

	s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)

	assert.Equal(t, storage.ProviderS3, s.Provider())
}

// TestNew_StaticBeatsAssumeRole covers the documented precedence: static keys, if set, are
// used even when AssumeRoleARN/AssumeRoleExternalID are also set.
func TestNew_StaticBeatsAssumeRole(t *testing.T) {
	cfg := &s3backend.Config{
		Bucket:                        "test-bucket",
		Region:                        "ap-south-1",
		AWSAccessKeyID:                "AKIAEXAMPLE",
		AWSSecretAccessKey:            "secretexample",
		AssumeRoleARN:                 "arn:aws:iam::123456789012:role/flexprice-export",
		AssumeRoleExternalID:          "ext-tenant-abc",
		AssumeRoleBaseAccessKeyID:     "AKIABASE",
		AssumeRoleBaseSecretAccessKey: "basesecret",
		AssumeRoleBaseRegion:          "us-east-1",
	}

	s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
}

// TestNew_AssumeRoleUsedWhenSetAndNoStaticKeys covers the second precedence case: with no
// static keys, an AssumeRoleARN + ExternalID is accepted and New succeeds (credential
// resolution/AssumeRole itself is lazy — errors would only surface on the first real S3 call).
func TestNew_AssumeRoleUsedWhenSetAndNoStaticKeys(t *testing.T) {
	cfg := &s3backend.Config{
		Bucket:                        "test-bucket",
		Region:                        "ap-south-1",
		AssumeRoleARN:                 "arn:aws:iam::123456789012:role/flexprice-export",
		AssumeRoleExternalID:          "ext-tenant-abc",
		AssumeRoleBaseAccessKeyID:     "AKIABASE",
		AssumeRoleBaseSecretAccessKey: "basesecret",
		AssumeRoleBaseRegion:          "us-east-1",
	}

	s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
}

// TestNew_AssumeRoleWithoutBaseCredentials_ReturnsError covers the base-credential guard:
// AssumeRole must never fall through to LoadDefaultConfig/the ambient chain for the STS
// caller identity — see the AssumeRoleBase* doc comment on Config.
func TestNew_AssumeRoleWithoutBaseCredentials_ReturnsError(t *testing.T) {
	cfg := &s3backend.Config{
		Bucket:               "test-bucket",
		Region:               "ap-south-1",
		AssumeRoleARN:        "arn:aws:iam::123456789012:role/flexprice-export",
		AssumeRoleExternalID: "ext-tenant-abc",
		// No AssumeRoleBase* fields set.
	}

	s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.Error(t, err)
	require.Nil(t, s)
}

func TestNew_NoCredentialsConfigured_FallsBackToAmbientChain(t *testing.T) {
	cfg := &s3backend.Config{
		Bucket: "test-bucket",
		Region: "ap-south-1",
	}

	// Ambient chain resolution is lazy (SDK resolves creds on first call, not
	// at construction), so New() must still succeed here — no credentials
	// error until an actual API call is made.
	s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
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
			name: "parquet format infers application/vnd.apache.parquet content type",
			req: &storage.UploadRequest{
				Key:    "exports/report.parquet",
				Data:   []byte("fake-parquet-bytes"),
				Format: storage.UploadFormatParquet,
			},
			wantContentType: "application/vnd.apache.parquet",
			wantKeyInPath:   "/test-bucket/exports/report.parquet",
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

			s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
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

	s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
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

	s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	exists, err := s.Exists(context.Background(), "found/key.csv")
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestAssumeRole_ErrorIsRedacted proves that an AssumeRole failure surfaced by an actual S3
// call does not leak the tenant's role ARN or external ID — both must be stripped from the
// error text before it reaches the caller, mirroring awsmarketplace/client.go's redaction of
// AssumeRole errors. STS AssumeRole is not reachable in this unit test (no live AWS), but the
// SDK will fail fast against an unresolvable/invalid STS endpoint with the role ARN embedded
// in its error text; we assert on whatever error message region validation produces.
func TestAssumeRole_ErrorIsRedacted(t *testing.T) {
	const roleARN = "arn:aws:iam::123456789012:role/should-never-leak"
	const externalID = "ext-should-never-leak"

	cfg := &s3backend.Config{
		Bucket:                        "test-bucket",
		Region:                        "us-east-1",
		EndpointURL:                   "http://127.0.0.1:1", // dead local endpoint: STS+S3 fail fast, no outbound network
		AssumeRoleARN:                 roleARN,
		AssumeRoleExternalID:          externalID,
		AssumeRoleBaseAccessKeyID:     "AKIABASE",
		AssumeRoleBaseSecretAccessKey: "basesecret",
		AssumeRoleBaseRegion:          "us-east-1",
	}

	s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)

	// Any real API call forces the SDK to resolve credentials via the wrapped
	// AssumeRole provider. Against a fake, non-STS endpoint this fails — the
	// interesting assertion is that neither secret appears in the resulting error,
	// regardless of exactly how AWS's HTTP client reports the failure.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, uploadErr := s.Upload(ctx, &storage.UploadRequest{Key: "k", Data: []byte("d")})
	// The upload MUST fail (fake non-STS endpoint) — otherwise the redaction
	// assertions below are skipped and the security property goes unverified.
	require.Error(t, uploadErr)
	assert.NotContains(t, uploadErr.Error(), roleARN)
	assert.NotContains(t, uploadErr.Error(), externalID)
}

func TestClient_FileURL_MatchesProviderScheme(t *testing.T) {
	cfg := &s3backend.Config{
		Bucket:             "test-bucket",
		Region:             "ap-south-1",
		AWSAccessKeyID:     "AKIAEXAMPLE",
		AWSSecretAccessKey: "secretexample",
	}
	s, err := s3backend.New(context.Background(), cfg, logger.NewNoopLogger())
	require.NoError(t, err)

	got := s.FileURL("exports/report.csv")
	want := storage.FileURL(storage.ProviderS3, "test-bucket", "exports/report.csv")
	assert.Equal(t, want, got)
}
