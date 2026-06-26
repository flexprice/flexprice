package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

type CheckoutSessionHandler struct {
	service service.CheckoutSessionService
	log     *logger.Logger
}

func NewCheckoutSessionHandler(svc service.CheckoutSessionService, log *logger.Logger) *CheckoutSessionHandler {
	return &CheckoutSessionHandler{service: svc, log: log}
}

// Create godoc
// @Summary Create checkout session
// @ID createCheckoutSession
// @Tags Checkout
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param session body dto.CreateCheckoutSessionRequest true "Checkout session to create"
// @Success 201 {object} dto.CheckoutSessionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /checkout/sessions [post]
func (h *CheckoutSessionHandler) Create(c *gin.Context) {
	var req dto.CreateCheckoutSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, resp)
}

// Get godoc
// @Summary Get checkout session
// @ID getCheckoutSession
// @Tags Checkout
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Success 200 {object} dto.CheckoutSessionResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /checkout/sessions/{id} [get]
func (h *CheckoutSessionHandler) Get(c *gin.Context) {
	id := c.Param("id")
	resp, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Delete godoc
// @Summary Delete checkout session
// @ID deleteCheckoutSession
// @Tags Checkout
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Success 204
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /checkout/sessions/{id} [delete]
func (h *CheckoutSessionHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
