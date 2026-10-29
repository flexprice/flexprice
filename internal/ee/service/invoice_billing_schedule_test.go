package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func retainedScheduleFixture() (context.Context, *invoice.Invoice, *invoice.InvoiceLineItem) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant"), "env")
	start := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	inv := &invoice.Invoice{ID: "inv", CustomerID: "customer", Currency: "usd", InvoiceStatus: types.InvoiceStatusDraft, EnvironmentID: "env", BaseModel: types.GetDefaultBaseModel(ctx)}
	line := invoice.NewInvoiceLineItemBuilder(&invoice.InvoiceLineItem{
		ID: "line", InvoiceID: inv.ID, CustomerID: inv.CustomerID, Currency: inv.Currency,
		Amount: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1),
		PeriodStart: &start, PeriodEnd: &end, CommitmentID: lo.ToPtr("commitment"),
		Metadata:      types.Metadata{"description": "Contract payment"},
		EnvironmentID: "env", BaseModel: types.GetDefaultBaseModel(ctx),
	}).WithBillingScheduleItemID(lo.ToPtr("schedule")).Build()
	inv.LineItems = []*invoice.InvoiceLineItem{line}
	return ctx, inv, line
}

func TestPreserveBillingScheduleRequest(t *testing.T) {
	for _, include := range []bool{false, true} {
		t.Run(map[bool]string{false: "source omitted", true: "source already emitted"}[include], func(t *testing.T) {
			ctx, inv, line := retainedScheduleFixture()
			req := &dto.InvoiceComputeRequest{LineItems: []dto.CreateInvoiceLineItemRequest{{Amount: decimal.NewFromInt(-20), Quantity: decimal.NewFromInt(1)}}}
			if include {
				req.LineItems = append(req.LineItems, billingScheduleLineRequest(line))
				req.Subtotal = decimal.NewFromInt(80)
			}
			originalSubtotal := req.Subtotal
			out, err := preserveBillingScheduleRequest(ctx, inv, req)
			require.NoError(t, err)
			require.Len(t, out.LineItems, 2)
			require.True(t, out.Subtotal.Equal(decimal.NewFromInt(80)))
			require.True(t, out.Total.Equal(out.Subtotal))
			require.True(t, out.AmountDue.Equal(out.Subtotal))
			require.True(t, req.Subtotal.Equal(originalSubtotal), "preparation must not mutate the caller's request")
			require.Len(t, req.LineItems, map[bool]int{false: 1, true: 2}[include])
			again, err := preserveBillingScheduleRequest(ctx, inv, out)
			require.NoError(t, err)
			require.Len(t, again.LineItems, 2)
			require.True(t, out.Subtotal.Equal(again.Subtotal))
		})
	}
}

func TestPreserveBillingScheduleRejectsChangedSources(t *testing.T) {
	for name, change := range map[string]func(*dto.CreateInvoiceLineItemRequest){
		"unknown source":     func(r *dto.CreateInvoiceLineItemRequest) { r.BillingScheduleItemID = lo.ToPtr("unknown") },
		"principal":          func(r *dto.CreateInvoiceLineItemRequest) { r.Amount = decimal.NewFromInt(99) },
		"quantity":           func(r *dto.CreateInvoiceLineItemRequest) { r.Quantity = decimal.NewFromInt(2) },
		"period":             func(r *dto.CreateInvoiceLineItemRequest) { r.PeriodEnd = lo.ToPtr(r.PeriodEnd.AddDate(0, 1, 0)) },
		"child subscription": func(r *dto.CreateInvoiceLineItemRequest) { r.SubscriptionID = lo.ToPtr("child") },
		"commitment":         func(r *dto.CreateInvoiceLineItemRequest) { r.CommitmentID = lo.ToPtr("other") },
		"price":              func(r *dto.CreateInvoiceLineItemRequest) { r.PriceID = lo.ToPtr("synthetic") },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, inv, line := retainedScheduleFixture()
			req := billingScheduleLineRequest(line)
			change(&req)
			_, _, _, err := splitBillingScheduleLines(ctx, inv, []dto.CreateInvoiceLineItemRequest{req})
			require.True(t, ierr.IsInvalidOperation(err), "%+v", err)
			require.Equal(t, "100", line.Amount.String())
		})
	}
	ctx, inv, line := retainedScheduleFixture()
	_, _, _, err := splitBillingScheduleLines(ctx, inv, []dto.CreateInvoiceLineItemRequest{billingScheduleLineRequest(line), billingScheduleLineRequest(line)})
	require.Error(t, err)
	inv.LineItems = append(inv.LineItems, line)
	_, _, _, err = splitBillingScheduleLines(ctx, inv, nil)
	require.Error(t, err)
}

