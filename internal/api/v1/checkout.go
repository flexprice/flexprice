package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// CheckoutHandler handles API requests for hosted checkout sessions.
type CheckoutHandler struct {
	service service.CheckoutService
	log     *logger.Logger
}

// NewCheckoutHandler creates a new checkout handler.
func NewCheckoutHandler(service service.CheckoutService, log *logger.Logger) *CheckoutHandler {
	return &CheckoutHandler{service: service, log: log}
}

// @Summary Create a checkout session
// @ID createCheckout
// @Description Use when opening a hosted checkout for a new subscription (payment objective in v1).
// @Tags Checkouts
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CreateCheckoutRequest true "Create checkout request"
// @Success 201 {object} dto.CheckoutResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /checkouts [post]
func (h *CheckoutHandler) CreateCheckout(c *gin.Context) {
	var req dto.CreateCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid checkout request").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get a checkout session
// @ID getCheckout
// @Description Use when retrieving the current status and hosted URL of a checkout session.
// @Tags Checkouts
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Checkout ID"
// @Success 200 {object} dto.CheckoutResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /checkouts/{id} [get]
func (h *CheckoutHandler) GetCheckout(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("checkout id is required").
			WithHint("Provide a checkout id").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
