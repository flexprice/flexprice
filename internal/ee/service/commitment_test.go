package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type commitmentFixtures struct {
	customer     *customer.Customer
	sub          *subscription.Subscription
	meterAPI     *meter.Meter
	meterStorage *meter.Meter
	priceAPI     *price.Price
	priceStorage *price.Price
	group        *group.Group
	lineAPI      *subscription.SubscriptionLineItem
	lineStorage  *subscription.SubscriptionLineItem

	groupAPIOnly           *group.Group
	priceOtherMeterInGroup *price.Price
}

// seedCommitmentFixtures stores a monthly subscription anchored 1 Jan 2027 with two usage prices in group grp_1,
// and enables commitments_v2.
func seedCommitmentFixtures(t *testing.T, ctx context.Context, stores testutil.Stores) commitmentFixtures {
	t.Helper()
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	fx := commitmentFixtures{}

	fx.customer = &customer.Customer{
		ID:         "cust_cmt",
		ExternalID: "ext_cust_cmt",
		Name:       "Commit Co",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.CustomerRepo.Create(ctx, fx.customer))

	fx.meterAPI = &meter.Meter{
		ID:        "meter_api",
		Name:      "GPUs",
		EventName: "gpu",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			Field:      "gpus",
			BucketSize: types.WindowSizeMinute,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}

	fx.meterStorage = &meter.Meter{
		ID:          "meter_storage",
		Name:        "Storage",
		EventName:   "storage",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "bytes"},
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.MeterRepo.CreateMeter(ctx, fx.meterAPI))
	require.NoError(t, stores.MeterRepo.CreateMeter(ctx, fx.meterStorage))

	fx.group = &group.Group{
		ID:            "grp_1",
		Name:          "Compute",
		EntityType:    types.GroupEntityTypePrice,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.GroupRepo.Create(ctx, fx.group))

	usagePrice := func(id, meterID string, amount decimal.Decimal) *price.Price {
		return &price.Price{
			ID:                 id,
			Amount:             amount,
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           "plan_cmt",
			Type:               types.PRICE_TYPE_USAGE,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			MeterID:            meterID,
			GroupID:            fx.group.ID,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}

	fx.priceAPI = usagePrice("price_api", fx.meterAPI.ID, dec("0.06"))
	fx.priceStorage = usagePrice("price_storage", fx.meterStorage.ID, dec("0.02"))
	require.NoError(t, stores.PriceRepo.Create(ctx, fx.priceAPI))
	require.NoError(t, stores.PriceRepo.Create(ctx, fx.priceStorage))

	fx.sub = &subscription.Subscription{
		ID:                 "subs_cmt",
		PlanID:             "plan_cmt",
		CustomerID:         fx.customer.ID,
		Currency:           "usd",
		StartDate:          jan,
		BillingAnchor:      jan,
		CurrentPeriodStart: jan,
		CurrentPeriodEnd:   jan.AddDate(0, 1, 0),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Timezone:           "UTC",
		SubscriptionStatus: types.SubscriptionStatusActive,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}

	line := func(id string, p *price.Price) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID:             id,
			SubscriptionID: fx.sub.ID,
			CustomerID:     fx.customer.ID,
			EntityID:       "plan_cmt",
			EntityType:     types.SubscriptionLineItemEntityTypePlan,
			PriceID:        p.ID,
			PriceType:      p.Type,
			MeterID:        p.MeterID,
			DisplayName:    p.ID,
			Quantity:       decimal.Zero,
			Currency:       "usd",
			BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
			InvoiceCadence: types.InvoiceCadenceArrear,
			StartDate:      jan,
			EnvironmentID:  types.GetEnvironmentID(ctx),
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
	}

	fx.lineAPI = line("subs_line_api", fx.priceAPI)
	fx.lineStorage = line("subs_line_storage", fx.priceStorage)
	require.NoError(t, stores.SubscriptionRepo.CreateWithLineItems(ctx, fx.sub,
		[]*subscription.SubscriptionLineItem{fx.lineAPI, fx.lineStorage}))

	fx.groupAPIOnly = &group.Group{
		ID:            "grp_api_only",
		Name:          "API only",
		EntityType:    types.GroupEntityTypePrice,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.GroupRepo.Create(ctx, fx.groupAPIOnly))
	// price_api_only moves price_api's twin into the API-only group; the other-meter price joins it unattached.
	apiOnly := usagePrice("price_api_only", fx.meterAPI.ID, dec("0.06"))
	apiOnly.GroupID = fx.groupAPIOnly.ID
	fx.priceOtherMeterInGroup = usagePrice("price_other_meter", fx.meterStorage.ID, dec("0.02"))
	fx.priceOtherMeterInGroup.GroupID = fx.groupAPIOnly.ID
	require.NoError(t, stores.PriceRepo.Create(ctx, apiOnly))
	require.NoError(t, stores.PriceRepo.Create(ctx, fx.priceOtherMeterInGroup))
	apiOnlyLine := line("subs_line_api_only", apiOnly)
	require.NoError(t, stores.SubscriptionLineItemRepo.Create(ctx, apiOnlyLine))

	setCommitmentsV2(t, ctx, stores, true)
	return fx
}

