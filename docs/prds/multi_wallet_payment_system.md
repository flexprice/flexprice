# Multi-Wallet Payment System PRD

## Payment Cases Matrix

### Invoice Types
- **A**: Pure USAGE charges ($0 FIXED + $X USAGE)
- **B**: Pure FIXED charges ($X FIXED + $0 USAGE)  
- **C**: Mixed charges ($X FIXED + $Y USAGE)

### Wallet Configurations
- **W1**: `["ALL"]` - Universal wallet
- **W2**: `["FIXED"]` - Fixed-only wallet
- **W3**: `["USAGE"]` - Usage-only wallet
- **W4**: No wallets

### Payment Outcomes Table

| Invoice           | Wallet Type      | Wallet Balance | Card Available | Result      | Card Pays | Wallet Pays |
|-------------------|------------------|----------------|----------------|-------------|-----------|-------------|
| **A: $50 USAGE** | W1 (ALL)         | $60            | ✅              | ✅ Success  | $0        | $50         |
| **A: $50 USAGE** | W1 (ALL)         | $30            | ✅              | ✅ Success  | $20        | $30         |
| **A: $50 USAGE** | W1 (ALL)         | $30            | ❌              | ❌ Failed  | -          | -           |
| **A: $50 USAGE** | W2 (FIXED)       | $60            | ✅              | ✅ Success  | $50        | $0          |
| **A: $50 USAGE** | W2 (FIXED)       | $60            | ❌              | ❌ Failed  | -          | -           |
| **A: $50 USAGE** | W3 (USAGE)       | $60            | ✅              | ✅ Success  | $0         | $50         |
| **A: $50 USAGE** | W3 (USAGE)       | $30            | ✅              | ✅ Success  | $20        | $30         |
| **A: $50 USAGE** | W3 (USAGE)       | $30            | ❌              | ❌ Failed  | -          | -           |
| **A: $50 USAGE** | W4 (None)        | $0             | ✅              | ✅ Success  | $50        | $0          |
| **A: $50 USAGE** | W4 (None)        | $0             | ❌              | ❌ Failed  | -          | -           |
| **B: $50 FIXED** | W1 (ALL)         | $60            | ✅              | ✅ Success  | $0         | $50         |
| **B: $50 FIXED** | W1 (ALL)         | $30            | ✅              | ✅ Success  | $20        | $30         |
| **B: $50 FIXED** | W1 (ALL)         | $30            | ❌              | ❌ Failed  | -          | -           |
| **B: $50 FIXED** | W2 (FIXED)       | $60            | ✅              | ✅ Success  | $0         | $50         |
| **B: $50 FIXED** | W2 (FIXED)       | $30            | ✅              | ✅ Success  | $20        | $30         |
| **B: $50 FIXED** | W2 (FIXED)       | $30            | ❌              | ❌ Failed  | -          | -           |
| **B: $50 FIXED** | W3 (USAGE)       | $60            | ✅              | ✅ Success  | $50        | $0          |
| **B: $50 FIXED** | W3 (USAGE)       | $60            | ❌              | ❌ Failed  | -          | -           |
| **B: $50 FIXED** | W4 (None)        | $0             | ✅              | ✅ Success  | $50        | $0          |
| **B: $50 FIXED** | W4 (None)        | $0             | ❌              | ❌ Failed  | -          | -           |
| **C: $20F + $30U** | W1 (ALL)        | $60            | ✅              | ✅ Success  | $0         | $50         |
| **C: $20F + $30U** | W1 (ALL)        | $40            | ✅              | ✅ Success  | $10        | $40         |
| **C: $20F + $30U** | W1 (ALL)        | $40            | ❌              | ❌ Failed  | -          | -           |
| **C: $20F + $30U** | W2 (FIXED)      | $30            | ✅              | ✅ Success  | $30        | $20         |
| **C: $20F + $30U** | W2 (FIXED)      | $10            | ✅              | ✅ Success  | $40        | $10         |
| **C: $20F + $30U** | W2 (FIXED)      | $30            | ❌              | ❌ Failed  | -          | -           |
| **C: $20F + $30U** | W3 (USAGE)      | $40            | ✅              | ✅ Success  | $20        | $30         |
| **C: $20F + $30U** | W3 (USAGE)      | $20            | ✅              | ✅ Success  | $30        | $20         |
| **C: $20F + $30U** | W3 (USAGE)      | $40            | ❌              | ❌ Failed  | -          | -           |
| **C: $20F + $30U** | W4 (None)       | $0             | ✅              | ✅ Success  | $50        | $0          |
| **C: $20F + $30U** | W4 (None)       | $0             | ❌              | ❌ Failed  | -          | -           |

### Multi-Wallet Scenarios

