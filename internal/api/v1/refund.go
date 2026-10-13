package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type RefundHandler struct {
	refundService service.RefundService
	logger        *logger.Logger
}

func NewRefundHandler(refundService service.RefundService, logger *logger.Logger) *RefundHandler {
	return &RefundHandler{
		refundService: refundService,
		logger:        logger,
	}
}

// @Summary List refunds
// @ID listRefunds
// @Description Use to see where refunded money actually went and whether it has settled. Filter by invoice_ids to get every settlement row for one invoice.
// @Tags Refunds
// @Produce json
// @Security ApiKeyAuth
// @Param invoice_ids query []string false "Filter by invoice IDs"
// @Param payment_ids query []string false "Filter by payment IDs"
// @Param credit_note_ids query []string false "Filter by credit note IDs"
// @Param refund_statuses query []string false "Filter by refund status"
// @Param refund_destinations query []string false "Filter by refund destination"
// @Param gateway query string false "Filter by payment gateway"
// @Param only_settled query boolean false "Only refunds that have settled"
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @x-scope "read"
// @Success 200 {object} dto.ListRefundsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /refunds [get]
func (h *RefundHandler) ListRefunds(c *gin.Context) {
	var filter types.RefundFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.refundService.ListRefunds(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Get refund
// @ID getRefund
// @Description Use to inspect a single refund: its destination, settled amount and failure reason.
// @Tags Refunds
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Refund ID"
// @x-scope "read"
// @Success 200 {object} dto.RefundResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /refunds/{id} [get]
func (h *RefundHandler) GetRefund(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("refund ID is required").
			WithHint("Please provide a refund ID").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.refundService.GetRefund(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Retry refund
// @ID retryRefund
// @Description Use when a refund failed or is stuck pending. A failed gateway refund is retried into the customer's wallet; an already-settled refund is rejected.
// @Tags Refunds
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Refund ID"
// @x-scope "write"
// @Success 200 {object} dto.RefundResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /refunds/{id}/retry [post]
func (h *RefundHandler) RetryRefund(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("refund ID is required").
			WithHint("Please provide a refund ID").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.refundService.RetryRefund(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
