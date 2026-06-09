package main

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
)

// ctxVariants are methods where ctx is already the first argument — just rename.
var ctxVariants = map[string]string{
	"InfowCtx":  "Info",
	"ErrorwCtx": "Error",
	"DebugwCtx": "Debug",
	"WarnwCtx":  "Warn",
	"InfofCtx":  "Info",
	"ErrorfCtx": "Error",
	"DebugfCtx": "Debug",
	"WarnfCtx":  "Warn",
}

// nonCtxW are methods where we need to inject ctx as the first argument.
var nonCtxW = map[string]string{
	"Infow":  "Info",
	"Errorw": "Error",
	"Debugw": "Debug",
}

// alwaysManual lists methods that always go to the report for human review.
var alwaysManual = map[string]string{
	"Warnw":  "needs reclassification to Info or Error",
	"Warnf":  "needs reclassification to Info or Error",
	"Errorf": "complex format string — verify args before migrating",
}

// rewriteFile parses filename, rewrites call expressions in-place, and returns
// a list of sites that need manual review.
func rewriteFile(filename string) ([]ManualItem, error) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		// Not valid Go — skip gracefully.
		return nil, nil
	}

	var manuals []ManualItem
	changed := false

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method := sel.Sel.Name

		// --- Case 1: *Ctx variants — rename only ---
		if newName, isCtx := ctxVariants[method]; isCtx {
			sel.Sel.Name = newName
			changed = true
			return true
		}

		// --- Case 2: non-ctx *w variants — inject ctx ---
		if newName, isW := nonCtxW[method]; isW {
			pos := fset.Position(call.Pos())
			ctx := findCtxInScope(f, call)
			if ctx == nil {
				manuals = append(manuals, ManualItem{
					File:   filename,
					Line:   pos.Line,
					Method: method,
					Reason: "no ctx param found in enclosing function — inject ctx manually",
				})
				return true
			}
			// Prepend ctx to args.
			newArgs := make([]ast.Expr, 0, len(call.Args)+1)
			newArgs = append(newArgs, ctx)
			newArgs = append(newArgs, call.Args...)
			call.Args = newArgs
			sel.Sel.Name = newName
			changed = true
			return true
		}

		// --- Case 3: Infof with no format args — inject ctx ---
		if method == "Infof" {
			pos := fset.Position(call.Pos())
			if len(call.Args) == 1 {
				// Single string arg, no format params — safe to rewrite.
				ctx := findCtxInScope(f, call)
				if ctx == nil {
					manuals = append(manuals, ManualItem{
						File:   filename,
						Line:   pos.Line,
						Method: method,
						Reason: "no ctx param found in enclosing function — inject ctx manually",
					})
					return true
				}
				newArgs := make([]ast.Expr, 0, 2)
				newArgs = append(newArgs, ctx)
				newArgs = append(newArgs, call.Args...)
				call.Args = newArgs
				sel.Sel.Name = "Info"
				changed = true
				return true
			}
			// Multi-arg Infof — flag for manual review.
			manuals = append(manuals, ManualItem{
				File:   filename,
				Line:   pos.Line,
				Method: method,
				Reason: "format string with args — verify before migrating",
			})
			return true
		}

		// --- Case 3b: Debugf with no format args — inject ctx ---
		if method == "Debugf" {
			pos := fset.Position(call.Pos())
			if len(call.Args) == 1 {
				// Single string arg, no format params — safe to rewrite.
				ctx := findCtxInScope(f, call)
				if ctx == nil {
					manuals = append(manuals, ManualItem{
						File:   filename,
						Line:   pos.Line,
						Method: method,
						Reason: "no ctx param found in enclosing function — inject ctx manually",
					})
					return true
				}
				newArgs := make([]ast.Expr, 0, 2)
				newArgs = append(newArgs, ctx)
				newArgs = append(newArgs, call.Args...)
				call.Args = newArgs
				sel.Sel.Name = "Debug"
				changed = true
				return true
			}
			// Multi-arg Debugf — flag for manual review.
			manuals = append(manuals, ManualItem{
				File:   filename,
				Line:   pos.Line,
				Method: method,
				Reason: "format string with args — verify before migrating",
			})
			return true
		}

		// --- Case 4: always-manual methods ---
		if reason, isManual := alwaysManual[method]; isManual {
			// Skip fmt.Errorf, fmt.Warnf etc — we only care about logger calls.
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "fmt" {
				return true
			}
			pos := fset.Position(call.Pos())
			manuals = append(manuals, ManualItem{
				File:   filename,
				Line:   pos.Line,
				Method: method,
				Reason: reason,
			})
			return true
		}

		// --- Case 5: GetLogger / GetLoggerWithContext — flag for DI injection ---
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "logger" {
			if sel.Sel.Name == "GetLogger" || sel.Sel.Name == "GetLoggerWithContext" {
				pos := fset.Position(call.Pos())
				manuals = append(manuals, ManualItem{
					File:   filename,
					Line:   pos.Line,
					Method: sel.Sel.Name,
					Reason: "GetLogger() must be replaced with DI-injected logger",
				})
				return true
			}
		}

		return true
	})

	if !changed {
		return manuals, nil
	}

	// Format and write back.
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return manuals, err
	}
	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return manuals, err
	}
	return manuals, nil
}

// findCtxInScope walks f looking for the innermost enclosing FuncDecl or
// FuncLit that contains target, then checks if any parameter is named "ctx".
func findCtxInScope(f *ast.File, target ast.Node) *ast.Ident {
	var result *ast.Ident
	ast.Inspect(f, func(n ast.Node) bool {
		if result != nil {
			return false
		}
		var funcType *ast.FuncType
		switch fn := n.(type) {
		case *ast.FuncDecl:
			funcType = fn.Type
		case *ast.FuncLit:
			funcType = fn.Type
		default:
			return true
		}
		if !nodeContains(n, target) {
			return true
		}
		if funcType.Params != nil {
			for _, field := range funcType.Params.List {
				for _, name := range field.Names {
					if name.Name == "ctx" {
						result = ast.NewIdent("ctx")
					}
				}
			}
		}
		return result == nil
	})
	return result
}

// nodeContains reports whether parent's subtree contains child.
func nodeContains(parent, child ast.Node) bool {
	found := false
	ast.Inspect(parent, func(n ast.Node) bool {
		if found {
			return false
		}
		if n == child {
			found = true
			return false
		}
		return true
	})
	return found
}
