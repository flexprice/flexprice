package service

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

// addonAttachParams is a fully-resolved addon attach that has not been written yet, so it can be
// priced before anyone decides whether to persist it. createAddonAttachParams builds it,
// calculateAddonProration prices it, persistAddonAttach writes it.
type addonAttachParams struct {
	subscription   *subscription.Subscription
	request        *dto.AddAddonToSubscriptionRequest
	association    *addonassociation.AddonAssociation
	lineItems      []*subscription.SubscriptionLineItem
	bucketCfgs     map[string]*dto.LineItemCommitmentConfig
	priceMap       map[string]*dto.PriceResponse
	requestedStart time.Time
	effectiveDate  time.Time

	// isReplay marks a plan whose association is an existing pending row being activated
	// rather than a new one being created.
	isReplay bool
}

func (p *addonAttachParams) getSubscription() *subscription.Subscription {
	if p == nil {
		return nil
	}
	return p.subscription
}

func (p *addonAttachParams) getRequest() *dto.AddAddonToSubscriptionRequest {
	if p == nil {
		return nil
	}
	return p.request
}

func (p *addonAttachParams) getAssociation() *addonassociation.AddonAssociation {
	if p == nil {
		return nil
	}
	return p.association
}

func (p *addonAttachParams) getLineItems() []*subscription.SubscriptionLineItem {
	if p == nil {
		return nil
	}
	return p.lineItems
}

func (p *addonAttachParams) getRequestedStart() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.requestedStart
}

func (p *addonAttachParams) getEffectiveDate() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.effectiveDate
}

// getBucketCfgs is keyed by LINE ITEM ID, matching applyLineItemCommitmentFromMap's contract.
func (p *addonAttachParams) getBucketCfgs() map[string]*dto.LineItemCommitmentConfig {
	if p == nil {
		return nil
	}
	return p.bucketCfgs
}

func (p *addonAttachParams) getPriceMap() map[string]*dto.PriceResponse {
	if p == nil {
		return nil
	}
	return p.priceMap
}

func (p *addonAttachParams) isReplayAttach() bool {
	if p == nil {
		return false
	}
	return p.isReplay
}

func (p *addonAttachParams) prorationIdempotencyKey() string {
	association := p.getAssociation()
	if association == nil {
		return ""
	}
	return fmt.Sprintf("addon_add_%s_%d", association.ID, p.getEffectiveDate().Unix())
}

// addonDetachParams is a fully-resolved addon removal that has not been written yet, so it can
// be priced before anyone decides whether to persist it. createAddonDetachParams builds it,
// calculateAddonDetachProration prices it, persistAddonDetach writes it.
type addonDetachParams struct {
	subscription  *subscription.Subscription
	association   *addonassociation.AddonAssociation
	lineItems     []*subscription.SubscriptionLineItem
	effectiveDate time.Time
	behavior      types.ProrationBehavior
	reason        string
}

func (p *addonDetachParams) getSubscription() *subscription.Subscription {
	if p == nil {
		return nil
	}
	return p.subscription
}

func (p *addonDetachParams) getAssociation() *addonassociation.AddonAssociation {
	if p == nil {
		return nil
	}
	return p.association
}

func (p *addonDetachParams) getLineItems() []*subscription.SubscriptionLineItem {
	if p == nil {
		return nil
	}
	return p.lineItems
}

func (p *addonDetachParams) getEffectiveDate() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.effectiveDate
}

func (p *addonDetachParams) getBehavior() types.ProrationBehavior {
	if p == nil {
		return ""
	}
	return p.behavior
}

func (p *addonDetachParams) getReason() string {
	if p == nil {
		return ""
	}
	return p.reason
}

func (p *addonDetachParams) prorationIdempotencyKey() string {
	association := p.getAssociation()
	if association == nil {
		return ""
	}

	return fmt.Sprintf("addon_remove_%s_%s_%d",
		association.EntityID, association.ID, p.getEffectiveDate().Unix())
}
