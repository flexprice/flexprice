package loglint

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// isBootstrapPkg returns true for bootstrap/setup packages that are
// allowed to use global loggers and fmt printing.
func isBootstrapPkg(pass *analysis.Pass) bool {
	pkg := pass.Pkg.Path()
	return strings.Contains(pkg, "/cmd") ||
		strings.Contains(pkg, "/scripts") ||
		strings.Contains(pkg, "/config") ||
		strings.HasSuffix(pkg, "cmd") ||
		strings.HasSuffix(pkg, "scripts")
}

// runLL002 flags uses of logger.L, logger.GetLogger(), and
// logger.GetLoggerWithContext() outside cmd/ and scripts/ packages.
func runLL002(pass *analysis.Pass) {
	if isBootstrapPkg(pass) {
		return
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodeFilter := []ast.Node{
		(*ast.SelectorExpr)(nil),
		(*ast.CallExpr)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			// logger.L (field/var access, not a call target handled in CallExpr)
			ident, ok := node.X.(*ast.Ident)
			if !ok {
				return
			}
			if ident.Name == "logger" && node.Sel.Name == "L" {
				// Resolve to confirm it's the actual internal/logger package.
				obj := pass.TypesInfo.Uses[ident]
				if obj == nil {
					return
				}
				pkgName, ok := obj.(*types.PkgName)
				if !ok {
					return
				}
				if !isLoggerPkg(pkgName.Imported().Path()) {
					return
				}
				pass.Reportf(node.Pos(), "LL002: use injected logger instead of logger.L / GetLogger()")
			}

		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return
			}
			if ident.Name != "logger" {
				return
			}
			// Resolve to confirm it's the actual internal/logger package.
			obj := pass.TypesInfo.Uses[ident]
			if obj == nil {
				return
			}
			pkgName, ok := obj.(*types.PkgName)
			if !ok {
				return
			}
			if !isLoggerPkg(pkgName.Imported().Path()) {
				return
			}
			switch sel.Sel.Name {
			case "GetLogger", "GetLoggerWithContext":
				pass.Reportf(node.Pos(), "LL002: use injected logger instead of logger.L / GetLogger()")
			}
		}
	})
}

// runLL004 flags fmt.Print* and builtin print/println calls outside cmd/ and scripts/.
func runLL004(pass *analysis.Pass) {
	if isBootstrapPkg(pass) {
		return
	}

	fmtPrintFuncs := map[string]bool{
		"Println":  true,
		"Printf":   true,
		"Print":    true,
		"Fprintln": true,
		"Fprintf":  true,
		"Fprint":   true,
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}

		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if !fmtPrintFuncs[fun.Sel.Name] {
				return
			}
			ident, ok := fun.X.(*ast.Ident)
			if !ok {
				return
			}
			// Resolve the identifier to check it's the "fmt" package
			obj := pass.TypesInfo.Uses[ident]
			if obj == nil {
				return
			}
			pkgName, ok := obj.(*types.PkgName)
			if !ok {
				return
			}
			if pkgName.Imported().Path() == "fmt" {
				pass.Reportf(call.Pos(), "LL004: fmt.%s banned outside cmd/ and scripts/ — use the injected logger", fun.Sel.Name)
			}

		case *ast.Ident:
			// builtin print / println
			if fun.Name != "print" && fun.Name != "println" {
				return
			}
			obj := pass.TypesInfo.Uses[fun]
			if obj == nil {
				// Unresolved — treat as builtin
				pass.Reportf(call.Pos(), "LL004: fmt.%s banned outside cmd/ and scripts/ — use the injected logger", fun.Name)
				return
			}
			// Universe scope: obj.Parent().Parent() == nil
			if obj.Parent() != nil && obj.Parent().Parent() == nil {
				pass.Reportf(call.Pos(), "LL004: fmt.%s banned outside cmd/ and scripts/ — use the injected logger", fun.Name)
			}
		}
	})
}

