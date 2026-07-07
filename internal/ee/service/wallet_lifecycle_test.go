package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// WalletLifecycleSuite covers UpdateWallet, ModifyWallet (prepaid → postpaid
// conversion), GetWallets, GetCustomerWallets and CheckBalanceThresholds.
type WalletLifecycleSuite struct {
	testutil.BaseServiceTestSuite
	service  WalletService
	testData struct {
		customer *customer.Customer
		now      time.Time
	}
}

func TestWalletLifecycleService(t *testing.T) {
	suite.Run(t, new(WalletLifecycleSuite))
}

func (s *WalletLifecycleSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewWalletService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         "cust_lifecycle",
		ExternalID: "ext_cust_lifecycle",
		Name:       "Lifecycle Customer",
		Email:      "lifecycle@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))
}

// createWallet creates a wallet with a backing credit transaction so the
// balance is actually consumable by debit operations.
func (s *WalletLifecycleSuite) createWallet(id string, walletType types.WalletType, currency string, balance decimal.Decimal, opts ...func(*wallet.Wallet)) *wallet.Wallet {
	w := &wallet.Wallet{
		ID:                  id,
		CustomerID:          s.testData.customer.ID,
		Name:                "Wallet " + id,
		Currency:            currency,
		Balance:             balance,
		CreditBalance:       balance,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		WalletType:          walletType,
		AlertState:          types.AlertStateOk,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	for _, opt := range opts {
		opt(w)
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), w))

	if balance.GreaterThan(decimal.Zero) {
		s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), &wallet.Transaction{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			WalletID:            w.ID,
			CustomerID:          w.CustomerID,
			Type:                types.TransactionTypeCredit,
			Amount:              balance,
			CreditAmount:        balance,
			CreditsAvailable:    balance,
			CreditBalanceBefore: decimal.Zero,
			CreditBalanceAfter:  balance,
			TxStatus:            types.TransactionStatusCompleted,
			ReferenceType:       types.WalletTxReferenceTypeExternal,
			ReferenceID:         "seed_" + w.ID,
			IdempotencyKey:      "seed_" + w.ID,
			Currency:            currency,
			BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
		}))
	}
	return w
}

