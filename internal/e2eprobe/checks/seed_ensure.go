package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

const (
	PersistentCustomerCount = 10
	PreFundedWalletCount    = 3
)

func strPtr(s string) *string { return &s }

func persistentExternalCustomerID(i int) string {
	return fmt.Sprintf("e2eprobe-cust-persistent-%d", i)
}

type SeedEnsure struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
}

func NewSeedEnsure(c e2eprobe.Client, r e2eprobe.Registry, runID string) *SeedEnsure {
	return &SeedEnsure{client: c, reg: r, runID: runID}
}

func (s *SeedEnsure) Name() string         { return "seed-ensure" }
func (s *SeedEnsure) Kind() e2eprobe.Kind { return e2eprobe.KindBootstrap }
func (s *SeedEnsure) Run(ctx context.Context) error {
	seeds := e2eprobe.Seeds{
		MeterIDs: map[string]string{},
	}
	if err := s.ensureMeters(ctx, &seeds); err != nil {
		return err
	}
	if err := s.ensureCustomers(ctx, &seeds); err != nil {
		return err
	}
	s.reg.LoadSeeds(seeds)
	return nil
}

var seedMeterSpecs = []e2eprobe.CreateMeterRequest{
	{EventName: "e2eprobe_count", Name: "E2EProbe Count", Aggregation: e2eprobe.MeterAggregation{Type: "COUNT"}, Metadata: seedMetadata("count")},
	{EventName: "e2eprobe_sum", Name: "E2EProbe Sum", Aggregation: e2eprobe.MeterAggregation{Type: "SUM", Field: "amount"}, Metadata: seedMetadata("sum")},
	{EventName: "e2eprobe_avg", Name: "E2EProbe Avg", Aggregation: e2eprobe.MeterAggregation{Type: "AVG", Field: "amount"}, Metadata: seedMetadata("avg")},
	{EventName: "e2eprobe_count_unique", Name: "E2EProbe CountUnique", Aggregation: e2eprobe.MeterAggregation{Type: "COUNT_UNIQUE", Field: "user_id"}, Metadata: seedMetadata("count_unique")},
	{EventName: "e2eprobe_latest", Name: "E2EProbe Latest", Aggregation: e2eprobe.MeterAggregation{Type: "LATEST", Field: "amount"}, Metadata: seedMetadata("latest")},
	{EventName: "e2eprobe_max", Name: "E2EProbe Max", Aggregation: e2eprobe.MeterAggregation{Type: "MAX", Field: "amount", BucketSize: "HOUR"}, Metadata: seedMetadata("max")},
	{EventName: "e2eprobe_sum_multiplier", Name: "E2EProbe SumMul", Aggregation: e2eprobe.MeterAggregation{Type: "SUM_WITH_MULTIPLIER", Field: "amount", Multiplier: "1000"}, Metadata: seedMetadata("sum_with_multiplier")},
	{EventName: "e2eprobe_weighted_sum", Name: "E2EProbe WeightedSum", Aggregation: e2eprobe.MeterAggregation{Type: "WEIGHTED_SUM", Field: "amount", Expression: "amount * duration_ms"}, Metadata: seedMetadata("weighted_sum")},
	{
		EventName:   "e2eprobe_sum_filtered",
		Name:        "E2EProbe Sum (api-only)",
		Aggregation: e2eprobe.MeterAggregation{Type: "SUM", Field: "amount"},
		Filters:     []e2eprobe.MeterFilter{{Key: "source", Values: []string{"api"}}},
		Metadata:    seedMetadata("sum_filtered"),
	},
}

func seedMetadata(agg string) map[string]string {
	return map[string]string{"e2eprobe": "true", "e2eprobe_role": "seed", "aggregation": agg}
}

func (s *SeedEnsure) ensureMeters(ctx context.Context, out *e2eprobe.Seeds) error {
	existing, err := s.client.Meters().List(ctx)
	if err != nil {
		return fmt.Errorf("list meters: %w", err)
	}
	byEvent := map[string]e2eprobe.Meter{}
	for _, m := range existing {
		byEvent[m.EventName] = m
	}
	for _, spec := range seedMeterSpecs {
		if m, ok := byEvent[spec.EventName]; ok {
			out.MeterIDs[spec.EventName] = m.ID
			continue
		}
		created, err := s.client.Meters().Create(ctx, spec)
		if err != nil {
			return fmt.Errorf("create meter %s: %w", spec.EventName, err)
		}
		out.MeterIDs[spec.EventName] = created.ID
	}
	return nil
}

func (s *SeedEnsure) ensureCustomers(ctx context.Context, out *e2eprobe.Seeds) error {
	for i := 0; i < PersistentCustomerCount; i++ {
		ext := persistentExternalCustomerID(i)
		out.PersistentCustomerIDs = append(out.PersistentCustomerIDs, ext)
		if _, err := s.client.Customers().GetByExternalID(ctx, ext); err == nil {
			continue
		}
		req := types.DtoCreateCustomerRequest{
			ExternalID: ext,
			Name:       strPtr(fmt.Sprintf("E2EProbe Persistent %d", i)),
			Email:      strPtr(fmt.Sprintf("%s@e2eprobe.flexprice.invalid", ext)),
			Metadata: map[string]string{
				"e2eprobe": "true",
				"e2eprobe_cohort": "persistent",
				"e2eprobe_role":   "seed",
				"e2eprobe_run_id": s.runID,
				"created_unix_ns":  fmt.Sprintf("%d", time.Now().UnixNano()),
			},
		}
		if _, err := s.client.Customers().Create(ctx, req); err != nil {
			return fmt.Errorf("create customer %s: %w", ext, err)
		}
	}
	for i := 0; i < PreFundedWalletCount && i < PersistentCustomerCount; i++ {
		out.PreFundedCustomerIDs = append(out.PreFundedCustomerIDs, persistentExternalCustomerID(i))
	}
	return nil
}
