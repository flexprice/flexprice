package service

import (
	"encoding/json"
	"testing"

	domainAlert "github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/domain/customer"
	domainSettings "github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/expression"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// UsageAlertGateSuite exercises hasUsageAlertConfigForCustomer — the cheap
// pre-schedule check that keeps the Temporal worker from spinning up for
// customers who have nothing to evaluate. The whole point of the gate is to
// avoid pulling all line items on the far side, so every case that would let
// the worker do useful work must return true, and every idle-customer case
// must return false.
type UsageAlertGateSuite struct {
	testutil.BaseServiceTestSuite
	svc  *meterUsageTrackingService
	cust *customer.Customer
}

func TestUsageAlertGateSuite(t *testing.T) {
	suite.Run(t, new(UsageAlertGateSuite))
}

func (s *UsageAlertGateSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.svc = &meterUsageTrackingService{
		ServiceParams: ServiceParams{
			Logger:                   s.GetLogger(),
			Config:                   s.GetConfig(),
			DB:                       s.GetDB(),
			SubRepo:                  s.GetStores().SubscriptionRepo,
			SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
			AlertRepo:                s.GetStores().AlertRepo,
			AlertLogsRepo:            s.GetStores().AlertLogsRepo,
			CustomerRepo:             s.GetStores().CustomerRepo,
			WalletRepo:               s.GetStores().WalletRepo,
			SettingsRepo:             s.GetStores().SettingsRepo,
			RedisCache:               testutil.NewInMemoryRedis(),
			WebhookPublisher:         s.GetWebhookPublisher(),
		},
		meterUsageRepo:      s.GetStores().MeterUsageRepo,
		expressionEvaluator: expression.NewCELEvaluator(),
	}

	s.cust = &customer.Customer{
		ID:         "cust_gate",
		ExternalID: "ext_cust_gate",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.cust))
}

// -----------------------------------------------------------------------------
// The gate itself.
// -----------------------------------------------------------------------------

