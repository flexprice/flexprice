// Package storage provides the cloud-agnostic Storage interface and shared
// helpers (object keys, file URLs, cloud detection). The interface itself
// lives in storagetypes to break an import cycle: backend implementations
// (s3backend, gcsbackend) must import the interface type without importing
// this package, since this package's platform.go imports the backends.
package storage

import (
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
)

type Provider = storagetypes.Provider

const (
	ProviderS3  = storagetypes.ProviderS3
	ProviderGCS = storagetypes.ProviderGCS
)

type UploadFormat = storagetypes.UploadFormat

const (
	UploadFormatCSV     = storagetypes.UploadFormatCSV
	UploadFormatJSON    = storagetypes.UploadFormatJSON
	UploadFormatPDF     = storagetypes.UploadFormatPDF
	UploadFormatParquet = storagetypes.UploadFormatParquet
)

// UploadRequest describes an object to store. Key is the full object key
// (already includes any prefix) — backends do not compute prefixes.
type UploadRequest = storagetypes.UploadRequest

type UploadResponse = storagetypes.UploadResponse

// Storage is the cloud-agnostic interface every caller in the codebase uses
// to read/write object storage. No caller outside internal/storage/ may
// hold a concrete backend type or import a cloud SDK package directly.
type Storage = storagetypes.Storage
