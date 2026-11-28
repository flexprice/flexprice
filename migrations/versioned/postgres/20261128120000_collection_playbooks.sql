-- migrate:up
-- Collection playbooks: tenant rules for chasing unpaid invoices, the runs that take an invoice through one, the
-- steps scheduled for each run, and the tasks a step creates for a person. All four tables are new, so building
-- their indexes inside this transaction blocks nothing.
SET lock_timeout = '3s';
SET statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS "collection_playbooks" (
  "id" varchar(50) NOT NULL,
  "tenant_id" varchar(50) NOT NULL,
  "status" varchar(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" varchar NULL,
  "updated_by" varchar NULL,
  "environment_id" varchar(50) NULL DEFAULT '',
  "name" varchar(255) NOT NULL,
  "description" text NULL,
  "state" varchar(20) NOT NULL DEFAULT 'draft',
  "priority" bigint NOT NULL,
  "trigger" varchar(20) NOT NULL DEFAULT 'overdue',
  "audience" jsonb NOT NULL,
  "steps" jsonb NOT NULL,
  "stops" jsonb NOT NULL,
  "min_gap_days" bigint NOT NULL DEFAULT 3,
  "skip_weekends" boolean NOT NULL DEFAULT true,
  "template_id" varchar(50) NULL,
  "version" bigint NOT NULL DEFAULT 1,
  "activated_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "collectionplaybook_tenant_id_environment_id_state_priority" ON "collection_playbooks" ("tenant_id", "environment_id", "state", "priority") WHERE ((status)::text = 'published'::text);

CREATE TABLE IF NOT EXISTS "collection_playbook_runs" (
  "id" varchar(50) NOT NULL,
  "tenant_id" varchar(50) NOT NULL,
  "status" varchar(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" varchar NULL,
  "updated_by" varchar NULL,
  "environment_id" varchar(50) NULL DEFAULT '',
  "invoice_id" varchar(50) NOT NULL,
  "customer_id" varchar(50) NOT NULL,
  "state" varchar(20) NOT NULL DEFAULT 'active',
  "stop_reason" varchar(30) NULL,
  "paused_reason" text NULL,
  "invoice_due_date" timestamptz NOT NULL,
  "next_step_at" timestamptz NULL,
  "started_at" timestamptz NOT NULL,
  "ended_at" timestamptz NULL,
  "playbook_id" varchar(50) NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "collection_playbook_runs_collection_playbooks_runs" FOREIGN KEY ("playbook_id") REFERENCES "collection_playbooks" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- An invoice is in at most one live (active or paused) run.
CREATE UNIQUE INDEX IF NOT EXISTS "idx_collection_playbook_run_live_invoice" ON "collection_playbook_runs" ("tenant_id", "environment_id", "invoice_id") WHERE (((status)::text = 'published'::text) AND ((state)::text = ANY ((ARRAY['active'::character varying, 'paused'::character varying])::text[])));
CREATE INDEX IF NOT EXISTS "idx_collection_playbook_run_playbook_state" ON "collection_playbook_runs" ("tenant_id", "environment_id", "playbook_id", "state");
CREATE INDEX IF NOT EXISTS "idx_collection_playbook_run_customer_state" ON "collection_playbook_runs" ("tenant_id", "environment_id", "customer_id", "state");
CREATE INDEX IF NOT EXISTS "collectionplaybookrun_tenant_id_environment_id_next_step_at" ON "collection_playbook_runs" ("tenant_id", "environment_id", "next_step_at") WHERE ((state)::text = 'active'::text);

CREATE TABLE IF NOT EXISTS "collection_playbook_step_runs" (
  "id" varchar(50) NOT NULL,
  "tenant_id" varchar(50) NOT NULL,
  "status" varchar(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" varchar NULL,
  "updated_by" varchar NULL,
  "environment_id" varchar(50) NULL DEFAULT '',
  "step_id" varchar(50) NOT NULL,
  "kind" varchar(20) NOT NULL,
  "due_at" timestamptz NOT NULL,
  "state" varchar(20) NOT NULL DEFAULT 'pending',
  "skip_reason" varchar(30) NULL,
  "result_ref" varchar(50) NULL,
  "attempts" bigint NOT NULL DEFAULT 0,
  "error" text NULL,
  "executed_at" timestamptz NULL,
  "run_id" varchar(50) NOT NULL,
  PRIMARY KEY ("id"),
  -- Ent's name for this foreign key: the natural one passes Postgres's 63-character limit, so Ent hashes it.
  CONSTRAINT "collection_playbook_step_runs__a6d438a91461570e1f8e8a39f1e3cf20" FOREIGN KEY ("run_id") REFERENCES "collection_playbook_runs" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- A step runs at most once per run.
CREATE UNIQUE INDEX IF NOT EXISTS "idx_collection_playbook_step_run_run_step" ON "collection_playbook_step_runs" ("run_id", "step_id");
CREATE INDEX IF NOT EXISTS "collectionplaybooksteprun_tenant_id_environment_id_state_due_at" ON "collection_playbook_step_runs" ("tenant_id", "environment_id", "state", "due_at");

CREATE TABLE IF NOT EXISTS "collection_tasks" (
  "id" varchar(50) NOT NULL,
  "tenant_id" varchar(50) NOT NULL,
  "status" varchar(20) NOT NULL DEFAULT 'published',
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  "created_by" varchar NULL,
  "updated_by" varchar NULL,
  "environment_id" varchar(50) NULL DEFAULT '',
  "invoice_id" varchar(50) NOT NULL,
  "customer_id" varchar(50) NOT NULL,
  "run_id" varchar(50) NULL,
  "step_id" varchar(50) NULL,
  "assignee_user_id" varchar(50) NULL,
  "assignee_email" varchar(255) NULL,
  "note" text NOT NULL,
  "state" varchar(20) NOT NULL DEFAULT 'open',
  "done_at" timestamptz NULL,
  "done_by" varchar(50) NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "collectiontask_tenant_id_environment_id_invoice_id" ON "collection_tasks" ("tenant_id", "environment_id", "invoice_id");
CREATE INDEX IF NOT EXISTS "collectiontask_tenant_id_environment_id_state_created_at" ON "collection_tasks" ("tenant_id", "environment_id", "state", "created_at");
-- A step creates at most one task per run, however often it is retried.
CREATE UNIQUE INDEX IF NOT EXISTS "idx_collection_task_run_step" ON "collection_tasks" ("run_id", "step_id") WHERE (run_id IS NOT NULL);

-- migrate:down
SET lock_timeout = '3s';
SET statement_timeout = '30s';

DROP TABLE IF EXISTS "collection_tasks";
DROP TABLE IF EXISTS "collection_playbook_step_runs";
DROP TABLE IF EXISTS "collection_playbook_runs";
DROP TABLE IF EXISTS "collection_playbooks";