func setCommitmentsV2(t *testing.T, ctx context.Context, stores testutil.Stores, enabled bool) {
	t.Helper()
	if _, err := stores.SettingsRepo.GetByKey(ctx, types.SettingKeyCommitmentsV2); err == nil {
		require.NoError(t, stores.SettingsRepo.DeleteByKey(ctx, types.SettingKeyCommitmentsV2))
	}

	setting := &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("setting"),
		Key:           types.SettingKeyCommitmentsV2,
		Value:         map[string]interface{}{"enabled": enabled},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	require.NoError(t, stores.SettingsRepo.Create(ctx, setting))
}

type CommitmentServiceSuite struct {
	testutil.BaseServiceTestSuite
	svc CommitmentService
	fx  commitmentFixtures
}

func TestCommitmentServiceSuite(t *testing.T) { suite.Run(t, new(CommitmentServiceSuite)) }

func (s *CommitmentServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewCommitmentService(s.fullParams())
	s.fx = seedCommitmentFixtures(s.T(), s.GetContext(), s.GetStores())
}

func (s *CommitmentServiceSuite) TestCreate_ComputesNoStoredTotalForPerPeriod() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	s.Nil(resp.TotalAmount)
	s.Equal("cmt_", resp.ID[:4])
	s.True(resp.CommitmentValue.Equal(dec("4.608")), "got %s", resp.CommitmentValue) // 8 × 0.048 × 12 periods
	s.Equal(types.CommitmentCollectionPerPeriod, lo.FromPtr(resp.CollectionMethod))
	s.Equal("Base", resp.InvoiceLabel)
	s.Equal(s.fx.customer.ID, resp.CustomerID)
	s.Nil(resp.OveragePriceID)
}

func (s *CommitmentServiceSuite) TestCreate_CommittedAndOveragePricesAreSubscriptionScoped() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.OverageRate = lo.ToPtr(dec("0.07"))
	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	for id, want := range map[string]decimal.Decimal{
		*resp.CommittedPriceID: dec("0.048"),
		*resp.OveragePriceID:   dec("0.07"),
	} {
		p, err := s.GetStores().PriceRepo.Get(s.GetContext(), id)
		s.Require().NoError(err)
		s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, p.EntityType)
		s.Equal(s.fx.sub.ID, p.EntityID)
		s.Equal(s.fx.meterAPI.ID, p.MeterID)
		s.Equal(types.PRICE_TYPE_USAGE, p.Type)
		s.Equal("usd", p.Currency)
		s.True(p.Amount.Equal(want), "price %s amount %s", id, p.Amount)
	}
}

func (s *CommitmentServiceSuite) TestCreate_AmountCommitmentHasNoPrices() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Type, req.CommittedRate, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.group.ID}
	resp, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	s.Nil(resp.CommittedPriceID)
	s.Nil(resp.OveragePriceID)
	s.True(resp.CommitmentValue.Equal(decimal.NewFromInt(1200)), "got %s", resp.CommitmentValue)
}

func (s *CommitmentServiceSuite) TestCreate_RejectedWhenDisabled() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	enabled, err := s.svc.IsEnabled(s.GetContext())
	s.Require().NoError(err)
	s.False(enabled)
}

func (s *CommitmentServiceSuite) TestCreate_IdempotencyKeyReturnsExisting() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.IdempotencyKey = lo.ToPtr("contract-1")
	a, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	b, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	s.Equal(a.ID, b.ID)
	n, err := s.GetStores().CommitmentRepo.Count(s.GetContext(), commitment.NewNoLimitFilter())
	s.Require().NoError(err)
	s.Equal(1, n)
}

func (s *CommitmentServiceSuite) TestCreateLocked_ReplayedKeyReturnsOriginalWithoutWrites() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.IdempotencyKey = lo.ToPtr("contract-2")
	first, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	pricesBefore, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)

	got, err := s.svc.(*commitmentService).createLocked(s.GetContext(), &req, commitmentCreateOptions{})
	s.Require().NoError(err)
	s.Equal(first.ID, got.ID)
	pricesAfter, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	s.Equal(pricesBefore, pricesAfter)
}

