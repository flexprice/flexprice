# Real-time usage for prepaid wallets

**Ticket:** [FLE-2405](https://linear.app/flexprice/issue/FLE-2405/prepaid-subscription) · **Status:** design agreed, a few open items · 

## Summary

Prepaid customers need to know, instantly, how much balance is left and whether they can afford the next use of a feature. Today each balance check re-adds the customer's entire month of usage in ClickHouse. That's slow for small customers and impossible for one sending 10,000 events per second.

This design keeps a **running count of usage per customer and meter**:
- **Recent hours** live in Redis, updated as events arrive.
- **Older usage** is folded into a Postgres snapshot by an hourly job.

A balance check adds the two and prices the result in memory. ClickHouse remains the source of truth and the fallback, but it leaves the hot path.

## 1. Background: why balance reads are slow today

```
balance = wallet balance − unpaid invoices − (this period's usage, priced) + credits already applied
```

The last part is the problem. On every read we:
1. list the customer's subscriptions;
2. ask ClickHouse to aggregate each meter's usage **from the start of the billing period**;
3. price it.

The work grows with how much the customer has used this month:

| Customer volume | Rows to aggregate by mid-month |
|---|---|
| 1 event/s | ~1.3M (milliseconds) |
| 10k events/s | **~13 billion (10+ seconds per read)** |

We've already seen the effects: wallet-balance 500s from many sequential ClickHouse aggregations, and ClickHouse CPU spent on these reads. A cache (30 min) hides it but serves stale balances.

## 2. Goals and non-goals

**Goals**
- Balance and "can this customer afford X?" answered in milliseconds, for a customer ingesting **10k events/s**.
- Numbers that stay correct: duplicates, late events, replays and deletions all end up counted exactly once.
- Much less ClickHouse load from balance reads, for every eligible customer, not only this one.

**Non-goals for V1**
- Changing how or when wallets are debited. That still happens at invoice finalization, exactly as today. There are no hourly debits.
- Changing invoicing. Invoices keep reading ClickHouse with deduplication (FINAL).
- Complex pricing: price overrides, commitments, bucketed meters, entitlement grants, and meters other than SUM, COUNT and MAX. Those subscriptions keep using today's path.

## 3. Key decision: count quantity, price at read time

We store **how much was used** (quantity per meter), not **how much it cost**.

Money looks simpler: add each event's price to a running total. But tiered and volume pricing price the **period total** ("the first 1M calls at ₹0.01, the rest at ₹0.005"), so a per-event price is wrong once a tier is crossed. Storing quantity keeps pricing independent: we apply the customer's current price to the running total whenever someone asks. Pricing in memory takes microseconds (measured: 0.3 µs for 5 meters, 26 µs for 500).

## 4. How it works

### 4.1 The three layers

| Layer | Holds | Updated |
|---|---|---|
| **Redis hourly buckets** | usage per customer, meter and hour, for roughly the last 3–4 hours | on every event, by the ingestion consumer |
| **Postgres snapshot** | usage per customer, meter and billing period, up to a time called `aggregated_until` | every hour, by a job that reads ClickHouse with deduplication |
| **ClickHouse** | every event (source of truth) | as today |

A balance read adds them: **snapshot + Redis buckets after the snapshot**. Any hour Redis doesn't have is fetched from ClickHouse.

### 4.2 A day in the life (example at 13:20)

1. An event arrives at 13:05. After it's written to ClickHouse, the consumer adds its quantity to the Redis bucket for hour 13.
2. At 13:00 the hourly job **finalized up to 10:00**. It read hours before 10:00 from ClickHouse (with deduplication) and folded them into the Postgres snapshot. Hours 10–13 stay in Redis. That 3-hour gap is the **late window**: events arriving a little late still land in Redis.
3. A balance read at 13:20 takes the snapshot (usage up to 10:00) + Redis buckets 10, 11, 12, 13, prices the total, and subtracts it from the wallet.
4. An event with timestamp 08:30 arriving at 13:20 is older than what Redis covers. It goes to the **late-event path** (section 4.6), which corrects the snapshot.

### 4.3 Ingestion

- After an event is safely in ClickHouse, the consumer adds its quantity to the customer's Redis bucket for the event's hour. There's no extra database lookup. Within each batch, events are summed first, so 200 events become a handful of Redis writes.
- **Duplicates:** the existing per-event dedup lock in Redis stays. Its lifetime drops from 24h to **5h**, because after that a duplicate can only arrive through the late-event path, where ClickHouse deduplicates it. That cuts lock memory from ~121 GB to ~25 GB at 10k events/s.
- **Too old for Redis:** events older than the late window skip Redis and go to the late-event path. The consumer re-checks this at processing time, which catches events delayed by consumer lag, DLQ replays and reprocessing.
- **Redis failures never block ingestion.** A missed increment shows up as a small under-count until the next hourly job fixes it, and it's tracked by a metric.
- **Throughput:** 10k events/s needs the **bulk** ingestion path. The single-event path inserts one event at a time and can't reach it. The bulk consumer must take its dedup locks in one batch call instead of one call per event. See the LLD for sizing.

### 4.4 The hourly job

- Runs every hour, and only for customers who had usage.
- For each customer and meter it reads the newly finalized hours from ClickHouse **with deduplication** and adds them to the Postgres snapshot. Each run makes the snapshot exactly right up to its point, so it also corrects anything Redis got wrong.
- Customers with no usage are skipped, and their snapshot simply stays where it is.
- When a billing period rolls over, usage after the boundary goes into a new snapshot row for the new period.

### 4.5 Reading the balance

Today a balance read loads the customer's subscriptions, line items, prices, meters, entitlements and commitments from Postgres on every call, then aggregates ClickHouse. The new flow removes all of that from the hot path:

- **Eligibility is a stored flag on the customer** (Postgres), recomputed whenever something that affects it changes: subscriptions, prices, addons, grants, plan changes and period rollover. The hourly job also re-checks it as a safety net. Not eligible means today's path, unchanged.
- **A small pricing profile per customer is cached in Redis:** the eligibility flag, the customer's external ID, and for each subscription its period and a `meter → price` map built from the plan and addon prices. Line items are never read. That's safe because any price change makes the subscription ineligible until the next period. The profile is rebuilt on a miss and cleared whenever the flag is recomputed.
- **A read is:** one Redis round trip (profile + snapshot + recent hours, all stored together for the customer), the wallet and unpaid invoices from Postgres, and pricing in memory. ClickHouse is only touched for missing hours.
- **Missing data falls back to ClickHouse:** a missing hour, a missing snapshot, or a Redis problem. For customers with little usage that fallback is cheap. Protecting ClickHouse when Redis is down entirely is still to be designed.
- **One switch, behind flags.** The change lives in the single function that billing, wallet balance, alerts and the usage summary all use to fetch usage. A per-tenant flag chooses the new flow or today's ClickHouse flow, and a second flag chooses where pricing config comes from (cache or Postgres). Invoicing can adopt the same flow later. Until then, invoice finalization keeps running against ClickHouse with deduplication.

### 4.6 Late events, replays and deletions

Anything that changes usage the hourly job has already finalized must correct the snapshot:
- late events, routed to a separate Kafka topic;
- DLQ replays;
- reprocessing;
- event deletions.

The late-event consumer writes the event to ClickHouse and notifies **one recompute workflow per tenant**. The workflow waits a few minutes to gather everything, then corrects the affected snapshots in batches. A hundred customers with a hundred late events each becomes one workflow run, not ten thousand. **How** it corrects them is the open problem in section 7.

## 5. Feature access check

A new endpoint answers "can this customer use N more units of feature X?":

1. find the feature's meter, its price and the wallet in that price's currency (one wallet per currency);
2. take the balance as in section 4.5;
3. allow if `balance ≥ price(current usage + N) − price(current usage)`.

Pricing the difference keeps tiers correct. Subscriptions outside V1 scope get the same answer through today's slower path.

The check doesn't reserve balance. Usage still in the ingestion pipeline isn't visible yet, so concurrent checks could overspend by roughly *check rate × ingestion lag × price*. **The client handles this.**

## 6. Alerts

Wallet alerts and auto top-up already compute the balance through the same function, so they get the new flow automatically. Because a read is now cheap, prepaid tenants can run alerts much more often than today's 5½-minute delay. Late-event corrections also trigger alerts, since they can drop a balance.

## 7. Open problem: correcting snapshots after late events

**The problem.** The simplest correction is to recompute the customer's whole billing period from ClickHouse with deduplication. It's exact, and cheap for normal customers. For the 10k events/s customer it's too heavy:

| Customer volume | Rows in a 30-day period | Whole-period recompute (month end, rough) |
|---|---|---|
| 1 event/s | ~2.6M | ~0.02 s |
| 100 events/s | ~260M | ~1–3 s |
| 1k events/s | ~2.6B | ~15–30 s |
| **10k events/s** | **~26B** | **~2–4 min** |

A steady trickle of late events from that customer would mean a multi-minute, memory-heavy ClickHouse query every few minutes, competing with invoicing, and getting worse as the month goes on.

The root cause is that the snapshot keeps **one number per period**. It can't tell how much of that number came from day 9, so fixing day 9 means recounting everything. (The team's earlier money-based design avoided this only because it recorded a debit per hour.)

