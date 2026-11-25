// Package codelint enforces the readability and placement rules in AGENTS.md.
// Legacy code predates them, so run it through changedlines, which keeps only
// the findings on lines a branch touched. See tools/lint/README.md for the
// rules and the reasoned //lint:ignore directive that silences one.
package codelint

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "codelint",
	Doc:      "Enforces Flexprice readability, placement and test rules",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// checker carries one package's pass and its suppression directives.
type checker struct {
	pass    *analysis.Pass
	insp    *inspector.Inspector
	ignores ignores
}

func run(pass *analysis.Pass) (interface{}, error) {
	c := &checker{
		pass:    pass,
		insp:    pass.ResultOf[inspect.Analyzer].(*inspector.Inspector),
		ignores: collectIgnores(pass),
	}
	c.checkComments()
	c.checkImports()
	c.checkTests()
	c.checkEntWrites()
	c.checkTimePrecision()
	c.checkNames()
	return nil, nil
}

// report emits a diagnostic unless a reasoned //lint:ignore covers its line.
func (c *checker) report(pos token.Pos, code, format string, args ...interface{}) {
	if c.ignores.covers(c.pass.Fset.Position(pos), code) {
		return
	}
	c.pass.Reportf(pos, code+": "+format, args...)
}

func (c *checker) filename(pos token.Pos) string {
	return c.pass.Fset.Position(pos).Filename
}

func (c *checker) isTestFile(file *ast.File) bool {
	return strings.HasSuffix(c.filename(file.Pos()), "_test.go")
}

// isGenerated reports files carrying the standard "Code generated ... DO NOT EDIT." header.
func isGenerated(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			return false
		}
		for _, comment := range group.List {
			text := comment.Text
			if strings.HasPrefix(text, "// Code generated ") && strings.HasSuffix(strings.TrimSpace(text), "DO NOT EDIT.") {
				return true
			}
		}
	}
	return false
}

// packagePath is the import path with any module prefix removed, so the
// layer rules read the same for real packages and test fixtures.
func (c *checker) packagePath() string {
	path := c.pass.Pkg.Path()
	if i := strings.Index(path, "internal/"); i >= 0 {
		return path[i:]
	}
	return path
}
