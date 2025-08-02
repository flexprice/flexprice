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
	logger          *logger.Logger
	temporalService *temporal.Service
	walletService   service.WalletService
	tenantService   service.TenantService
}

func NewWalletCronHandler(logger *logger.Logger, temporalService *temporal.Service, walletService service.WalletService, tenantService service.TenantService) *WalletCronHandler {
	return &WalletCronHandler{
		logger:          logger,
		temporalService: temporalService,
		walletService:   walletService,
		tenantService:   tenantService,
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

	// Get active wallets with alerts enabled
	wallets, err := h.walletService.GetWallets(c.Request.Context(), &types.WalletFilter{
		Status:       lo.ToPtr(types.WalletStatusActive),
		AlertEnabled: lo.ToPtr(true),
	})
	if err != nil {
		h.logger.Errorw("failed to get active wallets", "error", err)
		c.Error(err)
		return
	}

	h.logger.Infow("found wallets with alerts enabled", "count", len(wallets.Items))

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
			h.logger.Infow("skipping wallet - no alert config",
				"wallet_id", wallet.ID,
			)
			continue
		}

		// Skip if tenant is not in allowed list
		if len(wallet.AlertConfig.AllowedTenantIDs) > 0 && !lo.Contains(wallet.AlertConfig.AllowedTenantIDs, wallet.TenantID) {
			h.logger.Infow("skipping wallet - tenant not allowed",
				"wallet_id", wallet.ID,
				"tenant_id", wallet.TenantID,
				"allowed_tenants", wallet.AlertConfig.AllowedTenantIDs,
			)
			continue
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
		currentBalance := balance.RealTimeBalance
		if currentBalance == nil {
			currentBalance = &wallet.Balance
		}
		creditBalance := balance.RealTimeCreditBalance
		if creditBalance == nil {
			creditBalance = &wallet.CreditBalance
		}

		h.logger.Infow("checking balances against threshold",
			"wallet_id", wallet.ID,
			"threshold", threshold,
			"current_balance", currentBalance,
			"credit_balance", creditBalance,
			"alert_state", wallet.AlertState,
		)

		// Check if any balance is below threshold
		isCurrentBalanceBelowThreshold := currentBalance.LessThanOrEqual(threshold)
		isCreditBalanceBelowThreshold := creditBalance.LessThanOrEqual(threshold)
		isAnyBalanceBelowThreshold := isCurrentBalanceBelowThreshold || isCreditBalanceBelowThreshold

		h.logger.Infow("balance check results",
			"wallet_id", wallet.ID,
			"current_balance_below", isCurrentBalanceBelowThreshold,
			"credit_balance_below", isCreditBalanceBelowThreshold,
			"any_balance_below", isAnyBalanceBelowThreshold,
		)

		// Handle balance above threshold (recovery)
		if !isAnyBalanceBelowThreshold {
			h.logger.Infow("all balances above threshold - checking recovery",
				"wallet_id", wallet.ID,
				"threshold", threshold,
				"current_balance", currentBalance,
				"credit_balance", creditBalance,
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
			"credit_balance", creditBalance,
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
		if isCreditBalanceBelowThreshold {
			h.logger.Infow("triggering credit balance alert",
				"wallet_id", wallet.ID,
				"credit_balance", creditBalance,
				"threshold", threshold,
			)
			if err := h.walletService.PublishEvent(c.Request.Context(), types.WebhookEventWalletCreditBalanceDropped, wallet); err != nil {
				h.logger.Errorw("failed to publish credit balance alert",
					"wallet_id", wallet.ID,
					"error", err,
				)
			}
		}
		if isCurrentBalanceBelowThreshold {
			h.logger.Infow("triggering ongoing balance alert",
				"wallet_id", wallet.ID,
				"balance", currentBalance,
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

	h.logger.Infow("completed wallet balance alert check cron job")
	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}
