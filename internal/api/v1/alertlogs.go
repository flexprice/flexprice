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

type AlertLogsHandler struct {
	alertLogsService service.AlertLogsService
	customerService  service.CustomerService
	walletService    service.WalletService
	featureService   service.FeatureService
	log              *logger.Logger
}

func NewAlertLogsHandler(
	alertLogsService service.AlertLogsService,
	customerService service.CustomerService,
	walletService service.WalletService,
	featureService service.FeatureService,
	log *logger.Logger,
) *AlertLogsHandler {
	return &AlertLogsHandler{
		alertLogsService: alertLogsService,
		customerService:  customerService,
		walletService:    walletService,
		featureService:   featureService,
		log:              log,
	}
}

// ListAlertLogsByFilter godoc
// @Summary List alert logs by filter
// @ID listAlertLogsByFilter
// @Description List alert logs by filter with optional expand for customer, wallet, and feature
// @Tags Alert Logs
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.AlertLogFilter true "Filter"
// @Success 200 {object} dto.ListAlertLogsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /alert/search [post]
func (h *AlertLogsHandler) ListAlertLogsByFilter(c *gin.Context) {
	var filter types.AlertLogFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	resp, err := h.alertLogsService.ListAlertLogsByFilter(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	// Convert to DTO
	alertLogResponses := make([]*dto.AlertLogResponse, len(resp.Items))
	for i, alertLog := range resp.Items {
		alertLogResponses[i] = dto.ToAlertLogResponse(alertLog)
	}

	// Handle expansions if requested
	expand := filter.GetExpand()
	if !expand.IsEmpty() {
		// Build maps for efficient lookups
		customerIDs := make(map[string]bool)
		walletIDs := make(map[string]bool)
		featureIDs := make(map[string]bool)

		// Collect unique IDs
		for _, alertLog := range resp.Items {
			if expand.Has(types.ExpandCustomer) && alertLog.CustomerID != nil {
				customerIDs[*alertLog.CustomerID] = true
			}
			if expand.Has(types.ExpandWallet) && alertLog.EntityType == types.AlertEntityTypeWallet {
				walletIDs[alertLog.EntityID] = true
			} else if expand.Has(types.ExpandWallet) && alertLog.ParentEntityType != nil && *alertLog.ParentEntityType == "wallet" && alertLog.ParentEntityID != nil {
				walletIDs[*alertLog.ParentEntityID] = true
			}
			if expand.Has(types.ExpandFeature) && alertLog.EntityType == types.AlertEntityTypeFeature {
				featureIDs[alertLog.EntityID] = true
			}
		}

		// Fetch customers in bulk
		customerMap := make(map[string]*dto.CustomerResponse)
		if len(customerIDs) > 0 {
			for customerID := range customerIDs {
				customer, err := h.customerService.GetCustomer(c.Request.Context(), customerID)
				if err != nil {
					h.log.Warnw("failed to fetch customer for expand", "customer_id", customerID, "error", err)
					continue
				}
				customerMap[customerID] = customer
			}
		}

		// Fetch wallets in bulk
		walletMap := make(map[string]*dto.WalletResponse)
		if len(walletIDs) > 0 {
			for walletID := range walletIDs {
				wallet, err := h.walletService.GetWalletByID(c.Request.Context(), walletID)
				if err != nil {
					h.log.Warnw("failed to fetch wallet for expand", "wallet_id", walletID, "error", err)
					continue
				}
				walletMap[walletID] = wallet
			}
		}

		// Fetch features in bulk
		featureMap := make(map[string]*dto.FeatureResponse)
		if len(featureIDs) > 0 {
			for featureID := range featureIDs {
				feature, err := h.featureService.GetFeature(c.Request.Context(), featureID)
				if err != nil {
					h.log.Warnw("failed to fetch feature for expand", "feature_id", featureID, "error", err)
					continue
				}
				featureMap[featureID] = feature
			}
		}

		// Attach expanded data to responses
		for i, alertLog := range resp.Items {
			// Attach customer
			if expand.Has(types.ExpandCustomer) && alertLog.CustomerID != nil {
				if customer, ok := customerMap[*alertLog.CustomerID]; ok {
					alertLogResponses[i].Customer = customer
				}
			}

			// Attach wallet
			if expand.Has(types.ExpandWallet) {
				var walletID string
				if alertLog.EntityType == types.AlertEntityTypeWallet {
					walletID = alertLog.EntityID
				} else if alertLog.ParentEntityType != nil && *alertLog.ParentEntityType == "wallet" && alertLog.ParentEntityID != nil {
					walletID = *alertLog.ParentEntityID
				}
				if walletID != "" {
					if wallet, ok := walletMap[walletID]; ok {
						alertLogResponses[i].Wallet = wallet
					}
				}
			}

			// Attach feature
			if expand.Has(types.ExpandFeature) && alertLog.EntityType == types.AlertEntityTypeFeature {
				if feature, ok := featureMap[alertLog.EntityID]; ok {
					alertLogResponses[i].Feature = feature
				}
			}
		}
	}

	response := &dto.ListAlertLogsResponse{
		Items:      alertLogResponses,
		Pagination: &resp.Pagination,
	}

	c.JSON(http.StatusOK, response)
}
