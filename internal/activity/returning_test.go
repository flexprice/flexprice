package activity

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/checkoutsession"
	"github.com/flexprice/flexprice/ent/creditnote"
	"github.com/flexprice/flexprice/ent/customer"
	"github.com/flexprice/flexprice/ent/feature"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/payment"
	"github.com/flexprice/flexprice/ent/paymentmethod"
	"github.com/flexprice/flexprice/ent/plan"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/price"
	"github.com/flexprice/flexprice/ent/refund"
	"github.com/flexprice/flexprice/ent/subscription"
	"github.com/flexprice/flexprice/ent/wallet"
	"github.com/stretchr/testify/require"
)

func returningClient(f *recordingDriver) *ent.Client { return ent.NewClient(ent.Driver(f)) }

func quotedColumns(cols []string) string {
	return `"` + strings.Join(cols, `", "`) + `"`
}

func TestSaveReturning_SQLParity(t *testing.T) {
	ctx := context.Background()
	build := func(c *ent.Client) *ent.CheckoutSessionUpdate {
		return c.CheckoutSession.Update().Where(predicate.CheckoutSession(checkoutsession.IDIn("a", "b"))).SetFailureReason("x").SetUpdatedAt(fixedTime)
	}

	plain := newRecordingDriver()
	_, err := build(returningClient(plain)).Save(ctx)
	require.NoError(t, err)
	require.Len(t, plain.execs, 1)

	ret := newRecordingDriver()
	ret.columns = checkoutsession.Columns
	_, err = build(returningClient(ret)).SaveReturning(ctx)
	require.NoError(t, err)
	require.Empty(t, ret.execs)
	require.Len(t, ret.queries, 1)

	require.Equal(t, plain.execs[0].Query+" RETURNING "+quotedColumns(checkoutsession.Columns), ret.queries[0].Query)
	require.Equal(t, plain.execs[0].Args, ret.queries[0].Args)
}

func TestExecReturning_SQLParity(t *testing.T) {
	ctx := context.Background()
	build := func(c *ent.Client) *ent.CheckoutSessionDelete {
		return c.CheckoutSession.Delete().Where(predicate.CheckoutSession(checkoutsession.ID("a")))
	}

	plain := newRecordingDriver()
	_, err := build(returningClient(plain)).Exec(ctx)
	require.NoError(t, err)

	ret := newRecordingDriver()
	ret.columns = checkoutsession.Columns
	_, err = build(returningClient(ret)).ExecReturning(ctx)
	require.NoError(t, err)

	require.Equal(t, plain.execs[0].Query+" RETURNING "+quotedColumns(checkoutsession.Columns), ret.queries[0].Query)
	require.Equal(t, plain.execs[0].Args, ret.queries[0].Args)
}

func TestSaveReturning_HookReceivesRows(t *testing.T) {
	f := newRecordingDriver()
	f.columns = []string{checkoutsession.FieldID, checkoutsession.FieldFailureReason}
	f.rows = []map[string]any{
		{checkoutsession.FieldID: "cs_1", checkoutsession.FieldFailureReason: "x"},
		{checkoutsession.FieldID: "cs_2", checkoutsession.FieldFailureReason: "x"},
	}
	c := returningClient(f)

	var seen any
	c.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			v, err := next.Mutate(ctx, m)
			seen = v
			return v, err
		})
	})

	got, err := c.CheckoutSession.Update().Where(predicate.CheckoutSession(checkoutsession.IDIn("cs_1", "cs_2"))).SetFailureReason("x").SaveReturning(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "cs_1", got[0].ID)
	require.Equal(t, "x", *got[1].FailureReason)
	require.Equal(t, got, seen.([]*ent.CheckoutSession))
}

var fixedTime = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

