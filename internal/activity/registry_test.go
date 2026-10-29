package activity

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/checkoutsession"
	"github.com/flexprice/flexprice/ent/commitment"
	"github.com/flexprice/flexprice/ent/creditnote"
	"github.com/flexprice/flexprice/ent/customer"
	"github.com/flexprice/flexprice/ent/feature"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/payment"
	"github.com/flexprice/flexprice/ent/paymentmethod"
	"github.com/flexprice/flexprice/ent/plan"
	"github.com/flexprice/flexprice/ent/price"
	"github.com/flexprice/flexprice/ent/refund"
	"github.com/flexprice/flexprice/ent/subscription"
	"github.com/flexprice/flexprice/ent/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

var entColumns = map[types.SystemEntityType][]string{
	types.SystemEntityTypeCustomer:        customer.Columns,
	types.SystemEntityTypeCommitment:      commitment.Columns,
	types.SystemEntityTypeSubscription:    subscription.Columns,
	types.SystemEntityTypeInvoice:         invoice.Columns,
	types.SystemEntityTypeCreditNote:      creditnote.Columns,
	types.SystemEntityTypeWallet:          wallet.Columns,
	types.SystemEntityTypePayment:         payment.Columns,
	types.SystemEntityTypePaymentMethod:   paymentmethod.Columns,
	types.SystemEntityTypeRefund:          refund.Columns,
	types.SystemEntityTypeCheckoutSession: checkoutsession.Columns,
	types.SystemEntityTypePlan:            plan.Columns,
	types.SystemEntityTypePrice:           price.Columns,
	types.SystemEntityTypeFeature:         feature.Columns,
}

// bareRow returns an empty ent row for the registration, as a RETURNING write produces (no edges).
func bareRow(t *testing.T, name string) any {
	t.Helper()
	rows := map[string]any{
		"Commitment": &ent.Commitment{ID: "x"}, "Customer": &ent.Customer{ID: "x"}, "Subscription": &ent.Subscription{ID: "x"}, "Invoice": &ent.Invoice{ID: "x"},
		"CreditNote": &ent.CreditNote{ID: "x"}, "Wallet": &ent.Wallet{ID: "x"}, "Payment": &ent.Payment{ID: "x"},
		"PaymentMethod": &ent.PaymentMethod{ID: "x"}, "Refund": &ent.Refund{ID: "x"}, "CheckoutSession": &ent.CheckoutSession{ID: "x"},
		"Plan": &ent.Plan{ID: "x"}, "Price": &ent.Price{ID: "x"}, "Feature": &ent.Feature{ID: "x"},
	}
	row, ok := rows[name]
	require.True(t, ok, name)
	return row
}

func declaredConstants(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "actions.go", nil, 0)
	require.NoError(t, err)
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		if spec, ok := n.(*ast.ValueSpec); ok {
			for _, v := range spec.Values {
				s, err := strconv.Unquote(v.(*ast.BasicLit).Value)
				require.NoError(t, err)
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

func TestRegistry_ActionNames(t *testing.T) {
	reg := Registrations()
	shape := regexp.MustCompile(`^[a-z_]+\.[a-z_]+$`)
	seen := map[Action]bool{}
	var declared []string
	for entityType := range entColumns {
		r, ok := reg.ByType(entityType)
		require.True(t, ok, entityType)
		for _, action := range r.Actions {
			require.Regexp(t, shape, action)
			prefix, verb, _ := strings.Cut(string(action), ".")
			require.Equal(t, string(entityType), prefix, action)
			pastTense := slices.ContainsFunc(strings.Split(verb, "_"), func(w string) bool { return strings.HasSuffix(w, "ed") || w == "paid" })
			require.True(t, pastTense, "%s is not past tense", action)
			require.False(t, seen[action], "duplicate %s", action)
			seen[action] = true
			declared = append(declared, string(action))
		}
	}
	slices.Sort(declared)
	consts := declaredConstants(t)
	slices.Sort(consts)
	require.Equal(t, declared, consts)
}

func TestRegistry_SnapshotKeys(t *testing.T) {
	reg := Registrations()
	for entityType := range entColumns {
		r, _ := reg.ByType(entityType)
		keys := map[string]bool{}
		for _, f := range r.Snapshot.fields {
			keys[f.key] = true
		}
		var referenced []string
		referenced = append(referenced, r.Label...)
		for _, key := range referenced {
			require.True(t, keys[key], "%s: snapshot has no key %q", entityType, key)
		}
	}
}

func TestRegistry_SnapshotBareRow(t *testing.T) {
	reg := Registrations()
	for entityType := range entColumns {
		r, _ := reg.ByType(entityType)
		t.Run(string(entityType), func(t *testing.T) {
			require.NotPanics(t, func() {
				state, ok := r.Snapshot.state(bareRow(t, r.EntName))
				require.True(t, ok)
				_, err := json.Marshal(state)
				require.NoError(t, err)
			})
		})
	}
}

func TestLabel(t *testing.T) {
	customer := RegistrationEntry(t, types.SystemEntityTypeCustomer)
	require.Equal(t, "ext", customer.label(map[string]any{"external_id": "ext"}))
	require.Equal(t, "", customer.label(map[string]any{"name": "Ada"}))
	subscription := RegistrationEntry(t, types.SystemEntityTypeSubscription)
	require.Equal(t, "", subscription.label(map[string]any{"name": "x"}))
}
