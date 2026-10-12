package checks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/billingmatrix"
)

// rejectingAPI fails every call, so every scenario errors out at its first request.
type rejectingAPI struct{ calls int }

func (a *rejectingAPI) Do(context.Context, string, string, any, any) error {
	a.calls++
	return errors.New("api unavailable")
}

func TestBillingMatrixProbe_ReportsFailingScenarios(t *testing.T) {
	api := &rejectingAPI{}
	engine := billingmatrix.NewEngine(api, "test", false)
	probe := NewBillingMatrixProbe(engine, billingmatrix.FamilyOpening, 2, nil)

	if probe.Name() != "billing-matrix-opening" || probe.Kind() != e2eprobe.KindScenario {
		t.Fatalf("unexpected identity %s/%s", probe.Name(), probe.Kind())
	}
	err := probe.Run(context.Background())
	if err == nil {
		t.Fatal("want an error when every scenario fails")
	}
	var pe *e2eprobe.CheckError
	if !errors.As(err, &pe) || pe.Attributes["step"] != "billing_matrix_opening" {
		t.Fatalf("want step attr billing_matrix_opening, got %v", err)
	}
	if got := len(strings.Split(pe.Attributes["failed_scenarios"], ",")); got != 2 {
		t.Fatalf("want 2 failed scenarios per run, got %d (%s)", got, pe.Attributes["failed_scenarios"])
	}
	if api.calls == 0 {
		t.Fatal("scenarios never reached the API")
	}
}

func TestBillingMatrixProbe_RotatesThroughFamily(t *testing.T) {
	engine := billingmatrix.NewEngine(&rejectingAPI{}, "test", false)
	probe := NewBillingMatrixProbe(engine, billingmatrix.FamilyValidation, 1, nil)
	seen := map[string]bool{}
	total := len(engine.Names(billingmatrix.FamilyValidation))
	for i := 0; i < total; i++ {
		var pe *e2eprobe.CheckError
		if err := probe.Run(context.Background()); errors.As(err, &pe) {
			seen[pe.Attributes["failed_scenarios"]] = true
		}
	}
	if len(seen) != total {
		t.Fatalf("rotation covered %d of %d validation scenarios", len(seen), total)
	}
}
