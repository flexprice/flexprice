package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CustomerPortalService provides customer portal functionality
type CustomerPortalService interface {
	// CreatePortalSession creates a portal session URL for a customer
	CreatePortalSession(ctx context.Context, externalID string) (*dto.PortalSessionResponse, error)
	// GetCustomer returns customer info for the portal
	GetCustomer(ctx context.Context) (*dto.CustomerResponse, error)
	// UpdateCustomer updates customer info from the portal
	UpdateCustomer(ctx context.Context, req dto.UpdateCustomerRequest) (*dto.CustomerResponse, error)
	// GetSubscriptions returns subscriptions for the portal customer
	GetSubscriptions(ctx context.Context, req dto.PortalPaginatedRequest) (*dto.ListSubscriptionsResponse, error)
	// GetSubscription returns a specific subscription
	GetSubscription(ctx context.Context, subscriptionID string) (*dto.SubscriptionResponse, error)
	// GetInvoices returns invoices for the portal customer
	GetInvoices(ctx context.Context, req dto.PortalPaginatedRequest) (*dto.ListInvoicesResponse, error)
	// GetInvoice returns a specific invoice
	GetInvoice(ctx context.Context, invoiceID string) (*dto.InvoiceResponse, error)
	// GetWallets returns wallet balances for the portal customer
	GetWallets(ctx context.Context) ([]*dto.WalletBalanceResponse, error)
	// GetWallet returns a specific wallet
	GetWallet(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error)
	// GetInvoicePDFUrl returns a presigned URL for an invoice PDF
	GetInvoicePDFUrl(ctx context.Context, invoiceID string) (string, error)
	// GetWalletTransactions returns wallet transactions for the portal customer
	GetWalletTransactions(ctx context.Context, walletID string, filter *types.WalletTransactionFilter) (*dto.ListWalletTransactionsResponse, error)
	// GetAnalytics returns usage analytics for the portal customer
	GetAnalytics(ctx context.Context, req dto.PortalAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error)
	// GetCostAnalytics returns cost analytics for the portal customer
	GetCostAnalytics(ctx context.Context, req dto.PortalCostAnalyticsRequest) (*dto.GetDetailedCostAnalyticsResponse, error)
	// GetUsageSummary returns usage summary for the portal customer
	GetUsageSummary(ctx context.Context, req dto.GetCustomerUsageSummaryRequest) (*dto.CustomerUsageSummaryResponse, error)
	// GetPortalConfig returns the customer portal configuration for the current tenant/environment
	GetPortalConfig(ctx context.Context) (*dto.SettingResponse, error)
}

type customerPortalService struct {
	ServiceParams
	customerService         CustomerService
	revenueAnalyticsService RevenueAnalyticsService
}

// NewCustomerPortalService creates a new customer portal service
func NewCustomerPortalService(
	params ServiceParams,
	customerService CustomerService,
	revenueAnalyticsService RevenueAnalyticsService,
) CustomerPortalService {
	return &customerPortalService{
		ServiceParams:           params,
		customerService:         customerService,
		revenueAnalyticsService: revenueAnalyticsService,
	}
}

// CreatePortalSession creates a portal session for a customer
func (s *customerPortalService) CreatePortalSession(ctx context.Context, externalID string) (*dto.PortalSessionResponse, error) {

	// Look up the customer by external ID
	customer, err := s.customerService.GetCustomerByLookupKey(ctx, externalID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Customer not found with the provided external_customer_id").
			Mark(ierr.ErrNotFound)
	}

	// Get auth provider
	authProvider := auth.NewFlexpriceAuth(s.ServiceParams.Config)

	// Generate portal token
	token, expiresAt, err := authProvider.GenerateSessionToken(
		customer.ID,
		customer.ExternalID,
		customer.TenantID,
		customer.EnvironmentID,
		s.Config.CustomerPortal.TokenTimeoutHours,
	)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to generate dashboard token").
			Mark(ierr.ErrSystem)
	}

	// Build portal URL
	dashboardURL := fmt.Sprintf("%s?token=%s", s.Config.CustomerPortal.URL, token)

	return &dto.PortalSessionResponse{
		URL:       dashboardURL,
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

// GetCustomer returns customer info for the portal
func (s *customerPortalService) GetCustomer(ctx context.Context) (*dto.CustomerResponse, error) {

	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}
	customer, err := s.customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}

	return customer, nil
}

