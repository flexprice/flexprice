# Local SDK Test Failures Summary

From `make test-sdk-local` output (Go + Python + TypeScript).

---

## 1. Python (local) – no output

**What happened:** Between `--- Python (local) ---` and `--- TypeScript (local) ---` there is no Python output.

**Likely cause:** Only stdout was captured. If Python failed (import error, missing deps, or script error), everything went to **stderr** and was not saved.

**What to do:**
- Capture both streams: `make test-sdk-local > a.txt 2>&1`
- Or run Python alone: `cd api/tests/python && python test_local_sdk.py` and check the terminal for errors.

---

## 2. TypeScript – “Unexpected response shape” (root cause)

These tests expect the SDK to return the **entity object** (e.g. `{ id, name, ... }`). They check `response && 'id' in response && response.id`. If the API (or SDK) returns something else, they print “Unexpected response shape”.

| Area | Failing tests | Why it fails |
|------|----------------|--------------|
| **Customers** | Create, Get, Update, Lookup by External ID | Response doesn’t have top-level `id` (or is wrapped / wrong shape). |
| **Entitlements** | Create, Get, Update | Same: response shape doesn’t match. |
| **Subscriptions** | Create | Same → no `testSubscriptionID` → later subscription tests skipped. |
| **Prices** | Create | Same → no `testPriceID` → later price tests skipped. |

**Likely causes:**
- API returns a **wrapper** (e.g. `{ data: { id, name, ... } }`) and the TypeScript SDK returns that wrapper instead of unwrapping to the entity. The test then sees no `id` on the top-level object.
- Or the SDK returns a **different type** (e.g. error body or empty object) that still parses as success but has no `id`.

**What to do:**
- In the TS test, temporarily log the actual value:  
  `console.log('createCustomer response:', JSON.stringify(response, null, 2));`  
  right after `createCustomer`. That will show whether the body is wrapped (e.g. `data`) or missing `id`.
- If the API really returns a wrapper, either:
  - Change the SDK (or Speakeasy config) so single-entity endpoints return the unwrapped object, or
  - Change the test to use the wrapped shape (e.g. `response.data?.id`).

---

## 3. TypeScript – Update Entitlement 404

**Message:**  
`Error updating entitlement: Unexpected Status or Content-Type: Status 404 Content-Type text/plain. Body: 404 page not found`

**Cause:** Create Entitlement failed above, so `testEntitlementID` is never set (empty string). The client then calls something like `PUT /entitlements/` or `PUT /entitlements/undefined`, which the server doesn’t have → 404.

**Fix:** Fix Create Entitlement (and thus “Unexpected response shape”) so `testEntitlementID` is set; then Update Entitlement should succeed.

---

## 4. TypeScript – Search Wallets 500

**Message:**  
`Error searching wallets: Unexpected Status or Content-Type: Status 500 Content-Type "". Body: ""`

**Cause:** The test calls `client.wallets.queryWallet({})`. The server returns **500** with an empty body. So either:
- The backend has a bug for this endpoint with the given (or empty) query, or
- The request (e.g. query params or body) sent by the SDK doesn’t match what the API expects.

**What to do:**
- Reproduce with curl/Postman against the same host and same auth; if 500 persists, fix the backend.
- If the backend expects different parameters, align the SDK call or the test with the API (e.g. customer id, filters).

---

## 5. Cascading skips in TypeScript

Because Create Customer and Create Subscription fail:

- No `testCustomerID` → invoice, payment, wallet, credit note tests that need a customer are skipped.
- No `testSubscriptionID` → subscription get/entitlements/usage/cancel/addon, etc. are skipped.

Fixing the “Unexpected response shape” for Customer and Subscription (and then Entitlement and Price) will unblock these.

---

## Summary table

| Failure | Section | Cause |
|--------|--------|--------|
| No Python output | Python | stderr not captured or Python failed before printing. |
| Unexpected response shape | TS: Customer, Entitlement, Subscription, Price | API/SDK response not the plain entity (e.g. wrapped or wrong shape). |
| Update Entitlement 404 | TS: Entitlements | Create Entitlement failed → empty `testEntitlementID` → bad URL. |
| Search Wallets 500 | TS: Wallets | Server returns 500 for `queryWallet({})`; backend or request shape. |

**Recommended order:**  
1) Capture stderr and re-run to see Python errors.  
2) Log TS `createCustomer` (and if needed `createEntitlement` / `createSubscription` / `createPrice`) response and fix response shape (SDK or test).  
3) Re-run; then debug Search Wallets 500 (backend or request) if it still fails.
