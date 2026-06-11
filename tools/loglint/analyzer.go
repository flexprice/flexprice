package loglint

import (
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

var Analyzer = &analysis.Analyzer{
	Name:     "loglint",
	Doc:      "Enforces Flexprice logging conventions",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// isExemptPkg returns true for packages that should be skipped by all
// loglint rules. e2eprobe is the synthetic monitoring harness — it
// intentionally constructs its own logger plumbing (composite Reporter
// with Slack/OTEL/log sinks) and operates outside the standard service
// logging conventions.
func isExemptPkg(pass *analysis.Pass) bool {
	return strings.Contains(pass.Pkg.Path(), "/e2eprobe")
}

func run(pass *analysis.Pass) (interface{}, error) {
	if isExemptPkg(pass) {
		return nil, nil
	}
	runLL001(pass)
	runLL002(pass)
	runLL003(pass)
	runLL004(pass)
	runLL006(pass)
	runLL008(pass)
	runLL009(pass)
	return nil, nil
}
