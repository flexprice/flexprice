package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type SettingsServiceSuite struct {
	testutil.BaseServiceTestSuite
	service SettingsService
}

func TestSettingsService(t *testing.T) {
	suite.Run(t, new(SettingsServiceSuite))
}

func (s *SettingsServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = s.newService(s.GetConfig().Auth.SAML.Enabled)
}

func (s *SettingsServiceSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_test")
}

func (s *SettingsServiceSuite) newService(samlEnabled bool) SettingsService {
	cfg := *s.GetConfig()
	cfg.Auth.SAML.Enabled = samlEnabled
	return NewSettingsService(ServiceParams{
		Logger:       s.GetLogger(),
		Config:       &cfg,
		DB:           s.GetDB(),
		SettingsRepo: s.GetStores().SettingsRepo,
	})
}

func (s *SettingsServiceSuite) update(svc SettingsService, ctx context.Context, key types.SettingKey, value map[string]interface{}) (*dto.SettingResponse, error) {
	return svc.UpdateSettingByKey(ctx, key, &dto.UpdateSettingRequest{Value: value})
}

// partialUpdateCase sends each request in turn, carrying only the fields it
// changes, then checks the resolved value: fields from earlier requests and
// from defaults must survive alongside the ones just sent.
type partialUpdateCase struct {
	key      types.SettingKey
	requests []map[string]interface{}
	want     map[string]string
	nonEmpty []string
}