func TestBillingScheduleRecomputeRejectsUnsafeContext(t *testing.T) {
	for name, change := range map[string]func(*invoice.Invoice){
		"trial":               func(i *invoice.Invoice) { i.BillingReason = string(types.InvoiceBillingReasonSubscriptionTrialStart) },
		"paused":              func(i *invoice.Invoice) { i.BillingReason = string(types.InvoiceBillingReasonSubscriptionPaused) },
		"skipped":             func(i *invoice.Invoice) { i.InvoiceStatus = types.InvoiceStatusSkipped },
		"converted":           func(i *invoice.Invoice) { i.FxConversion = &types.FxConversion{} },
		"custom currency":     func(i *invoice.Invoice) { i.CustomCurrency = &types.CustomCurrency{} },
		"foreign environment": func(i *invoice.Invoice) { i.EnvironmentID = "another" },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, inv, _ := retainedScheduleFixture()
			change(inv)
			require.Error(t, validateBillingScheduleRecompute(ctx, inv))
		})
	}
}

func TestPreserveBillingScheduleRejectsUnattributedSubtotal(t *testing.T) {
	ctx, inv, _ := retainedScheduleFixture()
	_, err := preserveBillingScheduleRequest(ctx, inv, &dto.InvoiceComputeRequest{Subtotal: decimal.NewFromInt(50)})
	require.Error(t, err, "amount-only replacement must not silently discard unrelated principal")
}

func TestPreserveBillingScheduleRejectsInvalidStoredPrincipal(t *testing.T) {
	for name, change := range map[string]func(*invoice.InvoiceLineItem){
		"sub-cent principal": func(l *invoice.InvoiceLineItem) { l.Amount = decimal.RequireFromString("100.001") },
		"missing period":     func(l *invoice.InvoiceLineItem) { l.PeriodStart = nil },
		"zero period":        func(l *invoice.InvoiceLineItem) { l.PeriodStart = lo.ToPtr(time.Time{}) },
		"unserializable period": func(l *invoice.InvoiceLineItem) {
			l.PeriodEnd = lo.ToPtr(time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC))
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, inv, line := retainedScheduleFixture()
			change(line)
			_, _, _, err := splitBillingScheduleLines(ctx, inv, nil)
			require.Error(t, err)
		})
	}
}

func (s *InvoiceServiceSuite) addScheduledPayment(inv *invoice.Invoice, source string, amount int64) *invoice.InvoiceLineItem {
	ctx := s.GetContext()
	start, end := inv.PeriodStart, inv.PeriodEnd
	line := invoice.NewInvoiceLineItemBuilder(&invoice.InvoiceLineItem{
		ID: types.GenerateUUIDWithPrefix("invli"), InvoiceID: inv.ID, CustomerID: inv.CustomerID,
		SubscriptionID: inv.SubscriptionID, CommitmentID: lo.ToPtr("commitment"),
		Amount: decimal.NewFromInt(amount), Quantity: decimal.NewFromInt(1), Currency: inv.Currency,
		DisplayName: lo.ToPtr("Contract payment"), PeriodStart: start, PeriodEnd: end,
		EnvironmentID: inv.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx),
	}).WithBillingScheduleItemID(&source).Build()
	s.Require().NoError(s.invoiceRepo.AddLineItems(ctx, inv.ID, []*invoice.InvoiceLineItem{line}))
	inv.LineItems = append(inv.LineItems, line)
	inv.InvoiceStatus = types.InvoiceStatusDraft
	inv.Subtotal = inv.Subtotal.Add(line.Amount)
	inv.Total, inv.AmountDue, inv.AmountRemaining = inv.Subtotal, inv.Subtotal, inv.Subtotal
	s.Require().NoError(s.invoiceRepo.Update(ctx, inv))
	return line
}

