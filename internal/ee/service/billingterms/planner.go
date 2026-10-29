package billingterms

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

const (
	// MaxRules bounds a complete submitted rule set.
	MaxRules = 20
	// MaxItems bounds retained live claims plus newly generated occurrences.
	MaxItems = 500
)

// RuleInput is the create/edit wire definition before percentage normalization.
type RuleInput struct {
	Kind          types.BillingTermKind      `json:"kind"`
	ValueType     types.BillingTermValueType `json:"value_type"`
	Value         *decimal.Decimal           `json:"value,omitempty"`
	Percent       *decimal.Decimal           `json:"percent,omitempty"`
	BillOn        *time.Time                 `json:"bill_on,omitempty"`
	OnTrigger     bool                       `json:"on_trigger,omitempty"`
	StartDate     *time.Time                 `json:"start_date,omitempty"`
	EndDate       *time.Time                 `json:"end_date,omitempty"`
	Interval      *types.BillingPeriod       `json:"interval,omitempty"`
	IntervalCount *int                       `json:"interval_count,omitempty"`
	BillAt        *types.BillingTermTiming   `json:"bill_at,omitempty"`
	Split         *types.BillingTermSplit    `json:"split,omitempty"`
	Description   string                     `json:"description,omitempty"`
	Metadata      types.Metadata             `json:"metadata,omitempty"`
}

// PlanRequest carries authoritative owner economics and the full replacement input.
type PlanRequest struct {
	Owner    domain.OwnerSnapshot `json:"owner"`
	Rows     []RuleInput          `json:"rows"`
	Edit     bool                 `json:"edit"`
	Calendar Calendar             `json:"-"`
}

// Plan is an eager, unpersisted expansion with detached model getters.
type Plan interface {
	Rules() []*domain.Rule
	Items() []*domain.ScheduleItem
	ItemsForRule(index int) []*domain.ScheduleItem
	Budget() decimal.Decimal
}

// Planner validates and expands finite collection rules without persistence.
type Planner interface {
	Plan(context.Context, PlanRequest) (Plan, error)
}

type planner struct{}

type plan struct {
	rules       []*domain.Rule
	itemsByRule [][]*domain.ScheduleItem
	budget      decimal.Decimal
}

// NewPlanner returns the owner-neutral collection planner.
func NewPlanner() Planner { return &planner{} }

func (p *plan) Rules() []*domain.Rule {
	if p == nil {
		return nil
	}
	rules := make([]*domain.Rule, 0, len(p.rules))
	for _, rule := range p.rules {
		rules = append(rules, domain.NewRuleBuilder(rule).Build())
	}
	return rules
}

func (p *plan) Items() []*domain.ScheduleItem {
	if p == nil {
		return nil
	}
	var items []*domain.ScheduleItem
	for index := range p.itemsByRule {
		items = append(items, p.ItemsForRule(index)...)
	}
	return items
}

func (p *plan) ItemsForRule(index int) []*domain.ScheduleItem {
	if p == nil || index < 0 || index >= len(p.itemsByRule) {
		return nil
	}
	items := make([]*domain.ScheduleItem, 0, len(p.itemsByRule[index]))
	for _, item := range p.itemsByRule[index] {
		items = append(items, domain.NewScheduleItemBuilder(item).Build())
	}
	return items
}

func (p *plan) Budget() decimal.Decimal {
	if p == nil {
		return decimal.Zero
	}
	return p.budget
}

