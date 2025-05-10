# Guide to Creating a New Service in FlexPrice

This guide outlines the steps needed to create a new service in the FlexPrice codebase. We'll use the example of creating a "Report" service for generating billing reports.

## 1. Create Domain Models

First, create the domain model and repository interface:

**File: `/internal/domain/report/model.go`**
```go
package report

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/yourusername/flexprice/internal/domain/errors"
)

// Report represents a generated report in the system
type Report struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	EnvironmentID string    `json:"environment_id"`
	Name         string     `json:"name"`
	Type         ReportType `json:"type"`
	Status       ReportStatus `json:"status"`
	StartDate    time.Time  `json:"start_date"`
	EndDate      time.Time  `json:"end_date"`
	FileURL      string     `json:"file_url,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	CreatedBy    string     `json:"created_by"`
	UpdatedAt    time.Time  `json:"updated_at"`
	UpdatedBy    string     `json:"updated_by"`
}

// ReportType represents the type of report
type ReportType string

const (
	ReportTypeRevenue  ReportType = "revenue"
	ReportTypeUsage    ReportType = "usage"
	ReportTypeCustomer ReportType = "customer"
)

// ReportStatus represents the status of report generation
type ReportStatus string

const (
	ReportStatusPending   ReportStatus = "pending"
	ReportStatusProcessing ReportStatus = "processing"
	ReportStatusCompleted ReportStatus = "completed"
	ReportStatusFailed    ReportStatus = "failed"
)

// NewReport creates a new report
func NewReport(tenantID, environmentID, createdBy string, name string, reportType ReportType, startDate, endDate time.Time) (*Report, error) {
	if tenantID == "" {
		return nil, errors.ErrInvalidTenantID
	}

	if environmentID == "" {
		return nil, errors.ErrInvalidEnvironmentID
	}
	
	if name == "" {
		return nil, errors.New("report name is required")
	}

	if reportType == "" {
		return nil, errors.New("report type is required")
	}

	if startDate.IsZero() || endDate.IsZero() {
		return nil, errors.New("start date and end date are required")
	}

	if endDate.Before(startDate) {
		return nil, errors.New("end date cannot be before start date")
	}

	now := time.Now().UTC()
	return &Report{
		ID:           uuid.New().String(),
		TenantID:     tenantID,
		EnvironmentID: environmentID,
		Name:         name,
		Type:         reportType,
		Status:       ReportStatusPending,
		StartDate:    startDate,
		EndDate:      endDate,
		Metadata:     map[string]interface{}{},
		CreatedAt:    now,
		CreatedBy:    createdBy,
		UpdatedAt:    now,
		UpdatedBy:    createdBy,
	}, nil
}
```

**File: `/internal/domain/report/repository.go`**
```go
package report

import (
	"context"
)

// Repository defines the operations for report persistence
type Repository interface {
	Create(ctx context.Context, report *Report) error
	GetByID(ctx context.Context, id, tenantID, environmentID string) (*Report, error)
	Update(ctx context.Context, report *Report) error
	List(ctx context.Context, tenantID, environmentID string, filter ReportFilter) ([]*Report, error)
	Delete(ctx context.Context, id, tenantID, environmentID string) error
}

// ReportFilter defines filters for listing reports
type ReportFilter struct {
	Type     ReportType
	Status   ReportStatus
	StartDate *time.Time
	EndDate   *time.Time
	Limit    int
	Offset   int
}
```

## 2. Create Database Schema (Ent Schema)

**File: `/ent/schema/report.go`**
```go
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"entgo.io/ent/schema/mixin"
)

// Report holds the schema definition for the Report entity.
type Report struct {
	ent.Schema
}

// Fields of the Report.
func (Report) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Immutable().
			NotEmpty().
			Unique(),
		field.String("name").
			NotEmpty(),
		field.String("type").
			NotEmpty(),
		field.String("status").
			NotEmpty(),
		field.Time("start_date"),
		field.Time("end_date"),
		field.String("file_url").
			Optional(),
		field.JSON("metadata", map[string]interface{}{}).
			Optional(),
	}
}

// Edges of the Report.
func (Report) Edges() []ent.Edge {
	return nil
}

// Mixin of the Report.
func (Report) Mixin() []ent.Mixin {
	return []ent.Mixin{
		BaseMixin{},
		EnvironmentMixin{},
	}
}