// UpdateCustomer updates customer info from the portal
func (s *customerPortalService) UpdateCustomer(ctx context.Context, req dto.UpdateCustomerRequest) (*dto.CustomerResponse, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	customer, err := s.customerService.UpdateCustomer(ctx, customerID, req)
	if err != nil {
		return nil, err
	}

	return customer, nil
}

// GetSubscriptions returns subscriptions for the portal customer
func (s *customerPortalService) GetSubscriptions(ctx context.Context, req dto.PortalPaginatedRequest) (*dto.ListSubscriptionsResponse, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	subscriptionService := NewSubscriptionService(s.ServiceParams)

	// Build query filter with pagination
	queryFilter := types.NewDefaultQueryFilter()

	// Set limit first
	if req.Limit > 0 {
		queryFilter.Limit = &req.Limit
	}

	// Then calculate offset using the correct limit
	if req.Page > 0 {
		limit := queryFilter.GetLimit() // Now this gets the user-provided limit
		offset := (req.Page - 1) * limit
		queryFilter.Offset = &offset
	}

	filter := &types.SubscriptionFilter{
		CustomerID:  customerID,
		QueryFilter: queryFilter,
	}

	return subscriptionService.ListSubscriptions(ctx, filter)
}

// GetSubscription returns a specific subscription
func (s *customerPortalService) GetSubscription(ctx context.Context, subscriptionID string) (*dto.SubscriptionResponse, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	subscriptionService := NewSubscriptionService(s.ServiceParams)

	// Get the subscription
	subscription, err := subscriptionService.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Verify it belongs to this customer
	if subscription.CustomerID != customerID {
		return nil, ierr.NewError("subscription not found").
			WithHint("Subscription does not belong to this customer").
			Mark(ierr.ErrNotFound)
	}

	return subscription, nil
}

// GetInvoices returns invoices for the portal customer
func (s *customerPortalService) GetInvoices(ctx context.Context, req dto.PortalPaginatedRequest) (*dto.ListInvoicesResponse, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	invoiceService := NewInvoiceService(s.ServiceParams)

	// Build query filter with pagination
	queryFilter := types.NewDefaultQueryFilter()

	// Set limit first
	if req.Limit > 0 {
		queryFilter.Limit = &req.Limit
	}

	// Then calculate offset using the correct limit
	if req.Page > 0 {
		limit := queryFilter.GetLimit() // Now this gets the user-provided limit
		offset := (req.Page - 1) * limit
		queryFilter.Offset = &offset
	}

	filter := &types.InvoiceFilter{
		CustomerID:  customerID,
		QueryFilter: queryFilter,
	}

	return invoiceService.ListInvoices(ctx, filter)
}

// GetInvoice returns a specific invoice
func (s *customerPortalService) GetInvoice(ctx context.Context, invoiceID string) (*dto.InvoiceResponse, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	invoiceService := NewInvoiceService(s.ServiceParams)

	// Get the invoice with breakdown
	req := dto.GetInvoiceWithBreakdownRequest{
		ID: invoiceID,
	}
	invoice, err := invoiceService.GetInvoiceWithBreakdown(ctx, req)
	if err != nil {
		return nil, err
	}

	// Verify it belongs to this customer
	if invoice.CustomerID != customerID {
		return nil, ierr.NewError("invoice not found").
			WithHint("Invoice does not belong to this customer").
			Mark(ierr.ErrNotFound)
	}

	return invoice, nil
}

// GetWallets returns wallet balances for the portal customer
func (s *customerPortalService) GetWallets(ctx context.Context) ([]*dto.WalletBalanceResponse, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	walletService := NewWalletService(s.ServiceParams)

	// Only pass ID - validation requires either ID or LookupKey, not both
	getWalletsReq := &dto.GetCustomerWalletsRequest{
		ID: customerID,
	}
	return walletService.GetCustomerWallets(ctx, getWalletsReq)
}

// GetWallet returns a specific wallet
func (s *customerPortalService) GetWallet(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	walletService := NewWalletService(s.ServiceParams)

	// Get the wallet balance
	wallet, err := walletService.GetWalletBalance(ctx, walletID)
	if err != nil {
		return nil, err
	}

	// Verify it belongs to this customer
	if wallet.CustomerID != customerID {
		return nil, ierr.NewError("wallet not found").
			WithHint("Wallet does not belong to this customer").
			Mark(ierr.ErrNotFound)
	}

	return wallet, nil
}

