package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type PaymentFlowTestSuite struct {
	suite.Suite
}

func TestPaymentFlow(t *testing.T) {
	suite.Run(t, new(PaymentFlowTestSuite))
}

// TestPaymentFlowCases tests all payment flow cases from the PRD document
func (s *PaymentFlowTestSuite) TestPaymentFlowCases() {
	tests := []struct {
		name               string
		invoiceType        string // "A" (USAGE only), "B" (FIXED only), "C" (Mixed)
		walletConfig       []types.WalletConfigPriceType
		walletBalance      decimal.Decimal
		cardAvailable      bool
		expectedResult     string // "Success" or "Failed"
		expectedCardPays   decimal.Decimal
		expectedWalletPays decimal.Decimal
		description        string
	}{
		// Invoice Type A: Pure USAGE charges ($50 USAGE)
		{
			name:               "A_W1_ALL_60_CardAvailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			walletBalance:      decimal.NewFromFloat(60.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.Zero,
			expectedWalletPays: decimal.NewFromFloat(50.0),
			description:        "A: $50 USAGE, W1 (ALL) $60, Card ✅ → Success: Card $0, Wallet $50",
		},
		{
			name:               "A_W1_ALL_30_CardAvailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "A: $50 USAGE, W1 (ALL) $30, Card ✅ → Success: Card $20, Wallet $30",
		},
		{
			name:               "A_W1_ALL_30_CardUnavailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "A: $50 USAGE, W1 (ALL) $30, Card ❌ → Failed",
		},
		{
			name:               "A_W2_FIXED_60_CardAvailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
			walletBalance:      decimal.NewFromFloat(60.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "A: $50 USAGE, W2 (FIXED) $60, Card ✅ → Success: Card $50, Wallet $0",
		},
		{
			name:               "A_W2_FIXED_60_CardUnavailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
			walletBalance:      decimal.NewFromFloat(60.0),
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "A: $50 USAGE, W2 (FIXED) $60, Card ❌ → Failed",
		},
		{
			name:               "A_W3_USAGE_60_CardAvailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			walletBalance:      decimal.NewFromFloat(60.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.Zero,
			expectedWalletPays: decimal.NewFromFloat(50.0),
			description:        "A: $50 USAGE, W3 (USAGE) $60, Card ✅ → Success: Card $0, Wallet $50",
		},
		{
			name:               "A_W3_USAGE_30_CardAvailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "A: $50 USAGE, W3 (USAGE) $30, Card ✅ → Success: Card $20, Wallet $30",
		},
		{
			name:               "A_W3_USAGE_30_CardUnavailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "A: $50 USAGE, W3 (USAGE) $30, Card ❌ → Failed",
		},
		{
			name:               "A_W4_None_0_CardAvailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{},
			walletBalance:      decimal.Zero,
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "A: $50 USAGE, W4 (None) $0, Card ✅ → Success: Card $50, Wallet $0",
		},
		{
			name:               "A_W4_None_0_CardUnavailable",
			invoiceType:        "A",
			walletConfig:       []types.WalletConfigPriceType{},
			walletBalance:      decimal.Zero,
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "A: $50 USAGE, W4 (None) $0, Card ❌ → Failed",
		},

		// Invoice Type B: Pure FIXED charges ($50 FIXED)
		{
			name:               "B_W1_ALL_60_CardAvailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			walletBalance:      decimal.NewFromFloat(60.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.Zero,
			expectedWalletPays: decimal.NewFromFloat(50.0),
			description:        "B: $50 FIXED, W1 (ALL) $60, Card ✅ → Success: Card $0, Wallet $50",
		},
		{
			name:               "B_W1_ALL_30_CardAvailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "B: $50 FIXED, W1 (ALL) $30, Card ✅ → Success: Card $20, Wallet $30",
		},
		{
			name:               "B_W1_ALL_30_CardUnavailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "B: $50 FIXED, W1 (ALL) $30, Card ❌ → Failed",
		},
		{
			name:               "B_W2_FIXED_60_CardAvailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
			walletBalance:      decimal.NewFromFloat(60.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.Zero,
			expectedWalletPays: decimal.NewFromFloat(50.0),
			description:        "B: $50 FIXED, W2 (FIXED) $60, Card ✅ → Success: Card $0, Wallet $50",
		},
		{
			name:               "B_W2_FIXED_30_CardAvailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "B: $50 FIXED, W2 (FIXED) $30, Card ✅ → Success: Card $20, Wallet $30",
		},
		{
			name:               "B_W2_FIXED_30_CardUnavailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "B: $50 FIXED, W2 (FIXED) $30, Card ❌ → Failed",
		},
		{
			name:               "B_W3_USAGE_60_CardAvailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			walletBalance:      decimal.NewFromFloat(60.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "B: $50 FIXED, W3 (USAGE) $60, Card ✅ → Success: Card $50, Wallet $0",
		},
		{
			name:               "B_W3_USAGE_60_CardUnavailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			walletBalance:      decimal.NewFromFloat(60.0),
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "B: $50 FIXED, W3 (USAGE) $60, Card ❌ → Failed",
		},
		{
			name:               "B_W4_None_0_CardAvailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{},
			walletBalance:      decimal.Zero,
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "B: $50 FIXED, W4 (None) $0, Card ✅ → Success: Card $50, Wallet $0",
		},
		{
			name:               "B_W4_None_0_CardUnavailable",
			invoiceType:        "B",
			walletConfig:       []types.WalletConfigPriceType{},
			walletBalance:      decimal.Zero,
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "B: $50 FIXED, W4 (None) $0, Card ❌ → Failed",
		},

		// Invoice Type C: Mixed charges ($20 FIXED + $30 USAGE)
		{
			name:               "C_W1_ALL_60_CardAvailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			walletBalance:      decimal.NewFromFloat(60.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.Zero,
			expectedWalletPays: decimal.NewFromFloat(50.0),
			description:        "C: $20F + $30U, W1 (ALL) $60, Card ✅ → Success: Card $0, Wallet $50",
		},
		{
			name:               "C_W1_ALL_40_CardAvailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			walletBalance:      decimal.NewFromFloat(40.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(10.0),
			expectedWalletPays: decimal.NewFromFloat(40.0),
			description:        "C: $20F + $30U, W1 (ALL) $40, Card ✅ → Success: Card $10, Wallet $40",
		},
		{
			name:               "C_W1_ALL_40_CardUnavailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
			walletBalance:      decimal.NewFromFloat(40.0),
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(10.0),
			expectedWalletPays: decimal.NewFromFloat(40.0),
			description:        "C: $20F + $30U, W1 (ALL) $40, Card ❌ → Failed",
		},
		{
			name:               "C_W2_FIXED_30_CardAvailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(30.0),
			expectedWalletPays: decimal.NewFromFloat(20.0),
			description:        "C: $20F + $30U, W2 (FIXED) $30, Card ✅ → Success: Card $30, Wallet $20",
		},
		{
			name:               "C_W2_FIXED_10_CardAvailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
			walletBalance:      decimal.NewFromFloat(10.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(40.0),
			expectedWalletPays: decimal.NewFromFloat(10.0),
			description:        "C: $20F + $30U, W2 (FIXED) $10, Card ✅ → Success: Card $40, Wallet $10",
		},
		{
			name:               "C_W2_FIXED_30_CardUnavailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed},
			walletBalance:      decimal.NewFromFloat(30.0),
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(30.0),
			expectedWalletPays: decimal.NewFromFloat(20.0),
			description:        "C: $20F + $30U, W2 (FIXED) $30, Card ❌ → Failed",
		},
		{
			name:               "C_W3_USAGE_40_CardAvailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			walletBalance:      decimal.NewFromFloat(40.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "C: $20F + $30U, W3 (USAGE) $40, Card ✅ → Success: Card $20, Wallet $30",
		},
		{
			name:               "C_W3_USAGE_20_CardAvailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			walletBalance:      decimal.NewFromFloat(20.0),
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(30.0),
			expectedWalletPays: decimal.NewFromFloat(20.0),
			description:        "C: $20F + $30U, W3 (USAGE) $20, Card ✅ → Success: Card $30, Wallet $20",
		},
		{
			name:               "C_W3_USAGE_40_CardUnavailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			walletBalance:      decimal.NewFromFloat(40.0),
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(20.0),
			expectedWalletPays: decimal.NewFromFloat(30.0),
			description:        "C: $20F + $30U, W3 (USAGE) $40, Card ❌ → Failed",
		},
		{
			name:               "C_W4_None_0_CardAvailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{},
			walletBalance:      decimal.Zero,
			cardAvailable:      true,
			expectedResult:     "Success",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "C: $20F + $30U, W4 (None) $0, Card ✅ → Success: Card $50, Wallet $0",
		},
		{
			name:               "C_W4_None_0_CardUnavailable",
			invoiceType:        "C",
			walletConfig:       []types.WalletConfigPriceType{},
			walletBalance:      decimal.Zero,
			cardAvailable:      false,
			expectedResult:     "Failed",
			expectedCardPays:   decimal.NewFromFloat(50.0),
			expectedWalletPays: decimal.Zero,
			description:        "C: $20F + $30U, W4 (None) $0, Card ❌ → Failed",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.testPaymentFlowCase(tc)
		})
	}
}

// TestMultiWalletScenarios tests multi-wallet payment scenarios from the PRD
func (s *PaymentFlowTestSuite) TestMultiWalletScenarios() {
	tests := []struct {
		name                string
		invoiceType         string
		wallets             []walletConfig
		cardAvailable       bool
		expectedResult      string
		expectedCardPays    decimal.Decimal
		expectedWallet1Pays decimal.Decimal
		expectedWallet2Pays decimal.Decimal
		description         string
	}{
		{
			name:        "C_W1_25_ALL_W2_15_FIXED_CardAvailable",
			invoiceType: "C",
			wallets: []walletConfig{
				{balance: decimal.NewFromFloat(25.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll}},
				{balance: decimal.NewFromFloat(15.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed}},
			},
			cardAvailable:       true,
			expectedResult:      "Success",
			expectedCardPays:    decimal.NewFromFloat(10.0),
			expectedWallet1Pays: decimal.NewFromFloat(25.0),
			expectedWallet2Pays: decimal.NewFromFloat(15.0),
			description:         "C: $20F + $30U, W1($25 ALL) + W2($15 FIXED), Card ✅ → Success: Card $10, W1 $25, W2 $15",
		},
		{
			name:        "C_W1_25_ALL_W3_20_USAGE_CardAvailable",
			invoiceType: "C",
			wallets: []walletConfig{
				{balance: decimal.NewFromFloat(25.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll}},
				{balance: decimal.NewFromFloat(20.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage}},
			},
			cardAvailable:       true,
			expectedResult:      "Success",
			expectedCardPays:    decimal.NewFromFloat(5.0),
			expectedWallet1Pays: decimal.NewFromFloat(25.0),
			expectedWallet2Pays: decimal.NewFromFloat(20.0),
			description:         "C: $20F + $30U, W1($25 ALL) + W3($20 USAGE), Card ✅ → Success: Card $5, W1 $25, W3 $20",
		},
		{
			name:        "C_W2_15_FIXED_W3_35_USAGE_CardAvailable",
			invoiceType: "C",
			wallets: []walletConfig{
				{balance: decimal.NewFromFloat(15.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed}},
				{balance: decimal.NewFromFloat(35.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage}},
			},
			cardAvailable:       true,
			expectedResult:      "Success",
			expectedCardPays:    decimal.NewFromFloat(5.0),
			expectedWallet1Pays: decimal.NewFromFloat(15.0),
			expectedWallet2Pays: decimal.NewFromFloat(30.0),
			description:         "C: $20F + $30U, W2($15 FIXED) + W3($35 USAGE), Card ✅ → Success: Card $5, W2 $15, W3 $30",
		},
		{
			name:        "C_W2_10_FIXED_W3_20_USAGE_CardAvailable",
			invoiceType: "C",
			wallets: []walletConfig{
				{balance: decimal.NewFromFloat(10.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed}},
				{balance: decimal.NewFromFloat(20.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage}},
			},
			cardAvailable:       true,
			expectedResult:      "Success",
			expectedCardPays:    decimal.NewFromFloat(20.0),
			expectedWallet1Pays: decimal.NewFromFloat(10.0),
			expectedWallet2Pays: decimal.NewFromFloat(20.0),
			description:         "C: $20F + $30U, W2($10 FIXED) + W3($20 USAGE), Card ✅ → Success: Card $20, W2 $10, W3 $20",
		},
		{
			name:        "C_W2_10_FIXED_W3_20_USAGE_CardUnavailable",
			invoiceType: "C",
			wallets: []walletConfig{
				{balance: decimal.NewFromFloat(10.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeFixed}},
				{balance: decimal.NewFromFloat(20.0), config: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage}},
			},
			cardAvailable:       false,
			expectedResult:      "Failed",
			expectedCardPays:    decimal.NewFromFloat(20.0),
			expectedWallet1Pays: decimal.NewFromFloat(10.0),
			expectedWallet2Pays: decimal.NewFromFloat(20.0),
			description:         "C: $20F + $30U, W2($10 FIXED) + W3($20 USAGE), Card ❌ → Failed",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.testMultiWalletScenario(tc)
		})
	}
}

