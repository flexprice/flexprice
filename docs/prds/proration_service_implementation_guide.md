# Proration Service Implementation Guide

This guide details the implementation steps for the Proration Service within FlexPrice, following the principles outlined in the main `new_service_creation_guide.md` and the specific requirements from the `subscription_proration_and_workflow_implementaion.md` PRD.

The Proration Service is responsible for calculating proration amounts (credits and charges) when subscription line items change mid-cycle. It does not have its own database tables or direct API handlers but acts as a core component used by the Subscription service.

## 1. Create Proration Domain Models

Define the core data structures and types used for proration calculations.

**File: `internal/domain/proration/models.go`**

```go
package proration

import (
	"time"

	"github.com/shopspring/decimal"
	// Assuming subscription types are defined elsewhere, e.g., internal/domain/subscription
	// subModels "github.com/yourusername/flexprice/internal/domain/subscription"
)

// ProrationAction defines the type of change triggering proration.
type ProrationAction string

const (
	ProrationActionUpgrade        ProrationAction = "upgrade"
	ProrationActionDowngrade      ProrationAction = "downgrade"
	ProrationActionQuantityChange ProrationAction = "quantity_change"
	ProrationActionCancellation   ProrationAction = "cancellation"
	ProrationActionAddItem        ProrationAction = "add_item"
	ProrationActionRemoveItem     ProrationAction = "remove_item"
)

// ProrationStrategy defines how the proration coefficient is calculated.
type ProrationStrategy string

const (
	StrategyDayBased    ProrationStrategy = "day_based"    // Default
	StrategySecondBased ProrationStrategy = "second_based" // Future enhancement
)

// ProrationBehavior defines how proration is applied (e.g., create invoice items).
type ProrationBehavior string

const (
	ProrationBehaviorCreateInvoiceItems ProrationBehavior = "create_invoice_items" // Default: Create credits/charges on invoice
	ProrationBehaviorNone               ProrationBehavior = "none"               // Calculate but don't apply (e.g., for previews)
	// Potentially add others like "apply_to_balance" in the future
)

// BillingMode represents when a subscription is billed.
type BillingMode string

const (
	BillingModeInAdvance BillingMode = "in_advance"
	BillingModeInArrears BillingMode = "in_arrears"
)

// ScheduleType determines when subscription changes take effect.
type ScheduleType string

const (
	ScheduleTypeImmediate    ScheduleType = "immediate"
	ScheduleTypePeriodEnd    ScheduleType = "period_end"
	ScheduleTypeSpecificDate ScheduleType = "specific_date"
)

// TerminationReason represents why a subscription is being terminated.
type TerminationReason string

const (
    TerminationReasonUpgrade      TerminationReason = "upgrade"
    TerminationReasonDowngrade    TerminationReason = "downgrade"
    TerminationReasonCancellation TerminationReason = "cancellation"
    TerminationReasonExpiration   TerminationReason = "expiration"
)

// ProrationParams holds all necessary input for calculating proration.
type ProrationParams struct {
	// Subscription & Line Item Context
	SubscriptionID       string          // ID of the subscription
	LineItemID           string          // ID of the line item being changed (empty for add_item)
	PlanPayInAdvance     bool            // From the subscription's plan
	CurrentPeriodStart   time.Time       // Start of the current billing period
	CurrentPeriodEnd     time.Time       // End of the current billing period
	CustomerTimezone     string          // Customer's timezone (e.g., "America/New_York")

	// Change Details
	Action               ProrationAction // Type of change
	OldPriceID           string          // Old price ID (empty for add_item)
	NewPriceID           string          // New price ID (empty for cancellation/remove_item)
	OldQuantity          decimal.Decimal // Old quantity (zero for add_item)
	NewQuantity          decimal.Decimal // New quantity (zero for remove_item/cancellation)
	OldPricePerUnit      decimal.Decimal // Price per unit for the old item
	NewPricePerUnit      decimal.Decimal // Price per unit for the new item
	Currency             string          // Currency code (e.g., "USD")
	ProrationDate        time.Time       // Effective date/time of the change

	// Configuration & Context
	ProrationBehavior    ProrationBehavior // How to apply the result
	ProrationStrategy    ProrationStrategy // Calculation method (default: day_based)
	TerminationReason    TerminationReason // Required for cancellations/downgrades for credit logic
	ScheduleType         ScheduleType      // When the change should take effect
	ScheduleDate         time.Time         // Specific date for scheduled changes (if applicable)
	HasScheduleDate      bool              // Whether ScheduleDate is set

	// Handling Multiple Changes / Credits
	OriginalAmountPaid   decimal.Decimal // Amount originally paid for the item(s) being changed in this period
	PreviousCreditsIssued decimal.Decimal // Sum of credits already issued against OriginalAmountPaid in this period
}

// ProrationLineItem represents a single credit or charge line item.
type ProrationLineItem struct {
	Description string          `json:"description"`
	Amount      decimal.Decimal `json:"amount"` // Positive for charge, negative for credit
	StartDate   time.Time       `json:"start_date"` // Period this line item covers
	EndDate     time.Time       `json:"end_date"`   // Period this line item covers
	Quantity    decimal.Decimal `json:"quantity"`
	PriceID     string          `json:"price_id"`   // Associated price ID if applicable
	IsCredit    bool            `json:"is_credit"`
}

// ProrationResult holds the output of a proration calculation.
type ProrationResult struct {
	CreditItems      []ProrationLineItem // Items representing credits back to the customer
	ChargeItems      []ProrationLineItem // Items representing new charges to the customer
	NetAmount        decimal.Decimal     // Net amount (Sum of charges - sum of credits)
	Currency         string              // Currency code
	Action           ProrationAction     // The action that generated this result
	ProrationDate    time.Time           // Effective date used for calculation
	LineItemID       string              // ID of the affected line item (empty for new items)
	IsPreview        bool                // Indicates if this was calculated for a preview
}
```

