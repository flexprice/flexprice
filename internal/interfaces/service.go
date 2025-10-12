package interfaces

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CustomerService defines the interface for customer operations
type CustomerService interface {
	CreateCustomer(ctx context.Context, req dto.CreateCustomerRequest) (*dto.CustomerResponse, error)
	GetCustomer(ctx context.Context, id string) (*dto.CustomerResponse, error)
	GetCustomers(ctx context.Context, filter *types.CustomerFilter) (*dto.ListCustomersResponse, error)
	UpdateCustomer(ctx context.Context, id string, req dto.UpdateCustomerRequest) (*dto.CustomerResponse, error)
	DeleteCustomer(ctx context.Context, id string) error
	GetCustomerByLookupKey(ctx context.Context, lookupKey string) (*dto.CustomerResponse, error)
}

// PaymentService defines the interface for payment operations
type PaymentService interface {
	CreatePayment(ctx context.Context, req *dto.CreatePaymentRequest) (*dto.PaymentResponse, error)
	GetPayment(ctx context.Context, id string) (*dto.PaymentResponse, error)
	ListPayments(ctx context.Context, filter *types.PaymentFilter) (*dto.ListPaymentsResponse, error)
	UpdatePayment(ctx context.Context, id string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error)
	DeletePayment(ctx context.Context, id string) error
}

// InvoiceService defines the interface for invoice operations
type InvoiceService interface {
	CreateInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error)
	GetInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error)
	ListInvoices(ctx context.Context, filter *types.InvoiceFilter) (*dto.ListInvoicesResponse, error)
	UpdateInvoice(ctx context.Context, id string, req dto.UpdateInvoiceRequest) (*dto.InvoiceResponse, error)
	DeleteInvoice(ctx context.Context, id string) error
	ReconcilePaymentStatus(ctx context.Context, invoiceID string, paymentStatus types.PaymentStatus, paymentAmount *decimal.Decimal) error
}

type PlanService interface {
	CreatePlan(ctx context.Context, req dto.CreatePlanRequest) (*dto.CreatePlanResponse, error)
	GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error)
	GetPlans(ctx context.Context, filter *types.PlanFilter) (*dto.ListPlansResponse, error)
	UpdatePlan(ctx context.Context, id string, req dto.UpdatePlanRequest) (*dto.PlanResponse, error)
	DeletePlan(ctx context.Context, id string) error
	SyncPlanPrices(ctx context.Context, id string) (*dto.SyncPlanPricesResponse, error)

	// SyncSubscriptionWithPlanPrices synchronizes a single subscription with plan prices
	// NOTE: This method is primarily intended for internal use and testing.
	// For API handlers, use SyncPlanPrices instead which provides comprehensive
	// synchronization across all subscriptions for a plan.
	SyncSubscriptionWithPlanPrices(params *dto.SubscriptionSyncParams) *dto.SubscriptionSyncResult
}

