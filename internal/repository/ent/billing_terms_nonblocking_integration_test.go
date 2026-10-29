package ent

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/stretchr/testify/require"
)

func TestBillingTermsNonblockingEligibilityPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx, cancel := context.WithTimeout(billingTestContext("nonblocking_tenant", "nonblocking_environment"), 10*time.Second)
	defer cancel()
	store := NewBillingTermsExecutionStore(f.client)
	nonblocking := store.(domain.NonblockingEligibilityStore)
	a := domain.EligibilityScope{Type: "subscription", ID: "a"}
	b := domain.EligibilityScope{Type: "subscription", ID: "b"}
	input := []domain.EligibilityScope{b, a, a}
	original := append([]domain.EligibilityScope(nil), input...)
	err := store.WithTx(ctx, func(held context.Context) error {
		if err := store.LockEligibility(held, []domain.EligibilityScope{b}); err != nil {
			return err
		}
		busy := store.WithTx(ctx, func(contender context.Context) error {
			err := nonblocking.TryLockEligibility(contender, input)
			if !ierr.IsVersionConflict(err) {
				return errors.New("busy eligibility guard must immediately return a conflict")
			}
			probe := store.WithTx(ctx, func(probe context.Context) error {
				return nonblocking.TryLockEligibility(probe, []domain.EligibilityScope{a})
			})
			if !ierr.IsVersionConflict(probe) {
				return errors.New("canonical ordering must acquire a before discovering busy b")
			}
			return err
		})
		if !ierr.IsVersionConflict(busy) {
			return busy
		}
		if err := store.WithTx(ctx, func(probe context.Context) error {
			return nonblocking.TryLockEligibility(probe, []domain.EligibilityScope{a})
		}); err != nil {
			return err
		}
		other := billingTestContext("nonblocking_tenant", "another_environment")
		return store.WithTx(other, func(probe context.Context) error {
			return nonblocking.TryLockEligibility(probe, []domain.EligibilityScope{b})
		})
	})
	require.NoError(t, err)
	require.Equal(t, original, input)
	require.NoError(t, store.WithTx(ctx, func(tx context.Context) error {
		return nonblocking.TryLockEligibility(tx, []domain.EligibilityScope{b, a})
	}))
}

func TestBillingTermsNonblockingEligibilityValidationPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("validation_tenant", "validation_environment")
	store := NewBillingTermsExecutionStore(f.client)
	nonblocking := store.(domain.NonblockingEligibilityStore)
	valid := domain.EligibilityScope{Type: "subscription", ID: "a"}
	require.Error(t, nonblocking.TryLockEligibility(ctx, nil))
	require.Error(t, nonblocking.TryLockEligibility(nil, nil))
	tooMany := make([]domain.EligibilityScope, domain.MaxEligibilityScopes+1)
	for index := range tooMany {
		tooMany[index] = valid
	}
	for _, scopes := range [][]domain.EligibilityScope{{valid, {Type: "subscription", ID: " "}}, tooMany} {
		require.NoError(t, store.WithTx(ctx, func(tx context.Context) error {
			err := nonblocking.TryLockEligibility(tx, scopes)
			if !ierr.IsValidation(err) {
				return errors.New("invalid eligibility scopes must fail validation")
			}
			return store.WithTx(ctx, func(probe context.Context) error {
				return nonblocking.TryLockEligibility(probe, []domain.EligibilityScope{valid})
			})
		}))
	}
	require.NoError(t, store.WithTx(ctx, func(tx context.Context) error {
		return nonblocking.TryLockEligibility(tx, tooMany[:domain.MaxEligibilityScopes])
	}))
}

func TestBillingTermsLegacyNonblockingLockPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx, cancel := context.WithTimeout(billingTestContext("legacy_tenant", "legacy_environment"), 5*time.Second)
	defer cancel()
	locker := f.client.(postgres.NonblockingLocker)
	require.Error(t, locker.TryLock(ctx, postgres.LockRequest{Key: "commitments:sub"}))
	require.Error(t, locker.TryLock(nil, postgres.LockRequest{Key: "commitments:sub"}))
	require.NoError(t, f.client.WithTx(ctx, func(held context.Context) error {
		if err := f.client.LockWithWait(held, postgres.LockRequest{Key: "commitments:sub"}); err != nil {
			return err
		}
		for _, contenderCtx := range []context.Context{ctx, billingTestContext("other_tenant", "other_environment")} {
			err := f.client.WithTx(contenderCtx, func(contender context.Context) error {
				return locker.TryLock(contender, postgres.LockRequest{Key: "commitments:sub"})
			})
			require.True(t, ierr.IsVersionConflict(err), "legacy keys must serialize old writers, including harmless cross-scope hash collisions: %v", err)
		}
		return nil
	}))
	require.NoError(t, f.client.WithTx(ctx, func(tx context.Context) error {
		require.Error(t, locker.TryLock(tx, postgres.LockRequest{}))
		return locker.TryLock(tx, postgres.LockRequest{Key: "commitments:sub"})
	}))
}
