package service

import (
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

func (s *BillingEntityServiceSuite) renameEntity(id, name string) {
	ctx := s.GetContext()
	e, err := s.GetStores().BillingEntityRepo.Get(ctx, id)
	s.Require().NoError(err)
	e.Name = name
	s.Require().NoError(s.GetStores().BillingEntityRepo.Update(ctx, e))
}

func (s *BillingEntityServiceSuite) TestGetInvoiceBillingEntity() {
	env := types.GetEnvironmentID(s.GetContext())

	s.Run("finalized_keeps_snapshot", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_eu", env, "EU", false)
		inv := &invoice.Invoice{
			ID:            "inv_1",
			CustomerID:    "c1",
			InvoiceStatus: types.InvoiceStatusFinalized,
			BillingEntity: &types.InvoiceBillingEntity{BillingEntityID: "be_eu", Name: "Old EU B.V."},
		}
		s.renameEntity("be_eu", "New EU B.V.")

		got, err := s.invoiceSvc().GetInvoiceBillingEntity(s.GetContext(), inv)
		s.Require().NoError(err)
		s.Require().NotNil(got)
		s.Equal("Old EU B.V.", got.Name)
	})

	s.Run("voided_keeps_snapshot", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_eu", env, "EU", false)
		inv := &invoice.Invoice{
			ID:            "inv_1",
			CustomerID:    "c1",
			InvoiceStatus: types.InvoiceStatusVoided,
			BillingEntity: &types.InvoiceBillingEntity{BillingEntityID: "be_eu", Name: "Old EU B.V."},
		}
		s.renameEntity("be_eu", "New EU B.V.")

		got, err := s.invoiceSvc().GetInvoiceBillingEntity(s.GetContext(), inv)
		s.Require().NoError(err)
		s.Require().NotNil(got)
		s.Equal("Old EU B.V.", got.Name)
	})

	s.Run("issued_before_enable_bills_as_tenant", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_eu", env, "EU", false)
		inv := &invoice.Invoice{ID: "inv_1", CustomerID: "c1", InvoiceStatus: types.InvoiceStatusFinalized, BillingEntityID: lo.ToPtr("be_eu")}

		got, err := s.invoiceSvc().GetInvoiceBillingEntity(s.GetContext(), inv)
		s.Require().NoError(err)
		s.Nil(got)
	})

	s.Run("draft_uses_current_entity", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_eu", env, "EU", false)
		s.renameEntity("be_eu", "New EU B.V.")
		inv := &invoice.Invoice{ID: "inv_1", CustomerID: "c1", InvoiceStatus: types.InvoiceStatusDraft, BillingEntityID: lo.ToPtr("be_eu")}

		got, err := s.invoiceSvc().GetInvoiceBillingEntity(s.GetContext(), inv)
		s.Require().NoError(err)
		s.Require().NotNil(got)
		s.Equal("be_eu", got.BillingEntityID)
		s.Equal("New EU B.V.", got.Name)
	})

	s.Run("no_entity_bills_as_tenant", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeOff)
		inv := &invoice.Invoice{ID: "inv_1", CustomerID: "c1", InvoiceStatus: types.InvoiceStatusDraft}

		got, err := s.invoiceSvc().GetInvoiceBillingEntity(s.GetContext(), inv)
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

		got, err := s.invoiceSvc().numberPrefixFor(s.GetContext(), &invoice.Invoice{CustomerID: "c1", BillingEntityID: lo.ToPtr("be_eu")}, cfg)
		s.Require().NoError(err)
		s.Equal("EU", got)
	})

	s.Run("environment_prefix_without_entity", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeOff)

		got, err := s.invoiceSvc().numberPrefixFor(s.GetContext(), &invoice.Invoice{CustomerID: "c1"}, cfg)
		s.Require().NoError(err)
		s.Equal("INV", got)
	})
}

func (s *BillingEntityServiceSuite) TestFreezeBillingEntityIdempotent() {
	env := types.GetEnvironmentID(s.GetContext())
	s.ClearStores()
	s.setMode(types.BillingEntityModeEnabled)
	s.addEntity("be_eu", env, "EU", false)
	inv := &invoice.Invoice{ID: "inv_1", CustomerID: "c1", BillingEntityID: lo.ToPtr("be_eu")}
	svc := s.invoiceSvc()

	s.Require().NoError(svc.freezeBillingEntity(s.GetContext(), inv))
	s.Require().NotNil(inv.BillingEntity)
	s.Equal("be_eu", inv.BillingEntity.Name)

	s.renameEntity("be_eu", "Renamed")
	s.Require().NoError(svc.freezeBillingEntity(s.GetContext(), inv))
	s.Equal("be_eu", inv.BillingEntity.Name)
}