func (s *InvoiceServiceSuite) TestComputeInvoice_PreservesScheduledPrincipal() {
	for _, ordinary := range []int64{0, 40} {
		s.Run(decimal.NewFromInt(ordinary).String(), func() {
			s.SetupTest()
			ctx := s.GetContext()
			inv := s.computedCycleDraft("sub_scheduled_preserve", "usd", decimal.NewFromInt(ordinary))
			first := s.addScheduledPayment(inv, "schedule_a", 75)
			second := s.addScheduledPayment(inv, "schedule_b", 25)
			for range 2 {
				got, skipped, err := s.service.ComputeInvoice(ctx, inv.ID, nil)
				s.Require().NoError(err)
				s.False(skipped)
				s.Equal(types.InvoiceStatusDraft, got.InvoiceStatus)
				s.Equal(decimal.NewFromInt(ordinary+100).String(), got.Subtotal.String())
				s.Equal(got.Subtotal.String(), got.AmountDue.String())
				byID := lo.SliceToMap(got.LineItems, func(l *invoice.InvoiceLineItem) (string, *invoice.InvoiceLineItem) { return l.ID, l })
				for _, old := range []*invoice.InvoiceLineItem{first, second} {
					s.Require().NotNil(byID[old.ID])
					s.Equal(old.BillingScheduleItemID, byID[old.ID].BillingScheduleItemID)
					s.True(old.Amount.Equal(byID[old.ID].Amount))
					s.Equal(old.PeriodStart, byID[old.ID].PeriodStart)
					s.Nil(byID[old.ID].PriceID)
				}
				s.Len(got.LineItems, map[bool]int{false: 2, true: 3}[ordinary > 0])
			}
		})
	}
}

func (s *InvoiceServiceSuite) TestComputeInvoice_ScheduledPrincipalKeepsAppliedCredits() {
	inv := s.computedCycleDraft("sub_scheduled_credits", "usd", decimal.NewFromInt(40))
	s.addScheduledPayment(inv, "schedule_credits", 100)
	s.applyToDraft(inv, decimal.NewFromInt(20))
	for range 2 {
		got, skipped, err := s.service.ComputeInvoice(s.GetContext(), inv.ID, nil)
		s.Require().NoError(err)
		s.False(skipped)
		s.Equal("140", got.Subtotal.String())
		s.Equal("120", got.AmountDue.String())
		s.Equal("20", got.TotalPrepaidCreditsApplied.String())
	}
}

func (s *InvoiceServiceSuite) TestRecalculateV2_PreservesScheduledPrincipal() {
	s.service.(*invoiceService).SubscriptionPhaseRepo = s.GetStores().SubscriptionPhaseRepo
	inv := s.computedCycleDraft("sub_scheduled_v2", "usd", decimal.NewFromInt(40))
	line := s.addScheduledPayment(inv, "schedule_v2", 100)
	for range 2 {
		got, err := s.service.RecalculateInvoiceV2(s.GetContext(), inv.ID, false)
		s.Require().NoError(err)
		s.Equal("140", got.Subtotal.String())
		s.Equal("140", got.AmountDue.String())
		stored, err := s.GetStores().InvoiceLineItemRepo.Get(s.GetContext(), line.ID)
		s.Require().NoError(err)
		s.Equal(types.StatusPublished, stored.Status)
		s.Equal(line.BillingScheduleItemID, stored.BillingScheduleItemID)
	}
}

