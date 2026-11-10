package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// CommitmentE2ESuite drives commitments through the public subscription, commitment and billing services, from
// creation to the lines an invoice for the period carries. Usage is read from meter_usage, as invoicing reads it.
type CommitmentE2ESuite struct {
	testutil.BaseServiceTestSuite
	subscriptions SubscriptionService
	commitments   CommitmentService
	billing       BillingService
	analytics     MeterUsageService
	customer      *customer.Customer
	tokens        *meter.Meter
	gpus          *meter.Meter
	jan           time.Time // every subscription starts on 1 Jan 2026
}

func TestCommitmentE2ESuite(t *testing.T) { suite.Run(t, new(CommitmentE2ESuite)) }

func (s *CommitmentE2ESuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	ctx := s.GetContext()
	params := fullServiceParams(&s.BaseServiceTestSuite)
	s.subscriptions = NewSubscriptionService(params)
	s.commitments = NewCommitmentService(params)
	s.billing = NewBillingService(params)
	s.analytics = NewMeterUsageService(params)
	s.jan = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	s.customer = &customer.Customer{
		ID:         "cust_e2e",
		ExternalID: "ext_cust_e2e",
		Name:       "Acme",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, s.customer))

	s.tokens = &meter.Meter{
		ID:          "meter_e2e_tokens",
		Name:        "Tokens",
		EventName:   "tokens",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "tokens"},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.gpus = &meter.Meter{
		ID:          "meter_e2e_gpus",
		Name:        "GPUs",
		EventName:   "gpus",
		Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "gpus", BucketSize: types.WindowSizeDay},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.tokens))
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.gpus))
	setCommitmentsV2(s.T(), ctx, s.GetStores(), true)
}

// planWith stores a plan with one arrear usage price on the meter, billed every period; tiers, when given, make
// the price slab-tiered instead of a flat amount per unit.
func (s *CommitmentE2ESuite) planWith(
	id string,
	period types.BillingPeriod,
	m *meter.Meter,
	amount string,
	tiers ...price.PriceTier,
) *price.Price {
	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, &plan.Plan{
		ID:        "plan_" + id,
		Name:      id,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	p := &price.Price{
		ID:                 "price_" + id,
		Amount:             dec(amount),
		Currency:           "usd",
		DisplayName:        m.Name,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           "plan_" + id,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            m.ID,
		BillingPeriod:      period,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	if len(tiers) > 0 {
		p.BillingModel, p.TierMode, p.Tiers = types.BILLING_MODEL_TIERED, types.BILLING_TIER_SLAB, tiers
	}

	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, p))
	return p
}

// subscribe creates the customer's subscription to the plan from start, with any inline commitments.
func (s *CommitmentE2ESuite) subscribe(
	p *price.Price,
	start time.Time,
	commitments ...dto.CreateCommitmentRequest,
) *dto.SubscriptionResponse {
	resp, err := s.subscriptions.CreateSubscription(s.GetContext(), dto.CreateSubscriptionRequest{
		CustomerID:         s.customer.ID,
		PlanID:             p.EntityID,
		StartDate:          lo.ToPtr(start),
		Currency:           "usd",
		BillingPeriod:      p.BillingPeriod,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		Commitments:        commitments,
	})
	s.Require().NoError(err)
	s.Require().Equal(types.SubscriptionStatusActive, resp.SubscriptionStatus)
	return resp
}

// floorRequest is a binding amount commitment of value per billing period on one price, from start to end.
func floorRequest(name string, priceID string, value string, start, end time.Time) dto.CreateCommitmentRequest {
	return dto.CreateCommitmentRequest{
		Name:        name,
		Scope:       dto.CommitmentScopeInput{EntityType: types.CommitmentScopePrice, EntityID: priceID},
		Type:        types.COMMITMENT_TYPE_AMOUNT,
		Value:       dec(value),
		Measurement: types.CommitmentMeasurementBillingPeriod,
		Binding:     lo.ToPtr(true),
		StartDate:   start,
		EndDate:     lo.ToPtr(end),
	}
}

func (s *CommitmentE2ESuite) commit(subscriptionID string, req dto.CreateCommitmentRequest) *dto.CommitmentResponse {
	req.SubscriptionID = subscriptionID
	resp, err := s.commitments.Create(s.GetContext(), req)
	s.Require().NoError(err)
	return resp
}

// record stores one meter_usage row of qty for the customer at `at`.
func (s *CommitmentE2ESuite) record(m *meter.Meter, at time.Time, qty string) {
	s.recordFrom(m, "", at, qty)
}

// recordFrom stores one meter_usage row of qty for the customer at `at`, sent from source.
func (s *CommitmentE2ESuite) recordFrom(m *meter.Meter, source string, at time.Time, qty string) {
	ctx := s.GetContext()
	id := types.GenerateUUIDWithPrefix("mu")
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{
			ID:                 id,
			TenantID:           types.GetTenantID(ctx),
			EnvironmentID:      types.GetEnvironmentID(ctx),
			EventName:          m.EventName,
			CustomerID:         s.customer.ID,
			ExternalCustomerID: s.customer.ExternalID,
			Timestamp:          at,
			IngestedAt:         at,
			Source:             source,
		},
		MeterID:    m.ID,
		QtyTotal:   dec(qty),
		UniqueHash: id,
	}}))
}

// invoiceFor prepares the arrear invoice for [from, to) the way the period-end billing run does.
func (s *CommitmentE2ESuite) invoiceFor(subscriptionID string, from, to time.Time) *dto.CreateInvoiceRequest {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), subscriptionID)
	s.Require().NoError(err)

	req, err := s.billing.PrepareSubscriptionInvoiceRequest(s.GetContext(), &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   sub,
		PeriodStart:    from,
		PeriodEnd:      to,
		ReferencePoint: types.ReferencePointPeriodEnd,
		AsOf:           to,
	})
	s.Require().NoError(err)
	return req
}

func (s *CommitmentE2ESuite) requireInvoice(req *dto.CreateInvoiceRequest, total string, want ...wantLine) {
	requireLines(s.T(), want, req.LineItems)
	s.True(req.AmountDue.Equal(dec(total)), "amount due: want %s, got %s", total, req.AmountDue)
}

// openingInvoice returns the invoice issued when the subscription was created.
func (s *CommitmentE2ESuite) openingInvoice(subscriptionID string) *invoice.Invoice {
	opening := s.invoicesWithReason(subscriptionID, types.InvoiceBillingReasonSubscriptionCreate)
	s.Require().Len(opening, 1)
	return opening[0]
}

// billPeriod bills the month starting at from as the renewal run does, claiming its instalments, and returns the
// stored invoice. A month that bills nothing is skipped and keeps its lines.
func (s *CommitmentE2ESuite) billPeriod(subscriptionID string, from time.Time) *invoice.Invoice {
	to := from.AddDate(0, 1, 0)
	_, _, err := NewInvoiceService(fullServiceParams(&s.BaseServiceTestSuite)).CreateSubscriptionInvoice(
		s.GetContext(),
		&dto.CreateSubscriptionInvoiceRequest{
			SubscriptionID: subscriptionID,
			PeriodStart:    from,
			PeriodEnd:      to,
			ReferencePoint: types.ReferencePointPeriodEnd,
		},
		nil,
		types.InvoiceFlowRenewal,
		false,
	)
	s.Require().NoError(err)

	billed, err := s.GetStores().InvoiceRepo.GetForPeriod(s.GetContext(), subscriptionID, from, to, string(types.InvoiceBillingReasonSubscriptionCycle))
	s.Require().NoError(err)
	return billed
}

