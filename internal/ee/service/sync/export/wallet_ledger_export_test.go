package export

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletLedgerCSVRow(t *testing.T) {
	d := func(v string) decimal.Decimal { return decimal.RequireFromString(v) }
	created := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	paid := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		row  wallet.WalletLedgerExportData
		want WalletLedgerCSV
	}{
		{
			name: "purchase carries what was paid and when it was paid",
			row: wallet.WalletLedgerExportData{
				Type: types.TransactionTypeCredit, Credits: d("500"), AmountPaid: lo.ToPtr(d("450.004")),
				TopUpAmountRemainingBefore: d("0"), TopUpAmountRemainingAfter: d("450.004"),
				EffectiveAt: paid, CreatedAt: created,
			},
			want: WalletLedgerCSV{Credits: "500", AmountPaid: "450", TopUpAmountUsed: "", TopUpAmountRemainingAfter: "450", EffectiveAt: "2026-10-03T12:00:00Z", CreatedAt: "2026-10-01T00:00:00Z"},
		},
		{
			name: "adjustment has no paid amount",
			row:  wallet.WalletLedgerExportData{Type: types.TransactionTypeCredit, Credits: d("40")},
			want: WalletLedgerCSV{Credits: "40", AmountPaid: ""},
		},
		{
			name: "debit is negative and takes its share of the batch",
			row: wallet.WalletLedgerExportData{
				Type: types.TransactionTypeDebit, Credits: d("200"),
				WalletTxnCredits: lo.ToPtr(d("500")), WalletTxnAmountPaid: lo.ToPtr(d("450")),
				InvoicePeriodStart: lo.ToPtr(created), InvoicePeriodEnd: lo.ToPtr(paid),
			},
			want: WalletLedgerCSV{Credits: "-200", AmountPaid: "", TopUpAmountUsed: "180", InvoicePeriodStart: "2026-10-01T00:00:00Z", InvoicePeriodEnd: "2026-10-03T12:00:00Z"},
		},
		{
			name: "debit from credits with no paid amount",
			row:  wallet.WalletLedgerExportData{Type: types.TransactionTypeDebit, Credits: d("5")},
			want: WalletLedgerCSV{Credits: "-5", TopUpAmountUsed: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.row.Currency = "usd"
			got := toWalletLedgerCSV(&tt.row)
			assert.Equal(t, string(tt.row.Type), got.Type)
			assert.Equal(t, tt.want.Credits, got.Credits)
			assert.Equal(t, tt.want.AmountPaid, got.AmountPaid)
			assert.Equal(t, tt.want.TopUpAmountUsed, got.TopUpAmountUsed)
			assert.Equal(t, tt.want.InvoicePeriodStart, got.InvoicePeriodStart)
			assert.Equal(t, tt.want.InvoicePeriodEnd, got.InvoicePeriodEnd)
			if tt.want.TopUpAmountRemainingAfter != "" {
				assert.Equal(t, tt.want.TopUpAmountRemainingAfter, got.TopUpAmountRemainingAfter)
			}
			if tt.want.EffectiveAt != "" {
				assert.Equal(t, tt.want.EffectiveAt, got.EffectiveAt)
				assert.Equal(t, tt.want.CreatedAt, got.CreatedAt)
			}
		})
	}
}

// ledgerRepo records the ledger reads and returns one row per wallet asked for.
type ledgerRepo struct {
	wallet.Repository
	walletIDs   []string
	customerIDs []string
	batches     [][]string
}

func (r *ledgerRepo) ListWalletIDsForLedgerExport(_ context.Context, _, _ string, _, _ time.Time, customerExternalIDs []string) ([]string, error) {
	r.customerIDs = customerExternalIDs
	return r.walletIDs, nil
}

func (r *ledgerRepo) GetWalletLedgerForExport(_ context.Context, _, _ string, walletIDs []string, _, _ time.Time) ([]*wallet.WalletLedgerExportData, error) {
	r.batches = append(r.batches, walletIDs)
	return lo.Map(walletIDs, func(id string, _ int) *wallet.WalletLedgerExportData {
		return &wallet.WalletLedgerExportData{WalletID: id, Type: types.TransactionTypeCredit, Currency: "usd"}
	}), nil
}

func TestWalletLedgerWriteDataBatchesWallets(t *testing.T) {
	repo := &ledgerRepo{walletIDs: lo.Times(51, func(i int) string { return fmt.Sprintf("wallet_%02d", i) })}
	exporter := NewWalletLedgerExporter(repo, logger.NewNoopLogger())

	var buf bytes.Buffer
	count, err := exporter.WriteData(context.Background(), &dto.ExportRequest{CustomerExternalIDs: []string{"acme"}}, &buf)
	require.NoError(t, err)

	assert.Equal(t, []string{"acme"}, repo.customerIDs)
	require.Len(t, repo.batches, 2)
	assert.Len(t, repo.batches[0], 50)
	assert.Equal(t, []string{"wallet_50"}, repo.batches[1])
	assert.Equal(t, 51, count)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 52, "one header and one line per row")
	assert.True(t, strings.HasPrefix(lines[0], "transaction_id,"))
	assert.Len(t, lo.Filter(lines, func(l string, _ int) bool { return strings.HasPrefix(l, "transaction_id,") }), 1, "header written once")
}

func TestWalletLedgerWriteDataNoWallets(t *testing.T) {
	exporter := NewWalletLedgerExporter(&ledgerRepo{}, logger.NewNoopLogger())

	var buf bytes.Buffer
	count, err := exporter.WriteData(context.Background(), &dto.ExportRequest{}, &buf)
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.True(t, strings.HasPrefix(buf.String(), "transaction_id,"), "an empty export still has its header")
}

func TestExportObjectKeyCustomerFilter(t *testing.T) {
	exporter := &WalletLedgerExporter{}
	request := func(customers ...string) *dto.ExportRequest {
		return &dto.ExportRequest{
			StartTime:           time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			EndTime:             time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
			JobConfig:           &types.S3JobConfig{},
			CustomerExternalIDs: customers,
		}
	}

	full := exportObjectKey(request(), exporter)
	assert.NotContains(t, full, "customers")
	acme := exportObjectKey(request("acme"), exporter)
	assert.NotEqual(t, full, acme, "a filtered run never replaces the full export")
	assert.NotEqual(t, acme, exportObjectKey(request("globex"), exporter))
	assert.Equal(t, exportObjectKey(request("acme", "globex"), exporter), exportObjectKey(request("globex", "acme"), exporter))
}
