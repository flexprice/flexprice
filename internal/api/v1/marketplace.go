package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// MarketplaceHandler exposes marketplace agreement registration. It parses and validates the request
// and delegates to the service; it contains no business logic.
type MarketplaceHandler struct {
	service service.MarketplaceService
	log     *logger.Logger
}

func NewMarketplaceHandler(service service.MarketplaceService, log *logger.Logger) *MarketplaceHandler {
	return &MarketplaceHandler{
		service: service,
		log:     log,
	}
}

// RegisterAgreement godoc
// @Summary Register an AWS Marketplace agreement
// @Description Registers an AWS Marketplace buyer agreement against an existing FlexPrice subscription, upserting plan/subscription/customer integration mappings in one call.
// @Tags Marketplace
// @Accept json
// @Produce json
// @Param request body dto.RegisterMarketplaceAgreementRequest true "Agreement registration request"
// @Success 201 {object} dto.RegisterMarketplaceAgreementResponse
// @Router /marketplace/agreements [post]
func (h *MarketplaceHandler) RegisterAgreement(c *gin.Context) {
	var req dto.RegisterMarketplaceAgreementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.RegisterAgreement(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}
