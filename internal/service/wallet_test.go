package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type WalletServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  WalletService
	testData struct {
		wallet *wallet.Wallet
	}
}

func TestWalletService(t *testing.T) {
	suite.Run(t, new(WalletServiceSuite))
}

func (s *WalletServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

// TearDownTest is called after each test
func (s *WalletServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *WalletServiceSuite) setupService() {
	stores := s.GetStores()
	s.service = NewWalletService(
		stores.WalletRepo,
		s.GetLogger(),
		stores.SubscriptionRepo,
		stores.PlanRepo,
		stores.PriceRepo,
		stores.EventRepo,
		stores.MeterRepo,
		stores.CustomerRepo,
		stores.InvoiceRepo,
		s.GetDB(),
		s.GetPublisher(),
	)
}

func (s *WalletServiceSuite) setupTestData() {
	s.testData.wallet = &wallet.Wallet{
		ID:           "wallet-1",
		CustomerID:   "customer-1",
		Currency:     "USD",
		Balance:      decimal.NewFromInt(1000),
		WalletStatus: types.WalletStatusActive,
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallet))
}

func (s *WalletServiceSuite) TestCreateWallet() {
	req := &dto.CreateWalletRequest{
		CustomerID: "customer-2",
		Currency:   "USD",
		Metadata:   types.Metadata{"key": "value"},
	}

	resp, err := s.service.CreateWallet(s.GetContext(), req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.CustomerID, resp.CustomerID)
	s.Equal(req.Currency, resp.Currency)
	s.Equal(decimal.Zero, resp.Balance)
}

func (s *WalletServiceSuite) TestGetWalletByID() {
	resp, err := s.service.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(s.testData.wallet.CustomerID, resp.CustomerID)
	s.Equal(s.testData.wallet.Currency, resp.Currency)
	s.Equal(s.testData.wallet.Balance, resp.Balance)
}

func (s *WalletServiceSuite) TestGetWalletsByCustomerID() {
	// Create another wallet for same customer
	wallet2 := &wallet.Wallet{
		ID:           "wallet-2",
		CustomerID:   s.testData.wallet.CustomerID,
		Currency:     "EUR",
		Balance:      decimal.NewFromInt(500),
		WalletStatus: types.WalletStatusActive,
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), wallet2))

	resp, err := s.service.GetWalletsByCustomerID(s.GetContext(), s.testData.wallet.CustomerID)
	s.NoError(err)
	s.Len(resp, 2)
}

func (s *WalletServiceSuite) TestTopUpWallet() {
	topUpReq := &dto.TopUpWalletRequest{
		Amount: decimal.NewFromInt(500),
	}
	resp, err := s.service.TopUpWallet(s.GetContext(), s.testData.wallet.ID, topUpReq)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(decimal.NewFromInt(1500), resp.Balance)
}

func (s *WalletServiceSuite) TestTerminateWallet() {
	err := s.service.TerminateWallet(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)

	// Verify the wallet status
	updatedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	s.Equal(types.WalletStatusClosed, updatedWallet.WalletStatus)
	s.Equal(decimal.NewFromInt(0).Equal(updatedWallet.Balance), true)

	// Verify transaction creation
	transactions, err := s.GetStores().WalletRepo.GetTransactionsByWalletID(s.GetContext(), s.testData.wallet.ID, 10, 0)
	s.NoError(err)
	s.Len(transactions, 1)
	s.Equal(types.TransactionTypeDebit, transactions[0].Type)
	s.Equal(decimal.NewFromInt(1000).Equal(transactions[0].Amount), true)
}
