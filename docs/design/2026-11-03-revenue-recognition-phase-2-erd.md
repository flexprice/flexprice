# Revenue Recognition — Phase 2 (ERP posting)

**Author:** Harshit Gupta

**Dated**: 3rd Nov, 2026

Figures marked *assumed* are illustrative. Code facts are as of commit `2d35c1f8d`.

## 1. Problem

Flexprice's journal knows what was earned; the customer's books live in their ERP. Phase 2 posts Flexprice journal entries to Zoho Books through Rutter, against accounts the customer mapped, without the ERP counting anything twice.

### Example: Agent Workforce (assumed)

$60,000 annual access + $60,000 annual usage bank (available upfront), billed as four quarterly $30,000 invoices (15,000 + 15,000). Two POBs, $60,000 each. Q1 usage drawn: $8,000. Access earns monthly-even.


| End of Q1 | Access | Usage bank | Total  |
| --------- | ------ | ---------- | ------ |
| Billed    | 15,000 | 15,000     | 30,000 |
| Earned    | 15,000 | 8,000      | 23,000 |
| Deferred  | 0      | 7,000      | 7,000  |


Bank remaining: 52,000. Where the 7,000 ends up depends on the unused-bank policy (expiry, refund or rollover), which is still open.

### Why mappings are needed

Journal lines post to eight fixed roles: `cash`, `accounts_receivable`, `unbilled_receivable`, `deferred_revenue`, `customer_credit`, `tax_payable`, `revenue`, `contra_revenue`. Zoho needs an account ID from one organization's chart of accounts.

A line resolves its account in this order:

1. The Product-specific mapping for its role.
2. Otherwise, the connection-wide mapping for its role.
3. Otherwise, the role is unresolved and the **whole entry** is blocked. It still exists in Flexprice.

Product is what lets Agent Workforce's two `revenue` credits land in "Platform revenue" and "Usage revenue". Journal lines carry no `product_id` today, only `pob_id`, so Product has to be stored on the line.

### Why invoice export is not enough

Flexprice already syncs invoices to Zoho Books and QuickBooks. A synced invoice books revenue on its invoice date. For Agent Workforce, the Q1 invoice books 30,000 of revenue against 23,000 earned, or 0 if it is dated Apr 1.

If the invoice sync and journal posting both run, the same money lands twice:


| Q1 in Zoho | Invoice sync | Journals | Zoho shows | Correct |
| ---------- | ------------ | -------- | ---------- | ------- |
| Receivable | 30,000       | 30,000   | 60,000     | 30,000  |
| Revenue    | 30,000       | 23,000   | 53,000     | 23,000  |




### Rule: one carrier per effect

Each financial effect reaches the ERP once, either through a synced document or through a journal. **Anything not synced as a document goes out as a journal**; this is the default. Two modes follow from that:


| Mode                                | Documents synced                           | Journals posted                                                                        |
| ----------------------------------- | ------------------------------------------ | -------------------------------------------------------------------------------------- |
| Journals only                       | None                                       | Every entry                                                                            |
| Documents + recognition (**pilot**) | Invoices; payments when mark-as-paid is on | Recognition, offset, and everything not synced (credit notes, refunds, other payments) |


In the pilot mode, Zoho invoice lines book to the account mapped to `deferred_revenue`, not to an income account. Agent Workforce then gives Zoho: invoice Dr AR 30,000 / Cr Deferred 30,000; journals Dr Unbilled / Cr Revenue 23,000 and Dr Deferred / Cr Unbilled 23,000. Zoho ends with AR 30,000, revenue 23,000 and deferred 7,000, which matches Flexprice.

## 2. ERP Provider

### Interface


| Method                      | Purpose                                                                   |
| --------------------------- | ------------------------------------------------------------------------- |
| `ListAccounts`              | The chart of accounts: ERP ID, name, type, status                         |
| `ListItems`                 | ERP items, used for the Product mapping                                   |
| `CreateJournal(entry, key)` | Posts one entry. `key` is the Flexprice journal entry ID                  |
| `GetJournalResult(key)`     | Returns the outcome of an earlier create, after a timeout or an async job |
| `Capabilities`              | What this provider supports for this ERP (below)                          |


Capabilities shape posting, so they are reported rather than assumed. Each value below was tested against a Zoho Books sandbox.


| Capability                        | Rutter on Zoho                  | Apideck on Zoho                               |
| --------------------------------- | ------------------------------- | --------------------------------------------- |
| Idempotent create                 | Yes, `Idempotency-Key` (async)  | No; Apideck rejects the key for Zoho journals |
| Customer required on every line   | Yes                             | No                                            |
| Update or delete a posted journal | No                              | Yes                                           |
| Account on each invoice line      | Not yet readable (sync pending) | Yes                                           |
| Read freshness                    | Rutter's synced copy            | Live                                          |




### Rules that follow

- **Posted journals are never edited in the ERP.** A correction is a new reversing entry, matching Flexprice's immutable journal.
- **Retries never duplicate.** Where the provider takes an idempotency key, the Flexprice entry ID is the key; nothing Flexprice-specific is written into the ERP journal.
- **An unknown outcome is never retried automatically** where the provider has no idempotency (Apideck on Zoho). The entry waits for review: the reviewer either links the ERP journal it created, or confirms it is absent and Flexprice resends it. Flexprice lists likely matches (same date, amount, accounts, customer) to speed this up, but never decides alone.
- **Every line carries the customer** when the provider requires it: the Zoho contact of the entry's customer.
- **Errors fall into three outcomes:**
  - Rejected (unbalanced, unknown account): blocked until a mapping changes.
  - Connection needs attention (disconnected, not ready): paused for the whole connection.
  - Throttled or ERP unavailable: retried later.
- **Flexprice shows its own journal, invoice entries included.** Each entry shows how it reached the ERP: through the synced invoice or as a posted journal, with the ERP record. The ERP's own invoice postings are not read back as journals; when needed they are checked per invoice from the invoice's line accounts.



## 3. Product to ERP mapping



## 4. Chart of accounts sync



## 5. Posting

