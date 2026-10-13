package payload

import (
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/tracing"
)

// Services container for all services needed by payload builders.
//
// Tracing carries the OTel tracer + Sentry error-capture sink — callers that
// previously read .Sentry should use .Tracing. The field name is kept short
// because builders only use it for CaptureException today.
type Services struct {
	InvoiceService                  service.InvoiceService
	PlanService                     service.PlanService
	PriceService                    service.PriceService
	EntitlementService              service.EntitlementService
	FeatureService                  service.FeatureService
	SubscriptionService             service.SubscriptionService
	SubscriptionPhaseService        service.SubscriptionPhaseService
	WalletService                   service.WalletService
	CustomerService                 service.CustomerService
	PaymentService                  service.PaymentService
	Tracing                         *tracing.Service
	CreditNoteService               service.CreditNoteService
	RefundService                   service.RefundService
	CheckoutSessionService          interfaces.CheckoutSessionService
	GroupService                    service.GroupService
	EntitlementGrantSvc             service.EntitlementGrantService
	EntityIntegrationMappingService service.EntityIntegrationMappingService
}

// NewServices creates a new Services container
func NewServices(
	invoiceService service.InvoiceService,
	planService service.PlanService,
	priceService service.PriceService,
	entitlementService service.EntitlementService,
	featureService service.FeatureService,
	subscriptionService service.SubscriptionService,
	subscriptionPhaseService service.SubscriptionPhaseService,
	walletService service.WalletService,
	customerService service.CustomerService,
	paymentService service.PaymentService,
	tracingSvc *tracing.Service,
	creditNoteService service.CreditNoteService,
	refundService service.RefundService,
	checkoutSessionService interfaces.CheckoutSessionService,
	groupService service.GroupService,
	entitlementGrantSvc service.EntitlementGrantService,
	entityIntegrationMappingService service.EntityIntegrationMappingService,
) *Services {
	return &Services{
		InvoiceService:                  invoiceService,
		PlanService:                     planService,
		PriceService:                    priceService,
		EntitlementService:              entitlementService,
		FeatureService:                  featureService,
		SubscriptionService:             subscriptionService,
		SubscriptionPhaseService:        subscriptionPhaseService,
		WalletService:                   walletService,
		CustomerService:                 customerService,
		PaymentService:                  paymentService,
		Tracing:                         tracingSvc,
		CreditNoteService:               creditNoteService,
		RefundService:                   refundService,
		CheckoutSessionService:          checkoutSessionService,
		GroupService:                    groupService,
		EntitlementGrantSvc:             entitlementGrantSvc,
		EntityIntegrationMappingService: entityIntegrationMappingService,
	}
}