func (s *CommitmentServiceSuite) TestPreview_RejectedWhenDisabled() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	_, err := s.svc.Preview(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestPreview_DoesNotPersist() {
	before, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	resp, err := s.svc.Preview(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	s.True(resp.CommitmentValue.Equal(dec("4.608")), "got %s", resp.CommitmentValue)
	n, err := s.GetStores().CommitmentRepo.Count(s.GetContext(), commitment.NewNoLimitFilter())
	s.Require().NoError(err)
	s.Zero(n)
	prices, err := s.GetStores().PriceRepo.Count(s.GetContext(), types.NewNoLimitPriceFilter())
	s.Require().NoError(err)
	s.Equal(before, prices)
}

func (s *CommitmentServiceSuite) TestGetAndList() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(created.ID, got.ID)
	s.True(got.CommitmentValue.Equal(dec("4.608")), "got %s", got.CommitmentValue)

	f := commitment.NewDefaultFilter()
	f.SubscriptionIDs = []string{s.fx.sub.ID}
	list, err := s.svc.List(s.GetContext(), f)
	s.Require().NoError(err)
	s.Require().Len(list.Items, 1)
	s.Equal(1, list.Pagination.Total)
	s.True(list.Items[0].CommitmentValue.Equal(dec("4.608")))

	_, err = s.svc.Get(s.GetContext(), "cmt_missing")
	s.True(ierr.IsNotFound(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestUpdate_OnlyLabelsAndMetadata() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	updated, err := s.svc.Update(s.GetContext(), created.ID, dto.UpdateCommitmentRequest{
		InvoiceLabel: lo.ToPtr("8× H100"), Metadata: types.Metadata{"contract": "c-1"},
	})
	s.Require().NoError(err)
	s.Equal("8× H100", updated.InvoiceLabel)
	s.Equal("Base", updated.Name)
	s.Equal("c-1", updated.Metadata["contract"])
	s.True(updated.Value.Equal(created.Value))

	_, err = s.svc.Update(s.GetContext(), created.ID, dto.UpdateCommitmentRequest{Name: lo.ToPtr("")})
	s.True(ierr.IsValidation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestTerminate_SetsStatusAndTerminatedAt() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, 4, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, resp.CommitmentStatus)
	s.True(resp.TerminatedAt.Equal(at))
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Error(err)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestTerminate_RejectsEffectiveAtAfterEnd() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.EndDate.AddDate(0, 1, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.True(ierr.IsValidation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestTerminate_KeepsRequestedDateBeforeStart() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, -1, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)
	s.True(resp.TerminatedAt.Equal(at), "got %s", resp.TerminatedAt)
	s.True(resp.CommitmentValue.IsPositive(), "value is still reported for a commitment terminated before start")
}

func (s *CommitmentServiceSuite) TestTerminate_RejectsInvalidCancellationFee() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	midTerm := created.StartDate.AddDate(0, 3, 0)
	tests := []struct {
		name string
		at   time.Time
		fee  dto.CommitmentCancellationFee
	}{
		{
			name: "unknown type",
			at:   midTerm,
			fee: dto.CommitmentCancellationFee{
				Type:  "percent",
				Value: decimal.NewFromInt(10),
			},
		},
		{
			name: "missing type",
			at:   midTerm,
			fee: dto.CommitmentCancellationFee{
				Value: decimal.NewFromInt(10),
			},
		},
		{
			name: "zero value",
			at:   midTerm,
			fee: dto.CommitmentCancellationFee{
				Type:  types.CommitmentCancellationFeeFlat,
				Value: decimal.Zero,
			},
		},
		{
			name: "negative value",
			at:   midTerm,
			fee: dto.CommitmentCancellationFee{
				Type:  types.CommitmentCancellationFeeFlat,
				Value: decimal.NewFromInt(-5),
			},
		},
		{
			name: "percent above 100",
			at:   midTerm,
			fee: dto.CommitmentCancellationFee{
				Type:  types.CommitmentCancellationFeePercentOfRemaining,
				Value: dec("100.01"),
			},
		},
		{
			name: "terminate at end_date",
			at:   created.EndDate,
			fee: dto.CommitmentCancellationFee{
				Type:  types.CommitmentCancellationFeeFlat,
				Value: decimal.NewFromInt(10),
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
				EffectiveAt:     &tt.at,
				CancellationFee: &tt.fee,
			})
			s.True(ierr.IsValidation(err), "got %v", err)
		})
	}

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusActive, got.CommitmentStatus)
}

// createChargeable creates a binding quantity commitment of 100 units/month at 1.00 for 12 months from 1 Jan 2027,
// so 1,200.00 in total and 100.00 earned per month.
func (s *CommitmentServiceSuite) createChargeable(binding bool) *dto.CommitmentResponse {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Value, req.CommittedRate, req.Binding = decimal.NewFromInt(100), lo.ToPtr(dec("1")), lo.ToPtr(binding)
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	return created
}

func (s *CommitmentServiceSuite) invoiceLines(invoiceID string) []*invoice.InvoiceLineItem {
	lines, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(s.GetContext(), invoiceID)
	s.Require().NoError(err)
	return lines
}