func (s *InvoiceServiceSuite) TestRecalculateV2_ScheduledPrincipalFeedsDiscountsAndTaxes() {
	ctx := s.GetContext()
	s.service.(*invoiceService).SubscriptionPhaseRepo = s.GetStores().SubscriptionPhaseRepo
	inv := s.computedCycleDraft("sub_scheduled_tax", "usd", decimal.NewFromInt(40))
	line := s.addScheduledPayment(inv, "scheduled_tax", 100)
	c := &coupon.Coupon{ID: "coupon_scheduled", Name: "Ten percent", Type: types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromInt(10)), Cadence: types.CouponCadenceForever,
		Currency: "usd", EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))
	s.Require().NoError(s.GetStores().CouponAssociationRepo.Create(ctx, &coupon_association.CouponAssociation{
		ID: "association_scheduled", CouponID: c.ID, SubscriptionID: *inv.SubscriptionID,
		StartDate: *inv.PeriodStart, Coupon: c, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	rate := s.createPercentageTaxRate(10)
	s.createSubscriptionTaxAssociation(rate.ID, *inv.SubscriptionID)
	for range 2 {
		got, err := s.service.RecalculateInvoiceV2(ctx, inv.ID, false)
		s.Require().NoError(err)
		s.Equal("140", got.Subtotal.String())
		s.Equal("14", got.TotalDiscount.String())
		s.Equal("12.6", got.TotalTax.String())
		s.Equal("138.6", got.AmountDue.String())
		stored, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, line.ID)
		s.Require().NoError(err)
		s.Equal("100", stored.Amount.String())
		s.Equal("10", stored.InvoiceLevelDiscount.String())
	}
}

func (s *InvoiceServiceSuite) TestScheduledInvoice_OneOffComputeCannotDuplicateUnlinkedClientRows() {
	inv := s.computedCycleDraft("sub_scheduled_oneoff", "usd", decimal.Zero)
	s.addScheduledPayment(inv, "scheduled_oneoff", 100)
	inv.InvoiceType = types.InvoiceTypeOneOff
	s.Require().NoError(s.invoiceRepo.Update(s.GetContext(), inv))
	_, _, err := s.service.ComputeInvoice(s.GetContext(), inv.ID, &dto.InvoiceComputeRequest{
		Subtotal: decimal.NewFromInt(100), Total: decimal.NewFromInt(100), AmountDue: decimal.NewFromInt(100),
		LineItems: []dto.CreateInvoiceLineItemRequest{{Amount: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}},
	})
	s.True(ierr.IsInvalidOperation(err), "%+v", err)
	got, skipped, err := s.service.ComputeInvoice(s.GetContext(), inv.ID, nil)
	s.Require().NoError(err)
	s.False(skipped)
	s.Equal("100", got.Subtotal.String())
	s.Len(got.LineItems, 1)
}

func (s *InvoiceServiceSuite) TestScheduledInvoice_V2RechecksDraftUnderLock() {
	inv := s.computedCycleDraft("sub_scheduled_stale", "usd", decimal.NewFromInt(40))
	s.addScheduledPayment(inv, "scheduled_stale", 100)
	s.invoiceRepo.BeforeGetForUpdate = func(id string) {
		s.invoiceRepo.BeforeGetForUpdate = nil
		inv.InvoiceStatus = types.InvoiceStatusFinalized
		s.Require().NoError(s.invoiceRepo.Update(s.GetContext(), inv))
	}
	defer func() { s.invoiceRepo.BeforeGetForUpdate = nil }()
	_, err := s.service.RecalculateInvoiceV2(s.GetContext(), inv.ID, false)
	s.True(ierr.IsInvalidOperation(err), "%+v", err)
	stored, err := s.invoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
	s.Equal("140", stored.Subtotal.String())
}

func (s *InvoiceServiceSuite) TestScheduledInvoice_V2RejectsChangedPeriod() {
	inv := s.computedCycleDraft("sub_scheduled_period", "usd", decimal.NewFromInt(40))
	s.addScheduledPayment(inv, "scheduled_period", 100)
	newEnd := inv.PeriodEnd.AddDate(0, 0, 1)
	s.invoiceRepo.BeforeGetForUpdate = func(id string) {
		s.invoiceRepo.BeforeGetForUpdate = nil
		inv.PeriodEnd = &newEnd
		s.Require().NoError(s.invoiceRepo.Update(s.GetContext(), inv))
	}
	defer func() { s.invoiceRepo.BeforeGetForUpdate = nil }()
	_, err := s.service.RecalculateInvoiceV2(s.GetContext(), inv.ID, false)
	s.True(ierr.IsInvalidOperation(err), "%+v", err)
	stored, err := s.invoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.Equal(&newEnd, stored.PeriodEnd)
	s.Equal("140", stored.Subtotal.String())
}

func (s *InvoiceServiceSuite) TestScheduledInvoice_UnsafeRecomputePreservesState() {
	for _, reason := range []types.InvoiceBillingReason{types.InvoiceBillingReasonSubscriptionPaused, types.InvoiceBillingReasonSubscriptionTrialStart} {
		s.Run(string(reason), func() {
			s.SetupTest()
			inv := s.computedCycleDraft("sub_scheduled_unsafe", "usd", decimal.Zero)
			line := s.addScheduledPayment(inv, "scheduled_unsafe", 100)
			inv.BillingReason = string(reason)
			s.Require().NoError(s.invoiceRepo.Update(s.GetContext(), inv))
			_, _, err := s.service.ComputeInvoice(s.GetContext(), inv.ID, nil)
			s.True(ierr.IsInvalidOperation(err), "%+v", err)
			stored, err := s.invoiceRepo.Get(s.GetContext(), inv.ID)
			s.Require().NoError(err)
			s.Equal(types.InvoiceStatusDraft, stored.InvoiceStatus)
			s.Equal("100", stored.Subtotal.String())
			current, err := s.GetStores().InvoiceLineItemRepo.Get(s.GetContext(), line.ID)
			s.Require().NoError(err)
			s.Equal(types.StatusPublished, current.Status)
		})
	}
}

func (s *InvoiceServiceSuite) TestScheduledInvoice_LegacyRecalculateRequiresCorrection() {
	for _, status := range []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusFinalized, types.InvoiceStatusVoided} {
		s.Run(string(status), func() {
			s.SetupTest()
			inv := s.computedCycleDraft("sub_scheduled_legacy", "usd", decimal.NewFromInt(40))
			s.addScheduledPayment(inv, "scheduled_legacy", 100)
			inv.InvoiceStatus = status
			s.Require().NoError(s.invoiceRepo.Update(s.GetContext(), inv))
			_, err := s.service.RecalculateInvoice(s.GetContext(), inv.ID)
			s.True(ierr.IsInvalidOperation(err), "%+v", err)
			stored, err := s.invoiceRepo.Get(s.GetContext(), inv.ID)
			s.Require().NoError(err)
			s.Equal(status, stored.InvoiceStatus)
			s.Nil(stored.RecalculatedInvoiceID)
			s.True(stored.RefundedAmount.IsZero())
		})
	}
}

