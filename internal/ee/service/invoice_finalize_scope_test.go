package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *InvoiceConversionSuite) TestFinalizeInvoiceScopesOwnedStripeTax() {
	ctx := s.ctx()
	st := s.GetStores()
	s.Require().NoError(st.SettingsRepo.Create(ctx, &settings.Setting{
		ID: "setting_entities", Key: types.SettingKeyBillingEntityConfig,
		Value:         map[string]interface{}{"mode": string(types.BillingEntityModeEnabled)},
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	s.Require().NoError(st.SettingsRepo.Create(ctx, &settings.Setting{
		ID: "setting_tax", Key: types.SettingKeyTaxConfig,
		Value:         map[string]interface{}{"enabled": true, "provider": "stripe"},
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	s.Require().NoError(st.BillingEntityRepo.Create(ctx, &billingentity.BillingEntity{
		ID: "be_stripe", Name: "Stripe entity", InvoiceNumberPrefix: "STR",
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	s.seedCustomer("cust_scoped", nil)
	cust, err := st.CustomerRepo.Get(ctx, "cust_scoped")
	s.Require().NoError(err)
	cust.BillingEntityID = lo.ToPtr("be_stripe")
	cust.Metadata = types.Metadata{"stripe_customer_id": "cus_scoped"}
	s.Require().NoError(st.CustomerRepo.Update(ctx, cust))
	s.Require().NoError(st.ConnectionRepo.Create(ctx, &connection.Connection{
		ID: "conn_scoped", Name: "Stripe", ProviderType: types.SecretProviderStripe,
		BillingEntityID: lo.ToPtr("be_stripe"),
		EnvironmentID:   types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	s.Require().NoError(st.SubscriptionRepo.Create(ctx, &subscription.Subscription{
		ID: "sub_scoped", CustomerID: cust.ID, Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	inv := s.seedDraftInvoice("inv_scoped", cust.ID, "usd", types.InvoiceTypeSubscription,
		lo.ToPtr("sub_scoped"), []*invoice.InvoiceLineItem{line("il_scoped", "100")})
	params := s.svc.ServiceParams
	params.BillingEntityRepo = st.BillingEntityRepo
	params.IntegrationFactory = s.GetIntegrationFactory()
	svc := NewInvoiceService(params)

	response, err := json.Marshal(calculationWith("taxcalc_scoped", 0, 1800,
		breakdown(1800, 10000, "standard_rated")))
	s.Require().NoError(err)
	previous := http.DefaultTransport
	s.T().Cleanup(func() { http.DefaultTransport = previous })
	var paths []string
	http.DefaultTransport = finalizeTaxTransport(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		body := string(response)
		switch req.URL.Path {
		case "/v1/tax/calculations":
			s.NoError(req.ParseForm())
			s.Equal("usd", req.Form.Get("currency"))
			s.Equal("10000", req.Form.Get("line_items[0][amount]"))
		case "/v1/tax/transactions/create_from_calculation":
			s.NoError(req.ParseForm())
			s.Equal("taxcalc_scoped", req.Form.Get("calculation"))
			body = `{"id":"tax_transaction_scoped"}`
		default:
			s.Fail("unexpected Stripe request", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s.Empty(types.GetBillingEntityID(ctx))
	s.Require().NoError(svc.FinalizeInvoice(ctx, inv.ID, dto.FinalizeInvoiceRequest{}))
	s.Equal([]string{"/v1/tax/calculations", "/v1/tax/transactions/create_from_calculation"}, paths)
	stored, err := st.InvoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
	s.True(dec("18").Equal(stored.TotalTax))
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = inv.ID
	rows, err := st.TaxAppliedRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(rows, 1)
	s.Equal("tax_transaction_scoped", lo.FromPtr(rows[0].TaxTransactionID))
}

type finalizeTaxTransport func(*http.Request) (*http.Response, error)

func (f finalizeTaxTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
