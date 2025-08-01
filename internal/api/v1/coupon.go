package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type CouponHandler struct {
	couponService service.CouponService
	logger        *logger.Logger
}

func NewCouponHandler(couponService service.CouponService, logger *logger.Logger) *CouponHandler {
	return &CouponHandler{
		couponService: couponService,
		logger:        logger,
	}
}

// @Summary Create a new coupon
// @Description Creates a new coupon
// @Tags Coupons
// @Accept json
// @Produce json
// @Param coupon body dto.CreateCouponRequest true "Coupon request"
// @Success 201 {object} dto.CouponResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /coupons [post]
// @Security ApiKeyAuth
func (h *CouponHandler) CreateCoupon(c *gin.Context) {
	var req dto.CreateCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.couponService.CreateCoupon(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, response)
}

// @Summary Get a coupon by ID
// @Description Retrieves a coupon by ID
// @Tags Coupons
// @Accept json
// @Produce json
// @Param id path string true "Coupon ID"
// @Success 200 {object} dto.CouponResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /coupons/{id} [get]
// @Security ApiKeyAuth
func (h *CouponHandler) GetCoupon(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("coupon ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.couponService.GetCoupon(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Update a coupon
// @Description Updates an existing coupon
// @Tags Coupons
// @Accept json
// @Produce json
// @Param id path string true "Coupon ID"
// @Param coupon body dto.UpdateCouponRequest true "Coupon update request"
// @Success 200 {object} dto.CouponResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /coupons/{id} [put]
// @Security ApiKeyAuth
func (h *CouponHandler) UpdateCoupon(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("coupon ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.UpdateCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.couponService.UpdateCoupon(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Delete a coupon
// @Description Deletes a coupon
// @Tags Coupons
// @Accept json
// @Produce json
// @Param id path string true "Coupon ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /coupons/{id} [delete]
// @Security ApiKeyAuth
func (h *CouponHandler) DeleteCoupon(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("coupon ID is required").
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.couponService.DeleteCoupon(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Coupon deleted successfully"})
}

// @Summary List coupons with filtering
// @Description Lists coupons with filtering
// @Tags Coupons
// @Accept json
// @Produce json
// @Param filter query types.CouponFilter true "Filter options"
// @Success 200 {object} dto.ListCouponsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 401 {object} ierr.ErrorResponse
// @Failure 403 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /coupons [get]
// @Security ApiKeyAuth
func (h *CouponHandler) ListCouponsByFilter(c *gin.Context) {
	var filter types.CouponFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	response, err := h.couponService.ListCoupons(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}