func (s *WalletLifecycleSuite) TestUpdateWallet() {
	testCases := []struct {
		name          string
		setup         func() string // returns wallet ID to update
		req           *dto.UpdateWalletRequest
		expectedError bool
		verify        func(walletID string)
	}{
		{
			name: "updates_name_description_and_metadata",
			setup: func() string {
				return s.createWallet("wallet_upd_basic", types.WalletTypePrePaid, "usd", decimal.Zero).ID
			},
			req: &dto.UpdateWalletRequest{
				Name:        lo.ToPtr("Renamed Wallet"),
				Description: lo.ToPtr("Updated description"),
				Metadata:    lo.ToPtr(types.Metadata{"team": "billing"}),
			},
			verify: func(walletID string) {
				stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), walletID)
				s.NoError(err)
				s.Equal("Renamed Wallet", stored.Name)
				s.Equal("Updated description", stored.Description)
				s.Equal("billing", stored.Metadata["team"])
			},
		},
		{
			name: "merges_partial_auto_topup_with_existing_settings",
			setup: func() string {
				return s.createWallet("wallet_upd_topup", types.WalletTypePrePaid, "usd", decimal.Zero, func(w *wallet.Wallet) {
					w.AutoTopup = &types.AutoTopup{
						Enabled:   lo.ToPtr(true),
						Threshold: lo.ToPtr(decimal.NewFromInt(10)),
						Amount:    lo.ToPtr(decimal.NewFromInt(50)),
					}
				}).ID
			},
			req: &dto.UpdateWalletRequest{
				AutoTopup: &types.AutoTopup{Threshold: lo.ToPtr(decimal.NewFromInt(25))},
			},
			verify: func(walletID string) {
				stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), walletID)
				s.NoError(err)
				s.NotNil(stored.AutoTopup)
				// Only threshold changes; enabled and amount are preserved.
				s.True(lo.FromPtr(stored.AutoTopup.Enabled))
				s.True(stored.AutoTopup.Threshold.Equal(decimal.NewFromInt(25)))
				s.True(stored.AutoTopup.Amount.Equal(decimal.NewFromInt(50)))
			},
		},
		{
			name: "sets_auto_topup_when_none_exists",
			setup: func() string {
				return s.createWallet("wallet_upd_topup_new", types.WalletTypePrePaid, "usd", decimal.Zero).ID
			},
			req: &dto.UpdateWalletRequest{
				AutoTopup: &types.AutoTopup{
					Enabled:   lo.ToPtr(true),
					Threshold: lo.ToPtr(decimal.NewFromInt(5)),
					Amount:    lo.ToPtr(decimal.NewFromInt(100)),
				},
			},
			verify: func(walletID string) {
				stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), walletID)
				s.NoError(err)
				s.NotNil(stored.AutoTopup)
				s.True(lo.FromPtr(stored.AutoTopup.Enabled))
				s.True(stored.AutoTopup.Threshold.Equal(decimal.NewFromInt(5)))
				s.True(stored.AutoTopup.Amount.Equal(decimal.NewFromInt(100)))
			},
		},
		{
			name: "updates_config_allowed_price_types",
			setup: func() string {
				return s.createWallet("wallet_upd_config", types.WalletTypePrePaid, "usd", decimal.Zero).ID
			},
			req: &dto.UpdateWalletRequest{
				Config: &types.WalletConfig{
					AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
				},
			},
			verify: func(walletID string) {
				stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), walletID)
				s.NoError(err)
				s.Equal([]types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed}, stored.Config.AllowedPriceTypes)
			},
		},
		{
			name: "disabling_alerts_resets_alert_state_to_ok",
			setup: func() string {
				w := s.createWallet("wallet_upd_alerts", types.WalletTypePrePaid, "usd", decimal.Zero, func(w *wallet.Wallet) {
					w.AlertSettings = &types.AlertSettings{
						AlertEnabled: lo.ToPtr(true),
						Critical: &types.AlertThreshold{
							Threshold: decimal.NewFromInt(100),
							Condition: types.AlertConditionBelow,
						},
					}
					w.AlertState = types.AlertStateInAlarm
				})
				return w.ID
			},
			req: &dto.UpdateWalletRequest{
				AlertSettings: &types.AlertSettings{AlertEnabled: lo.ToPtr(false)},
			},
			verify: func(walletID string) {
				stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), walletID)
				s.NoError(err)
				s.Equal(types.AlertStateOk, stored.AlertState)
				s.False(stored.AlertSettings.IsAlertEnabled())
			},
		},
		{
			name: "invalid_config_price_type_returns_validation_error",
			setup: func() string {
				return s.createWallet("wallet_upd_bad_config", types.WalletTypePrePaid, "usd", decimal.Zero, func(w *wallet.Wallet) {
					w.Name = "Original Name"
				}).ID
			},
			req: &dto.UpdateWalletRequest{
				Name: lo.ToPtr("Should Not Apply"),
				Config: &types.WalletConfig{
					AllowedPriceTypes: []types.WalletConfigPriceType{"bogus"},
				},
			},
			expectedError: true,
			verify: func(walletID string) {
				stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), walletID)
				s.NoError(err)
				s.Equal("Original Name", stored.Name, "failed validation must not mutate the wallet")
			},
		},
		{
			name:          "wallet_not_found_returns_error",
			setup:         func() string { return "wallet_does_not_exist" },
			req:           &dto.UpdateWalletRequest{Name: lo.ToPtr("New Name")},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			walletID := tc.setup()
			updated, err := s.service.UpdateWallet(s.GetContext(), walletID, tc.req)
			if tc.expectedError {
				s.Error(err)
			} else {
				s.NoError(err)
				s.NotNil(updated)
				s.Equal(walletID, updated.ID)
			}
			if tc.verify != nil {
				tc.verify(walletID)
			}
		})
	}
}

