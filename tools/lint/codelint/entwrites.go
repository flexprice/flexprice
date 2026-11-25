package codelint

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"
)

const codeEntWrite = "CL004"

// trackedEntities are the ent types the activity log records. A count-only
// bulk write on one of them is invisible to the log hook.
// Keep in step with internal/activity/entities.go; TestTrackedEntities checks it.
var trackedEntities = map[string]bool{
	"BillingInstalment": true,
	"BillingTerm":       true,
	"CheckoutSession":   true,
	"CreditNote":        true,
	"Customer":          true,
	"Feature":           true,
	"Invoice":           true,
	"Payment":           true,
	"PaymentMethod":     true,
	"Plan":              true,
	"Price":             true,
	"Refund":            true,
	"Subscription":      true,
	"Wallet":            true,
}

// TrackedEntities lists the entities CL004 guards, sorted.
func TrackedEntities() []string {
	names := make([]string, 0, len(trackedEntities))
	for name := range trackedEntities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// checkEntWrites flags Update()...Save/Exec and Delete()...Exec on tracked
// entities; the returning variants let the activity hook see the rows.
func (c *checker) checkEntWrites() {
	c.insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Save" && sel.Sel.Name != "Exec") {
			return
		}
		entity, kind := entBulkBuilder(c.pass.TypesInfo.TypeOf(sel.X))
		if entity == "" || !trackedEntities[entity] {
			return
		}
		c.report(call.Pos(), codeEntWrite, "%s.%s().%s drops the rows the activity log needs; use %s", entity, kind, sel.Sel.Name, returningVariant(kind))
	})
}

// entBulkBuilder returns the entity and builder kind for *ent.<Entity>Update
// or *ent.<Entity>Delete; the One variants already return their row.
func entBulkBuilder(typ types.Type) (entity, kind string) {
	ptr, ok := typ.(*types.Pointer)
	if !ok {
		return "", ""
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok || named.Obj().Pkg() == nil || !isEntPackage(named.Obj().Pkg().Path()) {
		return "", ""
	}
	name := named.Obj().Name()
	for _, kind := range []string{"Update", "Delete"} {
		if strings.HasSuffix(name, kind) && !strings.HasSuffix(name, kind+"One") {
			return strings.TrimSuffix(name, kind), kind
		}
	}
	return "", ""
}

func returningVariant(kind string) string {
	if kind == "Delete" {
		return "ExecReturning(ctx)"
	}
	return "SaveReturning(ctx)"
}
