package commitment

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Commitment is a subscription-level spend or quantity commitment.
type Commitment struct {
	ID                  string                            `json:"id"`
	SubscriptionID      string                            `json:"subscription_id"`
	CustomerID          string                            `json:"customer_id"`
	Name                string                            `json:"name"`
	InvoiceLabel        string                            `json:"invoice_label"`
	InvoiceDescription  *string                           `json:"invoice_description,omitempty"`
	Currency            string                            `json:"currency"`
	ScopeEntityType     types.CommitmentScopeType         `json:"scope_entity_type"`
	ScopeEntityID       string                            `json:"scope_entity_id"`
	Type                types.CommitmentType              `json:"type"`
	Value               decimal.Decimal                   `json:"value" swaggertype:"string"`
	CommittedPriceID    *string                           `json:"committed_price_id,omitempty"`
	OveragePriceID      *string                           `json:"overage_price_id,omitempty"`
	Measurement         types.CommitmentMeasurement       `json:"measurement"`
	ActiveWindows       types.ActiveWindows               `json:"active_windows,omitempty"`
	Timezone            string                            `json:"timezone"`
	Priority            int                               `json:"priority"`
	Binding             bool                              `json:"binding"`
	CollectionMethod    *types.CommitmentCollectionMethod `json:"collection_method,omitempty"`
	TotalAmount         *decimal.Decimal                  `json:"total_amount,omitempty" swaggertype:"string"`
	InstalmentInvoicing *string                           `json:"instalment_invoicing,omitempty"`
	StartDate           time.Time                         `json:"start_date"`
	// EndDate is exclusive; nil for an open-ended commitment, which runs until terminated.
	EndDate            *time.Time                        `json:"end_date,omitempty"`
	OnEarlyTermination *types.CommitmentEarlyTermination `json:"on_early_termination,omitempty"`
	CommitmentStatus   types.CommitmentStatus            `json:"commitment_status"`
	TerminatedAt       *time.Time                        `json:"terminated_at,omitempty"`
	IdempotencyKey     *string                           `json:"idempotency_key,omitempty"`
	Metadata           types.Metadata                    `json:"metadata,omitempty"`
	EnvironmentID      string                            `json:"environment_id"`
	types.BaseModel
}

// IsActiveAt reports whether t falls inside the term and the commitment has not ended early.
func (c *Commitment) IsActiveAt(t time.Time) bool {
	return c != nil && !t.Before(c.StartDate) && !c.EndedBy(t)
}

// EffectiveEnd is the end of the billed term: terminated_at when it cuts the term short, otherwise end_date.
// It is nil for an open-ended commitment: open-ended and not terminated.
func (c *Commitment) EffectiveEnd() *time.Time {
	if c.TerminatedAt != nil && (c.EndDate == nil || c.TerminatedAt.Before(*c.EndDate)) {
		return lo.ToPtr(*c.TerminatedAt)
	}

	if c.EndDate == nil {
		return nil
	}

	return lo.ToPtr(*c.EndDate)
}

// OpenEnded reports whether the commitment has no effective end: no end_date and not terminated.
func (c *Commitment) OpenEnded() bool {
	return c.EffectiveEnd() == nil
}

// Until returns the effective end capped at latest; an open-ended commitment runs to latest.
func (c *Commitment) Until(latest time.Time) time.Time {
	if end := c.EffectiveEnd(); end != nil && !end.After(latest) {
		return *end
	}

	return latest
}

// EndedBy reports whether the commitment has no term left after t: its effective end is at or before t, or it
// was terminated before it ever started. An unending commitment never ends.
func (c *Commitment) EndedBy(t time.Time) bool {
	end := c.EffectiveEnd()
	return end != nil && (!end.After(t) || !end.After(c.StartDate))
}

// Location returns the commitment timezone, falling back to UTC.
func (c *Commitment) Location() *time.Location {
	if c == nil {
		return time.UTC
	}

	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}

	return loc
}

