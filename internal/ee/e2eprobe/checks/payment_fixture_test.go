package checks

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// paymentFixture wires a fakeClient so wallet top-ups with checkout open
// sessions on the fake payments ops, and wallet credits can be moved by hooks.
type paymentFixture struct {
	fc   *fakeClient
	reg  e2eprobe.Registry
	opts PaymentProbeOpts
}

func newPaymentFixture(t *testing.T, provider string, driver GatewayDriver) *paymentFixture {
	t.Helper()
	fc := newFakeClient()
	fc.wallets.creditBalance = "0"
	fc.wallets.checkout = func(req types.TopUpWalletRequest) (*types.CheckoutSessionResponse, error) {
		return fc.payments.startSession("topup", req.Checkout)
	}
	fc.payments.portalAddURL = "https://pay.example.com/add"
	fc.payments.setupURL = "https://pay.example.com/setup"
	fc.payments.gatewayCustomerID = "gw_cust_1"
	return &paymentFixture{
		fc:  fc,
		reg: e2eprobe.NewRegistry(),
		opts: PaymentProbeOpts{
			Provider:      e2eprobe.PaymentProviderConfig{Provider: provider, Currency: "USD"},
			Driver:        driver,
			SettleTimeout: 200 * time.Millisecond,
			PollInterval:  time.Millisecond,
		},
	}
}

// addCredits moves the fake wallet's credit balance, as a settled top-up would.
func (fx *paymentFixture) addCredits(n int64) {
	fx.fc.wallets.mu.Lock()
	defer fx.fc.wallets.mu.Unlock()
	cur, _ := decimal.NewFromString(fx.fc.wallets.creditBalance)
	fx.fc.wallets.creditBalance = cur.Add(decimal.NewFromInt(n)).String()
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

func apiError(status int) error {
	return sdkerrors.NewAPIError("rejected", status, fmt.Sprintf(`{"http_status_code":%d}`, status), nil)
}

func stepOf(err error) string {
	return e2eprobe.AttributesFrom(err)["step"]
}

// fakeGatewayDriver vaults cards straight into the fake saved-methods listing.
type fakeGatewayDriver struct {
	mu                 sync.Mutex
	payments           *fakePayments
	attached           []TestCard
	attachErr          error
	declineUnsupported bool
	notAutoChargeable  bool
}

func (d *fakeGatewayDriver) AttachCard(_ context.Context, _ string, card TestCard) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if card == TestCardDecline && d.declineUnsupported {
		return "", ErrTestCardUnsupported
	}
	if d.attachErr != nil {
		return "", d.attachErr
	}
	d.attached = append(d.attached, card)
	id := fmt.Sprintf("pm_%s_%d", card, len(d.attached))
	d.payments.mu.Lock()
	d.payments.savedMethods = append(d.payments.savedMethods, e2eprobe.SavedPaymentMethod{
		ID: id, Status: "active", CanAutoCharge: !d.notAutoChargeable,
	})
	d.payments.mu.Unlock()
	return id, nil
}
