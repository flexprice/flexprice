package contract

import (
	"testing"

	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"

	"github.com/stretchr/testify/require"
)

func term(v string) extraction.Term {
	return extraction.Term{Value: v, Confidence: 90, Page: 3, Quote: v, Options: []string{}}
}

func TestNormalizeCharges_SplitsPrepaidAllowance(t *testing.T) {
	in := []extraction.Charge{{
		Kind:             ChargeKindUsage,
		Name:             term("Annual Usage fee"),
		Amount:           term("120000"),
		AmountPeriod:     term(BillingPeriodAnnual),
		BillingPeriod:    term(BillingPeriodMonthly),
		BillingTiming:    term(BillingTimingArrears),
		IncludedQuantity: term("840000"),
		Unit:             term("minutes"),
		TierMode:         term(TierModeGraduated),
		Tiers:            []extraction.Tier{{UnitAmount: " 0.16 "}},
	}}

	got := extractor().normalizeCharges(in)
	require.Len(t, got, 2)

	fee := got[0]
	require.Equal(t, ChargeKindRecurringFixed, fee.Kind)
	require.Equal(t, "Annual Usage fee", fee.Name.Value)
	require.Equal(t, "120000", fee.Amount.Value)
	require.Equal(t, BillingPeriodAnnual, fee.AmountPeriod.Value)
	require.Equal(t, BillingPeriodMonthly, fee.BillingPeriod.Value)
	require.Equal(t, lowLabelConfidence, fee.BillingPeriod.Confidence)
	require.False(t, fee.BillingTiming.IsSet())
	require.False(t, fee.IncludedQuantity.IsSet())
	require.False(t, fee.TierMode.IsSet())
	require.Empty(t, fee.Tiers)

	overage := got[1]
	require.Equal(t, ChargeKindUsage, overage.Kind)
	require.Equal(t, "Annual Usage fee overage", overage.Name.Value)
	require.Equal(t, "0.16", overage.Amount.Value)
	require.Equal(t, 3, overage.Amount.Page)
	require.False(t, overage.AmountPeriod.IsSet())
	require.Equal(t, BillingPeriodMonthly, overage.BillingPeriod.Value)
	require.Equal(t, BillingTimingArrears, overage.BillingTiming.Value)
	require.Equal(t, "840000", overage.IncludedQuantity.Value)
	require.Equal(t, "minutes", overage.Unit.Value)
	require.False(t, overage.TierMode.IsSet())
	require.Empty(t, overage.Tiers)
}

func TestNormalizeCharges(t *testing.T) {
	tests := []struct {
		name       string
		in         extraction.Charge
		wantAmount string
		wantMode   string
		wantTiers  int
	}{
		{
			name:       "single tier becomes per-unit price",
			in:         extraction.Charge{Kind: ChargeKindUsage, TierMode: term(TierModeVolume), Tiers: []extraction.Tier{{UnitAmount: "0.02"}}},
			wantAmount: "0.02",
		},
		{
			name:      "two tiers kept",
			in:        extraction.Charge{Kind: ChargeKindUsage, TierMode: term(TierModeGraduated), Tiers: []extraction.Tier{{UpTo: "100", UnitAmount: "0.2"}, {UnitAmount: "0.1"}}},
			wantMode:  TierModeGraduated,
			wantTiers: 2,
		},
		{
			name:       "tier mode without tiers cleared",
			in:         extraction.Charge{Kind: ChargeKindUsage, Amount: term("0.05"), TierMode: term(TierModeUnknown)},
			wantAmount: "0.05",
		},
		{
			name:      "single tier with flat fee kept",
			in:        extraction.Charge{Kind: ChargeKindUsage, TierMode: term(TierModeVolume), Tiers: []extraction.Tier{{UnitAmount: "0.1", FlatAmount: "50"}}},
			wantMode:  TierModeVolume,
			wantTiers: 1,
		},
		{
			name:       "fixed charge untouched",
			in:         extraction.Charge{Kind: ChargeKindRecurringFixed, Amount: term("1000")},
			wantAmount: "1000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractor().normalizeCharges([]extraction.Charge{tt.in})
			require.Len(t, got, 1)
			require.Equal(t, tt.wantAmount, got[0].Amount.Value)
			require.Equal(t, tt.wantMode, got[0].TierMode.Value)
			require.Len(t, got[0].Tiers, tt.wantTiers)
		})
	}
}