## 2. Create Proration Calculator Logic

Implement the core calculation logic, handling timezones, strategies, and credit capping.

**File: `internal/domain/proration/calculator.go`**

```go
package proration

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	// Import timezone library if needed, e.g., "time/tzdata" or handle loading explicitly
)

// Calculator performs proration calculations.
// It's kept separate from the service to allow different calculation strategies or easier testing.
type Calculator interface {
	Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error)
}

// NewCalculator creates a default proration calculator.
func NewCalculator() Calculator {
	// Currently only supports day-based, but could select based on config in the future
	return &dayBasedCalculator{}
}

// dayBasedCalculator implements the default day-based proration logic.
type dayBasedCalculator struct{}

func (c *dayBasedCalculator) Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error) {
	if err := validateParams(params); err != nil {
		return nil, fmt.Errorf("invalid proration params: %w", err)
	}

	// Load customer timezone
	loc, err := time.LoadLocation(params.CustomerTimezone)
	if err != nil {
		// Consider falling back to UTC or returning a specific error
		return nil, fmt.Errorf("failed to load customer timezone '%s': %w", params.CustomerTimezone, err)
	}

	// Adjust times to customer timezone for accurate day boundaries
	prorationDateInTZ := params.ProrationDate.In(loc)
	periodStartInTZ := params.CurrentPeriodStart.In(loc)
	periodEndInTZ := params.CurrentPeriodEnd.In(loc)

	// Calculate total days in the period (inclusive start, exclusive end)
	totalDuration := periodEndInTZ.Sub(periodStartInTZ)
	totalDays := daysInDuration(totalDuration, loc)
	if totalDays <= 0 {
		// Avoid division by zero, maybe return zero result or error
		return nil, fmt.Errorf("invalid billing period: total days is zero or negative (%v to %v)", periodStartInTZ, periodEndInTZ)
	}

	// Calculate remaining days (inclusive proration date, exclusive end date)
	remainingDuration := periodEndInTZ.Sub(prorationDateInTZ)
	remainingDays := daysInDuration(remainingDuration, loc)
	if remainingDays < 0 {
		remainingDays = 0 // Change happened after period end?
	}

	// Calculate used days (inclusive start date, exclusive proration date)
	// usedDuration := prorationDateInTZ.Sub(periodStartInTZ)
	// usedDays := daysInDuration(usedDuration, loc)
	// if usedDays < 0 {
	// 	usedDays = 0 // Change happened before period start?
	// }

	// Calculate proration coefficient
	// Use decimal for precision
	decimalTotalDays := decimal.NewFromInt(int64(totalDays))
	decimalRemainingDays := decimal.NewFromInt(int64(remainingDays))
	// decimalUsedDays := decimal.NewFromInt(int64(usedDays))

	prorationCoefficient := decimal.Zero
	if decimalTotalDays.GreaterThan(decimal.Zero) {
		prorationCoefficient = decimalRemainingDays.Div(decimalTotalDays)
	}

	result := &ProrationResult{
		NetAmount:     decimal.Zero,
		Currency:      params.Currency,
		Action:        params.Action,
		ProrationDate: params.ProrationDate,
		LineItemID:    params.LineItemID,
		IsPreview:     params.ProrationBehavior == ProrationBehaviorNone,
		CreditItems:   []ProrationLineItem{},
		ChargeItems:   []ProrationLineItem{},
	}

	billingMode := BillingModeInArrears
	if params.PlanPayInAdvance {
		billingMode = BillingModeInAdvance
	}

	// --- Calculate Credit for Old Item (if applicable) ---
	if params.Action != ProrationActionAddItem {
		oldItemTotal := params.OldPricePerUnit.Mul(params.OldQuantity)
		potentialCredit := oldItemTotal.Mul(prorationCoefficient) // Credit for unused time

		// Only issue credits if billed in advance
		if billingMode == BillingModeInAdvance {
			// Apply credit capping based on original amount paid and previous credits
			cappedCredit := c.capCreditAmount(potentialCredit, params.OriginalAmountPaid, params.PreviousCreditsIssued)

			if cappedCredit.GreaterThan(decimal.Zero) {
				creditItem := ProrationLineItem{
					Description: fmt.Sprintf("Unused time on %s (ID: %s)", "item", params.LineItemID), // Improve description
					Amount:      cappedCredit.Neg(), // Negative for credit
					StartDate:   params.ProrationDate,
					EndDate:     params.CurrentPeriodEnd,
					Quantity:    params.OldQuantity,
					PriceID:     params.OldPriceID,
					IsCredit:    true,
				}
				result.CreditItems = append(result.CreditItems, creditItem)
				result.NetAmount = result.NetAmount.Add(creditItem.Amount)
			}
		}
		// Note: For IN_ARREARS, the credit is effectively handled by charging less at the end of the period.
		// We might still need to calculate the "reduction" if needed elsewhere.
	}

	// --- Calculate Charge for New Item (if applicable) ---
	if params.Action != ProrationActionRemoveItem && params.Action != ProrationActionCancellation {
		newItemTotal := params.NewPricePerUnit.Mul(params.NewQuantity)
		proratedCharge := newItemTotal.Mul(prorationCoefficient) // Charge for remaining time

		if proratedCharge.GreaterThan(decimal.Zero) {
			chargeItem := ProrationLineItem{
				Description: fmt.Sprintf("Charge for %s (ID: %s)", "item", params.NewPriceID), // Improve description
				Amount:      proratedCharge, // Positive for charge
				StartDate:   params.ProrationDate,
				EndDate:     params.CurrentPeriodEnd,
				Quantity:    params.NewQuantity,
				PriceID:     params.NewPriceID,
				IsCredit:    false,
			}
			result.ChargeItems = append(result.ChargeItems, chargeItem)
			result.NetAmount = result.NetAmount.Add(chargeItem.Amount)
		}
	}

	// TODO: Refine descriptions based on action (Upgrade, Downgrade, etc.)
	// TODO: Handle second-based strategy if/when implemented.

	return result, nil
}

// daysInDuration calculates the number of calendar days between two points in time,
// considering the given timezone for day boundaries.
// Note: This is a simplified example. Real-world implementation needs care around DST
// and precise definition of "day". Consider using a dedicated date/time library.
func daysInDuration(duration time.Duration, loc *time.Location) int {
	// A common approach: Calculate difference in Unix days or iterate day by day.
	// This simplified version uses 24 hours, which isn't always accurate due to DST.
	// A robust implementation is needed here.
	if duration <= 0 {
		return 0
	}
	// Example placeholder calculation (replace with robust logic)
	days := int(duration.Hours() / 24)
	if days == 0 && duration > 0 {
		return 1 // At least one day if there's any positive duration
	}
	return days
}

// capCreditAmount ensures credits do not exceed the original amount paid,
// considering any previous credits already issued for the same original payment.
func (c *dayBasedCalculator) capCreditAmount(
	potentialCredit decimal.Decimal,
	originalAmountPaid decimal.Decimal,
	previousCreditsIssued decimal.Decimal,
) decimal.Decimal {
	// Ensure non-negative potential credit
	if potentialCredit.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	// Cap at original amount paid
	if potentialCredit.GreaterThan(originalAmountPaid) {
		potentialCredit = originalAmountPaid
	}

	// Reduce by previous credits already issued against this original amount
	availableCredit := potentialCredit.Sub(previousCreditsIssued)

	// Ensure non-negative final credit
	if availableCredit.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	return availableCredit
}


// validateParams checks if essential parameters are provided.
func validateParams(params ProrationParams) error {
	if params.Currency == "" {
		return fmt.Errorf("currency is required")
	}
	if params.ProrationDate.IsZero() {
		return fmt.Errorf("proration date is required")
	}
	if params.CurrentPeriodStart.IsZero() || params.CurrentPeriodEnd.IsZero() {
		return fmt.Errorf("billing period start and end dates are required")
	}
	if params.CurrentPeriodEnd.Before(params.CurrentPeriodStart) {
		return fmt.Errorf("billing period end date cannot be before start date")
	}
	if params.CustomerTimezone == "" {
		// Default to UTC? Or require it?
		return fmt.Errorf("customer timezone is required")
	}
	// Add more checks as needed (e.g., quantities, prices based on action)
	return nil
}

```

