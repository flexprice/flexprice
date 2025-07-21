package v1

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// StripeMigrationHandler handles Stripe migration operations
type StripeMigrationHandler struct {
	service          StripeMigrationService
	migrationStorage MigrationStorage
}

// StripeMigrationService interface for migration operations
type StripeMigrationService interface {
	CreateCustomerMapping(ctx context.Context, req *CreateCustomerMappingRequest) error
	ValidateCustomerMapping(ctx context.Context, req *ValidateCustomerMappingRequest) (*ValidationResult, error)
	BulkCreateCustomerMappings(ctx context.Context, req *BulkCreateCustomerMappingsRequest) (*BulkOperationResult, error)
	GetMigrationStatus(ctx context.Context, migrationID uuid.UUID) (*MigrationStatus, error)
	PauseMigration(ctx context.Context, migrationID uuid.UUID) error
	ResumeMigration(ctx context.Context, migrationID uuid.UUID) error
	RollbackMigration(ctx context.Context, migrationID uuid.UUID) error
}

// MigrationStorage interface for storing migration state
type MigrationStorage interface {
	CreateMigration(ctx context.Context, migration *Migration) error
	UpdateMigration(ctx context.Context, migration *Migration) error
	GetMigration(ctx context.Context, migrationID uuid.UUID) (*Migration, error)
	ListMigrations(ctx context.Context, tenantID, environmentID uuid.UUID, limit, offset int) ([]*Migration, int, error)
}

// Request/Response DTOs

// CreateCustomerMappingRequest for single customer mapping
type CreateCustomerMappingRequest struct {
	ExternalID       string            `json:"external_id" binding:"required"`
	StripeCustomerID string            `json:"stripe_customer_id" binding:"required"`
	Email            string            `json:"email,omitempty"`
	Name             string            `json:"name,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// ValidateCustomerMappingRequest for validation
type ValidateCustomerMappingRequest struct {
	ExternalID       string `json:"external_id" binding:"required"`
	StripeCustomerID string `json:"stripe_customer_id" binding:"required"`
}

// ValidationResult contains validation results
type ValidationResult struct {
	Valid           bool     `json:"valid"`
	Errors          []string `json:"errors,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	ExistingMapping bool     `json:"existing_mapping"`
	ConflictDetails string   `json:"conflict_details,omitempty"`
}

// BulkCreateCustomerMappingsRequest for bulk operations
type BulkCreateCustomerMappingsRequest struct {
	Mappings        []CreateCustomerMappingRequest `json:"mappings" binding:"required"`
	DryRun          bool                           `json:"dry_run"`
	SkipExisting    bool                           `json:"skip_existing"`
	ValidateOnly    bool                           `json:"validate_only"`
	BatchSize       int                            `json:"batch_size,omitempty"`
	ContinueOnError bool                           `json:"continue_on_error"`
}

// BulkOperationResult contains bulk operation results
type BulkOperationResult struct {
	MigrationID      uuid.UUID              `json:"migration_id"`
	TotalRecords     int                    `json:"total_records"`
	ProcessedRecords int                    `json:"processed_records"`
	SuccessRecords   int                    `json:"success_records"`
	FailedRecords    int                    `json:"failed_records"`
	SkippedRecords   int                    `json:"skipped_records"`
	Status           string                 `json:"status"`
	StartedAt        time.Time              `json:"started_at"`
	CompletedAt      *time.Time             `json:"completed_at,omitempty"`
	Errors           []MigrationRecordError `json:"errors,omitempty"`
	ValidationErrors []string               `json:"validation_errors,omitempty"`
}

// MigrationRecordError contains details about a failed record
type MigrationRecordError struct {
	RecordIndex  int    `json:"record_index"`
	ExternalID   string `json:"external_id"`
	ErrorMessage string `json:"error_message"`
	ErrorCode    string `json:"error_code"`
	Retryable    bool   `json:"retryable"`
}

// MigrationStatus represents the current state of a migration
type MigrationStatus struct {
	ID               uuid.UUID              `json:"id"`
	TenantID         uuid.UUID              `json:"tenant_id"`
	EnvironmentID    uuid.UUID              `json:"environment_id"`
	Status           string                 `json:"status"`
	TotalRecords     int                    `json:"total_records"`
	ProcessedRecords int                    `json:"processed_records"`
	SuccessRecords   int                    `json:"success_records"`
	FailedRecords    int                    `json:"failed_records"`
	SkippedRecords   int                    `json:"skipped_records"`
	StartedAt        time.Time              `json:"started_at"`
	CompletedAt      *time.Time             `json:"completed_at,omitempty"`
	LastError        string                 `json:"last_error,omitempty"`
	Progress         float64                `json:"progress"`
	EstimatedETA     *time.Time             `json:"estimated_eta,omitempty"`
	Errors           []MigrationRecordError `json:"errors,omitempty"`
}

