package testutil

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/journal"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestInMemoryJournalSumLines mirrors the journal repo's TestSumLines: acme invoices 1,300 in Jan and earns 200; in Feb
// beta invoices 100 and each earns 100.
func TestInMemoryJournalSumLines(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	periods := NewInMemoryAccountingPeriodStore()
	store := NewInMemoryJournalStore(periods)

	month := func(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, time.UTC) }
	period := func(m time.Month) string {
		p, err := periods.GetOrCreate(ctx, month(m, 1))
		require.NoError(t, err)
		return p.ID()
	}
	jan, feb, mar := period(time.January), period(time.February), period(time.March)
	now := time.Now().UTC()

	line := func(account types.LedgerAccountKey, direction types.JournalDirection, amount int64, pobID string) *journal.Line {
		return journal.NewLineBuilder(nil).
			WithAccount(account).
			WithDirection(direction).
			WithAmount(decimal.NewFromInt(amount)).
			WithPOBID(pobID).
			Build()
	}
	debit := func(account types.LedgerAccountKey, amount int64, pobID string) *journal.Line {
		return line(account, types.JournalDirectionDebit, amount, pobID)
	}
	credit := func(account types.LedgerAccountKey, amount int64, pobID string) *journal.Line {
		return line(account, types.JournalDirectionCredit, amount, pobID)
	}
	post := func(eventType types.JournalEventType, periodID string, businessDate time.Time, customerID, contractID, currency string, createdAt time.Time, lines ...*journal.Line) {
		require.NoError(t, store.CreateWithLines(ctx, journal.NewBuilder(nil).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_JOURNAL_ENTRY)).
			WithEventType(eventType).
			WithSource(types.RevRecEntityTypeInvoice, "inv_1").
			WithAccountingPeriodID(periodID).
			WithBusinessDate(businessDate).
			WithCustomerID(customerID).
			WithContractID(contractID).
			WithCurrency(currency).
			WithLines(lines).
			WithCreatedAt(createdAt).
			Build()))
	}
	deferred, unbilled, ar := types.LedgerAccountDeferredRevenue, types.LedgerAccountUnbilledReceivable, types.LedgerAccountReceivable

	post(types.JournalEventInvoiceFinalized, jan, month(time.January, 1), "cust_acme", "contract_1", "usd", now,
		debit(ar, 1300, ""), credit(deferred, 1200, "pob_annual"), credit(deferred, 100, "pob_monthly"))
	post(types.JournalEventRevenueOffset, jan, month(time.January, 31), "cust_acme", "contract_1", "usd", now,
		debit(deferred, 100, "pob_annual"), debit(deferred, 100, "pob_monthly"), credit(unbilled, 100, "pob_annual"), credit(unbilled, 100, "pob_monthly"))
	post(types.JournalEventInvoiceFinalized, feb, month(time.February, 1), "cust_beta", "contract_2", "usd", now,
		debit(ar, 100, ""), credit(deferred, 100, "pob_beta"))
	post(types.JournalEventRevenueOffset, feb, month(time.February, 28), "cust_acme", "contract_1", "usd", now,
		debit(deferred, 100, "pob_annual"), credit(unbilled, 100, "pob_annual"))
	post(types.JournalEventRevenueOffset, feb, month(time.February, 28), "cust_beta", "contract_2", "usd", now,
		debit(deferred, 100, "pob_beta"), credit(unbilled, 100, "pob_beta"))
	// Dated in January but posted to February, as when January is closed.
	post(types.JournalEventCreditNoteFinalized, feb, month(time.January, 20), "cust_acme", "contract_1", "usd", now,
		debit(types.LedgerAccountContraRevenue, 10, ""), credit(ar, 10, ""))
	// Excluded unless the case says otherwise: after the range, another currency, created after as_of.
	post(types.JournalEventInvoiceFinalized, mar, month(time.March, 1), "cust_acme", "contract_1", "usd", now,
		debit(ar, 999, ""), credit(deferred, 999, "pob_annual"))
	post(types.JournalEventInvoiceFinalized, jan, month(time.January, 1), "cust_acme", "contract_1", "eur", now,
		debit(ar, 500, ""), credit(deferred, 500, "pob_annual"))
	post(types.JournalEventRevenueOffset, feb, month(time.February, 28), "cust_acme", "contract_1", "usd", now.Add(time.Hour),
		debit(deferred, 50, "pob_annual"), credit(unbilled, 50, "pob_annual"))

	febDeferred := func(edit func(f *journalRepo.SumFilter)) *journalRepo.SumFilter {
		f := &journalRepo.SumFilter{
			Currency:     "usd",
			PeriodStart:  month(time.February, 1),
			PeriodEnd:    month(time.February, 1),
			Accounts:     []types.LedgerAccountKey{deferred},
			CreatedAtLTE: &now,
		}
		edit(f)
		return f
	}
	tests := []struct {
		name   string
		filter *journalRepo.SumFilter
		want   []string
	}{
		{
			name:   "opening folds January, February by event type",
			filter: febDeferred(func(f *journalRepo.SumFilter) {}),
			want: []string{
				"opening|deferred_revenue|||||1100",
				"2026-02|deferred_revenue|invoice_finalized||||100",
				"2026-02|deferred_revenue|revenue_offset||||-200",
			},
		},
		{
			name: "grouped by customer and contract",
			filter: febDeferred(func(f *journalRepo.SumFilter) {
				f.GroupBy = []types.RevenueReportGroupBy{types.RevenueReportGroupByCustomer, types.RevenueReportGroupByContract}
			}),
			want: []string{
				"opening|deferred_revenue||cust_acme|contract_1||1100",
				"2026-02|deferred_revenue|invoice_finalized|cust_beta|contract_2||100",
				"2026-02|deferred_revenue|revenue_offset|cust_acme|contract_1||-100",
				"2026-02|deferred_revenue|revenue_offset|cust_beta|contract_2||-100",
			},
		},
		{
			name: "grouped by POB, filtered to one",
			filter: febDeferred(func(f *journalRepo.SumFilter) {
				f.GroupBy = []types.RevenueReportGroupBy{types.RevenueReportGroupByPOB}
				f.POBIDs = []string{"pob_annual"}
			}),
			want: []string{
				"opening|deferred_revenue||||pob_annual|1100",
				"2026-02|deferred_revenue|revenue_offset|||pob_annual|-100",
			},
		},
		{
			name:   "filtered by customer",
			filter: febDeferred(func(f *journalRepo.SumFilter) { f.CustomerIDs = []string{"cust_beta"} }),
			want: []string{
				"2026-02|deferred_revenue|invoice_finalized||||100",
				"2026-02|deferred_revenue|revenue_offset||||-100",
			},
		},
		{
			name:   "exclude opening: only the requested months",
			filter: febDeferred(func(f *journalRepo.SumFilter) { f.ExcludeOpening = true }),
			want: []string{
				"2026-02|deferred_revenue|invoice_finalized||||100",
				"2026-02|deferred_revenue|revenue_offset||||-200",
			},
		},
		{
			name:   "without as_of, later entries count",
			filter: febDeferred(func(f *journalRepo.SumFilter) { f.CreatedAtLTE = nil }),
			want: []string{
				"opening|deferred_revenue|||||1100",
				"2026-02|deferred_revenue|invoice_finalized||||100",
				"2026-02|deferred_revenue|revenue_offset||||-250",
			},
		},
		{
			name: "a line counts in its entry's period, not its business date",
			filter: febDeferred(func(f *journalRepo.SumFilter) {
				f.PeriodStart = month(time.January, 1)
				f.Accounts = []types.LedgerAccountKey{types.LedgerAccountContraRevenue}
			}),
			want: []string{"2026-02|contra_revenue|credit_note_finalized||||-10"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sums, err := store.SumLines(ctx, tt.filter)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.want, lo.Map(sums, func(s *journalRepo.LineSum, _ int) string {
				period := lo.TernaryF(s.PeriodStart == nil, func() string { return "opening" }, func() string { return s.PeriodStart.Format("2006-01") })
				return strings.Join([]string{
					period, string(s.Account), string(s.EventType),
					lo.FromPtr(s.CustomerID), lo.FromPtr(s.ContractID), lo.FromPtr(s.POBID), s.Net.String(),
				}, "|")
			}))
		})
	}
}
