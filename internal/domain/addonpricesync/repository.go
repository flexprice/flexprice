package addonpricesync

import (
	"context"
	"time"
)

// AddonAssociationRef identifies one addon association and the subscription it
// belongs to. EndDate is the association's end date when it is cancelled at period
// end (still billing until then), nil otherwise; created line items are capped to it.
type AddonAssociationRef struct {
	AssociationID  string
	SubscriptionID string
	EndDate        *time.Time
}

// ListBehindAssociationsParams drives discovery of associations stale relative to TargetSeq.
type ListBehindAssociationsParams struct {
	AddonID   string
	TargetSeq int64
	Limit     int
}

// TerminateEndedAddonLineItemsParams drives the termination UPDATE for a page of associations.
type TerminateEndedAddonLineItemsParams struct {
	AddonID        string
	AssociationIDs []string
}

// StampAssociationsParams forwards a page of associations to TargetSeq.
type StampAssociationsParams struct {
	TargetSeq      int64
	AssociationIDs []string
}

// Repository defines the DB-driven queries for the addon usage-price sync.
//
// Mirrors planpricesync but keyed on the addon association (subscriptions hold
// many addons, so the sync watermark lives on addon_associations, not the
// subscription). All queries are usage-only and idempotent.
type Repository interface {
	// CurrentAddonSequence returns max(prices.sequence) for the addon's published
	// usage prices. This is the target sequence associations are stamped to after
	// a successful pass. Returns 0 if the addon has no qualifying prices.
	CurrentAddonSequence(ctx context.Context, addonID string) (int64, error)

	// ListBehindAssociations returns still-billing associations of the addon whose
	// synced_price_sequence is below TargetSeq, on active/trialing subscriptions.
	// Still-billing means active, or cancelled with a future end date (cancel at
	// period end). Paging advances via stamping — stamped associations fall out of
	// the synced_price_sequence < TargetSeq filter on the next call.
	ListBehindAssociations(ctx context.Context, p ListBehindAssociationsParams) ([]AddonAssociationRef, error)

	// TerminateEndedAddonLineItems caps addon usage line items belonging to the given
	// associations to their catalog price's end date: it shortens any line whose end
	// date is null or later than the ended price. Idempotent. Returns rows affected.
	TerminateEndedAddonLineItems(ctx context.Context, p TerminateEndedAddonLineItemsParams) (int, error)

	// StampAssociations sets synced_price_sequence on the given associations.
	// Forward-only (GREATEST) so a concurrent newer stamp is not overwritten.
	StampAssociations(ctx context.Context, p StampAssociationsParams) (int, error)

	// CountUnsyncedAssociations returns how many of the addon's still-billing
	// associations are behind targetSeq. Mirrors the eligibility filter used by
	// ListBehindAssociations, without paging.
	CountUnsyncedAssociations(ctx context.Context, addonID string, targetSeq int64) (int64, error)
}