// Migration represents a migration operation
type Migration struct {
	ID               uuid.UUID              `json:"id"`
	TenantID         uuid.UUID              `json:"tenant_id"`
	EnvironmentID    uuid.UUID              `json:"environment_id"`
	Type             string                 `json:"type"`
	Status           string                 `json:"status"`
	TotalRecords     int                    `json:"total_records"`
	ProcessedRecords int                    `json:"processed_records"`
	SuccessRecords   int                    `json:"success_records"`
	FailedRecords    int                    `json:"failed_records"`
	SkippedRecords   int                    `json:"skipped_records"`
	Configuration    map[string]interface{} `json:"configuration"`
	StartedAt        time.Time              `json:"started_at"`
	CompletedAt      *time.Time             `json:"completed_at,omitempty"`
	LastError        string                 `json:"last_error,omitempty"`
	CreatedBy        uuid.UUID              `json:"created_by"`
	Errors           []MigrationRecordError `json:"errors,omitempty"`
}

// NewStripeMigrationHandler creates a new migration handler
func NewStripeMigrationHandler(service StripeMigrationService, storage MigrationStorage) *StripeMigrationHandler {
	return &StripeMigrationHandler{
		service:          service,
		migrationStorage: storage,
	}
}

// CreateCustomerMapping creates a single customer mapping
func (h *StripeMigrationHandler) CreateCustomerMapping(c *gin.Context) {
	var req CreateCustomerMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format", "details": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")
	environmentID := c.GetString("environment_id")

	if tenantID == "" || environmentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant_id and environment_id are required"})
		return
	}

	ctx := context.WithValue(c.Request.Context(), "tenant_id", tenantID)
	ctx = context.WithValue(ctx, "environment_id", environmentID)

	if err := h.service.CreateCustomerMapping(ctx, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create customer mapping"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Customer mapping created successfully"})
}

// ValidateCustomerMapping validates a customer mapping
func (h *StripeMigrationHandler) ValidateCustomerMapping(c *gin.Context) {
	var req ValidateCustomerMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format", "details": err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")
	environmentID := c.GetString("environment_id")

	ctx := context.WithValue(c.Request.Context(), "tenant_id", tenantID)
	ctx = context.WithValue(ctx, "environment_id", environmentID)

	result, err := h.service.ValidateCustomerMapping(ctx, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Validation failed"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// BulkCreateCustomerMappings creates multiple customer mappings
func (h *StripeMigrationHandler) BulkCreateCustomerMappings(c *gin.Context) {
	var req BulkCreateCustomerMappingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format", "details": err.Error()})
		return
	}

	// Validate request
	if len(req.Mappings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No mappings provided"})
		return
	}

	if len(req.Mappings) > 10000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Too many mappings (max 10,000)"})
		return
	}

	if req.BatchSize <= 0 {
		req.BatchSize = 100
	}

	tenantID := c.GetString("tenant_id")
	environmentID := c.GetString("environment_id")

	ctx := context.WithValue(c.Request.Context(), "tenant_id", tenantID)
	ctx = context.WithValue(ctx, "environment_id", environmentID)

	result, err := h.service.BulkCreateCustomerMappings(ctx, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start bulk operation"})
		return
	}

	c.JSON(http.StatusAccepted, result)
}

// UploadCustomerCSV uploads and processes a CSV file
func (h *StripeMigrationHandler) UploadCustomerCSV(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read uploaded file"})
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File must be a CSV"})
		return
	}

	// Parse form parameters
	dryRun := c.DefaultPostForm("dry_run", "false") == "true"
	skipExisting := c.DefaultPostForm("skip_existing", "true") == "true"
	batchSizeStr := c.DefaultPostForm("batch_size", "100")

	batchSize, err := strconv.Atoi(batchSizeStr)
	if err != nil || batchSize <= 0 {
		batchSize = 100
	}

	// Parse CSV
	mappings, err := h.parseCSVFile(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to parse CSV: %v", err)})
		return
	}

	if len(mappings) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV file contains no valid records"})
		return
	}

	if len(mappings) > 50000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV file too large (max 50,000 records)"})
		return
	}

	req := &BulkCreateCustomerMappingsRequest{
		Mappings:        mappings,
		DryRun:          dryRun,
		SkipExisting:    skipExisting,
		BatchSize:       batchSize,
		ContinueOnError: true,
	}

	tenantID := c.GetString("tenant_id")
	environmentID := c.GetString("environment_id")

	ctx := context.WithValue(c.Request.Context(), "tenant_id", tenantID)
	ctx = context.WithValue(ctx, "environment_id", environmentID)

	result, err := h.service.BulkCreateCustomerMappings(ctx, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process CSV"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":       "CSV uploaded and processing started",
		"migration_id":  result.MigrationID,
		"total_records": result.TotalRecords,
		"status":        result.Status,
	})
}

