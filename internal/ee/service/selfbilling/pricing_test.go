package selfbilling

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cloudPrice(id string) map[string]any {
	return map[string]any{
		"id":                   id,
		"display_name":         "Platform fee",
		"type":                 "FIXED",
		"amount":               "99",
		"currency":             "usd",
		"billing_model":        "FLAT_FEE",
		"billing_cadence":      "RECURRING",
		"billing_period":       "MONTHLY",
		"billing_period_count": 1,
		"invoice_cadence":      "ADVANCE",
		"price_unit_type":      "FIAT",
		"status":               "published",
		"tenant_id":            "billing_tenant",
		"environment_id":       "billing_env",
		"metadata": map[string]string{
			"internal_secret": "hidden",
		},
	}
}

func cloudLine(id string, p map[string]any) map[string]any {
	return map[string]any{
		"id":                   id,
		"price_id":             p["id"],
		"price":                p,
		"display_name":         "Negotiated platform fee",
		"quantity":             "2.75",
		"currency":             "usd",
		"billing_period":       "MONTHLY",
		"billing_period_count": 1,
		"invoice_cadence":      "ADVANCE",
		"status":               "published",
		"start_date":           time.Now().Add(-time.Hour),
	}
}

func cloudSubscription(f *fakeCloud, status types.SubscriptionStatus) map[string]any {
	f.subs["in:t1"] = []map[string]any{{
		"id":                  "sub_1",
		"subscription_status": status,
		"created_at":          time.Now(),
	}}
	line := cloudLine("li_1", cloudPrice("price_override"))
	details := map[string]any{
		"id":                  "sub_1",
		"subscription_status": status,
		"plan": map[string]any{
			"id":          "plan_enterprise",
			"name":        "Enterprise",
			"description": "Negotiated terms",
			"prices":      []any{cloudPrice("catalog_price")},
		},
		"line_items": []any{line},
	}
	f.subDetails["sub_1"] = details
	return details
}

func TestRemoteSummary_OfferedPlanFiltersPrices(t *testing.T) {
	f := newFakeCloud(map[string]string{
		"in:t1": "cust_1",
	})
	fixed := cloudPrice("fixed")
	fixed["min_quantity"] = "3"
	usage := cloudPrice("usage")
	usage["type"], usage["meter_id"], usage["amount"] = "USAGE", "meter_events", "0.000000123456789"
	usage["meter"] = map[string]any{
		"id":   "meter_events",
		"name": "Events",
	}
	oneTime := cloudPrice("onetime")
	oneTime["billing_period"] = "ONETIME"
	annual := cloudPrice("annual")
	annual["billing_period"] = "ANNUAL"
	prices := []any{fixed, usage, oneTime, annual}
	for _, change := range []map[string]any{
		{
			"id":       "other_currency",
			"currency": "eur",
		},
		{
			"id":       "expired",
			"end_date": time.Now().Add(-time.Hour),
		},
		{
			"id":         "future",
			"start_date": time.Now().Add(time.Hour),
		},
		{
			"id":     "archived",
			"status": "archived",
		},
	} {
		p := cloudPrice(change["id"].(string))
		for key, value := range change {
			p[key] = value
		}
		prices = append(prices, p)
	}
	f.plans["plan_1"]["prices"] = prices
	got, err := newTestRemoteClient(t, f).Summary(context.Background(), "in:t1")
	require.NoError(t, err)
	require.NotNil(t, got.plan)
	assert.Equal(t, "plan_1", got.plan.ID)
	require.Len(t, got.plan.Charges, 4)
	assert.Equal(t, types.BILLING_PERIOD_ANNUAL, got.plan.Charges[3].BillingPeriod)
	assert.Equal(t, "3", got.plan.Charges[0].Quantity.String())
	assert.Equal(t, "0", got.plan.Charges[1].Quantity.String())
	assert.Equal(t, "0.000000123456789", got.plan.Charges[1].Amount.String())
	assert.Equal(t, "Events", got.plan.Charges[1].MeterDisplayName)
	assert.Empty(t, got.plan.Charges[0].LineItemID)
	assert.Nil(t, got.subscription)
}

