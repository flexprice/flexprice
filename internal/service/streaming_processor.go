package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

// StreamingProcessor handles streaming processing of large files
type StreamingProcessor struct {
	Client           httpclient.Client
	Logger           *logger.Logger
	ProviderRegistry *FileProviderRegistry
}

// NewStreamingProcessor creates a new streaming processor
func NewStreamingProcessor(client httpclient.Client, logger *logger.Logger) *StreamingProcessor {
	return &StreamingProcessor{
		Client:           client,
		Logger:           logger,
		ProviderRegistry: NewFileProviderRegistry(),
	}
}

// ChunkProcessor defines the interface for processing file chunks
type ChunkProcessor interface {
	ProcessChunk(ctx context.Context, chunk [][]string, headers []string, chunkIndex int) (*ChunkResult, error)
}

// ChunkResult represents the result of processing a chunk
type ChunkResult struct {
	ProcessedRecords  int     `json:"processed_records"`
	SuccessfulRecords int     `json:"successful_records"`
	FailedRecords     int     `json:"failed_records"`
	ErrorSummary      *string `json:"error_summary,omitempty"`
}

// StreamingConfig holds configuration for streaming processing
type StreamingConfig struct {
	ChunkSize      int           `json:"chunk_size"`      // Number of records per chunk
	BufferSize     int           `json:"buffer_size"`     // Buffer size for reading
	UpdateInterval time.Duration `json:"update_interval"` // Progress update interval
	MaxRetries     int           `json:"max_retries"`     // Maximum retries for failed chunks
	RetryDelay     time.Duration `json:"retry_delay"`     // Delay between retries
}

// DefaultStreamingConfig returns default streaming configuration
func DefaultStreamingConfig() *StreamingConfig {
	return &StreamingConfig{
		ChunkSize:      1000,      // Process 1000 records per chunk
		BufferSize:     64 * 1024, // 64KB buffer
		UpdateInterval: 30 * time.Second,
		MaxRetries:     3,
		RetryDelay:     5 * time.Second,
	}
}

// ProcessFileStream processes a file in streaming fashion
func (sp *StreamingProcessor) ProcessFileStream(
	ctx context.Context,
	t *task.Task,
	processor ChunkProcessor,
	config *StreamingConfig,
) error {
	if config == nil {
		config = DefaultStreamingConfig()
	}

	// Download file stream
	stream, err := sp.downloadFileStream(ctx, t)
	if err != nil {
		return err
	}
	defer stream.Close()

	// Create CSV reader with buffering
	reader := csv.NewReader(bufio.NewReaderSize(stream, config.BufferSize))
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	reader.TrimLeadingSpace = true

	// Read headers
	headers, err := reader.Read()
	if err != nil {
		sp.Logger.Error("failed to read CSV headers", "error", err)
		return ierr.NewError("failed to read CSV headers").
			WithHint("Failed to read CSV headers").
			WithReportableDetails(map[string]interface{}{
				"error": err,
			}).
			Mark(ierr.ErrValidation)
	}

	sp.Logger.Debugw("parsed CSV headers", "headers", headers)

	// Process file in chunks
	var chunk [][]string
	chunkIndex := 0
	totalProcessed := 0
	totalSuccessful := 0
	totalFailed := 0
	var allErrors []string

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			sp.Logger.Error("failed to read CSV line", "error", err)
			allErrors = append(allErrors, fmt.Sprintf("CSV read error: %v", err))
			continue
		}

		chunk = append(chunk, record)

		// Process chunk when it reaches the configured size
		if len(chunk) >= config.ChunkSize {
			result, err := sp.processChunkWithRetry(ctx, processor, chunk, headers, chunkIndex, config)
			if err != nil {
				sp.Logger.Error("failed to process chunk", "chunk_index", chunkIndex, "error", err)
				allErrors = append(allErrors, fmt.Sprintf("Chunk %d: %v", chunkIndex, err))
			} else {
				totalProcessed += result.ProcessedRecords
				totalSuccessful += result.SuccessfulRecords
				totalFailed += result.FailedRecords
				if result.ErrorSummary != nil {
					allErrors = append(allErrors, *result.ErrorSummary)
				}
			}

			chunk = nil // Reset chunk
			chunkIndex++
		}
	}

	// Process remaining records in the last chunk
	if len(chunk) > 0 {
		result, err := sp.processChunkWithRetry(ctx, processor, chunk, headers, chunkIndex, config)
		if err != nil {
			sp.Logger.Error("failed to process final chunk", "chunk_index", chunkIndex, "error", err)
			allErrors = append(allErrors, fmt.Sprintf("Final chunk %d: %v", chunkIndex, err))
		} else {
			totalProcessed += result.ProcessedRecords
			totalSuccessful += result.SuccessfulRecords
			totalFailed += result.FailedRecords
			if result.ErrorSummary != nil {
				allErrors = append(allErrors, *result.ErrorSummary)
			}
		}
	}

	// Update final task status
	t.ProcessedRecords = totalProcessed
	t.SuccessfulRecords = totalSuccessful
	t.FailedRecords = totalFailed

	if len(allErrors) > 0 {
		errorSummary := strings.Join(allErrors, "; ")
		t.ErrorSummary = &errorSummary
	}

	sp.Logger.Infow("completed streaming processing",
		"task_id", t.ID,
		"total_processed", totalProcessed,
		"successful", totalSuccessful,
		"failed", totalFailed,
		"chunks_processed", chunkIndex+1)

	return nil
}