// requireBilled checks a stored invoice's amount due and its lines, in any order.
func (s *CommitmentE2ESuite) requireBilled(inv *invoice.Invoice, total string, want ...wantLine) {
	stored, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), inv.ID)
	s.Require().NoError(err)

	describe := func(kind, name string, qty, amount decimal.Decimal) string {
		return fmt.Sprintf("%s | %s | qty %s | amount %s", kind, name, qty, amount)
	}
	got := lo.Map(stored, func(line *invoice.InvoiceLineItem, _ int) string {
		return describe(line.Metadata[types.MetadataKeyCommitmentLineKind], lo.FromPtr(line.DisplayName), line.Quantity, line.Amount)
	})
	expected := lo.Map(want, func(line wantLine, _ int) string {
		return describe(string(line.kind), line.name, dec(line.qty), dec(line.amount))
	})

	s.ElementsMatch(expected, got, "invoice %s for %s", inv.ID, inv.PeriodStart)
	s.True(inv.AmountDue.Equal(dec(total)), "amount due: want %s, got %s", total, inv.AmountDue)
}

// requireFullyBilled checks the subscription has count instalments, all invoiced and worth total together.
func (s *CommitmentE2ESuite) requireFullyBilled(subscriptionID string, count int, total string) {
	instalments, err := s.GetStores().BillingInstalmentRepo.List(s.GetContext(), &billingterm.InstalmentFilter{SubscriptionIDs: []string{subscriptionID}})
	s.Require().NoError(err)
	s.Require().Len(instalments, count)

	sum := decimal.Zero
	for _, inst := range instalments {
		s.Equal(types.InstalmentStatusInvoiced, inst.InstalmentStatus, inst.InvoiceLabel)
		sum = sum.Add(inst.Amount)
	}
	s.True(sum.Equal(dec(total)), "instalments total %s, want %s", sum, total)
}

// A $100 monthly floor bills its true-up when usage falls short and overage when it runs over.
func (s *CommitmentE2ESuite) TestMonthlyAmountFloor_UnderThenOverUse() {
	tokens := s.planWith("monthly_floor", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "100", s.jan, s.jan.AddDate(0, 6, 0)))

	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "600")
	s.record(s.tokens, time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC), "1500")

	// January: 600 tokens cost $60, $40 short of the floor.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "100",
		wantLine{types.CommitmentLineUtilized, "Platform floor", "600", "60"},
		wantLine{types.CommitmentLineTrueUp, "Platform floor (unused)", "1", "40"},
	)

	// February: 1,500 tokens cost $150; the floor covers $100 of it.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan.AddDate(0, 1, 0), s.jan.AddDate(0, 2, 0)), "150",
		wantLine{types.CommitmentLineUtilized, "Platform floor", "1000", "100"},
		wantLine{types.CommitmentLineOverage, "Tokens", "500", "50"},
	)
}

// Committed units bill at the committed rate, units beyond them at the custom overage rate, and a short month
// bills its unused units at the committed rate.
func (s *CommitmentE2ESuite) TestQuantityCommitment_CommittedAndOverageRates() {
	tokens := s.planWith("quantity", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	req := floorRequest("Reserved tokens", tokens.ID, "1000", s.jan, s.jan.AddDate(0, 6, 0))
	req.Type = types.COMMITMENT_TYPE_QUANTITY
	req.CommittedPrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("0.08"))}
	req.OveragePrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("0.12"))}
	s.commit(sub.ID, req)

	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "1300")
	s.record(s.tokens, time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC), "700")

	// January: 1,000 committed at $0.08, the other 300 at the $0.12 overage rate.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "116",
		wantLine{types.CommitmentLineUtilized, "Reserved tokens", "1000", "80"},
		wantLine{types.CommitmentLineOverage, "Tokens", "300", "36"},
	)

	// February: 700 used, so 300 committed units are billed unused.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan.AddDate(0, 1, 0), s.jan.AddDate(0, 2, 0)), "80",
		wantLine{types.CommitmentLineUtilized, "Reserved tokens", "700", "56"},
		wantLine{types.CommitmentLineTrueUp, "Reserved tokens (unused)", "300", "24"},
	)
}

// Eight GPUs a day: full days fill the window, idle days leave it unused, and GPUs above eight bill as overage
// at the margin of each day's own usage.
func (s *CommitmentE2ESuite) TestDailyWindowQuantityCommitment() {
	gpus := s.planWith("daily_gpus", types.BILLING_PERIOD_MONTHLY, s.gpus, "2")
	sub := s.subscribe(gpus, s.jan)
	req := floorRequest("Reserved GPUs", gpus.ID, "8", s.jan, s.jan.AddDate(0, 3, 0))
	req.Type, req.Measurement = types.COMMITMENT_TYPE_QUANTITY, types.CommitmentMeasurementWindow
	req.CommittedPrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("1.5"))}
	s.commit(sub.ID, req)

	// 1-10 Jan run 8 GPUs, 11-20 Jan are idle, 21-31 Jan run 10.
	for day := 1; day <= 31; day++ {
		switch {
		case day <= 10:
			s.record(s.gpus, time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC), "8")
		case day >= 21:
			s.record(s.gpus, time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC), "10")
		}
	}

	// 168 of 248 GPU-days used at $1.50; 80 unused; 22 above the window at $2.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "416",
		wantLine{types.CommitmentLineUtilized, "Reserved GPUs", "168", "252"},
		wantLine{types.CommitmentLineTrueUp, "Reserved GPUs (unused)", "80", "120"},
		wantLine{types.CommitmentLineOverage, "GPUs", "22", "44"},
	)
}

