package loglint_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	loglint "github.com/flexprice/flexprice/tools/loglint"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), loglint.Analyzer, "sample")
}
