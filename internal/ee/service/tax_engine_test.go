package service

import (
	"context"
	"strings"
	"testing"

	cockroachdberrors "github.com/cockroachdb/errors"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// TaxEngineSelectionSuite covers which engine a tenant resolves to, which is the only place
// in the system that names a provider.
type TaxEngineSelectionSuite struct {
	testutil.BaseServiceTestSuite
	params ServiceParams
}

func TestTaxEngineSelection(t *testing.T) {
	suite.Run(t, new(TaxEngineSelectionSuite))
}

func (s *TaxEngineSelectionSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.WithEnvironment("env_tax_engine")
	st := s.GetStores()
	s.params = ServiceParams{
		Logger:         s.GetLogger(),
		Config:         s.GetConfig(),
		DB:             s.GetDB(),
		SettingsRepo:   st.SettingsRepo,
		ConnectionRepo: st.ConnectionRepo,
		TaxRateRepo:    st.TaxRateRepo,
		TaxAppliedRepo: st.TaxAppliedRepo,
		InvoiceRepo:    st.InvoiceRepo,
		CustomerRepo:   st.CustomerRepo,
	}
}

func (s *TaxEngineSelectionSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// seedProvider writes a tax config naming an enabled provider, which is the only shape that
// selects an external engine. A partial value map is enough: the settings service overlays it
// onto the defaults.
func (s *TaxEngineSelectionSuite) seedProvider(ctx context.Context, provider string) {
	s.seedTaxConfig(ctx, map[string]interface{}{"enabled": true, "provider": provider})
}

func (s *TaxEngineSelectionSuite) seedTaxConfig(ctx context.Context, value map[string]interface{}) {
	s.Require().NoError(s.GetStores().SettingsRepo.Create(ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeyTaxConfig,
		Value:         value,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
}

func (s *TaxEngineSelectionSuite) seedStripeConnection(ctx context.Context) {
	s.Require().NoError(s.GetStores().ConnectionRepo.Create(ctx, &connection.Connection{
		ID:            types.GenerateUUIDWithPrefix("conn"),
		Name:          "stripe",
		ProviderType:  types.SecretProviderStripe,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
}

func (s *TaxEngineSelectionSuite) TestNewTaxEngine_SettingAbsentResolvesToNative() {
	engine, err := NewTaxEngine(s.GetContext(), s.params)

	s.Require().NoError(err, "a tenant that never configured tax must keep working")
	s.Equal(types.TaxProviderFlexprice, engine.GetProvider())
	s.False(engine.GetProvider().IsExternal())
}

func (s *TaxEngineSelectionSuite) TestNewTaxEngine_SettingPresentButProviderEmptyResolvesToNative() {
	ctx := s.GetContext()
	s.seedTaxConfig(ctx, map[string]interface{}{"enabled": true})

	engine, err := NewTaxEngine(ctx, s.params)

	s.Require().NoError(err)
	s.Equal(types.TaxProviderFlexprice, engine.GetProvider())
}

// Disabled is not the absence of tax. It is the absence of an external engine, so the native
// one runs exactly as it does for a tenant that never held the setting.
func (s *TaxEngineSelectionSuite) TestNewTaxEngine_DisabledResolvesToNativeWhateverTheProvider() {
	ctx := s.GetContext()
	s.seedTaxConfig(ctx, map[string]interface{}{"enabled": false, "provider": string(types.TaxProviderStripe)})

	engine, err := NewTaxEngine(ctx, s.params)

	s.Require().NoError(err)
	s.Equal(types.TaxProviderFlexprice, engine.GetProvider())
	s.False(engine.GetProvider().IsExternal())
}

// A provider nobody can build is still native while it is switched off, so a half-configured
// setting cannot fail a tenant's billing.
func (s *TaxEngineSelectionSuite) TestNewTaxEngine_DisabledUnknownProviderIsNotAnError() {
	ctx := s.GetContext()
	s.seedTaxConfig(ctx, map[string]interface{}{"enabled": false, "provider": "avalara"})

	engine, err := NewTaxEngine(ctx, s.params)

	s.Require().NoError(err)
	s.Equal(types.TaxProviderFlexprice, engine.GetProvider())
}

func (s *TaxEngineSelectionSuite) TestNewTaxEngine_FlexpriceResolvesToNative() {
	ctx := s.GetContext()
	s.seedProvider(ctx, string(types.TaxProviderFlexprice))

	engine, err := NewTaxEngine(ctx, s.params)

	s.Require().NoError(err)
	s.Equal(types.TaxProviderFlexprice, engine.GetProvider())
}

func (s *TaxEngineSelectionSuite) TestNewTaxEngine_StripeWithConnectionResolvesToStripe() {
	ctx := s.GetContext()
	s.seedProvider(ctx, string(types.TaxProviderStripe))
	s.seedStripeConnection(ctx)

	engine, err := NewTaxEngine(ctx, s.params)

	s.Require().NoError(err)
	s.Equal(types.TaxProviderStripe, engine.GetProvider())
	s.True(engine.GetProvider().IsExternal())
}

func (s *TaxEngineSelectionSuite) TestNewTaxEngine_StripeWithoutConnectionIsAnErrorNotNative() {
	ctx := s.GetContext()
	s.seedProvider(ctx, string(types.TaxProviderStripe))

	engine, err := NewTaxEngine(ctx, s.params)

	s.Require().Error(err, "falling back to native would bill Flexprice rates under a Stripe configuration")
	s.Nil(engine)
	s.True(ierr.IsValidation(err), "the tenant has to connect Stripe, so this is theirs to fix")
}

func (s *TaxEngineSelectionSuite) TestNewTaxEngine_UnknownProviderIsAnError() {
	ctx := s.GetContext()
	s.seedProvider(ctx, "avalara")

	engine, err := NewTaxEngine(ctx, s.params)

	s.Require().Error(err, "a typo must not silently change how a tenant is taxed")
	s.Nil(engine)
	s.True(ierr.IsValidation(err))
}

func (s *TaxEngineSelectionSuite) TestNewTaxEngine_EnvironmentsOfOneTenantResolveIndependently() {
	stripeCtx := types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_stripe")
	nativeCtx := types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_native")

	s.seedProvider(stripeCtx, string(types.TaxProviderStripe))
	s.seedStripeConnection(stripeCtx)
	s.seedProvider(nativeCtx, string(types.TaxProviderFlexprice))

	stripeEngine, err := NewTaxEngine(stripeCtx, s.params)
	s.Require().NoError(err)
	s.Equal(types.TaxProviderStripe, stripeEngine.GetProvider())

	nativeEngine, err := NewTaxEngine(nativeCtx, s.params)
	s.Require().NoError(err)
	s.Equal(types.TaxProviderFlexprice, nativeEngine.GetProvider())
}

func (s *TaxEngineSelectionSuite) TestNativeEngine_CommitRecordsNothing() {
	transactionID, err := (&nativeTaxEngine{ServiceParams: s.params}).Commit(s.GetContext(), nil, nil)

	s.Require().NoError(err, "native tax is filed by the tenant, so there is nothing to record")
	s.Empty(transactionID)
}

// =============================================================================
// Tax associations under an external engine
// =============================================================================

// TaxAssociationGateSuite covers what happens to Flexprice's own rate associations once an
// external engine is calculating the tax.
type TaxAssociationGateSuite struct {
	testutil.BaseServiceTestSuite
	svc TaxService
}

func TestTaxAssociationGate(t *testing.T) {
	suite.Run(t, new(TaxAssociationGateSuite))
}

func (s *TaxAssociationGateSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.WithEnvironment("env_tax_gate")
	st := s.GetStores()
	s.svc = NewTaxService(ServiceParams{
		Logger:             s.GetLogger(),
		Config:             s.GetConfig(),
		DB:                 s.GetDB(),
		TaxRateRepo:        st.TaxRateRepo,
		TaxAppliedRepo:     st.TaxAppliedRepo,
		TaxAssociationRepo: st.TaxAssociationRepo,
		InvoiceRepo:        st.InvoiceRepo,
		CustomerRepo:       st.CustomerRepo,
		SubRepo:            st.SubscriptionRepo,
		SettingsRepo:       st.SettingsRepo,
		ConnectionRepo:     st.ConnectionRepo,
	})
}

func (s *TaxAssociationGateSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *TaxAssociationGateSuite) enableStripe(ctx context.Context) {
	s.Require().NoError(s.GetStores().SettingsRepo.Create(ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeyTaxConfig,
		Value:         map[string]interface{}{"enabled": true, "provider": string(types.TaxProviderStripe)},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
	s.Require().NoError(s.GetStores().ConnectionRepo.Create(ctx, &connection.Connection{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONNECTION),
		Name:          "Stripe",
		ProviderType:  types.SecretProviderStripe,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
}

func (s *TaxAssociationGateSuite) associationRequest() *dto.CreateTaxAssociationRequest {
	return &dto.CreateTaxAssociationRequest{
		TaxRateCode: "vat_20",
		EntityType:  types.TaxRateEntityTypeCustomer,
		EntityID:    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
	}
}

// An association is what makes a Flexprice rate apply. Under an external engine it would be
// stored and never read, which looks to a tenant like tax they configured.
func (s *TaxAssociationGateSuite) TestCreateTaxAssociationIsRefusedUnderAnExternalEngine() {
	ctx := s.GetContext()
	s.enableStripe(ctx)

	association, err := s.svc.CreateTaxAssociation(ctx, s.associationRequest())

	s.Require().Error(err)
	s.Nil(association)
	s.True(ierr.IsValidation(err), "the tenant configures tax at the provider instead, so this is theirs to fix")
	s.Contains(err.Error(), string(types.TaxProviderStripe), "the error names the engine in play")
}

// An override is a caller asking for these rates. Dropping that silently is how a tenant comes
// to believe it configured tax that was never applied.
func (s *TaxAssociationGateSuite) TestLinkTaxRatesRefusesOverridesUnderAnExternalEngine() {
	ctx := s.GetContext()
	s.enableStripe(ctx)

	err := s.svc.LinkTaxRatesToEntity(ctx, dto.LinkTaxRateToEntityRequest{
		EntityType: types.TaxRateEntityTypeCustomer,
		EntityID:   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		TaxRateOverrides: []*dto.TaxRateOverride{
			{TaxRateCode: "vat_20"},
		},
	})

	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
	s.Contains(err.Error(), string(types.TaxProviderStripe), "the error names the engine in play")
}

// The inherited cascade is Flexprice's own doing, not a request. Failing a customer over rows
// it never asked for helps nobody, so it is skipped and nothing is written.
func (s *TaxAssociationGateSuite) TestLinkTaxRatesSkipsInheritedAssociationsUnderAnExternalEngine() {
	ctx := s.GetContext()
	s.enableStripe(ctx)
	entityID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER)

	err := s.svc.LinkTaxRatesToEntity(ctx, dto.LinkTaxRateToEntityRequest{
		EntityType: types.TaxRateEntityTypeCustomer,
		EntityID:   entityID,
	})

	s.Require().NoError(err, "creating the customer must not fail over associations the engine ignores")

	filter := types.NewNoLimitTaxAssociationFilter()
	filter.EntityType = types.TaxRateEntityTypeCustomer
	filter.EntityID = entityID
	associations, err := s.GetStores().TaxAssociationRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Empty(associations, "nothing is linked, so nothing claims to be configured")
}

// The native path is untouched. A tenant with no tax config still links rates exactly as before.
func (s *TaxAssociationGateSuite) TestLinkTaxRatesStillRunsNatively() {
	ctx := s.GetContext()
	entityID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER)

	err := s.svc.LinkTaxRatesToEntity(ctx, dto.LinkTaxRateToEntityRequest{
		EntityType: types.TaxRateEntityTypeCustomer,
		EntityID:   entityID,
	})

	s.NoError(err, "an empty link on the native path is a no-op, not a refusal")
}

// The list a person is shown comes from the list that is checked, so adding an engine cannot
// leave the message naming the old set. Asserted against the hint, which is what the error
// handler returns as the response message.
func TestTaxProviderValidate_MessageNamesEveryAllowedProvider(t *testing.T) {
	err := types.TaxProvider("avalara").Validate()
	require.Error(t, err)

	hints := cockroachdberrors.GetAllHints(err)
	require.NotEmpty(t, hints, "the response message is the hint, so an error without one says nothing")
	hint := strings.Join(hints, " ")

	for _, provider := range types.TaxProviders {
		assert.Contains(t, hint, string(provider),
			"a provider that is accepted must be named in the message that rejects others")
	}
	assert.Contains(t, err.Error(), "avalara", "the error names what was received")
}

func TestTaxProviderValidate_EmptyIsNativeAndAccepted(t *testing.T) {
	assert.NoError(t, types.TaxProvider("").Validate(), "empty means the native engine")
}

// A provider that is switched off is not in force, so validating it would block the very update
// that turns an unsupported one off.
func TestTaxConfigValidate_DisabledProviderIsNotValidated(t *testing.T) {
	assert.NoError(t, types.TaxConfig{Enabled: false, Provider: "avalara"}.Validate(),
		"a disabled config resolves to native whatever it names")
	assert.NoError(t, types.TaxConfig{Enabled: false}.Validate())
}

func TestTaxConfigValidate_EnabledProviderIsValidated(t *testing.T) {
	require.Error(t, types.TaxConfig{Enabled: true, Provider: "avalara"}.Validate(),
		"an engine in force has to be one that can be built")
	assert.NoError(t, types.TaxConfig{Enabled: true, Provider: types.TaxProviderStripe}.Validate())
}

// The idempotency key is what makes a retry replay the original filing instead of colliding on
// the reference, so it has to be stable for one filing and different across filings.
func TestTaxProviderIdempotencyKey_StablePerFiling(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")

	first := taxProviderIdempotencyKey(ctx, "commit", "inv_1", "taxcalc_1")
	again := taxProviderIdempotencyKey(ctx, "commit", "inv_1", "taxcalc_1")
	assert.Equal(t, first, again, "a retry of the same filing must send the same key")

	assert.NotEqual(t, first, taxProviderIdempotencyKey(ctx, "commit", "inv_2", "taxcalc_1"),
		"a different invoice is a different filing")
	assert.NotEqual(t, first, taxProviderIdempotencyKey(ctx, "commit", "inv_1", "taxcalc_2"),
		"a recalculation is a different filing")
	assert.NotEqual(t, first, taxProviderIdempotencyKey(ctx, "reverse", "inv_1", "taxcalc_1"),
		"undoing a filing is not the filing")

	other := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_2"), "env_1")
	assert.NotEqual(t, first, taxProviderIdempotencyKey(other, "commit", "inv_1", "taxcalc_1"),
		"keys are scoped to the tenant that owns the filing")
}

// The scenario that matters after a tenant switches engines: the invoice was taxed and filed
// under Stripe, the tenant then moved to native (or the setting was deleted). Undoing that tax
// has to go back to Stripe, because Stripe holds the transaction. Reading the setting instead
// would resolve to native, report nothing to reverse, and leave the filing standing forever.
func (s *TaxEngineSelectionSuite) TestEngineForAFiledProviderIgnoresTheTenantSetting() {
	ctx := s.GetContext()
	s.seedStripeConnection(ctx)

	// The tenant is on native now: no tax_config row at all, which is the deleted-setting case.
	configured, err := NewTaxEngine(ctx, s.params)
	s.Require().NoError(err)
	s.Equal(types.TaxProviderFlexprice, configured.GetProvider(),
		"the tenant's current engine is native")

	// The invoice's own rows still say stripe, and that is what decides.
	filed, err := newTaxEngineForProvider(ctx, s.params, types.TaxProviderStripe)
	s.Require().NoError(err)
	s.Equal(types.TaxProviderStripe, filed.GetProvider(),
		"undoing tax goes back to the engine that filed it, not the one configured now")
}

// Same check with the setting explicitly disabled rather than absent, which is how a tenant
// turns an engine off.
func (s *TaxEngineSelectionSuite) TestEngineForAFiledProviderIgnoresADisabledSetting() {
	ctx := s.GetContext()
	s.seedStripeConnection(ctx)
	s.seedTaxConfig(ctx, map[string]interface{}{"enabled": false, "provider": string(types.TaxProviderStripe)})

	configured, err := NewTaxEngine(ctx, s.params)
	s.Require().NoError(err)
	s.Equal(types.TaxProviderFlexprice, configured.GetProvider(), "disabled means native for new work")

	filed, err := newTaxEngineForProvider(ctx, s.params, types.TaxProviderStripe)
	s.Require().NoError(err)
	s.Equal(types.TaxProviderStripe, filed.GetProvider(), "old work is still undone with Stripe")
}