## 3. Create Proration Service Interface

Define the contract for the Proration Service.

**File: `internal/domain/proration/service.go`**

```go
package proration

import (
	"context"

	// Import other necessary domain types, e.g., subscription, invoice
	// subModels "github.com/yourusername/flexprice/internal/domain/subscription"
	// invModels "github.com/yourusername/flexprice/internal/domain/invoice"
)

// ProrationService defines the operations for handling proration.
type ProrationService interface {
	// CalculateProration calculates the proration credits and charges for a given change.
	// It does not persist anything or modify the subscription/invoice directly.
	CalculateProration(ctx context.Context, params ProrationParams) (*ProrationResult, error)

	// ApplyProration takes a ProrationResult and applies it based on the ProrationBehavior.
	// For ProrationBehaviorCreateInvoiceItems, this typically means creating invoice line items
	// (or potentially credit notes) via the Invoice service/repository.
	// Requires context about the subscription and potentially the relevant invoice.
	ApplyProration(ctx context.Context,
		// sub *subModels.Subscription, // Subscription context
		// invoice *invModels.Invoice, // Optional: Target invoice if known
		result *ProrationResult,
		behavior ProrationBehavior,
		tenantID string,
		environmentID string,
		subscriptionID string,
		// Add other necessary context like user ID
	) error
}
```

