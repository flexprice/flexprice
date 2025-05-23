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

// WalletHandler handles wallet-related HTTP requests
type WalletHandler struct {
	walletService service.WalletService
	logger        *logger.Logger
}

// NewWalletHandler creates a new wallet handler
func NewWalletHandler(walletService service.WalletService, logger *logger.Logger) *WalletHandler {
	return &WalletHandler{
		walletService: walletService,
		logger:        logger,
	}
}

// CreateWallet godoc
// @Summary Create a new wallet
// @Description Create a new wallet for a customer
// @Tags Wallets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CreateWalletRequest true "Create wallet request"
// @Success 200 {object} dto.WalletResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /wallets [post]
func (h *WalletHandler) CreateWallet(c *gin.Context) {
	var req dto.CreateWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	wallet, err := h.walletService.CreateWallet(c.Request.Context(), &req)
	if err != nil {
		h.logger.Error("Failed to create wallet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, wallet)
}

// GetWalletsByCustomerID godoc
// @Summary Get wallets by customer ID
// @Description Get all wallets for a customer
// @Tags Wallets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Customer ID"
// @Success 200 {array} dto.WalletResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/{id}/wallets [get]
func (h *WalletHandler) GetWalletsByCustomerID(c *gin.Context) {
	customerID := c.Param("id")
	if customerID == "" {
		c.Error(ierr.NewError("customer_id is required").
			WithHint("Customer ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	wallets, err := h.walletService.GetWalletsByCustomerID(c.Request.Context(), customerID)
	if err != nil {
		h.logger.Error("Failed to get wallets", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, wallets)
}

// GetCustomerWallets godoc
// @Summary Get Customer Wallets
// @Description Get all wallets for a customer by lookup key or id
// @Tags Wallets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request query dto.GetCustomerWalletsRequest true "Get customer wallets request"
// @Success 200 {array} dto.WalletResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /customers/wallets [get]
func (h *WalletHandler) GetCustomerWallets(c *gin.Context) {
	var req dto.GetCustomerWalletsRequest
	// All data is present in the query params
	if err := c.ShouldBindQuery(&req); err != nil {
		h.logger.Error("Failed to bind query parameters", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	wallets, err := h.walletService.GetCustomerWallets(c.Request.Context(), &req)
	if err != nil {
		h.logger.Error("Failed to get customer wallets", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, wallets)
}

// GetWalletByID godoc
// @Summary Get wallet by ID
// @Description Get a wallet by its ID
// @Tags Wallets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Wallet ID"
// @Success 200 {object} dto.WalletResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /wallets/{id} [get]
func (h *WalletHandler) GetWalletByID(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.Error(ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	wallet, err := h.walletService.GetWalletByID(c.Request.Context(), walletID)
	if err != nil {
		h.logger.Error("Failed to get wallet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, wallet)
}

// GetWalletTransactions godoc
// @Summary Get wallet transactions
// @Description Get transactions for a wallet with pagination
// @Tags Wallets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Wallet ID"
// @Param filter query types.WalletTransactionFilter false "Filter"
// @Success 200 {object} dto.ListWalletTransactionsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /wallets/{id}/transactions [get]
func (h *WalletHandler) GetWalletTransactions(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.Error(ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var filter types.WalletTransactionFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		h.logger.Error("Failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	transactions, err := h.walletService.GetWalletTransactions(c.Request.Context(), walletID, &filter)
	if err != nil {
		h.logger.Error("Failed to get wallet transactions", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, transactions)
}

// TopUpWallet godoc
// @Summary Top up wallet
// @Description Add credits to a wallet
// @Tags Wallets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Wallet ID"
// @Param request body dto.TopUpWalletRequest true "Top up request"
// @Success 200 {object} dto.WalletResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /wallets/{id}/top-up [post]
func (h *WalletHandler) TopUpWallet(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.Error(ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.TopUpWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	wallet, err := h.walletService.TopUpWallet(c.Request.Context(), walletID, &req)
	if err != nil {
		h.logger.Error("Failed to top up wallet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, wallet)
}

// GetWalletBalance godoc
// @Summary Get wallet balance
// @Description Get real-time balance of a wallet
// @Tags Wallets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Wallet ID"
// @Success 200 {object} dto.WalletBalanceResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /wallets/{id}/balance/real-time [get]
func (h *WalletHandler) GetWalletBalance(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.Error(ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	balance, err := h.walletService.GetWalletBalance(c.Request.Context(), walletID)
	if err != nil {
		h.logger.Error("Failed to get wallet balance", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, balance)
}

// TerminateWallet godoc
// @Summary Terminate a wallet
// @Description Terminates a wallet by closing it and debiting remaining balance
// @Tags Wallets
// @Accept json
// @Produce json
// @Param id path string true "Wallet ID"
// @Success 200 {object} dto.WalletResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /wallets/{id}/terminate [post]
func (h *WalletHandler) TerminateWallet(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.Error(ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.walletService.TerminateWallet(c.Request.Context(), walletID)
	if err != nil {
		h.logger.Error("Failed to terminate wallet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "wallet terminated successfully"})
}

// UpdateWallet godoc
// @Summary Update a wallet
// @Description Update a wallet's details including auto top-up configuration
// @Tags Wallets
// @Accept json
// @Produce json
// @Param id path string true "Wallet ID"
// @Param request body dto.UpdateWalletRequest true "Update wallet request"
// @Success 200 {object} dto.WalletResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /wallets/{id} [put]
func (h *WalletHandler) UpdateWallet(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var req dto.UpdateWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	updated, err := h.walletService.UpdateWallet(ctx, id, &req)
	if err != nil {
		h.logger.Error("Failed to update wallet", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.FromWallet(updated))
}
