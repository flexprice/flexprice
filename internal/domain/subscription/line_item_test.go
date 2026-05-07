package subscription_test

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/stretchr/testify/assert"
)

func TestGetPeriodStart(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mid  := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	end  := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	li := &subscription.SubscriptionLineItem{StartDate: mid}
	assert.Equal(t, mid, li.GetPeriodStart(base)) // StartDate > default → use StartDate
	assert.Equal(t, end, li.GetPeriodStart(end))  // default > StartDate → use default
}

func TestGetPeriodEnd(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mid  := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	end  := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	li := &subscription.SubscriptionLineItem{EndDate: mid}
	assert.Equal(t, mid,  li.GetPeriodEnd(end))  // li.EndDate (Jan15) < end (Feb1) → use EndDate
	assert.Equal(t, base, li.GetPeriodEnd(base)) // defaultPeriodEnd (Jan1) < li.EndDate (Jan15) → clamp to default

	liNoEnd := &subscription.SubscriptionLineItem{}
	assert.Equal(t, end, liNoEnd.GetPeriodEnd(end)) // zero EndDate → use default
}

func TestGetPeriod(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mid   := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	end   := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	li := &subscription.SubscriptionLineItem{StartDate: mid}
	s, e := li.GetPeriod(start, end)
	assert.Equal(t, mid, s)
	assert.Equal(t, end, e)
}
