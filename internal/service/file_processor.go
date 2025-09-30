package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/h2non/filetype"
	"github.com/hashicorp/go-retryablehttp"
	jsoniter "github.com/json-iterator/go"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

// FileProcessor handles both streaming and regular file processing
// It provides intelligent file processing by automatically choosing between:
// - Memory-based processing for small files (< 10MB)
// - Streaming processing for large files (>= 10MB)
// This prevents OOM errors while maintaining performance for small files
type FileProcessor struct {
	*StreamingProcessor
	ProviderRegistry *FileProviderRegistry
	CSVProcessor     *CSVProcessor
	JSONProcessor    *JSONProcessor
	RetryClient      *retryablehttp.Client

	// Configuration for file size thresholds
	MaxMemoryFileSize int64 // Maximum file size to process in memory (default: 10MB)
	MaxFileSize       int64 // Maximum file size allowed (default: 1GB)
}

// NewFileProcessor creates a new file processor with default configuration
// Default settings:
// - MaxMemoryFileSize: 10MB (files smaller than this are processed in memory)
// - MaxFileSize: 1GB (maximum file size allowed)
func NewFileProcessor(client httpclient.Client, logger *logger.Logger) *FileProcessor {
	// Configure retryable HTTP client
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 30 * time.Second
	retryClient.Logger = logger.GetRetryableHTTPLogger()

	return &FileProcessor{
		StreamingProcessor: NewStreamingProcessor(client, logger),
		ProviderRegistry:   NewFileProviderRegistry(),
		CSVProcessor:       NewCSVProcessor(logger),
		JSONProcessor:      NewJSONProcessor(logger),
		RetryClient:        retryClient,
		MaxMemoryFileSize:  10 * 1024 * 1024,   // 10MB
		MaxFileSize:        1024 * 1024 * 1024, // 1GB
	}
}