func (s *InvoiceServiceSuite) TestScheduledInvoice_RecalculateRechecksLockedSources() {
	inv := s.computedCycleDraft("sub_scheduled_lock", "usd", decimal.NewFromInt(40))
	var source *invoice.InvoiceLineItem
	s.invoiceRepo.BeforeGetForUpdate = func(id string) {
		s.invoiceRepo.BeforeGetForUpdate = nil
		source = s.addScheduledPayment(inv, "arrived_under_lock", 100)
	}
	defer func() { s.invoiceRepo.BeforeGetForUpdate = nil }()
	_, err := s.service.RecalculateInvoice(s.GetContext(), inv.ID)
	s.True(ierr.IsInvalidOperation(err), "%+v", err)
	s.Require().NotNil(source)
	stored, err := s.invoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, stored.InvoiceStatus)
	s.Nil(stored.RecalculatedInvoiceID)
	current, err := s.GetStores().InvoiceLineItemRepo.Get(s.GetContext(), source.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusPublished, current.Status)
}

func (s *InvoiceServiceSuite) TestScheduledInvoice_V2RejectsDestinationDrift() {
	inv := s.computedCycleDraft("sub_scheduled_destination", "usd", decimal.NewFromInt(40))
	s.addScheduledPayment(inv, "scheduled_destination", 100)
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), *inv.SubscriptionID)
	s.Require().NoError(err)
	other := *s.testData.customer
	other.ID = "new_recipient"
	s.Require().NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), &other))
	sub.InvoicingCustomerID = &other.ID
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), sub))
	_, err = s.service.RecalculateInvoiceV2(s.GetContext(), inv.ID, false)
	s.True(ierr.IsInvalidOperation(err), "%+v", err)
	stored, err := s.invoiceRepo.Get(s.GetContext(), inv.ID)
	s.Require().NoError(err)
	s.Equal(inv.CustomerID, stored.CustomerID)
	s.Equal("140", stored.Subtotal.String())
}

