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

type CustomerHandler struct {
	service service.CustomerService
	billing service.BillingService
	log     *logger.Logger
}

func NewCustomerHandler(
	service service.CustomerService,
	billing service.BillingService,
	log *logger.Logger,
) *CustomerHandler {
	return &CustomerHandler{
		service: service,
		billing: billing,
		log:     log,
	}
}

// @Summary Create a customer
// @Description Create a customer
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param customer body dto.CreateCustomerRequest true "Customer"
// @Success 201 {object} dto.CustomerResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers [post]
func (h *CustomerHandler) CreateCustomer(c *gin.Context) {
	var req dto.CreateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateCustomer(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
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
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/{id} [get]
func (h *CustomerHandler) GetCustomer(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetCustomer(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
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
// @Param filter query types.CustomerFilter false "Filter"
// @Success 200 {object} dto.ListCustomersResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers [get]
func (h *CustomerHandler) GetCustomers(c *gin.Context) {
	var filter types.CustomerFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.GetCustomers(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
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
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/{id} [put]
func (h *CustomerHandler) UpdateCustomer(c *gin.Context) {
	id := c.Param("id")

	var req dto.UpdateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.UpdateCustomer(c.Request.Context(), id, req)
	if err != nil {
		c.Error(err)
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
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/{id} [delete]
func (h *CustomerHandler) DeleteCustomer(c *gin.Context) {
	id := c.Param("id")

	err := h.service.DeleteCustomer(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary Get a customer by lookup key
// @Description Get a customer by lookup key (external_id)
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param lookup_key path string true "Customer Lookup Key (external_id)"
// @Success 200 {object} dto.CustomerResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/lookup/{lookup_key} [get]
func (h *CustomerHandler) GetCustomerByLookupKey(c *gin.Context) {
	lookupKey := c.Param("lookup_key")
	if lookupKey == "" {
		c.Error(ierr.NewError("lookup key is required").
			WithHint("Lookup key is required").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetCustomerByLookupKey(c.Request.Context(), lookupKey)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get customer entitlements
// @Description Get customer entitlements
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Customer ID"
// @Param filter query dto.GetCustomerEntitlementsRequest false "Filter"
// @Success 200 {object} dto.CustomerEntitlementsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/{id}/entitlements [get]
func (h *CustomerHandler) GetCustomerEntitlements(c *gin.Context) {
	id := c.Param("id")

	// Parse query parameters using binding
	var req dto.GetCustomerEntitlementsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid query parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// Call billing service instead of customer service
	response, err := h.billing.GetCustomerEntitlements(c.Request.Context(), id, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Get customer usage summary
// @Description Get customer usage summary by customer_id or customer_lookup_key (external_customer_id)
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter query dto.GetCustomerUsageSummaryRequest false "Filter"
// @Success 200 {object} dto.CustomerUsageSummaryResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/usage [get]
func (h *CustomerHandler) GetCustomerUsageSummary(c *gin.Context) {
	var req dto.GetCustomerUsageSummaryRequest

	// Check if the deprecated path parameter route was used
	pathParamID := c.Param("id")
	if pathParamID != "" {

		// Still bind query parameters for other fields (feature_ids, subscription_ids, etc)
		if err := c.ShouldBindQuery(&req); err != nil {
			c.Error(ierr.WithError(err).
				WithHint("Invalid query parameters").
				Mark(ierr.ErrValidation))
			return
		}

		// If client also provided customer_id in query, ensure it matches the path id
		if req.CustomerID != "" && req.CustomerID != pathParamID {
			c.Error(ierr.NewError("path id and query customer_id refer to different customers").
				WithHint("Do not mix different identifiers on the deprecated route; use /v1/customers/usage").
				Mark(ierr.ErrValidation))
			return
		}

		// For backward compatibility, ensure customer_id is set from path param
		req.CustomerID = pathParamID
	} else {
		// Route in-place: /customers/usage with query parameters
		// Parse query parameters using binding
		if err := c.ShouldBindQuery(&req); err != nil {
			c.Error(ierr.WithError(err).
				WithHint("Invalid query parameters").
				Mark(ierr.ErrValidation))
			return
		}

		// Validate that at least one customer identifier is provided
		if req.CustomerID == "" && req.CustomerLookupKey == "" {
			c.Error(ierr.NewError("either customer_id or customer_lookup_key is required").
				WithHint("Provide customer_id or customer_lookup_key").
				Mark(ierr.ErrValidation))
			return
		}
	}

	// Resolve customer_lookup_key to customer_id if provided
	if req.CustomerLookupKey != "" {
		customer, err := h.service.GetCustomerByLookupKey(c.Request.Context(), req.CustomerLookupKey)
		if err != nil {
			c.Error(err)
			return
		}

		// Case: when both customer_id and customer_lookup_key are provided, ensure they refer to the same customer
		if req.CustomerID != "" && customer.ID != req.CustomerID {
			c.Error(ierr.NewError("customer_id and customer_lookup_key refer to different customers").
				WithHint("Providing either customer_id or customer_lookup_key is sufficient. But when providing both, ensure both identifiers refer to the same customer.").
				Mark(ierr.ErrValidation))
			return
		}

		req.CustomerID = customer.ID
	}

	// Call billing service
	response, err := h.billing.GetCustomerUsageSummary(c.Request.Context(), req.CustomerID, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary List customers by filter
// @Description List customers by filter
// @Tags Customers
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.CustomerFilter true "Filter"
// @Success 200 {object} dto.ListCustomersResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/search [post]
func (h *CustomerHandler) ListCustomersByFilter(c *gin.Context) {
	var filter types.CustomerFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.service.GetCustomers(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Get upcoming credit grant applications
// @Description Get upcoming credit grant applications for a customer
// @Tags Customers
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Customer ID"
// @Success 200 {object} dto.ListCreditGrantApplicationsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/{id}/grants/upcoming [get]
func (h *CustomerHandler) GetUpcomingCreditGrantApplications(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("customer ID is required").
			WithHint("Please provide a valid customer ID").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.GetUpcomingCreditGrantApplications(c.Request.Context(), id)
	if err != nil {
		h.log.Error("Failed to get upcoming credit grant applications", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