// Indexes of the Report.
func (Report) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "id").
			Unique(),
		index.Fields("tenant_id", "environment_id", "type", "status"),
	}
}
```

## 3. Generate Ent Files

Run the following command to generate Ent files:

```bash
go generate ./ent
```

## 4. Create Repository Implementation

**File: `/internal/repository/ent/report.go`**
```go
package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/yourusername/flexprice/ent"
	"github.com/yourusername/flexprice/ent/report"
	"github.com/yourusername/flexprice/internal/domain/errors"
	reportDomain "github.com/yourusername/flexprice/internal/domain/report"
)

type reportRepository struct {
	client *ent.Client
}

// NewReportRepository creates a new report repository
func NewReportRepository(client *ent.Client) reportDomain.Repository {
	return &reportRepository{
		client: client,
	}
}

// Create creates a new report
func (r *reportRepository) Create(ctx context.Context, report *reportDomain.Report) error {
	_, err := r.client.Report.Create().
		SetID(report.ID).
		SetTenantID(report.TenantID).
		SetEnvironmentID(report.EnvironmentID).
		SetName(report.Name).
		SetType(string(report.Type)).
		SetStatus(string(report.Status)).
		SetStartDate(report.StartDate).
		SetEndDate(report.EndDate).
		SetFileURL(report.FileURL).
		SetMetadata(report.Metadata).
		SetCreatedAt(report.CreatedAt).
		SetCreatedBy(report.CreatedBy).
		SetUpdatedAt(report.UpdatedAt).
		SetUpdatedBy(report.UpdatedBy).
		Save(ctx)

	return err
}

// GetByID retrieves a report by ID
func (r *reportRepository) GetByID(ctx context.Context, id, tenantID, environmentID string) (*reportDomain.Report, error) {
	result, err := r.client.Report.Query().
		Where(
			report.ID(id),
			report.TenantID(tenantID),
			report.EnvironmentID(environmentID),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.ErrReportNotFound
		}
		return nil, err
	}

	return entToDomainReport(result), nil
}

// Update updates a report
func (r *reportRepository) Update(ctx context.Context, report *reportDomain.Report) error {
	_, err := r.client.Report.UpdateOneID(report.ID).
		SetName(report.Name).
		SetStatus(string(report.Status)).
		SetFileURL(report.FileURL).
		SetMetadata(report.Metadata).
		SetUpdatedAt(report.UpdatedAt).
		SetUpdatedBy(report.UpdatedBy).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return errors.ErrReportNotFound
		}
		return err
	}

	return nil
}