func (p *planner) Plan(ctx context.Context, req PlanRequest) (Plan, error) {
	if nilInterface(ctx) {
		return nil, ierr.Validation("billing context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, budget, err := normalizeOwner(req.Owner, req.Edit)
	if err != nil {
		return nil, err
	}
	if len(req.Rows) == 0 || len(req.Rows) > MaxRules {
		return nil, ierr.Validationf("billing terms require between 1 and %d rules", MaxRules)
	}
	out := &plan{budget: budget, itemsByRule: make([][]*domain.ScheduleItem, len(req.Rows))}
	itemCount, remainderIndex := owner.RetainedItemCount, -1
	fixedTotal := decimal.Zero
	for index, input := range req.Rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if owner.DatedOnceOnly && (input.Kind != types.BillingTermOnce || input.OnTrigger || input.BillOn == nil) {
			return nil, ruleError(index, "kind", "owner permits only dated once collection corrections")
		}
		rule, err := normalizeRule(input, owner, req.Edit, index)
		if err != nil {
			return nil, err
		}
		if rule.ValueType == types.BillingTermRemainder {
			if remainderIndex >= 0 {
				return nil, ruleError(index, "value_type", "at most one remainder rule is allowed")
			}
			remainderIndex = index
		}
		items, err := expand(ctx, rule, owner, req.Calendar, MaxItems-itemCount, index)
		if err != nil {
			return nil, err
		}
		itemCount += len(items)
		if rule.ValueType == types.BillingTermFixed {
			fixedTotal = fixedTotal.Add(rule.Value.Mul(decimal.NewFromInt(int64(len(items)))))
		}
		out.rules = append(out.rules, rule)
		out.itemsByRule[index] = items
	}
	if remainderIndex < 0 {
		if !fixedTotal.Equal(budget) {
			return nil, ierr.Validation("fixed occurrences must allocate the complete unassigned target")
		}
		return out, nil
	}
	remainder := budget.Sub(fixedTotal)
	if !remainder.IsPositive() {
		return nil, ruleError(remainderIndex, "value_type", "remainder must be positive after fixed occurrences")
	}
	if err := allocateRemainder(out.itemsByRule[remainderIndex], out.rules[remainderIndex], remainder, owner.Currency); err != nil {
		return nil, ruleError(remainderIndex, "split", err.Error())
	}
	return out, nil
}

func normalizeOwner(owner domain.OwnerSnapshot, edit bool) (domain.OwnerSnapshot, decimal.Decimal, error) {
	owner.StartDate, owner.EndDate = billingDate(owner.StartDate), billingDate(owner.EndDate)
	if err := owner.Owner.Type.Validate(); err != nil {
		return owner, decimal.Zero, err
	}
	if owner.Owner.ID == "" || owner.TenantID == "" || owner.EnvironmentID == "" || owner.CustomerID == "" {
		return owner, decimal.Zero, ierr.Validation("owner, tenant, environment and invoice recipient are required")
	}
	if !validDate(owner.StartDate) || !validDate(owner.EndDate) || !owner.StartDate.Before(owner.EndDate) {
		return owner, decimal.Zero, ierr.Validation("owner requires an ordered finite term")
	}
	if !validTimezone(owner.Timezone) {
		return owner, decimal.Zero, ierr.Validation("owner requires a valid IANA timezone")
	}
	owner.Currency = strings.ToLower(owner.Currency)
	if err := types.ValidateCurrencyCode(owner.Currency); err != nil {
		return owner, decimal.Zero, err
	}
	if err := owner.Delivery.Validate(); err != nil {
		return owner, decimal.Zero, err
	}
	if owner.Delivery == types.BillingTermSubscriptionInvoice && (owner.SubscriptionID == nil || *owner.SubscriptionID == "") {
		return owner, decimal.Zero, ierr.Validation("subscription delivery requires a subscription")
	}
	if edit && !owner.CanEdit {
		return owner, decimal.Zero, ierr.Validation("owner does not permit collection editing")
	}
	if owner.RetainedItemCount < 0 || owner.RetainedItemCount >= MaxItems {
		return owner, decimal.Zero, ierr.Validation("retained items leave no capacity for a replacement schedule")
	}
	var err error
	owner.OriginalTotal, err = money(owner.OriginalTotal, owner.Currency)
	if err != nil {
		return owner, decimal.Zero, ierr.Validation("original total must be positive and fit currency/storage precision")
	}
	owner.CollectionTarget, err = money(owner.CollectionTarget, owner.Currency)
	if err != nil {
		return owner, decimal.Zero, ierr.Validation("collection target must be positive and fit currency/storage precision")
	}
	if owner.AssignedAmount.IsNegative() || (owner.AssignedAmount.IsPositive() != (owner.RetainedItemCount > 0)) {
		return owner, decimal.Zero, ierr.Validation("assigned principal and retained item count must be consistent")
	}
	if owner.AssignedAmount.IsPositive() {
		assigned, err := money(owner.AssignedAmount, owner.Currency)
		if err != nil || !assigned.Equal(owner.AssignedAmount) {
			return owner, decimal.Zero, ierr.Validation("assigned principal must already be currency-rounded")
		}
	}
	budget := owner.CollectionTarget.Sub(owner.AssignedAmount)
	if !budget.IsPositive() {
		return owner, decimal.Zero, ierr.Validation("owner has no positive unassigned collection target")
	}
	return owner, budget, nil
}

func normalizeRule(input RuleInput, owner domain.OwnerSnapshot, edit bool, index int) (*domain.Rule, error) {
	if err := input.Kind.Validate(); err != nil {
		return nil, ruleError(index, "kind", err.Error())
	}
	if err := input.ValueType.Validate(); err != nil {
		return nil, ruleError(index, "value_type", err.Error())
	}
	if utf8.RuneCountInString(input.Description) > 255 {
		return nil, ruleError(index, "description", "description exceeds 255 characters")
	}
	if input.Description == "" && utf8.RuneCountInString(owner.Description) > 255 {
		return nil, ruleError(index, "description", "resolved description exceeds 255 characters")
	}
	rule := &domain.Rule{
		OwnerType: owner.Owner.Type, OwnerID: owner.Owner.ID,
		Kind: input.Kind, ValueType: input.ValueType, OnTrigger: input.OnTrigger,
		Description: input.Description, Metadata: cloneMetadata(input.Metadata),
		EnvironmentID: owner.EnvironmentID,
		BaseModel:     types.BaseModel{TenantID: owner.TenantID, Status: types.StatusPublished},
	}
	if input.ValueType == types.BillingTermRemainder {
		if input.Value != nil || input.Percent != nil {
			return nil, ruleError(index, "value", "remainder rejects value and percent")
		}
	} else {
		if (input.Value == nil) == (input.Percent == nil) {
			return nil, ruleError(index, "value", "fixed requires exactly one value or percent")
		}
		var amount decimal.Decimal
		if input.Value != nil {
			amount = *input.Value
		} else {
			percent := *input.Percent
			if !percent.IsPositive() || magnitude(percent) > 3 || (magnitude(percent) >= 0 && percent.GreaterThan(decimal.NewFromInt(100))) {
				return nil, ruleError(index, "percent", "percent must be in (0,100]")
			}
			if magnitude(percent) <= -int64(types.GetCurrencyPrecision(owner.Currency))-11 {
				return nil, ruleError(index, "percent", "percent produces no positive currency-rounded amount")
			}
			amount = owner.OriginalTotal.Mul(percent).Shift(-2)
		}
		amount, err := money(amount, owner.Currency)
		if err != nil {
			return nil, ruleError(index, "value", err.Error())
		}
		rule.Value = &amount
		if input.Percent != nil {
			rule.Metadata["input_percent"] = input.Percent.String()
		}
	}
	if input.Kind == types.BillingTermOnce {
		if input.StartDate != nil || input.EndDate != nil || input.Interval != nil || input.IntervalCount != nil || input.BillAt != nil || input.Split != nil {
			return nil, ruleError(index, "kind", "once rejects recurring fields")
		}
		if input.OnTrigger {
			if input.BillOn != nil {
				return nil, ruleError(index, "bill_on", "trigger and date are mutually exclusive")
			}
			return rule, nil
		}
		if edit && input.BillOn == nil {
			return nil, ruleError(index, "bill_on", "dated once requires an explicit date on edit")
		}
		date := owner.StartDate
		if input.BillOn != nil {
			date = *input.BillOn
		}
		if !validDate(date) || date.Before(owner.StartDate) || date.After(owner.EndDate) {
			return nil, ruleError(index, "bill_on", "bill date must lie within the owner term")
		}
		date = billingDate(date)
		rule.BillOn = &date
		return rule, nil
	}
	if input.BillOn != nil || input.OnTrigger {
		return nil, ruleError(index, "kind", "recurring rejects once date and trigger fields")
	}
	if edit && input.StartDate == nil {
		return nil, ruleError(index, "start_date", "recurring requires an explicit start on edit")
	}
	start, end := owner.StartDate, owner.EndDate
	if input.StartDate != nil {
		start = *input.StartDate
	}
	if input.EndDate != nil {
		end = *input.EndDate
	}
	if !validDate(start) || !validDate(end) || !start.Before(end) || start.Before(owner.StartDate) || end.After(owner.EndDate) {
		return nil, ruleError(index, "start_date", "recurring range must be ordered and inside owner term")
	}
	if input.Interval == nil && owner.DefaultInterval != "" {
		input.Interval = &owner.DefaultInterval
		if input.IntervalCount == nil && owner.DefaultIntervalCount != 0 {
			input.IntervalCount = &owner.DefaultIntervalCount
		}
	}
	if input.Interval == nil || !validInterval(*input.Interval) {
		return nil, ruleError(index, "interval", "recurring requires a valid frequency")
	}
	count := 1
	if input.IntervalCount != nil {
		count = *input.IntervalCount
	}
	if !validCount(count) {
		return nil, ruleError(index, "interval_count", "interval count must be a positive 32-bit integer")
	}
	if input.BillAt == nil || input.BillAt.Validate() != nil {
		return nil, ruleError(index, "bill_at", "recurring requires advance or arrear timing")
	}
	if input.ValueType == types.BillingTermFixed && input.Split != nil {
		return nil, ruleError(index, "split", "split is only valid for recurring remainder")
	}
	start, end = billingDate(start), billingDate(end)
	if !start.Before(end) {
		return nil, ruleError(index, "start_date", "recurring range must span at least one microsecond")
	}
	interval, timing := *input.Interval, *input.BillAt
	rule.StartDate, rule.EndDate = &start, &end
	rule.Interval, rule.IntervalCount, rule.BillAt = &interval, &count, &timing
	if input.ValueType == types.BillingTermRemainder {
		split := types.BillingTermEven
		if input.Split != nil {
			split = *input.Split
		}
		if err := split.Validate(); err != nil {
			return nil, ruleError(index, "split", err.Error())
		}
		rule.Split = &split
	}
	return rule, nil
}

func expand(ctx context.Context, rule *domain.Rule, owner domain.OwnerSnapshot, calendar Calendar, capacity, index int) ([]*domain.ScheduleItem, error) {
	if capacity <= 0 {
		return nil, ruleError(index, "interval", "plan exceeds 500 relevant items")
	}
	if rule.Kind == types.BillingTermOnce {
		return []*domain.ScheduleItem{newItem(owner, rule, owner.StartDate, owner.EndDate, rule.BillOn)}, nil
	}
	if calendar == nil {
		return nil, ruleError(index, "interval", "recurring requires an owner calendar")
	}
	var items []*domain.ScheduleItem
	for start := *rule.StartDate; start.Before(*rule.EndDate); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(items) == capacity {
			return nil, ruleError(index, "interval", "plan exceeds 500 relevant items")
		}
		end, err := calendar.Next(start, *rule.EndDate, *rule.Interval, *rule.IntervalCount)
		if err != nil {
			return nil, ruleError(index, "start_date", err.Error())
		}
		end = billingDate(end)
		if !end.After(start) || end.After(*rule.EndDate) || !validDate(end) {
			return nil, ruleError(index, "interval", "owner calendar returned an invalid next boundary")
		}
		billOn := start
		if *rule.BillAt == types.BillingTermArrear {
			billOn = end
		}
		items = append(items, newItem(owner, rule, start, end, &billOn))
		start = end
	}
	return items, nil
}

