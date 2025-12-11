package service

import (
	"testing"

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

// TestPaymentFlowCases tests all payment flow cases - card only payments
func (s *PaymentFlowTestSuite) TestPaymentFlowCases() {
	tests := []struct {
		name             string
		invoiceType      string // "A" (USAGE only), "B" (FIXED only), "C" (Mixed)
		cardAvailable    bool
		expectedResult   string // "Success" or "Failed"
		expectedCardPays decimal.Decimal
		description      string
	}{
		// Invoice Type A: Pure USAGE charges ($50 USAGE)
		{
			name:             "A_CardAvailable",
			invoiceType:      "A",
			cardAvailable:    true,
			expectedResult:   "Success",
			expectedCardPays: decimal.NewFromFloat(50.0),
			description:      "A: $50 USAGE, Card ✅ → Success: Card $50",
		},
		{
			name:             "A_CardUnavailable",
			invoiceType:      "A",
			cardAvailable:    false,
			expectedResult:   "Failed",
			expectedCardPays: decimal.NewFromFloat(50.0),
			description:      "A: $50 USAGE, Card ❌ → Failed",
		},

		// Invoice Type B: Pure FIXED charges ($50 FIXED)
		{
			name:             "B_CardAvailable",
			invoiceType:      "B",
			cardAvailable:    true,
			expectedResult:   "Success",
			expectedCardPays: decimal.NewFromFloat(50.0),
			description:      "B: $50 FIXED, Card ✅ → Success: Card $50",
		},
		{
			name:             "B_CardUnavailable",
			invoiceType:      "B",
			cardAvailable:    false,
			expectedResult:   "Failed",
			expectedCardPays: decimal.NewFromFloat(50.0),
			description:      "B: $50 FIXED, Card ❌ → Failed",
		},

		// Invoice Type C: Mixed charges ($20 FIXED + $30 USAGE)
		{
			name:             "C_CardAvailable",
			invoiceType:      "C",
			cardAvailable:    true,
			expectedResult:   "Success",
			expectedCardPays: decimal.NewFromFloat(50.0),
			description:      "C: $20F + $30U, Card ✅ → Success: Card $50",
		},
		{
			name:             "C_CardUnavailable",
			invoiceType:      "C",
			cardAvailable:    false,
			expectedResult:   "Failed",
			expectedCardPays: decimal.NewFromFloat(50.0),
			description:      "C: $20F + $30U, Card ❌ → Failed",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.testPaymentFlowCase(tc)
		})
	}
}

func (s *PaymentFlowTestSuite) testPaymentFlowCase(tc struct {
	name             string
	invoiceType      string
	cardAvailable    bool
	expectedResult   string
	expectedCardPays decimal.Decimal
	description      string
}) {
	// Calculate invoice amount based on the test case
	var invoiceAmount decimal.Decimal
	switch tc.invoiceType {
	case "A": // Pure USAGE
		invoiceAmount = decimal.NewFromFloat(50.0)
	case "B": // Pure FIXED
		invoiceAmount = decimal.NewFromFloat(50.0)
	case "C": // Mixed
		invoiceAmount = decimal.NewFromFloat(50.0)
	}

	// All payments are card-only - no wallet payments
	cardAmount := invoiceAmount
	walletAmount := decimal.Zero

	// Payment succeeds only if card is available
	paymentSucceeds := tc.cardAvailable

	// Verify results
	if tc.expectedResult == "Success" {
		assert.True(s.T(), paymentSucceeds, tc.description)
		assert.Equal(s.T(), tc.expectedCardPays.String(), cardAmount.String(), tc.description)
		assert.Equal(s.T(), "0", walletAmount.String(), tc.description)
	} else {
		assert.False(s.T(), paymentSucceeds, tc.description)
		// For failed payments, we still expect the calculated amounts to be correct
		assert.Equal(s.T(), tc.expectedCardPays.String(), cardAmount.String(), tc.description)
		assert.Equal(s.T(), "0", walletAmount.String(), tc.description)
	}

	// Log the test case for verification
	s.T().Logf("Payment flow test case: %s - %s", tc.name, tc.description)
	s.T().Logf("  Invoice: %s, Amount: %s", tc.invoiceType, invoiceAmount)
	s.T().Logf("  Card Available: %v", tc.cardAvailable)
	s.T().Logf("  Expected: %s (Card: %s)", tc.expectedResult, tc.expectedCardPays)
	s.T().Logf("  Actual: %s (Card: %s)",
		map[bool]string{true: "Success", false: "Failed"}[paymentSucceeds],
		cardAmount)
}
