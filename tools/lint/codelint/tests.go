package codelint

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
)

const codeTest = "CL003"

// A test longer than this that is not table-driven is several tests in one.
const maxTestLines = 100

var quarantineMarker = regexp.MustCompile(`^\s*//\s*quarantine:\s*FLE-\d+\b`)

// checkTests keeps tests short, deterministic and honest about being skipped.
func (c *checker) checkTests() {
	for _, file := range c.pass.Files {
		if !c.isTestFile(file) {
			continue
		}
		quarantined := quarantinedLines(c.pass.Fset, file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			c.checkTestBody(fn, quarantined)
			if strings.HasPrefix(fn.Name.Name, "Test") {
				c.checkTestLength(fn)
			}
		}
	}
}

func (c *checker) checkTestBody(fn *ast.FuncDecl, quarantined map[int]bool) {
	var stack []ast.Node
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch {
		case sel.Sel.Name == "Sleep" && c.isPackage(sel.X, "time"):
			c.report(call.Pos(), codeTest, "wait on the event, channel or clock the test depends on, not on real time")
		case isSkipCall(sel.Sel.Name) && c.isTestingT(sel.X):
			c.checkSkip(call, stack, quarantined)
		}
		return true
	})
}

func (c *checker) checkSkip(call *ast.CallExpr, stack []ast.Node, quarantined map[int]bool) {
	if len(call.Args) == 0 {
		c.report(call.Pos(), codeTest, "say why the test is skipped")
		return
	}
	for _, ancestor := range stack {
		switch ancestor.(type) {
		case *ast.IfStmt, *ast.CaseClause, *ast.CommClause:
			return
		}
	}
	line := c.pass.Fset.Position(call.Pos()).Line
	if !quarantined[line-1] {
		c.report(call.Pos(), codeTest, "an unconditional skip is a quarantined test; put `// quarantine: FLE-1234 <why>` on the line above, or delete the test")
	}
}

func (c *checker) checkTestLength(fn *ast.FuncDecl) {
	start := c.pass.Fset.Position(fn.Body.Lbrace).Line
	end := c.pass.Fset.Position(fn.Body.Rbrace).Line
	if end-start <= maxTestLines || hasRange(fn.Body) {
		return
	}
	c.report(fn.Name.Pos(), codeTest, "%s runs %d lines; turn the scenarios into table rows or split it into tests named for one behaviour each", fn.Name.Name, end-start)
}

// hasRange is how a table-driven test shows itself: it ranges over its cases.
func hasRange(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.RangeStmt); ok {
			found = true
		}
		return !found
	})
	return found
}

func isSkipCall(name string) bool {
	return name == "Skip" || name == "Skipf" || name == "SkipNow"
}

func (c *checker) isTestingT(expr ast.Expr) bool {
	typ := c.pass.TypesInfo.TypeOf(expr)
	return typ != nil && (strings.HasSuffix(typ.String(), "testing.T") || strings.HasSuffix(typ.String(), "testing.B"))
}

func quarantinedLines(fset *token.FileSet, file *ast.File) map[int]bool {
	lines := map[int]bool{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if quarantineMarker.MatchString(comment.Text) {
				lines[fset.Position(comment.Pos()).Line] = true
			}
		}
	}
	return lines
}
