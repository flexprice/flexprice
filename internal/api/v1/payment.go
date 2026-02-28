package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type PaymentHandler struct {
	service   service.PaymentService
	processor service.PaymentProcessorService
	log       *logger.Logger
}

func NewPaymentHandler(service service.PaymentService, processor service.PaymentProcessorService, log *logger.Logger) *PaymentHandler {
	return &PaymentHandler{service: service, processor: processor, log: log}
}

// @Summary Create payment
// @ID createPayment
// @Description Use when recording a payment against an invoice (e.g. after receiving funds via a gateway or manual entry).
// @Tags Payments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param payment body dto.CreatePaymentRequest true "Payment configuration"
// @Success 201 {object} dto.PaymentResponse "Created payment"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /payments [post]
func (h *PaymentHandler) CreatePayment(c *gin.Context) {
	var req dto.CreatePaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreatePayment(c.Request.Context(), &req)
	if err != nil {
		h.log.Error("Failed to create payment", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get payment
// @ID getPayment
// @Description Use when you need to load a single payment (e.g. for a receipt view or reconciliation).
// @Tags Payments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Payment ID"
// @Success 200 {object} dto.PaymentResponse "Payment details"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Payment not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /payments/{id} [get]
func (h *PaymentHandler) GetPayment(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Payment ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetPayment(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get payment", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update payment
// @ID updatePayment
// @Description Use when updating payment status or metadata (e.g. after reconciliation or adding a reference).
// @Tags Payments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Payment ID"
// @Param payment body dto.UpdatePaymentRequest true "Payment configuration"
// @Success 200 {object} dto.PaymentResponse "Updated payment"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /payments/{id} [put]
func (h *PaymentHandler) UpdatePayment(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Payment ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdatePaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdatePayment(c.Request.Context(), id, req)
	if err != nil {
		h.log.Error("Failed to update payment", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List payments
// @ID listPayments
// @Description Use when listing or searching payments (e.g. reconciliation UI or customer payment history). Returns a paginated list; supports filtering by customer, invoice, status.
// @Tags Payments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.PaymentFilter true "Filter"
// @Success 200 {object} dto.ListPaymentsResponse "Paginated payments"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /payments [get]
func (h *PaymentHandler) ListPayments(c *gin.Context) {
	var filter types.PaymentFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.log.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.ListPayments(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error("Failed to list payments", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete payment
// @ID deletePayment
// @Description Use when removing or voiding a payment record (e.g. correcting erroneous entries). Returns 200 with success message.
// @Tags Payments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Payment ID"
// @Success 200 {object} dto.SuccessResponse "Payment deleted"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Payment not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /payments/{id} [delete]
func (h *PaymentHandler) DeletePayment(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Payment ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	if err := h.service.DeletePayment(c.Request.Context(), id); err != nil {
		h.log.Error("Failed to delete payment", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "payment deleted successfully"})
}

// @Summary Process payment
// @ID processPayment
// @Description Use when you need to charge or process a payment (e.g. trigger the payment provider to capture funds). Returns updated payment with status.
// @Tags Payments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Payment ID"
// @Success 200 {object} dto.PaymentResponse "Processed payment"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Payment not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /payments/{id}/process [post]
func (h *PaymentHandler) ProcessPayment(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("id is required").
			WithHint("Payment ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	p, err := h.processor.ProcessPayment(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to process payment", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.NewPaymentResponse(p))
}
