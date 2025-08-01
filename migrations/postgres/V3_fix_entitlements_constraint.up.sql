-- Fix entitlements table constraint to allow null plan_id when addon_id is provided
-- This migration updates the unique constraint to handle both plan_id and addon_id

-- Drop the existing unique constraint
DROP INDEX IF EXISTS "entitlement_tenant_id_environment_id_plan_id_feature_id";

-- Create a new unique constraint that allows either plan_id or addon_id to be null
-- but ensures uniqueness for the combination
CREATE UNIQUE INDEX "entitlement_tenant_id_environment_id_plan_id_feature_id" 
ON "entitlements" ("tenant_id", "environment_id", "plan_id", "feature_id") 
WHERE "status" = 'published' AND "plan_id" IS NOT NULL;

CREATE UNIQUE INDEX "entitlement_tenant_id_environment_id_addon_id_feature_id" 
ON "entitlements" ("tenant_id", "environment_id", "addon_id", "feature_id") 
WHERE "status" = 'published' AND "addon_id" IS NOT NULL;

-- Add a check constraint to ensure either plan_id or addon_id is provided, but not both
ALTER TABLE "entitlements" 
ADD CONSTRAINT "check_entitlement_source" 
CHECK (
    ("plan_id" IS NOT NULL AND "addon_id" IS NULL) OR 
    ("plan_id" IS NULL AND "addon_id" IS NOT NULL)
); 