-- migrate:up
-- New tables and indexes are empty. The live invoice table gets a nullable column
-- and an unvalidated FK; the later validation migration avoids a scan under its DDL lock.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE "billing_terms" (
  "id" character varying(50) NOT NULL,
  "tenant_id" character varying(50) NOT NULL,
  "status" character varying(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" character varying NULL,
  "updated_by" character varying NULL,
  "environment_id" character varying(50) NULL DEFAULT '',
  "owner_type" character varying(50) NOT NULL,
  "owner_id" character varying(50) NOT NULL,
  "kind" character varying(20) NOT NULL,
  "value_type" character varying(20) NOT NULL,
  "value" numeric(20,8) NULL,
  "bill_on" timestamptz NULL,
  "on_trigger" boolean NOT NULL DEFAULT false,
  "start_date" timestamptz NULL,
  "end_date" timestamptz NULL,
  "interval" character varying(20) NULL,
  "interval_count" bigint NULL,
  "bill_at" character varying(20) NULL,
  "split" character varying(20) NULL,
  "description" character varying(255) NULL,
  "metadata" jsonb NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "billing_terms_kind" CHECK (kind IN ('once', 'recurring')),
  CONSTRAINT "billing_terms_shape" CHECK (
    (kind = 'once'
      AND ((on_trigger AND bill_on IS NULL) OR (NOT on_trigger AND bill_on IS NOT NULL))
      AND start_date IS NULL
      AND end_date IS NULL
      AND "interval" IS NULL
      AND interval_count IS NULL
      AND bill_at IS NULL
      AND split IS NULL)
    OR (kind = 'recurring'
      AND NOT on_trigger
      AND bill_on IS NULL
      AND start_date IS NOT NULL
      AND end_date IS NOT NULL
      AND start_date < end_date
      AND "interval" IS NOT NULL
      AND "interval" IN ('DAILY', 'WEEKLY', 'MONTHLY', 'QUARTERLY', 'HALF_YEARLY', 'ANNUAL')
      AND interval_count IS NOT NULL
      AND interval_count > 0
      AND bill_at IS NOT NULL
      AND bill_at IN ('advance', 'arrear')
      AND ((value_type = 'fixed' AND split IS NULL)
        OR (value_type = 'remainder' AND split IS NOT NULL AND split IN ('even', 'proportional'))))
  ),
  CONSTRAINT "billing_terms_value" CHECK (
    (value_type = 'fixed' AND value IS NOT NULL AND value > 0)
    OR (value_type = 'remainder' AND value IS NULL)
  ),
  CONSTRAINT "billing_terms_value_type" CHECK (value_type IN ('fixed', 'remainder'))
);

CREATE INDEX "billingterm_tenant_id_environment_id_owner_type_owner_id_status"
  ON "billing_terms" ("tenant_id", "environment_id", "owner_type", "owner_id", "status");

CREATE INDEX "billingterm_tenant_id_environm_9115a8dcaafd4a46d8401332500aae06"
  ON "billing_terms" ("tenant_id", "environment_id", "owner_type", "owner_id", "created_at", "id");

CREATE TABLE "billing_schedule_items" (
  "id" character varying(50) NOT NULL,
  "tenant_id" character varying(50) NOT NULL,
  "status" character varying(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" character varying NULL,
  "updated_by" character varying NULL,
  "environment_id" character varying(50) NULL DEFAULT '',
  "owner_type" character varying(50) NOT NULL,
  "owner_id" character varying(50) NOT NULL,
  "customer_id" character varying(50) NOT NULL,
  "subscription_id" character varying(50) NULL,
  "currency" character varying(10) NOT NULL,
  "kind" character varying(20) NOT NULL DEFAULT 'charge',
  "bill_trigger" character varying(20) NOT NULL,
  "bill_on" timestamptz NULL,
  "amount" numeric(20,8) NOT NULL,
  "description" character varying(255) NOT NULL,
  "service_period_start" timestamptz NOT NULL,
  "service_period_end" timestamptz NOT NULL,
  "delivery" character varying(30) NOT NULL,
  "schedule_status" character varying(20) NOT NULL DEFAULT 'scheduled',
  "invoice_id" character varying(50) NULL,
  "invoice_line_item_id" character varying(50) NULL,
  "cancellation_reason" character varying(30) NULL,
  "metadata" jsonb NULL,
  "billing_term_id" character varying(50) NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "billing_schedule_items_billing_terms_schedule_items" FOREIGN KEY ("billing_term_id") REFERENCES "billing_terms" ("id") ON DELETE RESTRICT,
  CONSTRAINT "billing_schedule_items_amount" CHECK (amount > 0),
  CONSTRAINT "billing_schedule_items_cancellation" CHECK (
    (schedule_status = 'canceled'
      AND cancellation_reason IS NOT NULL
      AND cancellation_reason IN ('terms_amended', 'invoice_voided', 'termination'))
    OR (schedule_status != 'canceled' AND cancellation_reason IS NULL)
  ),
  CONSTRAINT "billing_schedule_items_delivery" CHECK (
    delivery IN ('subscription_invoice', 'separate_invoice')
    AND (delivery != 'subscription_invoice' OR subscription_id IS NOT NULL)
  ),
  CONSTRAINT "billing_schedule_items_kind" CHECK (
    kind IN ('charge', 'settlement')
    AND (kind = 'settlement' OR billing_term_id IS NOT NULL)
  ),
  CONSTRAINT "billing_schedule_items_service_period" CHECK (
    service_period_start < service_period_end
  ),
  CONSTRAINT "billing_schedule_items_state" CHECK (
    schedule_status IN ('scheduled', 'invoiced', 'canceled')
    AND ((invoice_id IS NULL AND invoice_line_item_id IS NULL)
      OR (invoice_id IS NOT NULL AND invoice_line_item_id IS NOT NULL))
    AND (schedule_status != 'scheduled' OR invoice_id IS NULL)
    AND (schedule_status != 'invoiced' OR invoice_id IS NOT NULL)
  ),
  CONSTRAINT "billing_schedule_items_trigger" CHECK (
    bill_trigger IN ('on_date', 'manual')
    AND (bill_trigger = 'manual' OR bill_on IS NOT NULL)
  )
);

CREATE INDEX "billingscheduleitem_tenant_id__24f6aea3a5421c5f5b548b13aa632a02"
  ON "billing_schedule_items" ("tenant_id", "environment_id", "owner_type", "owner_id", "status");

CREATE INDEX "billingscheduleitem_tenant_id__764c626ab728dd6bb1a466a0fb94f9ed"
  ON "billing_schedule_items" ("tenant_id", "environment_id", "status", "schedule_status", "delivery", "bill_on", "id");

CREATE INDEX "billingscheduleitem_tenant_id_environment_id_billing_term_id"
  ON "billing_schedule_items" ("tenant_id", "environment_id", "billing_term_id");

CREATE INDEX "billingscheduleitem_tenant_id_environment_id_invoice_id"
  ON "billing_schedule_items" ("tenant_id", "environment_id", "invoice_id");

CREATE INDEX "billingscheduleitem_tenant_id__f6df45cb20748debac0129b185aa3145"
  ON "billing_schedule_items" ("tenant_id", "environment_id", "owner_type", "owner_id", "created_at", "id");

CREATE UNIQUE INDEX "billingscheduleitem_tenant_id__7bb1adcbe0b2079762a2309f54eeeaa3"
  ON "billing_schedule_items" ("tenant_id", "environment_id", "invoice_line_item_id") WHERE (invoice_line_item_id IS NOT NULL);

ALTER TABLE "invoice_line_items"
  ADD COLUMN "billing_schedule_item_id" character varying(50) NULL,
  ADD CONSTRAINT "invoice_line_items_billing_schedule_items_source_line" FOREIGN KEY ("billing_schedule_item_id") REFERENCES "billing_schedule_items" ("id") ON DELETE RESTRICT NOT VALID;

-- migrate:down
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE "invoice_line_items" DROP COLUMN "billing_schedule_item_id";
DROP TABLE "billing_schedule_items";
DROP TABLE "billing_terms";