func newItem(owner domain.OwnerSnapshot, rule *domain.Rule, start, end time.Time, billOn *time.Time) *domain.ScheduleItem {
	description := rule.Description
	if description == "" {
		description = owner.Description
	}
	if description == "" {
		description = "Scheduled payment"
	}
	item := &domain.ScheduleItem{
		OwnerType: owner.Owner.Type, OwnerID: owner.Owner.ID,
		CustomerID: owner.CustomerID, SubscriptionID: clonePointer(owner.SubscriptionID),
		Currency: owner.Currency, Kind: types.BillingScheduleCharge,
		BillTrigger: types.BillingScheduleOnDate, BillOn: clonePointer(billOn),
		Description: description, ServicePeriodStart: start.UTC(), ServicePeriodEnd: end.UTC(),
		Delivery: owner.Delivery, ScheduleStatus: types.BillingScheduleScheduled,
		EnvironmentID: owner.EnvironmentID,
		BaseModel:     types.BaseModel{TenantID: owner.TenantID, Status: types.StatusPublished},
	}
	if rule.OnTrigger {
		item.BillTrigger = types.BillingScheduleManual
	}
	if rule.Value != nil {
		item.Amount = *rule.Value
	}
	return item
}

func allocateRemainder(items []*domain.ScheduleItem, rule *domain.Rule, amount decimal.Decimal, currency string) error {
	weights := make([]decimal.Decimal, len(items))
	total := decimal.Zero
	for i, item := range items {
		weight := decimal.NewFromInt(1)
		if rule.Split != nil && *rule.Split == types.BillingTermProportional {
			weight = elapsedNanos(item.ServicePeriodStart, item.ServicePeriodEnd)
		}
		weights[i], total = weight, total.Add(weight)
	}
	remaining := amount
	precision := types.GetCurrencyPrecision(currency)
	for i, item := range items {
		part := remaining
		if i < len(items)-1 {
			part = amount.Mul(weights[i]).DivRound(total, precision+24).Round(precision)
		}
		if !part.IsPositive() {
			return ierr.Validation("every rounded remainder occurrence must be positive")
		}
		items[i] = domain.NewScheduleItemBuilder(item).WithAmount(part).Build()
		remaining = remaining.Sub(part)
	}
	return nil
}

