package billingterms

import (
	"context"
	"slices"
	"strings"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// SettlementRequest dates the shortfall authorized by the owner's frozen collection target.
// Terminal settlement may collect after the original service term ends.
type SettlementRequest struct {
	Owner  domain.OwnerRef
	BillOn time.Time
}

func (s *service) WithSettlement(ctx context.Context, req SettlementRequest, apply func(context.Context, State) (bool, error)) (State, error) {
	if !validDate(req.BillOn) || apply == nil {
		return nil, ierr.Validation("settlement requires a finite date and owner decision callback")
	}
	var out *state
	err := s.transaction(ctx, req.Owner, func(tx context.Context, original *domain.OwnerSnapshot) error {
		current, err := s.load(tx, original, s.now().UTC())
		if err != nil {
			return err
		}
		changed, err := apply(tx, current)
		if err != nil {
			return err
		}
		owner, err := s.snapshot(tx, req.Owner)
		if err != nil {
			return err
		}
		if !slices.Equal(owner.EligibilityScopes, original.EligibilityScopes) {
			return ierr.NewError("settlement eligibility changed; retry the operation").Mark(ierr.ErrVersionConflict)
		}
		if !changed {
			out, err = s.load(tx, owner, s.now().UTC())
			if err != nil {
				return err
			}
			if out.token != current.token {
				return integrity("an unchanged settlement decision altered billing state")
			}
			out.noop = true
			return nil
		}
		if !owner.OriginalTotal.Equal(original.OriginalTotal) || !owner.StartDate.Equal(original.StartDate) || !owner.EndDate.Equal(original.EndDate) || !strings.EqualFold(owner.Currency, original.Currency) {
			return integrity("settlement cannot change original owner economics or service bounds")
		}
		if err := validateSnapshot(owner); err != nil {
			return err
		}
		shortfall := owner.CollectionTarget.Sub(current.summary.AssignedAmount)
		if shortfall.IsNegative() {
			return integrity("settlement target is below retained assigned principal")
		}
		if shortfall.IsPositive() && billingDate(req.BillOn).Before(owner.StartDate) {
			return ierr.Validation("a settlement charge cannot precede the owner service term")
		}
		if shortfall.IsPositive() && current.owner.RetainedItemCount >= MaxItems {
			return ierr.Validation("retained principal leaves no room for a settlement item")
		}
		for _, item := range current.items {
			if item.Status != types.StatusPublished || item.ScheduleStatus != types.BillingScheduleScheduled {
				continue
			}
			canceled, err := s.items.Cancel(tx, item.ID, types.BillingScheduleTermination)
			if err != nil {
				return err
			}
			if !canceled {
				return conflict(current, "billing schedule changed during settlement")
			}
		}
		if err := s.items.Archive(tx, req.Owner); err != nil {
			return err
		}
		if err := s.rules.Archive(tx, req.Owner); err != nil {
			return err
		}
		if shortfall.IsPositive() {
			amount, err := money(shortfall, owner.Currency)
			if err != nil || !amount.Equal(shortfall) {
				return integrity("settlement shortfall must fit currency and storage precision")
			}
			date := billingDate(req.BillOn)
			item := domain.NewScheduleItemBuilder(&domain.ScheduleItem{
				ID: types.GenerateUUIDWithPrefix("bsi"), OwnerType: req.Owner.Type, OwnerID: req.Owner.ID,
				CustomerID: owner.CustomerID, SubscriptionID: clonePointer(owner.SubscriptionID), Currency: owner.Currency,
				Kind: types.BillingScheduleSettlement, BillTrigger: types.BillingScheduleOnDate, BillOn: &date,
				Amount: amount, Description: owner.Description, ServicePeriodStart: owner.StartDate, ServicePeriodEnd: owner.EndDate,
				Delivery: owner.Delivery, ScheduleStatus: types.BillingScheduleScheduled,
				EnvironmentID: owner.EnvironmentID, BaseModel: types.GetDefaultBaseModel(tx),
			}).Build()
			if err := s.items.Create(tx, item); err != nil {
				return err
			}
		}
		out, err = s.load(tx, owner, s.now().UTC())
		if err != nil {
			return err
		}
		if out.summary.RequiresReplan {
			return integrity("settlement does not allocate its frozen target")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