func (s *LineItemEditSuite) TestScheduledLine_ManualEditsRequireCorrection() {
	for _, stale := range []bool{false, true} {
		s.Run(map[bool]string{false: "current ID", true: "parent ID"}[stale], func() {
			s.SetupTest()
			ctx := s.GetContext()
			inv, line := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(1))
			line = invoice.NewInvoiceLineItemBuilder(line).WithBillingScheduleItemID(lo.ToPtr("scheduled_edit")).Build()
			target := line.ID
			if stale {
				target = "prior_line"
				line = invoice.NewInvoiceLineItemBuilder(line).WithParentLineItemID(&target).Build()
			}
			s.Require().NoError(s.GetStores().InvoiceLineItemRepo.Update(ctx, line))
			_, err := s.service.UpdateLineItem(ctx, inv.ID, target, dto.UpdateLineItemRequest{DisplayName: lo.ToPtr("Changed")})
			s.True(ierr.IsInvalidOperation(err), "%+v", err)
			_, err = s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{LineItemIDs: []string{target}})
			s.True(ierr.IsInvalidOperation(err), "%+v", err)
			current, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, line.ID)
			s.Require().NoError(err)
			s.Equal(types.StatusPublished, current.Status)
			s.Equal(line.DisplayName, current.DisplayName)
			items, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
			s.Require().NoError(err)
			s.Len(items, 1)
		})
	}
}

func (s *LineItemEditSuite) TestScheduledLine_MixedRemovalIsRejectedBeforeWrites() {
	ctx := s.GetContext()
	inv, ordinary := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(40), decimal.NewFromInt(1))
	source := invoice.NewInvoiceLineItemBuilder(ordinary).WithID("scheduled_line").WithBillingScheduleItemID(lo.ToPtr("scheduled_remove")).Build()
	s.Require().NoError(s.GetStores().InvoiceLineItemRepo.Create(ctx, source))
	_, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{LineItemIDs: []string{ordinary.ID, source.ID}})
	s.True(ierr.IsInvalidOperation(err), "%+v", err)
	for _, id := range []string{ordinary.ID, source.ID} {
		stored, err := s.GetStores().InvoiceLineItemRepo.Get(ctx, id)
		s.Require().NoError(err)
		s.Equal(types.StatusPublished, stored.Status)
	}
}

func (s *LineItemEditSuite) TestScheduledInvoice_FinalizedEditCannotVoidAndCopy() {
	ctx := s.GetContext()
	inv, line := s.createDraftInvoiceWithLineItem(ctx, decimal.NewFromInt(100), decimal.NewFromInt(1))
	line = invoice.NewInvoiceLineItemBuilder(line).WithBillingScheduleItemID(lo.ToPtr("scheduled_finalized")).Build()
	s.Require().NoError(s.GetStores().InvoiceLineItemRepo.Update(ctx, line))
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	s.Require().NoError(s.GetStores().InvoiceRepo.Update(ctx, inv))
	_, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
		Type:           dto.InvoiceModifyTypeLineItem,
		LineItemParams: &dto.InvoiceModifyLineItemParams{Action: dto.InvoiceModifyLineItemActionAdd, Items: []dto.AddLineItemRequest{{DisplayName: "Other fee", Amount: decimal.NewFromInt(10), Quantity: decimal.NewFromInt(1)}}},
	})
	s.True(ierr.IsInvalidOperation(err), "%+v", err)
	_, err = s.service.UpdateInvoice(ctx, inv.ID, dto.UpdateInvoiceRequest{ApplyDiscount: true})
	s.True(ierr.IsInvalidOperation(err), "%+v", err)
	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, stored.InvoiceStatus)
	s.Nil(stored.RecalculatedInvoiceID)
	s.True(stored.RefundedAmount.IsZero())
	metadata := types.Metadata{"external_reference": "reference_123"}
	updated, err := s.service.UpdateInvoice(ctx, inv.ID, dto.UpdateInvoiceRequest{Metadata: &metadata})
	s.Require().NoError(err)
	s.Equal(metadata, updated.Metadata)
	s.Equal(types.InvoiceStatusFinalized, updated.InvoiceStatus)
	s.Equal("100", updated.Subtotal.String())
}
