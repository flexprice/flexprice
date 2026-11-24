package ent

import (
	"context"
	"errors"
	"testing"

	entgen "github.com/flexprice/flexprice/ent"
	walletdomain "github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
)

type txFlagKey struct{}

var errStubDB = errors.New("stub db")

// stubDriver counts failing reads.
type stubDriver struct{ queries int }

func (d *stubDriver) Exec(context.Context, string, any, any) error { return errStubDB }
func (d *stubDriver) Query(context.Context, string, any, any) error {
	d.queries++
	return errStubDB
}
func (d *stubDriver) Tx(context.Context) (dialect.Tx, error) { return nil, errStubDB }
func (d *stubDriver) Close() error                           { return nil }
func (d *stubDriver) Dialect() string                        { return dialect.Postgres }

// stubClient fakes an open transaction.
type stubClient struct {
	postgres.IClient
	ent *entgen.Client
}

func (c *stubClient) TxFromContext(ctx context.Context) *entgen.Tx {
	if ctx.Value(txFlagKey{}) != nil {
		return &entgen.Tx{}
	}
	return nil
}
func (c *stubClient) Reader(context.Context) *entgen.Client { return c.ent }
func (c *stubClient) Writer(context.Context) *entgen.Client { return c.ent }

func newCachedWalletRepo(t *testing.T) (*walletRepository, *stubDriver, context.Context) {
	drv := &stubDriver{}
	repo := &walletRepository{
		client:     &stubClient{ent: entgen.NewClient(entgen.Driver(drv))},
		redisCache: newFakeRedisCache(),
	}
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")
	repo.SetCache(ctx, &walletdomain.Wallet{ID: "wallet_1", CreditBalance: decimal.NewFromInt(5)})
	require.NotNil(t, repo.GetCache(ctx, "wallet_1"))
	return repo, drv, ctx
}

func TestGetWalletByID_InTxBypassesCache(t *testing.T) {
	repo, drv, ctx := newCachedWalletRepo(t)

	_, err := repo.GetWalletByID(context.WithValue(ctx, txFlagKey{}, true), "wallet_1")

	require.Error(t, err)
	require.Equal(t, 1, drv.queries)
}

func TestGetWalletByID_OutsideTxUsesCache(t *testing.T) {
	repo, drv, ctx := newCachedWalletRepo(t)

	w, err := repo.GetWalletByID(ctx, "wallet_1")

	require.NoError(t, err)
	require.True(t, decimal.NewFromInt(5).Equal(w.CreditBalance))
	require.Equal(t, 0, drv.queries)
}
