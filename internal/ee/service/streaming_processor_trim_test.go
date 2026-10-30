package service

import (
	"context"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/logger"
)

type captureProcessor struct {
	headers []string
	rows    [][]string
}

func (c *captureProcessor) ProcessChunk(_ context.Context, chunk [][]string, headers []string, _ int) (*ChunkResult, error) {
	c.headers = headers
	c.rows = append(c.rows, chunk...)
	return &ChunkResult{ProcessedRecords: len(chunk), SuccessfulRecords: len(chunk)}, nil
}

// A spreadsheet export that quotes its cells keeps the padding inside the quotes,
// which csv.TrimLeadingSpace does not strip. That padding is how a meter ended up
// aggregating on " output_tokens" and silently billing zero usage.
func TestProcessFileStream_TrimsHeadersAndCells(t *testing.T) {
	csvData := "name, aggregation_field \ngemma,\" output_tokens \"\n"

	log, err := logger.NewLogger(&config.Configuration{})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	sp := NewStreamingProcessor(log)
	proc := &captureProcessor{}

	cfg := DefaultStreamingConfig()
	cfg.ChunkSize = 1

	if err := sp.ProcessFileStream(context.Background(), &task.Task{}, strings.NewReader(csvData), proc, cfg); err != nil {
		t.Fatalf("process: %v", err)
	}

	if proc.headers[1] != "aggregation_field" {
		t.Fatalf("header not trimmed: %q", proc.headers[1])
	}
	if len(proc.rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(proc.rows))
	}
	if proc.rows[0][1] != "output_tokens" {
		t.Fatalf("cell not trimmed: %q", proc.rows[0][1])
	}
}