// runLL006 errors when log.Error(ctx, msg, fields...) is called without an "error" key.
// Exempt the logger package itself (it contains wrapper methods).
func runLL006(pass *analysis.Pass) {
	pkg := pass.Pkg.Path()
	if strings.HasSuffix(pkg, "/logger") || pkg == "logger" {
		return
	}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if sel.Sel.Name != "Error" {
			return
		}
		// Only check calls where the first arg is context.Context (our logger API).
		// This prevents false positives on assert.Error(t, err), http.Error(w, msg, code), etc.
		if len(call.Args) < 1 {
			return
		}
		firstArgType := pass.TypesInfo.TypeOf(call.Args[0])
		if firstArgType == nil {
			return
		}
		if !isContextType(firstArgType) {
			return
		}
		// Need at least (ctx, msg) arguments — fields start at index 2
		if len(call.Args) < 2 {
			return
		}
		// Scan field keys: even indices starting at 2
		for i := 2; i < len(call.Args); i += 2 {
			lit, ok := call.Args[i].(*ast.BasicLit)
			if !ok {
				continue
			}
			if lit.Kind == token.STRING {
				val := strings.Trim(lit.Value, "`")
				val = strings.Trim(val, `"`)
				if val == "error" {
					return // found the error key
				}
			}
		}
		// No "error" key found
		pass.Reportf(call.Pos(), "LL006: log.Error() missing \"error\" field — use logger.Err(err)")
	})
}

// checkpointPrefixes are dev-checkpoint phrases that should not appear in Info logs.
var checkpointPrefixes = []string{
	"entering ",
	"starting ",
	"processing ",
	"validating ",
	"inside ",
	"got ",
	"fetched ",
	"called ",
	"running ",
}

// runLL008 warns when Info/Infow/Infof calls use a dev-checkpoint message.
func runLL008(pass *analysis.Pass) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}

		var msgArgIdx int
		switch sel.Sel.Name {
		case "Info":
			// Heuristic: if first arg has context.Context type, msg is at index 1 (new API)
			// otherwise msg is at index 0 (old API).
			if len(call.Args) >= 2 && isContextArg(pass, call.Args[0]) {
				msgArgIdx = 1
			} else if len(call.Args) >= 1 {
				msgArgIdx = 0
			} else {
				return
			}
		case "Infow", "Infof":
			msgArgIdx = 0
		default:
			return
		}

		if msgArgIdx >= len(call.Args) {
			return
		}

		lit, ok := call.Args[msgArgIdx].(*ast.BasicLit)
		if !ok {
			return
		}
		if lit.Kind != token.STRING {
			return
		}

		raw := strings.Trim(lit.Value, "`")
		raw = strings.Trim(raw, `"`)
		msg := strings.ToLower(raw)
		for _, prefix := range checkpointPrefixes {
			if strings.HasPrefix(msg, prefix) {
				pass.Reportf(call.Pos(), "warning: LL008: dev-checkpoint log — delete or demote to Debug: %s", raw)
				return
			}
		}
	})
}

// isContextArg returns true if the expression has type context.Context.
func isContextArg(pass *analysis.Pass, expr ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return false
	}
	return strings.HasSuffix(t.String(), "context.Context")
}

// isLoggerPkg returns true if the import path refers to our internal/logger package.
// It accepts both the real path (ends with "/logger") and the bare "logger" used in
// test-data fake packages.
func isLoggerPkg(path string) bool {
	return path == "logger" || strings.HasSuffix(path, "/logger")
}

// isContextType returns true if typ is context.Context (or a named type
// whose string representation contains "context.Context").
func isContextType(typ types.Type) bool {
	if named, ok := typ.(*types.Named); ok {
		obj := named.Obj()
		if obj.Name() == "Context" && obj.Pkg() != nil && obj.Pkg().Path() == "context" {
			return true
		}
	}
	// Fallback: handle aliases / workflow.Context / pointer wrappers via string match.
	return strings.Contains(typ.String(), "context.Context")
}
