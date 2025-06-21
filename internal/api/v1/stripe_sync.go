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

// GetSyncStatus GET /stripe/sync/status
func (h *StripeSyncHandler) GetSyncStatus(c *gin.Context) {
	ctx := c.Request.Context()
	status, err := h.svc.GetSyncStatus(ctx, nil)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, status)
}

// ListBatches GET /stripe/sync/batches
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

// GetBatch GET /stripe/sync/batches/:id
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

// ManualSync POST /stripe/sync/manual
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

// RetryBatches POST /stripe/batches/retry
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
