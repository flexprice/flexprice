package service

import (
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *BillingEntityServiceSuite) invoiceSvc() *invoiceService {
	return NewInvoiceService(ServiceParams{
		Logger:            s.GetLogger(),
		Config:            s.GetConfig(),
		DB:                s.GetDB(),
		TenantRepo:        s.GetStores().TenantRepo,
		EnvironmentRepo:   s.GetStores().EnvironmentRepo,
		CustomerRepo:      s.GetStores().CustomerRepo,
		InvoiceRepo:       s.GetStores().InvoiceRepo,
		BillingEntityRepo: s.GetStores().BillingEntityRepo,
		SettingsRepo:      s.GetStores().SettingsRepo,
	}).(*invoiceService)
}

func (s *BillingEntityServiceSuite) addCustomer(id string, billingEntityID *string) {
	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, &customer.Customer{
		ID:              id,
		ExternalID:      id,
		BillingEntityID: billingEntityID,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}))
}

func (s *BillingEntityServiceSuite) renameEntity(id, legalName string) {
	ctx := s.GetContext()
	e, err := s.GetStores().BillingEntityRepo.Get(ctx, id)
	s.Require().NoError(err)
	e.LegalName = legalName
	s.Require().NoError(s.GetStores().BillingEntityRepo.Update(ctx, e))
}

func (s *BillingEntityServiceSuite) TestSellerFor() {
	env := types.GetEnvironmentID(s.GetContext())

	s.Run("finalized_keeps_snapshot", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_eu", env, "EU", false)
		s.addCustomer("c1", lo.ToPtr("be_eu"))
		inv := &invoice.Invoice{
			ID:             "inv_1",
			CustomerID:     "c1",
			InvoiceStatus:  types.InvoiceStatusFinalized,
			SellerSnapshot: &types.SellerSnapshot{BillingEntityID: "be_eu", LegalName: "Old EU B.V."},
		}
		s.renameEntity("be_eu", "New EU B.V.")

		got, err := s.invoiceSvc().sellerFor(s.GetContext(), inv)
		s.Require().NoError(err)
		s.Require().NotNil(got)
		s.Equal("Old EU B.V.", got.LegalName)
	})

	s.Run("voided_keeps_snapshot", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_eu", env, "EU", false)
		s.addCustomer("c1", lo.ToPtr("be_eu"))
		inv := &invoice.Invoice{
			ID:             "inv_1",
			CustomerID:     "c1",
			InvoiceStatus:  types.InvoiceStatusVoided,
			SellerSnapshot: &types.SellerSnapshot{BillingEntityID: "be_eu", LegalName: "Old EU B.V."},
		}
		s.renameEntity("be_eu", "New EU B.V.")

		got, err := s.invoiceSvc().sellerFor(s.GetContext(), inv)
		s.Require().NoError(err)
		s.Require().NotNil(got)
		s.Equal("Old EU B.V.", got.LegalName)
	})

	s.Run("issued_before_enable_is_tenant_seller", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_eu", env, "EU", false)
		s.addCustomer("c1", lo.ToPtr("be_eu"))
		inv := &invoice.Invoice{ID: "inv_1", CustomerID: "c1", InvoiceStatus: types.InvoiceStatusFinalized}

		got, err := s.invoiceSvc().sellerFor(s.GetContext(), inv)
		s.Require().NoError(err)
		s.Nil(got)
	})

	s.Run("draft_uses_current_entity", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_eu", env, "EU", false)
		s.addCustomer("c1", lo.ToPtr("be_eu"))
		s.renameEntity("be_eu", "New EU B.V.")
		inv := &invoice.Invoice{ID: "inv_1", CustomerID: "c1", InvoiceStatus: types.InvoiceStatusDraft}

		got, err := s.invoiceSvc().sellerFor(s.GetContext(), inv)
		s.Require().NoError(err)
		s.Require().NotNil(got)
		s.Equal("be_eu", got.BillingEntityID)
		s.Equal("New EU B.V.", got.LegalName)
	})

	s.Run("no_entity_is_tenant_seller", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeOff)
		s.addCustomer("c1", nil)
		inv := &invoice.Invoice{ID: "inv_1", CustomerID: "c1", InvoiceStatus: types.InvoiceStatusDraft}

		got, err := s.invoiceSvc().sellerFor(s.GetContext(), inv)
		s.Require().NoError(err)
		s.Nil(got)
	})
}

func (s *BillingEntityServiceSuite) TestNumberPrefixFor() {
	env := types.GetEnvironmentID(s.GetContext())
	cfg := &types.InvoiceConfig{InvoiceNumberPrefix: "INV"}

	s.Run("entity_prefix", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_eu", env, "EU", false)
		s.addCustomer("c1", lo.ToPtr("be_eu"))

		got, err := s.invoiceSvc().numberPrefixFor(s.GetContext(), &invoice.Invoice{CustomerID: "c1"}, cfg)
		s.Require().NoError(err)
		s.Equal("EU", got)
	})

	s.Run("environment_prefix_without_entity", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeOff)
		s.addCustomer("c1", nil)

		got, err := s.invoiceSvc().numberPrefixFor(s.GetContext(), &invoice.Invoice{CustomerID: "c1"}, cfg)
		s.Require().NoError(err)
		s.Equal("INV", got)
	})
}

func (s *BillingEntityServiceSuite) TestApplySellerSnapshotIdempotent() {
	env := types.GetEnvironmentID(s.GetContext())
	s.ClearStores()
	s.setMode(types.BillingEntityModeEnabled)
	s.addEntity("be_eu", env, "EU", false)
	s.addCustomer("c1", lo.ToPtr("be_eu"))
	inv := &invoice.Invoice{ID: "inv_1", CustomerID: "c1"}
	svc := s.invoiceSvc()

	s.Require().NoError(svc.applySellerSnapshot(s.GetContext(), inv))
	s.Require().NotNil(inv.SellerSnapshot)
	s.Equal("be_eu", inv.SellerSnapshot.LegalName)

	s.renameEntity("be_eu", "Renamed")
	s.Require().NoError(svc.applySellerSnapshot(s.GetContext(), inv))
	s.Equal("be_eu", inv.SellerSnapshot.LegalName)
}
