package enterprise

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// PrepaidCreditsService provides enterprise prepaid usage with credits (credit grants + wallet).
// Only enterprise feature methods are exposed.
type PrepaidCreditsService interface {
	// Credit grant (prepaid usage with credits)
	CreateCreditGrant(ctx context.Context, req dto.CreateCreditGrantRequest) (*dto.CreditGrantResponse, error)
	GetCreditGrant(ctx context.Context, id string) (*dto.CreditGrantResponse, error)
	ListCreditGrants(ctx context.Context, filter *types.CreditGrantFilter) (*dto.ListCreditGrantsResponse, error)

	// Wallet (prepaid balance)
	CreateWallet(ctx context.Context, req *dto.CreateWalletRequest) (*dto.WalletResponse, error)
	GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*dto.WalletResponse, error)
	GetWalletByID(ctx context.Context, id string) (*dto.WalletResponse, error)
	TopUpWallet(ctx context.Context, walletID string, req *dto.TopUpWalletRequest) (*dto.TopUpWalletResponse, error)
	GetWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error)
	GetCustomerWallets(ctx context.Context, req *dto.GetCustomerWalletsRequest) ([]*dto.WalletBalanceResponse, error)
}

type prepaidCreditsService struct {
	EnterpriseParams
	creditGrantSvc CreditGrantService
	walletSvc      WalletService
}

// NewPrepaidCreditsService creates a new enterprise prepaid credits service.
// Composes enterprise CreditGrantService and WalletService (full implementations, no service wrapper).
func NewPrepaidCreditsService(p EnterpriseParams) PrepaidCreditsService {
	return &prepaidCreditsService{
		EnterpriseParams: p,
		creditGrantSvc:   NewCreditGrantService(p),
		walletSvc:        NewWalletService(p),
	}
}

func (s *prepaidCreditsService) CreateCreditGrant(ctx context.Context, req dto.CreateCreditGrantRequest) (*dto.CreditGrantResponse, error) {
	return s.creditGrantSvc.CreateCreditGrant(ctx, req)
}

func (s *prepaidCreditsService) GetCreditGrant(ctx context.Context, id string) (*dto.CreditGrantResponse, error) {
	return s.creditGrantSvc.GetCreditGrant(ctx, id)
}

func (s *prepaidCreditsService) ListCreditGrants(ctx context.Context, filter *types.CreditGrantFilter) (*dto.ListCreditGrantsResponse, error) {
	return s.creditGrantSvc.ListCreditGrants(ctx, filter)
}

func (s *prepaidCreditsService) CreateWallet(ctx context.Context, req *dto.CreateWalletRequest) (*dto.WalletResponse, error) {
	return s.walletSvc.CreateWallet(ctx, req)
}

func (s *prepaidCreditsService) GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*dto.WalletResponse, error) {
	return s.walletSvc.GetWalletsByCustomerID(ctx, customerID)
}

func (s *prepaidCreditsService) GetWalletByID(ctx context.Context, id string) (*dto.WalletResponse, error) {
	return s.walletSvc.GetWalletByID(ctx, id)
}

func (s *prepaidCreditsService) TopUpWallet(ctx context.Context, walletID string, req *dto.TopUpWalletRequest) (*dto.TopUpWalletResponse, error) {
	return s.walletSvc.TopUpWallet(ctx, walletID, req)
}

func (s *prepaidCreditsService) GetWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error) {
	return s.walletSvc.GetWalletBalance(ctx, walletID)
}

func (s *prepaidCreditsService) GetCustomerWallets(ctx context.Context, req *dto.GetCustomerWalletsRequest) ([]*dto.WalletBalanceResponse, error) {
	return s.walletSvc.GetCustomerWallets(ctx, req)
}
