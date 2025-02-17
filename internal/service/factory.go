package service

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	webhookPublisher "github.com/flexprice/flexprice/internal/webhook/publisher"
	"go.uber.org/fx"
)

// ServiceParams holds common dependencies for services
// TODO: start using this for all services init
type ServiceParams struct {
	fx.In

	Logger *logger.Logger
	Config *config.Configuration
	DB     postgres.IClient

	// Repositories
	AuthRepo        auth.Repository
	UserRepo        user.Repository
	EventRepo       events.Repository
	MeterRepo       meter.Repository
	PriceRepo       price.Repository
	CustomerRepo    customer.Repository
	PlanRepo        plan.Repository
	SubRepo         subscription.Repository
	WalletRepo      wallet.Repository
	TenantRepo      tenant.Repository
	InvoiceRepo     invoice.Repository
	FeatureRepo     feature.Repository
	EntitlementRepo entitlement.Repository
	PaymentRepo     payment.Repository

	// Publishers
	EventPublisher   publisher.EventPublisher
	WebhookPublisher webhookPublisher.WebhookPublisher
}