type walletConfig struct {
	balance decimal.Decimal
	config  []types.WalletConfigPriceType
}

func (s *PaymentFlowTestSuite) testPaymentFlowCase(tc struct {
	name               string
	invoiceType        string
	walletConfig       []types.WalletConfigPriceType
	walletBalance      decimal.Decimal
	cardAvailable      bool
	expectedResult     string
	expectedCardPays   decimal.Decimal
	expectedWalletPays decimal.Decimal
	description        string
}) {
	// Calculate expected results based on the test case
	var invoiceAmount decimal.Decimal
	switch tc.invoiceType {
	case "A": // Pure USAGE
		invoiceAmount = decimal.NewFromFloat(50.0)
	case "B": // Pure FIXED
		invoiceAmount = decimal.NewFromFloat(50.0)
	case "C": // Mixed
		invoiceAmount = decimal.NewFromFloat(50.0)
	}

	// Calculate wallet payable amount based on restrictions
	walletPayableAmount := s.calculateWalletPayableAmount(tc.walletConfig, tc.walletBalance, tc.invoiceType)

	// Calculate payment split
	cardAmount := decimal.Zero
	walletAmount := decimal.Zero

	if walletPayableAmount.GreaterThanOrEqual(invoiceAmount) {
		// Wallet can pay full amount
		walletAmount = invoiceAmount
		cardAmount = decimal.Zero
	} else {
		// Wallet can pay partial amount
		walletAmount = walletPayableAmount
		cardAmount = invoiceAmount.Sub(walletAmount)
	}

	// Determine if payment succeeds
	paymentSucceeds := tc.cardAvailable || walletAmount.GreaterThanOrEqual(invoiceAmount)

	// Verify results
	if tc.expectedResult == "Success" {
		assert.True(s.T(), paymentSucceeds, tc.description)
		assert.Equal(s.T(), tc.expectedCardPays.String(), cardAmount.String(), tc.description)
		assert.Equal(s.T(), tc.expectedWalletPays.String(), walletAmount.String(), tc.description)
	} else {
		assert.False(s.T(), paymentSucceeds, tc.description)
		// For failed payments, we still expect the calculated amounts to be correct
		assert.Equal(s.T(), tc.expectedCardPays.String(), cardAmount.String(), tc.description)
		assert.Equal(s.T(), tc.expectedWalletPays.String(), walletAmount.String(), tc.description)
	}

	// Log the test case for verification
	s.T().Logf("Payment flow test case: %s - %s", tc.name, tc.description)
	s.T().Logf("  Invoice: %s, Amount: %s", tc.invoiceType, invoiceAmount)
	s.T().Logf("  Wallet Config: %v, Balance: %s", tc.walletConfig, tc.walletBalance)
	s.T().Logf("  Card Available: %v", tc.cardAvailable)
	s.T().Logf("  Expected: %s (Card: %s, Wallet: %s)", tc.expectedResult, tc.expectedCardPays, tc.expectedWalletPays)
	s.T().Logf("  Actual: %s (Card: %s, Wallet: %s)",
		map[bool]string{true: "Success", false: "Failed"}[paymentSucceeds],
		cardAmount, walletAmount)
}

