package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// DLQHandler exposes operator actions for dead-letter queues.
type DLQHandler struct {
	dlqReplayService service.DLQReplayService
	log              *logger.Logger
}

// NewDLQHandler creates a new DLQHandler.
func NewDLQHandler(dlqReplayService service.DLQReplayService, log *logger.Logger) *DLQHandler {
	return &DLQHandler{
		dlqReplayService: dlqReplayService,
		log:              log,
	}
}

// ReplayDLQ godoc
// @Summary Replay a dead-letter topic
// @Description Starts a Temporal workflow that drains a dead-letter (poison-queue) topic back to the origin topic each message was poisoned from. Run with dry_run=true first to preview routing.
// @Tags Admin
// @Accept json
// @Produce json
// @Param request body dto.ReplayDLQRequest true "DLQ replay request"
// @Success 200 {object} models.TemporalWorkflowResult
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Security ApiKeyAuth
// @Router /admin/dlq/replay [post]
func (h *DLQHandler) ReplayDLQ(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.ReplayDLQRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error(ctx, "Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		h.log.Error(ctx, "Failed to validate request", "error", err)
		c.Error(err)
		return
	}

	result, err := h.dlqReplayService.TriggerReplayDLQWorkflow(ctx, &service.ReplayDLQRequest{
		SourceTopic:    req.SourceTopic,
		TargetOverride: req.TargetOverride,
		Group:          req.Group,
		Since:          req.Since,
		FromStart:      req.FromStart,
		Max:            req.Max,
		MaxReplays:     req.MaxReplays,
		DryRun:         req.DryRun,
	})
	if err != nil {
		h.log.Error(ctx, "Failed to trigger DLQ replay workflow", "error", err, "source_topic", req.SourceTopic)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, result)
}
