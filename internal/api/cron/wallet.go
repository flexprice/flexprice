package cron

import (
	"context"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type WalletCronHandler struct {
	logger             *logger.Logger
	temporalService    *temporal.Service
	walletService      service.WalletService
	tenantService      service.TenantService
	environmentService service.EnvironmentService
}

func NewWalletCronHandler(logger *logger.Logger,
	temporalService *temporal.Service,
	walletService service.WalletService,
	tenantService service.TenantService,
	environmentService service.EnvironmentService,
) *WalletCronHandler {
	return &WalletCronHandler{
		logger:             logger,
		temporalService:    temporalService,
		walletService:      walletService,
		tenantService:      tenantService,
		environmentService: environmentService,
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
		ExpiryDateBefore:   lo.ToPtr(time.Now().UTC()),
		CreditsAvailableGT: lo.ToPtr(decimal.Zero),
	}

	response := &dto.ExpiredCreditsResponse{
		Items:   make([]*dto.ExpiredCreditsResponseItem, 0),
		Total:   0,
		Success: 0,
		Failed:  0,
	}

	for _, tenant := range tenants {
		tenantResponse := &dto.ExpiredCreditsResponseItem{
			TenantID: tenant.ID,
			Count:    0,
		}

		h.logger.Infow("tenant", "id", tenant.ID, "name", tenant.Name)
		ctx := context.WithValue(c.Request.Context(), types.CtxTenantID, tenant.ID)
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, "")
		// Get transactions with expired credits
		transactions, err := h.walletService.GetWalletTransactions(ctx, "", filter)
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
			if err := h.walletService.ExpireCredits(ctx, tx.ID); err != nil {
				h.logger.Errorw("failed to expire credits",
					"transaction_id", tx.ID,
					"error", err,
				)
				response.Failed++
				continue
			}

			tenantResponse.Count++
			response.Success++

			h.logger.Infow("expired credits successfully",
				"transaction_id", tx.ID,
				"wallet_id", tx.WalletID,
				"amount", tx.CreditsAvailable,
			)
		}

		response.Items = append(response.Items, tenantResponse)
	}

	h.logger.Infow("completed credit expiry cron job")
	c.JSON(http.StatusOK, response)
}

// CheckAlerts checks wallet balances and triggers alerts based on thresholds
func (h *WalletCronHandler) CheckAlerts(c *gin.Context) {
	h.logger.Infow("starting wallet balance alert check cron job", "time", time.Now().UTC().Format(time.RFC3339))

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
		environments, err := h.environmentService.GetEnvironments(c.Request.Context(), types.GetDefaultFilter())
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
						wallet.AlertConfig.Threshold = &types.AlertThreshold{
							Type:  "amount",
							Value: decimal.NewFromInt(1),
						}
					}
				}

				// Get real-time balance
				balance, err := h.walletService.GetWalletBalance(c.Request.Context(), wallet.ID)
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

				h.logger.Infow("checking balances against threshold",
					"wallet_id", wallet.ID,
					"threshold", threshold,
					"current_balance", currentBalance,
					"ongoing_balance", ongoingBalance,
					"alert_state", wallet.AlertState,
				)

				// Check balances separately
				isCurrentBalanceBelowThreshold := currentBalance.LessThanOrEqual(threshold)
				isOngoingBalanceBelowThreshold := ongoingBalance.LessThanOrEqual(threshold)
				isAnyBalanceBelowThreshold := isCurrentBalanceBelowThreshold || isOngoingBalanceBelowThreshold

				h.logger.Infow("balance check results",
					"wallet_id", wallet.ID,
					"current_balance_below", isCurrentBalanceBelowThreshold,
					"ongoing_balance_below", isOngoingBalanceBelowThreshold,
					"any_balance_below", isAnyBalanceBelowThreshold,
				)

				// Handle balance above threshold (recovery)
				if !isAnyBalanceBelowThreshold {
					h.logger.Infow("all balances above threshold - checking recovery",
						"wallet_id", wallet.ID,
						"threshold", threshold,
						"current_balance", currentBalance,
						"ongoing_balance", ongoingBalance,
						"alert_state", wallet.AlertState,
					)

					// If current state is alert, update to ok (recovery)
					if wallet.AlertState == string(types.AlertStateAlert) {
						if err := h.walletService.UpdateWalletAlertState(c.Request.Context(), wallet.ID, types.AlertStateOk); err != nil {
							h.logger.Errorw("failed to update wallet alert state",
								"wallet_id", wallet.ID,
								"error", err,
							)
						} else {
							h.logger.Infow("wallet recovered from alert state",
								"wallet_id", wallet.ID,
							)
							// Publish recovery webhook
							if err := h.walletService.PublishEvent(c.Request.Context(), types.WebhookEventWalletOngoingBalanceRecovered, wallet); err != nil {
								h.logger.Errorw("failed to publish recovery webhook",
									"wallet_id", wallet.ID,
									"error", err,
								)
							}
						}
					}
					continue
				}

				// Skip if already in alert state
				if wallet.AlertState == string(types.AlertStateAlert) {
					h.logger.Infow("skipping wallet - already in alert state",
						"wallet_id", wallet.ID,
					)
					continue
				}

				h.logger.Infow("balance below/equal threshold - triggering alert",
					"wallet_id", wallet.ID,
					"threshold", threshold,
					"current_balance", currentBalance,
					"ongoing_balance", ongoingBalance,
				)

				// Update wallet state to alert
				if err := h.walletService.UpdateWalletAlertState(c.Request.Context(), wallet.ID, types.AlertStateAlert); err != nil {
					h.logger.Errorw("failed to update wallet alert state",
						"wallet_id", wallet.ID,
						"error", err,
					)
					continue
				}

				// Trigger alerts based on which balance is below threshold
				if isCurrentBalanceBelowThreshold {
					h.logger.Infow("triggering credit balance alert",
						"wallet_id", wallet.ID,
						"credit_balance", currentBalance,
						"threshold", threshold,
					)
					if err := h.walletService.PublishEvent(c.Request.Context(), types.WebhookEventWalletCreditBalanceDropped, wallet); err != nil {
						h.logger.Errorw("failed to publish credit balance alert",
							"wallet_id", wallet.ID,
							"error", err,
						)
					}
				}
				if isOngoingBalanceBelowThreshold {
					h.logger.Infow("triggering ongoing balance alert",
						"wallet_id", wallet.ID,
						"ongoing_balance", ongoingBalance,
						"threshold", threshold,
					)
					if err := h.walletService.PublishEvent(c.Request.Context(), types.WebhookEventWalletOngoingBalanceDropped, wallet); err != nil {
						h.logger.Errorw("failed to publish ongoing balance alert",
							"wallet_id", wallet.ID,
							"error", err,
						)
					}
				}
			}
		}
	}
	h.logger.Infow("completed wallet balance alert check cron job")
	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}