func partialUpdateCases() []partialUpdateCase {
	return []partialUpdateCase{
		{
			key: types.SettingKeyInvoiceConfig,
			requests: []map[string]interface{}{
				{"prefix": "BILL", "suffix_length": 7},
				{"include_zero_value_line_items": true},
				{"auto_finalize": false},
			},
			want: map[string]string{
				"auto_finalize":                 "false",
				"prefix":                        "BILL",
				"suffix_length":                 "7",
				"include_zero_value_line_items": "true",
				"format":                        string(types.InvoiceNumberFormatYYYYMM),
				"timezone":                      types.DefaultTimezone,
				"start_sequence":                "1",
			},
		},
		{
			key: types.SettingKeySubscriptionConfig,
			requests: []map[string]interface{}{
				{"auto_cancellation_enabled": true},
				{"grace_period_days": 10},
			},
			want: map[string]string{"auto_cancellation_enabled": "true", "grace_period_days": "10"},
		},
		{
			key:      types.SettingKeyInvoicePDFConfig,
			requests: []map[string]interface{}{{"group_by": []string{"feature_name"}}},
			want:     map[string]string{"group_by": "[feature_name]", "template_name": string(types.TemplateInvoiceDefault)},
		},
		{
			key: types.SettingKeyTenantConfig,
			requests: []map[string]interface{}{
				{"max_users": 25},
				{"production": 1},
			},
			want: map[string]string{"max_users": "25", "production": "1", "development": "2"},
		},
		{
			key:      types.SettingKeyCustomerOnboarding,
			requests: []map[string]interface{}{{"actions": []interface{}{}}},
			want:     map[string]string{"workflow_type": "customer_onboarding"},
		},
		{
			key:      types.SettingKeyPrepareProcessedEvents,
			requests: []map[string]interface{}{{"actions": []interface{}{}}},
			want:     map[string]string{"workflow_type": "prepare_processed_events"},
		},
		{
			key:      types.SettingKeyCustomAnalytics,
			requests: []map[string]interface{}{{"rules": []interface{}{}}},
			want:     map[string]string{"rules": "[]"},
		},
		{
			// Enabling alone passes because the default critical threshold is merged in.
			key:      types.SettingKeyWalletBalanceAlertConfig,
			requests: []map[string]interface{}{{"alert_enabled": true}},
			want:     map[string]string{"alert_enabled": "true", "alert_threshold_type": string(types.AlertThresholdTypeAbsolute)},
			nonEmpty: []string{"critical"},
		},
		{
			key:      types.SettingKeySubscriptionAlertConfig,
			requests: []map[string]interface{}{{"alert_enabled": true}},
			want:     map[string]string{"alert_enabled": "true"},
		},
		{
			key:      types.SettingKeyEntitlementAlertConfig,
			requests: []map[string]interface{}{{"alert_enabled": true}},
			want:     map[string]string{"alert_enabled": "true"},
		},
		{
			key:      types.SettingKeyCustomerPortalConfig,
			requests: []map[string]interface{}{{"version": "2.0"}},
			want:     map[string]string{"version": "2.0"},
			nonEmpty: []string{"sections"},
		},
		{
			key: types.SettingKeyEventIngestionFilter,
			requests: []map[string]interface{}{
				{"allowed_external_customer_ids": []string{"cust_1"}},
				{"enabled": true},
			},
			want: map[string]string{"enabled": "true", "allowed_external_customer_ids": "[cust_1]"},
		},
		{
			key: types.SettingKeyPaymentMandateLimits,
			requests: []map[string]interface{}{
				{"mandate_limits": map[string]interface{}{"UPI": map[string]interface{}{"max_amount": "5000", "currency": "INR"}}},
			},
			want: map[string]string{"mandate_limits": "map[UPI:map[currency:INR max_amount:5000]]"},
		},
		{
			// Enabling alone passes because the stored slabs are merged in.
			key: types.SettingKeyBonusCreditsTopupConfig,
			requests: []map[string]interface{}{
				{"slabs": []map[string]interface{}{
					{"threshold": "100", "operator": "gte", "bonus": map[string]interface{}{"type": "flat", "value": "50"}},
				}},
				{"enabled": true},
			},
			want:     map[string]string{"enabled": "true"},
			nonEmpty: []string{"slabs"},
		},
		{
			key:      types.SettingKeyDraftInvoiceRecomputeConfig,
			requests: []map[string]interface{}{{"enabled": true}},
			want:     map[string]string{"enabled": "true"},
		},
		{
			// Both fields are API-immutable, so the request is accepted and ignored.
			key:      types.SettingKeyUsageAlertConfig,
			requests: []map[string]interface{}{{"schedule_delay_seconds": 60}},
			want:     map[string]string{"schedule_delay_seconds": "0", "stale_after_seconds": "0"},
		},
		{
			key:      types.SettingKeyWalletTopupConfig,
			requests: []map[string]interface{}{{"free_credit_limit_per_transaction": "500"}},
			want:     map[string]string{"free_credit_limit_per_transaction": "500", "min_topup_amount_per_currency": "map[USD:1]"},
		},
		{
			key: types.SettingKeyCustomCurrencyConfig,
			requests: []map[string]interface{}{
				{"default_fiat_currency": "usd"},
				{"custom_currencies": map[string]interface{}{
					"pts": map[string]interface{}{"name": "Points", "symbol": "P", "fiat_conversion_factors": map[string]interface{}{"usd": "0.01"}},
				}},
			},
			want:     map[string]string{"default_fiat_currency": "usd"},
			nonEmpty: []string{"custom_currencies"},
		},
		{
			// Enabling alone passes because the stored provider is merged in.
			key: types.SettingKeyTaxConfig,
			requests: []map[string]interface{}{
				{"provider": string(types.TaxProviderStripe)},
				{"enabled": true},
			},
			want: map[string]string{"enabled": "true", "provider": string(types.TaxProviderStripe)},
		},
		{
			key:      types.SettingKeyRevenueRecognitionConfig,
			requests: []map[string]interface{}{{"enabled": true}},
			want:     map[string]string{"enabled": "true"},
		},
		{
			key:      types.SettingKeyEmailNotificationConfig,
			requests: []map[string]interface{}{{"attach_invoice_pdf": false}},
			want: map[string]string{
				"attach_invoice_pdf": "false",
				"enabled":            "false",
				"auto_send":          "map[enabled:false entities:[invoice]]",
			},
		},
	}
}

