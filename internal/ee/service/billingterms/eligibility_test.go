package billingterms

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestOwnerOperationLocksCompleteEligibilityBeforeOwner(t *testing.T) {
	ctx, core, db := newCore(t)
	first := domain.EligibilityScope{Type: "subscription", ID: "a"}
	second := domain.EligibilityScope{Type: "subscription", ID: "b"}
	parent := domain.EligibilityScope{Type: "parent", ID: "p"}
	db.owner.EligibilityScopes = []domain.EligibilityScope{second, first, second, parent}
	installed, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	require.Equal(t, []string{"snapshot", "eligibility", "owner", "snapshot"}, db.lockOrder)
	require.Equal(t, [][]domain.EligibilityScope{{parent, first, second}}, db.eligibilityLocks)
	require.Equal(t, []domain.EligibilityScope{parent, first, second}, installed.Owner().EligibilityScopes)

	exposed := installed.Owner()
	exposed.EligibilityScopes[0].ID = "external-mutation"
	require.Equal(t, parent, installed.Owner().EligibilityScopes[0])
	require.Equal(t, parent, db.owner.EligibilityScopes[3])

	db.lockOrder = nil
	db.eligibilityLocks = nil
	noop, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: annualRows(), ExpectedState: "older-state"})
	require.NoError(t, err)
	require.True(t, noop.Noop())
	require.Equal(t, sourceIDs(installed), sourceIDs(noop))
	require.Equal(t, []string{"snapshot", "eligibility", "owner", "snapshot"}, db.lockOrder)
	require.Equal(t, [][]domain.EligibilityScope{{parent, first, second}}, db.eligibilityLocks)
}

func TestEligibilityScopeDriftAbortsWithoutLateLocks(t *testing.T) {
	ctx, core, db := newCore(t)
	initial := domain.EligibilityScope{Type: "subscription", ID: "initial"}
	changed := domain.EligibilityScope{Type: "subscription", ID: "changed"}
	db.owner.EligibilityScopes = []domain.EligibilityScope{initial}
	db.afterEligibility = func() { db.owner.EligibilityScopes = []domain.EligibilityScope{changed} }
	_, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
	require.Equal(t, []string{"snapshot", "eligibility", "owner", "snapshot"}, db.lockOrder)
	require.Equal(t, [][]domain.EligibilityScope{{initial}}, db.eligibilityLocks)
	require.Empty(t, db.rules)
	require.Empty(t, db.items)
}

func TestEligibilityComparisonUsesScopeSetAndFreshOwner(t *testing.T) {
	ctx, core, db := newCore(t)
	first := domain.EligibilityScope{Type: "subscription", ID: "a"}
	second := domain.EligibilityScope{Type: "subscription", ID: "b"}
	db.owner.EligibilityScopes = []domain.EligibilityScope{second, first, second}
	db.afterEligibility = func() {
		db.owner.EligibilityScopes = []domain.EligibilityScope{first, second, first}
		db.owner.OriginalTotal = decimal.NewFromInt(600)
		db.owner.CollectionTarget = decimal.NewFromInt(600)
	}
	rows := []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder}}
	installed, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: rows})
	require.NoError(t, err)
	require.Equal(t, "600", installed.Summary().ScheduledAmount.String())
	require.Equal(t, []string{"snapshot", "eligibility", "owner", "snapshot"}, db.lockOrder)
	require.Equal(t, [][]domain.EligibilityScope{{first, second}}, db.eligibilityLocks)
}

func TestEligibilityScopeSetParticipatesInStateToken(t *testing.T) {
	ctx, core, db := newCore(t)
	first := domain.EligibilityScope{Type: "subscription", ID: "a"}
	second := domain.EligibilityScope{Type: "subscription", ID: "b"}
	db.owner.EligibilityScopes = []domain.EligibilityScope{second, first, first}
	installed, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	db.owner.EligibilityScopes = []domain.EligibilityScope{first, second}
	reordered, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, installed.StateToken(), reordered.StateToken())
	db.owner.EligibilityScopes = []domain.EligibilityScope{first}
	changed, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.NotEqual(t, installed.StateToken(), changed.StateToken())
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: replacementRows(db.owner), ExpectedState: installed.StateToken()})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
}

