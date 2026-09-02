package service

import (
	"context"

	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// authorizeWallet ensures the wallet belongs to the portal-session customer.
// Without this a token holder could top up somebody else's wallet.
func (s *customerPortalService) authorizeWallet(ctx context.Context, walletID string) (*wallet.Wallet, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}
	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}
	if w.CustomerID != customerID {
		return nil, ierr.NewError("wallet does not belong to this customer").
			WithHint("You do not have access to this wallet").
			Mark(ierr.ErrPermissionDenied)
	}

	return w, nil
}

func (s *customerPortalService) authorizeSession(ctx context.Context, sessionID string) (*domainCheckout.CheckoutSession, error) {
	customerID := types.GetCustomerID(ctx)
	if customerID == "" {
		return nil, ierr.NewError("customer not found in context").Mark(ierr.ErrPermissionDenied)
	}
	session, err := s.CheckoutSessionRepo.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.CustomerID != customerID {
		return nil, ierr.NewError("checkout session does not belong to this customer").
			WithHint("You do not have access to this checkout session").
			Mark(ierr.ErrPermissionDenied)
	}
	return session, nil
}
