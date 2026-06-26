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

// TestComplete godoc
// @Summary [TEST ONLY] Complete a checkout session manually
// @ID testCompleteCheckoutSession
// @Tags Checkout
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Param result body types.CheckoutProviderResult false "Provider result (optional)"
// @Success 200 {object} dto.CheckoutSessionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /checkout/sessions/{id}/complete [post]
// TODO: remove after testing
func (h *CheckoutSessionHandler) TestComplete(c *gin.Context) {
	id := c.Param("id")
	var providerResult types.CheckoutProviderResult
	// body is optional — ignore parse errors
	_ = c.ShouldBindJSON(&providerResult)
	if err := h.service.CompleteCheckoutSession(c.Request.Context(), id, &providerResult); err != nil {
		c.Error(err)
		return
	}
	resp, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// TestCleanup godoc
// @Summary [TEST ONLY] Cleanup (fail) a checkout session manually
// @ID testCleanupCheckoutSession
// @Tags Checkout
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Checkout session ID"
// @Success 200 {object} dto.CheckoutSessionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /checkout/sessions/{id}/cleanup [post]
// TODO: remove after testing
func (h *CheckoutSessionHandler) TestCleanup(c *gin.Context) {
	id := c.Param("id")
	resp, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
	if err := h.service.CleanupCheckoutSession(c.Request.Context(), resp.CheckoutSession, nil); err != nil {
		c.Error(err)
		return
	}
	updated, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

// Query godoc
// @Summary Search checkout sessions
// @ID queryCheckoutSessions
// @Tags Checkout
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
		c.Error(ierr.WithError(err).WithHint("Invalid filter parameters").Mark(ierr.ErrValidation))
		return
	}
	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}
	resp, err := h.service.List(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