// Ten reserved GPUs a day at $1.50, with no end date: every month bills its full floor, used GPU-days plus a
// true-up for the rest. Terminated on 11 Mar, March's floor shrinks to the ten days it ran, and a larger
// reservation can take over from the termination.
func (s *CommitmentE2ESuite) TestOpenEndedDailyWindowCommitment_BillsEveryMonthUntilTerminated() {
	gpus := s.planWith("open_gpus", types.BILLING_PERIOD_MONTHLY, s.gpus, "2")
	sub := s.subscribe(gpus, s.jan)
	req := floorRequest("Reserved GPUs", gpus.ID, "10", s.jan, time.Time{})
	req.EndDate = nil
	req.Type, req.Measurement = types.COMMITMENT_TYPE_QUANTITY, types.CommitmentMeasurementWindow
	req.CommittedPrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("1.5"))}
	created := s.commit(sub.ID, req)
	s.Nil(created.EndDate)
	s.Nil(created.CommitmentValue, "an open-ended commitment has no finite value")

	// January runs 8 GPUs a day, February 6, the first ten days of March 7 and the rest of March 5.
	feb, mar, apr := s.jan.AddDate(0, 1, 0), s.jan.AddDate(0, 2, 0), s.jan.AddDate(0, 3, 0)
	for day := s.jan; day.Before(apr); day = day.AddDate(0, 0, 1) {
		qty := "8"
		switch {
		case !day.Before(mar.AddDate(0, 0, 10)):
			qty = "5"
		case !day.Before(mar):
			qty = "7"
		case !day.Before(feb):
			qty = "6"
		}

		s.record(s.gpus, day.Add(12*time.Hour), qty)
	}

	// January: 248 of 310 GPU-days used, 62 unused, the full $465 floor.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, feb), "465",
		wantLine{types.CommitmentLineUtilized, "Reserved GPUs", "248", "372"},
		wantLine{types.CommitmentLineTrueUp, "Reserved GPUs (unused)", "62", "93"},
	)

	// February: 168 of 280 GPU-days used, 112 unused, the full $420 floor.
	s.requireInvoice(s.invoiceFor(sub.ID, feb, mar), "420",
		wantLine{types.CommitmentLineUtilized, "Reserved GPUs", "168", "252"},
		wantLine{types.CommitmentLineTrueUp, "Reserved GPUs (unused)", "112", "168"},
	)

	terminatedAt := mar.AddDate(0, 0, 10)
	terminated, err := s.commitments.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt: lo.ToPtr(terminatedAt),
	})
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, terminated.CommitmentStatus)
	s.Nil(terminated.TerminationInvoiceID)

	// Twelve GPUs a day from the termination on; the terminated reservation no longer allocates from 11 Mar.
	expanded := floorRequest("Expanded GPUs", gpus.ID, "12", terminatedAt, time.Time{})
	expanded.EndDate = nil
	expanded.Type, expanded.Measurement = types.COMMITMENT_TYPE_QUANTITY, types.CommitmentMeasurementWindow
	expanded.CommittedPrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("1.5"))}
	s.commit(sub.ID, expanded)

	// March: the ten days to the termination are a $150 floor, 70 of 100 GPU-days used; the 21 days after it a
	// $378 floor on the new reservation, 105 of 252 used.
	s.requireInvoice(s.invoiceFor(sub.ID, mar, apr), "528",
		wantLine{types.CommitmentLineUtilized, "Reserved GPUs", "70", "105"},
		wantLine{types.CommitmentLineTrueUp, "Reserved GPUs (unused)", "30", "45"},
		wantLine{types.CommitmentLineUtilized, "Expanded GPUs", "105", "157.5"},
		wantLine{types.CommitmentLineTrueUp, "Expanded GPUs (unused)", "147", "220.5"},
	)
}

// A price edit on 16 Jan versions the scoped line onto a flat price on the same meter. The meter-scoped commitment
// keeps covering both versions, and each prices its own usage: overage units of the slab version bill at the
// margin of its tiers.
func (s *CommitmentE2ESuite) TestMidPeriodPriceEdit_CommitmentSpansBothVersions() {
	// The "00" ID sorts before generated price IDs, so the original version fills first.
	slab := s.planWith("00_versioned", types.BILLING_PERIOD_MONTHLY, s.tokens, "0",
		price.PriceTier{UpTo: lo.ToPtr(uint64(100)), UnitAmount: dec("0.5")},
		price.PriceTier{UnitAmount: dec("0.25")},
	)
	sub := s.subscribe(slab, s.jan)
	req := floorRequest("Reserved tokens", s.tokens.ID, "100", s.jan, s.jan.AddDate(0, 6, 0))
	req.Scope.EntityType, req.Type = types.CommitmentScopeMeter, types.COMMITMENT_TYPE_QUANTITY
	req.CommittedPrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("0.2"))}
	s.commit(sub.ID, req)

	original := sub.LineItems[0]
	editAt := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
	successor, err := s.subscriptions.UpdateSubscriptionLineItem(s.GetContext(), original.ID, dto.UpdateSubscriptionLineItemRequest{
		EffectiveFrom: lo.ToPtr(editAt),
		BillingModel:  types.BILLING_MODEL_FLAT_FEE,
		Amount:        lo.ToPtr(dec("1")),
	})
	s.Require().NoError(err)
	s.Equal(slab.MeterID, successor.MeterID)
	s.NotEqual(slab.ID, successor.PriceID)

	s.record(s.tokens, time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), "150")
	s.record(s.tokens, time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC), "60")

	// The 100 committed come from the slab version's 150. Its other 50 sit in the $0.25 tier: $12.50. All 60 on
	// the flat version are overage at $1.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "92.5",
		wantLine{types.CommitmentLineUtilized, "Reserved tokens", "100", "20"},
		wantLine{types.CommitmentLineOverage, "Tokens", "50", "12.5"},
		wantLine{types.CommitmentLineOverage, "Tokens", "60", "60"},
	)
}

// A commitment added halfway through a quarter has a floor for the half it covers; usage before it starts bills
// as plain usage.
func (s *CommitmentE2ESuite) TestQuarterlyCommitmentCreatedMidPeriod_ProratesItsFirstFloor() {
	tokens := s.planWith("quarterly", types.BILLING_PERIOD_QUARTER, s.tokens, "1")
	sub := s.subscribe(tokens, s.jan)
	midQuarter := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	created := s.commit(sub.ID, floorRequest("Quarterly floor", tokens.ID, "900", midQuarter, s.jan.AddDate(0, 6, 0)))
	// 45 of Q1's 90 days, then all of Q2.
	s.True(created.CommitmentValue.Equal(dec("1350")), "commitment value %s", created.CommitmentValue)

	s.record(s.tokens, time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC), "200")
	s.record(s.tokens, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), "300")

	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 3, 0)), "650",
		wantLine{types.CommitmentLineUtilized, "Quarterly floor", "300", "300"},
		wantLine{types.CommitmentLineTrueUp, "Quarterly floor (unused)", "1", "150"},
		wantLine{types.CommitmentLinePlain, "Tokens", "200", "200"},
	)
}

// A commitment sent with the subscription bills on the subscription's first invoice.
func (s *CommitmentE2ESuite) TestInlineCommitment_BillsOnTheFirstInvoice() {
	tokens := s.planWith("inline", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.05")
	sub := s.subscribe(tokens, s.jan, floorRequest("Launch floor", tokens.ID, "50", s.jan, s.jan.AddDate(0, 6, 0)))

	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "600")

	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "50",
		wantLine{types.CommitmentLineUtilized, "Launch floor", "600", "30"},
		wantLine{types.CommitmentLineTrueUp, "Launch floor (unused)", "1", "20"},
	)
}

// Terminated on 11 Jan with a $500 fee: January's floor shrinks to the ten days the commitment ran, usage after
// it bills as plain usage, and the fee is billed on its own one-off invoice.
func (s *CommitmentE2ESuite) TestTerminateMidPeriod_ClipsTheFloorAndBillsTheFee() {
	tokens := s.planWith("terminated", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	created := s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "310", s.jan, s.jan.AddDate(0, 6, 0)))

	terminatedAt := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
	terminated, err := s.commitments.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:     lo.ToPtr(terminatedAt),
		CancellationFee: fixedFee("500"),
	})
	s.Require().NoError(err)
	s.Require().NotNil(terminated.TerminationInvoiceID)

	fee, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), *terminated.TerminationInvoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeOneOff, fee.InvoiceType)
	s.Equal(string(types.InvoiceBillingReasonCommitmentTermination), fee.BillingReason)
	s.True(fee.AmountDue.Equal(dec("500")), "fee invoice %s", fee.AmountDue)
	feeLines, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), fee.ID)
	s.Require().NoError(err)
	s.Require().Len(feeLines, 1)
	s.Equal(string(types.CommitmentLineCancellationFee), feeLines[0].Metadata[types.MetadataKeyCommitmentLineKind])

	s.record(s.tokens, time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), "400")
	s.record(s.tokens, time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC), "300")

	// $310 a month for 10 of 31 days is a $100 floor; $40 used before the end, $30 after.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "130",
		wantLine{types.CommitmentLineUtilized, "Platform floor", "400", "40"},
		wantLine{types.CommitmentLineTrueUp, "Platform floor (unused)", "1", "60"},
		wantLine{types.CommitmentLinePlain, "Tokens", "300", "30"},
	)
}

