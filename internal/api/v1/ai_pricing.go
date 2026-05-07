package v1

import (
	"io"
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

const maxParseGeminiBodyBytes = 512 * 1024

// AIPricingHandler handles server-side AI pricing parse (Gemini proxy).
type AIPricingHandler struct {
	geminiPricing service.GeminiPricingService
	log           *logger.Logger
}

// NewAIPricingHandler constructs AIPricingHandler.
func NewAIPricingHandler(geminiPricing service.GeminiPricingService, log *logger.Logger) *AIPricingHandler {
	return &AIPricingHandler{
		geminiPricing: geminiPricing,
		log:           log,
	}
}

func (h *AIPricingHandler) ParseGeminiPricing(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxParseGeminiBodyBytes)

	var req dto.ParseGeminiPricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if err == io.EOF {
			c.Error(ierr.NewError("request body is required").
				WithHint("Request body is required").
				Mark(ierr.ErrValidation))
			return
		}
		var maxErr *http.MaxBytesError
		if ierr.As(err, &maxErr) {
			c.Error(ierr.NewError("request body too large").
				WithHint("Request is too large").
				Mark(ierr.ErrValidation))
			return
		}
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	payload, err := h.geminiPricing.ParsePricing(c.Request.Context(), &req)
	if err != nil {
		if ierr.IsTooManyRequests(err) {
			h.log.WarnwCtx(c.Request.Context(), "parse gemini pricing rate limited", "error", err)
		} else {
			h.log.ErrorwCtx(c.Request.Context(), "parse gemini pricing failed", "error", err)
		}
		c.Error(err)
		return
	}

	c.Data(http.StatusOK, "application/json", payload)
}