func TestMarkWaived(t *testing.T) {
	low := func(v, quote string) extraction.Term {
		return extraction.Term{Value: v, Confidence: 34, Page: 1, Quote: quote, Options: []string{}}
	}
	tests := []struct {
		name      string
		structure string
		amount    extraction.Term
		wantConf  int
		wantNote  string
	}{
		{name: "waived in billing structure", structure: "Monthly annual usage fee; setup/platform fees waived", amount: low("0", "$0"), wantConf: highLabelConfidence, wantNote: noteWaived},
		{name: "waived in own quote", structure: "Monthly fee", amount: low("0", "Setup fee: waived"), wantConf: highLabelConfidence, wantNote: noteWaived},
		{name: "zero without waiver kept", structure: "Monthly fee", amount: low("0", "$0"), wantConf: 34},
		{name: "non-zero untouched", structure: "fees waived", amount: low("500", "$500"), wantConf: 34},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &extraction.Result{
				Billing: extraction.Billing{Structure: term(tt.structure)},
				Charges: []extraction.Charge{{Kind: ChargeKindRecurringFixed, Amount: tt.amount}},
			}
			extractor().markWaived(r)
			require.Equal(t, tt.wantConf, r.Charges[0].Amount.Confidence)
			require.Equal(t, tt.wantNote, r.Charges[0].Amount.Note)
		})
	}
}

func TestMoveUsageBanks(t *testing.T) {
	bank := extraction.Charge{Kind: ChargeKindUsage, Name: term("Annual Usage Bank"), Amount: term("150000"), AmountPeriod: term(BillingPeriodAnnual),
		IncludedQuantity: term("150000"), Unit: term("USD"), Tiers: []extraction.Tier{{}}}
	minutes := extraction.Charge{Kind: ChargeKindUsage, Name: term("Minutes bundle"), Amount: term("120000"), AmountPeriod: term(BillingPeriodAnnual),
		IncludedQuantity: term("840000"), Unit: term("minutes")}
	rate := extraction.Charge{Kind: ChargeKindUsage, Name: term("Voice compute"), Unit: term("min"), Tiers: []extraction.Tier{{UnitAmount: "0.045"}}}
	fee := extraction.Charge{Kind: ChargeKindRecurringFixed, Name: term("Platform fee"), Amount: term("120000"), AmountPeriod: term(BillingPeriodAnnual)}
	allowance := extraction.Charge{Kind: ChargeKindUsage, Name: term("Usage fee"), Amount: term("120000"), AmountPeriod: term(BillingPeriodAnnual), Tiers: []extraction.Tier{{UnitAmount: "0.16"}}}
	r := &extraction.Result{Charges: []extraction.Charge{bank, rate, fee, allowance, minutes}}

	extractor().moveUsageBanks(r)

	names := make([]string, 0, len(r.Charges))
	for _, ch := range r.Charges {
		names = append(names, ch.Name.Value)
	}
	require.Equal(t, []string{"Voice compute", "Platform fee", "Usage fee", "Minutes bundle"}, names, "a fee that includes minutes stays a charge")
	require.Len(t, r.Commitments, 1)
	c := r.Commitments[0]
	require.Equal(t, "150000", c.Amount.Value)
	require.Equal(t, 3, c.Amount.Page, "the commitment keeps the citation")
	require.Equal(t, lowLabelConfidence, c.Amount.Confidence)
	require.Contains(t, c.Amount.Note, noteUsageBank)
	require.Equal(t, BillingPeriodAnnual, c.Period.Value)
	require.False(t, c.Covers.IsSet())
}
