package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// featureHolder is one config measuring a feature, and when the change takes it away.
type featureHolder struct {
	reset types.EntitlementUsageResetPeriod
	// departsAt is set only for a config this change removes; until then it still measures.
	departsAt *time.Time
}

func (s *subscriptionGrantService) validateEntitlementCompatibility(
	ctx context.Context,
	req GrantChangeRequest,
) error {
	incomingByAddon, err := s.getMeteredFeaturesEntitlementsByAddon(ctx, addonIDsOf(req.Incoming))
	if err != nil {
		return err
	}
	if len(incomingByAddon) == 0 {
		return nil
	}

	if err := s.validateResetPeriods(ctx, req, incomingByAddon); err != nil {
		return err
	}

	return validateGrantDates(req, incomingByAddon)
}

// A metered feature measures usage over one reset period, so every config feeding it has to
// agree on which.
func (s *subscriptionGrantService) validateResetPeriods(
	ctx context.Context,
	req GrantChangeRequest,
	incomingByAddon map[string][]*dto.EntitlementResponse,
) error {
	featuredEntitlmentHoldersMap, err := s.meteredFeatureHolders(ctx, req)
	if err != nil {
		return err
	}

	// Folded one entry at a time into the accumulating map, so two incoming configs that
	// disagree with each other are caught as well as one disagreeing with a survivor.
	for _, grantSource := range req.Incoming {
		for _, incomingEntitlement := range incomingByAddon[grantSource.AddonID] {
			if incomingEntitlement.FeatureType != types.FeatureTypeMetered {
				continue
			}

			for _, featureEntitlement := range featuredEntitlmentHoldersMap[incomingEntitlement.FeatureID] {
				if featureEntitlement.reset == incomingEntitlement.UsageResetPeriod {
					continue
				}

				// A config already gone by the time this entry lands cannot conflict with it.
				if featureEntitlement.departsAt != nil && !featureEntitlement.departsAt.After(grantSource.EffectiveDate) {
					continue
				}

				return ierr.NewError("metered feature usage reset period conflict").
					WithHintf("Feature %s is already measured over %s, but this addon measures it over %s",
						incomingEntitlement.FeatureID, featureEntitlement.reset, incomingEntitlement.UsageResetPeriod).
					WithReportableDetails(map[string]any{
						"subscription_id": req.Sub.ID,
						"addon_id":        grantSource.AddonID,
						"feature_id":      incomingEntitlement.FeatureID,
					}).
					Mark(ierr.ErrValidation)
			}

			featuredEntitlmentHoldersMap[incomingEntitlement.FeatureID] = append(featuredEntitlmentHoldersMap[incomingEntitlement.FeatureID],
				featureHolder{reset: incomingEntitlement.UsageResetPeriod})
		}
	}

	return nil
}

