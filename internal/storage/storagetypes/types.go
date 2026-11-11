package storagetypes

import (
	"context"
	"fmt"
	"io"
	"time"
)

type Provider string

const (
	ProviderS3  Provider = "s3"
	ProviderGCS Provider = "gcs"
)

type UploadFormat string

const (
	UploadFormatCSV     UploadFormat = "csv"
	UploadFormatJSON    UploadFormat = "json"
	UploadFormatPDF     UploadFormat = "pdf"
	UploadFormatParquet UploadFormat = "parquet"
)

// UploadRequest describes an object to store. Key is the full object key
// (already includes any prefix) — backends do not compute prefixes.
type UploadRequest struct {
	Key         string
	Data        []byte
	Format      UploadFormat
	ContentType string // optional, inferred from Format if empty
	Compress    bool
}

// StreamUploadRequest describes an upload backed by a seekable body, uploaded as-is.
type StreamUploadRequest struct {
	Key         string
	Body        io.ReadSeeker
	Format      UploadFormat
	ContentType string // optional, inferred from Format if empty
	// Gzipped marks Body as already gzip-compressed; UncompressedSize is then its size before compression.
	Gzipped          bool
	UncompressedSize int64
}

type UploadResponse struct {
	FileURL        string
	Bucket         string
	Key            string
	FileSizeBytes  int64
	CompressedSize int64
	UploadedAt     time.Time
}

// Storage is the cloud-agnostic interface every caller in the codebase uses
// to read/write object storage. No caller outside internal/storage/ may
// hold a concrete backend type or import a cloud SDK package directly.
type Storage interface {
	Upload(ctx context.Context, req *UploadRequest) (*UploadResponse, error)
	UploadStream(ctx context.Context, req *StreamUploadRequest) (*UploadResponse, error)
	Download(ctx context.Context, key string) ([]byte, error)
	Exists(ctx context.Context, key string) (bool, error)
	PresignGet(ctx context.Context, key string, duration time.Duration) (string, error)
	ValidateConnection(ctx context.Context) error
	// ValidateWrite writes an empty object at key to prove the credentials can write to the bucket.
	ValidateWrite(ctx context.Context, key string) error
	FileURL(key string) string
	Provider() Provider
}

func schemeFor(p Provider) string {
	switch p {
	case ProviderGCS:
		return "gs"
	default:
		return "s3"
	}
}

// FileURL formats a bucket+key pair as a scheme-prefixed URL for the given
// provider. Lives here (not in package storage) so backend implementations
// (s3backend, gcsbackend) can call it without importing package storage,
// which would reintroduce the storage -> backend -> storage import cycle
// that this package exists to break. Package storage re-exports this as
// storage.FileURL for all other callers.
func FileURL(provider Provider, bucket, key string) string {
	return fmt.Sprintf("%s://%s/%s", schemeFor(provider), bucket, key)
}