func TestRemoteSummary_SubscriptionOverrides(t *testing.T) {
	for _, status := range []types.SubscriptionStatus{
		types.SubscriptionStatusTrialing, types.SubscriptionStatusActive,
		types.SubscriptionStatusIncomplete, types.SubscriptionStatusPaused, types.SubscriptionStatusDraft,
	} {
		t.Run(string(status), func(t *testing.T) {
			f := newFakeCloud(map[string]string{
				"in:t1": "cust_1",
			})
			details := cloudSubscription(f, status)
			line := details["line_items"].([]any)[0].(map[string]any)
			p := line["price"].(map[string]any)
			p["amount"], p["end_date"] = "79.123456789", time.Now().Add(-time.Hour)
			line["commitment_amount"], line["commitment_overage_factor"] = "ignored", "ignored"
			line["commitment_true_up_enabled"] = true
			details["commitment_amount"], details["overage_factor"] = "ignored", "ignored"
			for _, change := range []map[string]any{
				{
					"id":       "ended",
					"end_date": time.Now().Add(-time.Minute),
				},
				{
					"id":         "future",
					"start_date": time.Now().Add(time.Hour),
				},
				{
					"id":     "archived",
					"status": "archived",
				},
			} {
				old := cloudLine(change["id"].(string), cloudPrice("old_price"))
				for key, value := range change {
					old[key] = value
				}
				delete(old, "price")
				details["line_items"] = append(details["line_items"].([]any), old)
			}
			got, err := newTestRemoteClient(t, f).Summary(context.Background(), "in:t1")
			require.NoError(t, err)
			assert.Equal(t, status, got.subscription.status)
			require.NotNil(t, got.plan)
			assert.Equal(t, "plan_enterprise", got.plan.ID)
			require.Len(t, got.plan.Charges, 1)
			charge := got.plan.Charges[0]
			assert.Equal(t, "li_1", charge.LineItemID)
			assert.Equal(t, "price_override", charge.PriceID)
			assert.Equal(t, "Negotiated platform fee", charge.DisplayName)
			assert.Equal(t, "79.123456789", charge.Amount.String())
			assert.Equal(t, "2.75", charge.Quantity.String())

			for _, request := range f.requests {
				assert.NotContains(t, request, "/plans/")
			}
			raw, err := json.Marshal(got.plan)
			require.NoError(t, err)
			for _, private := range []string{"tenant_id", "environment_id", "metadata", "internal_secret", "catalog_price", "commitment"} {
				assert.NotContains(t, string(raw), private)
			}
		})
	}
}

func TestRemoteSummary_TieredAndPackagePrices(t *testing.T) {
	f := newFakeCloud(map[string]string{
		"in:t1": "cust_1",
	})
	details := cloudSubscription(f, types.SubscriptionStatusActive)
	tiered := cloudPrice("tier_override")
	tiered["type"], tiered["billing_model"], tiered["tier_mode"], tiered["meter_id"] = "USAGE", "TIERED", "SLAB", "meter_1"
	tiered["meter"] = map[string]any{
		"id":   "meter_1",
		"name": "Events",
	}
	tiered["tiers"] = []any{
		map[string]any{
			"up_to":       1000,
			"unit_amount": "0.0000001",
			"flat_amount": "2.5",
		},
		map[string]any{
			"up_to":       nil,
			"unit_amount": "0.00000005",
		},
	}
	usage := cloudLine("li_usage", tiered)
	usage["quantity"] = "0"
	pack := cloudPrice("package_override")
	pack["billing_model"], pack["price_unit_type"], pack["price_unit"] = "PACKAGE", "CUSTOM", "CRD"
	pack["price_unit_amount"], pack["conversion_rate"] = "2.123456789", "0.25"
	pack["transform_quantity"] = map[string]any{
		"divide_by": 1000,
		"round":     "up",
	}
	details["line_items"] = []any{usage, cloudLine("li_addon", pack)}
	got, err := newTestRemoteClient(t, f).Summary(context.Background(), "in:t1")
	require.NoError(t, err)
	require.Len(t, got.plan.Charges, 2)
	charge := got.plan.Charges[0]
	assert.Equal(t, types.BILLING_MODEL_TIERED, charge.BillingModel)
	assert.Equal(t, "0", charge.Quantity.String())
	require.Len(t, charge.Tiers, 2)
	assert.Equal(t, "0.0000001", charge.Tiers[0].UnitAmount.String())
	assert.Equal(t, "2.5", charge.Tiers[0].FlatAmount.String())
	assert.Nil(t, charge.Tiers[1].UpTo)
	charge = got.plan.Charges[1]
	assert.Equal(t, "2.123456789", charge.PriceUnitAmount.String())
	assert.EqualValues(t, 1000, charge.TransformQuantity.DivideBy)
	assert.EqualValues(t, "up", charge.TransformQuantity.Round)
}

func TestRemoteSummary_NoCustomerOrCancelledOffersConfiguredPlan(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{
			false: "provisioning_pending",
			true:  "cancelled",
		}[cancelled], func(t *testing.T) {
			f := newFakeCloud(map[string]string{})
			if cancelled {
				f.byExtID["in:t1"] = "cust_1"
				cloudSubscription(f, types.SubscriptionStatusCancelled)
			}
			got, err := newTestRemoteClient(t, f).Summary(context.Background(), "in:t1")
			require.NoError(t, err)
			assert.Equal(t, "plan_1", got.plan.ID)
			assert.NotNil(t, got.plan.Charges)
			assert.Empty(t, got.plan.Charges)
			if !cancelled {
				assert.NotNil(t, got.invoices)
				assert.Nil(t, got.usage)
				for _, request := range f.requests {
					assert.NotContains(t, request, pathCustomerByExternalID)
					assert.NotContains(t, request, pathCustomerUsage)
				}
			}
		})
	}
}

