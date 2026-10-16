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
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	PrevFrom    string `json:"prev_from,omitempty"`
	PrevTo      string `json:"prev_to,omitempty"`
}

// AnalyticsQueryResult is the visualization-agnostic shape every analytics
// query renders into — the response body for POST /analytics/query and
// POST /analytics/views/{id}/query.
type AnalyticsQueryResult struct {
	Columns []*AnalyticsColumn `json:"columns"`
	Rows    [][]string         `json:"rows"`
	Meta    AnalyticsQueryMeta `json:"meta"`
}

type ReportDimension struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type ReportCatalogEntry struct {
	Key             string            `json:"key"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Category        string            `json:"category" enums:"billing,receivables,customers,credits,usage"`
	Kind            string            `json:"kind" enums:"flow,ratio,snapshot"`
	ValueType       string            `json:"value_type" enums:"money,count,days"`
	HigherIsBetter  bool              `json:"higher_is_better"`
	Shapes          []string          `json:"shapes" enums:"timeseries,breakdown,scalar"`
	DefaultShape    string            `json:"default_shape" enums:"timeseries,breakdown,scalar"`
	Dimensions      []ReportDimension `json:"dimensions"`
	Grains          []string          `json:"grains" enums:"day,week,month"`
	SupportsCompare bool              `json:"supports_compare"`
}

type ReportCatalogResponse struct {
	Items []*ReportCatalogEntry `json:"items"`
}

type ReportFilter struct {
	Field string   `json:"field"`
	Op    string   `json:"op" enums:"eq,in"`
	Value []string `json:"value"`
}

type ReportSort struct {
	Field string `json:"field"`
	Dir   string `json:"dir" enums:"asc,desc"`
}

// ReportQueryRequest is a report query body.
type ReportQueryRequest struct {
	Shape      string         `json:"shape" validate:"required" enums:"timeseries,breakdown,scalar"`
	Dimensions []string       `json:"dimensions,omitempty"`
	Range      string         `json:"range" validate:"required" example:"2026-09-01..2026-10-01"`
	Grain      string         `json:"grain,omitempty" enums:"day,week,month"`
	Currency   string         `json:"currency,omitempty"`
	Filters    []ReportFilter `json:"filters,omitempty"`
	Compare    string         `json:"compare,omitempty" enums:"previous_period,previous_year"`
	Limit      int            `json:"limit,omitempty"`
	Sort       []ReportSort   `json:"sort,omitempty"`
}

func (r *ReportQueryRequest) Validate() error {
	return validator.ValidateRequest(r)
}
