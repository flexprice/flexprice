package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type CheckoutSessionHandler struct {
	service service.CheckoutSessionService
	log     *logger.Logger
}

func NewCheckoutSessionHandler(svc service.CheckoutSessionService, log *logger.Logger) *CheckoutSessionHandler {
	return &CheckoutSessionHandler{service: svc, log: log}
}

// CreateCheckoutSession godoc
// @Summary Create checkout session
// @ID createCheckoutSession
// @Description Create a new checkout session to initiate a B2C payment flow.
// @Tags Checkout
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param session body dto.CreateCheckoutSessionRequest true "Checkout session to create"
// @Success 201 {object} dto.CheckoutSessionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 409 {object} ierr.ErrorResponse "Idempotency key conflict"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /checkout/sessions [post]
func (h *CheckoutSessionHandler) CreateCheckoutSession(c *gin.Context) {
	var req dto.CreateCheckoutSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateCheckoutSession(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// GetCheckoutSession godoc
// @Summary Get checkout session
// @ID getCheckoutSession
// @Description Retrieve a checkout session by ID.
// @Tags Checkout
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Success 200 {object} dto.CheckoutSessionResponse
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /checkout/sessions/{id} [get]
func (h *CheckoutSessionHandler) GetCheckoutSession(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("checkout session ID is required").
			WithHint("ID must be provided in the URL path").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetCheckoutSession(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// QueryCheckoutSessions godoc
// @Summary Search checkout sessions
// @ID queryCheckoutSessions
// @Description Use when listing or searching checkout sessions. Returns a paginated list; supports filtering by customer IDs, statuses, and other fields.
// @Tags Checkout
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.CheckoutSessionFilter true "Filter"
// @Success 200 {object} dto.ListCheckoutSessionsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid filter"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /checkout/sessions/search [post]
func (h *CheckoutSessionHandler) QueryCheckoutSessions(c *gin.Context) {
	var filter types.CheckoutSessionFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.ListCheckoutSessions(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
