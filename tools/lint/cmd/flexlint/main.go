// flexlint runs every Flexprice analyzer; its output is meant to be filtered
// to changed lines by changedlines, since legacy code predates the code rules.
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/flexprice/flexprice/tools/lint/codelint"
	"github.com/flexprice/flexprice/tools/lint/loglint"
)

func main() {
	multichecker.Main(loglint.Analyzer, codelint.Analyzer)
}
