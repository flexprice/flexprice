package chargebee

import (
	"context"
	"testing"
	"time"

	"github.com/chargebee/chargebee-go/v3/enum"
	paymentSourceModel "github.com/chargebee/chargebee-go/v3/models/paymentsource"
	paymentSourceEnum "github.com/chargebee/chargebee-go/v3/models/paymentsource/enum"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Chargebee has many more source types than PaymentMethodType has members. An
// unmappable type must be dropped, never coerced into a member meaning something
// else.
func TestMethodTypeFor(t *testing.T) {
	tests := []struct {
		cbType enum.Type
		want   types.PaymentMethodType
		mapped bool
	}{
		{enum.TypeCard, types.PaymentMethodTypeCard, true},
		{enum.TypeApplePay, types.PaymentMethodTypeCard, true},
		{enum.TypeGooglePay, types.PaymentMethodTypeCard, true},
		{enum.TypeDirectDebit, types.PaymentMethodTypeACH, true},
		{enum.TypeUpi, types.PaymentMethodTypeUPI, true},
		{enum.TypePaypalExpressCheckout, "", false},
		{enum.TypeAmazonPayments, "", false},
		{enum.TypeIdeal, "", false},
		{enum.TypeSofort, "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.cbType), func(t *testing.T) {
			got, ok := methodTypeFor(tt.cbType)
			assert.Equal(t, tt.mapped, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToProviderPaymentMethod_CardIsNormalized(t *testing.T) {
	created := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	src := &paymentSourceModel.PaymentSource{
		Id:               "pm_001",
		CustomerId:       "cb_cust_001",
		Type:             enum.TypeCard,
		Status:           paymentSourceEnum.StatusValid,
		Gateway:          enum.GatewayStripe,
		GatewayAccountId: "gw_001",
		CreatedAt:        created.Unix(),
		Card: &paymentSourceModel.Card{
			Brand:       paymentSourceEnum.CardBrandVisa,
			Last4:       "4242",
			ExpiryMonth: 12,
			ExpiryYear:  2030,
		},
	}

	got, ok := toProviderPaymentMethod(src, "pm_001")

	require.True(t, ok)
	assert.Equal(t, "pm_001", got.GatewayMethodID)
	assert.Equal(t, types.PaymentMethodTypeCard, got.Method)
	assert.True(t, got.IsDefault, "the primary source is the default")
	assert.True(t, got.Active)
	assert.Equal(t, "gw_001", got.GatewayAccountID, "kept so a split vault is diagnosable")
	require.NotNil(t, got.Card)
	assert.Equal(t, "4242", got.Card.Last4)
	assert.Equal(t, 12, got.Card.ExpMonth)
	assert.Equal(t, 2030, got.Card.ExpYear)
}

// Expiring cards still work, so they stay active; expired and invalid ones are
// reported inactive rather than dropped, so the portal can say why a card stopped
// working instead of silently showing a shorter list.
func TestToProviderPaymentMethod_ActiveByStatus(t *testing.T) {
	tests := []struct {
		status paymentSourceEnum.Status
		active bool
	}{
		{paymentSourceEnum.StatusValid, true},
		{paymentSourceEnum.StatusExpiring, true},
		{paymentSourceEnum.StatusExpired, false},
		{paymentSourceEnum.StatusInvalid, false},
		{paymentSourceEnum.StatusPendingVerification, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got, ok := toProviderPaymentMethod(&paymentSourceModel.PaymentSource{
				Id: "pm_001", Type: enum.TypeCard, Status: tt.status,
			}, "")

			require.True(t, ok)
			assert.Equal(t, tt.active, got.Active)
			assert.False(t, got.IsDefault, "no primary set means nothing is default")
			assert.Equal(t, string(tt.status), got.ProviderMetadata["chargebee_payment_source_status"])
		})
	}
}

func TestToProviderPaymentMethod_DeletedSourceIsDropped(t *testing.T) {
	_, ok := toProviderPaymentMethod(&paymentSourceModel.PaymentSource{
		Id: "pm_001", Type: enum.TypeCard, Status: paymentSourceEnum.StatusValid, Deleted: true,
	}, "")

	assert.False(t, ok)
}

// ── ownership check ─────────────────────────────────────────────────────────
//
// Chargebee's delete and assign_payment_role take a bare payment source id with no
// customer in the path, so this check is the only thing standing between a guessed
// id and acting on another customer's card.

type mappingStore struct {
	chargebeeCustomerIDByEntityID map[string]string
}

func (mappingStore) Create(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}
func (mappingStore) Get(_ context.Context, _ string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	return nil, ierr.NewError("not found").Mark(ierr.ErrNotFound)
}
func (s mappingStore) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	id, ok := s.chargebeeCustomerIDByEntityID[filter.EntityID]
	if !ok {
		return nil, nil
	}
	return []*entityintegrationmapping.EntityIntegrationMapping{{ProviderEntityID: id}}, nil
}
func (mappingStore) Count(_ context.Context, _ *types.EntityIntegrationMappingFilter) (int, error) {
	return 0, nil
}
func (mappingStore) Update(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}
func (mappingStore) Delete(_ context.Context, _ *entityintegrationmapping.EntityIntegrationMapping) error {
	return nil
}

type paymentSourceClient struct {
	ChargebeeClient
	sources     []*paymentSourceModel.PaymentSource
	deleteCalls []string
	roleCalls   []string
}

func (c *paymentSourceClient) ListPaymentSources(_ context.Context, _ string) ([]*paymentSourceModel.PaymentSource, error) {
	return c.sources, nil
}

func (c *paymentSourceClient) DeletePaymentSource(_ context.Context, paymentSourceID string) error {
	c.deleteCalls = append(c.deleteCalls, paymentSourceID)
	return nil
}

func (c *paymentSourceClient) AssignPaymentRole(_ context.Context, _, paymentSourceID string, _ enum.Role) error {
	c.roleCalls = append(c.roleCalls, paymentSourceID)
	return nil
}

func newOwnershipAdapter(sources ...*paymentSourceModel.PaymentSource) (*PaymentMethodAdapter, *paymentSourceClient) {
	client := &paymentSourceClient{sources: sources}
	return &PaymentMethodAdapter{
		Client: client,
		CustomerSvc: &CustomerService{CustomerServiceParams{
			EntityIntegrationMappingRepo: mappingStore{
				chargebeeCustomerIDByEntityID: map[string]string{"cust_ours": "cb_cust_ours"},
			},
			Logger: logger.NewNoopLogger(),
		}},
		Logger: logger.NewNoopLogger(),
	}, client
}

func ownedSource(id string) *paymentSourceModel.PaymentSource {
	return &paymentSourceModel.PaymentSource{Id: id, Type: enum.TypeCard, Status: paymentSourceEnum.StatusValid}
}

func TestDeleteSavedMethod_RejectsMethodOwnedByAnotherCustomer(t *testing.T) {
	adapter, client := newOwnershipAdapter(ownedSource("pm_ours"))

	err := adapter.DeleteSavedMethod(context.Background(), "cust_ours", "pm_someone_elses")

	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err), "must not confirm the id exists elsewhere")
	assert.Empty(t, client.deleteCalls, "nothing may reach Chargebee")
}

func TestSetDefaultSavedMethod_RejectsMethodOwnedByAnotherCustomer(t *testing.T) {
	adapter, client := newOwnershipAdapter(ownedSource("pm_ours"))

	err := adapter.SetDefaultSavedMethod(context.Background(), "cust_ours", "pm_someone_elses")

	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
	assert.Empty(t, client.roleCalls)
}

func TestDeleteAndSetDefault_AllowOwnedMethod(t *testing.T) {
	adapter, client := newOwnershipAdapter(ownedSource("pm_ours"))
	ctx := context.Background()

	require.NoError(t, adapter.DeleteSavedMethod(ctx, "cust_ours", "pm_ours"))
	require.NoError(t, adapter.SetDefaultSavedMethod(ctx, "cust_ours", "pm_ours"))

	assert.Equal(t, []string{"pm_ours"}, client.deleteCalls)
	assert.Equal(t, []string{"pm_ours"}, client.roleCalls)
}

func TestSavedMethodActions_RejectEmptyMethodID(t *testing.T) {
	adapter, client := newOwnershipAdapter(ownedSource("pm_ours"))

	err := adapter.DeleteSavedMethod(context.Background(), "cust_ours", "")

	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
	assert.Empty(t, client.deleteCalls)
}
