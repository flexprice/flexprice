package service

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// TopUpWallet starts a pay-first wallet top-up.
//
// use_saved_method selects collection_method=charge_automatically so the adapter
// charges off-session; otherwise the adapter returns a redirect action.
func (s *customerPortalService) TopUpWallet(ctx context.Context, walletID string, req *dto.PortalTopUpWalletRequest) (*dto.PortalTopUpWalletResponse, error) {
	if req == nil {
		return nil, ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}

	w, err := s.authorizeWallet(ctx, walletID)
	if err != nil {
		return nil, err
	}
	if err := s.validateTopupAmount(ctx, w, req.CreditsToAdd); err != nil {
		return nil, err
	}

	// Everything the portal customer must not choose is pinned here, not taken
	// from the request: reason (so they cannot grant themselves free credits),
	// expiry (none), priority (nil -> consumed after prioritized grants), the
	// provider config, and auto-complete (which would grant credits before the
	// invoice is paid).
	walletReq := &dto.TopUpWalletRequest{
		CreditsToAdd:      req.CreditsToAdd,
		Amount:            req.Amount,
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		IdempotencyKey:    req.IdempotencyKey,
		Description:       req.Description,
		TriggeringActor:   types.TriggeringActorEndCustomer,
	}
	if walletReq.Description == "" {
		walletReq.Description = "Wallet top-up from customer portal"
	}

	if req.Checkout != nil {
		provider, err := s.resolveCheckoutProvider(ctx, w.CustomerID, req.Checkout.PaymentProvider)
		if err != nil {
			return nil, err
		}

		// An abandoned session would otherwise lock the wallet until it expires, so
		// hand back the one already in flight rather than a conflict the customer
		// cannot act on.
		if existing, err := s.pendingTopupSession(ctx, w.CustomerID, walletID); err != nil {
			return nil, err
		} else if existing != nil {
			return &dto.PortalTopUpWalletResponse{
				Wallet:          dto.FromWallet(w),
				CheckoutSession: existing,
			}, nil
		}

		collectionMethod := types.CollectionMethodSendInvoice
		if req.Checkout.UseSavedMethod {
			collectionMethod = types.CollectionMethodChargeAutomatically
		}

		idempotencyKey := req.Checkout.IdempotencyKey
		if idempotencyKey == nil {
			idempotencyKey = req.IdempotencyKey
		}

		walletReq.Checkout = &dto.CheckoutParams{
			PaymentParams: dto.PaymentParams{
				PaymentProvider: provider,
				PaymentProviderConfig: &types.CheckoutPaymentProviderConfig{
					CollectionMethod: collectionMethod,
					CustomerPresent:  true,
				},
			},
			RedirectionParams: req.Checkout.RedirectionParams,
			IdempotencyKey:    idempotencyKey,
			Metadata:          req.Checkout.Metadata,
		}
	}

	walletSvc := NewWalletService(s.ServiceParams)
	topupResp, err := walletSvc.TopUpWallet(ctx, walletID, walletReq)
	if err != nil {
		return nil, err
	}

	resp := &dto.PortalTopUpWalletResponse{
		WalletTransaction: topupResp.WalletTransaction,
		InvoiceID:         topupResp.InvoiceID,
		Wallet:            topupResp.Wallet,
	}

	// No checkout requested: invoiced / pay-later behavior, nothing more to do.
	if req.Checkout == nil {
		return resp, nil
	}

	if topupResp.CheckoutSession == nil {
		return nil, ierr.NewError("top-up did not produce a checkout session").
			WithHint("Expected a pay-first checkout session").
			Mark(ierr.ErrInternal)
	}

	resp.CheckoutSession = toPortalCheckoutSession(topupResp.CheckoutSession)
	return resp, nil
}