// grants for same feature must have the same date for now because proration is not supported yet for multiple
// different grant dates.
func validateGrantDates(
	req GrantChangeRequest,
	incomingByAddon map[string][]*dto.EntitlementResponse,
) error {
	dateByFeature := make(map[string]time.Time)
	addonByFeature := make(map[string]string)

	for _, grantSource := range req.Incoming {
		if grantSource.ChangeType == types.ScheduleTypePeriodEnd ||
			!grantSource.requestedDate().Before(req.Sub.CurrentPeriodEnd) {
			continue
		}

		for _, incomingEntitlement := range incomingByAddon[grantSource.AddonID] {
			if incomingEntitlement.Entitlement == nil || !incomingEntitlement.Entitlement.HasGrantConfig() {
				continue
			}

			at, seen := dateByFeature[incomingEntitlement.FeatureID]
			if !seen {
				dateByFeature[incomingEntitlement.FeatureID] = grantSource.requestedDate()
				addonByFeature[incomingEntitlement.FeatureID] = grantSource.AddonID
				continue
			}
			if at.Equal(grantSource.requestedDate()) {
				continue
			}

			return ierr.NewError("addons granting the same feature must share a grant date").
				WithHint("Send them as separate changes, or give both the same date").
				WithReportableDetails(map[string]any{
					"subscription_id": req.Sub.ID,
					"feature_id":      incomingEntitlement.FeatureID,
					"addon_ids":       []string{addonByFeature[incomingEntitlement.FeatureID], grantSource.AddonID},
					"dates":           []string{at.String(), grantSource.requestedDate().String()},
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

func (s *subscriptionGrantService) meteredFeatureHolders(
	ctx context.Context,
	req GrantChangeRequest,
) (map[string][]featureHolder, error) {
	subSvc := &subscriptionService{ServiceParams: s.ServiceParams}

	current, err := subSvc.GetSubscriptionEntitlementsForSubscription(ctx, req.Sub)
	if err != nil {
		return nil, err
	}

	addonRemovalDatesMap := make(map[string]time.Time, len(req.Removed))
	for _, src := range req.Removed {
		if src.AddonID == "" {
			continue
		}
		if at, seen := addonRemovalDatesMap[src.AddonID]; !seen || src.EffectiveDate.Before(at) {
			addonRemovalDatesMap[src.AddonID] = src.EffectiveDate
		}
	}

	featuredEntitlmentHolderMap := make(map[string][]featureHolder)
	for _, ent := range current {
		if ent == nil || ent.Entitlement == nil || ent.FeatureType != types.FeatureTypeMetered {
			continue
		}

		held := featureHolder{reset: ent.UsageResetPeriod}
		if ent.EntityType == types.ENTITLEMENT_ENTITY_TYPE_ADDON {
			if at, leaving := addonRemovalDatesMap[ent.EntityID]; leaving {
				held.departsAt = lo.ToPtr(at)
			}
		}
		featuredEntitlmentHolderMap[ent.FeatureID] = append(featuredEntitlmentHolderMap[ent.FeatureID], held)
	}

	// Pending addons associations because of existing checkouts
	arriving := lo.SliceToMap(addonIDsOf(req.Incoming), func(id string) (string, bool) { return id, true })
	pending, err := subSvc.listPendingAddonAssociations(ctx, req.Sub.ID)
	if err != nil {
		return nil, err
	}

	pendingIDs := lo.FilterMap(pending, func(a *addonassociation.AddonAssociation, _ int) (string, bool) {
		return a.AddonID, a != nil && !arriving[a.AddonID]
	})
	pendingByAddon, err := s.getMeteredFeaturesEntitlementsByAddon(ctx, lo.Uniq(pendingIDs))
	if err != nil {
		return nil, err
	}

	for _, ents := range pendingByAddon {
		for _, ent := range ents {
			if ent.FeatureType != types.FeatureTypeMetered {
				continue
			}
			featuredEntitlmentHolderMap[ent.FeatureID] = append(featuredEntitlmentHolderMap[ent.FeatureID],
				featureHolder{reset: ent.UsageResetPeriod})
		}
	}

	return featuredEntitlmentHolderMap, nil
}

// entitlementsByAddon reads each addon once and keeps what either check needs: metered
// entitlements for the reset period, grant-bearing ones for the pooling date.
func (s *subscriptionGrantService) getMeteredFeaturesEntitlementsByAddon(
	ctx context.Context,
	addonIDs []string,
) (map[string][]*dto.EntitlementResponse, error) {
	entSvc := NewEntitlementService(s.ServiceParams)
	byAddon := make(map[string][]*dto.EntitlementResponse, len(addonIDs))

	for _, addonID := range lo.Uniq(addonIDs) {
		if _, read := byAddon[addonID]; read {
			continue
		}

		ents, err := entSvc.GetAddonEntitlements(ctx, addonID)
		if err != nil {
			return nil, err
		}

		relevant := lo.Filter(ents.Items, func(e *dto.EntitlementResponse, _ int) bool {
			return e != nil && e.Entitlement != nil &&
				(e.FeatureType == types.FeatureTypeMetered || e.Entitlement.HasGrantConfig())
		})
		if len(relevant) > 0 {
			byAddon[addonID] = relevant
		}
	}

	return byAddon, nil
}

func addonIDsOf(sources []GrantSource) []string {
	return lo.Uniq(lo.Compact(lo.Map(sources, func(src GrantSource, _ int) string { return src.AddonID })))
}
