package billingterms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type state struct {
	owner    *domain.OwnerSnapshot
	rules    []*domain.Rule
	items    []*domain.ScheduleItem
	bindings map[string]domain.BindingState
	summary  *domain.CollectionSummary
	token    string
	noop     bool
}

func (s *state) Owner() *domain.OwnerSnapshot {
	if s == nil {
		return nil
	}
	return cloneOwner(s.owner)
}

func (s *state) Rules() []*domain.Rule {
	if s == nil {
		return nil
	}
	out := make([]*domain.Rule, 0, len(s.rules))
	for _, rule := range s.rules {
		out = append(out, domain.NewRuleBuilder(rule).Build())
	}
	return out
}

func (s *state) Items() []*domain.ScheduleItem {
	if s == nil {
		return nil
	}
	out := make([]*domain.ScheduleItem, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, domain.NewScheduleItemBuilder(item).Build())
	}
	return out
}

func (s *state) Summary() *domain.CollectionSummary {
	if s == nil || s.summary == nil {
		return nil
	}
	copy := *s.summary
	copy.DeferredReason = clonePointer(copy.DeferredReason)
	copy.NextBillOn = clonePointer(copy.NextBillOn)
	return &copy
}

func (s *state) StateToken() string {
	if s == nil {
		return ""
	}
	return s.token
}

func (s *state) Noop() bool { return s != nil && s.noop }

