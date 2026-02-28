# SDK test run – what was breaking and what was fixed

Summary of failures from the full SDK test run (Go, Python, TypeScript) and the fixes applied in this repo.

---

## Go

- **Cleanup:** "Customer cannot be deleted due to associated wallets" – **expected** (API business rule). Not a test bug.
- All other Go tests passed.

---

## Python (fixes applied in `api/tests/python/test_sdk.py`)

| Issue | Cause | Fix |
|-------|--------|-----|
| `billing_reason` literal_error – must be MANUAL not manual | API/Pydantic expect uppercase enum. | Use `billing_reason="MANUAL"` everywhere. |
| Invalid entitlement usage reset period | API expects uppercase enum (e.g. MONTHLY). | Use `usage_reset_period="MONTHLY"`. |
| `get_plan_credit_grants() got an unexpected keyword argument 'plan_id'` | OpenAPI path param is `id` (Plan ID). | Call `get_plan_credit_grants(id=test_plan_id)`. |
| Billing period count must be greater than 0 (price/subscription) | Create price was not sending `billing_period_count`. | Add `billing_period_count=1` to `create_price` in test. |
| Record payment: "invalid payment status transition from SUCCEEDED to succeeded" | Case mismatch – API expects uppercase. | **SDK/API:** Ensure SDK sends uppercase status if API expects it. |
| Credit grant scope "must be one of: [PLAN SUBSCRIPTION]" with Scope: PLAN | Possible backend validation or enum format. | **Verify:** API may require different enum value or format. |
| Credit note "Please provide a valid credit note reason" (BILLING_ERROR sent) | Possible reason enum/format or line-item validation. | **Verify:** Allowed reasons and request shape in API. |
| Wallet top up: "Invalid transaction reason" | Enum/request format. | **SDK/API:** Align transaction reason with API. |
| `get_wallet_transactions() got an unexpected keyword argument 'id'` | SDK method signature doesn’t match API. | **SDK:** Fix param name to match OpenAPI (e.g. wallet_id). |

---

## TypeScript (fixes applied in `api/tests/ts/test_sdk_js.ts`)

| Issue | Cause | Fix |
|-------|--------|-----|
| "Unexpected response shape" for create/get customer, feature, plan, addon, entitlement, subscription, price | Published SDK may return body under `result` or `data` instead of top level. | Added `unwrap(response)` helper and use it for all create/get calls that set global IDs so tests accept both `response` and `response.result` / `response.data`. |
| Update entitlement: 404 page not found | Wrong URL or method for update entitlement. | **SDK/API:** Check base path and path params for update entitlement. |
| Search wallets: Status 500 | Server error. | **Backend:** Investigate wallet search endpoint. |

---

## Summary

- **In this repo:** Python enum casing (`MANUAL`, `MONTHLY`), Python `get_plan_credit_grants(id=...)`, Python `billing_period_count=1` for price, and TypeScript response unwrapping for entity bodies are fixed.
- **Remaining:** Python payment/credit note/wallet SDK vs API alignment; TS entitlement update 404 and wallet search 500 need SDK or backend fixes.