func TestEligibilityValidatesWholeSetBeforeLocking(t *testing.T) {
	tests := []struct {
		name   string
		scopes []domain.EligibilityScope
	}{
		{"blank type", []domain.EligibilityScope{{Type: " ", ID: "a"}}},
		{"blank id", []domain.EligibilityScope{{Type: "subscription", ID: " "}}},
		{"long type", []domain.EligibilityScope{{Type: strings.Repeat("t", 51), ID: "a"}}},
		{"long id", []domain.EligibilityScope{{Type: "subscription", ID: strings.Repeat("i", 51)}}},
		{"too many", make([]domain.EligibilityScope, domain.MaxEligibilityScopes+1)},
		{"invalid after valid", []domain.EligibilityScope{{Type: "subscription", ID: "a"}, {Type: "subscription"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, core, db := newCore(t)
			db.owner.EligibilityScopes = tt.scopes
			_, err := core.Get(ctx, db.owner.Owner)
			require.True(t, ierr.IsValidation(err), "%+v", err)
			require.Equal(t, []string{"snapshot"}, db.lockOrder)
			require.Empty(t, db.eligibilityLocks)
		})
	}
}

func TestEveryOwnerOperationUsesEligibilityBeforeOwner(t *testing.T) {
	for _, operation := range []string{"get", "preview", "replace", "trigger"} {
		t.Run(operation, func(t *testing.T) {
			ctx, core, db := newCore(t)
			db.owner.EligibilityScopes = []domain.EligibilityScope{{Type: "subscription", ID: "a"}}
			rows := []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: true}}
			initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: rows})
			require.NoError(t, err)
			db.lockOrder = nil
			switch operation {
			case "get":
				_, err = core.Get(ctx, db.owner.Owner)
			case "preview":
				_, err = core.Preview(ctx, PreviewRequest{Owner: db.owner.Owner, Rows: rows})
			case "replace":
				_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: rows, ExpectedState: initial.StateToken()})
			case "trigger":
				_, err = core.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner, ItemID: initial.Items()[0].ID, ExpectedState: initial.StateToken()})
			}
			require.NoError(t, err)
			require.Equal(t, []string{"snapshot", "eligibility", "owner", "snapshot"}, db.lockOrder)
		})
	}
}

func TestFreshOwnerPermissionsApplyBeforeNoop(t *testing.T) {
	ctx, core, db := newCore(t)
	db.owner.EligibilityScopes = []domain.EligibilityScope{{Type: "subscription", ID: "a"}}
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	db.afterEligibility = func() { db.owner.CanEdit = false }
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: annualRows(), ExpectedState: initial.StateToken()})
	require.True(t, ierr.IsValidation(err), "%+v", err)
	db.afterEligibility = nil
	db.owner.CanEdit = true
	current, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, initial.StateToken(), current.StateToken())
	require.Equal(t, sourceIDs(initial), sourceIDs(current))
}

func TestEligibilityMaximumScopeSet(t *testing.T) {
	ctx, core, db := newCore(t)
	for index := range domain.MaxEligibilityScopes {
		db.owner.EligibilityScopes = append(db.owner.EligibilityScopes, domain.EligibilityScope{Type: "subscription", ID: strconv.Itoa(index)})
	}
	current, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Len(t, current.Owner().EligibilityScopes, domain.MaxEligibilityScopes)
	require.Len(t, db.eligibilityLocks, 1)
	require.Len(t, db.eligibilityLocks[0], domain.MaxEligibilityScopes)
}

func TestStandaloneOwnerMayHaveNoEligibilityScopes(t *testing.T) {
	ctx, core, db := newCore(t)
	db.owner.Owner.Type = "fixed_fee"
	db.owner.SubscriptionID = nil
	db.owner.Delivery = types.BillingTermSeparateInvoice
	resolver, err := NewOwnerResolver(&memoryOwner{db: db})
	require.NoError(t, err)
	core.(*service).resolver = resolver
	rows := []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder}}
	installed, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: rows})
	require.NoError(t, err)
	require.Empty(t, installed.Owner().EligibilityScopes)
	require.Equal(t, [][]domain.EligibilityScope{{}}, db.eligibilityLocks)
	db.owner.EligibilityScopes = []domain.EligibilityScope{}
	unchanged, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, installed.StateToken(), unchanged.StateToken())
}

func TestEligibilityFailureAndWriteFailureLeaveInstalledSources(t *testing.T) {
	ctx, core, db := newCore(t)
	db.owner.EligibilityScopes = []domain.EligibilityScope{{Type: "subscription", ID: "a"}}
	installed, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	db.lockOrder = nil
	failure := errors.New("guard unavailable")
	db.eligibilityFailure = failure
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: replacementRows(db.owner), ExpectedState: installed.StateToken()})
	require.ErrorIs(t, err, failure)
	require.Equal(t, []string{"snapshot", "eligibility"}, db.lockOrder)
	db.eligibilityFailure = nil
	db.failCreate = true
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: replacementRows(db.owner), ExpectedState: installed.StateToken()})
	require.ErrorContains(t, err, "injected")
	db.failCreate = false
	current, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, installed.StateToken(), current.StateToken())
	require.Equal(t, sourceIDs(installed), sourceIDs(current))
}

func TestInvoiceVoidDoesNotAcquireEligibilityGuards(t *testing.T) {
	ctx, core, db, _, req := assignedVoidSource(t)
	db.lockOrder = nil
	db.eligibilityLocks = nil
	db.eligibilityFailure = errors.New("void must not acquire eligibility")
	err := db.WithTx(ctx, func(tx context.Context) error {
		return core.WithInvoiceVoid(tx, req, func(context.Context) error {
			voidMemoryInvoice(db, req.InvoiceID)
			return nil
		})
	})
	require.NoError(t, err)
	require.Equal(t, []string{"owner"}, db.lockOrder)
	require.Empty(t, db.eligibilityLocks)
}
