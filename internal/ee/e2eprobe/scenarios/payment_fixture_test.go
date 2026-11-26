package scenarios

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// paymentFixture binds a payer persona for one gateway to the fake. Wallet
// credits are one shared balance served through the balance override, so hooks
// can move it the way a settled payment would.
type paymentFixture struct {
	*testDeps
	persona string

	mu      sync.Mutex
	credits decimal.Decimal
}

// newPaymentFixture re-provisions the default suite with the payer persona
// bound to provider; spec tunes its payment block. A fixed customer named there
// is seeded first so the persona carries no payment gap.
func newPaymentFixture(t *testing.T, provider string, spec func(*suite.Payment)) *paymentFixture {
	t.Helper()
	d := newTestDeps(t)
	payment := suite.Payment{Provider: provider, Currency: "USD", SettleTimeout: suite.Duration{Duration: 200 * time.Millisecond}}
	if spec != nil {
		spec(&payment)
	}
	fx := &paymentFixture{testDeps: d, persona: "payer-" + provider}
	s := withPayer(suite.Default())
	for i := range s.Personas {
		if s.Personas[i].Name == "payer-stripe" {
			s.Personas[i].Name, s.Personas[i].Payment = fx.persona, &payment
		}
	}
	if payment.FixedCustomer != "" {
		d.fc.Customer.Add(payment.FixedCustomer, "cust_fixed", map[string]string{"e2eprobe": "true"}, testNow.Add(-24*time.Hour))
	}
	w := world.New(d.fc, s, "run-test", nil)
	w.SetClock(d.fc.Clock)
	if err := w.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	d.Suite, d.World = s, w
	d.fc.TenantOps.Connections = []e2eprobe.Connection{{ID: "c1", ProviderType: provider}}
	d.fc.Wallet.Balance = func(id string) (*dtos.GetWalletBalanceResponse, error) {
		return &dtos.GetWalletBalanceResponse{WalletBalanceResponse: &types.WalletBalanceResponse{
			ID: probetest.Ptr(id), CreditBalance: probetest.Ptr(fx.creditBalance()),
		}}, nil
	}
	d.fc.Payment.PortalAddURL = "https://pay.example.com/add"
	d.fc.Payment.SetupURL = "https://pay.example.com/setup"
	return fx
}

// addCredits moves the shared credit balance, as a settled top-up would.
func (fx *paymentFixture) addCredits(n int64) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.credits = fx.credits.Add(decimal.NewFromInt(n))
}

func (fx *paymentFixture) creditBalance() string {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return fx.credits.String()
}

func (fx *paymentFixture) run(t *testing.T, s scenario.Scenario) e2eprobe.Result {
	t.Helper()
	return run(t, fx.testDeps, s, fx.persona)
}

// The scenarios poll every millisecond so a 200ms settle budget is exhausted quickly.
func (fx *paymentFixture) checkoutLink() *checkoutLink {
	s := newCheckoutLink(fx.Deps)
	s.poll = time.Millisecond
	return s
}

func (fx *paymentFixture) paymentMethods() *paymentMethods {
	s := newPaymentMethods(fx.Deps)
	s.poll = time.Millisecond
	return s
}

func (fx *paymentFixture) autoCharge() *autoCharge {
	s := newAutoCharge(fx.Deps)
	s.poll = time.Millisecond
	return s
}

func collectionOf(cfg *types.CheckoutParams) types.CollectionMethod {
	if cfg == nil || cfg.PaymentProviderConfig == nil || cfg.PaymentProviderConfig.CollectionMethod == nil {
		return ""
	}
	return *cfg.PaymentProviderConfig.CollectionMethod
}

func customerNotPresent(cfg *types.CheckoutParams) bool {
	return cfg != nil && cfg.PaymentProviderConfig != nil && cfg.PaymentProviderConfig.CustomerNotPresent != nil && *cfg.PaymentProviderConfig.CustomerNotPresent
}

// rejection is the API error a gateway refusal surfaces with the given status.
func rejection(status int) error {
	return sdkerrors.NewAPIError("rejected", status, fmt.Sprintf(`{"http_status_code":%d}`, status), nil)
}

// stepOf is the step of the first failure a payment result carries.
func stepOf(r e2eprobe.Result) string {
	return r.Attrs["step"]
}