## 4. Create Proration Service Implementation

Implement the `ProrationService` interface.

**File: `internal/service/proration/service.go`**

```go
package proration

import (
	"context"
	"fmt"

	"github.com/yourusername/flexprice/internal/domain/proration"
	// Import necessary repositories or services (e.g., Invoice)
	// invoiceDomain "github.com/yourusername/flexprice/internal/domain/invoice"
	// You might need an InvoiceRepository or InvoiceService interface here
)

type service struct {
	calculator proration.Calculator
	// Inject dependencies needed for ApplyProration, e.g.:
	// invoiceRepo invoiceDomain.Repository
}

// NewService creates a new proration service.
func NewService(
	calculator proration.Calculator,
	// invoiceRepo invoiceDomain.Repository,
) proration.ProrationService {
	return &service{
		calculator: calculator,
		// invoiceRepo: invoiceRepo,
	}
}

// CalculateProration delegates to the underlying calculator.
func (s *service) CalculateProration(ctx context.Context, params proration.ProrationParams) (*proration.ProrationResult, error) {
	// Potentially add logging or instrumentation here
	result, err := s.calculator.Calculate(ctx, params)
	if err != nil {
		// Wrap error for context
		return nil, fmt.Errorf("proration calculation failed: %w", err)
	}
	return result, nil
}

// ApplyProration implements the logic to persist proration effects.
func (s *service) ApplyProration(ctx context.Context,
	result *proration.ProrationResult,
	behavior proration.ProrationBehavior,
	tenantID string,
	environmentID string,
	subscriptionID string,
) error {

	if behavior == proration.ProrationBehaviorNone || result == nil {
		// Nothing to apply for previews or empty results
		return nil
	}

	if behavior == proration.ProrationBehaviorCreateInvoiceItems {
		// --- Logic to create invoice line items ---
		// 1. Determine the target invoice (might be the latest open invoice for the subscription,
		//    or potentially requires creating a new one depending on timing/rules).
		//    This likely involves querying via invoiceRepo.
		// 2. For each item in result.CreditItems and result.ChargeItems:
		//    a. Create an invoiceDomain.InvoiceLineItem object.
		//    b. Map fields from ProrationLineItem (Description, Amount, Quantity, Dates).
		//    c. Set appropriate type (e.g., "proration_credit", "proration_charge").
		//    d. Link to the subscription and original line item if applicable.
		//    e. Call invoiceRepo.AddLineItems(ctx, targetInvoiceID, lineItems...).
		//
		// Example (Conceptual - requires Invoice domain/repo):
		/*
		targetInvoiceID, err := s.findOrCreateTargetInvoice(ctx, tenantID, environmentID, subscriptionID, result.ProrationDate)
		if err != nil {
			return fmt.Errorf("failed to find/create target invoice for proration: %w", err)
		}

		var itemsToAdd []invoiceDomain.InvoiceLineItem
		for _, credit := range result.CreditItems {
			itemsToAdd = append(itemsToAdd, mapProrationItemToInvoiceItem(credit, subscriptionID, targetInvoiceID, "proration_credit"))
		}
		for _, charge := range result.ChargeItems {
			itemsToAdd = append(itemsToAdd, mapProrationItemToInvoiceItem(charge, subscriptionID, targetInvoiceID, "proration_charge"))
		}

		if len(itemsToAdd) > 0 {
			if err := s.invoiceRepo.AddLineItems(ctx, targetInvoiceID, itemsToAdd); err != nil {
				return fmt.Errorf("failed to add proration line items to invoice %s: %w", targetInvoiceID, err)
			}
		}
		*/
		// Placeholder for actual implementation:
		fmt.Printf("Applying proration for Sub %s: Net Amount %s. Behavior: %s
",
			subscriptionID, result.NetAmount.String(), behavior)
		// Remember to implement the actual invoice interaction logic.
		return nil // Replace with actual error handling

	}

	// Handle other behaviors if added in the future
	return fmt.Errorf("unsupported proration behavior: %s", behavior)
}

/*
// Conceptual helper function (replace with actual logic)
func (s *service) findOrCreateTargetInvoice(ctx context.Context, tenantID, envID, subID string, effectiveDate time.Time) (string, error) {
	// Logic to find the appropriate open invoice or create a new one
	return "inv_latest_or_new", nil
}

// Conceptual helper function (replace with actual logic)
func mapProrationItemToInvoiceItem(prorationItem proration.ProrationLineItem, subID, invID, itemType string) invoiceDomain.InvoiceLineItem {
	// Mapping logic
	return invoiceDomain.InvoiceLineItem{
		// ... map fields ...
		InvoiceID: invID,
		SubscriptionID: subID,
		Type: itemType,
		Amount: prorationItem.Amount,
		Description: prorationItem.Description,
		// ... etc ...
	}
}
*/

```