func (s *CommitmentServiceSuite) requireLine(line *invoice.InvoiceLineItem, kind types.CommitmentLineKind, amount, commitmentID string) {
	s.Equal(string(kind), line.Metadata[types.MetadataKeyCommitmentLineKind])
	s.Equal(commitmentID, line.Metadata[types.MetadataKeyCommitmentID])
	s.Equal(commitmentID, lo.FromPtr(line.CommitmentID))
	s.Equal(commitmentID, lo.FromPtr(line.EntityID))
	s.Equal(string(types.InvoiceLineItemEntityTypeCommitment), lo.FromPtr(line.EntityType))
	s.True(line.Quantity.Equal(decimal.NewFromInt(1)), "quantity %s", line.Quantity)
	s.True(line.Amount.Equal(dec(amount)), "amount %s, want %s", line.Amount, amount)
}

func (s *CommitmentServiceSuite) TestTerminate_FlatFeeBillsOneOffInvoice() {
	created := s.createChargeable(true)
	at := created.StartDate.AddDate(0, 3, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:     &at,
		CancellationFee: &dto.CommitmentCancellationFee{Type: types.CommitmentCancellationFeeFlat, Value: dec("250.5")},
	})
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, resp.CommitmentStatus)
	s.Require().NotNil(resp.TerminationInvoiceID)

	inv, err := s.GetStores().InvoiceRepo.Get(s.GetContext(), *resp.TerminationInvoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceTypeOneOff, inv.InvoiceType)
	s.Equal(string(types.InvoiceBillingReasonProration), inv.BillingReason)
	s.Equal(s.fx.customer.ID, inv.CustomerID)
	s.True(inv.AmountDue.Equal(dec("250.5")), "got %s", inv.AmountDue)
	lines := s.invoiceLines(inv.ID)
	s.Require().Len(lines, 1)
	s.requireLine(lines[0], types.CommitmentLineCancellationFee, "250.5", created.ID)
	s.Equal(created.InvoiceLabel+" (cancellation fee)", lo.FromPtr(lines[0].DisplayName))
}

func (s *CommitmentServiceSuite) TestTerminate_PercentFeeAppliesToUnearnedValue() {
	created := s.createChargeable(true)
	// 3 of 12 monthly periods earned: 900.00 unearned, so 10% is 90.00.
	at := created.StartDate.AddDate(0, 3, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt: &at,
		CancellationFee: &dto.CommitmentCancellationFee{
			Type:  types.CommitmentCancellationFeePercentOfRemaining,
			Value: decimal.NewFromInt(10),
		},
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp.TerminationInvoiceID)
	lines := s.invoiceLines(*resp.TerminationInvoiceID)
	s.Require().Len(lines, 1)
	s.requireLine(lines[0], types.CommitmentLineCancellationFee, "90", created.ID)
}

// billRemaining terminates with bill_remaining and returns the single settlement line.
func (s *CommitmentServiceSuite) billRemaining(created *dto.CommitmentResponse, at time.Time) *invoice.InvoiceLineItem {
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:   &at,
		BillRemaining: true,
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp.TerminationInvoiceID)
	lines := s.invoiceLines(*resp.TerminationInvoiceID)
	s.Require().Len(lines, 1)
	s.Equal(created.InvoiceLabel+" (remaining commitment)", lo.FromPtr(lines[0].DisplayName))
	return lines[0]
}

func (s *CommitmentServiceSuite) TestTerminate_BillRemainingOnPeriodBoundary() {
	created := s.createChargeable(true)
	line := s.billRemaining(created, created.StartDate.AddDate(0, 3, 0))
	s.requireLine(line, types.CommitmentLineSettlement, "900", created.ID)
}

func (s *CommitmentServiceSuite) TestTerminate_BillRemainingProratesMidPeriod() {
	created := s.createChargeable(true)
	// January has 31 days: 15/31 of 100.00 is earned, so 1200 - 48.3871 = 1151.61.
	line := s.billRemaining(created, created.StartDate.AddDate(0, 0, 15))
	s.requireLine(line, types.CommitmentLineSettlement, "1151.61", created.ID)
}

func (s *CommitmentServiceSuite) TestTerminate_BillRemainingBeforeTermStartBillsFullValue() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Value, req.CommittedRate = decimal.NewFromInt(100), lo.ToPtr(dec("1"))
	req.StartDate = req.StartDate.AddDate(0, 2, 0)
	req.EndDate = req.StartDate.AddDate(1, 0, 0)
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	line := s.billRemaining(created, created.StartDate.AddDate(0, -1, 0))
	s.requireLine(line, types.CommitmentLineSettlement, "1200", created.ID)
}

func (s *CommitmentServiceSuite) TestTerminate_FeeAndBillRemainingShareOneInvoice() {
	created := s.createChargeable(true)
	at := created.StartDate.AddDate(0, 3, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:     &at,
		CancellationFee: &dto.CommitmentCancellationFee{Type: types.CommitmentCancellationFeeFlat, Value: decimal.NewFromInt(50)},
		BillRemaining:   true,
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp.TerminationInvoiceID)

	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), types.NewNoLimitInvoiceFilter())
	s.Require().NoError(err)
	s.Require().Len(invoices, 1)
	s.True(invoices[0].AmountDue.Equal(decimal.NewFromInt(950)), "got %s", invoices[0].AmountDue)

	lines := s.invoiceLines(*resp.TerminationInvoiceID)
	s.Require().Len(lines, 2)
	byKind := lo.KeyBy(lines, func(l *invoice.InvoiceLineItem) string { return l.Metadata[types.MetadataKeyCommitmentLineKind] })
	s.requireLine(byKind[string(types.CommitmentLineCancellationFee)], types.CommitmentLineCancellationFee, "50", created.ID)
	s.requireLine(byKind[string(types.CommitmentLineSettlement)], types.CommitmentLineSettlement, "900", created.ID)
}