// A $310 monthly floor for exactly one year from 15 Jan: January bills the 17 days from the start, a middle month
// the full floor, and the next January the 14 days to the end, with usage after the end billed as plain usage.
func (s *CommitmentE2ESuite) TestOneYearCommitmentFromMidPeriod_ProratesItsFirstAndLastFloors() {
	tokens := s.planWith("one_year", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	created := s.commit(sub.ID, floorRequest("Annual floor", tokens.ID, "310", start, start.AddDate(1, 0, 0)))
	// $170 + 11 × $310 + $140.
	s.True(created.CommitmentValue.Equal(dec("3720")), "commitment value %s", created.CommitmentValue)

	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "200")
	s.record(s.tokens, time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC), "1000")
	s.record(s.tokens, time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC), "2000")
	s.record(s.tokens, time.Date(2027, 1, 5, 12, 0, 0, 0, time.UTC), "600")
	s.record(s.tokens, time.Date(2027, 1, 20, 12, 0, 0, 0, time.UTC), "500")

	// January 2026: 17 of 31 days is a $170 floor; $100 used after the start, $20 before it.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "190",
		wantLine{types.CommitmentLineUtilized, "Annual floor", "1000", "100"},
		wantLine{types.CommitmentLineTrueUp, "Annual floor (unused)", "1", "70"},
		wantLine{types.CommitmentLinePlain, "Tokens", "200", "20"},
	)

	// June 2026: the full $310 floor against $200 used.
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	s.requireInvoice(s.invoiceFor(sub.ID, june, june.AddDate(0, 1, 0)), "310",
		wantLine{types.CommitmentLineUtilized, "Annual floor", "2000", "200"},
		wantLine{types.CommitmentLineTrueUp, "Annual floor (unused)", "1", "110"},
	)

	// January 2027: 14 of 31 days is a $140 floor; $60 used before the end, $50 after it.
	lastJan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	s.requireInvoice(s.invoiceFor(sub.ID, lastJan, lastJan.AddDate(0, 1, 0)), "190",
		wantLine{types.CommitmentLineUtilized, "Annual floor", "600", "60"},
		wantLine{types.CommitmentLineTrueUp, "Annual floor (unused)", "1", "80"},
		wantLine{types.CommitmentLinePlain, "Tokens", "500", "50"},
	)
}

// A floor of 150 a month from 17 Apr holds exactly 70 for April's last 14 of 30 days, so the commitment's value and
// April's invoice lines carry no noise from the proration, and a package-priced floor bills 7 packages, not 8.
func (s *CommitmentE2ESuite) TestCommitmentFromMidPeriod_ProratesItsFloorExactly() {
	apr, may := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	start, end := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		committed *dto.CommitmentPriceInput // prices a quantity floor; nil for an amount floor
		used      string
		value     string
		total     string
		lines     []wantLine
	}{
		{
			// $70 for April, then $150 for each of May, June and July; April's 80 tokens at $1 run $10 over its floor.
			name:  "amount floor",
			used:  "80",
			value: "520",
			total: "80",
			lines: []wantLine{
				{types.CommitmentLineUtilized, "Floor", "70", "70"},
				{types.CommitmentLineOverage, "Tokens", "10", "10"},
			},
		},
		{
			// 7 packages of 10 tokens at $5 for April, then 15 a month; the 30 used fill 3 and the other 40 bill unused.
			name: "package-priced quantity floor",
			committed: &dto.CommitmentPriceInput{
				BillingModel:      types.BILLING_MODEL_PACKAGE,
				Amount:            lo.ToPtr(dec("5")),
				TransformQuantity: &price.TransformQuantity{DivideBy: 10, Round: types.ROUND_UP},
			},
			used:  "30",
			value: "260",
			total: "35",
			lines: []wantLine{
				{types.CommitmentLineUtilized, "Floor", "30", "15"},
				{types.CommitmentLineTrueUp, "Floor (unused)", "40", "20"},
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			tokens := s.planWith("prorated_floor", types.BILLING_PERIOD_MONTHLY, s.tokens, "1")
			sub := s.subscribe(tokens, s.jan)
			req := floorRequest("Floor", tokens.ID, "150", start, end)
			if tt.committed != nil {
				req.Type, req.CommittedPrice = types.COMMITMENT_TYPE_QUANTITY, tt.committed
			}

			created := s.commit(sub.ID, req)
			s.Equal(tt.value, created.CommitmentValue.String())

			s.record(s.tokens, time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC), tt.used)
			s.requireInvoice(s.invoiceFor(sub.ID, apr, may), tt.total, tt.lines...)
		})
	}
}

// tokenSlabs bills the first 100 tokens at $0.50 and the rest at $0.25.
var tokenSlabs = []price.PriceTier{
	{UpTo: lo.ToPtr(uint64(100)), UnitAmount: dec("0.5")},
	{UnitAmount: dec("0.25")},
}

// recordSourcedTokens records January's 300 tokens from three sources: api 60, batch 90 and console 150. Walked on
// the slabs as one line they cost $100; walked per source they would cost $137.50.
func (s *CommitmentE2ESuite) recordSourcedTokens() {
	s.recordFrom(s.tokens, "api", time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), "20")
	s.recordFrom(s.tokens, "api", time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC), "40")
	s.recordFrom(s.tokens, "batch", time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC), "90")
	s.recordFrom(s.tokens, "console", time.Date(2026, 1, 8, 12, 0, 0, 0, time.UTC), "50")
	s.recordFrom(s.tokens, "console", time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC), "100")
}

// usageAnalytics returns the customer's January usage analytics.
func (s *CommitmentE2ESuite) usageAnalytics(
	groupBy []string,
	sources []string,
	window types.WindowSize,
) *dto.GetUsageAnalyticsResponse {
	resp, err := s.analytics.GetDetailedAnalytics(s.GetContext(), &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(s.GetContext()),
		EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.jan,
		EndTime:            s.jan.AddDate(0, 1, 0),
		GroupBy:            groupBy,
		Sources:            sources,
		WindowSize:         window,
	})
	s.Require().NoError(err)
	return resp
}

// requireSourceCosts checks the analytics has one item per source, at the given cost each, summing to invoiced.
func (s *CommitmentE2ESuite) requireSourceCosts(
	resp *dto.GetUsageAnalyticsResponse,
	invoice *dto.CreateInvoiceRequest,
	want map[string]string,
) map[string]dto.UsageAnalyticItem {
	got := lo.KeyBy(resp.Items, func(item dto.UsageAnalyticItem) string { return item.Source })
	s.Require().Len(got, len(want))
	sum := decimal.Zero
	for source, cost := range want {
		s.True(dec(cost).Equal(got[source].TotalCost), "%s: want %s, got %s", source, cost, got[source].TotalCost)
		sum = sum.Add(got[source].TotalCost)
	}

	invoiced := decimal.Zero
	for _, line := range invoice.LineItems {
		invoiced = invoiced.Add(line.Amount)
	}
	s.True(sum.Equal(invoiced), "sources sum to %s, invoice lines to %s", sum, invoiced)
	s.True(resp.TotalCost.Equal(invoiced), "analytics total %s, invoice lines %s", resp.TotalCost, invoiced)
	return got
}

