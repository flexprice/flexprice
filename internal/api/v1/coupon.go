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

// @Summary Create coupon
// @ID createCoupon
// @Description Use when creating a discount (e.g. promo code or referral). Ideal for percent or fixed value, with optional validity and usage limits.
// @Tags Coupons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param coupon body dto.CreateCouponRequest true "Coupon request"
// @Success 201 {object} dto.CouponResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 403 {object} ierr.ErrorResponse "Forbidden"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

// @Summary Get coupon
// @ID getCoupon
// @Description Use when you need to load a single coupon (e.g. for display or to validate a code).
// @Tags Coupons
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Coupon ID"
// @Success 200 {object} dto.CouponResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /coupons/{id} [get]
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

// @Summary Update coupon
// @ID updateCoupon
// @Description Use when changing coupon config (e.g. value, validity, or usage limits).
// @Tags Coupons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Coupon ID"
// @Param coupon body dto.UpdateCouponRequest true "Coupon update request"
// @Success 200 {object} dto.CouponResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 403 {object} ierr.ErrorResponse "Forbidden"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

// @Summary Delete coupon
// @ID deleteCoupon
// @Description Use when retiring a coupon (e.g. campaign ended). Returns 200 with success message.
// @Tags Coupons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Coupon ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 403 {object} ierr.ErrorResponse "Forbidden"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

func (h *CouponHandler) ListCoupons(c *gin.Context) {
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

// @Summary Query coupons
// @ID queryCoupon
// @Description Use when listing or searching coupons (e.g. promo management). Returns a paginated list; supports filtering and sorting.
// @Tags Coupons
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.CouponFilter true "Filter"
// @Success 200 {object} dto.ListCouponsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /coupons/search [post]
func (h *CouponHandler) QueryCoupons(c *gin.Context) {
	var filter types.CouponFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
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