// GetMigrationStatus retrieves migration status
func (h *StripeMigrationHandler) GetMigrationStatus(c *gin.Context) {
	migrationIDStr := c.Param("migration_id")
	migrationID, err := uuid.Parse(migrationIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid migration ID"})
		return
	}

	status, err := h.service.GetMigrationStatus(c.Request.Context(), migrationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get migration status"})
		return
	}

	c.JSON(http.StatusOK, status)
}

// ListMigrations lists migration operations
func (h *StripeMigrationHandler) ListMigrations(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 50
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	tenantID, _ := uuid.Parse(c.GetString("tenant_id"))
	environmentID, _ := uuid.Parse(c.GetString("environment_id"))

	migrations, total, err := h.migrationStorage.ListMigrations(c.Request.Context(), tenantID, environmentID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list migrations"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"migrations": migrations,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

// PauseMigration pauses a running migration
func (h *StripeMigrationHandler) PauseMigration(c *gin.Context) {
	migrationIDStr := c.Param("migration_id")
	migrationID, err := uuid.Parse(migrationIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid migration ID"})
		return
	}

	if err := h.service.PauseMigration(c.Request.Context(), migrationID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to pause migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Migration paused successfully"})
}

// ResumeMigration resumes a paused migration
func (h *StripeMigrationHandler) ResumeMigration(c *gin.Context) {
	migrationIDStr := c.Param("migration_id")
	migrationID, err := uuid.Parse(migrationIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid migration ID"})
		return
	}

	if err := h.service.ResumeMigration(c.Request.Context(), migrationID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resume migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Migration resumed successfully"})
}

// RollbackMigration rollbacks a migration
func (h *StripeMigrationHandler) RollbackMigration(c *gin.Context) {
	migrationIDStr := c.Param("migration_id")
	migrationID, err := uuid.Parse(migrationIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid migration ID"})
		return
	}

	if err := h.service.RollbackMigration(c.Request.Context(), migrationID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rollback migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Migration rollback started"})
}

// parseCSVFile parses a CSV file and returns customer mappings
func (h *StripeMigrationHandler) parseCSVFile(file io.Reader) ([]CreateCustomerMappingRequest, error) {
	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV must have at least header and one data row")
	}

	header := records[0]
	var externalIDCol, stripeIDCol, emailCol, nameCol int = -1, -1, -1, -1

	// Find required columns
	for i, col := range header {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "external_id":
			externalIDCol = i
		case "stripe_customer_id":
			stripeIDCol = i
		case "email":
			emailCol = i
		case "name":
			nameCol = i
		}
	}

	if externalIDCol == -1 || stripeIDCol == -1 {
		return nil, fmt.Errorf("CSV must have 'external_id' and 'stripe_customer_id' columns")
	}

	mappings := make([]CreateCustomerMappingRequest, 0, len(records)-1)

	for i, row := range records[1:] {
		if len(row) <= externalIDCol || len(row) <= stripeIDCol {
			return nil, fmt.Errorf("row %d: insufficient columns", i+2)
		}

		externalID := strings.TrimSpace(row[externalIDCol])
		stripeID := strings.TrimSpace(row[stripeIDCol])

		if externalID == "" || stripeID == "" {
			return nil, fmt.Errorf("row %d: external_id and stripe_customer_id cannot be empty", i+2)
		}

		mapping := CreateCustomerMappingRequest{
			ExternalID:       externalID,
			StripeCustomerID: stripeID,
			Metadata:         make(map[string]string),
		}

		if emailCol != -1 && len(row) > emailCol {
			mapping.Email = strings.TrimSpace(row[emailCol])
		}

		if nameCol != -1 && len(row) > nameCol {
			mapping.Name = strings.TrimSpace(row[nameCol])
		}

		// Store additional columns as metadata
		for j, value := range row {
			if j != externalIDCol && j != stripeIDCol && j != emailCol && j != nameCol {
				if j < len(header) && value != "" {
					mapping.Metadata[header[j]] = strings.TrimSpace(value)
				}
			}
		}

		mappings = append(mappings, mapping)
	}

	return mappings, nil
}
