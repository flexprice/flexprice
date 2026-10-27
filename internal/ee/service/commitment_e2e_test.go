package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

// CommitmentE2ESuite drives commitments through the public subscription, commitment and billing services, from
// creation to the lines an invoice for the period carries. Usage is read from meter_usage, as invoicing reads it.
type CommitmentE2ESuite struct {
	testutil.BaseServiceTestSuite
	subscriptions SubscriptionService
	commitments   CommitmentService
	billing       BillingService
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
		EndDate:     end,
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

// A $100 monthly floor bills its true-up when usage falls short and on-demand usage when it runs over.
func (s *CommitmentE2ESuite) TestMonthlyAmountFloor_UnderThenOverUse() {
	tokens := s.planWith("monthly_floor", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "100", s.jan, s.jan.AddDate(0, 6, 0)))

	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "600")
	s.record(s.tokens, time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC), "1500")

	// January: 600 tokens cost $60, $40 short of the floor.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "100",
		wantLine{types.CommitmentLineWithin, "Platform floor", "600", "60"},
		wantLine{types.CommitmentLineTrueUp, "Platform floor (unused)", "1", "40"},
	)

	// February: 1,500 tokens cost $150; the floor covers $100 of it.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan.AddDate(0, 1, 0), s.jan.AddDate(0, 2, 0)), "150",
		wantLine{types.CommitmentLineWithin, "Platform floor", "1000", "100"},
		wantLine{types.CommitmentLineOnDemand, "Tokens", "500", "50"},
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
		wantLine{types.CommitmentLineWithin, "Reserved tokens", "1000", "80"},
		wantLine{types.CommitmentLineOnDemand, "Tokens", "300", "36"},
	)

	// February: 700 used, so 300 committed units are billed unused.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan.AddDate(0, 1, 0), s.jan.AddDate(0, 2, 0)), "80",
		wantLine{types.CommitmentLineWithin, "Reserved tokens", "700", "56"},
		wantLine{types.CommitmentLineTrueUp, "Reserved tokens (unused)", "300", "24"},
	)
}

// Eight GPUs a day: full days fill the window, idle days leave it unused, and GPUs above eight bill on demand
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
		wantLine{types.CommitmentLineWithin, "Reserved GPUs", "168", "252"},
		wantLine{types.CommitmentLineTrueUp, "Reserved GPUs (unused)", "80", "120"},
		wantLine{types.CommitmentLineOnDemand, "GPUs", "22", "44"},
	)
}

// A price edit on 16 Jan versions the scoped line onto a flat price on the same meter. The meter-scoped commitment
// keeps covering both versions, and each prices its own usage: on-demand units of the slab version bill at the
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
	// the flat version are on demand at $1.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "92.5",
		wantLine{types.CommitmentLineWithin, "Reserved tokens", "100", "20"},
		wantLine{types.CommitmentLineOnDemand, "Tokens", "50", "12.5"},
		wantLine{types.CommitmentLineOnDemand, "Tokens", "60", "60"},
	)
}

// A commitment added halfway through a quarter has a floor for the half it covers; usage before it starts bills
// on demand.
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
		wantLine{types.CommitmentLineWithin, "Quarterly floor", "300", "300"},
		wantLine{types.CommitmentLineTrueUp, "Quarterly floor (unused)", "1", "150"},
		wantLine{types.CommitmentLineOnDemand, "Tokens", "200", "200"},
	)
}

// A commitment sent with the subscription bills on the subscription's first invoice.
func (s *CommitmentE2ESuite) TestInlineCommitment_BillsOnTheFirstInvoice() {
	tokens := s.planWith("inline", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.05")
	sub := s.subscribe(tokens, s.jan, floorRequest("Launch floor", tokens.ID, "50", s.jan, s.jan.AddDate(0, 6, 0)))

	s.record(s.tokens, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), "600")

	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "50",
		wantLine{types.CommitmentLineWithin, "Launch floor", "600", "30"},
		wantLine{types.CommitmentLineTrueUp, "Launch floor (unused)", "1", "20"},
	)
}

// Terminated on 11 Jan with a $500 fee: January's floor shrinks to the ten days the commitment ran, usage after
// it bills on demand, and the fee is billed on its own one-off invoice.
func (s *CommitmentE2ESuite) TestTerminateMidPeriod_ClipsTheFloorAndBillsTheFee() {
	tokens := s.planWith("terminated", types.BILLING_PERIOD_MONTHLY, s.tokens, "0.10")
	sub := s.subscribe(tokens, s.jan)
	created := s.commit(sub.ID, floorRequest("Platform floor", tokens.ID, "310", s.jan, s.jan.AddDate(0, 6, 0)))

	terminatedAt := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
	terminated, err := s.commitments.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:     lo.ToPtr(terminatedAt),
		CancellationFee: lo.ToPtr(dec("500")),
	})
	s.Require().NoError(err)
	s.Require().NotNil(terminated.TerminationInvoiceID)

	fee, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), *terminated.TerminationInvoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeOneOff, fee.InvoiceType)
	s.Equal(string(types.InvoiceBillingReasonProration), fee.BillingReason)
	s.True(fee.AmountDue.Equal(dec("500")), "fee invoice %s", fee.AmountDue)
	feeLines, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), fee.ID)
	s.Require().NoError(err)
	s.Require().Len(feeLines, 1)
	s.Equal(string(types.CommitmentLineCancellationFee), feeLines[0].Metadata[types.MetadataKeyCommitmentLineKind])

	s.record(s.tokens, time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC), "400")
	s.record(s.tokens, time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC), "300")

	// $310 a month for 10 of 31 days is a $100 floor; $40 used before the end, $30 after.
	s.requireInvoice(s.invoiceFor(sub.ID, s.jan, s.jan.AddDate(0, 1, 0)), "130",
		wantLine{types.CommitmentLineWithin, "Platform floor", "400", "40"},
		wantLine{types.CommitmentLineTrueUp, "Platform floor (unused)", "1", "60"},
		wantLine{types.CommitmentLineOnDemand, "Tokens", "300", "30"},
	)
}
