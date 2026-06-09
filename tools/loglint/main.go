//go:build ignore

package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	loglint "github.com/flexprice/flexprice/tools/loglint"
)

func main() {
	singlechecker.Main(loglint.Analyzer)
}