func (s *WalletLifecycleSuite) TestModifyWalletValidation() {
	testCases := []struct {
		name             string
		modificationType dto.WalletModificationType
	}{
		{name: "unknown_modification_type_returns_validation_error", modificationType: "downgrade"},
		{name: "empty_modification_type_returns_validation_error", modificationType: ""},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.ModifyWallet(s.GetContext(), "wallet_any", &dto.ModifyWalletRequest{
				ModificationType: tc.modificationType,
			})
			s.Error(err)
			s.True(ierr.IsValidation(err))
			s.Nil(resp)
		})
	}
}

func (s *WalletLifecycleSuite) TestModifyWalletConvertToPostpaid() {
	convertReq := &dto.ModifyWalletRequest{ModificationType: dto.WalletModificationTypePrepaidToPostpaid}

	s.Run("converts_prepaid_wallet_and_transfers_credits", func() {
		balance := decimal.RequireFromString("123.45")
		original := s.createWallet("wallet_convert_balance", types.WalletTypePrePaid, "usd", balance)

		resp, err := s.service.ModifyWallet(s.GetContext(), original.ID, convertReq)
		s.NoError(err)
		s.NotNil(resp)

		// Response reflects the conversion.
		s.Equal(original.ID, resp.OriginalWallet.ID)
		s.Equal(types.WalletStatusClosed, resp.OriginalWallet.WalletStatus)
		s.Equal(types.WalletTypePostPaid, resp.NewWallet.WalletType)
		s.NotEqual(original.ID, resp.NewWallet.ID)

		// Read back the original wallet: closed with zero balances.
		storedOriginal, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), original.ID)
		s.NoError(err)
		s.Equal(types.WalletStatusClosed, storedOriginal.WalletStatus)
		s.True(storedOriginal.CreditBalance.IsZero())
		s.True(storedOriginal.Balance.IsZero())

		// Read back the new wallet: active postpaid carrying the transferred credits.
		// NOTE: the new wallet's balance is asserted through the transfer
		// transaction snapshots below rather than the wallet record — the
		// in-memory store keeps the service's pointer, so the service's
		// post-persist response bookkeeping aliases the stored record.
		storedNew, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), resp.NewWallet.ID)
		s.NoError(err)
		s.Equal(types.WalletStatusActive, storedNew.WalletStatus)
		s.Equal(types.WalletTypePostPaid, storedNew.WalletType)
		s.Equal([]types.WalletConfigPriceType{types.WalletConfigPriceTypeAll}, storedNew.Config.AllowedPriceTypes)
		s.Equal(original.ID, storedNew.Metadata["converted_from"])

		// Termination debit recorded on the original wallet.
		debits, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
			QueryFilter:       types.NewNoLimitQueryFilter(),
			WalletID:          lo.ToPtr(original.ID),
			Type:              lo.ToPtr(types.TransactionTypeDebit),
			TransactionReason: lo.ToPtr(types.TransactionReasonWalletTermination),
		})
		s.NoError(err)
		s.Len(debits, 1)
		s.True(debits[0].CreditAmount.Equal(balance))

		// Transfer credit recorded on the new wallet.
		credits, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			WalletID:    lo.ToPtr(storedNew.ID),
			Type:        lo.ToPtr(types.TransactionTypeCredit),
		})
		s.NoError(err)
		s.Len(credits, 1)
		s.True(credits[0].CreditAmount.Equal(balance))
		s.Equal(types.TransactionReasonFreeCredit, credits[0].TransactionReason)
		// Balance snapshots prove the transfer credited exactly once.
		s.True(credits[0].CreditBalanceBefore.IsZero())
		s.True(credits[0].CreditBalanceAfter.Equal(balance), "expected %s got %s", balance, credits[0].CreditBalanceAfter)
	})

	s.Run("converts_zero_balance_wallet_without_credit_transfer", func() {
		original := s.createWallet("wallet_convert_zero", types.WalletTypePrePaid, "eur", decimal.Zero)

		resp, err := s.service.ModifyWallet(s.GetContext(), original.ID, convertReq)
		s.NoError(err)

		storedNew, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), resp.NewWallet.ID)
		s.NoError(err)
		s.True(storedNew.CreditBalance.IsZero())

		txns, err := s.GetStores().WalletRepo.ListWalletTransactions(s.GetContext(), &types.WalletTransactionFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			WalletID:    lo.ToPtr(storedNew.ID),
		})
		s.NoError(err)
		s.Empty(txns, "zero balance conversion must not create transfer transactions")
	})

	s.Run("rejects_postpaid_wallet", func() {
		w := s.createWallet("wallet_convert_postpaid", types.WalletTypePostPaid, "gbp", decimal.Zero)

		resp, err := s.service.ModifyWallet(s.GetContext(), w.ID, convertReq)
		s.Error(err)
		s.True(ierr.IsInvalidOperation(err))
		s.Nil(resp)
	})

	s.Run("rejects_closed_wallet", func() {
		w := s.createWallet("wallet_convert_closed", types.WalletTypePrePaid, "inr", decimal.Zero, func(w *wallet.Wallet) {
			w.WalletStatus = types.WalletStatusClosed
		})

		resp, err := s.service.ModifyWallet(s.GetContext(), w.ID, convertReq)
		s.Error(err)
		s.True(ierr.IsInvalidOperation(err))
		s.Nil(resp)
	})

	s.Run("rejects_when_active_postpaid_wallet_with_same_currency_exists", func() {
		s.createWallet("wallet_existing_postpaid", types.WalletTypePostPaid, "aud", decimal.Zero)
		prepaid := s.createWallet("wallet_convert_conflict", types.WalletTypePrePaid, "aud", decimal.Zero)

		resp, err := s.service.ModifyWallet(s.GetContext(), prepaid.ID, convertReq)
		s.Error(err)
		s.True(ierr.IsAlreadyExists(err))
		s.Nil(resp)

		// Prepaid wallet must remain untouched.
		stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), prepaid.ID)
		s.NoError(err)
		s.Equal(types.WalletStatusActive, stored.WalletStatus)
		s.Equal(types.WalletTypePrePaid, stored.WalletType)
	})

	s.Run("empty_wallet_id_returns_validation_error", func() {
		resp, err := s.service.ModifyWallet(s.GetContext(), "", convertReq)
		s.Error(err)
		s.True(ierr.IsValidation(err))
		s.Nil(resp)
	})
}