// List retrieves reports based on filters
func (r *reportRepository) List(ctx context.Context, tenantID, environmentID string, filter reportDomain.ReportFilter) ([]*reportDomain.Report, error) {
	query := r.client.Report.Query().
		Where(
			report.TenantID(tenantID),
			report.EnvironmentID(environmentID),
		)

	if filter.Type != "" {
		query = query.Where(report.Type(string(filter.Type)))
	}

	if filter.Status != "" {
		query = query.Where(report.Status(string(filter.Status)))
	}

	if filter.StartDate != nil {
		query = query.Where(report.StartDateGTE(*filter.StartDate))
	}

	if filter.EndDate != nil {
		query = query.Where(report.EndDateLTE(*filter.EndDate))
	}

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	results, err := query.Order(ent.Desc(report.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, err
	}

	reports := make([]*reportDomain.Report, len(results))
	for i, r := range results {
		reports[i] = entToDomainReport(r)
	}

	return reports, nil
}

// Delete deletes a report
func (r *reportRepository) Delete(ctx context.Context, id, tenantID, environmentID string) error {
	_, err := r.client.Report.Delete().
		Where(
			report.ID(id),
			report.TenantID(tenantID),
			report.EnvironmentID(environmentID),
		).
		Exec(ctx)

	if err != nil {
		return err
	}

	return nil
}

// entToDomainReport converts an ent report to a domain report
func entToDomainReport(e *ent.Report) *reportDomain.Report {
	return &reportDomain.Report{
		ID:           e.ID,
		TenantID:     e.TenantID,
		EnvironmentID: e.EnvironmentID,
		Name:         e.Name,
		Type:         reportDomain.ReportType(e.Type),
		Status:       reportDomain.ReportStatus(e.Status),
		StartDate:    e.StartDate,
		EndDate:      e.EndDate,
		FileURL:      e.FileURL,
		Metadata:     e.Metadata,
		CreatedAt:    e.CreatedAt,
		CreatedBy:    e.CreatedBy,
		UpdatedAt:    e.UpdatedAt,
		UpdatedBy:    e.UpdatedBy,
	}
}
```

## 5. Update Repository Factory

**File: `/internal/repository/factory.go`**
```go
// Add to imports
reportDomain "github.com/yourusername/flexprice/internal/domain/report"

// Add to Factory struct
ReportRepository reportDomain.Repository

// Add to NewFactory function
func NewFactory(client *ent.Client, clickhouseClient *clickhouse.Client) *Factory {
	// ...existing code...
	
	// Add this line:
	reportRepository := ent.NewReportRepository(client)
	
	return &Factory{
		// ...existing fields...
		
		// Add this line:
		ReportRepository: reportRepository,
	}
}
```

## 6. Create Service Layer

**File: `/internal/service/report.go`**
```go
package service

import (
	"context"
	"time"

	"github.com/yourusername/flexprice/internal/domain/errors"
	reportDomain "github.com/yourusername/flexprice/internal/domain/report"
)

// ReportService provides methods to work with reports
type ReportService interface {
	CreateReport(ctx context.Context, tenantID, environmentID, userID string, name string, reportType reportDomain.ReportType, startDate, endDate time.Time) (*reportDomain.Report, error)
	GetReportByID(ctx context.Context, id, tenantID, environmentID string) (*reportDomain.Report, error)
	UpdateReportStatus(ctx context.Context, id, tenantID, environmentID, userID string, status reportDomain.ReportStatus, fileURL string) (*reportDomain.Report, error)
	ListReports(ctx context.Context, tenantID, environmentID string, filter reportDomain.ReportFilter) ([]*reportDomain.Report, error)
	DeleteReport(ctx context.Context, id, tenantID, environmentID string) error
}

type reportService struct {
	reportRepository reportDomain.Repository
}

// NewReportService creates a new report service
func NewReportService(reportRepository reportDomain.Repository) ReportService {
	return &reportService{
		reportRepository: reportRepository,
	}
}

// CreateReport creates a new report
func (s *reportService) CreateReport(ctx context.Context, tenantID, environmentID, userID string, name string, reportType reportDomain.ReportType, startDate, endDate time.Time) (*reportDomain.Report, error) {
	report, err := reportDomain.NewReport(tenantID, environmentID, userID, name, reportType, startDate, endDate)
	if err != nil {
		return nil, err
	}

	err = s.reportRepository.Create(ctx, report)
	if err != nil {
		return nil, err
	}

	return report, nil
}

// GetReportByID retrieves a report by ID
func (s *reportService) GetReportByID(ctx context.Context, id, tenantID, environmentID string) (*reportDomain.Report, error) {
	return s.reportRepository.GetByID(ctx, id, tenantID, environmentID)
}

// UpdateReportStatus updates a report's status and file URL
func (s *reportService) UpdateReportStatus(ctx context.Context, id, tenantID, environmentID, userID string, status reportDomain.ReportStatus, fileURL string) (*reportDomain.Report, error) {
	report, err := s.reportRepository.GetByID(ctx, id, tenantID, environmentID)
	if err != nil {
		return nil, err
	}

	report.Status = status
	report.FileURL = fileURL
	report.UpdatedAt = time.Now().UTC()
	report.UpdatedBy = userID

	err = s.reportRepository.Update(ctx, report)
	if err != nil {
		return nil, err
	}

	return report, nil
}

// ListReports retrieves reports based on filters
func (s *reportService) ListReports(ctx context.Context, tenantID, environmentID string, filter reportDomain.ReportFilter) ([]*reportDomain.Report, error) {
	return s.reportRepository.List(ctx, tenantID, environmentID, filter)
}

// DeleteReport deletes a report
func (s *reportService) DeleteReport(ctx context.Context, id, tenantID, environmentID string) error {
	return s.reportRepository.Delete(ctx, id, tenantID, environmentID)
}
```

**File: `/internal/service/report_test.go`**
```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	reportDomain "github.com/yourusername/flexprice/internal/domain/report"
	"github.com/yourusername/flexprice/internal/domain/errors"
)

// MockReportRepository is a mock implementation of report.Repository
type MockReportRepository struct {
	mock.Mock
}

func (m *MockReportRepository) Create(ctx context.Context, report *reportDomain.Report) error {
	args := m.Called(ctx, report)
	return args.Error(0)
}