// GetAnalytics returns usage analytics for the portal customer
func (s *customerPortalService) GetAnalytics(ctx context.Context, req dto.PortalAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	customerID := types.GetCustomerID(ctx)
	externalCustomerID := types.GetExternalCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	// Convert dashboard request to internal request with customer ID injected
	internalReq := req.ToInternalRequest(externalCustomerID)

	// Use FeatureUsageTrackingService to get detailed usage analytics
	featureUsageTrackingService := NewFeatureUsageTrackingService(s.ServiceParams, s.EventRepo, s.FeatureUsageRepo)
	return featureUsageTrackingService.GetDetailedUsageAnalyticsV2(ctx, internalReq)
}

// GetCostAnalytics returns cost analytics for the portal customer
func (s *customerPortalService) GetCostAnalytics(ctx context.Context, req dto.PortalCostAnalyticsRequest) (*dto.GetDetailedCostAnalyticsResponse, error) {
	customerID := types.GetCustomerID(ctx)
	externalCustomerID := types.GetExternalCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	// Convert dashboard request to internal request with customer ID injected
	internalReq := req.ToInternalRequest(externalCustomerID)

	// Validate internal request
	if err := internalReq.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid cost analytics request").
			Mark(ierr.ErrValidation)
	}

	// Call the revenue analytics service which handles both cost and revenue analytics
	return s.revenueAnalyticsService.GetDetailedCostAnalytics(ctx, internalReq)
}

// GetUsageSummary returns usage summary for the portal customer
func (s *customerPortalService) GetUsageSummary(ctx context.Context, req dto.GetCustomerUsageSummaryRequest) (*dto.CustomerUsageSummaryResponse, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	// Override any customer_id in the request with the authenticated customer_id
	req.CustomerID = customerID

	// Use billing service to get usage summary
	billingService := NewBillingService(s.ServiceParams)
	return billingService.GetCustomerUsageSummary(ctx, customerID, &req)
}

// GetInvoicePDFUrl returns a presigned URL for an invoice PDF
func (s *customerPortalService) GetInvoicePDFUrl(ctx context.Context, invoiceID string) (string, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return "", ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	invoiceService := NewInvoiceService(s.ServiceParams)

	// Get the invoice to verify ownership
	req := dto.GetInvoiceWithBreakdownRequest{
		ID: invoiceID,
	}
	invoice, err := invoiceService.GetInvoiceWithBreakdown(ctx, req)
	if err != nil {
		return "", err
	}

	// Verify it belongs to this customer
	if invoice.CustomerID != customerID {
		return "", ierr.NewError("invoice not found").
			WithHint("Invoice does not belong to this customer").
			Mark(ierr.ErrNotFound)
	}

	// Get the presigned URL
	return invoiceService.GetInvoicePDFUrl(ctx, invoiceID)
}

// GetPortalConfig returns the customer_portal_config setting for the current tenant/environment.
// If no config is saved for the tenant, the hardcoded defaults from GetDefaultSettings() are returned.
func (s *customerPortalService) GetPortalConfig(ctx context.Context) (*dto.SettingResponse, error) {
	settingsService := NewSettingsService(s.ServiceParams)
	return settingsService.GetSettingByKey(ctx, types.SettingKeyCustomerPortalConfig)
}

// GetWalletTransactions returns transactions for a wallet with pagination
func (s *customerPortalService) GetWalletTransactions(ctx context.Context, walletID string, filter *types.WalletTransactionFilter) (*dto.ListWalletTransactionsResponse, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	walletService := NewWalletService(s.ServiceParams)

	// First verify the wallet belongs to this customer by getting the wallet
	wallet, err := walletService.GetWalletBalance(ctx, walletID)
	if err != nil {
		return nil, err
	}

	// Verify it belongs to this customer
	if wallet.CustomerID != customerID {
		return nil, ierr.NewError("wallet not found").
			WithHint("Wallet does not belong to this customer").
			Mark(ierr.ErrNotFound)
	}

	// Get the transactions
	return walletService.GetWalletTransactions(ctx, walletID, filter)
}
