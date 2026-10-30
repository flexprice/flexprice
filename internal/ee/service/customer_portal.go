package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/domain/events"
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
	// GetGroupablePropertyKeys returns the property keys portal users may group or filter usage by
	GetGroupablePropertyKeys(ctx context.Context, req dto.PortalPropertyKeysRequest) (*dto.PropertyKeysResponse, error)
	// GetPropertyValues returns a visible property key's values in the portal customer's usage
	GetPropertyValues(ctx context.Context, req dto.PortalPropertyValuesRequest) (*dto.PropertyValuesResponse, error)
	// GetUsageSummary returns usage summary for the portal customer
	GetUsageSummary(ctx context.Context, req dto.GetCustomerUsageSummaryRequest) (*dto.CustomerUsageSummaryResponse, error)
	// GetPortalConfig returns the customer portal configuration for the current tenant/environment
	GetPortalConfig(ctx context.Context) (*dto.SettingResponse, error)
	GetIntegrations(ctx context.Context) (*dto.IntegrationsResponse, error)

	// Wallet
	TopUpWallet(ctx context.Context, walletID string, req *dto.PortalTopUpWalletRequest) (*dto.PortalTopUpWalletResponse, error)
	UpdateAutoTopup(ctx context.Context, walletID string, req *dto.PortalUpdateAutoTopupRequest) (*dto.WalletResponse, error)

	// Checkout
	GetCheckoutSession(ctx context.Context, sessionID string) (*dto.PortalCheckoutSessionResponse, error)
	CancelCheckoutSession(ctx context.Context, sessionID string) (*dto.PortalCheckoutSessionResponse, error)

	// Invoices
	PayInvoice(ctx context.Context, invoiceID string, req *dto.PortalPayInvoiceRequest) (*dto.PortalPayInvoiceResponse, error)

	// Payment methods
	ListPaymentMethods(ctx context.Context, req *dto.ListSavedPaymentMethodsRequest) (*dto.SavedPaymentMethodsResponse, error)
	AddPaymentMethod(ctx context.Context, req *dto.PortalAddPaymentMethodRequest) (*dto.AddPaymentMethodResponse, error)
	DeletePaymentMethod(ctx context.Context, req *dto.PortalDeletePaymentMethodRequest) (*dto.SavedPaymentMethodsResponse, error)
	SetDefaultPaymentMethod(ctx context.Context, req *dto.PortalSetDefaultPaymentMethodRequest) (*dto.SavedPaymentMethodsResponse, error)
}

type customerPortalService struct {
	ServiceParams
	customerService         CustomerService
	revenueAnalyticsService RevenueAnalyticsService
	paymentService          PaymentService
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
		paymentService:          NewPaymentService(params),
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

	resp, err := invoiceService.ListInvoices(ctx, filter)
	if err != nil {
		return nil, err
	}
	if err := invoiceService.WithHostedInvoiceURLs(ctx, resp.Items...); err != nil {
		return nil, err
	}
	return resp, nil
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

	if err := invoiceService.WithHostedInvoiceURLs(ctx, invoice); err != nil {
		return nil, err
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

	if err := s.validatePropertyKeys(ctx, req); err != nil {
		return nil, err
	}

	// Convert dashboard request to internal request with customer ID injected
	internalReq := req.ToInternalRequest(externalCustomerID)

	meterUsageService := NewMeterUsageService(s.ServiceParams)
	return meterUsageService.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerID:  internalReq.ExternalCustomerID,
		ExternalCustomerIDs: internalReq.ExternalCustomerIDs,
		FeatureIDs:          internalReq.FeatureIDs,
		StartTime:           internalReq.StartTime,
		EndTime:             internalReq.EndTime,
		GroupBy:             internalReq.GroupBy,
		PropertyFilters:     internalReq.PropertyFilters,
		Sources:             internalReq.Sources,
		WindowSize:          internalReq.WindowSize,
		Expand:              internalReq.Expand,
		IncludeChildren:     internalReq.IncludeChildren,
	})
}

// GetGroupablePropertyKeys returns the visible property keys in the portal customer's usage
func (s *customerPortalService) GetGroupablePropertyKeys(ctx context.Context, req dto.PortalPropertyKeysRequest) (*dto.PropertyKeysResponse, error) {
	// An empty external customer ID would widen the lookup to the whole tenant.
	if types.GetCustomerID(ctx) == "" || types.GetExternalCustomerID(ctx) == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	props, err := s.usageProperties(ctx)
	if err != nil {
		return nil, err
	}
	if props == nil || !props.Enabled {
		return &dto.PropertyKeysResponse{Keys: []string{}}, nil
	}

	keys, err := NewMeterUsageService(s.ServiceParams).GetGroupablePropertyKeys(ctx, &events.PropertyLookupParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: types.GetExternalCustomerID(ctx),
		FeatureIDs:         req.FeatureIDs,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
	})
	if err != nil {
		return nil, err
	}

	return &dto.PropertyKeysResponse{
		Keys: lo.Filter(keys, func(key string, _ int) bool { return visiblePropertyKey(key, props, nil) }),
	}, nil
}