func elapsedNanos(start, end time.Time) decimal.Decimal {
	seconds := decimal.NewFromInt(end.Unix()).Sub(decimal.NewFromInt(start.Unix()))
	return seconds.Shift(9).Add(decimal.NewFromInt(int64(end.Nanosecond() - start.Nanosecond())))
}

func magnitude(value decimal.Decimal) int64 {
	return int64(len(strings.TrimPrefix(value.Coefficient().String(), "-"))) + int64(value.Exponent())
}

func money(value decimal.Decimal, currency string) (decimal.Decimal, error) {
	precision := types.GetCurrencyPrecision(currency)
	if !value.IsPositive() || magnitude(value) > 12 || magnitude(value) < -int64(precision) {
		return decimal.Zero, ierr.Validation("amount must be positive at currency precision and fit numeric(20,8)")
	}
	rounded := value.Round(precision)
	if !rounded.IsPositive() || magnitude(rounded) > 12 || precision > 8 {
		return decimal.Zero, ierr.Validation("amount must be positive at currency precision and fit numeric(20,8)")
	}
	return rounded, nil
}

func validDate(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1 && value.Year() <= 9999 &&
		value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
}

func billingDate(value time.Time) time.Time { return value.UTC().Truncate(time.Microsecond) }

func cloneMetadata(input types.Metadata) types.Metadata {
	metadata := make(types.Metadata, len(input))
	for key, value := range input {
		metadata[key] = value
	}
	return metadata
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func ruleError(index int, field, message string) error {
	return ierr.NewErrorf("billing term %d: %s", index, message).
		WithHint(message).
		WithReportableDetails(map[string]any{"rule_index": index, "field": field}).
		Mark(ierr.ErrValidation)
}
