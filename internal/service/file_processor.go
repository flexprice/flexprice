package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

// FileProcessor handles both streaming and regular file processing
type FileProcessor struct {
	*StreamingProcessor
	ProviderRegistry *FileProviderRegistry
	CSVProcessor     *CSVProcessor
	JSONProcessor    *JSONProcessor
}

// NewFileProcessor creates a new file processor
func NewFileProcessor(client httpclient.Client, logger *logger.Logger) *FileProcessor {
	return &FileProcessor{
		StreamingProcessor: NewStreamingProcessor(client, logger),
		ProviderRegistry:   NewFileProviderRegistry(),
		CSVProcessor:       NewCSVProcessor(logger),
		JSONProcessor:      NewJSONProcessor(logger),
	}
}

// DownloadFile downloads a file and returns the full content (for regular processing)
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

// FileType represents the type of file being processed
type FileType string

const (
	FileTypeCSV  FileType = "csv"
	FileTypeJSON FileType = "json"
)

// DetectFileType attempts to determine if the file is CSV or JSON
func (fp *FileProcessor) DetectFileType(fileContent []byte) FileType {
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

// PrepareJSONReader creates a configured JSON decoder from the file content
func (fp *FileProcessor) PrepareJSONReader(fileContent []byte) (*json.Decoder, error) {
	return fp.JSONProcessor.PrepareJSONReader(fileContent)
}