// DownloadFile downloads a file and returns the full content (for regular processing)
// WARNING: This method loads the entire file into memory. Use DownloadFileStream for large files.
// This method is suitable for small files (< 10MB) to avoid OOM errors.
func (fp *FileProcessor) DownloadFile(ctx context.Context, t *task.Task) ([]byte, error) {
	// Get the appropriate provider for the file URL
	provider := fp.ProviderRegistry.GetProvider(t.FileURL)

	// Get the actual download URL from the provider
	downloadURL, err := provider.GetDownloadURL(ctx, t.FileURL)
	if err != nil {
		fp.Logger.Error("failed to get download URL", "error", err, "url", t.FileURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to get download URL: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, ierr.WithError(err).
			WithHint("Failed to get download URL").
			WithReportableDetails(map[string]interface{}{
				"provider": provider.GetProviderName(),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	fp.Logger.Debugw("using file provider", "original_url", t.FileURL, "download_url", downloadURL, "provider", provider.GetProviderName())

	// Download file
	req := &httpclient.Request{
		Method: "GET",
		URL:    downloadURL,
	}

	resp, err := fp.Client.Send(ctx, req)
	if err != nil {
		fp.Logger.Error("failed to download file", "error", err, "url", downloadURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to download file: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, ierr.WithError(err).
			WithHint("Failed to download file").
			WithReportableDetails(map[string]interface{}{
				"provider": provider.GetProviderName(),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	if resp.StatusCode != http.StatusOK {
		fp.Logger.Error("failed to download file", "status_code", resp.StatusCode, "url", downloadURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to download file: HTTP %d", resp.StatusCode)
		t.ErrorSummary = &errorSummary
		return nil, ierr.NewErrorf("failed to download file: HTTP %d", resp.StatusCode).
			WithHint("Failed to download file").
			WithReportableDetails(map[string]interface{}{
				"provider":    provider.GetProviderName(),
				"status_code": resp.StatusCode,
			}).
			Mark(ierr.ErrHTTPClient)
	}

	// Log the first few bytes of the response for debugging
	previewLen := 200
	if len(resp.Body) < previewLen {
		previewLen = len(resp.Body)
	}
	fp.Logger.Debugw("received file content preview",
		"preview", string(resp.Body[:previewLen]),
		"content_type", resp.Headers["Content-Type"],
		"content_length", len(resp.Body),
		"provider", provider.GetProviderName())

	return resp.Body, nil
}

// DownloadFileStream downloads a file and returns a stream for large file processing
// This method is memory-efficient and suitable for large files (>= 10MB)
// The returned io.ReadCloser must be closed by the caller to prevent resource leaks
func (fp *FileProcessor) DownloadFileStream(ctx context.Context, t *task.Task) (io.ReadCloser, error) {
	// Get the appropriate provider for the file URL
	provider := fp.ProviderRegistry.GetProvider(t.FileURL)

	// Get the actual download URL from the provider
	downloadURL, err := provider.GetDownloadURL(ctx, t.FileURL)
	if err != nil {
		fp.Logger.Error("failed to get download URL for streaming", "error", err, "url", t.FileURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to get download URL: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, ierr.WithError(err).
			WithHint("Failed to get download URL for streaming").
			WithReportableDetails(map[string]interface{}{
				"provider": provider.GetProviderName(),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	fp.Logger.Debugw("using file provider for streaming", "original_url", t.FileURL, "download_url", downloadURL, "provider", provider.GetProviderName())

	// Create HTTP request directly for streaming
	httpReq, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		fp.Logger.Error("failed to create HTTP request for streaming", "error", err, "url", downloadURL)
		errorSummary := fmt.Sprintf("Failed to create HTTP request: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, ierr.WithError(err).
			WithHint("Failed to create HTTP request for streaming").
			WithReportableDetails(map[string]interface{}{
				"provider": provider.GetProviderName(),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	// Make the request with extended timeout for large file downloads
	httpClient := &http.Client{
		Timeout: 10 * time.Minute, // Extended timeout for large file downloads
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		fp.Logger.Error("failed to download file stream", "error", err, "url", downloadURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to download file: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, ierr.WithError(err).
			WithHint("Failed to download file stream").
			WithReportableDetails(map[string]interface{}{
				"provider": provider.GetProviderName(),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close() // Close the response body on error
		fp.Logger.Error("failed to download file stream", "status_code", resp.StatusCode, "url", downloadURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to download file: HTTP %d", resp.StatusCode)
		t.ErrorSummary = &errorSummary
		return nil, ierr.NewErrorf("failed to download file: HTTP %d", resp.StatusCode).
			WithHint("Failed to download file stream").
			WithReportableDetails(map[string]interface{}{
				"provider":    provider.GetProviderName(),
				"status_code": resp.StatusCode,
			}).
			Mark(ierr.ErrHTTPClient)
	}

	fp.Logger.Debugw("successfully opened file stream",
		"content_type", resp.Header.Get("Content-Type"),
		"content_length", resp.Header.Get("Content-Length"),
		"provider", provider.GetProviderName())

	return resp.Body, nil
}

// GetFileSize retrieves the file size without downloading the entire file
// This is useful for determining whether to use memory-based or streaming processing
func (fp *FileProcessor) GetFileSize(ctx context.Context, t *task.Task) (int64, error) {
	// Get the appropriate provider for the file URL
	provider := fp.ProviderRegistry.GetProvider(t.FileURL)

	// Get the actual download URL from the provider
	downloadURL, err := provider.GetDownloadURL(ctx, t.FileURL)
	if err != nil {
		fp.Logger.Error("failed to get download URL for size check", "error", err, "url", t.FileURL, "provider", provider.GetProviderName())
		return 0, ierr.WithError(err).
			WithHint("Failed to get download URL for size check").
			WithReportableDetails(map[string]interface{}{
				"provider": provider.GetProviderName(),
			}).
			Mark(ierr.ErrHTTPClient)
	}

	// Create HEAD request to get file size without downloading
	httpReq, err := http.NewRequestWithContext(ctx, "HEAD", downloadURL, nil)
	if err != nil {
		fp.Logger.Error("failed to create HEAD request", "error", err, "url", downloadURL)
		return 0, ierr.WithError(err).
			WithHint("Failed to create HEAD request").
			Mark(ierr.ErrHTTPClient)
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second, // Shorter timeout for HEAD requests
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		fp.Logger.Error("failed to get file size", "error", err, "url", downloadURL, "provider", provider.GetProviderName())
		return 0, ierr.WithError(err).
			WithHint("Failed to get file size").
			WithReportableDetails(map[string]interface{}{
				"provider": provider.GetProviderName(),
			}).
			Mark(ierr.ErrHTTPClient)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fp.Logger.Error("failed to get file size", "status_code", resp.StatusCode, "url", downloadURL, "provider", provider.GetProviderName())
		return 0, ierr.NewErrorf("failed to get file size: HTTP %d", resp.StatusCode).
			WithHint("Failed to get file size").
			WithReportableDetails(map[string]interface{}{
				"provider":    provider.GetProviderName(),
				"status_code": resp.StatusCode,
			}).
			Mark(ierr.ErrHTTPClient)
	}

	// Parse Content-Length header
	contentLength := resp.Header.Get("Content-Length")
	if contentLength == "" {
		fp.Logger.Warn("Content-Length header not available", "url", downloadURL, "provider", provider.GetProviderName())
		return 0, ierr.NewError("Content-Length header not available").
			WithHint("File size cannot be determined").
			WithReportableDetails(map[string]interface{}{
				"provider": provider.GetProviderName(),
			}).
			Mark(ierr.ErrValidation)
	}

	fileSize, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil {
		fp.Logger.Error("failed to parse Content-Length", "error", err, "content_length", contentLength, "url", downloadURL)
		return 0, ierr.WithError(err).
			WithHint("Failed to parse file size").
			WithReportableDetails(map[string]interface{}{
				"content_length": contentLength,
				"provider":       provider.GetProviderName(),
			}).
			Mark(ierr.ErrValidation)
	}

	fp.Logger.Debugw("retrieved file size", "size", fileSize, "url", downloadURL, "provider", provider.GetProviderName())
	return fileSize, nil
}

// ShouldUseStreaming determines if a file should be processed using streaming
// based on its size and configuration thresholds
func (fp *FileProcessor) ShouldUseStreaming(fileSize int64) bool {
	return fileSize >= fp.MaxMemoryFileSize
}

// ValidateFileSize checks if the file size is within acceptable limits
func (fp *FileProcessor) ValidateFileSize(fileSize int64) error {
	if fileSize <= 0 {
		return ierr.NewError("invalid file size").
			WithHint("File size must be greater than 0").
			Mark(ierr.ErrValidation)
	}

	if fileSize > fp.MaxFileSize {
		return ierr.NewErrorf("file too large: %d bytes (max: %d bytes)", fileSize, fp.MaxFileSize).
			WithHint("File exceeds maximum allowed size").
			WithReportableDetails(map[string]interface{}{
				"file_size":     fileSize,
				"max_file_size": fp.MaxFileSize,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// FileType represents the type of file being processed
type FileType string

const (
	FileTypeCSV  FileType = "csv"
	FileTypeJSON FileType = "json"
)

// DetectFileType attempts to determine if the file is CSV or JSON using battle-tested filetype package
func (fp *FileProcessor) DetectFileType(fileContent []byte) FileType {
	// Use filetype package for more accurate detection
	if filetype.Is(fileContent, "csv") {
		return FileTypeCSV
	}

	if filetype.Is(fileContent, "json") {
		return FileTypeJSON
	}

	// Fallback to content-based detection
	// Skip BOM if present
	if len(fileContent) >= 3 && fileContent[0] == 0xEF && fileContent[1] == 0xBB && fileContent[2] == 0xBF {
		fileContent = fileContent[3:]
	}

	// Trim whitespace
	trimmed := bytes.TrimSpace(fileContent)
	if len(trimmed) == 0 {
		return FileTypeCSV // Default to CSV for empty files
	}

	// Check if content starts with [ for JSON array
	if trimmed[0] == '[' {
		return FileTypeJSON
	}

	// Default to CSV
	return FileTypeCSV
}

// PrepareCSVReader creates a configured CSV reader from the file content
func (fp *FileProcessor) PrepareCSVReader(fileContent []byte) (*csv.Reader, error) {
	return fp.CSVProcessor.PrepareCSVReader(fileContent)
}

// PrepareJSONReader creates a configured JSON decoder from the file content using jsoniter
func (fp *FileProcessor) PrepareJSONReader(fileContent []byte) (*jsoniter.Decoder, error) {
	// Convert standard decoder to jsoniter decoder
	reader := bytes.NewReader(fileContent)
	return jsoniter.NewDecoder(reader), nil
}
