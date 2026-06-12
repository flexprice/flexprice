package loglint

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

var Analyzer = &analysis.Analyzer{
	Name:     "loglint",
	Doc:      "Enforces Flexprice logging conventions",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (interface{}, error) {
	runLL001(pass)
	runLL002(pass)
	runLL003(pass)
	runLL004(pass)
	runLL006(pass)
	runLL008(pass)
	runLL009(pass)
	return nil, nil
}