func (s *SettingsServiceSuite) TestUpdateSettingByKey_PartialUpdate() {
	for _, tc := range partialUpdateCases() {
		s.Run(string(tc.key), func() {
			for i, value := range tc.requests {
				_, err := s.update(s.service, s.GetContext(), tc.key, value)
				s.Require().NoError(err, "request %d: %v", i+1, value)
			}

			resp, err := s.service.GetSettingByKey(s.GetContext(), tc.key)
			s.Require().NoError(err)

			for field, want := range tc.want {
				s.Equal(want, fmt.Sprint(resp.Value[field]), "field %q", field)
			}
			for _, field := range tc.nonEmpty {
				s.NotEmpty(resp.Value[field], "field %q", field)
			}
		})
	}
}

// Every key the settings API serves must have a partial update case above.
func (s *SettingsServiceSuite) TestUpdateSettingByKey_PartialUpdateCoversEveryKey() {
	covered := map[types.SettingKey]bool{
		types.SettingKeySAMLConfig:          true, // TestUpdateSettingByKey_SAMLPartialUpdate
		types.SettingKeyBillingEntityConfig: true, // TestUpdateSettingByKey_BillingEntityConfigRejected
	}
	for _, tc := range partialUpdateCases() {
		covered[tc.key] = true
	}

	defaults, err := types.GetDefaultSettings()
	s.Require().NoError(err)
	for key := range defaults {
		// Not dispatched by UpdateSettingByKey, so it cannot be written through the settings API.
		if key == types.SettingKeyRevenueAnalyticsConfig {
			continue
		}
		s.True(covered[key], "setting %q has no partial update case", key)
	}
}

func (s *SettingsServiceSuite) TestUpdateSettingByKey_SAMLPartialUpdate() {
	// The real validator lives in the saml package, which imports this one, so a
	// stand-in enforcing one of its rules checks that the merged value reaches it.
	types.RegisterSAMLConfigValidator(func(value map[string]interface{}) error {
		if value["enabled"] == true && value["idp_sso_url"] == "" {
			return ierr.NewError("idp_sso_url is required when SAML is enabled").Mark(ierr.ErrValidation)
		}
		return nil
	})
	defer types.RegisterSAMLConfigValidator(nil)

	svc := s.newService(true)
	ctx := context.WithValue(s.GetContext(), types.CtxRoles, []string{types.RoleSuperAdmin.String()})

	_, err := s.update(svc, ctx, types.SettingKeySAMLConfig, map[string]interface{}{"enabled": true})
	s.Require().Error(err, "enabling with no identity provider stored must fail")
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)

	_, err = s.update(svc, ctx, types.SettingKeySAMLConfig, map[string]interface{}{
		"idp_entity_id":   "https://idp.example.com",
		"idp_sso_url":     "https://idp.example.com/sso",
		"idp_certificate": "cert",
	})
	s.Require().NoError(err)

	resp, err := s.update(svc, ctx, types.SettingKeySAMLConfig, map[string]interface{}{"enabled": true})
	s.Require().NoError(err, "enabling alone must pass once the identity provider is stored")
	s.Equal(true, resp.Value["enabled"])
	s.Equal("https://idp.example.com/sso", resp.Value["idp_sso_url"])
	s.Equal(false, resp.Value["active"])
}

// billing_entity_config is read-only through the settings API, partial or not.
func (s *SettingsServiceSuite) TestUpdateSettingByKey_BillingEntityConfigRejected() {
	_, err := s.update(s.service, s.GetContext(), types.SettingKeyBillingEntityConfig, map[string]interface{}{"mode": "single"})
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
}

// Validation still applies to the merged value: an invalid field sent on its
// own is rejected.
func (s *SettingsServiceSuite) TestUpdateSettingByKey_InvalidFieldRejected() {
	_, err := s.update(s.service, s.GetContext(), types.SettingKeyInvoiceConfig, map[string]interface{}{"format": "MMYYYY"})
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
}

func (s *SettingsServiceSuite) TestUpdateSettingByKey_NilValueRejected() {
	_, err := s.update(s.service, s.GetContext(), types.SettingKeyInvoiceConfig, nil)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
}