func (s *WalletLifecycleSuite) TestGetWallets() {
	w1 := s.createWallet("wallet_list_1", types.WalletTypePrePaid, "usd", decimal.NewFromInt(10))
	w2 := s.createWallet("wallet_list_2", types.WalletTypePostPaid, "usd", decimal.NewFromInt(20))

	s.Run("nil_filter_returns_all_wallets", func() {
		resp, err := s.service.GetWallets(s.GetContext(), nil)
		s.NoError(err)
		s.Len(resp.Items, 2)
		ids := []string{resp.Items[0].ID, resp.Items[1].ID}
		s.ElementsMatch([]string{w1.ID, w2.ID}, ids)
	})

	s.Run("filters_by_wallet_ids", func() {
		filter := types.NewWalletFilter()
		filter.WalletIDs = []string{w2.ID}
		resp, err := s.service.GetWallets(s.GetContext(), filter)
		s.NoError(err)
		s.Len(resp.Items, 1)
		s.Equal(w2.ID, resp.Items[0].ID)
		s.True(resp.Items[0].Balance.Equal(decimal.NewFromInt(20)))
	})

	s.Run("returns_empty_for_unknown_wallet_id", func() {
		filter := types.NewWalletFilter()
		filter.WalletIDs = []string{"wallet_unknown"}
		resp, err := s.service.GetWallets(s.GetContext(), filter)
		s.NoError(err)
		s.Empty(resp.Items)
	})
}