// GetPropertyValues rejects keys hidden from portal users
func (s *customerPortalService) GetPropertyValues(ctx context.Context, req dto.PortalPropertyValuesRequest) (*dto.PropertyValuesResponse, error) {
	// An empty external customer ID would widen the lookup to the whole tenant.
	if types.GetCustomerID(ctx) == "" || types.GetExternalCustomerID(ctx) == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	// Same checks as grouping and filtering, aggregation keys included.
	if err := s.validatePropertyKeys(ctx, dto.PortalAnalyticsRequest{
		FeatureIDs:      req.FeatureIDs,
		StartTime:       req.StartTime,
		EndTime:         req.EndTime,
		PropertyFilters: map[string][]string{req.Key: nil},
	}); err != nil {
		return nil, err
	}

	values, err := NewMeterUsageService(s.ServiceParams).GetPropertyValues(ctx, &events.PropertyLookupParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: types.GetExternalCustomerID(ctx),
		FeatureIDs:         req.FeatureIDs,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
		Key:                req.Key,
		Prefix:             req.Prefix,
		Limit:              req.Limit,
	})
	if err != nil {
		return nil, err
	}

	return &dto.PropertyValuesResponse{Values: values}, nil
}

// GetCostAnalytics returns cost analytics for the portal customer
func (s *customerPortalService) GetCostAnalytics(ctx context.Context, req dto.PortalCostAnalyticsRequest) (*dto.GetDetailedCostAnalyticsResponse, error) {
	customerID := types.GetCustomerID(ctx)
	externalCustomerID := types.GetExternalCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}

	if err := s.validatePropertyKeys(ctx, dto.PortalAnalyticsRequest{
		FeatureIDs:      req.FeatureIDs,
		StartTime:       req.StartTime,
		EndTime:         req.EndTime,
		PropertyFilters: req.PropertyFilters,
	}); err != nil {
		return nil, err
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
	return invoiceService.GetInvoicePDFUrl(ctx, dto.InvoicePDFRequest{InvoiceID: invoiceID})
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

// validatePropertyKeys rejects property group-by and filter keys hidden from portal users.
func (s *customerPortalService) validatePropertyKeys(ctx context.Context, req dto.PortalAnalyticsRequest) error {
	keys := lo.Keys(req.PropertyFilters)
	for _, g := range req.GroupBy {
		if name, ok := strings.CutPrefix(g, "properties."); ok {
			keys = append(keys, name)
		}
	}
	if len(keys) == 0 {
		return nil
	}

	props, err := s.usageProperties(ctx)
	if err != nil {
		return err
	}

	// Scoped to the customer's meters (the features', or those with usage in the window), never the whole tenant.
	lookup := &events.PropertyLookupParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: types.GetExternalCustomerID(ctx),
		FeatureIDs:         req.FeatureIDs,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
	}
	// Same defaults as GetDetailedAnalytics for a request without a window.
	if lookup.EndTime.IsZero() {
		lookup.EndTime = types.Now()
	}
	if lookup.StartTime.IsZero() {
		lookup.StartTime = lookup.EndTime.Add(-6 * time.Hour)
	}
	meterIDs, err := resolvePropertyLookupMeters(ctx, s.ServiceParams, lookup)
	if err != nil {
		return err
	}

	aggregationKeys, err := aggregationPropertyKeys(ctx, s.MeterRepo, meterIDs)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if !visiblePropertyKey(key, props, aggregationKeys) {
			return ierr.NewError("property key is not available").
				WithHintf("property key %q is not available", key).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// usageProperties returns the tenant's portal usage-property config, or nil when unset.
func (s *customerPortalService) usageProperties(ctx context.Context) (*types.CustomerPortalUsageProperties, error) {
	cfg, err := GetSetting[types.CustomerPortalConfig](&settingsService{ServiceParams: s.ServiceParams}, ctx, types.SettingKeyCustomerPortalConfig)
	if err != nil {
		return nil, err
	}
	if cfg.AnalyticsConfig == nil {
		return nil, nil
	}
	return cfg.AnalyticsConfig.UsageProperties, nil
}

// visiblePropertyKey reports whether portal users may group or filter by the property key.
func visiblePropertyKey(key string, cfg *types.CustomerPortalUsageProperties, aggregationKeys map[string]struct{}) bool {
	if _, ok := aggregationKeys[key]; ok {
		return false
	}

	if cfg == nil || !cfg.Enabled {
		return false
	}
	if len(cfg.AllowedKeys) > 0 && !slices.Contains(cfg.AllowedKeys, key) {
		return false
	}

	return !slices.Contains(cfg.HiddenKeys, key)
}
