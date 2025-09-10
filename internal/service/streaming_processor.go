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
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	CircuitBreakerClosed CircuitBreakerState = iota
	CircuitBreakerOpen
	CircuitBreakerHalfOpen
)

// CircuitBreaker implements the circuit breaker pattern for chunk processing
type CircuitBreaker struct {
	maxFailures  int
	failureCount int
	lastFailure  time.Time
	resetTimeout time.Duration
	state        CircuitBreakerState
	mutex        sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        CircuitBreakerClosed,
	}
}

// CanExecute returns true if the circuit breaker allows execution
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case CircuitBreakerClosed:
		return true
	case CircuitBreakerOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailure) >= cb.resetTimeout {
			cb.mutex.RUnlock()
			cb.mutex.Lock()
			if cb.state == CircuitBreakerOpen && time.Since(cb.lastFailure) >= cb.resetTimeout {
				cb.state = CircuitBreakerHalfOpen
			}
			cb.mutex.Unlock()
			cb.mutex.RLock()
			return cb.state == CircuitBreakerHalfOpen
		}
		return false
	case CircuitBreakerHalfOpen:
		return true
	default:
		return false
	}
}

// RecordSuccess records a successful execution
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failureCount = 0
	cb.state = CircuitBreakerClosed
}

// RecordFailure records a failed execution
func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failureCount++
	cb.lastFailure = time.Now()

	if cb.failureCount >= cb.maxFailures {
		cb.state = CircuitBreakerOpen
	}
}

// StreamingProcessor handles streaming processing of large files
type StreamingProcessor struct {
	Client           httpclient.Client
	Logger           *logger.Logger
	ProviderRegistry *FileProviderRegistry
	CSVProcessor     *CSVProcessor
	JSONProcessor    *JSONProcessor
	CircuitBreaker   *CircuitBreaker
}

// NewStreamingProcessor creates a new streaming processor
func NewStreamingProcessor(client httpclient.Client, logger *logger.Logger) *StreamingProcessor {
	return &StreamingProcessor{
		Client:           client,
		Logger:           logger,
		ProviderRegistry: NewFileProviderRegistry(),
		CircuitBreaker:   NewCircuitBreaker(5, 30*time.Second), // 5 failures in 30 seconds opens circuit
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
	csvReader.ReuseRecord = true
	csvReader.TrimLeadingSpace = true

	// Read headers
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

		chunk = append(chunk, record)

		// Process chunk when it reaches the configured size
		if len(chunk) >= config.ChunkSize {
			// Check circuit breaker
			if !sp.CircuitBreaker.CanExecute() {
				sp.Logger.Error("circuit breaker is open, stopping processing", "chunk_index", chunkIndex)
				break
			}

			result, err := sp.processChunkWithRetry(ctx, processor, chunk, headers, chunkIndex, config)
			if err != nil {
				sp.Logger.Error("failed to process chunk", "chunk_index", chunkIndex, "error", err)
				sp.CircuitBreaker.RecordFailure()
				allErrors = append(allErrors, fmt.Sprintf("Chunk %d: %v", chunkIndex, err))

				// Check error limit
				if len(allErrors) >= config.MaxErrors {
					sp.Logger.Error("maximum error limit reached, stopping processing", "max_errors", config.MaxErrors)
					break
				}
			} else {
				sp.CircuitBreaker.RecordSuccess()
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

	// Create HTTP request directly for streaming
	httpReq, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		sp.Logger.Error("failed to create HTTP request", "error", err, "url", downloadURL)
		errorSummary := fmt.Sprintf("Failed to create HTTP request: %v", err)
		t.ErrorSummary = &errorSummary
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Make the request with extended timeout for large file downloads
	httpClient := &http.Client{
		Timeout: 10 * time.Minute, // Extended timeout for large file downloads
	}
	resp, err := httpClient.Do(httpReq)
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
	// This would typically update the task in the database
	// For now, we'll just log the progress
	sp.Logger.Infow("updating task progress",
		"task_id", t.ID,
		"processed", processed,
		"successful", successful,
		"failed", failed,
		"chunk_index", chunkIndex)

	// Update task fields
	t.ProcessedRecords = processed
	t.SuccessfulRecords = successful
	t.FailedRecords = failed

	// TODO: Implement actual database update here
	// This should be done through the task repository
}
