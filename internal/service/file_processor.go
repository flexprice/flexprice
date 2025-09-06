package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"

	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

// FileProcessor handles both streaming and regular file processing
type FileProcessor struct {
	*StreamingProcessor
	ProviderRegistry *FileProviderRegistry
	CSVProcessor     *CSVProcessor
}

// NewFileProcessor creates a new file processor
func NewFileProcessor(client httpclient.Client, logger *logger.Logger) *FileProcessor {
	return &FileProcessor{
		StreamingProcessor: NewStreamingProcessor(client, logger),
		ProviderRegistry:   NewFileProviderRegistry(),
		CSVProcessor:       NewCSVProcessor(logger),
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
		return nil, fmt.Errorf("failed to get download URL from %s provider: %w", provider.GetProviderName(), err)
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
		return nil, fmt.Errorf("failed to download file from %s: %w", provider.GetProviderName(), err)
	}

	if resp.StatusCode != http.StatusOK {
		fp.Logger.Error("failed to download file", "status_code", resp.StatusCode, "url", downloadURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to download file: HTTP %d", resp.StatusCode)
		t.ErrorSummary = &errorSummary
		return nil, fmt.Errorf("failed to download file from %s: HTTP %d", provider.GetProviderName(), resp.StatusCode)
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

// PrepareCSVReader creates a configured CSV reader from the file content
func (fp *FileProcessor) PrepareCSVReader(fileContent []byte) (*csv.Reader, error) {
	return fp.CSVProcessor.PrepareCSVReader(fileContent)
}
