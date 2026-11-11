//go:build pgintegration

package journal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent/journalline"
	"github.com/flexprice/flexprice/internal/config"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	accountingPeriodRepo "github.com/flexprice/flexprice/internal/repository/accountingperiod"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// newPGClient connects to local Postgres; each run gets its own environment.
func newPGClient(t *testing.T) (context.Context, postgres.IClient, *logger.Logger, string) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)

	runID := types.GenerateUUIDWithPrefix("jt")
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_"+runID)
	return ctx, postgres.NewClient(clients, log, nil), log, runID
}

func TestCreateWithLines(t *testing.T) {
	ctx, client, log, runID := newPGClient(t)
	repo := NewRepository(client, log)

	line := func(account types.LedgerAccountKey, direction types.JournalDirection, amount int64) *domainJournal.Line {
		return domainJournal.NewLineBuilder(nil).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_JOURNAL_LINE)).
			WithAccount(account).
			WithDirection(direction).
			WithAmount(decimal.NewFromInt(amount)).
			Build()
	}
	entry := func(key string, lines ...*domainJournal.Line) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_JOURNAL_ENTRY)).
			WithEventType(types.JournalEventInvoiceFinalized).
			WithSource(types.RevRecEntityTypeInvoice, "inv_"+runID).
			WithIdempotencyKey(key).
			WithAccountingPeriodID("acct_period_" + runID).
			WithBusinessDate(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).
			WithCustomerID("cust_" + runID).
			WithCurrency("usd").
			WithLines(lines).
			WithCreatedAt(time.Now().UTC()).
			Build()
	}
	balanced := func() []*domainJournal.Line {
		return []*domainJournal.Line{
			line(types.LedgerAccountReceivable, types.JournalDirectionDebit, 118),
			line(types.LedgerAccountDeferredRevenue, types.JournalDirectionCredit, 100),
			line(types.LedgerAccountTaxPayable, types.JournalDirectionCredit, 18),
		}
	}
	key := "invoice_finalized:inv_" + runID
	first := entry(key, balanced()...)

	tests := []struct {
		name    string
		entry   *domainJournal.Entry
		wantID  string
		wantErr bool
	}{
		{name: "first post", entry: first, wantID: first.ID()},
		{name: "same key returns the posted entry", entry: entry(key, balanced()...), wantID: first.ID()},
		{name: "no key never conflicts", entry: entry("", balanced()...)},
		{name: "no key never conflicts again", entry: entry("", balanced()...)},
		{name: "unbalanced", entry: entry("other:"+runID,
			line(types.LedgerAccountReceivable, types.JournalDirectionDebit, 10),
			line(types.LedgerAccountRevenue, types.JournalDirectionCredit, 9)), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.CreateWithLines(ctx, tt.entry)
			if tt.wantErr {
				require.True(t, ierr.IsValidation(err), err)
				return
			}
			id := tt.entry.ID()
			if tt.wantID != "" && tt.wantID != id {
				require.True(t, ierr.IsAlreadyExists(err), err)
				id, err = repo.GetIDByIdempotencyKey(ctx, key)
			}
			require.NoError(t, err)
			require.Equal(t, lo.Ternary(tt.wantID == "", tt.entry.ID(), tt.wantID), id)
			lines, err := client.Reader(ctx).JournalLine.Query().Where(journalline.JournalEntryID(id)).All(ctx)
			require.NoError(t, err)
			require.Len(t, lines, len(tt.entry.Lines()), "a repeat writes no new lines")
		})
	}

	got, err := repo.Get(ctx, first.ID())
	require.NoError(t, err)
	require.Len(t, got.Lines(), 3)
	require.Equal(t, key, got.IdempotencyKey())

	count, err := repo.Count(ctx, NewFilter())
	require.NoError(t, err)
	require.Equal(t, 3, count)

	byAccount := NewFilter()
	byAccount.Accounts = []types.LedgerAccountKey{types.LedgerAccountTaxPayable}
	byAccount.SourceEntityIDs = []string{"inv_" + runID}
	listed, err := repo.List(ctx, byAccount)
	require.NoError(t, err)
	require.Len(t, listed, 3)
	require.Len(t, listed[0].Lines(), 3)

	byAccount.Accounts = []types.LedgerAccountKey{types.LedgerAccountCash}
	listed, err = repo.List(ctx, byAccount)
	require.NoError(t, err)
	require.Empty(t, listed)
}

