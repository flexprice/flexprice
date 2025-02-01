package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type WalletHandler struct {
	walletService service.WalletService
	logger        *logger.Logger
}

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
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /wallets [post]
func (h *WalletHandler) CreateWallet(c *gin.Context) {
	var req dto.CreateWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		NewErrorResponse(c, http.StatusBadRequest, "invalid request", err)
		return
	}

	wallet, err := h.walletService.CreateWallet(c.Request.Context(), &req)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "failed to create wallet", err)
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
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /customers/{id}/wallets [get]
func (h *WalletHandler) GetWalletsByCustomerID(c *gin.Context) {
	customerID := c.Param("id")
	if customerID == "" {
		NewErrorResponse(c, http.StatusBadRequest, "customer id is required", nil)
		return
	}

	wallets, err := h.walletService.GetWalletsByCustomerID(c.Request.Context(), customerID)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "failed to get wallets", err)
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
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /wallets/{id} [get]
func (h *WalletHandler) GetWalletByID(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		NewErrorResponse(c, http.StatusBadRequest, "id is required", nil)
		return
	}

	wallet, err := h.walletService.GetWalletByID(c.Request.Context(), walletID)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "failed to get wallet", err)
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
// @Param filter query types.WalletTransactionFilter false "Filter"
// @Success 200 {object} dto.ListWalletTransactionsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /wallets/{id}/transactions [get]
func (h *WalletHandler) GetWalletTransactions(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		NewErrorResponse(c, http.StatusBadRequest, "id is required", nil)
		return
	}

	var filter types.WalletTransactionFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		NewErrorResponse(c, http.StatusBadRequest, "invalid filter parameters", err)
		return
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	transactions, err := h.walletService.GetWalletTransactions(c.Request.Context(), walletID, &filter)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "failed to get transactions", err)
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
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /wallets/{id}/top-up [post]
func (h *WalletHandler) TopUpWallet(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		NewErrorResponse(c, http.StatusBadRequest, "id is required", nil)
		return
	}

	var req dto.TopUpWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		NewErrorResponse(c, http.StatusBadRequest, "invalid request", err)
		return
	}

	wallet, err := h.walletService.TopUpWallet(c.Request.Context(), walletID, &req)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "failed to top up wallet", err)
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
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /wallets/{id}/balance/real-time [get]
func (h *WalletHandler) GetWalletBalance(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		NewErrorResponse(c, http.StatusBadRequest, "id is required", nil)
		return
	}

	balance, err := h.walletService.GetWalletBalance(c.Request.Context(), walletID)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "failed to get wallet balance", err)
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
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /wallets/{id}/terminate [post]
func (h *WalletHandler) TerminateWallet(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		NewErrorResponse(c, http.StatusBadRequest, "wallet id is required", nil)
		return
	}

	err := h.walletService.TerminateWallet(c.Request.Context(), walletID)
	if err != nil {
		NewErrorResponse(c, http.StatusInternalServerError, "failed to terminate wallet", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "wallet terminated successfully"})
}
