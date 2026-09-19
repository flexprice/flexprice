package v1

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// AnalyticsHandler handles ad-hoc and view analytics query endpoints.
type AnalyticsHandler struct {
	svc        service.AnalyticsService
	revenueSvc service.RevenueService
	log        *logger.Logger
}

func NewAnalyticsHandler(svc service.AnalyticsService, revenueSvc service.RevenueService, log *logger.Logger) *AnalyticsHandler {
	return &AnalyticsHandler{svc: svc, revenueSvc: revenueSvc, log: log}
}

// Query runs an ad-hoc analytics query against a view definition.
// @Summary Run an ad-hoc analytics query
// @Description Resolves the given view definition against the supplied variables and executes it.
// @ID queryAnalytics
// @Tags Analytics
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.AnalyticsQueryRequest true "Analytics query request"
// @Success 200 {object} dto.AnalyticsQueryResult
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /analytics/query [post]
func (h *AnalyticsHandler) Query(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.AnalyticsQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	res, err := h.svc.ExecuteView(ctx, req.Definition, req.Variables)
	if err != nil {
		h.log.Error(ctx, "failed to execute analytics query", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, res)
}

// CreateView creates a new analytics view.
// @Summary Create an analytics view
// @Description Persists a named view definition that can later be queried by ID.
// @ID createAnalyticsView
// @Tags Analytics
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CreateViewRequest true "View request"
// @Success 201 {object} dto.ViewResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /analytics/views [post]
func (h *AnalyticsHandler) CreateView(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.CreateViewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	v := &analytics.View{
		Name:       req.Name,
		Definition: req.Definition,
	}

	if err := h.svc.CreateView(ctx, v); err != nil {
		h.log.Error(ctx, "failed to create analytics view", "error", err, "view_id", v.ID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, dto.NewViewResponse(v))
}

// QueryView executes a previously created analytics view by ID.
// @Summary Query an analytics view
// @Description Resolves the view's definition against the supplied variables and executes it.
// @ID queryAnalyticsView
// @Tags Analytics
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "View ID"
// @Param request body dto.ViewQueryRequest true "View query request"
// @Success 200 {object} dto.AnalyticsQueryResult
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "View not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /analytics/views/{id}/query [post]
func (h *AnalyticsHandler) QueryView(c *gin.Context) {
	ctx := c.Request.Context()

	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("view id is required").
			WithHint("Provide a valid view id").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ViewQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	res, err := h.svc.QueryView(ctx, id, req.Variables)
	if err != nil {
		h.log.Error(ctx, "failed to query analytics view", "error", err, "view_id", id)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, res)
}

// revenueFactsCSVHeader is the export column order — keep in sync with
// writeRevenueFactCSVRow and docs/export/revenue-facts.md.
var revenueFactsCSVHeader = []string{
	"id", "customer_id", "subscription_id", "sub_line_item_id", "price_id", "meter_id",
	"aggregation_type", "revenue_source", "period_start", "period_end", "day",
	"usage_at_list_rate", "tier_delta", "entitlement_amount", "line_discount", "invoice_discount",
	"net_amount", "billable_qty", "entitlement_qty", "decomposition_mode", "currency",
	"status", "is_revert", "invoice_id", "invoice_line_item_id", "computed_at", "version",
}

// ExportRevenueFacts streams the tenant's revenue_facts as CSV.
// @Summary Export revenue facts
// @Description Streams revenue_facts rows as CSV, ordered by (computed_at, id). Pass since (RFC3339) to export only rows recomputed after a prior export's max computed_at; omit it for a full snapshot. Requires the tenant's revenue analytics setting to be enabled. Column reference: docs/export/revenue-facts.md.
// @ID exportRevenueFacts
// @Tags Analytics
// @Produce text/csv
// @Security ApiKeyAuth
// @Param since query string false "Only rows with computed_at strictly after this RFC3339 instant"
// @Success 200 {string} string "CSV stream"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 403 {object} ierr.ErrorResponse "Revenue analytics not enabled"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /analytics/revenue-facts/export [get]
func (h *AnalyticsHandler) ExportRevenueFacts(c *gin.Context) {
	ctx := c.Request.Context()

	var since time.Time
	if raw := c.Query("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.Error(ierr.WithError(err).
				WithHint("since must be an RFC3339 timestamp").
				Mark(ierr.ErrValidation))
			return
		}
		since = parsed
	}

	// Gate before any bytes are written so a denial is a clean error response.
	firstPage, err := h.revenueSvc.ExportFacts(ctx, since, "", 0)
	if err != nil {
		c.Error(err)
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="revenue_facts.csv"`)
	w := csv.NewWriter(c.Writer)
	if err := w.Write(revenueFactsCSVHeader); err != nil {
		h.log.Error(ctx, "revenue facts export write failed", "error", err)
		return
	}

	page := firstPage
	watermark, afterID := since, ""
	for {
		for _, f := range page {
			if err := w.Write(revenueFactCSVRow(f)); err != nil {
				h.log.Error(ctx, "revenue facts export write failed", "error", err)
				return
			}
			watermark, afterID = f.ComputedAt, f.ID
		}
		if len(page) == 0 {
			break
		}
		w.Flush()
		if page, err = h.revenueSvc.ExportFacts(ctx, watermark, afterID, 0); err != nil {
			// Body already streaming — log and truncate; the client re-runs
			// from its last watermark.
			h.log.Error(ctx, "revenue facts export page failed", "error", err)
			return
		}
	}
	w.Flush()
}

func revenueFactCSVRow(f *revenuefact.RevenueFact) []string {
	const day = "2006-01-02"
	return []string{
		f.ID, f.CustomerID, f.SubscriptionID,
		lo.FromPtr(f.SubLineItemID), lo.FromPtr(f.PriceID), lo.FromPtr(f.MeterID),
		string(lo.FromPtr(f.AggregationType)), string(f.RevenueSource),
		f.PeriodStart.Format(day), f.PeriodEnd.Format(day), f.Day.Format(day),
		f.UsageAtListRate.String(), f.TierDelta.String(), f.EntitlementAmount.String(),
		f.LineDiscount.String(), f.InvoiceDiscount.String(), f.NetAmount.String(),
		f.BillableQty.String(), f.EntitlementQty.String(),
		string(f.DecompositionMode), f.Currency, string(f.Status),
		strconv.FormatBool(f.IsRevert),
		lo.FromPtr(f.InvoiceID), lo.FromPtr(f.InvoiceLineItemID),
		f.ComputedAt.UTC().Format(time.RFC3339Nano),
		strconv.FormatInt(f.Version, 10),
	}
}
