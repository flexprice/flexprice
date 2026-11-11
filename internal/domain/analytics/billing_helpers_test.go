package analytics

import "testing"

func withMetric(t *testing.T, bm *BillingMetric) {
	t.Helper()
	prev, had := billingMetrics[bm.Metric]
	billingMetrics[bm.Metric] = bm
	t.Cleanup(func() {
		if had {
			billingMetrics[bm.Metric] = prev
		} else {
			delete(billingMetrics, bm.Metric)
		}
	})
}
