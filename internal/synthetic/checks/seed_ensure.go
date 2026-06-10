package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
	"github.com/flexprice/go-sdk/v2/models/types"
)

const (
	PersistentCustomerCount = 10
	PreFundedWalletCount    = 3
)

func strPtr(s string) *string { return &s }

func persistentExternalCustomerID(i int) string {
	return fmt.Sprintf("synthetic-cust-persistent-%d", i)
}

type SeedEnsure struct {
	client synthetic.Client
	reg    synthetic.Registry
	runID  string
}

func NewSeedEnsure(c synthetic.Client, r synthetic.Registry, runID string) *SeedEnsure {
	return &SeedEnsure{client: c, reg: r, runID: runID}
}

func (s *SeedEnsure) Name() string         { return "seed-ensure" }
func (s *SeedEnsure) Kind() synthetic.Kind { return synthetic.KindBootstrap }
func (s *SeedEnsure) Run(ctx context.Context) error {
	seeds := synthetic.Seeds{
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

var seedMeterSpecs = []synthetic.CreateMeterRequest{
	{EventName: "synthetic_count", Name: "Synthetic Count", Aggregation: synthetic.MeterAggregation{Type: "COUNT"}, Metadata: seedMetadata("count")},
	{EventName: "synthetic_sum", Name: "Synthetic Sum", Aggregation: synthetic.MeterAggregation{Type: "SUM", Field: "amount"}, Metadata: seedMetadata("sum")},
	{EventName: "synthetic_avg", Name: "Synthetic Avg", Aggregation: synthetic.MeterAggregation{Type: "AVG", Field: "amount"}, Metadata: seedMetadata("avg")},
	{EventName: "synthetic_count_unique", Name: "Synthetic CountUnique", Aggregation: synthetic.MeterAggregation{Type: "COUNT_UNIQUE", Field: "user_id"}, Metadata: seedMetadata("count_unique")},
	{EventName: "synthetic_latest", Name: "Synthetic Latest", Aggregation: synthetic.MeterAggregation{Type: "LATEST", Field: "amount"}, Metadata: seedMetadata("latest")},
	{EventName: "synthetic_max", Name: "Synthetic Max", Aggregation: synthetic.MeterAggregation{Type: "MAX", Field: "amount", BucketSize: "HOUR"}, Metadata: seedMetadata("max")},
	{EventName: "synthetic_sum_multiplier", Name: "Synthetic SumMul", Aggregation: synthetic.MeterAggregation{Type: "SUM_WITH_MULTIPLIER", Field: "amount", Multiplier: "1000"}, Metadata: seedMetadata("sum_with_multiplier")},
	{EventName: "synthetic_weighted_sum", Name: "Synthetic WeightedSum", Aggregation: synthetic.MeterAggregation{Type: "WEIGHTED_SUM", Field: "amount", Expression: "amount * duration_ms"}, Metadata: seedMetadata("weighted_sum")},
	{
		EventName:   "synthetic_sum_filtered",
		Name:        "Synthetic Sum (api-only)",
		Aggregation: synthetic.MeterAggregation{Type: "SUM", Field: "amount"},
		Filters:     []synthetic.MeterFilter{{Key: "source", Values: []string{"api"}}},
		Metadata:    seedMetadata("sum_filtered"),
	},
}

func seedMetadata(agg string) map[string]string {
	return map[string]string{"synthetic": "true", "synthetic_role": "seed", "aggregation": agg}
}

func (s *SeedEnsure) ensureMeters(ctx context.Context, out *synthetic.Seeds) error {
	existing, err := s.client.Meters().List(ctx)
	if err != nil {
		return fmt.Errorf("list meters: %w", err)
	}
	byEvent := map[string]synthetic.Meter{}
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

func (s *SeedEnsure) ensureCustomers(ctx context.Context, out *synthetic.Seeds) error {
	for i := 0; i < PersistentCustomerCount; i++ {
		ext := persistentExternalCustomerID(i)
		out.PersistentCustomerIDs = append(out.PersistentCustomerIDs, ext)
		if _, err := s.client.Customers().GetByExternalID(ctx, ext); err == nil {
			continue
		}
		req := types.DtoCreateCustomerRequest{
			ExternalID: ext,
			Name:       strPtr(fmt.Sprintf("Synthetic Persistent %d", i)),
			Email:      strPtr(fmt.Sprintf("%s@synthetic.flexprice.invalid", ext)),
			Metadata: map[string]string{
				"synthetic":        "true",
				"synthetic_cohort": "persistent",
				"synthetic_role":   "seed",
				"synthetic_run_id": s.runID,
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
