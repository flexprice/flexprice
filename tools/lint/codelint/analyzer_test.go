package codelint_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/flexprice/flexprice/tools/lint/codelint"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), codelint.Analyzer,
		"sample",
		"internal/api/v1",
		"internal/domain/wallet",
		"internal/ee/service",
		"internal/repository",
		"internal/utils",
		"internal/clock",
	)
}

// TestTrackedEntities fails when the activity log registers an entity the
// CL004 list does not know, or the other way round.
func TestTrackedEntities(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../../../internal/activity/entities.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var registered []string
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "EntName" {
			registered = append(registered, strings.Trim(kv.Value.(*ast.BasicLit).Value, `"`))
		}
		return true
	})
	sort.Strings(registered)

	tracked := codelint.TrackedEntities()
	if strings.Join(registered, ",") != strings.Join(tracked, ",") {
		t.Fatalf("activity registrations and codelint.trackedEntities differ:\nregistered: %v\ntracked:    %v", registered, tracked)
	}
}
