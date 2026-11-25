package loglint

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Phase 5 rules — activated after codemod sweep completes.

var deprecatedMethods = map[string]bool{
	"Infow": true, "Errorw": true, "Debugw": true, "Warnw": true, "Fatalw": true,
	"Infof": true, "Errorf": true, "Debugf": true, "Warnf": true, "Fatalf": true,
	"InfowCtx": true, "ErrorwCtx": true, "DebugwCtx": true, "WarnwCtx": true,
	"InfofCtx": true, "ErrorfCtx": true, "DebugfCtx": true, "WarnfCtx": true,
}

// runLL001 flags calls to deprecated logger methods (w/f/Ctx variants).
// Exempt the logger package itself — it defines and internally wraps these methods.
func runLL001(pass *analysis.Pass) {
	pkg := pass.Pkg.Path()
	if strings.HasSuffix(pkg, "/logger") || pkg == "logger" {
		return
	}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if !deprecatedMethods[sel.Sel.Name] {
			return
		}
		// Only fire if the receiver is a logger type (type string contains "Logger").
		if pass.TypesInfo != nil {
			t := pass.TypesInfo.TypeOf(sel.X)
			if t != nil && !strings.Contains(t.String(), "Logger") {
				return
			}
		}
		baseName := sel.Sel.Name
		baseName = strings.TrimSuffix(baseName, "Ctx")
		baseName = strings.TrimSuffix(baseName, "w")
		baseName = strings.TrimSuffix(baseName, "f")
		pass.Reportf(call.Pos(), "LL001: %s is deprecated — use the ctx-first API: log.%s(ctx, msg, fields...)",
			sel.Sel.Name, baseName)
	})
}

var warnBootstrapAllowlist = []string{"/cmd", "/scripts", "/config", "/logger", "/temporal"}
var warnFuncPrefixes = []string{"New", "Init", "Setup", "Bootstrap", "init", "main"}

// runLL003 flags Warn() calls outside bootstrap/setup code.
func runLL003(pass *analysis.Pass) {
	pkg := pass.Pkg.Path()
	for _, allowed := range warnBootstrapAllowlist {
		if strings.Contains(pkg, allowed) {
			return
		}
	}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if sel.Sel.Name != "Warn" {
			return
		}
		// Check if inside a bootstrap function.
		for _, f := range pass.Files {
			ast.Inspect(f, func(node ast.Node) bool {
				fn, ok := node.(*ast.FuncDecl)
				if !ok {
					return true
				}
				if fn.Body == nil {
					return true
				}
				// Check if this function contains the call.
				var contains bool
				ast.Inspect(fn.Body, func(inner ast.Node) bool {
					if inner == call {
						contains = true
						return false
					}
					return true
				})
				if contains && fn.Name != nil {
					for _, prefix := range warnFuncPrefixes {
						if strings.HasPrefix(fn.Name.Name, prefix) {
							return false // allowed
						}
					}
					pass.Reportf(call.Pos(), "LL003: Warn is restricted to bootstrap/setup code — use Error (if failed) or Info (if recovered)")
				}
				return true
			})
		}
	})
}

// runLL009 flags ctx passed in the fields position instead of the first arg.
func runLL009(pass *analysis.Pass) {
	newMethods := map[string]bool{"Debug": true, "Info": true, "Warn": true, "Error": true, "Fatal": true}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if !newMethods[sel.Sel.Name] {
			return
		}
		// Fields start at index 2. Check if any is an ident named "ctx".
		for i := 2; i < len(call.Args); i++ {
			if ident, ok := call.Args[i].(*ast.Ident); ok && ident.Name == "ctx" {
				pass.Reportf(call.Pos(), "LL009: ctx is in fields position — first arg should be ctx: log.%s(ctx, msg, fields...)", sel.Sel.Name)
			}
		}
	})
}
