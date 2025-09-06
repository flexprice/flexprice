package service

import (
	"bytes"
	"encoding/csv"

	"github.com/flexprice/flexprice/internal/logger"
)

// CSVProcessor handles CSV-specific operations
type CSVProcessor struct {
	Logger *logger.Logger
}

// NewCSVProcessor creates a new CSV processor
func NewCSVProcessor(logger *logger.Logger) *CSVProcessor {
	return &CSVProcessor{
		Logger: logger,
	}
}

// PrepareCSVReader creates a configured CSV reader from the file content
func (cp *CSVProcessor) PrepareCSVReader(fileContent []byte) (*csv.Reader, error) {
	// Check for and remove BOM if present
	if len(fileContent) >= 3 && fileContent[0] == 0xEF && fileContent[1] == 0xBB && fileContent[2] == 0xBF {
		// BOM detected, remove it
		fileContent = fileContent[3:]
		cp.Logger.Debug("DEBUG: BOM detected and removed from file content")
	}

	reader := csv.NewReader(bytes.NewReader(fileContent))

	// Configure CSV reader to handle potential issues
	reader.LazyQuotes = true       // Allow lazy quotes
	reader.FieldsPerRecord = -1    // Allow variable number of fields
	reader.ReuseRecord = true      // Reuse record memory
	reader.TrimLeadingSpace = true // Trim leading space

	return reader, nil
}