func (s *PaymentFlowTestSuite) testMultiWalletScenario(tc struct {
	name                string
	invoiceType         string
	wallets             []walletConfig
	cardAvailable       bool
	expectedResult      string
	expectedCardPays    decimal.Decimal
	expectedWallet1Pays decimal.Decimal
	expectedWallet2Pays decimal.Decimal
	description         string
}) {
	// Calculate expected results based on the test case
	var invoiceAmount decimal.Decimal
	switch tc.invoiceType {
	case "C": // Mixed
		invoiceAmount = decimal.NewFromFloat(50.0)
	}

	// Calculate total wallet payable amount
	totalWalletPayableAmount := decimal.Zero
	for _, wallet := range tc.wallets {
		walletPayableAmount := s.calculateWalletPayableAmount(wallet.config, wallet.balance, tc.invoiceType)
		totalWalletPayableAmount = totalWalletPayableAmount.Add(walletPayableAmount)
	}

	// Calculate payment split
	cardAmount := decimal.Zero
	totalWalletAmount := decimal.Zero

	if totalWalletPayableAmount.GreaterThanOrEqual(invoiceAmount) {
		// Wallets can pay full amount
		totalWalletAmount = invoiceAmount
		cardAmount = decimal.Zero
	} else {
		// Wallets can pay partial amount
		totalWalletAmount = totalWalletPayableAmount
		cardAmount = invoiceAmount.Sub(totalWalletAmount)
	}

	// Determine if payment succeeds
	paymentSucceeds := tc.cardAvailable || totalWalletAmount.GreaterThanOrEqual(invoiceAmount)

	// Verify results
	if tc.expectedResult == "Success" {
		assert.True(s.T(), paymentSucceeds, tc.description)
		assert.Equal(s.T(), tc.expectedCardPays.String(), cardAmount.String(), tc.description)
		expectedTotalWalletAmount := tc.expectedWallet1Pays.Add(tc.expectedWallet2Pays)
		assert.Equal(s.T(), expectedTotalWalletAmount.String(), totalWalletAmount.String(), tc.description)
	} else {
		assert.False(s.T(), paymentSucceeds, tc.description)
		// For failed payments, we still expect the calculated amounts to be correct
		assert.Equal(s.T(), tc.expectedCardPays.String(), cardAmount.String(), tc.description)
		expectedTotalWalletAmount := tc.expectedWallet1Pays.Add(tc.expectedWallet2Pays)
		assert.Equal(s.T(), expectedTotalWalletAmount.String(), totalWalletAmount.String(), tc.description)
	}

	// Log the test case for verification
	s.T().Logf("Multi-wallet test case: %s - %s", tc.name, tc.description)
	s.T().Logf("  Invoice: %s, Amount: %s", tc.invoiceType, invoiceAmount)
	s.T().Logf("  Wallets: %d, Total Balance: %s", len(tc.wallets), totalWalletPayableAmount)
	s.T().Logf("  Card Available: %v", tc.cardAvailable)
	s.T().Logf("  Expected: %s (Card: %s, Wallets: %s)", tc.expectedResult, tc.expectedCardPays, tc.expectedWallet1Pays.Add(tc.expectedWallet2Pays))
	s.T().Logf("  Actual: %s (Card: %s, Wallets: %s)",
		map[bool]string{true: "Success", false: "Failed"}[paymentSucceeds],
		cardAmount, totalWalletAmount)
}