## 5. Create Proration Service Tests

Add unit tests for the service implementation, mocking dependencies like the calculator and repositories.

**File: `internal/service/proration/service_test.go`**

```go
package proration

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/yourusername/flexprice/internal/domain/proration"
	// Mock necessary dependencies like invoice repository
	// mockInvoiceRepo "github.com/yourusername/flexprice/internal/mocks/domain/invoice"
)

// MockProrationCalculator is a mock implementation of proration.Calculator
type MockProrationCalculator struct {
	mock.Mock
}

func (m *MockProrationCalculator) Calculate(ctx context.Context, params proration.ProrationParams) (*proration.ProrationResult, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*proration.ProrationResult), args.Error(1)
}


func TestCalculateProration(t *testing.T) {
	mockCalc := new(MockProrationCalculator)
	// mockInvRepo := new(mockInvoiceRepo.Repository) // Assuming you have mocks for invoice repo

	// Pass mock calculator, mock invoice repo
	service := NewService(mockCalc /*, mockInvRepo */)
	ctx := context.Background()

	params := proration.ProrationParams{
		Currency: "USD",
		ProrationDate: time.Now(),
		// ... other params ...
	}
	expectedResult := &proration.ProrationResult{
		NetAmount: decimal.NewFromFloat(10.0),
		// ... other fields ...
	}

	mockCalc.On("Calculate", ctx, params).Return(expectedResult, nil)

	result, err := service.CalculateProration(ctx, params)

	assert.NoError(t, err)
	assert.Equal(t, expectedResult, result)
	mockCalc.AssertExpectations(t)
}

func TestApplyProration_CreateInvoiceItems(t *testing.T) {
	mockCalc := new(MockProrationCalculator)
	// mockInvRepo := new(mockInvoiceRepo.Repository) // Mock invoice repo is crucial here
	service := NewService(mockCalc /*, mockInvRepo */)
	ctx := context.Background()

	prorationResult := &proration.ProrationResult{
		NetAmount: decimal.NewFromFloat(-5.50), // Net credit
		Currency:  "USD",
		CreditItems: []proration.ProrationLineItem{
			{Amount: decimal.NewFromFloat(-5.50), Description: "Credit item"},
		},
		ChargeItems: []proration.ProrationLineItem{},
	}
	behavior := proration.ProrationBehaviorCreateInvoiceItems
	tenantID := "t1"
	envID := "e1"
	subID := "sub1"

	// --- Mock the invoice repository interactions ---
	// Example: Assume ApplyProration needs to find an invoice and add items
	// mockInvRepo.On("FindLatestOpenInvoiceBySubscription", ctx, tenantID, envID, subID).Return("inv-123", nil)
	// mockInvRepo.On("AddLineItems", ctx, "inv-123", mock.AnythingOfType("[]invoice.InvoiceLineItem")).Return(nil)

	err := service.ApplyProration(ctx, prorationResult, behavior, tenantID, envID, subID)

	assert.NoError(t, err)
	// mockInvRepo.AssertExpectations(t) // Verify invoice repo calls were made
}


func TestApplyProration_NoBehavior(t *testing.T) {
	mockCalc := new(MockProrationCalculator)
	// mockInvRepo := new(mockInvoiceRepo.Repository)
	service := NewService(mockCalc /*, mockInvRepo */)
	ctx := context.Background()

	prorationResult := &proration.ProrationResult{NetAmount: decimal.NewFromInt(1)}
	behavior := proration.ProrationBehaviorNone // No action expected

	err := service.ApplyProration(ctx, prorationResult, behavior, "t1", "e1", "sub1")

	assert.NoError(t, err)
	// Assert that no calls were made to the invoice repository (if applicable)
	// mockInvRepo.AssertNotCalled(t, "AddLineItems", mock.Anything, mock.Anything, mock.Anything)
}


// Add more tests for edge cases, error handling, different behaviors, etc.

```

