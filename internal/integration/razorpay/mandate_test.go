package razorpay

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRazorpaySubscriptionMethod(t *testing.T) {
	tests := []struct {
		name    string
		input   types.PaymentMethodType
		want    string
		wantErr bool
	}{
		{name: "empty defaults to upi", input: "", want: "upi", wantErr: false},
		{name: "UPI maps to upi", input: types.PaymentMethodTypeUPI, want: "upi", wantErr: false},
		{name: "Card maps to card", input: types.PaymentMethodTypeCard, want: "card", wantErr: false},
		{name: "unsupported method errors", input: types.PaymentMethodTypeACH, want: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := razorpaySubscriptionMethod(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, ierr.IsNotImplemented(err), "expected ErrNotImplemented-marked error")
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

type stubRazorpayCustomerSvc struct {
	tokens    []*interfaces.ProviderPaymentMethod
	err       error
	customer  *customer.Customer
	savedLink string
	syncErr   error
}

func (s *stubRazorpayCustomerSvc) UpdateCustomerInRazorpay(ctx context.Context, c *customer.Customer) error {
	return nil
}

func (s *stubRazorpayCustomerSvc) VerifyLinkCustomer(ctx context.Context, c *customer.Customer) (string, error) {
	return "cust_rzp", s.syncErr
}

func (s *stubRazorpayCustomerSvc) SaveSetupLink(ctx context.Context, customerID, linkID string) error {
	s.savedLink = linkID
	return nil
}

func (s *stubRazorpayCustomerSvc) GetSetupLink(ctx context.Context, customerID string) (*interfaces.SetupLinkResponse, error) {
	return nil, nil
}

func (s *stubRazorpayCustomerSvc) CancelSetupLink(ctx context.Context, customerID string) (*interfaces.SetupLinkResponse, error) {
	return nil, nil
}

func (s *stubRazorpayCustomerSvc) EnsureCustomerSyncedToRazorpay(ctx context.Context, customerID string, customerService interfaces.CustomerService) (*customer.Customer, error) {
	return s.customer, nil
}
func (s *stubRazorpayCustomerSvc) SyncCustomerToRazorpay(ctx context.Context, flexpriceCustomer *customer.Customer) (string, error) {
	return "", nil
}
func (s *stubRazorpayCustomerSvc) GetRazorpayCustomerID(ctx context.Context, customerID string) (string, error) {
	return "", nil
}
func (s *stubRazorpayCustomerSvc) UpdateRazorpayCustomerNotes(ctx context.Context, razorpayCustomerID string, notes map[string]interface{}) error {
	return nil
}
func (s *stubRazorpayCustomerSvc) ListCustomerTokens(ctx context.Context, customerID string) (string, []*interfaces.ProviderPaymentMethod, error) {
	if s.err != nil {
		return "", nil, s.err
	}
	return "cust_rzp_1", s.tokens, nil
}

func TestRazorpayHasAutoChargeableMethod(t *testing.T) {
	ctx := context.Background()

	t.Run("nil adapter or service returns false, nil", func(t *testing.T) {
		var nilAdapter *CheckoutAdapter
		has, err := nilAdapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
		assert.NoError(t, err)
		assert.False(t, has)

		adapterWithoutSvc := &CheckoutAdapter{}
		has, err = adapterWithoutSvc.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
		assert.NoError(t, err)
		assert.False(t, has)

		adapterWithoutCustomerSvc := &CheckoutAdapter{Svc: &PaymentService{}}
		has, err = adapterWithoutCustomerSvc.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
		assert.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns true when customer has confirmed recurring mandate token with nil amount", func(t *testing.T) {
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					tokens: []*interfaces.ProviderPaymentMethod{
						{GatewayMethodID: "token_123", Method: types.PaymentMethodTypeUPI, Active: true, Recurring: &interfaces.ProviderRecurringPaymentDetails{Status: types.RecurringPaymentStatusActive}},
					},
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
		assert.NoError(t, err)
		assert.True(t, has)
	})

	t.Run("returns true when amount is within mandate ceiling", func(t *testing.T) {
		maxAmount := decimal.NewFromInt(1000)
		amount := decimal.NewFromInt(500)
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					tokens: []*interfaces.ProviderPaymentMethod{
						{GatewayMethodID: "token_123", Method: types.PaymentMethodTypeUPI, Active: true, Recurring: &interfaces.ProviderRecurringPaymentDetails{Status: types.RecurringPaymentStatusActive, MaxAmount: &maxAmount}},
					},
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1", Amount: &amount})
		assert.NoError(t, err)
		assert.True(t, has)
	})

	t.Run("returns false when amount exceeds mandate ceiling", func(t *testing.T) {
		maxAmount := decimal.NewFromInt(500)
		amount := decimal.NewFromInt(1000)
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					tokens: []*interfaces.ProviderPaymentMethod{
						{GatewayMethodID: "token_123", Method: types.PaymentMethodTypeUPI, Active: true, Recurring: &interfaces.ProviderRecurringPaymentDetails{Status: types.RecurringPaymentStatusActive, MaxAmount: &maxAmount}},
					},
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1", Amount: &amount})
		assert.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns false when all confirmed tokens are expired", func(t *testing.T) {
		past := time.Now().Add(-24 * time.Hour).UTC()
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					tokens: []*interfaces.ProviderPaymentMethod{
						{GatewayMethodID: "token_123", Method: types.PaymentMethodTypeUPI, Active: true, Recurring: &interfaces.ProviderRecurringPaymentDetails{Status: types.RecurringPaymentStatusActive, AutoChargeableTill: &past}},
					},
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
		assert.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns false when no confirmed tokens exist", func(t *testing.T) {
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					tokens: []*interfaces.ProviderPaymentMethod{},
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
		assert.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns false, nil when customer is not found", func(t *testing.T) {
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					err: ierr.NewError("customer not found").Mark(ierr.ErrNotFound),
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
		assert.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns false when all confirmed tokens are inactive", func(t *testing.T) {
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					tokens: []*interfaces.ProviderPaymentMethod{
						{GatewayMethodID: "token_123", Method: types.PaymentMethodTypeUPI, Active: false, Recurring: &interfaces.ProviderRecurringPaymentDetails{Status: types.RecurringPaymentStatusActive}},
					},
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
		assert.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("returns false, err when a real error occurs", func(t *testing.T) {
		adapter := &CheckoutAdapter{
			Svc: &PaymentService{
				customerSvc: &stubRazorpayCustomerSvc{
					err: ierr.NewError("api failure").Mark(ierr.ErrHTTPClient),
				},
			},
		}
		has, err := adapter.HasAutoChargeableMethod(ctx, interfaces.HasAutoChargeableMethodRequest{CustomerID: "cust_1"})
		assert.Error(t, err)
		assert.True(t, ierr.IsHTTPClient(err))
		assert.False(t, has)
	})
}

func TestNormalizeRazorpayToken(t *testing.T) {
	future := float64(time.Now().Add(24 * time.Hour).Unix())
	past := float64(time.Now().Add(-time.Hour).Unix())
	confirmed := map[string]interface{}{"status": "confirmed"}

	tests := []struct {
		name          string
		raw           map[string]interface{}
		wantNil       bool
		wantActive    bool
		wantRecurring types.RecurringPaymentStatus
		check         func(t *testing.T, pm *interfaces.ProviderPaymentMethod)
	}{
		{
			name: "confirmed card maps card and mandate details",
			raw: map[string]interface{}{
				"id": "tok_card", "method": "card", "recurring": true, "recurring_details": confirmed,
				"card":       map[string]interface{}{"last4": "8950", "network": "Visa", "expiry_month": float64(12), "expiry_year": "2030"},
				"expired_at": future, "max_amount": float64(1500000),
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusActive,
			check: func(t *testing.T, pm *interfaces.ProviderPaymentMethod) {
				require.NotNil(t, pm.Card)
				assert.Equal(t, "8950", pm.Card.Last4)
				assert.Equal(t, "Visa", pm.Card.Brand)
				assert.Equal(t, 12, pm.Card.ExpMonth)
				assert.Equal(t, 2030, pm.Card.ExpYear)
				assert.Equal(t, "15000", pm.RecurringMaxAmount().String())
				assert.False(t, pm.InstantlyChargeable)
			},
		},
		{
			name: "confirmed UPI maps VPA",
			raw: map[string]interface{}{
				"id": "tok_upi", "method": "upi", "recurring": true, "recurring_details": confirmed,
				"vpa": map[string]interface{}{"username": "gaurav.kumar", "handle": "upi"},
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusActive,
			check: func(t *testing.T, pm *interfaces.ProviderPaymentMethod) {
				require.NotNil(t, pm.UPI)
				assert.Equal(t, "gaurav.kumar@upi", pm.UPI.VPA)
			},
		},
		{
			name: "initiated mandate maps to pending",
			raw: map[string]interface{}{
				"id": "tok_init", "method": "upi", "recurring": true,
				"recurring_details": map[string]interface{}{"status": "initiated"},
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusPending,
		},
		{
			name: "lapsed mandate reports expired",
			raw: map[string]interface{}{
				"id": "tok_old", "method": "card", "recurring": true, "recurring_details": confirmed, "expired_at": past,
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusExpired,
		},
		{
			name: "non-recurring token has no mandate",
			raw: map[string]interface{}{
				"id": "tok_saved", "method": "card", "recurring": false, "recurring_details": confirmed,
			},
			wantActive: true,
		},
		{
			name: "token status from razorpay decides active",
			raw: map[string]interface{}{
				"id": "tok_deact", "method": "card", "status": "deactivated", "recurring": true, "recurring_details": confirmed,
			},
			wantActive:    false,
			wantRecurring: types.RecurringPaymentStatusActive,
		},
		{
			name: "confirmed mandate on a card with flows.recurring false stays active",
			raw: map[string]interface{}{
				"id": "token_Tcc26RAH6R0pD2", "method": "card", "status": "active", "recurring": true, "recurring_details": confirmed,
				"card": map[string]interface{}{
					"last4": "1301", "network": "MasterCard", "expiry_month": "01", "expiry_year": "2099",
					"flows": map[string]interface{}{"otp": true, "recurring": false},
				},
				"expired_at": future, "max_amount": float64(1500000),
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusActive,
		},
		{
			name: "card with recurring support keeps its mandate status",
			raw: map[string]interface{}{
				"id": "tok_flows", "method": "card", "recurring": true, "recurring_details": confirmed,
				"card": map[string]interface{}{"flows": map[string]interface{}{"otp": true, "recurring": true}},
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusActive,
		},
		{
			name: "cancelled mandate stays cancelled on a card without recurring support",
			raw: map[string]interface{}{
				"id": "tok_cancel", "method": "card", "recurring": true,
				"recurring_details": map[string]interface{}{"status": "cancelled"},
				"card":              map[string]interface{}{"flows": map[string]interface{}{"recurring": false}},
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusCancelled,
		},
		{
			name: "paused mandate stays paused on a card without recurring support",
			raw: map[string]interface{}{
				"id": "tok_paused", "method": "card", "recurring": true,
				"recurring_details": map[string]interface{}{"status": "paused"},
				"card":              map[string]interface{}{"flows": map[string]interface{}{"recurring": false}},
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusPaused,
		},
		{
			name: "confirmed emandate is an active bank mandate",
			raw: map[string]interface{}{
				"id": "tok_em", "method": "emandate", "recurring": true, "recurring_details": confirmed,
				"auth_type":    "netbanking",
				"bank_details": map[string]interface{}{"account_number": "XXXXXXXX1234", "ifsc": "HDFC0000001", "account_type": "savings"},
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusActive,
			check: func(t *testing.T, pm *interfaces.ProviderPaymentMethod) {
				assert.Equal(t, types.PaymentMethodTypeENACH, pm.Method)
				assert.Equal(t, &interfaces.ProviderBankAccountDetails{IFSC: "HDFC0000001", Last4: "1234", AccountType: "savings", AuthType: "netbanking"}, pm.BankAccount)
			},
		},
		{
			name: "rejected emandate keeps its status",
			raw: map[string]interface{}{
				"id": "tok_em_rej", "method": "emandate", "recurring": false,
				"recurring_details": map[string]interface{}{"status": "rejected", "failure_reason": "Rejected by bank"},
			},
			wantActive:    true,
			wantRecurring: types.RecurringPaymentStatusRejected,
		},
		{
			name:    "unknown method is skipped",
			raw:     map[string]interface{}{"id": "tok_nb", "method": "netbanking", "recurring": true, "recurring_details": confirmed},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm, err := NormalizeRazorpayToken(tt.raw)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, pm)
				return
			}
			require.NotNil(t, pm)
			assert.Equal(t, tt.wantActive, pm.Active)
			assert.Equal(t, tt.wantRecurring, pm.RecurringStatus())
			if tt.check != nil {
				tt.check(t, pm)
			}
		})
	}
}

func TestSelectUsableTokenRequiresLiveMandate(t *testing.T) {
	pending := &interfaces.ProviderPaymentMethod{
		GatewayMethodID: "tok_pending", Method: types.PaymentMethodTypeCard, Active: true,
		Recurring: &interfaces.ProviderRecurringPaymentDetails{Status: types.RecurringPaymentStatusPending},
	}
	vaulted := &interfaces.ProviderPaymentMethod{GatewayMethodID: "tok_vaulted", Method: types.PaymentMethodTypeCard, Active: true}

	_, ok := SelectUsableToken([]*interfaces.ProviderPaymentMethod{pending, vaulted}, types.PaymentMethodTypeCard, decimal.Zero)
	assert.False(t, ok)
}

func TestPaymentMethodAdapterListSavedMethods(t *testing.T) {
	ctx := context.Background()

	t.Run("returns tokens", func(t *testing.T) {
		a := NewPaymentMethodAdapter(nil, &stubRazorpayCustomerSvc{tokens: []*interfaces.ProviderPaymentMethod{
			{GatewayMethodID: "token_1", Method: types.PaymentMethodTypeCard, Active: true, Recurring: &interfaces.ProviderRecurringPaymentDetails{Status: types.RecurringPaymentStatusActive}},
		}}, nil)
		got, err := a.ListSavedMethods(ctx, "cust_1")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "token_1", got[0].GatewayMethodID)
	})

	t.Run("unsynced customer has nothing saved", func(t *testing.T) {
		a := NewPaymentMethodAdapter(nil, &stubRazorpayCustomerSvc{err: ierr.NewError("not synced").Mark(ierr.ErrNotFound)}, nil)
		got, err := a.ListSavedMethods(ctx, "cust_1")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("gateway failure propagates", func(t *testing.T) {
		a := NewPaymentMethodAdapter(nil, &stubRazorpayCustomerSvc{err: errors.New("boom")}, nil)
		_, err := a.ListSavedMethods(ctx, "cust_1")
		assert.Error(t, err)
	})
}

func TestPaymentMethodAdapterRejectsManagement(t *testing.T) {
	ctx := context.Background()
	a := NewPaymentMethodAdapter(nil, &stubRazorpayCustomerSvc{}, nil)

	assert.True(t, ierr.IsValidation(a.DeleteSavedMethod(ctx, "cust_1", "token_1")))
	assert.True(t, ierr.IsValidation(a.SetDefaultSavedMethod(ctx, "cust_1", "token_1")))
}

type setupLinkClient struct {
	RazorpayClient
	req          *AuthorizationLinkRequest
	linkCustomer string
	cancelled    []string
}

func (c *setupLinkClient) CreateAuthorizationLink(_ context.Context, req AuthorizationLinkRequest) (*AuthorizationLinkResponse, error) {
	c.req = &req
	return &AuthorizationLinkResponse{ID: "inv_link", ShortURL: "https://rzp.io/x", CustomerID: lo.CoalesceOrEmpty(c.linkCustomer, "cust_rzp"), Status: "issued", ExpireBy: req.ExpireBy}, nil
}

func (c *setupLinkClient) CancelInvoice(_ context.Context, invoiceID string) (*AuthorizationLinkResponse, error) {
	c.cancelled = append(c.cancelled, invoiceID)
	return &AuthorizationLinkResponse{ID: invoiceID, Status: "cancelled"}, nil
}

func TestCreateCustomerLink(t *testing.T) {
	refused := ierr.NewError("refused").Mark(ierr.ErrValidation)
	tests := []struct {
		name          string
		syncErr       error
		linkCustomer  string
		wantErr       bool
		wantCreated   bool
		wantCancelled []string
	}{
		{name: "link on our customer is kept", wantCreated: true},
		{name: "mismatched customer creates no link", syncErr: refused, wantErr: true},
		{name: "link on another customer is cancelled", linkCustomer: "cust_other", wantErr: true, wantCreated: true, wantCancelled: []string{"inv_link"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &setupLinkClient{linkCustomer: tt.linkCustomer}
			link, err := createCustomerLink(context.Background(), client, &stubRazorpayCustomerSvc{syncErr: tt.syncErr}, &customer.Customer{ID: "cust_1"}, AuthorizationLinkRequest{})

			assert.Equal(t, tt.wantCreated, client.req != nil)
			assert.Equal(t, tt.wantCancelled, client.cancelled)
			if tt.wantErr {
				assert.True(t, ierr.IsValidation(err))
				assert.Nil(t, link)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "inv_link", link.ID)
		})
	}
}

func TestPaymentMethodAdapterCreateSetupLink(t *testing.T) {
	maxAmount := decimal.NewFromInt(500000)
	full := &customer.Customer{Name: "Asha", Email: "asha@example.com", Contact: lo.ToPtr("9000090000")}

	tests := []struct {
		name          string
		customer      *customer.Customer
		maxAmount     *decimal.Decimal
		wantErr       bool
		wantMaxAmount int64
	}{
		{name: "default max amount is left to razorpay", customer: full},
		{name: "max amount is sent in paise", customer: full, maxAmount: &maxAmount, wantMaxAmount: 50000000},
		{name: "missing phone is refused", customer: &customer.Customer{Name: "Asha", Email: "asha@example.com"}, wantErr: true},
		{name: "missing email is refused", customer: &customer.Customer{Name: "Asha", Contact: lo.ToPtr("9000090000")}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &setupLinkClient{}
			customerSvc := &stubRazorpayCustomerSvc{customer: tt.customer}
			a := NewPaymentMethodAdapter(client, customerSvc, nil)

			resp, err := a.CreateSetupLink(context.Background(), interfaces.SetupLinkRequest{CustomerID: "cust_1", MaxAmount: tt.maxAmount})
			if tt.wantErr {
				assert.True(t, ierr.IsValidation(err))
				assert.Nil(t, client.req)
				assert.Empty(t, customerSvc.savedLink)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "https://rzp.io/x", resp.URL)
			assert.Equal(t, "inv_link", resp.ProviderSessionID)
			assert.Equal(t, "issued", resp.Status)
			assert.Equal(t, "inv_link", customerSvc.savedLink)
			require.NotNil(t, resp.ExpiresAt)

			assert.Zero(t, client.req.Amount)
			assert.Equal(t, map[string]interface{}{"flexprice_customer_id": "cust_1"}, client.req.Notes)
			assert.Equal(t, resp.ExpiresAt.Unix(), client.req.ExpireBy)
			assert.Equal(t, SubscriptionRegistration{Method: "emandate", MaxAmount: tt.wantMaxAmount}, client.req.SubscriptionRegistration)
		})
	}
}

func TestMissingMandateContact(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		contact *string
		want    string
	}{
		{name: "both present", email: "a@example.com", contact: lo.ToPtr("9000090000")},
		{name: "no phone", email: "a@example.com", want: "a phone number"},
		{name: "no email", contact: lo.ToPtr("9000090000"), want: "an email"},
		{name: "neither", want: "an email and a phone number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, missingMandateContact(&customer.Customer{Email: tt.email, Contact: tt.contact}))
		})
	}
}

func TestSelectSavedTokenHonoursChosenMandate(t *testing.T) {
	enach := func(id string, created time.Time) *interfaces.ProviderPaymentMethod {
		max := decimal.NewFromInt(1000)
		return &interfaces.ProviderPaymentMethod{
			GatewayMethodID: id, Method: types.PaymentMethodTypeENACH, Active: true, CreatedAt: created,
			Recurring: &interfaces.ProviderRecurringPaymentDetails{Status: types.RecurringPaymentStatusActive, MaxAmount: &max},
		}
	}
	svc := &PaymentService{customerSvc: &stubRazorpayCustomerSvc{tokens: []*interfaces.ProviderPaymentMethod{
		enach("token_old", time.Unix(1, 0)), enach("token_new", time.Unix(2, 0)),
	}}, logger: logger.NewNoopLogger()}
	ctx := context.Background()

	_, picked, err := svc.SelectSavedToken(ctx, "cust_1", decimal.NewFromInt(10), types.PaymentMethodTypeENACH, "")
	require.NoError(t, err)
	assert.Equal(t, "token_new", picked.GatewayMethodID, "without a choice, the newest usable mandate")

	_, picked, err = svc.SelectSavedToken(ctx, "cust_1", decimal.NewFromInt(10), types.PaymentMethodTypeENACH, "token_old")
	require.NoError(t, err)
	assert.Equal(t, "token_old", picked.GatewayMethodID, "the chosen mandate is charged")

	_, picked, err = svc.SelectSavedToken(ctx, "cust_1", decimal.NewFromInt(10), types.PaymentMethodTypeENACH, "token_gone")
	require.NoError(t, err)
	assert.Nil(t, picked, "an unknown mandate is not swapped for another")
}
