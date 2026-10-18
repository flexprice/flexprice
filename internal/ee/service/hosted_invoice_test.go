package service

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/domain/hostedinvoicetoken"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type HostedInvoiceResolveSuite struct {
	testutil.BaseServiceTestSuite
	svc    HostedInvoiceService
	tenant *tenant.Tenant
	inv    *invoice.Invoice
	token  *hostedinvoicetoken.HostedInvoiceToken
}

func TestHostedInvoiceResolve(t *testing.T) {
	suite.Run(t, new(HostedInvoiceResolveSuite))
}

func (s *HostedInvoiceResolveSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.WithEnvironment("env_hosted")
	ctx := s.GetContext()
	stores := s.GetStores()

	s.svc = NewHostedInvoiceService(ServiceParams{
		Logger:                 s.GetLogger(),
		Config:                 s.GetConfig(),
		DB:                     s.GetDB(),
		TenantRepo:             stores.TenantRepo,
		InvoiceRepo:            stores.InvoiceRepo,
		HostedInvoiceTokenRepo: stores.HostedInvoiceTokenRepo,
	})

	s.tenant = &tenant.Tenant{ID: types.GetTenantID(ctx), Name: "Acme Ltd", InternalStatus: types.TenantInternalStatusActive}
	s.Require().NoError(stores.TenantRepo.Create(ctx, s.tenant))

	s.inv = &invoice.Invoice{
		ID:            "inv_hosted",
		CustomerID:    "cust_1",
		EnvironmentID: "env_hosted",
		InvoiceStatus: types.InvoiceStatusFinalized,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(stores.InvoiceRepo.Create(ctx, s.inv))

	s.token = &hostedinvoicetoken.HostedInvoiceToken{
		ID:            "hit_resolve",
		EnvironmentID: "env_hosted",
		InvoiceID:     s.inv.ID,
		ExpiresAt:     time.Now().UTC().Add(24 * time.Hour),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(stores.HostedInvoiceTokenRepo.Create(ctx, s.token))
}

func (s *HostedInvoiceResolveSuite) sign(id string) string {
	return auth.NewHostedInvoiceSigner(s.GetConfig()).Sign(id)
}

func (s *HostedInvoiceResolveSuite) requireRejected(token, message string) {
	_, err := s.svc.Resolve(context.Background(), token)
	s.Require().Error(err)
	s.True(ierr.IsNotFound(err))
	s.Equal([]string{message}, errors.GetAllHints(err))
}

func (s *HostedInvoiceResolveSuite) TestNotValid() {
	for _, token := range []string{"", "garbage", "hit_resolve.badsig", s.sign("hit_missing")} {
		s.requireRejected(token, "This link is not valid.")
	}
}

func (s *HostedInvoiceResolveSuite) TestNoLongerValid() {
	const message = "This link is no longer valid, contact Acme Ltd."
	tests := []struct {
		name   string
		mutate func()
	}{
		{name: "revoked", mutate: func() { s.token.RevokedAt = lo.ToPtr(time.Now().UTC()) }},
		{name: "expired", mutate: func() { s.token.ExpiresAt = time.Now().UTC().Add(-time.Minute) }},
		{name: "stripe hosted", mutate: func() {
			s.inv.Metadata = types.Metadata{string(types.InvoiceMetadataKeyStripeHostedInvoiceURL): "https://invoice.stripe.com/i/x"}
			s.Require().NoError(s.GetStores().InvoiceRepo.Update(s.GetContext(), s.inv))
		}},
		{name: "tenant suspended", mutate: func() { s.tenant.InternalStatus = types.TenantInternalStatusSuspended }},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			tt.mutate()
			s.requireRejected(s.sign(s.token.ID), message)
		})
	}
}

func (s *HostedInvoiceResolveSuite) TestValid() {
	ctx, err := s.svc.Resolve(context.Background(), s.sign(s.token.ID))
	s.Require().NoError(err)

	s.Equal(s.tenant.ID, types.GetTenantID(ctx))
	s.Equal("env_hosted", types.GetEnvironmentID(ctx))
	s.Equal(s.inv.ID, types.GetHostedInvoiceID(ctx))
	s.Equal(s.token.ID, types.GetHostedInvoiceTokenID(ctx))

	actor, source, ok := types.GetActor(ctx)
	s.Require().True(ok)
	s.Equal(types.Actor{Type: types.ActorTypeCustomer, ID: "cust_1"}, actor)
	s.Equal(types.SourceHostedInvoice, source)
}