func TestReturning_SQLParityAllNodes(t *testing.T) {
	ctx := context.Background()
	type node struct {
		name    string
		columns []string
		update  func(c *ent.Client) (plain func() error, ret func() error)
		del     func(c *ent.Client) (plain func() error, ret func() error)
	}
	nodes := []node{
		{"customer", customer.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.CustomerUpdate { return c.Customer.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.Customer.Delete().Exec(ctx); return e }, func() error { _, e := c.Customer.Delete().ExecReturning(ctx); return e }
			}},
		{"subscription", subscription.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.SubscriptionUpdate { return c.Subscription.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.Subscription.Delete().Exec(ctx); return e }, func() error { _, e := c.Subscription.Delete().ExecReturning(ctx); return e }
			}},
		{"invoice", invoice.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.InvoiceUpdate { return c.Invoice.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.Invoice.Delete().Exec(ctx); return e }, func() error { _, e := c.Invoice.Delete().ExecReturning(ctx); return e }
			}},
		{"credit_note", creditnote.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.CreditNoteUpdate { return c.CreditNote.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.CreditNote.Delete().Exec(ctx); return e }, func() error { _, e := c.CreditNote.Delete().ExecReturning(ctx); return e }
			}},
		{"wallet", wallet.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.WalletUpdate { return c.Wallet.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.Wallet.Delete().Exec(ctx); return e }, func() error { _, e := c.Wallet.Delete().ExecReturning(ctx); return e }
			}},
		{"payment", payment.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.PaymentUpdate { return c.Payment.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.Payment.Delete().Exec(ctx); return e }, func() error { _, e := c.Payment.Delete().ExecReturning(ctx); return e }
			}},
		{"payment_method", paymentmethod.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.PaymentMethodUpdate { return c.PaymentMethod.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.PaymentMethod.Delete().Exec(ctx); return e }, func() error { _, e := c.PaymentMethod.Delete().ExecReturning(ctx); return e }
			}},
		{"refund", refund.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.RefundUpdate { return c.Refund.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.Refund.Delete().Exec(ctx); return e }, func() error { _, e := c.Refund.Delete().ExecReturning(ctx); return e }
			}},
		{"checkout_session", checkoutsession.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.CheckoutSessionUpdate { return c.CheckoutSession.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.CheckoutSession.Delete().Exec(ctx); return e }, func() error { _, e := c.CheckoutSession.Delete().ExecReturning(ctx); return e }
			}},
		{"plan", plan.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.PlanUpdate { return c.Plan.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.Plan.Delete().Exec(ctx); return e }, func() error { _, e := c.Plan.Delete().ExecReturning(ctx); return e }
			}},
		{"price", price.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.PriceUpdate { return c.Price.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.Price.Delete().Exec(ctx); return e }, func() error { _, e := c.Price.Delete().ExecReturning(ctx); return e }
			}},
		{"feature", feature.Columns,
			func(c *ent.Client) (func() error, func() error) {
				b := func() *ent.FeatureUpdate { return c.Feature.Update().SetUpdatedAt(fixedTime) }
				return func() error { _, e := b().Save(ctx); return e }, func() error { _, e := b().SaveReturning(ctx); return e }
			},
			func(c *ent.Client) (func() error, func() error) {
				return func() error { _, e := c.Feature.Delete().Exec(ctx); return e }, func() error { _, e := c.Feature.Delete().ExecReturning(ctx); return e }
			}},
	}

	for _, n := range nodes {
		for kind, pick := range map[string]func(node) func(c *ent.Client) (func() error, func() error){
			"update": func(n node) func(c *ent.Client) (func() error, func() error) { return n.update },
			"delete": func(n node) func(c *ent.Client) (func() error, func() error) { return n.del },
		} {
			t.Run(n.name+"/"+kind, func(t *testing.T) {
				plain, ret := newRecordingDriver(), newRecordingDriver()
				ret.columns = n.columns
				plainFn, _ := pick(n)(returningClient(plain))
				_, retFn := pick(n)(returningClient(ret))
				require.NoError(t, plainFn())
				require.NoError(t, retFn())
				require.Len(t, plain.execs, 1)
				require.Len(t, ret.queries, 1)
				require.Equal(t, plain.execs[0].Query+" RETURNING "+quotedColumns(n.columns), ret.queries[0].Query)
				require.Equal(t, plain.execs[0].Args, ret.queries[0].Args)
			})
		}
	}
}
