package payload

import (
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
)

// Services container for all services needed by payload builders
type Services struct {
	InvoiceService      service.InvoiceService
	PlanService         service.PlanService
	PriceService        service.PriceService
	EntitlementService  service.EntitlementService
	FeatureService      service.FeatureService
	SubscriptionService service.SubscriptionService
	WalletService       service.WalletService
	CustomerService     service.CustomerService
	PaymentService      service.PaymentService
	AlertService        service.AlertService
	Sentry              *sentry.Service
}

// NewServices creates a new Services container
func NewServices(
	invoiceService service.InvoiceService,
	planService service.PlanService,
	priceService service.PriceService,
	entitlementService service.EntitlementService,
	featureService service.FeatureService,
	subscriptionService service.SubscriptionService,
	walletService service.WalletService,
	customerService service.CustomerService,
	paymentService service.PaymentService,
	alertService service.AlertService,
	sentry *sentry.Service,
) *Services {
	return &Services{
		InvoiceService:      invoiceService,
		PlanService:         planService,
		PriceService:        priceService,
		EntitlementService:  entitlementService,
		FeatureService:      featureService,
		SubscriptionService: subscriptionService,
		WalletService:       walletService,
		CustomerService:     customerService,
		PaymentService:      paymentService,
		AlertService:        alertService,
		Sentry:              sentry,
	}
}