## 6. Dependency Injection

Ensure the `ProrationService` and its dependencies (`Calculator`, potentially `InvoiceRepository`) are registered correctly with your dependency injection framework (e.g., `fx`).

**File: `internal/service/factory.go` (or relevant fx module)**

```go
// Add to imports
prorationDomain "github.com/yourusername/flexprice/internal/domain/proration"
prorationService "github.com/yourusername/flexprice/internal/service/proration"
// Potentially invoice domain/repo imports

// Add to Provider struct (or relevant struct holding services)
ProrationService prorationDomain.ProrationService

// Add to NewProvider function (or relevant fx provider function)
func NewProvider(
	// ... existing parameters ...
	// invoiceRepository invoiceDomain.Repository, // Assuming invoice repo is available
	// ... existing parameters ...
) *Provider {
	// ... existing code ...

	// Create Proration dependencies
	prorationCalculator := prorationDomain.NewCalculator()
	// Create Proration service, injecting its dependencies
	prorationSvc := prorationService.NewService(
		prorationCalculator,
		// invoiceRepository, // Inject invoice repo
	)

	return &Provider{
		// ... existing fields ...

		// Add this line:
		ProrationService: prorationSvc,
	}
}

// Ensure the necessary repositories/services are tagged correctly for injection
// Example update to fx.Provide in the Module definition:
var Module = fx.Options(
	fx.Provide(
		// ... existing providers ...
		// Ensure invoice repository is provided with a tag like "repository:invoice"

		// Provide Proration Service
		fx.Annotate(
			func(p *Provider) prorationDomain.ProrationService { return p.ProrationService },
			fx.ResultTags(`name:"service:proration"`), // Tag the result if needed elsewhere
		),

		// Annotate the main Provider constructor if necessary
		fx.Annotate(
			NewProvider,
			fx.ParamTags(
				// ... existing tags ...
				// `name:"repository:invoice"`, // Tag for invoice repository parameter
				// ... existing tags ...
			),
		),
	),
)

```

Remember to replace placeholder comments and conceptual logic (especially around invoice interactions in `ApplyProration`) with your actual implementation details based on your existing `Invoice` domain and repository.

## 10. Integration with Subscription Operations

The Proration Service integrates with the subscription change operations through the `SubscriptionChangeOperation` interface. Here's how each operation type uses the proration service:

### 10.1 Operation Integration Example

**File: `internal/service/subscription/operations/update_line_item.go`**

