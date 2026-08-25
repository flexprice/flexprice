package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// UsageRecordHandler exposes read access to the usage_records table. It parses and validates the
// request and delegates to the service; it contains no business logic.
type UsageRecordHandler struct {
	service service.UsageRecordService
	log     *logger.Logger
}

func NewUsageRecordHandler(service service.UsageRecordService, log *logger.Logger) *UsageRecordHandler {
	return &UsageRecordHandler{
		service: service,
		log:     log,
	}
}

// QueryUsageRecords godoc
// @Summary List usage records
// @Description Lists usage records. Also accepts filters/sort for a filtered query.
// @Tags Usage Records
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body types.UsageRecordFilter true "Usage record filter"
// @Success 200 {object} dto.ListUsageRecordsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /usage-records/search [post]
func (h *UsageRecordHandler) QueryUsageRecords(c *gin.Context) {
	var filter types.UsageRecordFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.service.ListUsageRecords(c.Request.Context(), &filter)
	if err != nil {
		h.log.Error(c.Request.Context(), "Failed to search usage records", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