// The tiers walk the line's whole 300 tokens and the $150 floor's true-up joins them; each source takes its usage
// share of the $150 the invoice bills.
func (s *CommitmentE2ESuite) TestAnalytics_GroupedAmountFloorSplitsTheInvoice() {
	tokens := s.planWith("analytics_floor", types.BILLING_PERIOD_MONTHLY, s.tokens, "0", tokenSlabs...)
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "150", s.jan, s.jan.AddDate(0, 6, 0)))
	s.recordSourcedTokens()

	invoice := s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0))
	s.requireInvoice(invoice, "150",
		wantLine{types.CommitmentLineUtilized, "Platform floor", "300", "100"},
		wantLine{types.CommitmentLineTrueUp, "Platform floor (unused)", "1", "50"},
	)

	got := s.requireSourceCosts(s.usageAnalytics([]string{"source"}, nil, ""), invoice,
		map[string]string{"api": "30", "batch": "45", "console": "75"})
	console := got["console"].CommitmentInfo
	s.Require().NotNil(console)
	s.Equal(types.COMMITMENT_TYPE_AMOUNT, console.Type)
	s.True(console.TrueUpEnabled)
	s.True(console.ComputedCommitmentUtilizedAmount.Equal(dec("50")), "within %s", console.ComputedCommitmentUtilizedAmount)
	s.True(console.ComputedTrueUpAmount.Equal(dec("25")), "true-up %s", console.ComputedTrueUpAmount)
	s.True(console.ComputedOverageAmount.IsZero(), "overage %s", console.ComputedOverageAmount)
	s.True(console.Amount.Equal(dec("75")), "committed share %s", console.Amount)
}

// 200 reserved tokens at $0.20 and 100 more at the $0.40 overage price bill $80; every source pays the same
// blended rate on its tokens.
func (s *CommitmentE2ESuite) TestAnalytics_GroupedQuantityOverUseBlendsOneRate() {
	tokens := s.planWith("analytics_quantity", types.BILLING_PERIOD_MONTHLY, s.tokens, "0", tokenSlabs...)
	sub := s.subscribe(tokens, s.jan)
	req := floorRequest("Reserved tokens", tokens.ID, "200", s.jan, s.jan.AddDate(0, 6, 0))
	req.Type = types.COMMITMENT_TYPE_QUANTITY
	req.CommittedPrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("0.2"))}
	req.OveragePrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("0.4"))}
	s.commit(sub.ID, req)
	s.recordSourcedTokens()

	invoice := s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0))
	s.requireInvoice(invoice, "80",
		wantLine{types.CommitmentLineUtilized, "Reserved tokens", "200", "40"},
		wantLine{types.CommitmentLineOverage, "Tokens", "100", "40"},
	)

	got := s.requireSourceCosts(s.usageAnalytics([]string{"source"}, nil, ""), invoice,
		map[string]string{"api": "16", "batch": "24", "console": "40"})
	for source, item := range got {
		rate := item.TotalCost.Div(item.TotalUsage)
		s.True(rate.Equal(dec("80").Div(dec("300"))), "%s pays %s a token", source, rate)
		s.True(item.CommitmentInfo.ComputedOverageAmount.Equal(item.TotalCost.Div(dec("2"))), "%s overage share", source)
	}
}

// Without group_by the line's one item carries the whole within and true-up.
func (s *CommitmentE2ESuite) TestAnalytics_UngroupedLineCarriesTheCommitment() {
	tokens := s.planWith("analytics_ungrouped", types.BILLING_PERIOD_MONTHLY, s.tokens, "0", tokenSlabs...)
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "150", s.jan, s.jan.AddDate(0, 6, 0)))
	s.recordSourcedTokens()

	resp := s.usageAnalytics(nil, nil, "")
	s.Require().Len(resp.Items, 1)
	item := resp.Items[0]
	s.True(item.TotalCost.Equal(dec("150")), "total %s", item.TotalCost)
	s.Require().NotNil(item.CommitmentInfo)
	s.True(item.CommitmentInfo.ComputedCommitmentUtilizedAmount.Equal(dec("100")))
	s.True(item.CommitmentInfo.ComputedTrueUpAmount.Equal(dec("50")))
}

// A query ending mid-period before today is priced as that window's invoice prices it, with the floor prorated to the
// window; only a window reaching today projects the open period's floor to its end.
func (s *CommitmentE2ESuite) TestAnalytics_PastMidPeriodQueryPricesItsOwnWindow() {
	tokens := s.planWith("analytics_midwindow", types.BILLING_PERIOD_MONTHLY, s.tokens, "0", tokenSlabs...)
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "150", s.jan, s.jan.AddDate(0, 6, 0)))
	s.recordSourcedTokens()

	end := s.jan.AddDate(0, 0, 20)
	invoice := s.invoiceFor(sub.ID, s.jan, end)
	billed := map[types.CommitmentLineKind]decimal.Decimal{}
	for _, line := range invoice.LineItems {
		kind := types.CommitmentLineKind(line.Metadata[types.MetadataKeyCommitmentLineKind])
		billed[kind] = billed[kind].Add(line.Amount)
	}
	s.True(billed[types.CommitmentLineOverage].IsPositive(), "the prorated floor leaves overage: %v", billed)

	for _, groupBy := range [][]string{nil, {"source"}} {
		resp, err := s.analytics.GetDetailedAnalytics(s.GetContext(), &events.MeterUsageDetailedAnalyticsParams{
			TenantID:           types.GetTenantID(s.GetContext()),
			EnvironmentID:      types.GetEnvironmentID(s.GetContext()),
			ExternalCustomerID: s.customer.ExternalID,
			StartTime:          s.jan,
			EndTime:            end,
			GroupBy:            groupBy,
		})
		s.Require().NoError(err)

		total, utilized, trueUp, overage := decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
		for _, item := range resp.Items {
			total = total.Add(item.TotalCost)
			s.Require().NotNil(item.CommitmentInfo, "group_by %v", groupBy)
			utilized = utilized.Add(item.CommitmentInfo.ComputedCommitmentUtilizedAmount)
			trueUp = trueUp.Add(item.CommitmentInfo.ComputedTrueUpAmount)
			overage = overage.Add(item.CommitmentInfo.ComputedOverageAmount)
		}

		s.True(total.Equal(invoice.AmountDue), "group_by %v: total %s, invoice %s", groupBy, total, invoice.AmountDue)
		s.True(utilized.Equal(billed[types.CommitmentLineUtilized]), "group_by %v: utilized %s", groupBy, utilized)
		s.True(overage.Equal(billed[types.CommitmentLineOverage]), "group_by %v: overage %s", groupBy, overage)
		s.True(trueUp.Equal(billed[types.CommitmentLineTrueUp]), "group_by %v: true-up %s", groupBy, trueUp)
	}
}

// A month with no usage still shows the line, carrying the floor the invoice bills as true-up.
func (s *CommitmentE2ESuite) TestAnalytics_UnusedFloorStillShows() {
	tokens := s.planWith("analytics_unused", types.BILLING_PERIOD_MONTHLY, s.tokens, "0", tokenSlabs...)
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "150", s.jan, s.jan.AddDate(0, 6, 0)))

	for _, groupBy := range [][]string{nil, {"source"}} {
		resp := s.usageAnalytics(groupBy, nil, "")
		s.Require().Len(resp.Items, 1, "group_by %v", groupBy)
		s.True(resp.Items[0].TotalCost.Equal(dec("150")), "group_by %v: total %s", groupBy, resp.Items[0].TotalCost)
		s.True(resp.Items[0].CommitmentInfo.ComputedTrueUpAmount.Equal(dec("150")))
	}
}