func (s *CommitmentServiceSuite) TestTerminate_WithoutChargesCreatesNoInvoice() {
	created := s.createChargeable(true)
	at := created.StartDate.AddDate(0, 3, 0)
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)
	s.Nil(resp.TerminationInvoiceID)

	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), types.NewNoLimitInvoiceFilter())
	s.Require().NoError(err)
	s.Empty(invoices)
}

func (s *CommitmentServiceSuite) TestTerminate_ChargesRequireBindingCommitment() {
	created := s.createChargeable(false)
	at := created.StartDate.AddDate(0, 3, 0)
	for name, req := range map[string]dto.TerminateCommitmentRequest{
		"fee": {
			EffectiveAt:     &at,
			CancellationFee: &dto.CommitmentCancellationFee{Type: types.CommitmentCancellationFeeFlat, Value: decimal.NewFromInt(5)},
		},
		"bill remaining": {EffectiveAt: &at, BillRemaining: true},
	} {
		s.Run(name, func() {
			_, err := s.svc.Terminate(s.GetContext(), created.ID, req)
			s.requireValidation(err, "binding")
		})
	}

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusActive, got.CommitmentStatus)
}

func (s *CommitmentServiceSuite) TestTerminate_ChargesCannotReachBackIntoInvoicedPeriods() {
	created := s.createChargeable(true)
	sub := *s.fx.sub
	sub.CurrentPeriodStart = created.StartDate.AddDate(0, 2, 0)
	sub.CurrentPeriodEnd = created.StartDate.AddDate(0, 3, 0)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), &sub))

	at := created.StartDate.AddDate(0, 1, 14)
	_, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at, BillRemaining: true})
	s.requireValidation(err, "current_period_start")

	// Without a charge only the end_date bound applies.
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, resp.CommitmentStatus)
}

func (s *CommitmentServiceSuite) TestTerminate_ChargesAtCurrentPeriodStartAreAllowed() {
	created := s.createChargeable(true)
	at := s.fx.sub.CurrentPeriodStart
	resp, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at, BillRemaining: true})
	s.Require().NoError(err)
	s.Require().NotNil(resp.TerminationInvoiceID)
}

func (s *CommitmentServiceSuite) TestTerminate_ChargesRejectedAtOrAfterEnd() {
	created := s.createChargeable(true)
	at := created.EndDate
	_, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at, BillRemaining: true})
	s.True(ierr.IsValidation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestTerminate_SecondTerminateBillsNothing() {
	created := s.createChargeable(true)
	at := created.StartDate.AddDate(0, 3, 0)
	req := dto.TerminateCommitmentRequest{EffectiveAt: &at, BillRemaining: true}
	_, err := s.svc.Terminate(s.GetContext(), created.ID, req)
	s.Require().NoError(err)

	_, err = s.svc.Terminate(s.GetContext(), created.ID, req)
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), types.NewNoLimitInvoiceFilter())
	s.Require().NoError(err)
	s.Len(invoices, 1)
}

func (s *CommitmentServiceSuite) TestTerminate_RejectedWhenDisabled() {
	created := s.createChargeable(true)
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	at := created.StartDate.AddDate(0, 3, 0)
	_, err := s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.True(ierr.IsInvalidOperation(err), "got %v", err)
}

// failingInvoiceRepo rejects every invoice insert.
type failingInvoiceRepo struct{ invoice.Repository }

func (failingInvoiceRepo) Create(context.Context, *invoice.Invoice) error {
	return ierr.NewError("invoice store unavailable").Mark(ierr.ErrDatabase)
}

func (failingInvoiceRepo) CreateWithLineItems(context.Context, *invoice.Invoice) error {
	return ierr.NewError("invoice store unavailable").Mark(ierr.ErrDatabase)
}

// The in-memory DB has no rollback, so the invoice is created before the status change: a failure leaves the
// commitment active here, and the transaction guarantees the same against a real database.
func (s *CommitmentServiceSuite) TestTerminate_InvoiceFailureLeavesCommitmentActive() {
	created := s.createChargeable(true)
	params := s.fullParams()
	params.InvoiceRepo = failingInvoiceRepo{params.InvoiceRepo}

	at := created.StartDate.AddDate(0, 3, 0)
	_, err := NewCommitmentService(params).Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{
		EffectiveAt:   &at,
		BillRemaining: true,
	})
	s.Require().Error(err)

	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusActive, got.CommitmentStatus)
	s.Nil(got.TerminatedAt)
}

