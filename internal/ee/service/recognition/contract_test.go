package recognition

import (
	"testing"
	"time"

	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestComputeMonth(t *testing.T) {
	d := func(v int64) decimal.Decimal { return decimal.NewFromInt(v) }
	tests := []struct {
		name          string
		pos           pobPosition
		earned        decimal.Decimal
		delta, offset decimal.Decimal
	}{
		{"example 1, Feb 1 run for January", pobPosition{billed: d(1200)}, d(100), d(100), d(100)},
		{"example 1, Feb 2 rerun", pobPosition{billed: d(1200), revenue: d(100), offset: d(100)}, d(100), d(0), d(0)},
		{"example 1, Mar 1 run for February", pobPosition{billed: d(1200), revenue: d(100), offset: d(100)}, d(200), d(100), d(100)},
		{"example 1, backdated cut of January to 90", pobPosition{billed: d(1200), revenue: d(100), offset: d(100)}, d(90), d(-10), d(-10)},
		{"example 2, January: nothing billed by Jan 31", pobPosition{}, d(300), d(300), d(0)},
		{"example 2, February: 300 billed by Feb 28", pobPosition{billed: d(300), revenue: d(300)}, d(550), d(250), d(300)},
		{"a void after the offset gives it back", pobPosition{billed: d(0), revenue: d(100), offset: d(100)}, d(100), d(0), d(-100)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta, offset := computeMonth(tt.pos, tt.earned)
			require.Equal(t, tt.delta.String(), delta.String(), "delta")
			require.Equal(t, tt.offset.String(), offset.String(), "offset")
		})
	}
}

func TestPositionAt(t *testing.T) {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	entry := func(eventType types.JournalEventType, date time.Time, postings ...domainJournal.Posting) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).WithEventType(eventType).WithBusinessDate(date).WithLines(domainJournal.LinesFrom(postings)).Build()
	}
	deferred := func(amount int64, pobID string) domainJournal.Posting {
		return domainJournal.Posting{Account: types.LedgerAccountDeferredRevenue, Amount: decimal.NewFromInt(amount), POBID: pobID}
	}
	unbilled := func(amount int64) domainJournal.Posting {
		return domainJournal.Posting{Account: types.LedgerAccountUnbilledReceivable, Amount: decimal.NewFromInt(amount), POBID: "pob_1"}
	}
	revenue := func(amount int64) domainJournal.Posting {
		return domainJournal.Posting{Account: types.LedgerAccountRevenue, Amount: decimal.NewFromInt(amount), POBID: "pob_1"}
	}
	receivable := domainJournal.Posting{Account: types.LedgerAccountReceivable, Amount: decimal.NewFromInt(-1200)}

	entries := []*domainJournal.Entry{
		entry(types.JournalEventInvoiceFinalized, day(1, 1), receivable, deferred(1200, "pob_1")),
		entry(types.JournalEventInvoiceFinalized, day(1, 5), domainJournal.Posting{Account: types.LedgerAccountReceivable, Amount: decimal.NewFromInt(-50)}, deferred(50, "pob_2")),
		entry(types.JournalEventRevenueEarned, day(1, 31), unbilled(-100), revenue(100)),
		entry(types.JournalEventRevenueOffset, day(1, 31), deferred(-100, "pob_1"), unbilled(100)),
		entry(types.JournalEventInvoiceVoided, day(2, 10), deferred(-1200, "pob_1"), domainJournal.Posting{Account: types.LedgerAccountReceivable, Amount: decimal.NewFromInt(1200)}),
		entry(types.JournalEventRevenueEarned, day(2, 28), unbilled(-90), revenue(90)),
	}

	tests := []struct {
		name                    string
		monthEnd                time.Time
		billed, revenue, offset int64
	}{
		{"before anything", day(12, 31).AddDate(-1, 0, 0), 0, 0, 0},
		{"January: invoice, recognition and offset; other POBs ignored", day(1, 31), 1200, 100, 100},
		{"February: the void nets billed to zero", day(2, 28), 0, 190, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := positionAt(entries, "pob_1", tt.monthEnd)
			require.Equal(t, decimal.NewFromInt(tt.billed).String(), pos.billed.String(), "billed")
			require.Equal(t, decimal.NewFromInt(tt.revenue).String(), pos.revenue.String(), "revenue")
			require.Equal(t, decimal.NewFromInt(tt.offset).String(), pos.offset.String(), "offset")
		})
	}
}