func (s *UsageAlertGateSuite) TestGate_NoSubsNoWallets_ReturnsFalse() {
	s.False(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_ActiveSubButNoAlertConfig_ReturnsFalse() {
	s.createSub("sub_1")
	s.False(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_SubScopedAlertConfig_ReturnsTrue() {
	sub := s.createSub("sub_scoped")
	s.createSubAlertSettings("as_sub", sub.ID, true)
	s.True(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_SubScopedAlertConfigDisabled_ReturnsFalse() {
	// Enabled=false rows must not gate the workflow open — the evaluator
	// wouldn't fire them anyway, and letting them count would defeat the
	// OOM-prevention purpose.
	sub := s.createSub("sub_disabled")
	s.createSubAlertSettings("as_disabled", sub.ID, false)
	s.False(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_LineItemScopedAlertConfig_ReturnsTrue() {
	// Line-item and group scopes both live under ParentEntityIDs; verifying
	// one covers the code path for the other.
	sub := s.createSub("sub_with_li")
	s.createLineItemAlertSettings("as_li", "sli_1", sub.ID, true)
	s.True(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_WalletAlertsEnabledPlusWallet_ReturnsTrue() {
	s.enableTenantWalletAlerts()
	s.createWallet("w_1")
	s.True(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_WalletAlertsEnabledButNoWallet_ReturnsFalse() {
	// Tenant flag alone isn't enough — the evaluator no-ops on an empty
	// wallet list, so we skip the schedule.
	s.enableTenantWalletAlerts()
	s.False(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_WalletAlertsDisabledTenant_ReturnsFalse() {
	// Customer has a wallet but tenant flag off. Wallet path irrelevant.
	s.createWallet("w_disabled")
	s.False(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_AlertConfigOnOtherCustomersSub_Isolated() {
	// A different customer's alert-configured sub must not gate our
	// customer's workflow open. This is the multi-tenant isolation guard.
	otherCust := &customer.Customer{
		ID:         "cust_other",
		ExternalID: "ext_cust_other",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), otherCust))
	otherSub := &subscription.Subscription{
		ID:                 "sub_other",
		CustomerID:         otherCust.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), otherSub))
	s.createSubAlertSettings("as_other", otherSub.ID, true)

	// Our customer has zero subs. Gate must not surface the other row.
	s.False(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_NonActiveSubNotConsidered() {
	// A cancelled sub with an enabled alert config still doesn't gate the
	// workflow open — activeSubscriptionIDsForCustomer filters on
	// active+trialing to match the evaluator's own filter.
	sub := &subscription.Subscription{
		ID:                 "sub_cancelled",
		CustomerID:         s.cust.ID,
		SubscriptionStatus: types.SubscriptionStatusCancelled,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	s.createSubAlertSettings("as_cancelled_sub", sub.ID, true)
	s.False(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))
}

func (s *UsageAlertGateSuite) TestGate_NilCustomer_ReturnsFalse() {
	s.False(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), nil))
}

func (s *UsageAlertGateSuite) TestGate_CachesAcrossRepeatCalls() {
	// First call populates the cache; second call must return the same result
	// even after the underlying config is deleted. Proves reads short-circuit
	// through Redis and don't hammer the alert/subscription/wallet repos on
	// every event in a burst.
	sub := s.createSub("sub_cache")
	s.createSubAlertSettings("as_cache", sub.ID, true)

	s.True(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust))

	s.Require().NoError(s.GetStores().AlertRepo.Delete(s.GetContext(), "as_cache"))

	s.True(s.svc.hasUsageAlertConfigForCustomer(s.GetContext(), s.cust),
		"cache should still say true until TTL expires")
}

// -----------------------------------------------------------------------------
// helper fixtures
// -----------------------------------------------------------------------------

func (s *UsageAlertGateSuite) createSub(id string) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 id,
		CustomerID:         s.cust.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	return sub
}

func (s *UsageAlertGateSuite) createSubAlertSettings(id, subID string, enabled bool) {
	as := &domainAlert.AlertSettings{
		ID:         id,
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   subID,
		Enabled:    enabled,
		Config:     &types.AlertSettings{AlertEnabled: lo.ToPtr(enabled)},
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().AlertRepo.Create(s.GetContext(), as))
}

func (s *UsageAlertGateSuite) createLineItemAlertSettings(id, lineItemID, parentSubID string, enabled bool) {
	parent := string(types.AlertEntityTypeSubscription)
	as := &domainAlert.AlertSettings{
		ID:               id,
		EntityType:       types.AlertEntityTypeSubscriptionLineItem,
		EntityID:         lineItemID,
		ParentEntityType: &parent,
		ParentEntityID:   &parentSubID,
		Enabled:          enabled,
		Config:           &types.AlertSettings{AlertEnabled: lo.ToPtr(enabled)},
		BaseModel:        types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().AlertRepo.Create(s.GetContext(), as))
}

func (s *UsageAlertGateSuite) createWallet(id string) {
	w := &wallet.Wallet{
		ID:           id,
		CustomerID:   s.cust.ID,
		WalletStatus: types.WalletStatusActive,
		Balance:      decimal.Zero,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))
}

func (s *UsageAlertGateSuite) enableTenantWalletAlerts() {
	// GetSetting[types.AlertSettings] unmarshals the JSONB Value map into the
	// target struct via GetValue's json round-trip. Round-tripping the whole
	// AlertSettings once at seed time is easier than replicating its field
	// names by hand.
	cfg := types.AlertSettings{AlertEnabled: lo.ToPtr(true)}
	raw, err := json.Marshal(cfg)
	s.Require().NoError(err)
	var value map[string]interface{}
	s.Require().NoError(json.Unmarshal(raw, &value))
	setting := &domainSettings.Setting{
		ID:            "setting_wallet_alerts",
		Key:           types.SettingKeyWalletBalanceAlertConfig,
		Value:         value,
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.Require().NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), setting))
}
