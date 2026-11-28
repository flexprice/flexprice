package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	productRepo "github.com/flexprice/flexprice/internal/repository/product"
	"github.com/gin-gonic/gin"
)

type ProductHandler struct {
	service service.ProductService
	log     *logger.Logger
}

func NewProductHandler(service service.ProductService, log *logger.Logger) *ProductHandler {
	return &ProductHandler{service: service, log: log}
}

// @Summary Create product
// @ID createProduct
// @Description Use when defining a reusable commercial identity for what you sell. Prices can reference it through product_id.
// @Tags Products
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param product body dto.CreateProductRequest true "Product Request"
// @Success 201 {object} dto.ProductResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /products [post]
func (h *ProductHandler) CreateProduct(c *gin.Context) {
	var req dto.CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateProduct(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get product
// @ID getProduct
// @Description Use when you need to load a single product.
// @Tags Products
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Product ID"
// @Success 200 {object} dto.ProductResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /products/{id} [get]
func (h *ProductHandler) GetProduct(c *gin.Context) {
	resp, err := h.service.GetProduct(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List products
// @ID listProducts
// @Description Use when listing products. Returns a paginated list.
// @Tags Products
// @Produce json
// @Security ApiKeyAuth
// @Param filter query productRepo.Filter false "Filter"
// @Success 200 {object} dto.ListProductsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /products [get]
func (h *ProductHandler) ListProducts(c *gin.Context) {
	var filter productRepo.Filter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetProducts(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Query products
// @ID queryProduct
// @Description Use when searching products by ids, lookup keys, filters and sorts. Returns a paginated list.
// @Tags Products
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param filter body productRepo.Filter true "Filter"
// @Success 200 {object} dto.ListProductsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /products/search [post]
func (h *ProductHandler) QueryProducts(c *gin.Context) {
	var filter productRepo.Filter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetProducts(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update product
// @ID updateProduct
// @Description Use when changing a product's name or description. The lookup key never changes.
// @Tags Products
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Product ID"
// @Param product body dto.UpdateProductRequest true "Update Product Request"
// @Success 200 {object} dto.ProductResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /products/{id} [put]
func (h *ProductHandler) UpdateProduct(c *gin.Context) {
	var req dto.UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateProduct(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete product
// @ID deleteProduct
// @Description Use when retiring a product. Fails while any published price references it.
// @Tags Products
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Product ID"
// @Success 200 {object} dto.SuccessResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /products/{id} [delete]
func (h *ProductHandler) DeleteProduct(c *gin.Context) {
	if err := h.service.DeleteProduct(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse{Message: "product deleted successfully"})
}