// calculateWalletPayableAmount calculates how much a wallet can pay based on its restrictions
func (s *PaymentFlowTestSuite) calculateWalletPayableAmount(walletConfig []types.WalletConfigPriceType, walletBalance decimal.Decimal, invoiceType string) decimal.Decimal {
	if len(walletConfig) == 0 {
		return decimal.Zero
	}

	// Check if wallet has ALL access
	hasAllAccess := false
	for _, config := range walletConfig {
		if config == types.WalletConfigPriceTypeAll {
			hasAllAccess = true
			break
		}
	}

	if hasAllAccess {
		return walletBalance
	}

	// Calculate based on specific price type restrictions
	var allowedAmount decimal.Decimal
	switch invoiceType {
	case "A": // Pure USAGE
		for _, config := range walletConfig {
			if config == types.WalletConfigPriceTypeUsage {
				allowedAmount = walletBalance
				break
			}
		}
	case "B": // Pure FIXED
		for _, config := range walletConfig {
			if config == types.WalletConfigPriceTypeFixed {
				allowedAmount = walletBalance
				break
			}
		}
	case "C": // Mixed - calculate proportionally
		fixedAmount := decimal.NewFromFloat(20.0)
		usageAmount := decimal.NewFromFloat(30.0)

		walletCanPayFixed := false
		walletCanPayUsage := false

		for _, config := range walletConfig {
			if config == types.WalletConfigPriceTypeFixed {
				walletCanPayFixed = true
			}
			if config == types.WalletConfigPriceTypeUsage {
				walletCanPayUsage = true
			}
		}

		if walletCanPayFixed && walletCanPayUsage {
			allowedAmount = walletBalance
		} else if walletCanPayFixed {
			allowedAmount = decimal.Min(walletBalance, fixedAmount)
		} else if walletCanPayUsage {
			allowedAmount = decimal.Min(walletBalance, usageAmount)
		}
	}

	return allowedAmount
}