// An open-ended floor shows in the month's analytics like a dated one, carrying the floor the invoice bills.
func (s *CommitmentE2ESuite) TestAnalytics_OpenEndedFloorShowsItsPeriodFloor() {
	tokens := s.planWith("analytics_open_ended", types.BILLING_PERIOD_MONTHLY, s.tokens, "0", tokenSlabs...)
	sub := s.subscribe(tokens, s.jan)
	req := floorRequest("Platform floor", tokens.ID, "150", s.jan, time.Time{})
	req.EndDate = nil
	s.commit(sub.ID, req)
	s.recordSourcedTokens()

	invoice := s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0))
	s.requireInvoice(invoice, "150",
		wantLine{types.CommitmentLineUtilized, "Platform floor", "300", "100"},
		wantLine{types.CommitmentLineTrueUp, "Platform floor (unused)", "1", "50"},
	)

	s.requireSourceCosts(s.usageAnalytics([]string{"source"}, nil, ""), invoice,
		map[string]string{"api": "30", "batch": "45", "console": "75"})
}

// A meter-scoped floor over a line versioned on 16 Jan shares its true-up between the two versions by what each
// used within it, so the versions sum to the invoice.
func (s *CommitmentE2ESuite) TestAnalytics_TrueUpSharedByWithinAcrossScopedLines() {
	slab := s.planWith("analytics_versioned", types.BILLING_PERIOD_MONTHLY, s.tokens, "0", tokenSlabs...)
	sub := s.subscribe(slab, s.jan)
	req := floorRequest("Platform floor", s.tokens.ID, "200", s.jan, s.jan.AddDate(0, 6, 0))
	req.Scope.EntityType = types.CommitmentScopeMeter
	s.commit(sub.ID, req)

	original := sub.LineItems[0]
	successor, err := s.subscriptions.UpdateSubscriptionLineItem(s.GetContext(), original.ID, dto.UpdateSubscriptionLineItemRequest{
		EffectiveFrom: lo.ToPtr(time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)),
		BillingModel:  types.BILLING_MODEL_FLAT_FEE,
		Amount:        lo.ToPtr(dec("1")),
	})
	s.Require().NoError(err)

	// The slab version's 150 tokens cost $62.50 and the flat version's 60 cost $60: $77.50 short of the floor.
	s.record(s.tokens, time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), "150")
	s.record(s.tokens, time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC), "60")
	invoice := s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0))
	s.True(invoice.AmountDue.Equal(dec("200")), "amount due %s", invoice.AmountDue)

	resp := s.usageAnalytics(nil, nil, "")
	byLine := lo.KeyBy(resp.Items, func(item dto.UsageAnalyticItem) string { return item.SubLineItemID })
	s.Require().Len(byLine, 2)
	s.True(resp.TotalCost.Equal(dec("200")), "analytics total %s", resp.TotalCost)
	for lineID, within := range map[string]string{original.ID: "62.5", successor.ID: "60"} {
		info := byLine[lineID].CommitmentInfo
		s.Require().NotNil(info, lineID)
		s.True(info.ComputedCommitmentUtilizedAmount.Equal(dec(within)), "%s within %s", lineID, info.ComputedCommitmentUtilizedAmount)
		share := dec("77.5").Mul(dec(within)).Div(dec("122.5"))
		s.True(info.ComputedTrueUpAmount.Sub(share).Abs().LessThan(dec("0.000001")), "%s true-up %s, want %s", lineID, info.ComputedTrueUpAmount, share)
	}
}

// Eight reserved GPUs a day on a daily MAX meter fed by two sources: each source takes its share of the window
// stack's within, true-up and overage by the GPU-days it reported.
func (s *CommitmentE2ESuite) TestAnalytics_GroupedDailyWindowSplitsTheInvoice() {
	gpus := s.planWith("analytics_gpus", types.BILLING_PERIOD_MONTHLY, s.gpus, "2")
	sub := s.subscribe(gpus, s.jan)
	req := floorRequest("Reserved GPUs", gpus.ID, "8", s.jan, s.jan.AddDate(0, 3, 0))
	req.Type, req.Measurement = types.COMMITMENT_TYPE_QUANTITY, types.CommitmentMeasurementWindow
	req.CommittedPrice = &dto.CommitmentPriceInput{Amount: lo.ToPtr(dec("1.5"))}
	s.commit(sub.ID, req)

	// 1-10 Jan the trainer runs 8 GPUs; 11-20 Jan are idle; 21-31 Jan it runs 6 while inference peaks at 10.
	for day := 1; day <= 31; day++ {
		at := time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC)
		switch {
		case day <= 10:
			s.recordFrom(s.gpus, "trainer", at, "8")
		case day >= 21:
			s.recordFrom(s.gpus, "trainer", at, "6")
			s.recordFrom(s.gpus, "inference", at.Add(time.Hour), "10")
		}
	}

	// The line peaks at 8 GPUs for 10 days and 10 for 11: 168 GPU-days used, 80 unused, 22 overage.
	invoice := s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0))
	s.requireInvoice(invoice, "416",
		wantLine{types.CommitmentLineUtilized, "Reserved GPUs", "168", "252"},
		wantLine{types.CommitmentLineTrueUp, "Reserved GPUs (unused)", "80", "120"},
		wantLine{types.CommitmentLineOverage, "GPUs", "22", "44"},
	)

	// The trainer reported 146 GPU-days and inference 110, so they split $416 as 146:110.
	s.requireSourceCosts(s.usageAnalytics([]string{"source"}, nil, ""), invoice,
		map[string]string{"trainer": "237.25", "inference": "178.75"})
}

// Console retains 150/300 of the $100 usage charge; the commitment floor stays excluded.
func (s *CommitmentE2ESuite) TestAnalytics_SourcesFilterSkipsCommitments() {
	tokens := s.planWith("analytics_filtered", types.BILLING_PERIOD_MONTHLY, s.tokens, "0", tokenSlabs...)
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "150", s.jan, s.jan.AddDate(0, 6, 0)))
	s.recordSourcedTokens()

	resp := s.usageAnalytics([]string{"source"}, []string{"console"}, "")
	s.Require().Len(resp.Items, 1)
	s.True(resp.Items[0].TotalCost.Equal(dec("50")), "console receives half the unfiltered usage charge, got %s", resp.Items[0].TotalCost)
	s.True(resp.TotalCost.Equal(resp.Items[0].TotalCost))
	s.Nil(resp.Items[0].CommitmentInfo)
}

// Each source's daily points share its amortized cost by usage, so they sum to it.
func (s *CommitmentE2ESuite) TestAnalytics_PointsSumToTheirSourceCost() {
	tokens := s.planWith("analytics_points", types.BILLING_PERIOD_MONTHLY, s.tokens, "0", tokenSlabs...)
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "150", s.jan, s.jan.AddDate(0, 6, 0)))
	s.recordSourcedTokens()

	resp := s.usageAnalytics([]string{"source"}, nil, types.WindowSizeDay)
	s.Require().Len(resp.Items, 3)
	for _, item := range resp.Items {
		s.Require().NotEmpty(item.Points, item.Source)
		pointsCost := decimal.Zero
		for _, p := range item.Points {
			pointsCost = pointsCost.Add(p.Cost)
		}
		s.True(pointsCost.Equal(item.TotalCost), "%s points sum to %s, item costs %s", item.Source, pointsCost, item.TotalCost)
	}
}

