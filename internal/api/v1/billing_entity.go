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

type BillingEntityHandler struct {
	service service.BillingEntityService
	logger  *logger.Logger
}

func NewBillingEntityHandler(service service.BillingEntityService, logger *logger.Logger) *BillingEntityHandler {
	return &BillingEntityHandler{service: service, logger: logger}
}

// @Summary Create a billing entity
// @ID createBillingEntity
// @Description Create a legal entity that bills customers in this environment. Requires billing entities to be enabled.
// @Tags Billing Entities
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param billing_entity body dto.CreateBillingEntityRequest true "Billing entity to create"
// @Success 201 {object} dto.BillingEntityResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request or billing entities not enabled"
// @Failure 409 {object} ierr.ErrorResponse "Invoice prefix already in use"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /billing-entities [post]
func (h *BillingEntityHandler) CreateBillingEntity(c *gin.Context) {
	var req dto.CreateBillingEntityRequest
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

// @Summary Get a billing entity
// @ID getBillingEntity
// @Description Load a single billing entity by ID.
// @Tags Billing Entities
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Billing entity ID"
// @Success 200 {object} dto.BillingEntityResponse
// @Failure 400 {object} ierr.ErrorResponse "Billing entities not enabled"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /billing-entities/{id} [get]
func (h *BillingEntityHandler) GetBillingEntity(c *gin.Context) {
	resp, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary List billing entities
// @ID listBillingEntities
// @Description List the billing entities of this environment.
// @Tags Billing Entities
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.BillingEntityFilter false "Filter"
// @Success 200 {object} dto.ListBillingEntitiesResponse
// @Failure 400 {object} ierr.ErrorResponse "Billing entities not enabled"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /billing-entities [get]
func (h *BillingEntityHandler) ListBillingEntities(c *gin.Context) {
	filter := types.NewBillingEntityFilter()
	if err := c.ShouldBindQuery(filter); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid filter parameters").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Update a billing entity
// @ID updateBillingEntity
// @Description Edit a billing entity. Drafts and future invoices use the new details; finalized invoices keep theirs.
// @Tags Billing Entities
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Billing entity ID"
// @Param billing_entity body dto.UpdateBillingEntityRequest true "Fields to change"
// @Success 200 {object} dto.BillingEntityResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request or billing entities not enabled"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 409 {object} ierr.ErrorResponse "Invoice prefix already in use"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /billing-entities/{id} [put]
func (h *BillingEntityHandler) UpdateBillingEntity(c *gin.Context) {
	var req dto.UpdateBillingEntityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