func (s *CommitmentServiceSuite) TestTerminateForSubscription_KeepsRequestedDateAndIsIdempotent() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, -1, 0)
	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, at))
	first, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.True(first.TerminatedAt.Equal(at), "got %s", first.TerminatedAt)

	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, at.AddDate(0, 2, 0)))
	second, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, second.CommitmentStatus)
	s.True(second.TerminatedAt.Equal(*first.TerminatedAt))
}

func (s *CommitmentServiceSuite) TestTerminateForSubscription_TerminatesActiveCommitments() {
	a, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	req := validReq(s.fx.sub.ID, s.fx.priceStorage.ID)
	b, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	at := a.StartDate.AddDate(0, 2, 0)
	s.Require().NoError(s.svc.TerminateForSubscription(s.GetContext(), s.fx.sub.ID, at))
	for _, id := range []string{a.ID, b.ID} {
		got, err := s.svc.Get(s.GetContext(), id)
		s.Require().NoError(err)
		s.Equal(types.CommitmentStatusTerminated, got.CommitmentStatus)
		s.True(got.TerminatedAt.Equal(at))
	}
}

func (s *CommitmentServiceSuite) TestActiveForPeriod_IncludesTerminated() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, 4, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)

	got, err := s.svc.ActiveForPeriod(s.GetContext(), s.fx.sub.ID, at.AddDate(0, -1, 0), at)
	s.Require().NoError(err)
	s.Require().Len(got, 1)
	s.Equal(created.ID, got[0].ID)

	got, err = s.svc.ActiveForPeriod(s.GetContext(), "subs_other", at.AddDate(0, -1, 0), at)
	s.Require().NoError(err)
	s.Empty(got)
}

// Validation rules that need stored subscription data.

func (s *CommitmentServiceSuite) requireValidation(err error, contains ...string) {
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "want validation error, got %v", err)
	for _, c := range contains {
		s.Contains(err.Error(), c)
	}
}

func (s *CommitmentServiceSuite) TestValidation_V1_EndDateNotOnBillingBoundary() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.EndDate = time.Date(2027, 4, 23, 0, 0, 0, 0, time.UTC)
	_, err := s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "1 Apr 2027", "1 May 2027")
}

func (s *CommitmentServiceSuite) TestValidation_V2_QuantityOverGroupWithTwoMeters() {
	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.group.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "exactly one meter")
}

func (s *CommitmentServiceSuite) TestValidation_V3_AmountAndQuantityOverlap() {
	amount := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	amount.Type, amount.CommittedRate, amount.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	_, err := s.svc.Create(s.GetContext(), amount)
	s.Require().NoError(err)

	qty := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	qty.Priority = 1
	_, err = s.svc.Create(s.GetContext(), qty)
	s.requireValidation(err, "different type or measurement")
}

func (s *CommitmentServiceSuite) TestValidation_V4_SamePriorityOverlap() {
	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	_, err = s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "priority 0")
}

func (s *CommitmentServiceSuite) TestValidation_V5_LegacyLineItemCommitment() {
	li := s.fx.lineAPI
	li.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(50))
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(s.GetContext(), li))
	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "legacy line-item commitment")
}

func (s *CommitmentServiceSuite) TestValidation_V18_DifferentOverageRates() {
	a := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	a.OverageRate = lo.ToPtr(dec("0.07"))
	_, err := s.svc.Create(s.GetContext(), a)
	s.Require().NoError(err)

	b := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	b.Priority, b.OverageRate = 1, lo.ToPtr(dec("0.08"))
	_, err = s.svc.Create(s.GetContext(), b)
	s.requireValidation(err, "different overage rate")

	b.OverageRate = lo.ToPtr(dec("0.07"))
	_, err = s.svc.Create(s.GetContext(), b)
	s.Require().NoError(err)
}

func (s *CommitmentServiceSuite) TestValidation_C16_SubscriptionScopeWithLegacyCommitment() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(500))
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID)
	req.Type, req.CommittedRate, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeSubscription, EntityID: s.fx.sub.ID}
	_, err = s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "legacy subscription-level commitment")
}

