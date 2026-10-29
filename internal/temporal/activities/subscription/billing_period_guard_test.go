package subscription

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type billingPeriodTestDB struct{ postgres.IClient }

func (d *billingPeriodTestDB) TryLock(ctx context.Context, req postgres.LockRequest) error {
	return d.LockWithWait(ctx, req)
}

func (d *billingPeriodTestDB) WithTx(ctx context.Context, fn func(context.Context) error) error {
	if d.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	tx := types.WithTxStore(types.WithPostCommitHooks(context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})))
	if err := fn(tx); err != nil {
		types.DiscardPostCommitHooks(tx)
		return err
	}
	types.RunPostCommitHooks(tx)
	return nil
}

type billingPeriodTestStore struct{ domain.ExecutionStore }

func (*billingPeriodTestStore) InTransaction(ctx context.Context) bool {
	return ctx.Value(types.CtxDBTransaction) != nil
}
func (*billingPeriodTestStore) LockEligibility(context.Context, []domain.EligibilityScope) error {
	return nil
}
func (*billingPeriodTestStore) TryLockEligibility(context.Context, []domain.EligibilityScope) error {
	return nil
}
