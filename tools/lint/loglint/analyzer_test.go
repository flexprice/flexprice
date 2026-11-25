package loglint_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/flexprice/flexprice/tools/lint/loglint"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), loglint.Analyzer, "sample", "internal/ee/service")
}
