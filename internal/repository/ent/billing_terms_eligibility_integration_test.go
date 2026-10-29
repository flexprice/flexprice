package ent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type eligibilityLockRecorder struct {
	postgres.IClient
	keys []string
}

func (c *eligibilityLockRecorder) LockWithWait(ctx context.Context, req postgres.LockRequest) error {
	c.keys = append(c.keys, req.Key)
	return c.IClient.LockWithWait(ctx, req)
}

func TestBillingTermsOwnerLockKeyCompatibility(t *testing.T) {
	owner := domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: "owner"}
	require.Equal(t, "billingterms:owner:6:tenant3:env10:commitment5:owner", billingOwnerLockKey("tenant", "env", owner))
	require.NotEqual(t,
		billingScopedLockKey("eligibility", "a:b", "c", "subscription", "d"),
		billingScopedLockKey("eligibility", "a", "b:c", "subscription", "d"),
	)
}

func TestBillingTermsEligibilityValidationPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("eligibility_tenant", "eligibility_environment")
	recorder := &eligibilityLockRecorder{IClient: f.client}
	store := NewBillingTermsExecutionStore(recorder)
	valid := domain.EligibilityScope{Type: "subscription", ID: "subscription_a"}
	var typedNil *struct{ context.Context }
	for _, missing := range []context.Context{nil, typedNil, context.Background(), billingTestContext("eligibility_tenant", ""), billingTestContext("", "eligibility_environment")} {
		require.Error(t, store.LockEligibility(missing, []domain.EligibilityScope{valid}))
	}
	require.Error(t, store.LockEligibility(ctx, nil), "even an empty guard set requires the caller transaction")
	for _, invalid := range []domain.EligibilityScope{
		{ID: valid.ID}, {Type: " \t", ID: valid.ID}, {Type: strings.Repeat("a", 51), ID: valid.ID},
		{Type: valid.Type}, {Type: valid.Type, ID: " \t"}, {Type: valid.Type, ID: strings.Repeat("a", 51)},
		{Type: strings.Repeat("é", 26), ID: valid.ID},
	} {
		require.NoError(t, store.WithTx(ctx, func(tx context.Context) error {
			err := store.LockEligibility(tx, []domain.EligibilityScope{valid, invalid})
			require.True(t, ierr.IsValidation(err), "%v", err)
			require.Empty(t, recorder.keys, "the complete set is validated before its first lock")
			return nil
		}))
	}
	tooMany := make([]domain.EligibilityScope, domain.MaxEligibilityScopes+1)
	for index := range tooMany {
		tooMany[index] = valid
	}
	require.NoError(t, store.WithTx(ctx, func(tx context.Context) error {
		require.Error(t, store.LockEligibility(tx, tooMany), "submitted entries are bounded before deduplication")
		require.Empty(t, recorder.keys)
		require.NoError(t, store.LockEligibility(tx, nil))
		require.NoError(t, store.LockEligibility(tx, []domain.EligibilityScope{}))
		return nil
	}))
	boundary := domain.EligibilityScope{Type: strings.Repeat("t", 50), ID: strings.Repeat("i", 50)}
	for index := range tooMany[:domain.MaxEligibilityScopes] {
		tooMany[index] = boundary
	}
	require.NoError(t, store.WithTx(ctx, func(tx context.Context) error {
		return store.LockEligibility(tx, tooMany[:domain.MaxEligibilityScopes])
	}))
	require.Len(t, recorder.keys, 1, "duplicate scopes acquire one real PostgreSQL lock")
}

func TestBillingTermsEligibilityOrderingPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx, cancel := context.WithTimeout(billingTestContext("ordering_tenant", "ordering_environment"), 10*time.Second)
	defer cancel()
	recorder := &eligibilityLockRecorder{IClient: f.client}
	store := NewBillingTermsExecutionStore(recorder)
	a := domain.EligibilityScope{Type: "a", ID: "one"}
	b := domain.EligibilityScope{Type: "a", ID: "two"}
	c := domain.EligibilityScope{Type: "b", ID: "one"}
	input := []domain.EligibilityScope{c, b, a, b, a}
	original := append([]domain.EligibilityScope(nil), input...)
	require.NoError(t, store.WithTx(ctx, func(tx context.Context) error { return store.LockEligibility(tx, input) }))
	require.Equal(t, original, input)
	require.Equal(t, []string{
		billingScopedLockKey("eligibility", types.GetTenantID(ctx), types.GetEnvironmentID(ctx), a.Type, a.ID),
		billingScopedLockKey("eligibility", types.GetTenantID(ctx), types.GetEnvironmentID(ctx), b.Type, b.ID),
		billingScopedLockKey("eligibility", types.GetTenantID(ctx), types.GetEnvironmentID(ctx), c.Type, c.ID),
	}, recorder.keys)

	store = NewBillingTermsExecutionStore(f.client)
	start, done := make(chan struct{}), make(chan error, 2)
	var active atomic.Int32
	for _, scopes := range [][]domain.EligibilityScope{{c, b, a, a}, {a, b, c, c}} {
		go func(scopes []domain.EligibilityScope) {
			<-start
			done <- store.WithTx(ctx, func(tx context.Context) error {
				if err := store.LockEligibility(tx, scopes); err != nil {
					return err
				}
				if active.Add(1) != 1 {
					return errors.New("eligibility critical sections overlapped")
				}
				defer active.Add(-1)
				select {
				case <-time.After(10 * time.Millisecond):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}(scopes)
	}
	close(start)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
}

func TestBillingTermsEligibilityIsolationPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx, cancel := context.WithTimeout(billingTestContext("isolation_tenant", "isolation_environment"), 10*time.Second)
	defer cancel()
	store := NewBillingTermsExecutionStore(f.client)
	scope := domain.EligibilityScope{Type: "subscription", ID: "subscription_a"}
	locked, release, firstDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	failure := errors.New("roll back the guarded lifecycle mutation")
	go func() {
		firstDone <- store.WithTx(ctx, func(tx context.Context) error {
			if err := store.LockEligibility(tx, []domain.EligibilityScope{scope}); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return failure
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-locked:
	case err := <-firstDone:
		t.Fatalf("initial eligibility lock failed: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	for _, different := range []struct {
		ctx   context.Context
		scope domain.EligibilityScope
	}{
		{billingTestContext("another_tenant", "isolation_environment"), scope},
		{billingTestContext("isolation_tenant", "another_environment"), scope},
		{ctx, domain.EligibilityScope{Type: "another_type", ID: scope.ID}},
	} {
		other, otherCancel := context.WithTimeout(different.ctx, time.Second)
		err := store.WithTx(other, func(tx context.Context) error {
			return store.LockEligibility(tx, []domain.EligibilityScope{different.scope})
		})
		otherCancel()
		require.NoError(t, err)
	}
	require.NoError(t, store.WithTx(ctx, func(tx context.Context) error {
		return store.LockOwner(tx, domain.OwnerRef{Type: types.BillingTermOwnerType(scope.Type), ID: scope.ID})
	}), "owner and eligibility locks have separate namespaces")

	secondAcquired, secondDone := make(chan struct{}), make(chan error, 1)
	go func() {
		secondDone <- store.WithTx(ctx, func(tx context.Context) error {
			if err := store.LockEligibility(tx, []domain.EligibilityScope{scope, scope}); err != nil {
				return err
			}
			close(secondAcquired)
			return nil
		})
	}()
	select {
	case <-secondAcquired:
		t.Fatal("the same eligibility scope entered before rollback")
	case err := <-secondDone:
		t.Fatalf("second eligibility lock failed before rollback: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	released = true
	require.ErrorIs(t, <-firstDone, failure)
	require.NoError(t, <-secondDone)
	<-secondAcquired
}