func (s *WalletLifecycleSuite) TestGetCustomerWallets() {
	w1 := s.createWallet("wallet_cust_1", types.WalletTypePrePaid, "usd", decimal.NewFromInt(30))
	w2 := s.createWallet("wallet_cust_2", types.WalletTypePostPaid, "eur", decimal.NewFromInt(75))

	// Customer with no wallets at all.
	emptyCustomer := &customer.Customer{
		ID:         "cust_no_wallets_lifecycle",
		ExternalID: "ext_cust_no_wallets_lifecycle",
		Name:       "No Wallet Customer",
		Email:      "nowallets@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), emptyCustomer))

	s.Run("missing_id_and_lookup_key_returns_validation_error", func() {
		resp, err := s.service.GetCustomerWallets(s.GetContext(), &dto.GetCustomerWalletsRequest{})
		s.Error(err)
		s.True(ierr.IsValidation(err))
		s.Nil(resp)
	})

	s.Run("both_id_and_lookup_key_returns_validation_error", func() {
		resp, err := s.service.GetCustomerWallets(s.GetContext(), &dto.GetCustomerWalletsRequest{
			ID:        s.testData.customer.ID,
			LookupKey: s.testData.customer.ExternalID,
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
		s.Nil(resp)
	})

	s.Run("unknown_customer_id_returns_error", func() {
		resp, err := s.service.GetCustomerWallets(s.GetContext(), &dto.GetCustomerWalletsRequest{ID: "cust_missing"})
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("unknown_lookup_key_returns_error", func() {
		resp, err := s.service.GetCustomerWallets(s.GetContext(), &dto.GetCustomerWalletsRequest{LookupKey: "ext_missing"})
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("returns_wallets_by_customer_id_without_real_time_balance", func() {
		resp, err := s.service.GetCustomerWallets(s.GetContext(), &dto.GetCustomerWalletsRequest{ID: s.testData.customer.ID})
		s.NoError(err)
		s.Len(resp, 2)
		ids := []string{resp[0].Wallet.ID, resp[1].Wallet.ID}
		s.ElementsMatch([]string{w1.ID, w2.ID}, ids)
		for _, item := range resp {
			s.Nil(item.RealTimeBalance, "real-time balance must not be computed unless requested")
		}
	})

	s.Run("returns_wallets_by_lookup_key", func() {
		resp, err := s.service.GetCustomerWallets(s.GetContext(), &dto.GetCustomerWalletsRequest{LookupKey: s.testData.customer.ExternalID})
		s.NoError(err)
		s.Len(resp, 2)
	})

	s.Run("returns_empty_slice_for_customer_without_wallets", func() {
		resp, err := s.service.GetCustomerWallets(s.GetContext(), &dto.GetCustomerWalletsRequest{ID: emptyCustomer.ID})
		s.NoError(err)
		s.NotNil(resp)
		s.Empty(resp)
	})

	s.Run("includes_real_time_balance_when_requested", func() {
		resp, err := s.service.GetCustomerWallets(s.GetContext(), &dto.GetCustomerWalletsRequest{
			ID:                     s.testData.customer.ID,
			IncludeRealTimeBalance: true,
		})
		s.NoError(err)
		s.Len(resp, 2)
		byID := map[string]*dto.WalletBalanceResponse{}
		for _, item := range resp {
			byID[item.Wallet.ID] = item
		}
		// Postpaid wallet real-time balance equals its stored balance.
		s.NotNil(byID[w2.ID].RealTimeBalance)
		s.True(byID[w2.ID].RealTimeBalance.Equal(decimal.NewFromInt(75)))
		// Prepaid wallet with no subscriptions or unpaid invoices keeps full balance.
		s.NotNil(byID[w1.ID].RealTimeBalance)
		s.True(byID[w1.ID].RealTimeBalance.Equal(decimal.NewFromInt(30)))
	})
}

func (s *WalletLifecycleSuite) TestCheckBalanceThresholds() {
	alertSettings := &types.AlertSettings{
		AlertEnabled: lo.ToPtr(true),
		Critical: &types.AlertThreshold{
			Threshold: decimal.NewFromInt(100),
			Condition: types.AlertConditionBelow,
		},
	}

	balanceOf := func(w *wallet.Wallet, amount decimal.Decimal) *dto.WalletBalanceResponse {
		return &dto.WalletBalanceResponse{
			Wallet:                w,
			RealTimeBalance:       lo.ToPtr(amount),
			RealTimeCreditBalance: lo.ToPtr(amount),
		}
	}

	s.Run("noop_when_alerts_not_enabled", func() {
		w := s.createWallet("wallet_thresh_disabled", types.WalletTypePostPaid, "usd", decimal.NewFromInt(10))

		err := s.service.CheckBalanceThresholds(s.GetContext(), w, balanceOf(w, decimal.NewFromInt(10)))
		s.NoError(err)

		stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
		s.NoError(err)
		s.Equal(types.AlertStateOk, stored.AlertState)
	})

	s.Run("sets_alarm_state_when_balance_below_critical_threshold", func() {
		w := s.createWallet("wallet_thresh_breach", types.WalletTypePostPaid, "usd", decimal.NewFromInt(40), func(w *wallet.Wallet) {
			w.AlertSettings = alertSettings
			w.AlertState = types.AlertStateOk
		})

		err := s.service.CheckBalanceThresholds(s.GetContext(), w, balanceOf(w, decimal.NewFromInt(40)))
		s.NoError(err)

		stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
		s.NoError(err)
		s.Equal(types.AlertStateInAlarm, stored.AlertState)
	})

	s.Run("skips_when_already_in_alarm_state", func() {
		w := s.createWallet("wallet_thresh_alarm", types.WalletTypePostPaid, "usd", decimal.NewFromInt(40), func(w *wallet.Wallet) {
			w.AlertSettings = alertSettings
			w.AlertState = types.AlertStateInAlarm
		})

		err := s.service.CheckBalanceThresholds(s.GetContext(), w, balanceOf(w, decimal.NewFromInt(40)))
		s.NoError(err)

		stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
		s.NoError(err)
		s.Equal(types.AlertStateInAlarm, stored.AlertState)
	})

	s.Run("recovers_to_ok_when_balance_back_above_threshold", func() {
		w := s.createWallet("wallet_thresh_recover", types.WalletTypePostPaid, "usd", decimal.NewFromInt(500), func(w *wallet.Wallet) {
			w.AlertSettings = alertSettings
			w.AlertState = types.AlertStateInAlarm
		})

		err := s.service.CheckBalanceThresholds(s.GetContext(), w, balanceOf(w, decimal.NewFromInt(500)))
		s.NoError(err)

		stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
		s.NoError(err)
		s.Equal(types.AlertStateOk, stored.AlertState)
	})

	s.Run("stays_ok_when_balance_healthy_and_state_ok", func() {
		w := s.createWallet("wallet_thresh_healthy", types.WalletTypePostPaid, "usd", decimal.NewFromInt(500), func(w *wallet.Wallet) {
			w.AlertSettings = alertSettings
		})

		err := s.service.CheckBalanceThresholds(s.GetContext(), w, balanceOf(w, decimal.NewFromInt(500)))
		s.NoError(err)

		stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
		s.NoError(err)
		s.Equal(types.AlertStateOk, stored.AlertState)
	})

	s.Run("falls_back_to_wallet_balance_when_realtime_values_nil", func() {
		w := s.createWallet("wallet_thresh_nil_balance", types.WalletTypePostPaid, "usd", decimal.NewFromInt(20), func(w *wallet.Wallet) {
			w.AlertSettings = alertSettings
		})

		err := s.service.CheckBalanceThresholds(s.GetContext(), w, &dto.WalletBalanceResponse{Wallet: w})
		s.NoError(err)

		stored, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), w.ID)
		s.NoError(err)
		s.Equal(types.AlertStateInAlarm, stored.AlertState, "wallet balance of 20 is below the critical threshold of 100")
	})
}