```go
package operations

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yourusername/flexprice/internal/domain/proration"
	"github.com/yourusername/flexprice/internal/domain/subscription"
)

// UpdateLineItemOperation handles quantity and price changes for subscription line items
type UpdateLineItemOperation struct {
	LineItemID      string
	NewPriceID      string
	NewQuantity     decimal.Decimal
	ProrationParams *proration.ProrationParams
	ProrationSvc    proration.ProrationService
	SubRepo         subscription.Repository
	PriceService    PriceService
}

func (o *UpdateLineItemOperation) Plan(ctx context.Context, sub *subscription.Subscription) (*OperationPlan, error) {
	// Find the line item
	lineItem := sub.FindLineItem(o.LineItemID)
	if lineItem == nil {
		return nil, fmt.Errorf("line item %s not found", o.LineItemID)
	}

	// Get price details if changing price
	var newPrice *Price
	var err error
	if o.NewPriceID != "" && o.NewPriceID != lineItem.PriceID {
		newPrice, err = o.PriceService.GetPrice(ctx, o.NewPriceID)
		if err != nil {
			return nil, err
		}
	}

	// Prepare proration parameters
	params := o.ProrationParams
	params.LineItemID = o.LineItemID
	params.OldPriceID = lineItem.PriceID
	params.OldQuantity = lineItem.Quantity
	params.OldPricePerUnit = lineItem.PricePerUnit
	params.NewQuantity = o.NewQuantity
	params.NewPricePerUnit = newPrice.UnitAmount
	params.Action = determineProrationAction(lineItem, newPrice, o.NewQuantity)

	// Calculate proration
	prorationResult, err := o.ProrationSvc.CalculateProration(ctx, sub, params)
	if err != nil {
		return nil, err
	}

	// Create updated subscription clone
	updatedSub := sub.Clone()
	updatedLineItem := updatedSub.FindLineItem(o.LineItemID)
	if o.NewPriceID != "" {
		updatedLineItem.PriceID = o.NewPriceID
		updatedLineItem.PricePerUnit = newPrice.UnitAmount
	}
	updatedLineItem.Quantity = o.NewQuantity

	return &OperationPlan{
		ProrationResult: prorationResult,
		UpdatedSubscription: updatedSub,
	}, nil
}

func (o *UpdateLineItemOperation) Execute(ctx context.Context, sub *subscription.Subscription, plan *OperationPlan, isPreview bool) error {
	if isPreview {
		return nil
	}

	// Handle scheduled changes
	if o.ProrationParams.ScheduleType != proration.ScheduleTypeImmediate {
		return o.scheduleChange(ctx, sub, plan)
	}

	// Update line item
	if err := o.SubRepo.UpdateLineItem(ctx, o.LineItemID, o.NewPriceID, o.NewQuantity); err != nil {
		return err
	}

	// Apply proration if needed
	if plan.ProrationResult != nil && o.ProrationParams.ProrationBehavior != proration.ProrationBehaviorNone {
		if err := o.ProrationSvc.ApplyProration(ctx, sub, plan.ProrationResult, o.ProrationParams.ProrationBehavior); err != nil {
			return err
		}
	}

	return nil
}

func determineProrationAction(lineItem *subscription.LineItem, newPrice *Price, newQuantity decimal.Decimal) proration.ProrationAction {
	if newPrice != nil {
		if newPrice.UnitAmount.GreaterThan(lineItem.PricePerUnit) {
			return proration.ProrationActionUpgrade
		}
		return proration.ProrationActionDowngrade
	}
	return proration.ProrationActionQuantityChange
}
```

### 10.2 Orchestrator Integration

The Proration Service integrates with the Subscription Update Orchestrator:

**File: `internal/service/subscription/orchestrator.go`**

```go
type subscriptionUpdateOrchestrator struct {
	ProrationSvc proration.ProrationService
	SubRepo      subscription.Repository
	// ... other dependencies
}

func (o *subscriptionUpdateOrchestrator) ProcessUpdate(ctx context.Context, subID string, req dto.UpdateSubscriptionRequest, isPreview bool) (*dto.UpdateSubscriptionResponse, error) {
	// Get subscription with line items
	sub, err := o.SubRepo.GetWithLineItems(ctx, subID)
	if err != nil {
		return nil, err
	}

	// Create operations from request
	operations, err := o.createOperationsFromRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	// Inject proration service into operations that need it
	for _, op := range operations {
		if prorationOp, ok := op.(ProrationAwareOperation); ok {
			prorationOp.SetProrationService(o.ProrationSvc)
		}
	}

	// Continue with planning and execution...
}
```

## 11. Edge Case Handling

### 11.1 Trial Period Handling