| Invoice           | Wallets                    | Total Balance | Card | Result      | Card Pays | Wallet 1 Pays        | Wallet 2 Pays        |
|-------------------|----------------------------|---------------|------|-------------|-----------|----------------------|----------------------|
| **C: $20F + $30U** | W1($25) + W2($15)         | $40           | ✅    | ✅ Success  | $10       | $25 (ALL)            | $15 (FIXED)          |
| **C: $20F + $30U** | W1($25) + W3($20)         | $45           | ✅    | ✅ Success  | $5        | $25 (ALL)            | $20 (USAGE)          |
| **C: $20F + $30U** | W2($15) + W3($35)         | $50           | ✅    | ✅ Success  | $0        | $15 (FIXED)          | $30 (USAGE)          |
| **C: $20F + $30U** | W2($10) + W3($20)         | $30           | ✅    | ✅ Success  | $20       | $10 (FIXED)          | $20 (USAGE)          |
| **C: $20F + $30U** | W2($10) + W3($20)         | $30           | ❌    | ❌ Failed  | -         | -                    | -                    |

## Code Implementation

### Core Functions

```go
// 1. Calculate price type breakdown from invoice
func (s *subscriptionPaymentProcessor) calculatePriceTypeAmounts(lineItems []*invoice.LineItem) map[string]decimal.Decimal {
    priceTypeAmounts := map[string]decimal.Decimal{
        "FIXED": decimal.Zero,
        "USAGE": decimal.Zero,
    }
    
    for _, item := range lineItems {
        if amount, err := decimal.NewFromString(item.Amount); err == nil {
            priceTypeAmounts[string(item.PriceType)] = priceTypeAmounts[string(item.PriceType)].Add(amount)
        }
    }
    
    return priceTypeAmounts
}

// 2. Calculate what wallets can pay based on restrictions
func (s *subscriptionPaymentProcessor) calculateWalletPayableAmount(ctx context.Context, customerID string, priceTypeAmounts map[string]decimal.Decimal, availableCredits decimal.Decimal) decimal.Decimal {
    wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
    if err != nil {
        return decimal.Zero
    }
    
    totalPayableAmount := decimal.Zero
    
    for _, w := range wallets {
        if w.WalletStatus != types.WalletStatusActive {
            continue
        }
        
        allowedAmount := s.calculateWalletAllowedAmount(w, priceTypeAmounts)
        walletPayableAmount := decimal.Min(allowedAmount, w.Balance)
        totalPayableAmount = totalPayableAmount.Add(walletPayableAmount)
    }
    
    return decimal.Min(totalPayableAmount, availableCredits)
}

// 3. Calculate what a single wallet can pay based on restrictions
func (s *subscriptionPaymentProcessor) calculateWalletAllowedAmount(w *wallet.Wallet, priceTypeAmounts map[string]decimal.Decimal) decimal.Decimal {
    allowedAmount := decimal.Zero
    
    if w.Config.AllowedPriceTypes == nil || len(w.Config.AllowedPriceTypes) == 0 {
        // Default to ALL if not specified
        for _, amount := range priceTypeAmounts {
            allowedAmount = allowedAmount.Add(amount)
        }
    } else {
        for _, allowedType := range w.Config.AllowedPriceTypes {
            allowedTypeStr := string(allowedType)
            if allowedTypeStr == "ALL" {
                for _, amount := range priceTypeAmounts {
                    allowedAmount = allowedAmount.Add(amount)
                }
                break
            } else if amount, exists := priceTypeAmounts[allowedTypeStr]; exists {
                allowedAmount = allowedAmount.Add(amount)
            }
        }
    }
    
    return allowedAmount
}

// 4. Main payment processing logic
func (s *subscriptionPaymentProcessor) processPayment(ctx context.Context, sub *subscription.Subscription, inv *dto.InvoiceResponse) *PaymentResult {
    // Get full invoice with line items
    fullInvoice, err := s.InvoiceRepo.Get(ctx, inv.ID)
    if err != nil {
        return &PaymentResult{Success: false}
    }
    
    // Calculate price type breakdown
    priceTypeAmounts := s.calculatePriceTypeAmounts(fullInvoice.LineItems)
    
    // Check available credits and calculate wallet payable amount
    availableCredits := s.checkAvailableCredits(ctx, sub, inv)
    walletPayableAmount := s.calculateWalletPayableAmount(ctx, sub.CustomerID, priceTypeAmounts, availableCredits)
    
    // Determine payment split
    remainingAmount, _ := decimal.NewFromString(inv.AmountRemaining)
    cardAmount := remainingAmount.Sub(walletPayableAmount)
    walletAmount := walletPayableAmount
    
    // Execute payments: Card first, then wallets
    if cardAmount.GreaterThan(decimal.Zero) {
        if !s.processCardPayment(ctx, sub, inv, cardAmount) {
            return &PaymentResult{Success: false}
        }
    }
    
    if walletAmount.GreaterThan(decimal.Zero) {
        if !s.processCreditsPayment(ctx, sub, inv) {
            return &PaymentResult{Success: false}
        }
    }
    
    return &PaymentResult{Success: true}
}
```

### Key Features
- **Price Type Analysis**: `calculatePriceTypeAmounts()` categorizes invoice charges
- **Wallet Restriction Logic**: `calculateWalletAllowedAmount()` respects wallet configurations  
- **Payment Orchestration**: `processPayment()` coordinates card and wallet payments
- **Automatic Fallback**: Card covers what wallets cannot pay
