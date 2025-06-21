package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/integration"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// StripeMeterMappingHandler handles meter-mapping related Stripe integration endpoints
// Only create endpoint is exposed for now.
type StripeMeterMappingHandler struct {
	stripeIntegrationService service.StripeIntegrationService
	logger                   *logger.Logger
}

func NewStripeMeterMappingHandler(svc service.StripeIntegrationService, log *logger.Logger) *StripeMeterMappingHandler {
	return &StripeMeterMappingHandler{
		stripeIntegrationService: svc,
		logger:                   log,
	}
}

// CreateMeterMapping godoc
// @Summary     Create Stripe meter mapping
// @Description Map a FlexPrice meter to a provider-specific meter (e.g. Stripe).
// @Tags        stripe-meter-mapping
// @Accept      json
// @Produce     json
// @Security    ApiKeyAuth
// @Param       body body dto.CreateMeterMappingRequest true "Meter mapping request"
// @Success     201 {object} dto.MeterMappingResponse
// @Failure     400 {object} ierr.ErrorResponse
// @Failure     401 {object} ierr.ErrorResponse
// @Failure     500 {object} ierr.ErrorResponse
// @Router      /stripe/meter-mappings [post]
func (h *StripeMeterMappingHandler) CreateMeterMapping(c *gin.Context) {
	var req dto.CreateMeterMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("invalid request payload", "error", err)
		c.Error(ierr.WithError(err).WithHint("Invalid request payload").Mark(ierr.ErrValidation))
		return
	}

	ctx := c.Request.Context()

	// Build service-layer request
	svcReq := service.CreateMeterMappingRequest{
		MeterID:         req.MeterID,
		ProviderType:    integration.ProviderType(req.ProviderType),
		ProviderMeterID: req.ProviderMeterID,
		SyncEnabled:     req.SyncEnabled,
		Configuration:   req.Configuration,
	}

	mapping, err := h.stripeIntegrationService.CreateMeterMapping(ctx, svcReq)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, h.toDTO(mapping))
}

func (h *StripeMeterMappingHandler) toDTO(m *service.MeterMappingResponse) *dto.MeterMappingResponse {
	if m == nil {
		return nil
	}
	return &dto.MeterMappingResponse{
		ID:              m.ID,
		MeterID:         m.MeterID,
		ProviderType:    string(m.ProviderType),
		ProviderMeterID: m.ProviderMeterID,
		SyncEnabled:     m.SyncEnabled,
		Configuration:   m.Configuration,
		TenantID:        m.TenantID,
		EnvironmentID:   m.EnvironmentID,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}
