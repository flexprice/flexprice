//go:build pgintegration

package journal

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent/journalline"
	"github.com/flexprice/flexprice/internal/config"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCreateWithLines(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	runID := types.GenerateUUIDWithPrefix("jt")
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_"+runID)
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
