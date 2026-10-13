package dto

import (
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// AnalyticsQueryRequest is the request for POST /v1/analytics/query (ad-hoc, unsaved view).
// Every variable value is a string list; a scalar variable is a 1-element
// list, and a date_range variable is a 1-element range-string.
type AnalyticsQueryRequest struct {
	Definition *analytics.ViewDefinition `json:"definition" binding:"required" validate:"required"`
	Variables  map[string][]string       `json:"variables"`
}

func (r *AnalyticsQueryRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// CreateViewRequest is the request for POST /v1/analytics/views.
type CreateViewRequest struct {
	Name       string                    `json:"name" validate:"required"`
	Definition *analytics.ViewDefinition `json:"definition" binding:"required" validate:"required"`
}

func (r *CreateViewRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// ViewQueryRequest is the request for POST /v1/analytics/views/{id}/query.
type ViewQueryRequest struct {
	Variables map[string][]string `json:"variables"`
}

func (r *ViewQueryRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// ViewResponse is the response for POST /v1/analytics/views. It exposes
// only the fields a client needs, unlike the domain analytics.View which
// embeds types.BaseModel (tenant_id, status, created_by, timestamps, ...).
type ViewResponse struct {
	ID         string                    `json:"id"`
	Name       string                    `json:"name"`
	Version    int                       `json:"version"`
	Definition *analytics.ViewDefinition `json:"definition"`
}

// NewViewResponse maps a domain analytics.View onto its response DTO.
func NewViewResponse(v *analytics.View) *ViewResponse {
	if v == nil {
		return nil
	}
	return &ViewResponse{
		ID:         v.ID,
		Name:       v.Name,
		Version:    v.Version,
		Definition: v.Definition,
	}
}

// AnalyticsColumn describes one column of a shaped AnalyticsQueryResult.
type AnalyticsColumn struct {
	Name     string           `json:"name"`
	Type     types.ColumnType `json:"type"`
	Role     types.ColumnRole `json:"role"`
	Currency string           `json:"currency,omitempty"`
}

func NewAnalyticsColumn(name string, columnType types.ColumnType, role types.ColumnRole, currency string) *AnalyticsColumn {
	return &AnalyticsColumn{
		Name:     name,
		Type:     columnType,
		Role:     role,
		Currency: currency,
	}
}

// AnalyticsQueryMeta carries non-tabular metadata about a query result.
type AnalyticsQueryMeta struct {
	QuerySource string `json:"query_source"`
}

// AnalyticsQueryResult is the visualization-agnostic shape every analytics
// query renders into — the response body for POST /analytics/query and
// POST /analytics/views/{id}/query.
type AnalyticsQueryResult struct {
	Columns []*AnalyticsColumn `json:"columns"`
	Rows    [][]string         `json:"rows"`
	Meta    AnalyticsQueryMeta `json:"meta"`
}
