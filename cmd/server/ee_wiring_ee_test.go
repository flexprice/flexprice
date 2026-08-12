//go:build ee

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"

	"github.com/flexprice/flexprice/internal/api"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/temporal"
)

// TestEERegistriesPopulated is the guard for a failure a passing build cannot
// catch: `-tags ee` compiling fine while the binary contains no enterprise code
// at all. That happens whenever the import chain into ee/ is broken — a missing
// eeOptions() call in main, or a blank import dropped from ee/module.go.
//
// Nothing here references ee/ directly. The registries are populated purely as
// a side effect of this package importing ee, which is the property under test.
func TestEERegistriesPopulated(t *testing.T) {
	// Force the import chain exactly as main() does.
	if got := len(eeOptions()); got != 1 {
		t.Fatalf("eeOptions() returned %d options, want 1 — is ee_enabled.go compiled?", got)
	}

	checks := []struct {
		registry string
		count    int
		feature  string
	}{
		{"temporal contributors", temporal.EEContributorCount(), "ee/alerts UsageAlertWorkflow"},
		{"auth providers", auth.EEProviderCount(), "ee/auth/saml"},
		{"api route registrars", api.EERouteRegistrarCount(), "ee/auth/saml routes"},
	}

	for _, c := range checks {
		if c.count == 0 {
			t.Errorf("%s registry is empty — %s never reached init(); "+
				"check the blank import in ee/module.go", c.registry, c.feature)
		}
	}
}

// TestMainInvokesEEOptions closes a blind spot in TestEERegistriesPopulated:
// that test calls eeOptions() itself, so it triggers the ee import chain even
// when main() has stopped doing so. Reading the source is the only way to tell
// the two apart from inside the same package.
//
// This is the exact defect that shipped once already — the hooks were present,
// the build was green, and the binary contained no enterprise code.
func TestMainInvokesEEOptions(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	var called bool
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "eeOptions" {
			called = true
			return false
		}
		return true
	})

	if !called {
		t.Fatal("main.go never calls eeOptions() — enterprise features would be " +
			"absent from an -tags ee binary even though it builds and links cleanly")
	}
}