func FromEnt(e *ent.Commitment) *Commitment {
	if e == nil {
		return nil
	}

	return &Commitment{
		ID:                  e.ID,
		SubscriptionID:      e.SubscriptionID,
		CustomerID:          e.CustomerID,
		Name:                e.Name,
		InvoiceLabel:        e.InvoiceLabel,
		InvoiceDescription:  e.InvoiceDescription,
		Currency:            e.Currency,
		ScopeEntityType:     e.ScopeEntityType,
		ScopeEntityID:       e.ScopeEntityID,
		Type:                e.Type,
		Value:               e.Value,
		CommittedPriceID:    e.CommittedPriceID,
		OveragePriceID:      e.OveragePriceID,
		Measurement:         e.Measurement,
		ActiveWindows:       e.ActiveWindows,
		Timezone:            e.Timezone,
		Priority:            e.Priority,
		Binding:             e.Binding,
		CollectionMethod:    e.CollectionMethod,
		TotalAmount:         e.TotalAmount,
		InstalmentInvoicing: e.InstalmentInvoicing,
		StartDate:           e.StartDate,
		EndDate:             e.EndDate,
		OnEarlyTermination:  e.OnEarlyTermination,
		CommitmentStatus:    e.CommitmentStatus,
		TerminatedAt:        e.TerminatedAt,
		IdempotencyKey:      e.IdempotencyKey,
		Metadata:            e.Metadata,
		EnvironmentID:       e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

func FromEntList(list []*ent.Commitment) []*Commitment {
	out := make([]*Commitment, 0, len(list))
	for _, e := range list {
		out = append(out, FromEnt(e))
	}

	return out
}

// Filter selects commitments for List and Count.
type Filter struct {
	*types.QueryFilter
	*types.TimeRangeFilter

	Filters         []*types.FilterCondition   `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort            []*types.SortCondition     `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	IDs             []string                   `json:"ids,omitempty" form:"ids"`
	SubscriptionIDs []string                   `json:"subscription_ids,omitempty" form:"subscription_ids"`
	CustomerIDs     []string                   `json:"customer_ids,omitempty" form:"customer_ids"`
	Statuses        []types.CommitmentStatus   `json:"statuses,omitempty" form:"statuses"`
	ScopeType       *types.CommitmentScopeType `json:"scope_entity_type,omitempty" form:"scope_entity_type"`
	ScopeIDs        []string                   `json:"scope_entity_ids,omitempty" form:"scope_entity_ids"`

	// ActiveFrom/ActiveTo select commitments whose term overlaps [ActiveFrom, ActiveTo).
	ActiveFrom *time.Time `json:"active_from,omitempty" form:"active_from"`
	ActiveTo   *time.Time `json:"active_to,omitempty" form:"active_to"`
}

func NewDefaultFilter() *Filter {
	return &Filter{
		QueryFilter: types.NewDefaultQueryFilter(),
	}
}

func NewNoLimitFilter() *Filter {
	return &Filter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	}
}

func (f Filter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	for _, condition := range f.Filters {
		if err := condition.Validate(); err != nil {
			return err
		}
	}

	for _, sort := range f.Sort {
		if err := sort.Validate(); err != nil {
			return err
		}
	}

	for _, s := range f.Statuses {
		if err := s.Validate(); err != nil {
			return err
		}
	}

	if f.ScopeType != nil {
		return f.ScopeType.Validate()
	}

	return nil
}

// GetLimit implements BaseFilter interface.
func (f *Filter) GetLimit() int {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetLimit()
	}

	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface.
func (f *Filter) GetOffset() int {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetOffset()
	}

	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface.
func (f *Filter) GetSort() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetSort()
	}

	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface.
func (f *Filter) GetOrder() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetOrder()
	}

	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter — row-level status, not the commitment status.
func (f *Filter) GetStatus() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetStatus()
	}

	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface.
func (f *Filter) GetExpand() types.Expand {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetExpand()
	}

	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if this is an unlimited query.
func (f *Filter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().IsUnlimited()
	}

	return f.QueryFilter.IsUnlimited()
}