func (s *CommitmentServiceSuite) fullParams() ServiceParams {
	st := s.GetStores()
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      st.SubscriptionRepo,
		SubscriptionLineItemRepo:     st.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        st.SubscriptionPhaseRepo,
		SubScheduleRepo:              st.SubscriptionScheduleRepo,
		PlanRepo:                     st.PlanRepo,
		PriceRepo:                    st.PriceRepo,
		PriceUnitRepo:                st.PriceUnitRepo,
		MeterRepo:                    st.MeterRepo,
		CustomerRepo:                 st.CustomerRepo,
		InvoiceRepo:                  st.InvoiceRepo,
		InvoiceLineItemRepo:          st.InvoiceLineItemRepo,
		HostedInvoiceTokenRepo:       st.HostedInvoiceTokenRepo,
		EntitlementRepo:              st.EntitlementRepo,
		FeatureRepo:                  st.FeatureRepo,
		AddonRepo:                    st.AddonRepo,
		AddonAssociationRepo:         st.AddonAssociationRepo,
		CreditGrantRepo:              st.CreditGrantRepo,
		CreditGrantApplicationRepo:   st.CreditGrantApplicationRepo,
		GroupRepo:                    st.GroupRepo,
		SettingsRepo:                 st.SettingsRepo,
		AddonPriceSyncRepo:           st.AddonPriceSyncRepo,
		CommitmentRepo:               st.CommitmentRepo,
		TaxAssociationRepo:           st.TaxAssociationRepo,
		TaxRateRepo:                  st.TaxRateRepo,
		TaxAppliedRepo:               st.TaxAppliedRepo,
		WalletRepo:                   st.WalletRepo,
		CouponAssociationRepo:        st.CouponAssociationRepo,
		ConnectionRepo:               st.ConnectionRepo,
		EntityIntegrationMappingRepo: st.EntityIntegrationMappingRepo,
		MeterUsageRepo:               st.MeterUsageRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
	}
}

func (s *CommitmentServiceSuite) TestCancelSubscription_TerminatesCommitments() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	subSvc := NewSubscriptionService(s.fullParams())
	at := created.StartDate.AddDate(0, 2, 0)
	s.Require().NoError(subSvc.TerminateSubscriptionResources(s.GetContext(), dto.TerminateSubscriptionResourcesRequest{
		SubscriptionID: s.fx.sub.ID, EffectiveDate: at,
	}))
	got, err := s.svc.Get(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Equal(types.CommitmentStatusTerminated, got.CommitmentStatus)
}

func (s *CommitmentServiceSuite) TestTerminateResources_NoopWhenDisabled() {
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	subSvc := NewSubscriptionService(s.fullParams())
	s.Require().NoError(subSvc.TerminateSubscriptionResources(s.GetContext(), dto.TerminateSubscriptionResourcesRequest{
		SubscriptionID: s.fx.sub.ID, EffectiveDate: s.fx.sub.StartDate.AddDate(0, 2, 0),
	}))
}

func (s *CommitmentServiceSuite) TestAttachPrice_RejectsSecondMeterInQuantityGroupScope() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
	subSvc := NewSubscriptionService(s.fullParams())
	_, err = subSvc.AddSubscriptionLineItem(
		s.GetContext(),
		s.fx.sub.ID,
		dto.CreateSubscriptionLineItemRequest{PriceID: s.fx.priceOtherMeterInGroup.ID},
	)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_NoopCases() {
	sub, p := s.fx.sub, s.fx.priceOtherMeterInGroup
	at := sub.StartDate
	// No commitments yet.
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), sub, p, at))

	req := validReq(sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	// A price outside the scoped group, and a fixed price, never trigger the check.
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), sub, s.fx.priceStorage, at))
	fixed := *p
	fixed.Type = types.PRICE_TYPE_FIXED
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), sub, &fixed, at))

	// Disabled feature is a no-op.
	setCommitmentsV2(s.T(), s.GetContext(), s.GetStores(), false)
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), sub, p, at))
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_RejectsBucketMismatchForMeterScopedWindow() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterAPI.ID}
	req.Measurement = types.CommitmentMeasurementWindow
	created, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	hourly := *s.fx.priceAPI
	hourly.ID, hourly.BucketSize = "price_api_hourly", types.WindowSizeHour
	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, &hourly, s.fx.sub.StartDate)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Contains(err.Error(), created.ID)

	same := *s.fx.priceAPI
	same.ID = "price_api_same_bucket"
	s.Require().NoError(s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, &same, s.fx.sub.StartDate))
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_RejectsOverlappingCommitmentsThatDisagree() {
	a := validReq(s.fx.sub.ID, "")
	a.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	a.Type, a.CommittedRate, a.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	ca, err := s.svc.Create(s.GetContext(), a)
	s.Require().NoError(err)

	b := validReq(s.fx.sub.ID, "")
	b.Name = "Storage"
	b.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeMeter, EntityID: s.fx.meterStorage.ID}
	cb, err := s.svc.Create(s.GetContext(), b)
	s.Require().NoError(err)

	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, s.fx.priceOtherMeterInGroup, s.fx.sub.StartDate)
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "got %v", err)
	s.Contains(err.Error(), ca.ID)
	s.Contains(err.Error(), cb.ID)
}

// recordingDB and recordingCommitmentRepo log the order of transactions, locks and reads.
type recordingDB struct {
	postgres.IClient
	events *[]string
}

func (d *recordingDB) WithTx(ctx context.Context, fn func(context.Context) error) error {
	*d.events = append(*d.events, "tx")
	return d.IClient.WithTx(ctx, fn)
}

func (d *recordingDB) LockWithWait(ctx context.Context, req postgres.LockRequest) error {
	*d.events = append(*d.events, "lock:"+req.Key)
	return d.IClient.LockWithWait(ctx, req)
}