// TestSumLines: acme invoices 1,300 in Jan and earns 200; in Feb beta invoices 100 and each earns 100.
func TestSumLines(t *testing.T) {
	ctx, client, log, _ := newPGClient(t)
	repo := NewRepository(client, log)
	periods := accountingPeriodRepo.NewRepository(client, log)

	month := func(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, time.UTC) }
	period := func(m time.Month) string {
		p, err := periods.GetOrCreate(ctx, month(m, 1))
		require.NoError(t, err)
		return p.ID()
	}
	jan, feb, mar := period(time.January), period(time.February), period(time.March)
	now := time.Now().UTC()

	line := func(account types.LedgerAccountKey, direction types.JournalDirection, amount int64, pobID string) *domainJournal.Line {
		return domainJournal.NewLineBuilder(nil).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_JOURNAL_LINE)).
			WithAccount(account).
			WithDirection(direction).
			WithAmount(decimal.NewFromInt(amount)).
			WithPOBID(pobID).
			Build()
	}
	debit := func(account types.LedgerAccountKey, amount int64, pobID string) *domainJournal.Line {
		return line(account, types.JournalDirectionDebit, amount, pobID)
	}
	credit := func(account types.LedgerAccountKey, amount int64, pobID string) *domainJournal.Line {
		return line(account, types.JournalDirectionCredit, amount, pobID)
	}
	post := func(eventType types.JournalEventType, periodID string, businessDate time.Time, customerID, contractID, currency string, createdAt time.Time, lines ...*domainJournal.Line) {
		require.NoError(t, repo.CreateWithLines(ctx, domainJournal.NewBuilder(nil).
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

	febDeferred := func(edit func(f *SumFilter)) *SumFilter {
		f := &SumFilter{
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
		filter *SumFilter
		want   []string
	}{
		{
			name:   "opening folds January, February by event type",
			filter: febDeferred(func(f *SumFilter) {}),
			want: []string{
				"opening|deferred_revenue|||||1100",
				"2026-02|deferred_revenue|invoice_finalized||||100",
				"2026-02|deferred_revenue|revenue_offset||||-200",
			},
		},
		{
			name: "grouped by customer and contract",
			filter: febDeferred(func(f *SumFilter) {
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
			filter: febDeferred(func(f *SumFilter) {
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
			filter: febDeferred(func(f *SumFilter) { f.CustomerIDs = []string{"cust_beta"} }),
			want: []string{
				"2026-02|deferred_revenue|invoice_finalized||||100",
				"2026-02|deferred_revenue|revenue_offset||||-100",
			},
		},
		{
			name:   "exclude opening: only the requested months",
			filter: febDeferred(func(f *SumFilter) { f.ExcludeOpening = true }),
			want: []string{
				"2026-02|deferred_revenue|invoice_finalized||||100",
				"2026-02|deferred_revenue|revenue_offset||||-200",
			},
		},
		{
			name:   "without as_of, later entries count",
			filter: febDeferred(func(f *SumFilter) { f.CreatedAtLTE = nil }),
			want: []string{
				"opening|deferred_revenue|||||1100",
				"2026-02|deferred_revenue|invoice_finalized||||100",
				"2026-02|deferred_revenue|revenue_offset||||-250",
			},
		},
		{
			name: "a line counts in its entry's period, not its business date",
			filter: febDeferred(func(f *SumFilter) {
				f.PeriodStart = month(time.January, 1)
				f.Accounts = []types.LedgerAccountKey{types.LedgerAccountContraRevenue}
			}),
			want: []string{"2026-02|contra_revenue|credit_note_finalized||||-10"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sums, err := repo.SumLines(ctx, tt.filter)
			require.NoError(t, err)
			require.Equal(t, tt.want, lo.Map(sums, func(s *LineSum, _ int) string { return sumRow(s) }))
		})
	}
}

// sumRow renders a LineSum as period|account|event|customer|contract|pob|net.
func sumRow(s *LineSum) string {
	period := lo.TernaryF(s.PeriodStart == nil, func() string { return "opening" }, func() string { return s.PeriodStart.Format("2006-01") })
	return strings.Join([]string{
		period, string(s.Account), string(s.EventType),
		lo.FromPtr(s.CustomerID), lo.FromPtr(s.ContractID), lo.FromPtr(s.POBID), s.Net.String(),
	}, "|")
}
