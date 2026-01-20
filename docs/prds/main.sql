-- Query 1: Line items to terminate (plan-wide)
WITH subs AS (
  SELECT id
  FROM subscriptions
  WHERE tenant_id = 'tenant_01K1TJDVNSN7TWY8CZY870QMNV'
    AND environment_id = 'env_01K5RA3WB8QBRZ1DRDPVE7GPZ2'
    AND status = 'published'
    AND plan_id = 'plan_01KFD76GMWNKFPMKQ599BEV0NX'
    AND subscription_status IN ('active','trialing')
),
ended_plan_prices AS (
  SELECT id, end_date
  FROM prices
  WHERE tenant_id = 'tenant_01K1TJDVNSN7TWY8CZY870QMNV'
    AND environment_id = 'env_01K5RA3WB8QBRZ1DRDPVE7GPZ2'
    AND status = 'published'
    AND entity_type = 'PLAN'
    AND entity_id = 'plan_01KFD76GMWNKFPMKQ599BEV0NX'
    AND end_date IS NOT NULL
    AND type <> 'FIXED'
)
SELECT
  li.id              AS line_item_id,
  li.subscription_id AS subscription_id,
  li.price_id        AS price_id,
  p.end_date         AS target_end_date
FROM subscription_line_items li
JOIN subs s
  ON s.id = li.subscription_id
JOIN ended_plan_prices p
  ON p.id = li.price_id
WHERE
  li.tenant_id = 'tenant_01K1TJDVNSN7TWY8CZY870QMNV'
  AND li.environment_id = 'env_01K5RA3WB8QBRZ1DRDPVE7GPZ2'
  AND li.status = 'published'
  AND li.entity_type = 'plan'
  AND li.end_date IS NULL
;



-- Query 2: Missing (subscription_id, price_id) pairs to create (plan-wide)
WITH subs AS (
  SELECT id, tenant_id, environment_id, currency, billing_period, billing_period_count
  FROM subscriptions
  WHERE tenant_id = 'tenant_01K1TJDVNSN7TWY8CZY870QMNV'
    AND environment_id = 'env_01K5RA3WB8QBRZ1DRDPVE7GPZ2'
    AND status = 'published'
    AND plan_id = 'plan_01KFD76GMWNKFPMKQ599BEV0NX'
    AND subscription_status IN ('active','trialing')
),
plan_prices AS (
  SELECT id, tenant_id, environment_id, currency, billing_period, billing_period_count
  FROM prices
  WHERE tenant_id = 'tenant_01K1TJDVNSN7TWY8CZY870QMNV'
    AND environment_id = 'env_01K5RA3WB8QBRZ1DRDPVE7GPZ2'
    AND status = 'published'
    AND entity_type = 'PLAN'
    AND entity_id = 'plan_01KFD76GMWNKFPMKQ599BEV0NX'
    AND type <> 'FIXED'
)
SELECT
  s.id AS subscription_id,
  p.id AS missing_price_id
FROM subs s
JOIN plan_prices p
  ON p.currency = s.currency
 AND p.billing_period = s.billing_period
 AND p.billing_period_count = s.billing_period_count
WHERE
  NOT EXISTS (
    SELECT 1
    FROM prices sp
    WHERE sp.tenant_id = s.tenant_id
      AND sp.environment_id = s.environment_id
      AND sp.status = 'published'
      AND sp.entity_type = 'SUBSCRIPTION'
      AND sp.entity_id = s.id
      AND sp.parent_price_id = p.id
  )
  AND NOT EXISTS (
    SELECT 1
    FROM subscription_line_items li
    WHERE li.tenant_id = s.tenant_id
      AND li.environment_id = s.environment_id
      AND li.status = 'published'
      AND li.subscription_id = s.id
      AND li.price_id = p.id
      AND li.entity_type = 'plan'
  )
;