func (s *service) load(ctx context.Context, snapshot *domain.OwnerSnapshot, asOf time.Time) (*state, error) {
	owner := cloneOwner(snapshot)
	if err := validateSnapshot(owner); err != nil {
		return nil, err
	}
	ref := owner.Owner
	rows, err := s.rules.List(ctx, &domain.RuleFilter{Owner: &ref, Limit: MaxRules + 1})
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxRules {
		return nil, integrity("billing owner exceeds the current rule limit")
	}
	out := &state{owner: owner, rules: rows, bindings: make(map[string]domain.BindingState)}
	currentRules := make(map[string]bool, len(rows))
	for _, rule := range rows {
		if rule == nil || rule.ID == "" || currentRules[rule.ID] || rule.OwnerType != ref.Type || rule.OwnerID != ref.ID || rule.TenantID != owner.TenantID || rule.EnvironmentID != owner.EnvironmentID || rule.Status != types.StatusPublished {
			return nil, integrity("current billing rule has invalid scoped identity")
		}
		currentRules[rule.ID] = true
	}
	filter := &domain.ScheduleItemFilter{Owner: &ref, IncludeArchived: true, RelevantOnly: true, Limit: MaxItems}
	out.items, err = s.items.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(out.items) > MaxItems {
		return nil, integrity("billing owner exceeds the relevant item limit")
	}
	if len(out.items) == MaxItems {
		last := out.items[len(out.items)-1]
		if last == nil {
			return nil, integrity("billing schedule contains a missing source")
		}
		filter.After = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
		filter.Limit = 1
		extra, err := s.items.List(ctx, filter)
		if err != nil {
			return nil, err
		}
		if len(extra) > 0 {
			return nil, integrity("billing owner exceeds the relevant item limit")
		}
	}
	seen := make(map[string]bool, len(out.items))
	assigned := make([]*domain.ScheduleItem, 0)
	for _, item := range out.items {
		if err = validateItem(item, owner, currentRules, seen); err != nil {
			return nil, err
		}
		if item.ScheduleStatus == types.BillingScheduleInvoiced {
			assigned = append(assigned, item)
		}
	}
	if len(assigned) > 0 {
		states, err := s.store.BindingStates(ctx, assigned)
		if err != nil {
			return nil, err
		}
		for _, binding := range states {
			if binding.ItemID == "" || out.bindings[binding.ItemID].ItemID != "" {
				return nil, integrity("billing invoice state has duplicate or missing source identity")
			}
			out.bindings[binding.ItemID] = binding
		}
		if len(out.bindings) != len(assigned) {
			return nil, integrity("billing invoice state is incomplete")
		}
		for _, item := range assigned {
			binding, ok := out.bindings[item.ID]
			if !ok || binding.InvoiceID != *item.InvoiceID || binding.InvoiceLineItemID != *item.InvoiceLineItemID || binding.TenantID != owner.TenantID || binding.EnvironmentID != owner.EnvironmentID {
				return nil, integrity("billing invoice state does not match its scoped source")
			}
			if binding.InvoiceStatus != types.InvoiceStatusDraft && binding.InvoiceStatus != types.InvoiceStatusFinalized {
				return nil, integrity("billing item has a voided, skipped or invalid invoice binding; cancel its source before replanning")
			}
		}
	}
	out.summary, err = collectionSummary(owner, out.items, out.bindings, asOf)
	if err != nil {
		return nil, err
	}
	owner.AssignedAmount = out.summary.AssignedAmount
	owner.RetainedItemCount = len(assigned)
	out.token, err = stateToken(out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func validateSnapshot(owner *domain.OwnerSnapshot) error {
	owner.StartDate, owner.EndDate = billingDate(owner.StartDate), billingDate(owner.EndDate)
	owner.CalendarAnchor = billingDate(owner.CalendarAnchor)
	if owner.CustomerID == "" || !validDate(owner.StartDate) || !validDate(owner.EndDate) || !owner.StartDate.Before(owner.EndDate) {
		return integrity("billing owner has no finite ordered term or invoice recipient")
	}
	if !validTimezone(owner.Timezone) {
		return integrity("billing owner timezone is invalid")
	}
	owner.Currency = strings.ToLower(owner.Currency)
	if types.ValidateCurrencyCode(owner.Currency) != nil {
		return integrity("billing owner currency is unsupported")
	}
	if owner.Delivery.Validate() != nil || (owner.Delivery == types.BillingTermSubscriptionInvoice && (owner.SubscriptionID == nil || *owner.SubscriptionID == "")) {
		return integrity("billing owner delivery is invalid")
	}
	original, err := money(owner.OriginalTotal, owner.Currency)
	if err != nil || !original.Equal(owner.OriginalTotal) {
		return integrity("billing original total is invalid")
	}
	if owner.CollectionTarget.IsNegative() {
		return integrity("billing target cannot be negative")
	}
	if owner.CollectionTarget.IsPositive() {
		target, err := money(owner.CollectionTarget, owner.Currency)
		if err != nil || !target.Equal(owner.CollectionTarget) {
			return integrity("billing target is invalid")
		}
	}
	return nil
}

func validateItem(item *domain.ScheduleItem, owner *domain.OwnerSnapshot, currentRules, seen map[string]bool) error {
	if item == nil || item.ID == "" || seen[item.ID] {
		return integrity("billing schedule contains a duplicate or missing source")
	}
	seen[item.ID] = true
	if item.OwnerType != owner.Owner.Type || item.OwnerID != owner.Owner.ID || item.TenantID != owner.TenantID || item.EnvironmentID != owner.EnvironmentID || !strings.EqualFold(item.Currency, owner.Currency) {
		return integrity("billing schedule source does not match its owner scope or currency")
	}
	if item.CustomerID == "" || (item.SubscriptionID != nil && *item.SubscriptionID == "") || (item.Delivery == types.BillingTermSubscriptionInvoice && item.SubscriptionID == nil) {
		return integrity("billing schedule source is missing its invoice recipient or subscription")
	}
	if item.Status != types.StatusPublished && item.Status != types.StatusArchived {
		return integrity("billing schedule source visibility is invalid")
	}
	if item.Kind.Validate() != nil || item.BillTrigger.Validate() != nil || item.Delivery.Validate() != nil || !validDate(item.ServicePeriodStart) || !validDate(item.ServicePeriodEnd) || !item.ServicePeriodStart.Before(item.ServicePeriodEnd) {
		return integrity("billing schedule source economics are invalid")
	}
	if item.ServicePeriodStart.Before(owner.StartDate) || item.ServicePeriodEnd.After(owner.EndDate) {
		return integrity("billing schedule service period lies outside its owner term")
	}
	amount, err := money(item.Amount, owner.Currency)
	if err != nil || !amount.Equal(item.Amount) {
		return integrity("billing schedule amount is invalid")
	}
	if item.BillOn != nil && !validDate(*item.BillOn) {
		return integrity("billing schedule date is invalid")
	}
	if item.BillTrigger == types.BillingScheduleOnDate && item.BillOn == nil {
		return integrity("dated billing schedule item is missing its date")
	}
	if item.Kind == types.BillingScheduleCharge && (item.BillingTermID == nil || *item.BillingTermID == "") {
		return integrity("billing schedule charge has no source rule")
	}
	if item.Status == types.StatusPublished && item.BillingTermID != nil && !currentRules[*item.BillingTermID] {
		return integrity("current billing schedule item has an archived or missing rule")
	}
	switch item.ScheduleStatus {
	case types.BillingScheduleScheduled:
		if item.Status != types.StatusPublished || item.InvoiceID != nil || item.InvoiceLineItemID != nil || item.CancellationReason != nil {
			return integrity("pending billing source has invalid execution history")
		}
	case types.BillingScheduleInvoiced:
		if item.InvoiceID == nil || *item.InvoiceID == "" || item.InvoiceLineItemID == nil || *item.InvoiceLineItemID == "" || item.BillOn == nil || item.CancellationReason != nil {
			return integrity("assigned billing source is missing its invoice history")
		}
	default:
		return integrity("irrelevant or canceled billing schedule history was returned")
	}
	return nil
}

func sameRules(inputs []RuleInput, current *state) (bool, error) {
	if len(inputs) == 0 || len(inputs) > MaxRules {
		return false, ierr.Validationf("billing terms require between 1 and %d rules", MaxRules)
	}
	if len(inputs) != len(current.rules) {
		return false, nil
	}
	desired := make([]string, 0, len(inputs))
	existing := make([]string, 0, len(inputs))
	for index, input := range inputs {
		rule, err := normalizeRule(input, *current.owner, false, index)
		if err != nil {
			return false, err
		}
		signature, err := ruleSignature(rule)
		if err != nil {
			return false, err
		}
		desired = append(desired, signature)
	}
	for _, rule := range current.rules {
		signature, err := ruleSignature(rule)
		if err != nil {
			return false, err
		}
		existing = append(existing, signature)
	}
	sort.Strings(desired)
	sort.Strings(existing)
	for index := range desired {
		if desired[index] != existing[index] {
			return false, nil
		}
	}
	return true, nil
}

func ruleSignature(rule *domain.Rule) (string, error) {
	metadata := cloneMetadata(rule.Metadata)
	delete(metadata, "input_percent")
	input := RuleInput{
		Kind:          rule.Kind,
		ValueType:     rule.ValueType,
		Value:         rule.Value,
		BillOn:        rule.BillOn,
		OnTrigger:     rule.OnTrigger,
		StartDate:     rule.StartDate,
		EndDate:       rule.EndDate,
		Interval:      rule.Interval,
		IntervalCount: rule.IntervalCount,
		BillAt:        rule.BillAt,
		Split:         rule.Split,
		Description:   rule.Description,
		Metadata:      metadata,
	}
	encoded, err := json.Marshal(input)
	return string(encoded), err
}

func stateToken(current *state) (string, error) {
	owner := current.owner
	ownerState := map[string]any{
		"owner":              owner.Owner,
		"tenant":             owner.TenantID,
		"environment":        owner.EnvironmentID,
		"original":           owner.OriginalTotal.String(),
		"target":             owner.CollectionTarget.String(),
		"start":              owner.StartDate.UTC(),
		"end":                owner.EndDate.UTC(),
		"timezone":           owner.Timezone,
		"currency":           owner.Currency,
		"customer":           owner.CustomerID,
		"subscription":       owner.SubscriptionID,
		"delivery":           owner.Delivery,
		"can_edit":           owner.CanEdit,
		"can_collect":        owner.CanCollect,
		"dated_once_only":    owner.DatedOnceOnly,
		"lifecycle":          owner.LifecycleKey,
		"alignment":          owner.AlignmentKey,
		"anchor":             owner.CalendarAnchor.UTC(),
		"default_interval":   owner.DefaultInterval,
		"default_count":      owner.DefaultIntervalCount,
		"deferred":           owner.DeferredReason,
		"description":        owner.Description,
		"eligibility_scopes": owner.EligibilityScopes,
	}
	rules := make([]string, 0, len(current.rules))
	for _, rule := range current.rules {
		definition, err := ruleSignature(rule)
		if err != nil {
			return "", err
		}
		rules = append(rules, rule.ID+":"+definition)
	}
	sort.Strings(rules)
	items := make([]string, 0, len(current.items))
	for _, item := range current.items {
		projection := map[string]any{
			"id":              item.ID,
			"owner_type":      item.OwnerType,
			"owner_id":        item.OwnerID,
			"source":          item.BillingTermID,
			"customer":        item.CustomerID,
			"subscription":    item.SubscriptionID,
			"currency":        item.Currency,
			"kind":            item.Kind,
			"trigger":         item.BillTrigger,
			"bill_on":         item.BillOn,
			"amount":          item.Amount.String(),
			"description":     item.Description,
			"period_start":    item.ServicePeriodStart.UTC(),
			"period_end":      item.ServicePeriodEnd.UTC(),
			"delivery":        item.Delivery,
			"status":          item.Status,
			"schedule_status": item.ScheduleStatus,
			"invoice":         item.InvoiceID,
			"line":            item.InvoiceLineItemID,
			"cancellation":    item.CancellationReason,
			"metadata":        item.Metadata,
		}
		if binding, ok := current.bindings[item.ID]; ok {
			projection["invoice_status"] = binding.InvoiceStatus
			projection["invoice_archived"] = binding.InvoiceArchived
		}
		encoded, err := json.Marshal(projection)
		if err != nil {
			return "", err
		}
		items = append(items, string(encoded))
	}
	sort.Strings(items)
	encoded, err := json.Marshal(map[string]any{"owner": ownerState, "rules": rules, "items": items})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
