// loglint is the logging-only vettool; it stays clean across the whole repo.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/flexprice/flexprice/tools/lint/loglint"
)

func main() {
	singlechecker.Main(loglint.Analyzer)
}
