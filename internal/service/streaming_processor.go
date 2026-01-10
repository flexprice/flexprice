package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/hashicorp/go-retryablehttp"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// StreamingProcessor handles streaming processing of large files
type StreamingProcessor struct {
	Client           httpclient.Client
	Logger           *logger.Logger
	ProviderRegistry *FileProviderRegistry
	CSVProcessor     *CSVProcessor
	JSONProcessor    *JSONProcessor
	RetryClient      *retryablehttp.Client
}

// NewStreamingProcessor creates a new streaming processor
func NewStreamingProcessor(client httpclient.Client, logger *logger.Logger) *StreamingProcessor {
	// Configure retryable HTTP client
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 30 * time.Second
	retryClient.Logger = logger.GetRetryableHTTPLogger()

	return &StreamingProcessor{
		Client:           client,
		Logger:           logger,
		ProviderRegistry: NewFileProviderRegistry(),
		RetryClient:      retryClient,
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
	MaxErrors      int           `json:"max_errors"`      // Maximum errors to accumulate before stopping
	BatchSize      int           `json:"batch_size"`      // Number of chunks to process before updating progress
}

// DefaultStreamingConfig returns default streaming configuration
func DefaultStreamingConfig() *StreamingConfig {
	return &StreamingConfig{
		ChunkSize:      1000,       // Process 1000 records per chunk
		BufferSize:     256 * 1024, // 256KB buffer for better performance
		UpdateInterval: 30 * time.Second,
		MaxRetries:     3,
		RetryDelay:     5 * time.Second,
		MaxErrors:      1000, // Stop processing after 1000 errors
		BatchSize:      10,   // Update progress every 10 chunks
	}
}

// detectFileType attempts to determine if the file is CSV or JSON
func (sp *StreamingProcessor) detectFileType(content []byte) FileType {
	// Skip BOM if present
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}

	// Trim whitespace
	trimmed := bytes.TrimSpace(content)
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

	// Use the file type from the task
	switch t.FileType {
	case types.FileTypeCSV:
		return sp.processCSVStream(ctx, t, processor, config, stream)
	case types.FileTypeJSON:
		return sp.processJSONStream(ctx, t, processor, config, stream)
	default:
		return ierr.NewErrorf("unsupported file type: %s", t.FileType).
			WithHint("Unsupported file type").
			WithReportableDetails(map[string]interface{}{
				"file_type": t.FileType,
			}).
			Mark(ierr.ErrValidation)
	}
}