func (m *MockReportRepository) GetByID(ctx context.Context, id, tenantID, environmentID string) (*reportDomain.Report, error) {
	args := m.Called(ctx, id, tenantID, environmentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*reportDomain.Report), args.Error(1)
}

func (m *MockReportRepository) Update(ctx context.Context, report *reportDomain.Report) error {
	args := m.Called(ctx, report)
	return args.Error(0)
}

func (m *MockReportRepository) List(ctx context.Context, tenantID, environmentID string, filter reportDomain.ReportFilter) ([]*reportDomain.Report, error) {
	args := m.Called(ctx, tenantID, environmentID, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*reportDomain.Report), args.Error(1)
}

func (m *MockReportRepository) Delete(ctx context.Context, id, tenantID, environmentID string) error {
	args := m.Called(ctx, id, tenantID, environmentID)
	return args.Error(0)
}

func TestCreateReport(t *testing.T) {
	mockRepo := &MockReportRepository{}
	service := NewReportService(mockRepo)
	ctx := context.Background()

	tenantID := "tenant-1"
	environmentID := "env-1"
	userID := "user-1"
	name := "Monthly Revenue Report"
	reportType := reportDomain.ReportTypeRevenue
	startDate := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2025, 5, 31, 0, 0, 0, 0, time.UTC)

	mockRepo.On("Create", ctx, mock.AnythingOfType("*report.Report")).Return(nil)

	report, err := service.CreateReport(ctx, tenantID, environmentID, userID, name, reportType, startDate, endDate)
	
	assert.NoError(t, err)
	assert.NotNil(t, report)
	assert.Equal(t, tenantID, report.TenantID)
	assert.Equal(t, environmentID, report.EnvironmentID)
	assert.Equal(t, name, report.Name)
	assert.Equal(t, reportType, report.Type)
	assert.Equal(t, reportDomain.ReportStatusPending, report.Status)
	assert.Equal(t, startDate, report.StartDate)
	assert.Equal(t, endDate, report.EndDate)
	assert.Equal(t, userID, report.CreatedBy)
	
	mockRepo.AssertExpectations(t)
}

// Add more tests for other service methods...
```

## 7. Update Service Factory

**File: `/internal/service/factory.go`**
```go
// Add to imports
reportDomain "github.com/yourusername/flexprice/internal/domain/report"

// Add to Provider struct
ReportService ReportService

// Add to NewProvider function
func NewProvider(
	// ... existing parameters ...
	reportRepository reportDomain.Repository,
	// ... existing parameters ...
) *Provider {
	// ... existing code ...
	
	// Add this line:
	reportService := NewReportService(reportRepository)
	
	return &Provider{
		// ... existing fields ...
		
		// Add this line:
		ReportService: reportService,
	}
}

// Update the fx.Provide in the Module function
var Module = fx.Options(
	fx.Provide(
		// ... existing providers ...
		fx.Annotate(
			NewProvider,
			fx.ParamTags(
				// ... existing tags ...
				"repository:report",
				// ... existing tags ...
			),
		),
	),
)
```

## 8. Create DTOs for API Layer

**File: `/internal/api/dto/report.go`**
```go
package dto

import (
	"time"

	reportDomain "github.com/yourusername/flexprice/internal/domain/report"
)

// ReportType represents the type of report
type ReportType string

const (
	ReportTypeRevenue  ReportType = "revenue"
	ReportTypeUsage    ReportType = "usage"
	ReportTypeCustomer ReportType = "customer"
)

// ReportStatus represents the status of report generation
type ReportStatus string

const (
	ReportStatusPending    ReportStatus = "pending"
	ReportStatusProcessing ReportStatus = "processing"
	ReportStatusCompleted  ReportStatus = "completed"
	ReportStatusFailed     ReportStatus = "failed"
)

// CreateReportRequest represents a request to create a new report
type CreateReportRequest struct {
	Name      string     `json:"name" binding:"required"`
	Type      ReportType `json:"type" binding:"required,oneof=revenue usage customer"`
	StartDate string     `json:"start_date" binding:"required,datetime=2006-01-02"`
	EndDate   string     `json:"end_date" binding:"required,datetime=2006-01-02"`
}

