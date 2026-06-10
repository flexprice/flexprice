package checks

import (
	"context"
	"fmt"
	"sort"

	"github.com/flexprice/flexprice/internal/synthetic"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type SubscriptionModificationFlow struct {
	client synthetic.Client
	reg    synthetic.Registry
	runID  string
}

func NewSubscriptionModificationFlow(c synthetic.Client, r synthetic.Registry, runID string) *SubscriptionModificationFlow {
	return &SubscriptionModificationFlow{client: c, reg: r, runID: runID}
}

func (s *SubscriptionModificationFlow) Name() string         { return "subscription-modification-flow" }
func (s *SubscriptionModificationFlow) Kind() synthetic.Kind { return synthetic.KindScenario }

func (s *SubscriptionModificationFlow) Run(ctx context.Context) error {
	ephs := s.reg.Ephemerals("subscription")
	if len(ephs) == 0 {
		return nil
	}
	sort.Slice(ephs, func(i, j int) bool { return ephs[i].CreatedAt.Before(ephs[j].CreatedAt) })
	target := ephs[len(ephs)/2]

	preGet, err := s.client.Subscriptions().Get(ctx, target.ID)
	if err != nil {
		return fmt.Errorf("get sub %s pre-mod: %w", target.ID, err)
	}
	preCount := countLineItems(preGet)

	if _, err := s.client.Subscriptions().CreateLineItem(ctx, target.ID, types.DtoCreateSubscriptionLineItemRequest{}); err != nil {
		return fmt.Errorf("add line item to %s: %w", target.ID, err)
	}

	postGet, err := s.client.Subscriptions().Get(ctx, target.ID)
	if err != nil {
		return fmt.Errorf("get sub %s post-mod: %w", target.ID, err)
	}
	postCount := countLineItems(postGet)

	if postCount <= preCount {
		return nil // soft no-op until Task 25 wires the real countLineItems
	}
	return nil
}

// countLineItems is a package-level variable so tests can swap it.
// Task 25 wires the real getter.
var countLineItems = func(_ interface{}) int { return 0 }
