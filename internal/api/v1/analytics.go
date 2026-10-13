package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// AnalyticsHandler handles ad-hoc and view analytics query endpoints.
type AnalyticsHandler struct {
	svc        service.AnalyticsService
	revenueSvc interfaces.RevenueService
	log        *logger.Logger
}

func NewAnalyticsHandler(svc service.AnalyticsService, revenueSvc interfaces.RevenueService, log *logger.Logger) *AnalyticsHandler {
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

// GetRevenueAnalytics aggregates revenue facts into grouped, time-bucketed rows.
// @Summary Query revenue analytics
// @Description Aggregates revenue_facts by the requested dimensions at day/period/total granularity. allocation_policy places whole-period charges on their booked day (billed) or spreads them across the period (amortized); include_adjustments breaks out true-up/overage/revert amounts as labeled rows. Requires the tenant's revenue analytics setting.
// @ID getRevenueAnalytics
// @Tags Analytics
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.RevenueAnalyticsRequest true "Revenue analytics request"
// @Success 200 {object} dto.RevenueAnalyticsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 403 {object} ierr.ErrorResponse "Revenue analytics not enabled"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @x-scope "read"
// @Router /analytics/revenue [post]
func (h *AnalyticsHandler) GetRevenueAnalytics(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.RevenueAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	res, err := h.revenueSvc.GetRevenueAnalytics(ctx, &req)
	if err != nil {
		h.log.Error(ctx, "failed to query revenue analytics", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, res)
}
