package service

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// StreamingProcessor parses a CSV stream into fixed-size chunks and hands each
// chunk to a ChunkProcessor. It does NOT fetch the file — the caller opens the
// stream (from S3, disk, whatever) and hands it in. This decoupling is what
// dropped the FileProvider abstraction: the import pipeline is the only
// consumer and it now sources bytes from storage.Storage.Download directly.
type StreamingProcessor struct {
	Logger *logger.Logger
}

func NewStreamingProcessor(logger *logger.Logger) *StreamingProcessor {
	return &StreamingProcessor{Logger: logger}
}

// ChunkProcessor consumes rows in batches. Implementations live in task.go
// (EventsChunkProcessor, etc.).
type ChunkProcessor interface {
	ProcessChunk(ctx context.Context, chunk [][]string, headers []string, chunkIndex int) (*ChunkResult, error)
}

// ChunkResult is the outcome of one chunk.
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

// ProcessFileStream parses a CSV reader in chunks and dispatches each chunk to
// processor. The task struct is updated in place with running counters.
func (sp *StreamingProcessor) ProcessFileStream(
	ctx context.Context,
	t *task.Task,
	reader io.Reader,
	processor ChunkProcessor,
	config *StreamingConfig,
) error {
	if config == nil {
		config = DefaultStreamingConfig()
	}

	csvReader := csv.NewReader(bufio.NewReaderSize(reader, config.BufferSize))
	csvReader.LazyQuotes = true
	csvReader.FieldsPerRecord = -1
	// Reuse disabled: chunks are held past a Read call, so reused slices would
	// mutate under us.
	csvReader.ReuseRecord = false
	csvReader.TrimLeadingSpace = true

	headers, err := csvReader.Read()
	if err != nil {
		sp.Logger.Error(ctx, "failed to read CSV headers", "error", err)
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

	sp.Logger.Debug(ctx, "parsed CSV headers", "headers", headers)

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
			sp.Logger.Error(ctx, "failed to read CSV line", "error", err)
			allErrors = append(allErrors, fmt.Sprintf("CSV read error: %v", err))

			if len(allErrors) >= config.MaxErrors {
				sp.Logger.Info(ctx, "maximum error limit reached, stopping processing", "max_errors", config.MaxErrors)
				break
			}
			continue
		}

		chunk = append(chunk, record)

		if len(chunk) >= config.ChunkSize {
			result, err := sp.processChunkWithRetry(ctx, processor, chunk, headers, chunkIndex, config)
			if err != nil {
				sp.Logger.Error(ctx, "failed to process chunk", "error", err, "chunk_index", chunkIndex)
				allErrors = append(allErrors, fmt.Sprintf("Chunk %d: %v", chunkIndex, err))
				if len(allErrors) >= config.MaxErrors {
					sp.Logger.Info(ctx, "maximum error limit reached, stopping processing", "max_errors", config.MaxErrors)
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

			chunk = nil
			chunkIndex++

			if chunkIndex%config.BatchSize == 0 || time.Since(lastProgressUpdate) >= config.UpdateInterval {
				sp.updateTaskProgress(ctx, t, totalProcessed, totalSuccessful, totalFailed, chunkIndex)
				lastProgressUpdate = time.Now()
			}
		}
	}

	if len(chunk) > 0 {
		result, err := sp.processChunkWithRetry(ctx, processor, chunk, headers, chunkIndex, config)
		if err != nil {
			sp.Logger.Error(ctx, "failed to process final chunk", "error", err, "chunk_index", chunkIndex)
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

	return sp.finalizeProcessing(ctx, t, totalProcessed, totalSuccessful, totalFailed, allErrors, chunkIndex)
}

// finalizeProcessing writes the final counters onto the task struct. The
// database update is done by taskService after ProcessFileStream returns.
func (sp *StreamingProcessor) finalizeProcessing(
	ctx context.Context,
	t *task.Task,
	totalProcessed int,
	totalSuccessful int,
	totalFailed int,
	allErrors []string,
	chunkIndex int,
) error {
	t.ProcessedRecords = totalProcessed
	t.SuccessfulRecords = totalSuccessful
	t.FailedRecords = totalFailed

	if len(allErrors) > 0 {
		errorSummary := strings.Join(allErrors, "; ")
		t.ErrorSummary = &errorSummary
	}

	sp.Logger.Info(ctx, "completed streaming processing",
		"task_id", t.ID,
		"total_processed", totalProcessed,
		"successful", totalSuccessful,
		"failed", totalFailed,
		"chunks_processed", chunkIndex+1)

	return nil
}

// processChunkWithRetry runs processor.ProcessChunk under exponential backoff.
func (sp *StreamingProcessor) processChunkWithRetry(
	ctx context.Context,
	processor ChunkProcessor,
	chunk [][]string,
	headers []string,
	chunkIndex int,
	config *StreamingConfig,
) (*ChunkResult, error) {
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
		sp.Logger.Info(ctx, "chunk processing failed after retries",
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

// updateTaskProgress bumps in-memory counters and logs. The DB write is
// done once at the end by taskService to avoid write amplification.
func (sp *StreamingProcessor) updateTaskProgress(ctx context.Context, t *task.Task, processed, successful, failed, chunkIndex int) {
	t.ProcessedRecords = processed
	t.SuccessfulRecords = successful
	t.FailedRecords = failed

	sp.Logger.Info(ctx, "updating task progress",
		"task_id", t.ID,
		"processed", processed,
		"successful", successful,
		"failed", failed,
		"chunk_index", chunkIndex)
}
