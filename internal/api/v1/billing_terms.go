package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

// BillingTermsHandler serves collection rules and concrete charges for commitments.
type BillingTermsHandler interface {
	Get(*gin.Context)
	Preview(*gin.Context)
	Replace(*gin.Context)
	ListItems(*gin.Context)
	GetItem(*gin.Context)
	History(*gin.Context)
	Trigger(*gin.Context)
}

type billingTermsHandler struct {
	service service.CommitmentBillingTermsService
}

// NewBillingTermsHandler delegates scoped collection operations to the commitment service.
func NewBillingTermsHandler(service service.CommitmentBillingTermsService) BillingTermsHandler {
	return &billingTermsHandler{service: service}
}

// @Summary Get commitment billing terms
// @ID getCommitmentBillingTerms
// @Description Returns current collection rules, a bounded schedule page and lifetime collection summary.
// @Tags Commitments
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Commitment ID"
// @Success 200 {object} dto.BillingTermsStateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 503 {object} ierr.ErrorResponse
// @Router /commitments/{id}/billing-terms [get]
func (h *billingTermsHandler) Get(c *gin.Context) {
	response, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// @Summary Preview commitment billing terms
// @ID previewCommitmentBillingTerms
// @Description Previews a complete replacement without changing rules, invoices or schedule items.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Commitment ID"
// @Param request body dto.PreviewBillingTermsRequest true "Complete proposed collection arrangement"
// @Success 200 {object} dto.BillingTermsPreviewResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse
// @Router /commitments/{id}/billing-terms/preview [post]
func (h *billingTermsHandler) Preview(c *gin.Context) {
	var request dto.PreviewBillingTermsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid billing terms preview").Mark(ierr.ErrValidation))
		return
	}
	response, err := h.service.Preview(c.Request.Context(), c.Param("id"), request)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// @Summary Replace commitment billing terms
// @ID replaceCommitmentBillingTerms
// @Description Archives the previous rules and replaces unclaimed charges while retaining invoice provenance.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Commitment ID"
// @Param request body dto.ReplaceBillingTermsRequest true "Reviewed replacement command"
// @Success 200 {object} dto.BillingTermsStateResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse
// @Router /commitments/{id}/billing-terms [put]
func (h *billingTermsHandler) Replace(c *gin.Context) {
	var request dto.ReplaceBillingTermsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid billing terms replacement").Mark(ierr.ErrValidation))
		return
	}
	response, err := h.service.Replace(c.Request.Context(), c.Param("id"), request)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// @Summary List commitment billing schedule items
// @ID listCommitmentBillingScheduleItems
// @Description Lists concrete charges with exact invoice sources; current items sort by bill date, archived history by creation time.
// @Tags Commitments
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Commitment ID"
// @Param limit query int false "Page size, 1–100; default 50"
// @Param cursor query string false "Opaque scoped page cursor"
// @Param include_archived query boolean false "Include historical sources"
// @Param schedule_status query string false "scheduled, invoiced or canceled"
// @Success 200 {object} dto.ListBillingScheduleItemsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Router /commitments/{id}/billing-schedule-items [get]
func (h *billingTermsHandler) ListItems(c *gin.Context) {
	var request dto.ListBillingScheduleItemsRequest
	if err := c.ShouldBindQuery(&request); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid billing schedule filter").Mark(ierr.ErrValidation))
		return
	}
	response, err := h.service.ListItems(c.Request.Context(), c.Param("id"), request)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// @Summary Get commitment billing schedule item
// @ID getCommitmentBillingScheduleItem
// @Description Returns an exact charge and its original rule, including archived sources.
// @Tags Commitments
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Commitment ID"
// @Param item_id path string true "Billing schedule item ID"
// @Success 200 {object} dto.BillingScheduleItemResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Router /commitments/{id}/billing-schedule-items/{item_id} [get]
func (h *billingTermsHandler) GetItem(c *gin.Context) {
	response, err := h.service.GetItem(c.Request.Context(), c.Param("id"), c.Param("item_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// @Summary Get commitment billing terms history
// @ID getCommitmentBillingTermsHistory
// @Description Returns current and archived schedule sources with their original rules.
// @Tags Commitments
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Commitment ID"
// @Param limit query int false "Page size, 1–100; default 50"
// @Param cursor query string false "Opaque scoped history cursor"
// @Param schedule_status query string false "scheduled, invoiced or canceled"
// @Success 200 {object} dto.ListBillingScheduleItemsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Router /commitments/{id}/billing-terms/history [get]
func (h *billingTermsHandler) History(c *gin.Context) {
	var request dto.ListBillingScheduleItemsRequest
	if err := c.ShouldBindQuery(&request); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid billing history filter").Mark(ierr.ErrValidation))
		return
	}
	response, err := h.service.History(c.Request.Context(), c.Param("id"), request)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// @Summary Trigger a commitment billing milestone
// @ID triggerCommitmentBillingScheduleItem
// @Description Resolves a manual charge's date without creating an invoice synchronously.
// @Tags Commitments
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Commitment ID"
// @Param item_id path string true "Billing schedule item ID"
// @Param request body dto.TriggerBillingScheduleItemRequest true "Reviewed manual trigger"
// @Success 200 {object} dto.TriggerBillingScheduleItemResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 409 {object} ierr.ErrorResponse
// @Router /commitments/{id}/billing-schedule-items/{item_id}/trigger [post]
func (h *billingTermsHandler) Trigger(c *gin.Context) {
	var request dto.TriggerBillingScheduleItemRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid billing milestone trigger").Mark(ierr.ErrValidation))
		return
	}
	response, err := h.service.Trigger(c.Request.Context(), c.Param("id"), c.Param("item_id"), request)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}