// A2: $1,200 for 2026 is one instalment on the invoice issued when the subscription opens. The months after it show
// usage within the commitment at $0, bill only the usage above it, and carry no true-up.
func (s *CommitmentE2ESuite) TestE2E_A2_Upfront() {
	end := s.jan.AddDate(1, 0, 0)
	tokens := s.planWith("a2", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan, termsRequest("Platform", tokens.ID, "100", s.jan, end, wholeValueUpfront(nil)))

	opening := s.openingInvoice(sub.ID)
	s.requireBilled(opening, "1200", wantLine{types.CommitmentLineInstalment, "Platform - Instalment 1 of 1", "1", "1200"})
	paid := s.billedInstalments(opening.ID)[0]
	s.True(paid.ServicePeriodStart.Equal(s.jan) && paid.ServicePeriodEnd.Equal(end), "the instalment pays for the whole year")

	// January uses $60 of the $100 a month, February $120 and March nothing.
	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "600")
	s.record(s.tokens, time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC), "1200")

	january := s.billPeriod(sub.ID, s.jan)
	s.Equal(types.InvoiceStatusSkipped, january.InvoiceStatus, "usage within the commitment bills nothing")
	s.requireBilled(january, "0", wantLine{types.CommitmentLineUtilized, "Platform (covered by commitment)", "600", "0"})

	s.requireBilled(s.billPeriod(sub.ID, s.jan.AddDate(0, 1, 0)), "20",
		wantLine{types.CommitmentLineUtilized, "Platform (covered by commitment)", "1000", "0"},
		wantLine{types.CommitmentLineOverage, "Tokens", "200", "20"},
	)
	s.requireBilled(s.billPeriod(sub.ID, s.jan.AddDate(0, 2, 0)), "0")

	s.requireFullyBilled(sub.ID, 1, "1200")
}

// A3: $1,000 over ten months is $300 on the opening invoice, then $70 on each month's invoice. February's usage
// is on the invoice that also bills February's instalment, at $0.
func (s *CommitmentE2ESuite) TestE2E_A3_PartialUpfront() {
	tokens := s.planWith("a3", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan, termsRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(0, 10, 0), upfrontPlusRemainder()...))

	s.requireBilled(s.openingInvoice(sub.ID), "300", wantLine{types.CommitmentLineInstalment, "Platform - Instalment 1 of 11", "1", "300"})

	s.record(s.tokens, time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC), "700")

	for month := 0; month < 10; month++ {
		want := []wantLine{{types.CommitmentLineInstalment, fmt.Sprintf("Platform - Instalment %d of 11", month+2), "1", "70"}}
		if month == 1 {
			want = append(want, wantLine{types.CommitmentLineUtilized, "Platform (covered by commitment)", "700", "0"})
		}

		s.requireBilled(s.billPeriod(sub.ID, s.jan.AddDate(0, month, 0)), "70", want...)
	}

	s.requireBilled(s.billPeriod(sub.ID, s.jan.AddDate(0, 10, 0)), "0")
	s.requireFullyBilled(sub.ID, 11, "1000")
}

// A4: $1,200 for 2026 in milestones of 30%, 50% and the rest. Each is billed on its own invoice: 360 on 1 Jan, 600
// on 1 Jul and 240 on the closing invoice of 1 Jan 2027.
func (s *CommitmentE2ESuite) TestE2E_A4_Milestones() {
	jul, end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), s.jan.AddDate(1, 0, 0)
	tokens := s.planWith("a4", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan, termsRequest("Platform", tokens.ID, "100", s.jan, end,
		dto.BillingTermInput{
			Kind:         types.BillingTermKindOnce,
			ValueType:    types.BillingTermValueFixed,
			Value:        lo.ToPtr(dec("360")),
			InvoiceLabel: lo.ToPtr("Signing"),
		},
		dto.BillingTermInput{
			Kind:         types.BillingTermKindOnce,
			ValueType:    types.BillingTermValueFixed,
			Value:        lo.ToPtr(dec("600")),
			BillOn:       lo.ToPtr(jul),
			InvoiceLabel: lo.ToPtr("Mid-year"),
		},
		dto.BillingTermInput{
			Kind:         types.BillingTermKindOnce,
			ValueType:    types.BillingTermValueRemainder,
			BillOn:       lo.ToPtr(end),
			InvoiceLabel: lo.ToPtr("Completion"),
		},
	))

	s.requireBilled(s.openingInvoice(sub.ID), "360", wantLine{types.CommitmentLineInstalment, "Platform - Signing", "1", "360"})

	for month := 0; month < 12; month++ {
		inv := s.billPeriod(sub.ID, s.jan.AddDate(0, month, 0))
		switch month {
		case 5:
			s.requireBilled(inv, "600", wantLine{types.CommitmentLineInstalment, "Platform - Mid-year", "1", "600"})
		case 11:
			s.requireBilled(inv, "240", wantLine{types.CommitmentLineInstalment, "Platform - Completion", "1", "240"})
		default:
			s.requireBilled(inv, "0")
		}
	}

	s.requireFullyBilled(sub.ID, 3, "1200")
}

// A6: $100 a month for 2026, with a $300 deposit. The deposit is billed on 1 Jan, the first three months bill only
// usage above the commitment, and from April each month's invoice carries its $100. The deposit's own label takes no
// number, so the monthly instalments count 1 to 9.
func (s *CommitmentE2ESuite) TestE2E_A6_Deposit() {
	apr, end := s.jan.AddDate(0, 3, 0), s.jan.AddDate(1, 0, 0)
	tokens := s.planWith("a6", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan, termsRequest("Platform", tokens.ID, "100", s.jan, end,
		dto.BillingTermInput{
			Kind:         types.BillingTermKindOnce,
			ValueType:    types.BillingTermValueFixed,
			Value:        lo.ToPtr(dec("300")),
			InvoiceLabel: lo.ToPtr("Deposit"),
		},
		dto.BillingTermInput{
			Kind:           types.BillingTermKindRecurring,
			ValueType:      types.BillingTermValueFixed,
			Value:          lo.ToPtr(dec("100")),
			StartDate:      lo.ToPtr(apr),
			InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear),
		},
	))

	opening := s.openingInvoice(sub.ID)
	s.requireBilled(opening, "300", wantLine{types.CommitmentLineInstalment, "Platform - Deposit", "1", "300"})
	deposit := s.billedInstalments(opening.ID)[0]
	s.Require().Len(sub.Commitments, 1)
	returned, ok := lo.Find(sub.Commitments[0].Instalments, func(i *billingterm.Instalment) bool { return i.ID == deposit.ID })
	s.Require().True(ok)
	s.Equal(types.InstalmentStatusInvoiced, returned.InstalmentStatus, "the response shows the deposit the opening invoice billed")
	s.True(deposit.ServicePeriodStart.Equal(s.jan) && deposit.ServicePeriodEnd.Equal(end), "a once instalment's service period is the term")

	// $80 of usage in January, $150 in February and $100 in April.
	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "800")
	s.record(s.tokens, time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC), "1500")
	s.record(s.tokens, time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC), "1000")

	s.requireBilled(s.billPeriod(sub.ID, s.jan), "0", wantLine{types.CommitmentLineUtilized, "Platform (covered by commitment)", "800", "0"})
	s.requireBilled(s.billPeriod(sub.ID, s.jan.AddDate(0, 1, 0)), "50",
		wantLine{types.CommitmentLineUtilized, "Platform (covered by commitment)", "1000", "0"},
		wantLine{types.CommitmentLineOverage, "Tokens", "500", "50"},
	)
	s.requireBilled(s.billPeriod(sub.ID, s.jan.AddDate(0, 2, 0)), "0")

	for month := 3; month < 12; month++ {
		want := []wantLine{{types.CommitmentLineInstalment, fmt.Sprintf("Platform - Instalment %d of 9", month-2), "1", "100"}}
		if month == 3 {
			want = append(want, wantLine{types.CommitmentLineUtilized, "Platform (covered by commitment)", "1000", "0"})
		}

		s.requireBilled(s.billPeriod(sub.ID, s.jan.AddDate(0, month, 0)), "100", want...)
	}

	s.requireFullyBilled(sub.ID, 10, "1200")
}

