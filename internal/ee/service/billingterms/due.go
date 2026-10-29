package billingterms

import (
	"context"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// DueBatchRequest resumes one scoped traversal at a fixed collection cutoff.
type DueBatchRequest struct {
	AsOf   time.Time `json:"as_of"`
	Cursor string    `json:"cursor,omitempty"`
	Limit  int       `json:"limit"`
}

// DueCandidate identifies a source to recover or collect after fresh validation.
type DueCandidate struct {
	ItemID string `json:"item_id"`
}

// DueBatch bounds both emitted work and rows inspected while resolving fallbacks.
type DueBatch struct {
	Items         []DueCandidate `json:"items"`
	NextCursor    string         `json:"next_cursor,omitempty"`
	Scanned       int            `json:"scanned"`
	FailedItemIDs []string       `json:"failed_item_ids,omitempty"`
}

// DueInvoiceRequest retries one source at the traversal's original cutoff.
type DueInvoiceRequest struct {
	ItemID string    `json:"item_id"`
	AsOf   time.Time `json:"as_of"`
}

// DueInvoice returns the existing or newly committed invoice; empty means deferred.
type DueInvoice struct {
	InvoiceID      string `json:"invoice_id,omitempty"`
	DeferredReason string `json:"deferred_reason,omitempty"`
}

// FallbackRequest resolves an untriggered charge at its owner's deadline.
type FallbackRequest struct {
	Owner  domain.OwnerRef
	ItemID string
	AsOf   time.Time
}

func (s *service) ResolveFallback(ctx context.Context, req FallbackRequest) error {
	if !voidSourceID(req.ItemID) || !validDate(req.AsOf) {
		return ierr.Validation("fallback requires a schedule item and finite cutoff")
	}
	return s.transaction(ctx, req.Owner, func(tx context.Context, owner *domain.OwnerSnapshot) error {
		if owner.EndDate.After(req.AsOf) {
			return nil
		}
		current, err := s.load(tx, owner, req.AsOf)
		if err != nil {
			return err
		}
		for _, item := range current.items {
			if item.ID != req.ItemID || item.Status != types.StatusPublished || item.ScheduleStatus != types.BillingScheduleScheduled || item.BillTrigger != types.BillingScheduleManual || item.BillOn != nil {
				continue
			}
			changed, err := s.items.ResolveBillOn(tx, item.ID, billingDate(owner.EndDate))
			if err != nil {
				return err
			}
			if !changed {
				return integrity("manual fallback source changed while locked")
			}
			return nil
		}
		return nil
	})
}