func TestRemoteSummary_PricingErrorsNeverFallBack(t *testing.T) {
	tests := []struct {
		name   string
		change func(*fakeCloud, map[string]any)
		remote bool
	}{
		{
			name: "missing_expanded_price",
			change: func(_ *fakeCloud, line map[string]any) {
				delete(line, "price")
			},
		},
		{
			name: "wrong_expanded_price",
			change: func(_ *fakeCloud, line map[string]any) {
				line["price_id"] = "another_price"
			},
		},
		{
			name: "malformed_amount",
			change: func(_ *fakeCloud, line map[string]any) {
				line["price"].(map[string]any)["amount"] = "12,50"
			},
		},
		{
			name: "malformed_quantity",
			change: func(_ *fakeCloud, line map[string]any) {
				line["quantity"] = "invalid"
			},
		},
		{
			name: "malformed_tier_amount",
			change: func(_ *fakeCloud, line map[string]any) {
				line["price"].(map[string]any)["tiers"] = []any{map[string]any{
					"unit_amount": "invalid",
				}}
			},
		},
		{
			name: "negative_tier_boundary",
			change: func(_ *fakeCloud, line map[string]any) {
				line["price"].(map[string]any)["tiers"] = []any{map[string]any{
					"unit_amount": "0.1",
					"up_to":       -1,
				}}
			},
		},
		{
			name: "subscription_unavailable",
			change: func(f *fakeCloud, _ map[string]any) {
				f.failPaths["/subscriptions/sub_1/v2"] = cannedResponse{
					status: http.StatusServiceUnavailable,
					body:   `{"message":"unavailable"}`,
				}
			},
			remote: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{
				"in:t1": "cust_1",
			})
			details := cloudSubscription(f, types.SubscriptionStatusTrialing)
			tt.change(f, details["line_items"].([]any)[0].(map[string]any))
			_, err := newTestRemoteClient(t, f).Summary(context.Background(), "in:t1")
			require.Error(t, err)
			if tt.remote {
				assert.True(t, isRemoteFailure(err))
			} else {
				assert.True(t, ierr.IsInternal(err))
			}
			for _, request := range f.requests {
				assert.False(t, strings.Contains(request, "/plans/"), request)
			}
		})
	}
}

func TestLatestSubscription_IgnoresCancelledHistory(t *testing.T) {
	f := newFakeCloud(map[string]string{
		"in:t1":    "cust_1",
		"in:other": "cust_other",
	})
	for i := 0; i < 100; i++ {
		f.subs["in:t1"] = append(f.subs["in:t1"], map[string]any{
			"id":                  "cancelled",
			"subscription_status": "cancelled",
			"created_at":          time.Now(),
		})
	}

	f.subs["in:t1"] = append(f.subs["in:t1"], map[string]any{
		"id":                  "enterprise",
		"plan_id":             "enterprise_plan",
		"subscription_status": "paused",
		"created_at":          time.Now().Add(-time.Hour),
	})
	f.subs["in:other"] = []map[string]any{{
		"id":                  "other_tenant",
		"subscription_status": "active",
		"created_at":          time.Now(),
	}}

	got, err := newTestRemoteClient(t, f).LatestSubscription(context.Background(), "in:t1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "enterprise", got.id)
	require.Len(t, f.searches, 1)
	assert.EqualValues(t, 1, f.searches[0]["limit"])
	assert.Equal(t, "in:t1", f.searches[0]["external_customer_id"])
	assert.NotContains(t, f.searches[0]["subscription_status"], "cancelled")
}

func TestSummary_RemotePricingAndStatusUseSameSubscription(t *testing.T) {
	f := newFakeCloud(map[string]string{
		"in:t1": "cust_1",
	})
	details := cloudSubscription(f, types.SubscriptionStatusTrialing)
	details["subscription_status"] = types.SubscriptionStatusActive
	line := details["line_items"].([]any)[0].(map[string]any)
	line["price"].(map[string]any)["amount"] = "79.123456789"
	s, d := newTestService(newTestRemoteClient(t, f))
	s.Config.Billing.Region = "in"
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)

	got, err := s.Summary(tenantCtx("t1"), "t1")
	require.NoError(t, err)
	assert.Equal(t, types.TenantInternalStatusActive, got.Status)
	assert.Equal(t, types.SubscriptionStatusActive, got.SubscriptionStatus)
	require.NotNil(t, got.Plan)
	require.Len(t, got.Plan.Charges, 1)
	assert.Equal(t, "79.123456789", got.Plan.Charges[0].Amount.String())
	assert.Equal(t, 1, productionEnvs(t, d, "t1"))
}