```go
func (c *dayBasedCalculator) Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error) {
	// ... existing validation ...

	// Check if subscription is in trial
	if isInTrial(params.CurrentPeriodStart, params.CurrentPeriodEnd, params.TrialEnd) {
		// No proration needed during trial
		return &ProrationResult{
			NetAmount: decimal.Zero,
			Currency: params.Currency,
			Action: params.Action,
			IsPreview: params.ProrationBehavior == ProrationBehaviorNone,
		}, nil
	}

	// ... continue with normal calculation ...
}

func isInTrial(periodStart, periodEnd, trialEnd time.Time) bool {
	if trialEnd.IsZero() {
		return false
	}
	return !periodEnd.After(trialEnd)
}
```

### 11.2 Calendar vs Anniversary Billing

```go
type BillingAlignment string

const (
	BillingAlignmentAnniversary BillingAlignment = "anniversary"
	BillingAlignmentCalendar    BillingAlignment = "calendar"
)

func (c *dayBasedCalculator) calculateNextBillingDate(
	currentPeriodEnd time.Time,
	interval int,
	intervalUnit string,
	alignment BillingAlignment,
	customerTimezone string,
) time.Time {
	loc, _ := time.LoadLocation(customerTimezone)
	endInTZ := currentPeriodEnd.In(loc)

	if alignment == BillingAlignmentCalendar {
		// Align to calendar month/year
		switch intervalUnit {
		case "month":
			// Move to first of next month
			return time.Date(endInTZ.Year(), endInTZ.Month()+1, 1, 0, 0, 0, 0, loc)
		case "year":
			// Move to January 1st
			return time.Date(endInTZ.Year()+1, 1, 1, 0, 0, 0, 0, loc)
		}
	}

	// Anniversary billing - just add interval
	switch intervalUnit {
	case "month":
		return endInTZ.AddDate(0, interval, 0)
	case "year":
		return endInTZ.AddDate(interval, 0, 0)
	default:
		return endInTZ
	}
}
```

### 11.3 DST Transition Handling

```go
func (c *dayBasedCalculator) daysInDurationWithDST(start, end time.Time, loc *time.Location) int {
	// Normalize times to midnight in customer timezone
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)

	// Count calendar days, handling DST transitions
	days := 0
	current := startDay
	for current.Before(endDay) {
		days++
		// Add 24 hours, then normalize to midnight to handle DST
		next := current.Add(24 * time.Hour)
		current = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc)
	}

	return days
}
```

## 12. Testing Strategy

Update the test suite to include comprehensive edge case coverage:

**File: `internal/domain/proration/calculator_test.go`**

```go
func TestCalculator_MultipleChanges(t *testing.T) {
	tests := []struct {
		name           string
		originalAmount decimal.Decimal
		previousCredits decimal.Decimal
		changeAmount   decimal.Decimal
		expectedCredit decimal.Decimal
	}{
		{
			name: "Multiple upgrades should not exceed original payment",
			originalAmount: decimal.NewFromInt(100),
			previousCredits: decimal.NewFromInt(60),
			changeAmount: decimal.NewFromInt(50),
			expectedCredit: decimal.NewFromInt(40), // 100 - 60 previous credits
		},
		// ... more test cases ...
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := NewCalculator()
			params := ProrationParams{
				OriginalAmountPaid: tt.originalAmount,
				PreviousCreditsIssued: tt.previousCredits,
				// ... other params ...
			}
			result, err := calc.Calculate(context.Background(), params)
			assert.NoError(t, err)
			assert.True(t, result.NetAmount.Equal(tt.expectedCredit))
		})
	}
}

func TestCalculator_TimezoneEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		timezone string
		periodStart time.Time
		periodEnd time.Time
		prorationDate time.Time
		expectedDays int
	}{
		{
			name: "DST transition forward",
			timezone: "America/New_York",
			periodStart: time.Date(2024, 3, 10, 0, 0, 0, 0, time.UTC),
			periodEnd: time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC),
			prorationDate: time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC),
			expectedDays: 1,
		},
		// ... more timezone test cases ...
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := NewCalculator()
			params := ProrationParams{
				CustomerTimezone: tt.timezone,
				CurrentPeriodStart: tt.periodStart,
				CurrentPeriodEnd: tt.periodEnd,
				ProrationDate: tt.prorationDate,
				// ... other params ...
			}
			result, err := calc.Calculate(context.Background(), params)
			assert.NoError(t, err)
			// Verify proration calculations handle timezone correctly
		})
	}
}
```

// ... existing code ...
