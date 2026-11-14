//go:build agenteval

package customermatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/agents/providers/openrouter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// Run against the real model:
//
//	FLEXPRICE_OPENROUTER_API_KEY=... CUSTOMER_MATCH_EVAL_MODEL=... go test -tags=agenteval -run TestEval -v ./internal/ee/agents/services/customermatch/
//
// Optional: CUSTOMER_MATCH_EVAL_CASES (comma-separated case names), CUSTOMER_MATCH_EVAL_PAUSE (e.g. 20s, for rate-limited free models).
type evalSet struct {
	Customers []CustomerRecord `json:"customers"`
	Cases     []struct {
		Name       string       `json:"name"`
		Request    MatchRequest `json:"request"`
		Best       *string      `json:"best"`
		Acceptable []string     `json:"acceptable"`
	} `json:"cases"`
}

type evalDirectory struct{ customers []CustomerRecord }

func (d *evalDirectory) SearchByName(_ context.Context, query string, limit int) ([]CustomerRecord, error) {
	return d.filter(limit, func(c CustomerRecord) bool {
		return strings.Contains(strings.ToLower(c.Name), strings.ToLower(query))
	}), nil
}

func (d *evalDirectory) SearchByEmail(_ context.Context, value string, limit int) ([]CustomerRecord, error) {
	value = strings.ToLower(strings.TrimPrefix(value, "@"))
	if strings.Contains(value, "@") {
		return d.filter(limit, func(c CustomerRecord) bool { return strings.EqualFold(c.Email, value) }), nil
	}
	if lo.Contains([]string{"gmail.com", "outlook.com", "yahoo.com", "hotmail.com"}, value) {
		return nil, errors.New("free mail domain")
	}
	return d.filter(limit, func(c CustomerRecord) bool { return strings.HasSuffix(strings.ToLower(c.Email), "@"+value) }), nil
}

func (d *evalDirectory) Get(_ context.Context, id string) (*CustomerRecord, error) {
	for _, c := range d.customers {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, errors.New("not found")
}

func (d *evalDirectory) filter(limit int, keep func(CustomerRecord) bool) []CustomerRecord {
	out := []CustomerRecord{}
	for _, c := range d.customers {
		if keep(c) && len(out) < limit {
			out = append(out, c)
		}
	}
	return out
}

func TestEval(t *testing.T) {
	key := os.Getenv("FLEXPRICE_OPENROUTER_API_KEY")
	model := os.Getenv("CUSTOMER_MATCH_EVAL_MODEL")
	if key == "" || model == "" {
		t.Skip("set FLEXPRICE_OPENROUTER_API_KEY and CUSTOMER_MATCH_EVAL_MODEL")
	}
	raw, err := os.ReadFile("testdata/eval.json")
	require.NoError(t, err)
	var set evalSet
	require.NoError(t, json.Unmarshal(raw, &set))

	cfg := &config.Configuration{
		OpenRouter:    config.OpenRouterConfig{APIKey: key, ZDR: os.Getenv("FLEXPRICE_OPENROUTER_ZDR") == "true", TimeoutSeconds: 90},
		CustomerMatch: config.CustomerMatchConfig{Model: model, TimeoutSeconds: 120},
	}
	log := logger.NewNoopLogger()
	svc := NewCustomerMatchService(openrouter.NewClient(cfg, log), cfg, log)
	dir := &evalDirectory{customers: set.Customers}

	only := lo.Filter(strings.Split(os.Getenv("CUSTOMER_MATCH_EVAL_CASES"), ","), func(s string, _ int) bool { return s != "" })
	pause, _ := time.ParseDuration(os.Getenv("CUSTOMER_MATCH_EVAL_PAUSE"))

	passed, run, wrongPicks := 0, 0, 0
	var cost float64
	for _, tc := range set.Cases {
		if len(only) > 0 && !lo.Contains(only, tc.Name) {
			continue
		}
		if run > 0 {
			time.Sleep(pause)
		}
		run++
		res, err := svc.Match(context.Background(), &tc.Request, dir)
		if err != nil {
			t.Errorf("%-34s ERROR %v", tc.Name, err)
			continue
		}
		cost += res.Usage.CostUSD
		want := lo.FromPtr(tc.Best)
		top := ""
		if len(res.Candidates) > 0 {
			top = fmt.Sprintf("%s(%d)", res.Candidates[0].CustomerID, res.Candidates[0].Confidence)
		}
		ok := res.BestCustomerID == want
		if !ok && res.BestCustomerID != "" {
			wrongPicks++
		}
		mark := "PASS"
		if !ok {
			mark = "FAIL"
		} else {
			passed++
		}
		t.Logf("%s %-34s want=%-14s best=%-14s top=%-20s steps=%d tools=%d", mark, tc.Name, lo.Ternary(want == "", "none", want),
			lo.Ternary(res.BestCustomerID == "", "none", res.BestCustomerID), lo.Ternary(top == "", "-", top), res.Steps, res.ToolCalls)
	}
	t.Logf("model=%s passed=%d/%d confident_wrong_picks=%d cost_usd=%.4f", model, passed, run, wrongPicks, cost)
}