type recordingCommitmentRepo struct {
	commitment.Repository
	events *[]string
}

func (r *recordingCommitmentRepo) Get(ctx context.Context, id string) (*commitment.Commitment, error) {
	*r.events = append(*r.events, "get")
	return r.Repository.Get(ctx, id)
}

func (s *CommitmentServiceSuite) TestUpdate_ReadsUnderCommitmentLockInTx() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)

	var events []string
	stores := s.GetStores()
	params := ServiceParams{
		Logger:         s.GetLogger(),
		Config:         s.GetConfig(),
		DB:             &recordingDB{IClient: s.GetDB(), events: &events},
		CommitmentRepo: &recordingCommitmentRepo{Repository: stores.CommitmentRepo, events: &events},
		SubRepo:        stores.SubscriptionRepo,
		PriceRepo:      stores.PriceRepo,
		MeterRepo:      stores.MeterRepo,
		GroupRepo:      stores.GroupRepo,
		SettingsRepo:   stores.SettingsRepo,
	}

	_, err = NewCommitmentService(params).Update(
		s.GetContext(),
		created.ID,
		dto.UpdateCommitmentRequest{Name: lo.ToPtr("Renamed")},
	)
	s.Require().NoError(err)
	s.Require().GreaterOrEqual(len(events), 3, "events %v", events)
	s.Equal([]string{"tx", "lock:commitment:" + created.ID, "get"}, events[:3])
}

func (s *CommitmentServiceSuite) TestUpdate_AfterTerminateKeepsTerminated() {
	created, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.Require().NoError(err)
	at := created.StartDate.AddDate(0, 4, 0)
	_, err = s.svc.Terminate(s.GetContext(), created.ID, dto.TerminateCommitmentRequest{EffectiveAt: &at})
	s.Require().NoError(err)

	updated, err := s.svc.Update(
		s.GetContext(),
		created.ID,
		dto.UpdateCommitmentRequest{InvoiceLabel: lo.ToPtr("Renamed")},
	)
	s.Require().NoError(err)
	s.Equal("Renamed", updated.InvoiceLabel)
	s.Equal(types.CommitmentStatusTerminated, updated.CommitmentStatus)
	s.Require().NotNil(updated.TerminatedAt)
	s.True(updated.TerminatedAt.Equal(at), "got %s", updated.TerminatedAt)
}

func (s *CommitmentServiceSuite) TestValidation_EndDateAfterSubscriptionEnd() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.EndDate = lo.ToPtr(sub.StartDate.AddDate(0, 6, 0))
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	req := validReq(s.fx.sub.ID, s.fx.priceAPI.ID) // ends 12 months in, on a billing date
	_, err = s.svc.Create(s.GetContext(), req)
	s.requireValidation(err, "subscription end")

	req.EndDate = *sub.EndDate
	_, err = s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)
}

func (s *CommitmentServiceSuite) TestValidation_C16_AnyScopeWithLegacyCommitment() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(500))
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	_, err = s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "legacy subscription-level commitment")
}

func (s *CommitmentServiceSuite) TestValidation_ScopePriceMustBillEverySubscriptionPeriod() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.BillingPeriod = types.BILLING_PERIOD_QUARTER
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	_, err = s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "commitments require scope prices billed every subscription period")
}

func (s *CommitmentServiceSuite) TestCheckPriceAttach_RejectsPriceOnAnotherCadence() {
	req := validReq(s.fx.sub.ID, "")
	req.Scope = dto.CommitmentScopeInput{EntityType: types.CommitmentScopeGroup, EntityID: s.fx.groupAPIOnly.ID}
	req.Type, req.CommittedRate, req.Value = types.COMMITMENT_TYPE_AMOUNT, nil, decimal.NewFromInt(100)
	_, err := s.svc.Create(s.GetContext(), req)
	s.Require().NoError(err)

	weekly := *s.fx.priceOtherMeterInGroup
	weekly.ID, weekly.BillingPeriod = "price_other_meter_weekly", types.BILLING_PERIOD_WEEKLY
	err = s.svc.CheckPriceAttach(s.GetContext(), s.fx.sub, &weekly, s.fx.sub.StartDate)
	s.requireValidation(err, "commitments require scope prices billed every subscription period")
}

func (s *CommitmentServiceSuite) TestValidation_FutureScopeLineOnAnotherCadence() {
	future := *s.fx.lineAPI
	future.ID, future.BillingPeriod = "subs_line_api_weekly", types.BILLING_PERIOD_WEEKLY
	future.StartDate = time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &future))

	_, err := s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "commitments require scope prices billed every subscription period")
}

func (s *CommitmentServiceSuite) TestCreate_RejectsIncompleteSubscription() {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.fx.sub.ID)
	s.Require().NoError(err)
	sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))

	_, err = s.svc.Create(s.GetContext(), validReq(s.fx.sub.ID, s.fx.priceAPI.ID))
	s.requireValidation(err, "must be active or trialing")
}
