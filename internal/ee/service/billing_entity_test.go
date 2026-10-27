package service

import (
	"testing"

	"github.com/cockroachdb/errors"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type BillingEntityServiceSuite struct {
	testutil.BaseServiceTestSuite
	service BillingEntityService
}

func TestBillingEntityService(t *testing.T) {
	suite.Run(t, new(BillingEntityServiceSuite))
}

func (s *BillingEntityServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.WithEnvironment("env_test")
	s.service = NewBillingEntityService(ServiceParams{
		Logger:             s.GetLogger(),
		Config:             s.GetConfig(),
		DB:                 s.GetDB(),
		TenantRepo:         s.GetStores().TenantRepo,
		CustomerRepo:       s.GetStores().CustomerRepo,
		InvoiceRepo:        s.GetStores().InvoiceRepo,
		SubRepo:            s.GetStores().SubscriptionRepo,
		BillingEntityRepo:  s.GetStores().BillingEntityRepo,
		SettingsRepo:       s.GetStores().SettingsRepo,
		TaxRateRepo:        s.GetStores().TaxRateRepo,
		TaxAssociationRepo: s.GetStores().TaxAssociationRepo,
		ConnectionRepo:     s.GetStores().ConnectionRepo,
	})
}

func (s *BillingEntityServiceSuite) setMode(mode types.BillingEntityMode) {
	ctx := s.GetContext()
	t := &tenant.Tenant{ID: types.GetTenantID(ctx), Name: "Acme", Status: types.StatusPublished}
	_ = s.GetStores().TenantRepo.Create(ctx, t)
	s.Require().NoError(s.GetStores().TenantRepo.SetBillingEntityMode(ctx, t.ID, mode))
}

func (s *BillingEntityServiceSuite) addEntity(id, environmentID, prefix string, isDefault bool) *billingentity.BillingEntity {
	ctx := s.GetContext()
	e := &billingentity.BillingEntity{
		ID:                  id,
		Name:                id,
		LegalName:           id,
		IsDefault:           isDefault,
		InvoiceNumberPrefix: prefix,
		EnvironmentID:       environmentID,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().BillingEntityRepo.Create(ctx, e))
	return e
}

func (s *BillingEntityServiceSuite) TestResolveForCustomerCreate() {
	env := types.GetEnvironmentID(s.GetContext())

	tests := []struct {
		name      string
		mode      types.BillingEntityMode
		requested *string
		wantID    *string
		wantErr   error
	}{
		{name: "off_none", mode: types.BillingEntityModeOff, wantID: nil},
		{name: "off_requested", mode: types.BillingEntityModeOff, requested: lo.ToPtr("be_eu"), wantErr: ierr.ErrBillingEntitiesNotEnabled},
		{name: "enabling_none", mode: types.BillingEntityModeEnabling, wantID: lo.ToPtr("be_default")},
		{name: "enabling_requested", mode: types.BillingEntityModeEnabling, requested: lo.ToPtr("be_eu"), wantErr: ierr.ErrBillingEntitiesNotEnabled},
		{name: "enabled_none", mode: types.BillingEntityModeEnabled, wantID: lo.ToPtr("be_default")},
		{name: "enabled_same_env", mode: types.BillingEntityModeEnabled, requested: lo.ToPtr("be_eu"), wantID: lo.ToPtr("be_eu")},
		{name: "enabled_other_env", mode: types.BillingEntityModeEnabled, requested: lo.ToPtr("be_other"), wantErr: ierr.ErrValidation},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.ClearStores()
			s.setMode(tc.mode)
			if tc.mode != types.BillingEntityModeOff {
				s.addEntity("be_default", env, "INV", true)
				s.addEntity("be_eu", env, "EU", false)
				s.addEntity("be_other", "env_other", "OT", false)
			}

			got, err := s.service.ResolveForCustomerCreate(s.GetContext(), tc.mode, tc.requested)
			if tc.wantErr != nil {
				s.Require().Error(err)
				s.True(errors.Is(err, tc.wantErr), "got %v", err)
				return
			}
			s.Require().NoError(err)
			s.Equal(tc.wantID, got)
		})
	}
}

func (s *BillingEntityServiceSuite) TestEffectiveEntity() {
	env := types.GetEnvironmentID(s.GetContext())

	s.Run("stored_entity", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabled)
		s.addEntity("be_default", env, "INV", true)
		eu := s.addEntity("be_eu", env, "EU", false)

		got, err := s.service.EffectiveEntity(s.GetContext(), &customer.Customer{ID: "c1", BillingEntityID: lo.ToPtr(eu.ID)})
		s.Require().NoError(err)
		s.Require().NotNil(got)
		s.Equal("be_eu", got.ID)
	})

	s.Run("unassigned_during_enabling_is_default", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeEnabling)
		s.addEntity("be_default", env, "INV", true)

		got, err := s.service.EffectiveEntity(s.GetContext(), &customer.Customer{ID: "c1"})
		s.Require().NoError(err)
		s.Require().NotNil(got)
		s.Equal("be_default", got.ID)
	})

	s.Run("off_tenant_has_none", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeOff)

		got, err := s.service.EffectiveEntity(s.GetContext(), &customer.Customer{ID: "c1"})
		s.Require().NoError(err)
		s.Nil(got)
	})
}

func (s *BillingEntityServiceSuite) TestCustomerServesIntegrations() {
	env := types.GetEnvironmentID(s.GetContext())

	tests := []struct {
		name     string
		mode     types.BillingEntityMode
		customer *string
		want     bool
	}{
		{name: "off_no_entity", mode: types.BillingEntityModeOff, want: true},
		{name: "stored_default", mode: types.BillingEntityModeEnabled, customer: lo.ToPtr("be_default"), want: true},
		{name: "stored_non_default", mode: types.BillingEntityModeEnabled, customer: lo.ToPtr("be_eu"), want: false},
		{name: "unassigned_while_enabling", mode: types.BillingEntityModeEnabling, want: true},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.ClearStores()
			s.setMode(tc.mode)
			if tc.mode != types.BillingEntityModeOff {
				s.addEntity("be_default", env, "INV", true)
				s.addEntity("be_eu", env, "EU", false)
			}
			s.addCustomer("c1", tc.customer)

			got, err := s.service.CustomerServesIntegrations(s.GetContext(), "c1")
			s.Require().NoError(err)
			s.Equal(tc.want, got)
		})
	}
}

func (s *BillingEntityServiceSuite) TestSubscriptionServesIntegrations() {
	env := types.GetEnvironmentID(s.GetContext())
	s.ClearStores()
	s.setMode(types.BillingEntityModeEnabled)
	s.addEntity("be_default", env, "INV", true)
	s.addEntity("be_eu", env, "EU", false)
	s.addCustomer("c_eu", lo.ToPtr("be_eu"))
	s.addCustomer("c_default", lo.ToPtr("be_default"))

	ctx := s.GetContext()
	for id, customerID := range map[string]string{"sub_eu": "c_eu", "sub_default": "c_default"} {
		s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, &subscription.Subscription{
			ID:            id,
			CustomerID:    customerID,
			EnvironmentID: env,
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}))
	}

	served, err := s.service.SubscriptionServesIntegrations(ctx, "sub_eu")
	s.Require().NoError(err)
	s.False(served)

	served, err = s.service.SubscriptionServesIntegrations(ctx, "sub_default")
	s.Require().NoError(err)
	s.True(served)
}
