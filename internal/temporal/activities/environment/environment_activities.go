package activities

import (
	"context"
	"fmt"

	domainCreditGrant "github.com/flexprice/flexprice/internal/domain/creditgrant"
	domainEntitlement "github.com/flexprice/flexprice/internal/domain/entitlement"
	domainFeature "github.com/flexprice/flexprice/internal/domain/feature"
	domainGroup "github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/meter"
	domainPlan "github.com/flexprice/flexprice/internal/domain/plan"
	domainPrice "github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const ActivityPrefix = "EnvironmentActivities"

// CloneFeaturesResult is the Temporal activity return type for CloneEnvironmentFeatures.
// Errors are lifted to EnvironmentCloneResult.Errors by the workflow.
type CloneFeaturesResult struct {
	temporalmodels.CloneEnvironmentFeaturesOutput
	Errors []temporalmodels.CloneError `json:"errors,omitempty"`
}

// ClonePlansResult is the Temporal activity return type for CloneEnvironmentPlans.
// Errors are lifted to EnvironmentCloneResult.Errors by the workflow.
type ClonePlansResult struct {
	temporalmodels.CloneEnvironmentPlansOutput
	Errors []temporalmodels.CloneError `json:"errors,omitempty"`
}

// EnvironmentActivities contains all environment-clone-related activities.
type EnvironmentActivities struct {
	params service.ServiceParams
}

// NewEnvironmentActivities creates a new EnvironmentActivities instance.
func NewEnvironmentActivities(params service.ServiceParams) *EnvironmentActivities {
	return &EnvironmentActivities{
		params: params,
	}
}

// CloneEnvironmentFeatures clones all published features (and their meters) from source
// to target environment. Features whose lookup_key already exists in the target are
// skipped (idempotent). Each feature is cloned in its own DB transaction.
func (a *EnvironmentActivities) CloneEnvironmentFeatures(ctx context.Context, input temporalmodels.CloneEnvironmentInput) (*CloneFeaturesResult, error) {
	log := logger.GetLogger()

	if input.SourceEnvironmentID == "" || input.TargetEnvironmentID == "" {
		return nil, ierr.NewError("source and target environment IDs are required").
			WithHint("Source and target environment IDs are required").
			Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	sourceCtx := types.SetTenantID(ctx, input.TenantID)
	sourceCtx = types.SetEnvironmentID(sourceCtx, input.SourceEnvironmentID)
	sourceCtx = types.SetUserID(sourceCtx, input.UserID)

	targetCtx := types.SetTenantID(ctx, input.TenantID)
	targetCtx = types.SetEnvironmentID(targetCtx, input.TargetEnvironmentID)
	targetCtx = types.SetUserID(targetCtx, input.UserID)

	// Fetch all published features from source
	filter := types.NewNoLimitFeatureFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	sourceFeatures, err := a.params.FeatureRepo.List(sourceCtx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list features from source environment").
			Mark(ierr.ErrDatabase)
	}

	// Build index of existing target features by lookup_key for idempotency check
	targetFeatures, err := a.params.FeatureRepo.List(targetCtx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list features from target environment").
			Mark(ierr.ErrDatabase)
	}
	existingByKey := make(map[string]bool, len(targetFeatures))
	for _, f := range targetFeatures {
		if f.LookupKey != "" {
			existingByKey[f.LookupKey] = true
		}
	}

	result := &CloneFeaturesResult{}

	for _, srcFeat := range sourceFeatures {
		// Idempotency: skip if feature with same lookup_key already exists in target
		if srcFeat.LookupKey != "" && existingByKey[srcFeat.LookupKey] {
			log.Infow("env_clone_feature_skipped_existing",
				"feature_id", srcFeat.ID, "lookup_key", srcFeat.LookupKey,
				"source_env", input.SourceEnvironmentID, "target_env", input.TargetEnvironmentID)
			result.FeaturesSkipped++
			result.SkippedFeatureIDs = append(result.SkippedFeatureIDs, srcFeat.ID)
			continue
		}

		newFeatureID, err := a.cloneFeat(sourceCtx, targetCtx, srcFeat, input.TargetEnvironmentID)
		if err != nil {
			log.Warnw("env_clone_feature_failed", "feature_id", srcFeat.ID, "name", srcFeat.Name, "error", err)
			result.Errors = append(result.Errors, temporalmodels.CloneError{
				EntityType: "feature",
				EntityID:   srcFeat.ID,
				EntityName: srcFeat.Name,
				Reason:     err.Error(),
			})
			continue
		}

		result.FeaturesCloned++
		result.FeatureIDs = append(result.FeatureIDs, newFeatureID)
	}

	log.Infow("env_clone_features_completed",
		"source_env", input.SourceEnvironmentID,
		"target_env", input.TargetEnvironmentID,
		"features_cloned", result.FeaturesCloned,
		"features_skipped", result.FeaturesSkipped,
		"errors", len(result.Errors),
	)

	return result, nil
}

// cloneFeat clones one feature (and its meter if metered) into the target env
// inside a single DB transaction.
func (a *EnvironmentActivities) cloneFeat(
	sourceCtx, targetCtx context.Context,
	srcFeat *domainFeature.Feature,
	targetEnvID string,
) (string, error) {
	metadata := make(types.Metadata, len(srcFeat.Metadata)+2)
	for k, v := range srcFeat.Metadata {
		metadata[k] = v
	}
	metadata["source_environment_feature_id"] = srcFeat.ID
	metadata["source_environment_id"] = srcFeat.EnvironmentID

	newFeature := &domainFeature.Feature{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FEATURE),
		Name:          srcFeat.Name,
		LookupKey:     srcFeat.LookupKey,
		Description:   srcFeat.Description,
		Type:          srcFeat.Type,
		MeterID:       srcFeat.MeterID, // will be overwritten for metered features
		Metadata:      metadata,
		UnitSingular:  srcFeat.UnitSingular,
		UnitPlural:    srcFeat.UnitPlural,
		ReportingUnit: srcFeat.ReportingUnit,
		AlertSettings: srcFeat.AlertSettings,
		EnvironmentID: targetEnvID,
		BaseModel:     types.GetDefaultBaseModel(targetCtx),
	}

	err := a.params.DB.WithTx(targetCtx, func(txCtx context.Context) error {
		// Resolve group in target env
		if srcFeat.GroupID != "" {
			resolvedID, err := a.resolveOrCreateGroup(sourceCtx, txCtx, srcFeat.GroupID)
			if err != nil {
				if ierr.IsNotFound(err) {
					// Source group no longer exists — drop the association gracefully
					newFeature.GroupID = ""
				} else {
					return fmt.Errorf("failed to resolve group %s: %w", srcFeat.GroupID, err)
				}
			} else {
				newFeature.GroupID = resolvedID
			}
		}

		// Clone meter for metered features
		if srcFeat.Type == types.FeatureTypeMetered && srcFeat.MeterID != "" {
			sourceMeter, err := a.params.MeterRepo.GetMeter(sourceCtx, srcFeat.MeterID)
			if err != nil {
				return fmt.Errorf("failed to get source meter %s: %w", srcFeat.MeterID, err)
			}

			// Deep-copy filters to avoid aliasing
			filtersCopy := make([]meter.Filter, len(sourceMeter.Filters))
			copy(filtersCopy, sourceMeter.Filters)

			newMeter := &meter.Meter{
				ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
				Name:          sourceMeter.Name,
				EventName:     sourceMeter.EventName,
				Aggregation:   sourceMeter.Aggregation,
				Filters:       filtersCopy,
				ResetUsage:    sourceMeter.ResetUsage,
				EnvironmentID: targetEnvID,
				BaseModel:     types.GetDefaultBaseModel(txCtx),
			}
			newMeter.Status = types.StatusPublished
			if err := a.params.MeterRepo.CreateMeter(txCtx, newMeter); err != nil {
				return fmt.Errorf("failed to create meter in target env: %w", err)
			}
			newFeature.MeterID = newMeter.ID
		}

		return a.params.FeatureRepo.Create(txCtx, newFeature)
	})
	if err != nil {
		return "", err
	}
	return newFeature.ID, nil
}

// Plan cloning activity

// CloneEnvironmentPlans clones all published plans (with prices, entitlements, grants)
// from source to target environment. Plans whose lookup_key already exists in the target
// are skipped. Each plan is cloned in its own DB transaction. If any price or entitlement
// references a feature/meter that doesn't exist in the target env, that entire plan is failed.
func (a *EnvironmentActivities) CloneEnvironmentPlans(ctx context.Context, input temporalmodels.CloneEnvironmentInput) (*ClonePlansResult, error) {
	log := logger.GetLogger()

	if input.SourceEnvironmentID == "" || input.TargetEnvironmentID == "" {
		return nil, ierr.NewError("source and target environment IDs are required").
			WithHint("Source and target environment IDs are required").
			Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	sourceCtx := types.SetTenantID(ctx, input.TenantID)
	sourceCtx = types.SetEnvironmentID(sourceCtx, input.SourceEnvironmentID)
	sourceCtx = types.SetUserID(sourceCtx, input.UserID)

	targetCtx := types.SetTenantID(ctx, input.TenantID)
	targetCtx = types.SetEnvironmentID(targetCtx, input.TargetEnvironmentID)
	targetCtx = types.SetUserID(targetCtx, input.UserID)

	// Build feature/meter ID maps: source → target by lookup_key
	featureIDMap, meterIDMap, err := a.buildCrossEnvIDMaps(sourceCtx, targetCtx)
	if err != nil {
		return nil, err
	}

	// Build group ID map for price groups
	groupIDMap := make(map[string]string)

	// Fetch all published plans from source
	planFilter := types.NewNoLimitPlanFilter()
	planFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	sourcePlans, err := a.params.PlanRepo.List(sourceCtx, planFilter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list plans from source environment").
			Mark(ierr.ErrDatabase)
	}

	// Build index of existing target plans by lookup_key for idempotency
	targetPlans, err := a.params.PlanRepo.List(targetCtx, planFilter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list plans from target environment").
			Mark(ierr.ErrDatabase)
	}
	existingByKey := make(map[string]bool, len(targetPlans))
	for _, p := range targetPlans {
		if p.LookupKey != "" {
			existingByKey[p.LookupKey] = true
		}
	}

	result := &ClonePlansResult{}

	for _, srcPlan := range sourcePlans {
		// Idempotency: skip if plan with same lookup_key already exists in target
		if srcPlan.LookupKey != "" && existingByKey[srcPlan.LookupKey] {
			log.Infow("env_clone_plan_skipped_existing",
				"plan_id", srcPlan.ID, "lookup_key", srcPlan.LookupKey,
				"source_env", input.SourceEnvironmentID, "target_env", input.TargetEnvironmentID)
			result.PlansSkipped++
			result.SkippedPlanIDs = append(result.SkippedPlanIDs, srcPlan.ID)
			continue
		}

		newPlanID, err := a.clonePlan(sourceCtx, targetCtx, srcPlan, input.TargetEnvironmentID,
			featureIDMap, meterIDMap, groupIDMap)
		if err != nil {
			log.Warnw("env_clone_plan_failed",
				"plan_id", srcPlan.ID, "name", srcPlan.Name, "error", err)
			result.Errors = append(result.Errors, temporalmodels.CloneError{
				EntityType: "plan",
				EntityID:   srcPlan.ID,
				EntityName: srcPlan.Name,
				Reason:     err.Error(),
			})
			continue
		}

		result.PlansCloned++
		result.PlanIDs = append(result.PlanIDs, newPlanID)
	}

	log.Infow("env_clone_plans_completed",
		"source_env", input.SourceEnvironmentID,
		"target_env", input.TargetEnvironmentID,
		"plans_cloned", result.PlansCloned,
		"plans_skipped", result.PlansSkipped,
		"errors", len(result.Errors),
	)

	return result, nil
}

// cloneSinglePlan clones one plan (with prices, entitlements, grants) into the target env
// inside a single DB transaction. Returns error if any price/entitlement references a
// feature/meter that doesn't exist in the target environment.
func (a *EnvironmentActivities) clonePlan(
	sourceCtx, targetCtx context.Context,
	srcPlan *domainPlan.Plan,
	targetEnvID string,
	featureIDMap, meterIDMap map[string]string,
	groupIDMap map[string]string,
) (string, error) {
	// Fetch source plan's prices, entitlements, credit grants
	sourcePrices, err := a.params.PriceRepo.List(sourceCtx, types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{srcPlan.ID}).
		WithEntityType(types.PRICE_ENTITY_TYPE_PLAN).
		WithStatus(types.StatusPublished))
	if err != nil {
		return "", fmt.Errorf("failed to fetch prices: %w", err)
	}

	sourceEntitlements, err := a.params.EntitlementRepo.List(sourceCtx, types.NewNoLimitEntitlementFilter().
		WithPlanIDs([]string{srcPlan.ID}).
		WithStatus(types.StatusPublished))
	if err != nil {
		return "", fmt.Errorf("failed to fetch entitlements: %w", err)
	}

	sourceGrants, err := a.params.CreditGrantRepo.List(sourceCtx, types.NewNoLimitCreditGrantFilter().
		WithPlanIDs([]string{srcPlan.ID}).
		WithStatus(types.StatusPublished).
		WithScope(types.CreditGrantScopePlan))
	if err != nil {
		return "", fmt.Errorf("failed to fetch credit grants: %w", err)
	}

	// Build new plan
	newPlanID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN)
	metadata := make(types.Metadata, len(srcPlan.Metadata)+1)
	for k, v := range srcPlan.Metadata {
		metadata[k] = v
	}
	metadata["source_environment_plan_id"] = srcPlan.ID
	metadata["source_environment_id"] = srcPlan.EnvironmentID

	entityTypePlan := types.PRICE_ENTITY_TYPE_PLAN
	entEntityTypePlan := types.ENTITLEMENT_ENTITY_TYPE_PLAN
	scopePlan := types.CreditGrantScopePlan
	emptyLookupKey := ""

	// Execute in single transaction — all building (including group resolution) happens
	// inside so that group inserts are rolled back if any subsequent write fails.
	const batchSize = 100
	err = a.params.DB.WithTx(targetCtx, func(txCtx context.Context) error {
		// Build prices with remapping
		newPrices := make([]*domainPrice.Price, 0, len(sourcePrices))
		for _, p := range sourcePrices {
			overrides := &domainPrice.PriceCloneOverrides{
				ID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)),
				EntityType: &entityTypePlan,
				EntityID:   &newPlanID,
				LookupKey:  lo.ToPtr(emptyLookupKey),
			}

			if p.MeterID != "" {
				newMID, ok := meterIDMap[p.MeterID]
				if !ok || newMID == "" {
					return fmt.Errorf("price %s references meter %s which has no mapping in target env — feature may not have been cloned", p.ID, p.MeterID)
				}
				overrides.MeterID = &newMID
			}

			if p.GroupID != "" {
				if newGID, ok := groupIDMap[p.GroupID]; ok {
					overrides.GroupID = lo.ToPtr(newGID)
				} else {
					resolved, err := a.resolveOrCreateGroup(sourceCtx, txCtx, p.GroupID)
					if err != nil {
						overrides.GroupID = lo.ToPtr("") // group unreachable — clear
					} else {
						groupIDMap[p.GroupID] = resolved
						overrides.GroupID = lo.ToPtr(resolved)
					}
				}
			}

			newPrices = append(newPrices, p.CopyWith(txCtx, overrides))
		}

		// Build entitlements with remapping — fail fast if feature mapping is missing
		newEntitlements := make([]*domainEntitlement.Entitlement, 0, len(sourceEntitlements))
		for _, e := range sourceEntitlements {
			entOverrides := &domainEntitlement.EntitlementCloneOverrides{
				ID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT)),
				EntityType: &entEntityTypePlan,
				EntityID:   &newPlanID,
			}
			if e.FeatureID != "" {
				newFID, ok := featureIDMap[e.FeatureID]
				if !ok || newFID == "" {
					return fmt.Errorf("entitlement %s references feature %s which has no mapping in target env — feature may not have been cloned", e.ID, e.FeatureID)
				}
				entOverrides.FeatureID = &newFID
			}
			newEntitlements = append(newEntitlements, e.CopyWith(txCtx, entOverrides))
		}

		// Build grants
		newGrants := make([]*domainCreditGrant.CreditGrant, 0, len(sourceGrants))
		for _, cg := range sourceGrants {
			newGrants = append(newGrants, cg.CopyWith(txCtx, &domainCreditGrant.CreditGrantCloneOverrides{
				ID:     lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT)),
				Scope:  &scopePlan,
				PlanID: &newPlanID,
			}))
		}

		newPlan := &domainPlan.Plan{
			ID:            newPlanID,
			Name:          srcPlan.Name,
			LookupKey:     srcPlan.LookupKey,
			Description:   srcPlan.Description,
			EnvironmentID: targetEnvID,
			Metadata:      metadata,
			DisplayOrder:  srcPlan.DisplayOrder,
			BaseModel:     types.GetDefaultBaseModel(txCtx),
		}
		if err := a.params.PlanRepo.Create(txCtx, newPlan); err != nil {
			return fmt.Errorf("failed to create plan: %w", err)
		}

		for _, batch := range lo.Chunk(newPrices, batchSize) {
			if err := a.params.PriceRepo.CreateBulk(txCtx, batch); err != nil {
				return fmt.Errorf("failed to create prices: %w", err)
			}
		}
		for _, batch := range lo.Chunk(newEntitlements, batchSize) {
			if _, err := a.params.EntitlementRepo.CreateBulk(txCtx, batch); err != nil {
				return fmt.Errorf("failed to create entitlements: %w", err)
			}
		}
		for _, batch := range lo.Chunk(newGrants, batchSize) {
			if _, err := a.params.CreditGrantRepo.CreateBulk(txCtx, batch); err != nil {
				return fmt.Errorf("failed to create credit grants: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return newPlanID, nil
}

// buildCrossEnvIDMaps resolves feature and meter ID translations between two environments.
// lookup_key is the stable identifier — fetches published features from both envs, joins by lookup_key.
func (a *EnvironmentActivities) buildCrossEnvIDMaps(
	sourceCtx, targetCtx context.Context,
) (featureIDMap, meterIDMap map[string]string, err error) {
	featureIDMap = make(map[string]string)
	meterIDMap = make(map[string]string)

	publishedFilter := types.NewNoLimitFeatureFilter()
	publishedFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)

	sourceFeatures, err := a.params.FeatureRepo.List(sourceCtx, publishedFilter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch source features: %w", err)
	}

	targetFeatures, err := a.params.FeatureRepo.List(targetCtx, publishedFilter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch target features: %w", err)
	}

	tgtByKey := make(map[string]*domainFeature.Feature, len(targetFeatures))
	for _, f := range targetFeatures {
		if f.LookupKey != "" {
			tgtByKey[f.LookupKey] = f
		}
	}

	for _, src := range sourceFeatures {
		if src.LookupKey == "" {
			continue
		}
		tgt, ok := tgtByKey[src.LookupKey]
		if !ok {
			continue
		}
		featureIDMap[src.ID] = tgt.ID
		if src.MeterID != "" && tgt.MeterID != "" {
			meterIDMap[src.MeterID] = tgt.MeterID
		}
	}

	return featureIDMap, meterIDMap, nil
}

// resolveOrCreateGroup finds or creates a matching group in the target environment by lookup_key.
func (a *EnvironmentActivities) resolveOrCreateGroup(sourceCtx, targetCtx context.Context, sourceGroupID string) (string, error) {
	sourceGroup, err := a.params.GroupRepo.Get(sourceCtx, sourceGroupID)
	if err != nil {
		return "", err
	}

	existingGroup, err := a.params.GroupRepo.GetByLookupKey(targetCtx, sourceGroup.LookupKey)
	if err == nil && existingGroup != nil {
		return existingGroup.ID, nil
	}
	if err != nil && !ierr.IsNotFound(err) {
		return "", err
	}

	newGroup := &domainGroup.Group{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_GROUP),
		Name:          sourceGroup.Name,
		EntityType:    sourceGroup.EntityType,
		EnvironmentID: types.GetEnvironmentID(targetCtx),
		LookupKey:     sourceGroup.LookupKey,
		Metadata:      sourceGroup.Metadata,
		BaseModel:     types.GetDefaultBaseModel(targetCtx),
	}
	if err := a.params.GroupRepo.Create(targetCtx, newGroup); err != nil {
		return "", err
	}
	return newGroup.ID, nil
}