// ReportResponse represents a report in API responses
type ReportResponse struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Status       string                 `json:"status"`
	StartDate    string                 `json:"start_date"`
	EndDate      string                 `json:"end_date"`
	FileURL      string                 `json:"file_url,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	CreatedBy    string                 `json:"created_by"`
	UpdatedAt    string                 `json:"updated_at"`
	UpdatedBy    string                 `json:"updated_by"`
}

// UpdateReportStatusRequest represents a request to update a report's status
type UpdateReportStatusRequest struct {
	Status  ReportStatus `json:"status" binding:"required,oneof=pending processing completed failed"`
	FileURL string       `json:"file_url,omitempty"`
}

// ListReportsResponse represents a paginated list of reports
type ListReportsResponse struct {
	Items      []ReportResponse        `json:"items"`
	Pagination PaginationResponse `json:"pagination"`
}

// ListReportsParams represents query parameters for listing reports
type ListReportsParams struct {
	Type      string `form:"type" binding:"omitempty,oneof=revenue usage customer"`
	Status    string `form:"status" binding:"omitempty,oneof=pending processing completed failed"`
	StartDate string `form:"start_date" binding:"omitempty,datetime=2006-01-02"`
	EndDate   string `form:"end_date" binding:"omitempty,datetime=2006-01-02"`
	Limit     int    `form:"limit" binding:"omitempty,min=1,max=1000"`
	Offset    int    `form:"offset" binding:"omitempty,min=0"`
}

// ToReportFilter converts ListReportsParams to a domain ReportFilter
func (p *ListReportsParams) ToReportFilter() reportDomain.ReportFilter {
	var filter reportDomain.ReportFilter

	if p.Type != "" {
		filter.Type = reportDomain.ReportType(p.Type)
	}

	if p.Status != "" {
		filter.Status = reportDomain.ReportStatus(p.Status)
	}

	if p.StartDate != "" {
		startDate, _ := time.Parse("2006-01-02", p.StartDate)
		filter.StartDate = &startDate
	}

	if p.EndDate != "" {
		endDate, _ := time.Parse("2006-01-02", p.EndDate)
		filter.EndDate = &endDate
	}

	filter.Limit = p.Limit
	filter.Offset = p.Offset

	return filter
}

// DomainToResponse converts a domain Report to a ReportResponse
func DomainToResponse(report *reportDomain.Report) ReportResponse {
	return ReportResponse{
		ID:           report.ID,
		Name:         report.Name,
		Type:         string(report.Type),
		Status:       string(report.Status),
		StartDate:    report.StartDate.Format("2006-01-02"),
		EndDate:      report.EndDate.Format("2006-01-02"),
		FileURL:      report.FileURL,
		Metadata:     report.Metadata,
		CreatedAt:    report.CreatedAt.Format(time.RFC3339),
		CreatedBy:    report.CreatedBy,
		UpdatedAt:    report.UpdatedAt.Format(time.RFC3339),
		UpdatedBy:    report.UpdatedBy,
	}
}
```

## 9. Create API Handler

**File: `/internal/api/v1/report.go`**
```go
package v1

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yourusername/flexprice/internal/api/dto"
	"github.com/yourusername/flexprice/internal/domain/errors"
	reportDomain "github.com/yourusername/flexprice/internal/domain/report"
	"github.com/yourusername/flexprice/internal/service"
)

// ReportHandler handles HTTP requests for reports
type ReportHandler struct {
	reportService service.ReportService
}

// NewReportHandler creates a new report handler
func NewReportHandler(reportService service.ReportService) *ReportHandler {
	return &ReportHandler{
		reportService: reportService,
	}
}

// CreateReport godoc
// @Summary Create a report
// @Description Create a new report
// @Tags Reports
// @Accept json
// @Produce json
// @Param report body dto.CreateReportRequest true "Report creation request"
// @Success 201 {object} dto.ReportResponse
// @Failure 400 {object} errors.ErrorResponse
// @Failure 500 {object} errors.ErrorResponse
// @Router /reports [post]
// @Security ApiKeyAuth
func (h *ReportHandler) CreateReport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	environmentID := c.GetString("environment_id")
	userID := c.GetString("user_id")

	var req dto.CreateReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	startDate, _ := time.Parse("2006-01-02", req.StartDate)
	endDate, _ := time.Parse("2006-01-02", req.EndDate)

	report, err := h.reportService.CreateReport(
		c.Request.Context(),
		tenantID,
		environmentID,
		userID,
		req.Name,
		reportDomain.ReportType(req.Type),
		startDate,
		endDate,
	)

	if err != nil {
		handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.DomainToResponse(report))
}

