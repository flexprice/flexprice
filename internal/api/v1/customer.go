package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type CustomerHandler struct {
	service service.CustomerService
	log     *logger.Logger
}

func NewCustomerHandler(service service.CustomerService, log *logger.Logger) *CustomerHandler {
	return &CustomerHandler{service: service, log: log}
}

// @Summary Create a customer
// @Description Create a customer
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param customer body dto.CreateCustomerRequest true "Customer"
// @Success 201 {object} dto.CustomerResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /customers [post]
func (h *CustomerHandler) CreateCustomer(c *gin.Context) {
	var req dto.CreateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.CreateCustomer(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get a customer
// @Description Get a customer
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Customer ID"
// @Success 200 {object} dto.CustomerResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /customers/{id} [get]
func (h *CustomerHandler) GetCustomer(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetCustomer(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get customers
// @Description Get customers
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query types.Filter false "Filter"
// @Success 200 {object} dto.ListCustomersResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /customers [get]
func (h *CustomerHandler) GetCustomers(c *gin.Context) {
	var filter types.Filter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.GetCustomers(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Update a customer
// @Description Update a customer
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Customer ID"
// @Param customer body dto.UpdateCustomerRequest true "Customer"
// @Success 200 {object} dto.CustomerResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /customers/{id} [put]
func (h *CustomerHandler) UpdateCustomer(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.service.UpdateCustomer(c.Request.Context(), id, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete a customer
// @Description Delete a customer
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Customer ID"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /customers/{id} [delete]
func (h *CustomerHandler) DeleteCustomer(c *gin.Context) {
	id := c.Param("id")

	err := h.service.DeleteCustomer(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}