// processChunkWithRetry processes a chunk with retry logic
func (sp *StreamingProcessor) processChunkWithRetry(
	ctx context.Context,
	processor ChunkProcessor,
	chunk [][]string,
	headers []string,
	chunkIndex int,
	config *StreamingConfig,
) (*ChunkResult, error) {
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			sp.Logger.Debugw("retrying chunk processing",
				"chunk_index", chunkIndex,
				"attempt", attempt,
				"delay", config.RetryDelay)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(config.RetryDelay):
			}
		}

		result, err := processor.ProcessChunk(ctx, chunk, headers, chunkIndex)
		if err == nil {
			return result, nil
		}

		lastErr = err
		sp.Logger.Warnw("chunk processing failed",
			"chunk_index", chunkIndex,
			"attempt", attempt,
			"error", err)
	}

	return nil, ierr.WithError(lastErr).
		WithHint("Failed to process chunk after retries").
		WithReportableDetails(map[string]interface{}{
			"chunk_index": chunkIndex,
			"max_retries": config.MaxRetries,
		}).
		Mark(ierr.ErrValidation)
}

// downloadFileStream downloads a file and returns a stream
func (sp *StreamingProcessor) downloadFileStream(ctx context.Context, t *task.Task) (io.ReadCloser, error) {
	// Get the appropriate provider for the file URL
	provider := sp.ProviderRegistry.GetProvider(t.FileURL)

	// Get the actual download URL from the provider
	downloadURL, err := provider.GetDownloadURL(ctx, t.FileURL)
	if err != nil {
		sp.Logger.Error("failed to get download URL", "error", err, "url", t.FileURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to get download URL: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, fmt.Errorf("failed to get download URL from %s provider: %w", provider.GetProviderName(), err)
	}

	sp.Logger.Debugw("using file provider for streaming", "original_url", t.FileURL, "download_url", downloadURL, "provider", provider.GetProviderName())

	// Download file
	req := &httpclient.Request{
		Method: "GET",
		URL:    downloadURL,
	}

	resp, err := sp.Client.Send(ctx, req)
	if err != nil {
		sp.Logger.Error("failed to download file", "error", err, "url", downloadURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to download file: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, fmt.Errorf("failed to download file from %s: %w", provider.GetProviderName(), err)
	}

	if resp.StatusCode != http.StatusOK {
		sp.Logger.Error("failed to download file", "status_code", resp.StatusCode, "url", downloadURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to download file: HTTP %d", resp.StatusCode)
		t.ErrorSummary = &errorSummary
		return nil, fmt.Errorf("failed to download file from %s: HTTP %d", provider.GetProviderName(), resp.StatusCode)
	}

	// Return a reader from the response body
	return io.NopCloser(bytes.NewReader(resp.Body)), nil
}
