package codelint

import (
	"go/ast"
	"go/types"
	"strings"
)

const codeTimePrecision = "CL005"

// checkTimePrecision keeps microsecond truncation in internal/clock, the
// one place that owns the storage precision contract.
func (c *checker) checkTimePrecision() {
	if strings.HasPrefix(c.packagePath(), "internal/clock") {
		return
	}
	c.insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Truncate" || len(call.Args) != 1 {
			return
		}
		arg, ok := call.Args[0].(*ast.SelectorExpr)
		if !ok || arg.Sel.Name != "Microsecond" || !c.isPackage(arg.X, "time") {
			return
		}
		// Duration.Truncate rounds an elapsed span; only Time.Truncate touches an instant.
		if !c.isTimeTruncate(sel) {
			return
		}
		c.report(call.Pos(), codeTimePrecision, "business code never truncates instants; clock.Now(), the bindings, the activity interceptor and the ent hook already hold the precision contract")
	})
}

// isTimeTruncate resolves the selected method, so a value, pointer or
// embedded time.Time all count and time.Duration does not.
func (c *checker) isTimeTruncate(sel *ast.SelectorExpr) bool {
	method, ok := c.pass.TypesInfo.ObjectOf(sel.Sel).(*types.Func)
	if !ok || method.Pkg() == nil || method.Pkg().Path() != "time" {
		return false
	}
	recv := method.Signature().Recv()
	if recv == nil {
		return false
	}
	named, ok := types.Unalias(recv.Type()).(*types.Named)
	return ok && named.Obj().Name() == "Time"
}

// isPackage reports whether expr names the package with the given path.
func (c *checker) isPackage(expr ast.Expr, path string) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	pkgName, ok := c.pass.TypesInfo.Uses[ident].(*types.PkgName)
	return ok && pkgName.Imported().Path() == path
}
