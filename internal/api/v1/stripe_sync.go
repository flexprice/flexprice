package v1

import (
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/integration"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// StripeSyncHandler exposes monitoring & manual ops endpoints for Stripe sync
type StripeSyncHandler struct {
	svc    service.StripeIntegrationService
	logger *logger.Logger
}

func NewStripeSyncHandler(svc service.StripeIntegrationService, log *logger.Logger) *StripeSyncHandler {
	return &StripeSyncHandler{svc: svc, logger: log}
}

// GetSyncStatus godoc
// @Summary Get Stripe sync status
// @Description Get the current status and metrics of Stripe sync batches
// @Tags stripe-sync
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} service.StripeSyncStatusResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /stripe/sync/status [get]
func (h *StripeSyncHandler) GetSyncStatus(c *gin.Context) {
	ctx := c.Request.Context()
	status, err := h.svc.GetSyncStatus(ctx, nil)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, status)
}

// ListBatches godoc
// @Summary List Stripe sync batches
// @Description List all Stripe sync batches with optional filters
// @Tags stripe-sync
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} service.ListStripeSyncBatchesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /stripe/sync/batches [get]
func (h *StripeSyncHandler) ListBatches(c *gin.Context) {
	ctx := c.Request.Context()
	// For now, no filters parsed
	resp, err := h.svc.GetSyncBatches(ctx, nil)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetBatch godoc
// @Summary Get Stripe sync batch by ID
// @Description Get details of a specific Stripe sync batch
// @Tags stripe-sync
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Batch ID"
// @Success 200 {object} service.StripeSyncBatchResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /stripe/sync/batches/{id} [get]
func (h *StripeSyncHandler) GetBatch(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()
	resp, err := h.svc.GetSyncBatch(ctx, id)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ManualSync godoc
// @Summary Trigger manual Stripe sync
// @Description Manually trigger a Stripe sync for a specific entity and time range
// @Tags stripe-sync
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.ManualSyncRequest true "Manual sync request"
// @Success 202 {object} service.ManualSyncResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /stripe/sync/manual [post]
func (h *StripeSyncHandler) ManualSync(c *gin.Context) {
	var req dto.ManualSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid payload").Mark(ierr.ErrValidation))
		return
	}
	ctx := c.Request.Context()
	svcReq := service.TriggerManualSyncRequest{
		EntityID:   req.EntityID,
		EntityType: integration.EntityType(req.EntityType),
		MeterID:    req.MeterID,
		TimeFrom:   req.TimeFrom,
		TimeTo:     req.TimeTo,
		ForceRerun: req.ForceRerun,
	}
	resp, err := h.svc.TriggerManualSync(ctx, svcReq)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusAccepted, resp)
}

// RetryBatches godoc
// @Summary Retry failed Stripe sync batches
// @Description Retry failed Stripe sync batches by batch IDs, entity, or meter
// @Tags stripe-sync
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.RetryFailedBatchesRequest true "Retry failed batches request"
// @Success 200 {object} service.RetryFailedBatchesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /stripe/batches/retry [post]
func (h *StripeSyncHandler) RetryBatches(c *gin.Context) {
	var req dto.RetryFailedBatchesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid payload").Mark(ierr.ErrValidation))
		return
	}
	var durPtr *time.Duration
	if req.MaxRetryAge != "" {
		if d, err := time.ParseDuration(req.MaxRetryAge); err == nil {
			durPtr = &d
		}
	}
	ctx := c.Request.Context()
	svcReq := service.RetryFailedBatchesRequest{
		BatchIDs:    req.BatchIDs,
		MaxRetryAge: durPtr,
		EntityID:    req.EntityID,
		MeterID:     req.MeterID,
	}
	resp, err := h.svc.RetryFailedBatches(ctx, svcReq)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
