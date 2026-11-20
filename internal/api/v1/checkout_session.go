package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
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
// @Param expand query string false "Comma-separated fields to expand: invoice, payment"
// @Success 200 {object} dto.CheckoutSessionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /checkout/sessions/{id} [get]
func (h *CheckoutSessionHandler) Get(c *gin.Context) {
	id := c.Param("id")
	expand := types.NewExpand(c.Query("expand"))
	if err := expand.Validate(types.CheckoutSessionExpandConfig); err != nil {
		c.Error(err)
		return
	}
	resp, err := h.service.GetAndReconcile(c.Request.Context(), id, expand)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Query godoc
// @Summary Query checkout sessions
// @ID queryCheckoutSession
// @Description Use when listing checkout sessions, e.g. a customer's sessions with their invoice, payment and attempts. Set expand to "invoice,payment".
// @Tags Checkout
// @x-scope "read"
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.CheckoutSessionFilter true "Filter"
// @Success 200 {object} dto.ListCheckoutSessionsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /checkout/sessions/search [post]
func (h *CheckoutSessionHandler) Query(c *gin.Context) {
	var filter types.CheckoutSessionFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}
	if err := filter.GetExpand().Validate(types.CheckoutSessionExpandConfig); err != nil {
		c.Error(err)
		return
	}

	resp, err := h.service.List(c.Request.Context(), &filter)
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

// Cancel godoc
// @Summary Cancel checkout session
// @ID cancelCheckoutSession
// @Tags Checkout
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Success 200 {object} dto.CheckoutSessionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /checkout/sessions/{id}/cancel [post]
func (h *CheckoutSessionHandler) Cancel(c *gin.Context) {
	id := c.Param("id")
	resp, err := h.service.Cancel(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
