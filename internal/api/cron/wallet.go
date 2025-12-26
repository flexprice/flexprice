package cron

import (
	"context"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type WalletCronHandler struct {
	logger             *logger.Logger
	walletService      service.WalletService
	tenantService      service.TenantService
	environmentService service.EnvironmentService
	featureService     service.FeatureService
	alertLogsService   service.AlertLogsService
}

func NewWalletCronHandler(logger *logger.Logger,
	walletService service.WalletService,
	tenantService service.TenantService,
	environmentService service.EnvironmentService,
	featureService service.FeatureService,
	alertLogsService service.AlertLogsService,
) *WalletCronHandler {
	return &WalletCronHandler{
		logger:             logger,
		walletService:      walletService,
		tenantService:      tenantService,
		environmentService: environmentService,
		featureService:     featureService,
		alertLogsService:   alertLogsService,
	}
}

// ExpireCredits finds and expires credits that have passed their expiry date
func (h *WalletCronHandler) ExpireCredits(c *gin.Context) {
	h.logger.Infow("starting credit expiry cron job - %s", time.Now().UTC().Format(time.RFC3339))

	tenants, err := h.tenantService.GetAllTenants(c.Request.Context())
	if err != nil {
		h.logger.Errorw("failed to get all tenants", "error", err)
		c.Error(err)
		return
	}

	// Create filter to find expired credits
	filter := &types.WalletTransactionFilter{
		Type:               lo.ToPtr(types.TransactionTypeCredit),
		TransactionStatus:  lo.ToPtr(types.TransactionStatusCompleted),
		ExpiryDateBefore:   lo.ToPtr(time.Now().UTC().Add(6 * time.Hour)),
		CreditsAvailableGT: lo.ToPtr(decimal.Zero),
	}

	response := &dto.ExpiredCreditsResponse{
		Items:   make([]*dto.ExpiredCreditsResponseItem, 0),
		Total:   0,
		Success: 0,
		Failed:  0,
	}

	for _, tenant := range tenants {
		ctx := context.WithValue(c.Request.Context(), types.CtxTenantID, tenant.ID)
		// fetch all environments for the tenant
		environments, err := h.environmentService.GetEnvironments(ctx, types.GetDefaultFilter())
		if err != nil {
			h.logger.Errorw("failed to get all environments", "error", err)
			c.Error(err)
			return
		}

		for _, environment := range environments.Environments {
			ctx = context.WithValue(ctx, types.CtxEnvironmentID, environment.ID)

			tenantResponse := &dto.ExpiredCreditsResponseItem{
				TenantID:      tenant.ID,
				EnvironmentID: environment.ID,
				Count:         0,
			}

			// Get transactions with expired credits
			transactions, err := h.walletService.ListWalletTransactionsByFilter(ctx, filter)
			if err != nil {
				h.logger.Errorw("failed to list expired credits",
					"error", err,
				)
				c.Error(err)
				return
			}

			h.logger.Infow("found expired credits", "count", len(transactions.Items))

			// Process each expired credit
			for _, tx := range transactions.Items {
				tenantResponse.Count++
				response.Total++

				// Setting same user for expiry as creation for RBAC in future
				ctx = context.WithValue(ctx, types.CtxUserID, tx.CreatedBy)
				if err := h.walletService.ExpireCredits(ctx, tx.ID); err != nil {
					h.logger.Errorw("failed to expire credits",
						"transaction_id", tx.ID,
						"error", err,
					)
					response.Failed++
					continue
				}

				response.Success++
				h.logger.Infow("expired credits successfully",
					"transaction_id", tx.ID,
					"wallet_id", tx.WalletID,
					"amount", tx.CreditsAvailable,
				)
			}

			response.Items = append(response.Items, tenantResponse)
		}
	}

	h.logger.Infow("completed credit expiry cron job")
	c.JSON(http.StatusOK, response)
}

// CheckAlerts checks wallet balances and triggers alerts based on thresholds
func (h *WalletCronHandler) CheckAlerts(c *gin.Context) {
	h.logger.Infow("starting wallet balance alert and feature alert check cron job", "time", time.Now().UTC().Format(time.RFC3339))

	// parse request body
	var req types.CheckAlertsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to parse request body", "error", err)
		c.Error(err)
		return
	}

	tenantIDs := req.TenantIDs

	// Process each tenant
	for _, tenantID := range tenantIDs {
		h.logger.Infow("processing tenant", "tenant_id", tenantID)
		ctx := context.WithValue(c.Request.Context(), types.CtxTenantID, tenantID)

		// fetch all environments
		environments, err := h.environmentService.GetEnvironments(ctx, types.GetDefaultFilter())
		if err != nil {
			h.logger.Errorw("failed to get all environments", "error", err)
			c.Error(err)
			return
		}

		finalEnvs := make([]dto.EnvironmentResponse, 0)
		if len(req.EnvIDs) > 0 {
			for _, envID := range req.EnvIDs {
				for _, environment := range environments.Environments {
					if environment.ID == envID {
						finalEnvs = append(finalEnvs, environment)
					}
				}
			}
		} else {
			finalEnvs = environments.Environments
		}

		for _, environment := range finalEnvs {
			ctx = context.WithValue(ctx, types.CtxEnvironmentID, environment.ID)

			// Get active wallets for this tenant
			wallets, err := h.walletService.GetWallets(ctx, &types.WalletFilter{
				Status:       lo.ToPtr(types.WalletStatusActive),
				AlertEnabled: lo.ToPtr(true),
				WalletIDs:    req.WalletIDs,
			})
			if err != nil {
				h.logger.Errorw("failed to get active wallets for tenant",
					"tenant_id", tenantID,
					"error", err,
				)
				continue
			}

			h.logger.Infow("found wallets for tenant",
				"tenant_id", tenantID,
				"count", len(wallets.Items),
			)

			// Get all features for this tenant+environment - we'll filter by alert settings in code
			queryFilter := types.NewNoLimitQueryFilter()
			queryFilter.Status = lo.ToPtr(types.StatusPublished)

			features, err := h.featureService.GetFeatures(ctx, &types.FeatureFilter{
				QueryFilter: queryFilter,
			})
			if err != nil {
				h.logger.Errorw("failed to get features for alert checking",
					"environment_id", environment.ID,
					"tenant_id", tenantID,
					"error", err,
				)
				continue
			}

			// Filter to only features with alert settings enabled
			featuresWithAlerts := lo.Filter(features.Items, func(feature *dto.FeatureResponse, _ int) bool {
				return feature.AlertSettings != nil && feature.AlertSettings.IsAlertEnabled()
			})

			h.logger.Infow("found features with alert settings enabled",
				"environment_id", environment.ID,
				"tenant_id", tenantID,
				"total_features", len(features.Items),
				"features_with_alerts_enabled", len(featuresWithAlerts),
			)

			// Process each wallet
			for _, wallet := range wallets.Items {
				h.logger.Infow("processing wallet",
					"wallet_id", wallet.ID,
					"alert_enabled", wallet.AlertEnabled,
					"alert_state", wallet.AlertState,
					"tenant_id", wallet.TenantID,
					"environment_id", wallet.EnvironmentID,
					"alert_config", wallet.AlertConfig,
				)

				// Skip if alert config is not set
				if wallet.AlertConfig == nil || wallet.AlertConfig.Threshold == nil {
					// assume default threshold
					wallet.AlertConfig = &types.AlertConfig{
						Threshold: req.Threshold,
					}

					if req.Threshold == nil {
						wallet.AlertConfig.Threshold = &types.WalletAlertThreshold{
							Type:  types.AlertThresholdTypeAmount,
							Value: decimal.NewFromInt(1),
						}
					}
				}

				// Get real-time balance
				balance, err := h.walletService.GetWalletBalanceV2(ctx, wallet.ID)
				if err != nil {
					h.logger.Errorw("failed to get wallet balance",
						"wallet_id", wallet.ID,
						"error", err,
					)
					continue
				}

				// Get threshold and balances
				threshold := wallet.AlertConfig.Threshold.Value
				currentBalance := wallet.Balance // Current balance is just the credits
				ongoingBalance := balance.RealTimeBalance
				if ongoingBalance == nil {
					ongoingBalance = &currentBalance
				}

				// Check feature alerts
				// Process each feature with alert settings enabled for this wallet
				for _, feature := range featuresWithAlerts {
					// Determine alert status based on ongoing balance vs alert settings
					alertStatus, err := feature.AlertSettings.AlertState(*ongoingBalance)
					if err != nil {
						h.logger.Errorw("failed to determine alert status",
							"feature_id", feature.ID,
							"feature_name", feature.Name,
							"wallet_id", wallet.ID,
							"ongoing_balance", ongoingBalance,
							"error", err,
						)
						continue
					}

					h.logger.Debugw("feature alert status determined",
						"feature_id", feature.ID,
						"feature_name", feature.Name,
						"wallet_id", wallet.ID,
						"ongoing_balance", ongoingBalance,
						"critical", feature.AlertSettings.Critical,
						"warning", feature.AlertSettings.Warning,
						"alert_enabled", feature.AlertSettings.AlertEnabled,
						"alert_status", alertStatus,
					)

					// Log the alert using AlertLogsService (includes state transition logic and webhook publishing)
					// Get customer ID from wallet if available
					var customerID *string
					if wallet.CustomerID != "" {
						customerID = lo.ToPtr(wallet.CustomerID)
					}

					err = h.alertLogsService.LogAlert(ctx, &service.LogAlertRequest{
						EntityType:       types.AlertEntityTypeFeature,
						EntityID:         feature.ID,
						ParentEntityType: lo.ToPtr("wallet"),  // Parent entity is the wallet
						ParentEntityID:   lo.ToPtr(wallet.ID), // Wallet ID as parent entity ID
						CustomerID:       customerID,          // Customer ID from wallet
						AlertType:        types.AlertTypeFeatureWalletBalance,
						AlertStatus:      alertStatus,
						AlertInfo: types.AlertInfo{
							AlertSettings: feature.AlertSettings, // Include full alert settings
							ValueAtTime:   *ongoingBalance,       // Ongoing balance at time of check
							Timestamp:     time.Now().UTC(),
						},
					})
					if err != nil {
						h.logger.Errorw("failed to check feature alert",
							"feature_id", feature.ID,
							"wallet_id", wallet.ID,
							"alert_status", alertStatus,
							"error", err,
						)
						continue
					}

					h.logger.Debugw("feature alert check completed",
						"feature_id", feature.ID,
						"wallet_id", wallet.ID,
						"alert_status", alertStatus,
					)
				}

				// Check wallet ongoing balance alert
				h.logger.Debugw("checking balances against threshold",
					"wallet_id", wallet.ID,
					"threshold", threshold,
					"current_balance", currentBalance,
					"ongoing_balance", ongoingBalance,
					"alert_state", wallet.AlertState,
				)

				// Check ongoing balance
				isOngoingBalanceBelowThreshold := ongoingBalance.LessThanOrEqual(threshold)

				h.logger.Debugw("wallet ongoing balance check results",
					"wallet_id", wallet.ID,
					"ongoing_balance_below", isOngoingBalanceBelowThreshold,
				)

				// Determine alert status based on balance check
				var alertStatus types.AlertState
				if isOngoingBalanceBelowThreshold {
					alertStatus = types.AlertStateInAlarm
				} else {
					alertStatus = types.AlertStateOk
				}

				h.logger.Debugw("logging wallet ongoing balance alert status",
					"wallet_id", wallet.ID,
					"threshold", threshold,
					"ongoing_balance", ongoingBalance,
					"alert_status", alertStatus,
					"ongoing_balance_alert_state", wallet.AlertState,
				)

				// Use AlertLogsService to handle alert logging and webhook publishing
				// For wallet alerts, we store the threshold info in AlertSettings format for consistency
				// Get customer ID from wallet if available
				var customerID *string
				if wallet.CustomerID != "" {
					customerID = lo.ToPtr(wallet.CustomerID)
				}

				err = h.alertLogsService.LogAlert(ctx, &service.LogAlertRequest{
					EntityType:  types.AlertEntityTypeWallet,
					EntityID:    wallet.ID,
					CustomerID:  customerID, // Customer ID from wallet
					AlertType:   types.AlertTypeLowOngoingBalance,
					AlertStatus: alertStatus,
					AlertInfo: types.AlertInfo{
						AlertSettings: &types.AlertSettings{
							Critical: &types.AlertThreshold{
								Threshold: wallet.AlertConfig.Threshold.Value,
								Condition: types.AlertConditionBelow, // Wallet alerts are "below" threshold
							},
							AlertEnabled: lo.ToPtr(true),
						},
						ValueAtTime: *ongoingBalance,
						Timestamp:   time.Now().UTC(),
					},
				})
				if err != nil {
					h.logger.Errorw("failed to check wallet ongoing balance alert",
						"wallet_id", wallet.ID,
						"alert_status", alertStatus,
						"error", err,
					)
					continue
				}

				h.logger.Debugw("wallet ongoing balance alert check completed",
					"wallet_id", wallet.ID,
					"alert_status", alertStatus,
				)

				// Update wallet alert state to match the logged status (if it changed)
				if wallet.AlertState != string(alertStatus) {
					if err := h.walletService.UpdateWalletAlertState(ctx, wallet.ID, alertStatus); err != nil {
						h.logger.Errorw("failed to update wallet alert state",
							"wallet_id", wallet.ID,
							"new_state", alertStatus,
							"error", err,
						)
					} else {
						h.logger.Infow("updated wallet alert state",
							"wallet_id", wallet.ID,
							"old_state", wallet.AlertState,
							"new_state", alertStatus,
						)
					}
				}
			}
		}
	}
	h.logger.Infow("completed wallet balance alert check cron job")
	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}
