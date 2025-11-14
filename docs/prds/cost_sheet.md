## 🚀 Objective: Implement Cost Sheet Revenue Analytics

We’ve recently integrated a **Cost Sheet** module into the system. The goal is to build an **analytics layer** on top of it that provides key financial insights such as **ROI, Margin, Total Cost, and Revenue Breakdown**.

---

## 🧩 Architecture Overview

### 1. Existing Analytics API

* We already have a **Function Analytics API** that operates based on `customer_id`.
* This API aggregates **usage and revenue data** from existing billing and tracking systems.

### 2. New Cost Sheet Analytics API

* Build a **new API endpoint** to compute **cost-related metrics**.
* Data Source: `events` table in **ClickHouse** (contains raw usage and billing events).
* Flow:

  1. Fetch raw usage events from ClickHouse.
  2. Pass them through existing **Billing Services** to compute associated costs.
  3. Calculate:

     * **Total Cost**
     * **Breakdown per meter** (granular cost attribution)

### 3. Combined Analytics Layer

* Integrate the **Function Analytics API** (revenue data) and **Cost Sheet Analytics API** (cost data).
* Compute derived metrics such as:

  * **Revenue** (from existing function analytics)
  * **Total Cost**
  * **Margin = (Revenue - Cost)**
  * **ROI = (Revenue - Cost) / Cost**
* Expose this as a **unified analytics API**, providing both high-level and detailed breakdowns.

---

## ⚙️ Implementation Notes

* Use existing **Billing Services** for cost calculation to ensure consistency.
* The new API should be modular, allowing independent computation of cost or revenue if needed.
* Optimize ClickHouse queries for aggregation (consider materialized views if necessary).
* Ensure the API is **scalable** and supports **batch processing** for multiple customers.