**Options**

| | Approach | Cost per correction | Exact? | Trade-off |
|---|---|---|---|---|
| **A** | **Keep daily (or hourly) partial totals inside the snapshot row.** Recompute only the affected day | one customer-day (~5–15 s for the hot customer; ~1 s with hourly partials) | ✅ | slightly larger row (~1 KB daily, ~20 KB hourly) |
| B | Longer late window (Redis keeps 6–24h) | fewer corrections | ✅ | more Redis memory |

Point lookups in ClickHouse to detect duplicates were considered and rejected.

**Recommendation:** A with daily partials, using B as a tuning knob. Switch high-volume customers to hourly partials if the load test shows a customer-day is still too heavy. **Decision pending.**

## 8. How the numbers stay correct

| Situation | What happens |
|---|---|
| Pipeline redelivers an event, or the client resends within 5h | dedup lock skips it |
| Client resends after 5h | goes through the late-event path, where ClickHouse deduplicates it |
| A batch fails halfway | locks released, batch retried; Redis is only updated after the whole batch succeeds |
| Redis misses a write, or a failover loses locks | small error until the hourly job finalizes that hour from ClickHouse |
| Event arrives after its hour was finalized | late-event path corrects the snapshot |
| Events are deleted | deletion triggers the same correction and clears the affected Redis hours |
| Hourly job fails | reads keep using Redis and ClickHouse for the hours the snapshot doesn't cover yet |
| Redis is down entirely | reads fall back to ClickHouse (guarding against overload is still to be designed) |