func (s *customerPortalService) UpdateAutoTopup(ctx context.Context, walletID string, req *dto.PortalUpdateAutoTopupRequest) (*dto.WalletResponse, error) {
	if req == nil {
		return nil, ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}

	w, err := s.authorizeWallet(ctx, walletID)
	if err != nil {
		return nil, err
	}

	// Invoicing is pinned, never taken from the request: it selects the transaction
	// reason and so decides whether credits are paid for at all.
	autoTopup := &types.AutoTopup{
		Enabled:   lo.ToPtr(req.Enabled),
		Threshold: req.Threshold,
		Amount:    req.Amount,
		Invoicing: lo.ToPtr(true),
		Cooldown:  req.Cooldown,
	}

	if req.Enabled {
		if req.Amount == nil || req.Threshold == nil {
			return nil, ierr.NewError("threshold and amount are required to enable auto top-up").
				WithHint("Specify both the balance threshold and the amount to add").
				Mark(ierr.ErrValidation)
		}
		if err := s.validateTopupAmount(ctx, w, *req.Amount); err != nil {
			return nil, err
		}
		gateway, err := fetchGatewayWithAutoChargeSupport(ctx, s.ServiceParams, s.customerService, w.CustomerID)
		if err != nil {
			return nil, err
		}
		if gateway == "" {
			return nil, ierr.NewError("no payment method can be charged automatically").
				WithHint("Add a payment method that supports automatic charges before enabling auto top-up").
				Mark(ierr.ErrInvalidOperation)
		}
	}

	updated, err := NewWalletService(s.ServiceParams).UpdateWallet(ctx, walletID, &dto.UpdateWalletRequest{
		AutoTopup: autoTopup,
	})
	if err != nil {
		return nil, err
	}
	return dto.FromWallet(updated), nil
}

// resolveCheckoutProvider turns an optional caller choice into the one gateway that
// can host this checkout, then back into the checkout vocabulary.
func (s *customerPortalService) resolveCheckoutProvider(
	ctx context.Context,
	customerID string,
	requested *types.PaymentGatewayType,
) (types.CheckoutPaymentProvider, error) {
	resolved, err := NewPaymentProviderResolver(s.ServiceParams).
		ResolveProvider(ctx, customerID, types.IntegrationCapabilityCheckout, lo.FromPtr(requested))
	if err != nil {
		return "", err
	}

	provider, ok := types.CheckoutProviderFromGateway(resolved)
	if !ok {
		return "", ierr.NewError("resolved provider cannot host a checkout").
			WithReportableDetails(map[string]any{"gateway": resolved}).
			Mark(ierr.ErrInternal)
	}
	return provider, nil
}

func (s *customerPortalService) pendingTopupSession(
	ctx context.Context,
	customerID string,
	walletID string,
) (*dto.PortalCheckoutSessionResponse, error) {
	walletSvc := NewWalletService(s.ServiceParams).(*walletService)
	existing, err := walletSvc.getAnyPendingCheckoutSession(ctx, customerID, walletID)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		return nil, nil
	}

	return toPortalCheckoutSession(dto.ToCheckoutSessionResponse(existing[0])), nil
}

// validateTopupAmount rejects a credit amount that converts to less than the
// tenant's per-currency floor, which the gateway would otherwise reject only
// after the customer has been redirected.
func (s *customerPortalService) validateTopupAmount(ctx context.Context, w *wallet.Wallet, creditsToAdd decimal.Decimal) error {
	if creditsToAdd.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("credits must be greater than zero").
			WithHint("Enter a credit amount greater than zero").
			Mark(ierr.ErrValidation)
	}

	minAmount, err := s.minTopupAmount(ctx, w.Currency)
	if err != nil {
		return err
	}
	if !minAmount.IsPositive() {
		return nil
	}

	amount := NewWalletService(s.ServiceParams).GetCurrencyAmountFromCredits(creditsToAdd, w.TopupConversionRate)
	if amount.LessThan(minAmount) {
		return ierr.NewError("top-up amount is below the minimum").
			WithHintf("Minimum top-up for %s is %s", strings.ToUpper(w.Currency), minAmount.String()).
			WithReportableDetails(map[string]any{
				"currency":       w.Currency,
				"minimum_amount": minAmount.String(),
				"amount":         amount.String(),
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (s *customerPortalService) minTopupAmount(ctx context.Context, currency string) (decimal.Decimal, error) {
	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
	cfg, err := GetSetting[types.WalletTopupConfig](settingsSvc, ctx, types.SettingKeyWalletTopupConfig)
	if err != nil {
		return decimal.Zero, err
	}

	return cfg.MinTopupAmount(currency), nil
}