// processCSVStream processes a CSV file in streaming fashion
func (sp *StreamingProcessor) processCSVStream(
	ctx context.Context,
	t *task.Task,
	processor ChunkProcessor,
	config *StreamingConfig,
	reader io.Reader,
) error {
	// Create CSV reader with buffering
	csvReader := csv.NewReader(bufio.NewReaderSize(reader, config.BufferSize))
	csvReader.LazyQuotes = true
	csvReader.FieldsPerRecord = -1
	csvReader.ReuseRecord = false // Disable record reuse to avoid slice reference issues
	csvReader.TrimLeadingSpace = true

	// Read headers first
	headers, err := csvReader.Read()
	if err != nil {
		sp.Logger.Error("failed to read CSV headers", "error", err)
		return ierr.NewError("failed to read CSV headers").
			WithHint("Failed to read CSV headers").
			WithReportableDetails(map[string]interface{}{
				"error": err,
			}).
			Mark(ierr.ErrValidation)
	}

	// Strip BOM from first header if present
	if len(headers) > 0 && len(headers[0]) >= 3 {
		if headers[0][0] == '\xEF' && headers[0][1] == '\xBB' && headers[0][2] == '\xBF' {
			headers[0] = headers[0][3:]
		}
	}

	sp.Logger.Debugw("parsed CSV headers", "headers", headers)

	// Process file in chunks
	var chunk [][]string
	chunkIndex := 0
	totalProcessed := 0
	totalSuccessful := 0
	totalFailed := 0
	var allErrors []string
	lastProgressUpdate := time.Now()

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			sp.Logger.Error("failed to read CSV line", "error", err)
			allErrors = append(allErrors, fmt.Sprintf("CSV read error: %v", err))

			// Check error limit
			if len(allErrors) >= config.MaxErrors {
				sp.Logger.Error("maximum error limit reached, stopping processing", "max_errors", config.MaxErrors)
				break
			}
			continue
		}

		// Log each record being processed for debugging
		sp.Logger.Debugw("processing CSV record",
			"record", record,
			"chunk_size", len(chunk),
			"chunk_index", chunkIndex)

		chunk = append(chunk, record)

		// Process chunk when it reaches the configured size
		if len(chunk) >= config.ChunkSize {
			sp.Logger.Debugw("processing chunk",
				"chunk_index", chunkIndex,
				"chunk_size", len(chunk),
				"records", chunk)

			result, err := sp.processChunkWithRetry(ctx, processor, chunk, headers, chunkIndex, config)
			if err != nil {
				sp.Logger.Error("failed to process chunk", "chunk_index", chunkIndex, "error", err)
				allErrors = append(allErrors, fmt.Sprintf("Chunk %d: %v", chunkIndex, err))

				// Check error limit
				if len(allErrors) >= config.MaxErrors {
					sp.Logger.Error("maximum error limit reached, stopping processing", "max_errors", config.MaxErrors)
					break
				}
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

			// Update progress in batches
			if chunkIndex%config.BatchSize == 0 || time.Since(lastProgressUpdate) >= config.UpdateInterval {
				sp.updateTaskProgress(ctx, t, totalProcessed, totalSuccessful, totalFailed, chunkIndex)
				lastProgressUpdate = time.Now()
			}
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

	return sp.finalizeProcessing(t, totalProcessed, totalSuccessful, totalFailed, allErrors, chunkIndex)
}

// processJSONStream processes a JSON file in streaming fashion
func (sp *StreamingProcessor) processJSONStream(
	ctx context.Context,
	t *task.Task,
	processor ChunkProcessor,
	config *StreamingConfig,
	reader io.Reader,
) error {
	// Use standard library for JSON processing
	decoder := json.NewDecoder(bufio.NewReaderSize(reader, config.BufferSize))

	// Read opening bracket
	token, err := decoder.Token()
	if err != nil {
		return ierr.NewErrorf("invalid JSON content: %v", err).
			WithHint("Invalid JSON content").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrValidation)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '[' {
		return ierr.NewError("JSON content must start with an array").
			WithHint("Invalid JSON format").
			Mark(ierr.ErrValidation)
	}

	// Extract headers from first object
	headers, err := sp.JSONProcessor.ExtractHeaders(decoder)
	if err != nil {
		return err
	}

	sp.Logger.Debugw("parsed JSON headers", "headers", headers)

	// Process objects in chunks
	var chunk [][]string
	chunkIndex := 0
	totalProcessed := 0
	totalSuccessful := 0
	totalFailed := 0
	var allErrors []string

	for decoder.More() {
		var obj map[string]interface{}
		if err := decoder.Decode(&obj); err != nil {
			sp.Logger.Error("failed to decode JSON object", "error", err)
			allErrors = append(allErrors, fmt.Sprintf("JSON decode error: %v", err))
			continue
		}

		// Convert object to string array matching CSV format
		record := make([]string, len(headers))
		for i, header := range headers {
			if val, ok := obj[header]; ok {
				record[i] = fmt.Sprintf("%v", val)
			}
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

	return sp.finalizeProcessing(t, totalProcessed, totalSuccessful, totalFailed, allErrors, chunkIndex)
}

// finalizeProcessing updates the task with final processing results
func (sp *StreamingProcessor) finalizeProcessing(
	t *task.Task,
	totalProcessed int,
	totalSuccessful int,
	totalFailed int,
	allErrors []string,
	chunkIndex int,
) error {
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

// processChunkWithRetry processes a chunk with retry logic using battle-tested backoff
func (sp *StreamingProcessor) processChunkWithRetry(
	ctx context.Context,
	processor ChunkProcessor,
	chunk [][]string,
	headers []string,
	chunkIndex int,
	config *StreamingConfig,
) (*ChunkResult, error) {
	// Configure exponential backoff
	backoffConfig := backoff.NewExponentialBackOff()
	backoffConfig.MaxElapsedTime = 5 * time.Minute
	backoffConfig.InitialInterval = config.RetryDelay
	backoffConfig.MaxInterval = 30 * time.Second

	var chunkResult *ChunkResult
	operation := func() error {
		var err error
		chunkResult, err = processor.ProcessChunk(ctx, chunk, headers, chunkIndex)
		return err
	}

	err := backoff.Retry(operation, backoffConfig)
	if err != nil {
		sp.Logger.Warnw("chunk processing failed after retries",
			"chunk_index", chunkIndex,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to process chunk after retries").
			WithReportableDetails(map[string]interface{}{
				"chunk_index": chunkIndex,
				"max_retries": config.MaxRetries,
			}).
			Mark(ierr.ErrValidation)
	}

	return chunkResult, nil
}

// downloadFileStream downloads a file and returns a stream using retryable HTTP client
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

	// Create retryable HTTP request
	req, err := retryablehttp.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		sp.Logger.Error("failed to create retryable HTTP request", "error", err, "url", downloadURL)
		errorSummary := fmt.Sprintf("Failed to create HTTP request: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Execute request with automatic retries
	resp, err := sp.RetryClient.Do(req)
	if err != nil {
		sp.Logger.Error("failed to download file", "error", err, "url", downloadURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to download file: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, fmt.Errorf("failed to download file from %s: %w", provider.GetProviderName(), err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		sp.Logger.Error("failed to download file", "status_code", resp.StatusCode, "url", downloadURL, "provider", provider.GetProviderName())
		errorSummary := fmt.Sprintf("Failed to download file: HTTP %d", resp.StatusCode)
		t.ErrorSummary = &errorSummary
		return nil, fmt.Errorf("failed to download file from %s: HTTP %d", provider.GetProviderName(), resp.StatusCode)
	}

	// Return the response body directly for true streaming (no memory loading)
	return resp.Body, nil
}

// updateTaskProgress updates the task progress in the database
func (sp *StreamingProcessor) updateTaskProgress(ctx context.Context, t *task.Task, processed, successful, failed, chunkIndex int) {
	// Update task fields in memory
	t.ProcessedRecords = processed
	t.SuccessfulRecords = successful
	t.FailedRecords = failed

	sp.Logger.Infow("updating task progress",
		"task_id", t.ID,
		"processed", processed,
		"successful", successful,
		"failed", failed,
		"chunk_index", chunkIndex)

	// Note: The actual database update will be done by the task service
	// after the streaming processing is complete to avoid frequent DB writes
}

// Close cleans up resources
func (sp *StreamingProcessor) Close() {
	// No resources to clean up
}