**Accepted gaps**
- Regular (non-invoice) ClickHouse reads don't deduplicate. With the shorter lock lifetime, more duplicate rows reach ClickHouse, and they show in the slow path and fallback reads until ClickHouse merges them. Invoices are unaffected.
- A resend with the same event ID but a **different timestamp** is counted twice by ClickHouse (known, unchanged from today).

## 9. Scope of V1 (eligibility)

Eligibility is decided per customer and stored on the customer. A customer uses the new flow only if **all** of their active subscriptions meet these conditions. Otherwise they use today's path, unchanged:
- only SUM, COUNT or MAX meters;
- no price overrides, commitments, bucketed meters or entitlement grants;
- no price change in the current period, on the subscription, its plan or its addons (later: split the snapshot at the change). After a change, the customer becomes eligible again at the next period, once the subscription is synced with its plan;
- not a parent/child subscription (later: add up the children).

## 10. Rollout

| Phase | What ships | Serves balances? |
|---|---|---|
| 1 | Ingestion to Redis, hourly job, late-event path, period rollover, metrics comparing against ClickHouse; benchmark + simulation on a throwaway cluster | no |
| 2 | Read path, eligibility, pricing cache, feature access check, alerts; enabled tenant by tenant | yes |

After a tenant is enabled, a metric keeps comparing the new balance with ClickHouse. A difference that survives an hourly run is a bug.

## 11. Open items

1. **Late-event correction method** (section 7).
2. Bulk consumer capacity per region (partitions, replicas, rate limit), from the load test.
3. Redis sizing, and confirming the 3h late window and 5h lock lifetime, from the benchmark.
4. Expected balance and feature-check read rate. It must be fast; the load test sets the target.
5. Prepaid alert thresholds and timing.
6. Guarding the fallback against a ClickHouse stampede when Redis is down, and which reads to cache (discussed later).
7. Shadow comparison sampling (later).