func TestRemoteSummary_OfferErrorsAreReturned(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{
			false: "plan_unavailable",
			true:  "malformed_price",
		}[malformed], func(t *testing.T) {
			f := newFakeCloud(map[string]string{
				"in:t1": "cust_1",
			})
			if malformed {
				p := cloudPrice("broken")
				p["amount"] = "invalid"
				f.plans["plan_1"]["prices"] = []any{p}
			} else {
				f.failPaths["/plans/plan_1"] = cannedResponse{
					status: http.StatusServiceUnavailable,
					body:   `{"message":"unavailable"}`,
				}
			}
			_, err := newTestRemoteClient(t, f).Summary(context.Background(), "in:t1")
			require.Error(t, err)
			if malformed {
				assert.True(t, ierr.IsInternal(err))
			} else {
				assert.True(t, isRemoteFailure(err))
			}
		})
	}
}

func TestRemoteSummary_CancellationReselectsSubscription(t *testing.T) {
	for _, olderLive := range []bool{false, true} {
		t.Run(map[bool]string{
			false: "no_live_subscription",
			true:  "older_live_subscription",
		}[olderLive], func(t *testing.T) {
			f := newFakeCloud(map[string]string{
				"in:t1": "cust_1",
			})
			details := cloudSubscription(f, types.SubscriptionStatusActive)
			details["subscription_status"] = types.SubscriptionStatusCancelled

			if olderLive {
				f.subs["in:t1"] = append(f.subs["in:t1"], map[string]any{
					"id":                  "sub_older",
					"subscription_status": "active",
					"created_at":          time.Now().Add(-time.Hour),
				})
				f.subDetails["sub_older"] = map[string]any{
					"id":                  "sub_older",
					"subscription_status": "active",
					"plan": map[string]any{
						"id":   "plan_older",
						"name": "Existing terms",
					},
					"line_items": []any{cloudLine("li_older", cloudPrice("older_price"))},
				}
			}

			got, err := newTestRemoteClient(t, f).Summary(context.Background(), "in:t1")

			require.NoError(t, err)
			if olderLive {
				assert.Equal(t, "sub_older", got.subscription.id)
				assert.Equal(t, types.SubscriptionStatusActive, got.subscription.status)
				assert.Equal(t, "plan_older", got.plan.ID)
				require.Len(t, f.searches, 2)
			} else {
				assert.Equal(t, types.SubscriptionStatusCancelled, got.subscription.status)
				assert.Equal(t, "plan_1", got.plan.ID)
				require.Len(t, f.searches, 3)
			}
		})
	}
}

func TestRemoteSummary_EffectiveBucketSize(t *testing.T) {
	for _, offered := range []bool{false, true} {
		for _, priceBucket := range []string{"", "DAY"} {
			t.Run(map[bool]string{
				false: "subscribed",
				true:  "offered",
			}[offered]+"/"+priceBucket, func(t *testing.T) {
				f := newFakeCloud(map[string]string{
					"in:t1": "cust_1",
				})
				legacy := cloudPrice("legacy_price")
				legacy["type"] = "USAGE"
				legacy["meter_id"] = "meter_legacy"
				if priceBucket != "" {
					legacy["bucket_size"] = priceBucket
				}

				meter := map[string]any{
					"id":   "meter_legacy",
					"name": "Seats",
					"aggregation": map[string]any{
						"type":        "MAX",
						"bucket_size": "HOUR",
					},
				}

				if offered {
					legacy["meter"] = meter
					f.plans["plan_1"]["prices"] = []any{legacy}
				} else {
					details := cloudSubscription(f, types.SubscriptionStatusActive)
					details["line_items"] = []any{cloudLine("li_legacy", legacy), cloudLine("li_same_meter", legacy)}
					f.prices["legacy_price"] = map[string]any{
						"id":    "legacy_price",
						"meter": meter,
					}
				}

				got, err := newTestRemoteClient(t, f).Summary(context.Background(), "in:t1")

				require.NoError(t, err)
				if offered {
					require.Len(t, got.plan.Charges, 1)
				} else {
					require.Len(t, got.plan.Charges, 2)
				}

				wantBucket := priceBucket
				if wantBucket == "" {
					wantBucket = "HOUR"
				}

				for _, charge := range got.plan.Charges {
					assert.Equal(t, types.WindowSize(wantBucket), charge.BucketSize)
				}

				priceReads := 0
				for _, request := range f.requests {
					if strings.HasPrefix(request, "GET /prices/") {
						priceReads++
					}
				}

				if !offered && priceBucket == "" {
					assert.Equal(t, 1, priceReads)
				} else {
					assert.Zero(t, priceReads)
				}
			})
		}
	}
}
