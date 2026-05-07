package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	addonDomain "github.com/flexprice/flexprice/internal/domain/addon"
	featureDomain "github.com/flexprice/flexprice/internal/domain/feature"
	groupDomain "github.com/flexprice/flexprice/internal/domain/group"
	meterDomain "github.com/flexprice/flexprice/internal/domain/meter"
	planDomain "github.com/flexprice/flexprice/internal/domain/plan"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	subscriptionDomain "github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

// buildFullAnalyticsData constructs an AnalyticsData with every map field populated.
// prefix is appended to IDs so two calls produce distinct entries.
func buildFullAnalyticsData(prefix string) *AnalyticsData {
	sub := &subscriptionDomain.Subscription{ID: prefix + "sub1"}
	sub.LineItems = []*subscriptionDomain.SubscriptionLineItem{
		{ID: prefix + "li1"},
	}
	return &AnalyticsData{
		Subscriptions:         []*subscriptionDomain.Subscription{sub},
		SubscriptionsMap:      map[string]*subscriptionDomain.Subscription{sub.ID: sub},
		SubscriptionLineItems: map[string]*subscriptionDomain.SubscriptionLineItem{prefix + "li1": sub.LineItems[0]},
		Features:              map[string]*featureDomain.Feature{prefix + "f1": {ID: prefix + "f1"}},
		Meters:                map[string]*meterDomain.Meter{prefix + "m1": {ID: prefix + "m1"}},
		Prices:                map[string]*priceDomain.Price{prefix + "p1": {ID: prefix + "p1"}},
		PriceResponses:        map[string]*dto.PriceResponse{prefix + "p1": {Price: &priceDomain.Price{ID: prefix + "p1"}}},
		Plans:                 map[string]*planDomain.Plan{prefix + "pl1": {ID: prefix + "pl1"}},
		Addons:                map[string]*addonDomain.Addon{prefix + "a1": {ID: prefix + "a1"}},
		Groups:                map[string]*groupDomain.Group{prefix + "g1": {ID: prefix + "g1"}},
	}
}

func TestMergeAnalyticsData_HappyPath(t *testing.T) {
	svc := &featureUsageTrackingService{}
	aggregated := buildFullAnalyticsData("parent-")
	additional := buildFullAnalyticsData("child-")

	svc.mergeAnalyticsData(aggregated, additional)

	// All child entries must be present
	assert.Contains(t, aggregated.SubscriptionsMap, "child-sub1")
	assert.Contains(t, aggregated.SubscriptionLineItems, "child-li1")
	assert.Contains(t, aggregated.Features, "child-f1")
	assert.Contains(t, aggregated.Meters, "child-m1")
	assert.Contains(t, aggregated.Prices, "child-p1")
	assert.Contains(t, aggregated.PriceResponses, "child-p1")
	assert.Contains(t, aggregated.Plans, "child-pl1")
	assert.Contains(t, aggregated.Addons, "child-a1")
	assert.Contains(t, aggregated.Groups, "child-g1")

	// Parent entries must still be present
	assert.Contains(t, aggregated.SubscriptionsMap, "parent-sub1")
	assert.Contains(t, aggregated.PriceResponses, "parent-p1")

	// Subscriptions slice must contain both parent and child entries
	assert.Len(t, aggregated.Subscriptions, 2)
}

func TestMergeAnalyticsData_FirstWins(t *testing.T) {
	// FirstWins validates post-fix merge semantics: when both parent and child have the
	// same price ID, the aggregated (parent) value is kept. This test passes both before
	// and after the fix — it guards against regressions in the merge order, not the bug itself.
	svc := &featureUsageTrackingService{}

	parentPrice := &dto.PriceResponse{Price: &priceDomain.Price{ID: "shared-p1", BillingCadence: types.BILLING_CADENCE_RECURRING}}
	childPrice := &dto.PriceResponse{Price: &priceDomain.Price{ID: "shared-p1", BillingCadence: types.BillingCadence("ANNUAL")}}

	aggregated := &AnalyticsData{
		PriceResponses: map[string]*dto.PriceResponse{"shared-p1": parentPrice},
	}
	additional := &AnalyticsData{
		PriceResponses: map[string]*dto.PriceResponse{"shared-p1": childPrice},
	}

	svc.mergeAnalyticsData(aggregated, additional)

	// Parent (first) wins
	assert.Equal(t, types.BILLING_CADENCE_RECURRING, aggregated.PriceResponses["shared-p1"].BillingCadence)
}

func TestMergeAnalyticsData_NilAggregated(t *testing.T) {
	svc := &featureUsageTrackingService{}
	additional := buildFullAnalyticsData("child-")

	// Must not panic
	assert.NotPanics(t, func() {
		svc.mergeAnalyticsData(nil, additional)
	})
}

func TestMergeAnalyticsData_EmptyAdditional(t *testing.T) {
	svc := &featureUsageTrackingService{}
	aggregated := buildFullAnalyticsData("parent-")
	additional := &AnalyticsData{}

	svc.mergeAnalyticsData(aggregated, additional)

	// Nothing should change
	assert.Contains(t, aggregated.PriceResponses, "parent-p1")
	assert.Len(t, aggregated.PriceResponses, 1)
	assert.Len(t, aggregated.Features, 1)
	assert.Len(t, aggregated.Meters, 1)
}