type EntityIntegrationMappingService interface {
	CreateEntityIntegrationMapping(ctx context.Context, req dto.CreateEntityIntegrationMappingRequest) (*dto.EntityIntegrationMappingResponse, error)
	GetEntityIntegrationMapping(ctx context.Context, id string) (*dto.EntityIntegrationMappingResponse, error)
	GetEntityIntegrationMappings(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (*dto.ListEntityIntegrationMappingsResponse, error)
	UpdateEntityIntegrationMapping(ctx context.Context, id string, req dto.UpdateEntityIntegrationMappingRequest) (*dto.EntityIntegrationMappingResponse, error)
	DeleteEntityIntegrationMapping(ctx context.Context, id string) error
}

type SubscriptionService interface {
	CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error)
	GetSubscription(ctx context.Context, id string) (*dto.SubscriptionResponse, error)
	UpdateSubscription(ctx context.Context, subscriptionID string, req dto.UpdateSubscriptionRequest) (*dto.SubscriptionResponse, error)
	CancelSubscription(ctx context.Context, subscriptionID string, req *dto.CancelSubscriptionRequest) (*dto.CancelSubscriptionResponse, error)
	ActivateIncompleteSubscription(ctx context.Context, subscriptionID string) error
	ListSubscriptions(ctx context.Context, filter *types.SubscriptionFilter) (*dto.ListSubscriptionsResponse, error)
	GetUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error)
	UpdateBillingPeriods(ctx context.Context) (*dto.SubscriptionUpdatePeriodResponse, error)

	// Pause-related methods
	PauseSubscription(ctx context.Context, subscriptionID string, req *dto.PauseSubscriptionRequest) (*dto.PauseSubscriptionResponse, error)
	ResumeSubscription(ctx context.Context, subscriptionID string, req *dto.ResumeSubscriptionRequest) (*dto.ResumeSubscriptionResponse, error)
	GetPause(ctx context.Context, pauseID string) (*subscription.SubscriptionPause, error)
	ListPauses(ctx context.Context, subscriptionID string) (*dto.ListSubscriptionPausesResponse, error)
	CalculatePauseImpact(ctx context.Context, subscriptionID string, req *dto.PauseSubscriptionRequest) (*types.BillingImpactDetails, error)
	CalculateResumeImpact(ctx context.Context, subscriptionID string, req *dto.ResumeSubscriptionRequest) (*types.BillingImpactDetails, error)

	// Schedule-related methods
	CreateSubscriptionSchedule(ctx context.Context, req *dto.CreateSubscriptionScheduleRequest) (*dto.SubscriptionScheduleResponse, error)
	GetSubscriptionSchedule(ctx context.Context, id string) (*dto.SubscriptionScheduleResponse, error)
	GetScheduleBySubscriptionID(ctx context.Context, subscriptionID string) (*dto.SubscriptionScheduleResponse, error)
	UpdateSubscriptionSchedule(ctx context.Context, id string, req *dto.UpdateSubscriptionScheduleRequest) (*dto.SubscriptionScheduleResponse, error)
	AddSchedulePhase(ctx context.Context, scheduleID string, req *dto.AddSchedulePhaseRequest) (*dto.SubscriptionScheduleResponse, error)
	AddSubscriptionPhase(ctx context.Context, subscriptionID string, req *dto.AddSchedulePhaseRequest) (*dto.SubscriptionScheduleResponse, error)

	// Coupon-related methods
	ApplyCouponsToSubscriptionWithLineItems(ctx context.Context, subscriptionID string, subscriptionCoupons []string, lineItemCoupons map[string][]string, lineItems []*subscription.SubscriptionLineItem) error

	ValidateAndFilterPricesForSubscription(ctx context.Context, entityID string, entityType types.PriceEntityType, subscription *subscription.Subscription, workflowType *types.TemporalWorkflowType) ([]*dto.PriceResponse, error)

	// Addon management for subscriptions
	AddAddonToSubscription(ctx context.Context, subscriptionID string, req *dto.AddAddonToSubscriptionRequest) (*addonassociation.AddonAssociation, error)
	RemoveAddonFromSubscription(ctx context.Context, subscriptionID string, addonID string, reason string) error

	// Line item management
	AddSubscriptionLineItem(ctx context.Context, subscriptionID string, req dto.CreateSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error)
	DeleteSubscriptionLineItem(ctx context.Context, lineItemID string, req dto.DeleteSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error)
	UpdateSubscriptionLineItem(ctx context.Context, lineItemID string, req dto.UpdateSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error)

	// Auto-cancellation methods
	ProcessAutoCancellationSubscriptions(ctx context.Context) error
	// Renewal due alert methods
	ProcessSubscriptionRenewalDueAlert(ctx context.Context) error

	// Feature usage tracking
	GetFeatureUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error)
}

type ServiceDependencies struct {
	CustomerService                 CustomerService
	PaymentService                  PaymentService
	InvoiceService                  InvoiceService
	PlanService                     PlanService
	SubscriptionService             SubscriptionService
	EntityIntegrationMappingService EntityIntegrationMappingService
	DB                              postgres.IClient
}