// A7: $1,200 for 2026 invoiced each quarter in advance. $300 is billed on the invoices issued on 1 Jan, 1 Apr, 1 Jul
// and 1 Oct, and on no other.
func (s *CommitmentE2ESuite) TestE2E_A7_QuarterlyAdvance() {
	end := s.jan.AddDate(1, 0, 0)
	tokens := s.planWith("a7", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan, termsRequest("Platform", tokens.ID, "100", s.jan, end, dto.BillingTermInput{
		Kind:           types.BillingTermKindRecurring,
		ValueType:      types.BillingTermValueFixed,
		Value:          lo.ToPtr(dec("300")),
		PeriodCount:    lo.ToPtr(3),
		InvoiceCadence: lo.ToPtr(types.InvoiceCadenceAdvance),
	}))

	s.record(s.tokens, time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC), "500")

	opening := s.openingInvoice(sub.ID)
	s.requireBilled(opening, "300", wantLine{types.CommitmentLineInstalment, "Platform - Instalment 1 of 4", "1", "300"})
	s.True(s.billedInstalments(opening.ID)[0].ServicePeriodEnd.Equal(s.jan.AddDate(0, 3, 0)), "the first instalment pays for the first quarter")

	// A quarter's instalment is billed on the invoice issued as the quarter opens, which closes the month before it.
	instalmentOf := map[int]int{2: 2, 5: 3, 8: 4}
	for month := 0; month < 12; month++ {
		inv := s.billPeriod(sub.ID, s.jan.AddDate(0, month, 0))
		n, billsInstalment := instalmentOf[month]
		switch {
		case billsInstalment:
			billed := s.billedInstalments(inv.ID)[0]
			s.True(billed.ServicePeriodStart.Equal(s.jan.AddDate(0, 3*(n-1), 0)), "instalment %d pays for the quarter it opens", n)
			s.requireBilled(inv, "300", wantLine{types.CommitmentLineInstalment, fmt.Sprintf("Platform - Instalment %d of 4", n), "1", "300"})
		case month == 3:
			s.requireBilled(inv, "0", wantLine{types.CommitmentLineUtilized, "Platform (covered by commitment)", "500", "0"})
		default:
			s.requireBilled(inv, "0")
		}
	}

	s.requireFullyBilled(sub.ID, 4, "1200")
}

// B1: a term starting on 25 Aug bills its $300 upfront on a one-off invoice of its own, and each monthly instalment on
// another, rather than on the 1 Sep subscription invoice.
func (s *CommitmentE2ESuite) TestE2E_B1_SeparateInvoice() {
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	start, end := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), aug.AddDate(0, 2, 0)

	tokens := s.planWith("b1", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, aug)

	// $310 a month is $70 for the 7 days of August the term covers, so the term is worth $380.
	req := termsRequest("Platform", tokens.ID, "310", start, end,
		dto.BillingTermInput{Kind: types.BillingTermKindOnce, ValueType: types.BillingTermValueFixed, Value: lo.ToPtr(dec("300"))},
		dto.BillingTermInput{Kind: types.BillingTermKindRecurring, ValueType: types.BillingTermValueRemainder, InvoiceCadence: lo.ToPtr(types.InvoiceCadenceArrear)},
	)
	req.InstalmentInvoicing = lo.ToPtr(types.InstalmentDeliverySeparateInvoice)
	committed := s.commit(sub.ID, req)
	s.Require().Len(committed.Instalments, 3)
	s.True(committed.Instalments[0].BillOn.Equal(start), "the upfront is due on the day the term starts")

	// 200 tokens before the term and 500 within it.
	s.record(s.tokens, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), "200")
	s.record(s.tokens, time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), "500")

	// The August invoice, issued on 1 Sep, carries no instalment although all three are due by then.
	s.requireBilled(s.billPeriod(sub.ID, aug), "20",
		wantLine{types.CommitmentLineUtilized, "Platform (covered by commitment)", "500", "0"},
		wantLine{types.CommitmentLinePlain, "Tokens", "200", "20"},
	)

	s.Require().NoError(s.commitments.BillDueInstalments(s.GetContext()))

	for i, want := range []struct{ amount, label string }{
		{"300", "Instalment 1 of 3"},
		{"40", "Instalment 2 of 3"},
		{"40", "Instalment 3 of 3"},
	} {
		billed := s.instalmentStatus(committed.Instalments[i].ID)
		s.Equal(types.InstalmentStatusInvoiced, billed.InstalmentStatus, want.label)

		inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), lo.FromPtr(billed.InvoiceID))
		s.Require().NoError(err)
		s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType, want.label)
		s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus, want.label)
		s.Equal(string(types.InvoiceBillingReasonBillingTerms), inv.BillingReason, want.label)
		s.requireBilled(inv, want.amount, wantLine{types.CommitmentLineInstalment, "Platform - " + want.label, "1", want.amount})
	}

	upfront := s.instalmentStatus(committed.Instalments[0].ID)
	s.True(upfront.ServicePeriodStart.Equal(start), "the upfront invoice pays for the term from 25 Aug")
	s.requireFullyBilled(sub.ID, 3, "380")
	s.Len(s.invoicesWithReason(sub.ID, types.InvoiceBillingReasonBillingTerms), 3)
}

// A1: a per_period commitment of $100 a month keeps billing as before: usage within it, a true-up for the shortfall
// and overage above it, with no instalments.
func (s *CommitmentE2ESuite) TestE2E_PerPeriodUnchanged() {
	tokens := s.planWith("a1", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	req := floorRequest("Platform", tokens.ID, "100", s.jan, s.jan.AddDate(1, 0, 0))
	req.CollectionMethod = lo.ToPtr(types.CommitmentCollectionPerPeriod)
	sub := s.subscribe(tokens, s.jan, req)

	// $60 of usage in January and $120 in February.
	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "600")
	s.record(s.tokens, time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC), "1200")

	january := s.billPeriod(sub.ID, s.jan)
	s.Equal(types.InvoiceStatusFinalized, january.InvoiceStatus)
	s.requireBilled(january, "100",
		wantLine{types.CommitmentLineUtilized, "Platform", "600", "60"},
		wantLine{types.CommitmentLineTrueUp, "Platform (unused)", "1", "40"},
	)
	s.requireBilled(s.billPeriod(sub.ID, s.jan.AddDate(0, 1, 0)), "120",
		wantLine{types.CommitmentLineUtilized, "Platform", "1000", "100"},
		wantLine{types.CommitmentLineOverage, "Tokens", "200", "20"},
	)

	for month := 2; month < 12; month++ {
		s.requireBilled(s.billPeriod(sub.ID, s.jan.AddDate(0, month, 0)), "100",
			wantLine{types.CommitmentLineTrueUp, "Platform (unused)", "1", "100"},
		)
	}

	instalments, err := s.GetStores().BillingInstalmentRepo.List(s.GetContext(), &billingterm.InstalmentFilter{SubscriptionIDs: []string{sub.ID}})
	s.Require().NoError(err)
	s.Empty(instalments)
}
