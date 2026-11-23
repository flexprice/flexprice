-- migrate:up
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE "invoice_reminders" ADD COLUMN "metadata" jsonb NULL;
UPDATE "invoice_reminders"
SET "metadata" = jsonb_build_object('original_tone', "original_tone", 'original_subject', "original_subject", 'original_body', "original_body");
ALTER TABLE "invoice_reminders" DROP COLUMN "original_tone", DROP COLUMN "original_subject", DROP COLUMN "original_body";
ALTER TABLE "invoice_reminders" RENAME COLUMN "mail_tone" TO "tone";
ALTER TABLE "invoice_reminders" RENAME COLUMN "mail_subject" TO "subject";
ALTER TABLE "invoice_reminders" RENAME COLUMN "mail_body" TO "body";

-- migrate:down
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE "invoice_reminders" RENAME COLUMN "body" TO "mail_body";
ALTER TABLE "invoice_reminders" RENAME COLUMN "subject" TO "mail_subject";
ALTER TABLE "invoice_reminders" RENAME COLUMN "tone" TO "mail_tone";
ALTER TABLE "invoice_reminders" ADD COLUMN "original_tone" varchar(20) NOT NULL DEFAULT '';
ALTER TABLE "invoice_reminders" ADD COLUMN "original_subject" text NOT NULL DEFAULT '';
ALTER TABLE "invoice_reminders" ADD COLUMN "original_body" text NOT NULL DEFAULT '';
UPDATE "invoice_reminders"
SET "original_tone" = coalesce("metadata"->>'original_tone', "mail_tone"),
    "original_subject" = coalesce("metadata"->>'original_subject', "mail_subject"),
    "original_body" = coalesce("metadata"->>'original_body', "mail_body");
ALTER TABLE "invoice_reminders" ALTER COLUMN "original_tone" DROP DEFAULT, ALTER COLUMN "original_subject" DROP DEFAULT, ALTER COLUMN "original_body" DROP DEFAULT;
ALTER TABLE "invoice_reminders" DROP COLUMN "metadata";
