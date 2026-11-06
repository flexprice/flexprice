package service

import (
	"testing"

	"github.com/cockroachdb/errors"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/environment"
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
		EnvironmentRepo:    s.GetStores().EnvironmentRepo,
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
	env := &environment.Environment{ID: types.GetEnvironmentID(ctx), Name: "Test", Type: types.EnvironmentDevelopment}
	_ = s.GetStores().EnvironmentRepo.Create(ctx, env)
	s.Require().NoError(s.service.(*billingEntityService).setMode(ctx, mode))
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

func (s *BillingEntityServiceSuite) addEntity(id, environmentID, prefix string, isDefault bool) *billingentity.BillingEntity {
	ctx := s.GetContext()
	e := &billingentity.BillingEntity{
		ID:                  id,
		Name:                id,
		IsDefault:           isDefault,
		InvoiceNumberPrefix: prefix,
		EnvironmentID:       environmentID,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().BillingEntityRepo.Create(ctx, e))
	return e
}

func (s *BillingEntityServiceSuite) TestResolveBillingEntityID() {
	env := types.GetEnvironmentID(s.GetContext())

	tests := []struct {
		name      string
		mode      types.BillingEntityMode
		requested *string
		wantID    *string
		wantErr   error
	}{
		{name: "off_none", mode: types.BillingEntityModeOff, wantID: nil},
		{name: "off_requested", mode: types.BillingEntityModeOff, requested: lo.ToPtr("be_eu"), wantErr: billingentity.ErrNotEnabled},
		{name: "enabling_none", mode: types.BillingEntityModeEnabling, wantID: lo.ToPtr("be_default")},
		{name: "enabling_requested", mode: types.BillingEntityModeEnabling, requested: lo.ToPtr("be_eu"), wantErr: billingentity.ErrNotEnabled},
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

			got, err := s.service.ResolveBillingEntityID(s.GetContext(), tc.mode, tc.requested)
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

func (s *BillingEntityServiceSuite) TestResolveConnectionOwner() {
	env := types.GetEnvironmentID(s.GetContext())

	tests := []struct {
		name         string
		requested    *string
		connectionID string
		wantID       *string
		wantErr      error
	}{
		{name: "no_id_no_entity", wantID: lo.ToPtr("be_default")},
		{name: "no_id_default_entity", requested: lo.ToPtr("be_default"), wantID: lo.ToPtr("be_default")},
		{name: "no_id_other_entity", requested: lo.ToPtr("be_eu"), wantErr: ierr.ErrValidation},
		{name: "id_other_entity", requested: lo.ToPtr("be_eu"), connectionID: "conn_01J0000000000000000000000A", wantID: lo.ToPtr("be_eu")},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.ClearStores()
			s.setMode(types.BillingEntityModeEnabled)
			s.addEntity("be_default", env, "INV", true)
			s.addEntity("be_eu", env, "EU", false)

			got, err := resolveConnectionOwner(s.GetContext(), s.service, types.BillingEntityModeEnabled, types.SecretProviderStripe, tc.requested, tc.connectionID)
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

func (s *BillingEntityServiceSuite) TestScopeToCustomer() {
	env := types.GetEnvironmentID(s.GetContext())

	s.Run("off", func() {
		s.ClearStores()
		s.setMode(types.BillingEntityModeOff)

		// The customer does not exist, so any read would fail.
		ctx, err := s.service.ScopeToCustomer(s.GetContext(), "c_missing")
		s.Require().NoError(err)
		s.Empty(types.GetBillingEntityID(ctx))
	})

	tests := []struct {
		name     string
		mode     types.BillingEntityMode
		customer *string
		want     string
	}{
		{name: "enabling_unassigned", mode: types.BillingEntityModeEnabling, want: "be_default"},
		{name: "enabled_assigned", mode: types.BillingEntityModeEnabled, customer: lo.ToPtr("be_eu"), want: "be_eu"},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.ClearStores()
			s.setMode(tc.mode)
			s.addEntity("be_default", env, "INV", true)
			s.addEntity("be_eu", env, "EU", false)
			s.addCustomer("c1", tc.customer)

			ctx, err := s.service.ScopeToCustomer(s.GetContext(), "c1")
			s.Require().NoError(err)
			s.Equal(tc.want, types.GetBillingEntityID(ctx))
		})
	}
}

func (s *BillingEntityServiceSuite) TestScopeToSubscription() {
	env := types.GetEnvironmentID(s.GetContext())
	s.ClearStores()
	s.setMode(types.BillingEntityModeEnabled)
	s.addEntity("be_default", env, "INV", true)
	s.addEntity("be_eu", env, "EU", false)
	s.addCustomer("c_eu", lo.ToPtr("be_eu"))

	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, &subscription.Subscription{
		ID:            "sub_eu",
		CustomerID:    "c_eu",
		EnvironmentID: env,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))

	scoped, err := s.service.ScopeToSubscription(ctx, "sub_eu")
	s.Require().NoError(err)
	s.Equal("be_eu", types.GetBillingEntityID(scoped))
}
