package service

import (
	"github.com/flexprice/flexprice/internal/testutil"
)

// newTestServiceParams builds a fully-wired ServiceParams from the base suite's
// in-memory stores. Prefer this over hand-rolling a partial ServiceParams in each
// suite: services often call sibling services internally, and an unwired repo
// surfaces as a nil-pointer panic deep inside a code path instead of a clear
// test failure.
//
// Fields with no in-memory double yet (S3, TracingSvc, Client, CostSheetRepo,
// CostSheetUsageRepo, ProcessedEventRepo, RawEventRepo, ScheduledTaskRepo,
// WorkflowExecutionRepo, and the pub-subs) are left nil; add a store in
// internal/testutil and wire it here when a test needs one.
func newTestServiceParams(s *testutil.BaseServiceTestSuite) ServiceParams {
	stores := s.GetStores()
	return ServiceParams{
		Logger:        s.GetLogger(),
		Config:        s.GetConfig(),
		DB:            s.GetDB(),
		PDFGenerator:  s.GetPDFGenerator(),
		InMemoryCache: s.GetInMemoryCache(),
		RedisCache:    s.GetRedisCache(),

		AuthRepo:                     stores.AuthRepo,
		UserRepo:                     stores.UserRepo,
		EventRepo:                    stores.EventRepo,
		FeatureUsageRepo:             stores.FeatureUsageRepo,
		MeterUsageRepo:               stores.MeterUsageRepo,
		MeterRepo:                    stores.MeterRepo,
		PriceRepo:                    stores.PriceRepo,
		PriceUnitRepo:                stores.PriceUnitRepo,
		CustomerRepo:                 stores.CustomerRepo,
		PlanRepo:                     stores.PlanRepo,
		SubRepo:                      stores.SubscriptionRepo,
		SubscriptionLineItemRepo:     stores.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        stores.SubscriptionPhaseRepo,
		SubScheduleRepo:              stores.SubscriptionScheduleRepo,
		WalletRepo:                   stores.WalletRepo,
		TenantRepo:                   stores.TenantRepo,
		InvoiceRepo:                  stores.InvoiceRepo,
		InvoiceLineItemRepo:          stores.InvoiceLineItemRepo,
		FeatureRepo:                  stores.FeatureRepo,
		EntitlementRepo:              stores.EntitlementRepo,
		PaymentRepo:                  stores.PaymentRepo,
		SecretRepo:                   stores.SecretRepo,
		EnvironmentRepo:              stores.EnvironmentRepo,
		TaskRepo:                     stores.TaskRepo,
		CreditGrantRepo:              stores.CreditGrantRepo,
		CreditGrantApplicationRepo:   stores.CreditGrantApplicationRepo,
		CreditNoteRepo:               stores.CreditNoteRepo,
		CreditNoteLineItemRepo:       stores.CreditNoteLineItemRepo,
		TaxRateRepo:                  stores.TaxRateRepo,
		TaxAssociationRepo:           stores.TaxAssociationRepo,
		TaxAppliedRepo:               stores.TaxAppliedRepo,
		CouponRepo:                   stores.CouponRepo,
		CouponAssociationRepo:        stores.CouponAssociationRepo,
		CouponApplicationRepo:        stores.CouponApplicationRepo,
		AddonRepo:                    stores.AddonRepo,
		AddonAssociationRepo:         stores.AddonAssociationRepo,
		ConnectionRepo:               stores.ConnectionRepo,
		EntityIntegrationMappingRepo: stores.EntityIntegrationMappingRepo,
		SettingsRepo:                 stores.SettingsRepo,
		AlertLogsRepo:                stores.AlertLogsRepo,
		GroupRepo:                    stores.GroupRepo,
		PlanPriceSyncRepo:            stores.PlanPriceSyncRepo,
		CheckoutSessionRepo:          stores.CheckoutSessionRepo,

		EventPublisher:      s.GetPublisher(),
		WebhookPublisher:    s.GetWebhookPublisher(),
		ProrationCalculator: s.GetCalculator(),
		IntegrationFactory:  s.GetIntegrationFactory(),
	}
}
