package ent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/flexprice/flexprice/ent"
	itemdb "github.com/flexprice/flexprice/ent/billingscheduleitem"
	ruledb "github.com/flexprice/flexprice/ent/billingterm"
	customerdb "github.com/flexprice/flexprice/ent/customer"
	invoicedb "github.com/flexprice/flexprice/ent/invoice"
	linedb "github.com/flexprice/flexprice/ent/invoicelineitem"
	"github.com/flexprice/flexprice/ent/predicate"
	subscriptiondb "github.com/flexprice/flexprice/ent/subscription"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

const (
	billingDefaultPage = 100
	billingMaximumPage = 500
)

type billingTermRepository struct {
	client postgres.IClient
}

type billingScheduleItemRepository struct {
	client postgres.IClient
}

// NewBillingTermRepository provides tenant-scoped immutable rule persistence.
func NewBillingTermRepository(client postgres.IClient) domain.RuleRepository {
	if client == nil {
		return nil
	}
	return &billingTermRepository{client: client}
}

// NewBillingScheduleItemRepository provides scoped charge and claim persistence.
func NewBillingScheduleItemRepository(client postgres.IClient) domain.ScheduleItemRepository {
	if client == nil {
		return nil
	}
	return &billingScheduleItemRepository{client: client}
}

func (r *billingTermRepository) Create(ctx context.Context, rule *domain.Rule) error {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return err
	}
	if rule == nil {
		return ierr.Validation("billing rule is required")
	}
	if err := billingModelScope(rule.TenantID, rule.EnvironmentID, tenantID, environmentID); err != nil {
		return err
	}
	if err := billingIdentifier("billing rule id", rule.ID); err != nil {
		return err
	}
	if err := billingOwner(domain.OwnerRef{Type: rule.OwnerType, ID: rule.OwnerID}); err != nil {
		return err
	}
	if err := rule.Kind.Validate(); err != nil {
		return err
	}
	if err := rule.ValueType.Validate(); err != nil {
		return err
	}
	status, err := billingCreateStatus(rule.Status)
	if err != nil {
		return err
	}
	copy := domain.NewRuleBuilder(rule).Build()
	create := r.client.Writer(ctx).BillingTerm.Create().
		SetID(copy.ID).SetTenantID(tenantID).SetEnvironmentID(environmentID).
		SetOwnerType(copy.OwnerType).SetOwnerID(copy.OwnerID).
		SetKind(copy.Kind).SetValueType(copy.ValueType).SetNillableValue(copy.Value).
		SetNillableBillOn(copy.BillOn).SetOnTrigger(copy.OnTrigger).
		SetNillableStartDate(copy.StartDate).SetNillableEndDate(copy.EndDate).
		SetNillableInterval(copy.Interval).SetNillableIntervalCount(copy.IntervalCount).
		SetNillableBillAt(copy.BillAt).SetNillableSplit(copy.Split).
		SetDescription(copy.Description).SetMetadata(copy.Metadata).
		SetStatus(string(status)).SetCreatedBy(types.GetUserID(ctx)).SetUpdatedBy(types.GetUserID(ctx))
	if !copy.CreatedAt.IsZero() {
		create.SetCreatedAt(copy.CreatedAt)
	}
	if !copy.UpdatedAt.IsZero() {
		create.SetUpdatedAt(copy.UpdatedAt)
	}
	if _, err := create.Save(ctx); err != nil {
		return billingPersistenceError(err, "create billing rule")
	}
	return nil
}

func (r *billingTermRepository) Get(ctx context.Context, id string) (*domain.Rule, error) {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return nil, err
	}
	if err := billingIdentifier("billing rule id", id); err != nil {
		return nil, err
	}
	row, err := r.client.Reader(ctx).BillingTerm.Query().Where(
		ruledb.TenantID(tenantID), ruledb.EnvironmentID(environmentID), ruledb.ID(id),
		ruledb.StatusIn(string(types.StatusPublished), string(types.StatusArchived)),
	).Only(ctx)
	if err != nil {
		return nil, billingPersistenceError(err, "get billing rule")
	}
	return billingRuleFromEnt(row), nil
}

func (r *billingTermRepository) List(ctx context.Context, filter *domain.RuleFilter) ([]*domain.Rule, error) {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return nil, err
	}
	f := domain.RuleFilter{}
	if filter != nil {
		f = *filter
	}
	limit, err := billingListLimit(f.Limit, f.After)
	if err != nil {
		return nil, err
	}
	if err := billingIdentifiers("rule ids", f.IDs); err != nil {
		return nil, err
	}
	q := r.client.Reader(ctx).BillingTerm.Query().Where(ruledb.TenantID(tenantID), ruledb.EnvironmentID(environmentID))
	if f.IncludeArchived {
		q.Where(ruledb.StatusIn(string(types.StatusPublished), string(types.StatusArchived)))
	} else {
		q.Where(ruledb.Status(string(types.StatusPublished)))
	}
	if f.Owner != nil {
		if err := billingOwner(*f.Owner); err != nil {
			return nil, err
		}
		q.Where(ruledb.OwnerType(f.Owner.Type), ruledb.OwnerID(f.Owner.ID))
	}
	if len(f.IDs) > 0 {
		q.Where(ruledb.IDIn(f.IDs...))
	}
	if f.After != nil {
		q.Where(ruledb.Or(ruledb.CreatedAtGT(f.After.CreatedAt), ruledb.And(ruledb.CreatedAtEQ(f.After.CreatedAt), ruledb.IDGT(f.After.ID))))
	}
	rows, err := q.Order(ent.Asc(ruledb.FieldCreatedAt), ent.Asc(ruledb.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return nil, billingPersistenceError(err, "list billing rules")
	}
	result := make([]*domain.Rule, 0, len(rows))
	for _, row := range rows {
		result = append(result, billingRuleFromEnt(row))
	}
	return result, nil
}

func (r *billingTermRepository) Archive(ctx context.Context, owner domain.OwnerRef) error {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return err
	}
	if err := billingOwner(owner); err != nil {
		return err
	}
	_, err = r.client.Writer(ctx).BillingTerm.Update().Where(
		ruledb.TenantID(tenantID), ruledb.EnvironmentID(environmentID),
		ruledb.OwnerType(owner.Type), ruledb.OwnerID(owner.ID), ruledb.Status(string(types.StatusPublished)),
	).SetStatus(string(types.StatusArchived)).SetUpdatedAt(time.Now().UTC()).SetUpdatedBy(types.GetUserID(ctx)).Save(ctx)
	if err != nil {
		return billingPersistenceError(err, "archive billing rules")
	}
	return nil
}

func (r *billingScheduleItemRepository) Create(ctx context.Context, item *domain.ScheduleItem) error {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return err
	}
	if item == nil {
		return ierr.Validation("billing schedule item is required")
	}
	if err := billingModelScope(item.TenantID, item.EnvironmentID, tenantID, environmentID); err != nil {
		return err
	}
	if err := billingIdentifier("schedule item id", item.ID); err != nil {
		return err
	}
	if err := billingOwner(domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}); err != nil {
		return err
	}
	if err := item.Kind.Validate(); err != nil {
		return err
	}
	if err := item.BillTrigger.Validate(); err != nil {
		return err
	}
	if err := item.Delivery.Validate(); err != nil {
		return err
	}
	if item.ScheduleStatus != "" && item.ScheduleStatus != types.BillingScheduleScheduled {
		return ierr.Validation("new schedule items must be scheduled")
	}
	if item.InvoiceID != nil || item.InvoiceLineItemID != nil || item.CancellationReason != nil {
		return ierr.Validation("new schedule items cannot have execution history")
	}
	status, err := billingCreateStatus(item.Status)
	if err != nil {
		return err
	}
	if item.BillingTermID == nil {
		if item.Kind != types.BillingScheduleSettlement {
			return ierr.Validation("planned charges require a billing rule")
		}
	} else {
		if err := billingIdentifier("source billing rule id", *item.BillingTermID); err != nil {
			return err
		}
		exists, err := r.client.Writer(ctx).BillingTerm.Query().Where(
			ruledb.TenantID(tenantID), ruledb.EnvironmentID(environmentID),
			ruledb.ID(*item.BillingTermID), ruledb.OwnerType(item.OwnerType), ruledb.OwnerID(item.OwnerID),
			ruledb.Status(string(types.StatusPublished)),
		).Exist(ctx)
		if err != nil {
			return billingPersistenceError(err, "resolve schedule source")
		}
		if !exists {
			return ierr.NewError("billing rule source not found for owner").Mark(ierr.ErrNotFound)
		}
	}
	if err := r.validateRecipient(ctx, item); err != nil {
		return err
	}
	copy := domain.NewScheduleItemBuilder(item).Build()
	create := r.client.Writer(ctx).BillingScheduleItem.Create().
		SetID(copy.ID).SetTenantID(tenantID).SetEnvironmentID(environmentID).
		SetOwnerType(copy.OwnerType).SetOwnerID(copy.OwnerID).SetNillableBillingTermID(copy.BillingTermID).
		SetCustomerID(copy.CustomerID).SetNillableSubscriptionID(copy.SubscriptionID).SetCurrency(copy.Currency).
		SetKind(copy.Kind).SetBillTrigger(copy.BillTrigger).SetNillableBillOn(copy.BillOn).
		SetAmount(copy.Amount).SetDescription(copy.Description).
		SetServicePeriodStart(copy.ServicePeriodStart).SetServicePeriodEnd(copy.ServicePeriodEnd).
		SetDelivery(copy.Delivery).SetScheduleStatus(types.BillingScheduleScheduled).
		SetMetadata(copy.Metadata).SetStatus(string(status)).
		SetCreatedBy(types.GetUserID(ctx)).SetUpdatedBy(types.GetUserID(ctx))
	if !copy.CreatedAt.IsZero() {
		create.SetCreatedAt(copy.CreatedAt)
	}
	if !copy.UpdatedAt.IsZero() {
		create.SetUpdatedAt(copy.UpdatedAt)
	}
	if _, err := create.Save(ctx); err != nil {
		return billingPersistenceError(err, "create billing schedule item")
	}
	return nil
}

func (r *billingScheduleItemRepository) validateRecipient(ctx context.Context, item *domain.ScheduleItem) error {
	if err := billingIdentifier("schedule customer id", item.CustomerID); err != nil {
		return err
	}
	exists, err := r.client.Writer(ctx).Customer.Query().Where(
		customerdb.ID(item.CustomerID), customerdb.TenantID(item.TenantID), customerdb.EnvironmentID(item.EnvironmentID),
	).Exist(ctx)
	if err != nil {
		return billingPersistenceError(err, "resolve schedule customer")
	}
	if !exists {
		return ierr.NewError("schedule customer not found").Mark(ierr.ErrNotFound)
	}
	if item.SubscriptionID == nil {
		return nil
	}
	if err := billingIdentifier("schedule subscription id", *item.SubscriptionID); err != nil {
		return err
	}
	exists, err = r.client.Writer(ctx).Subscription.Query().Where(
		subscriptiondb.ID(*item.SubscriptionID), subscriptiondb.TenantID(item.TenantID), subscriptiondb.EnvironmentID(item.EnvironmentID),
	).Exist(ctx)
	if err != nil {
		return billingPersistenceError(err, "resolve schedule subscription")
	}
	if !exists {
		return ierr.NewError("schedule subscription not found").Mark(ierr.ErrNotFound)
	}
	return nil
}

func (r *billingScheduleItemRepository) Get(ctx context.Context, id string) (*domain.ScheduleItem, error) {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return nil, err
	}
	if err := billingIdentifier("schedule item id", id); err != nil {
		return nil, err
	}
	row, err := r.client.Reader(ctx).BillingScheduleItem.Query().Where(
		itemdb.TenantID(tenantID), itemdb.EnvironmentID(environmentID), itemdb.ID(id),
		itemdb.StatusIn(string(types.StatusPublished), string(types.StatusArchived)),
	).Only(ctx)
	if err != nil {
		return nil, billingPersistenceError(err, "get billing schedule item")
	}
	return billingItemFromEnt(row), nil
}

func (r *billingScheduleItemRepository) List(ctx context.Context, filter *domain.ScheduleItemFilter) ([]*domain.ScheduleItem, error) {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return nil, err
	}
	f := domain.ScheduleItemFilter{}
	if filter != nil {
		f = *filter
	}
	limit, err := billingListLimit(f.Limit, f.After)
	if err != nil {
		return nil, err
	}
	for label, ids := range map[string][]string{"item ids": f.IDs, "rule ids": f.BillingTermIDs, "invoice ids": f.InvoiceIDs, "subscription ids": f.SubscriptionIDs} {
		if err := billingIdentifiers(label, ids); err != nil {
			return nil, err
		}
	}
	if len(f.ScheduleStatuses) > billingMaximumPage {
		return nil, ierr.Validationf("schedule statuses exceeds %d entries", billingMaximumPage)
	}
	for _, status := range f.ScheduleStatuses {
		if err := status.Validate(); err != nil {
			return nil, err
		}
	}
	q := r.client.Reader(ctx).BillingScheduleItem.Query().Where(itemdb.TenantID(tenantID), itemdb.EnvironmentID(environmentID))
	if f.IncludeArchived {
		q.Where(itemdb.StatusIn(string(types.StatusPublished), string(types.StatusArchived)))
	} else {
		q.Where(itemdb.Status(string(types.StatusPublished)))
	}
	if f.RelevantOnly {
		q.Where(itemdb.Or(itemdb.Status(string(types.StatusPublished)), itemdb.ScheduleStatus(types.BillingScheduleInvoiced)))
	}
	if f.Owner != nil {
		if err := billingOwner(*f.Owner); err != nil {
			return nil, err
		}
		q.Where(itemdb.OwnerType(f.Owner.Type), itemdb.OwnerID(f.Owner.ID))
	}
	if len(f.IDs) > 0 {
		q.Where(itemdb.IDIn(f.IDs...))
	}
	if len(f.BillingTermIDs) > 0 {
		q.Where(itemdb.BillingTermIDIn(f.BillingTermIDs...))
	}
	if len(f.InvoiceIDs) > 0 {
		q.Where(itemdb.InvoiceIDIn(f.InvoiceIDs...))
	}
	if len(f.SubscriptionIDs) > 0 {
		q.Where(itemdb.SubscriptionIDIn(f.SubscriptionIDs...))
	}
	if len(f.ScheduleStatuses) > 0 {
		q.Where(itemdb.ScheduleStatusIn(f.ScheduleStatuses...))
	}
	if f.Delivery != nil {
		if err := f.Delivery.Validate(); err != nil {
			return nil, err
		}
		q.Where(itemdb.DeliveryEQ(*f.Delivery))
	}
	if f.DueBefore != nil {
		if f.DueBefore.IsZero() {
			return nil, ierr.Validation("due_before must be a nonzero date")
		}
		q.Where(itemdb.BillOnNotNil(), itemdb.BillOnLTE(*f.DueBefore))
	}
	if f.UnresolvedManualBefore != nil {
		if f.UnresolvedManualBefore.IsZero() {
			return nil, ierr.Validation("manual fallback cutoff must be a nonzero date")
		}
		q.Where(itemdb.BillTrigger(types.BillingScheduleManual), itemdb.BillOnIsNil(), itemdb.ServicePeriodEndLTE(*f.UnresolvedManualBefore))
	}
	if f.CreatedBefore != nil {
		if f.CreatedBefore.IsZero() {
			return nil, ierr.Validation("created_before must be a nonzero date")
		}
		q.Where(itemdb.CreatedAtLTE(*f.CreatedBefore))
	}
	if f.RecoverableInvoicesOnly {
		q.Where(func(selector *sql.Selector) {
			inv := sql.Table(invoicedb.Table).As("recovery_invoice")
			selector.Where(sql.Exists(sql.Select(inv.C(invoicedb.FieldID)).From(inv).Where(sql.And(
				sql.ColumnsEQ(inv.C(invoicedb.FieldID), selector.C(itemdb.FieldInvoiceID)),
				sql.EQ(inv.C(invoicedb.FieldTenantID), tenantID),
				sql.EQ(inv.C(invoicedb.FieldEnvironmentID), environmentID),
				sql.EQ(inv.C(invoicedb.FieldStatus), string(types.StatusPublished)),
				sql.Or(
					sql.EQ(inv.C(invoicedb.FieldInvoiceStatus), types.InvoiceStatusDraft),
					sql.And(
						sql.EQ(inv.C(invoicedb.FieldInvoiceStatus), types.InvoiceStatusFinalized),
						sql.In(inv.C(invoicedb.FieldPaymentStatus), types.PaymentStatusInitiated, types.PaymentStatusPending, types.PaymentStatusProcessing, types.PaymentStatusFailed),
					),
				),
			))))
		})
	}
	if f.After != nil {
		q.Where(itemdb.Or(itemdb.CreatedAtGT(f.After.CreatedAt), itemdb.And(itemdb.CreatedAtEQ(f.After.CreatedAt), itemdb.IDGT(f.After.ID))))
	}
	rows, err := q.Order(ent.Asc(itemdb.FieldCreatedAt), ent.Asc(itemdb.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return nil, billingPersistenceError(err, "list billing schedule items")
	}
	result := make([]*domain.ScheduleItem, 0, len(rows))
	for _, row := range rows {
		result = append(result, billingItemFromEnt(row))
	}
	return result, nil
}

func (r *billingScheduleItemRepository) ResolveBillOn(ctx context.Context, id string, billOn time.Time) (bool, error) {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return false, err
	}
	if err := billingIdentifier("schedule item id", id); err != nil {
		return false, err
	}
	if billOn.IsZero() {
		return false, ierr.Validation("bill_on must be a nonzero date")
	}
	count, err := r.client.Writer(ctx).BillingScheduleItem.Update().Where(
		itemdb.TenantID(tenantID), itemdb.EnvironmentID(environmentID), itemdb.ID(id),
		itemdb.Status(string(types.StatusPublished)), itemdb.ScheduleStatus(types.BillingScheduleScheduled),
		itemdb.BillTrigger(types.BillingScheduleManual), itemdb.BillOnIsNil(),
		itemdb.InvoiceIDIsNil(), itemdb.InvoiceLineItemIDIsNil(), billingCurrentSource(tenantID, environmentID),
	).SetBillOn(billOn).SetUpdatedAt(time.Now().UTC()).SetUpdatedBy(types.GetUserID(ctx)).Save(ctx)
	if err != nil {
		return false, billingPersistenceError(err, "resolve schedule bill date")
	}
	return count == 1, nil
}

func (r *billingScheduleItemRepository) Claim(ctx context.Context, id, invoiceID, invoiceLineItemID string) (bool, error) {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return false, err
	}
	for label, value := range map[string]string{"schedule item id": id, "invoice id": invoiceID, "invoice line id": invoiceLineItemID} {
		if err := billingIdentifier(label, value); err != nil {
			return false, err
		}
	}
	source := []predicate.InvoiceLineItem{
		linedb.TenantID(tenantID), linedb.EnvironmentID(environmentID), linedb.ID(invoiceLineItemID),
		linedb.InvoiceID(invoiceID), linedb.BillingScheduleItemID(id), linedb.Status(string(types.StatusPublished)),
		linedb.HasInvoiceWith(invoicedb.TenantID(tenantID), invoicedb.EnvironmentID(environmentID), invoicedb.ID(invoiceID), invoicedb.Status(string(types.StatusPublished)), invoicedb.InvoiceStatusIn(types.InvoiceStatusDraft, types.InvoiceStatusFinalized)),
	}
	item, err := r.Get(types.WithForceWriter(ctx), id)
	if err != nil {
		return false, err
	}
	line, err := r.client.Writer(ctx).InvoiceLineItem.Query().Where(source...).WithInvoice(func(q *ent.InvoiceQuery) {
		q.Where(invoicedb.TenantID(tenantID), invoicedb.EnvironmentID(environmentID))
	}).Only(ctx)
	if err != nil {
		return false, billingPersistenceError(err, "resolve schedule invoice source")
	}
	binding := domain.NewScheduleItemBuilder(item).WithInvoiceID(&invoiceID).WithInvoiceLineItemID(&invoiceLineItemID).Build()
	if err := billingValidateBinding(binding, line); err != nil {
		return false, err
	}
	if line.FxConversion != nil || line.Edges.Invoice.FxConversion != nil {
		return false, billingBindingIntegrity("new scheduled claims cannot use currency conversion")
	}
	source = append(source,
		linedb.CustomerID(item.CustomerID), linedb.AmountEQ(item.Amount), linedb.QuantityEQ(decimal.NewFromInt(1)),
		linedb.CurrencyEqualFold(item.Currency), linedb.PeriodStartEQ(item.ServicePeriodStart), linedb.PeriodEndEQ(item.ServicePeriodEnd),
		billingJSONAbsent(linedb.FieldFxConversion, linedb.FieldCustomCurrency),
		linedb.HasInvoiceWith(invoicedb.CustomerID(item.CustomerID), invoicedb.CurrencyEqualFold(item.Currency), billingJSONAbsent(invoicedb.FieldFxConversion, invoicedb.FieldCustomCurrency)),
	)
	if item.SubscriptionID == nil {
		source = append(source, linedb.SubscriptionIDIsNil())
	} else {
		source = append(source, linedb.SubscriptionID(*item.SubscriptionID))
	}
	if item.OwnerType == types.BillingTermOwnerCommitment {
		source = append(source, linedb.CommitmentID(item.OwnerID))
	} else {
		source = append(source, linedb.CommitmentIDIsNil())
	}
	count, err := r.client.Writer(ctx).BillingScheduleItem.Update().Where(
		itemdb.TenantID(tenantID), itemdb.EnvironmentID(environmentID), itemdb.ID(id),
		itemdb.Status(string(types.StatusPublished)), itemdb.ScheduleStatus(types.BillingScheduleScheduled),
		itemdb.InvoiceIDIsNil(), itemdb.InvoiceLineItemIDIsNil(), itemdb.BillOnNotNil(),
		billingCurrentSource(tenantID, environmentID),
		itemdb.HasSourceLineWith(source...),
	).SetInvoiceID(invoiceID).SetInvoiceLineItemID(invoiceLineItemID).SetScheduleStatus(types.BillingScheduleInvoiced).
		SetUpdatedAt(time.Now().UTC()).SetUpdatedBy(types.GetUserID(ctx)).Save(ctx)
	if err != nil {
		return false, billingPersistenceError(err, "claim billing schedule item")
	}
	return count == 1, nil
}

// Ent writes optional JSON pointers as either SQL NULL or JSON null.
func billingJSONAbsent(columns ...string) func(*sql.Selector) {
	return func(selector *sql.Selector) {
		for _, column := range columns {
			selector.Where(sql.Or(sql.IsNull(selector.C(column)), sqljson.ValueIsNull(selector.C(column))))
		}
	}
}

func (r *billingScheduleItemRepository) Cancel(ctx context.Context, id string, reason types.BillingScheduleCancellationReason) (bool, error) {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return false, err
	}
	if err := billingIdentifier("schedule item id", id); err != nil {
		return false, err
	}
	if err := reason.Validate(); err != nil {
		return false, err
	}
	update := r.client.Writer(ctx).BillingScheduleItem.Update().Where(
		itemdb.TenantID(tenantID), itemdb.EnvironmentID(environmentID), itemdb.ID(id),
		itemdb.StatusIn(string(types.StatusPublished), string(types.StatusArchived)),
		itemdb.ScheduleStatusNEQ(types.BillingScheduleCanceled),
	)
	if reason == types.BillingScheduleInvoiceVoided {
		update.Where(itemdb.ScheduleStatus(types.BillingScheduleInvoiced), itemdb.InvoiceIDNotNil(), itemdb.InvoiceLineItemIDNotNil(), billingVoidedInvoiceSource(tenantID, environmentID))
	} else {
		update.Where(itemdb.Status(string(types.StatusPublished)), itemdb.ScheduleStatus(types.BillingScheduleScheduled), itemdb.InvoiceIDIsNil(), itemdb.InvoiceLineItemIDIsNil())
	}
	count, err := update.SetStatus(string(types.StatusArchived)).SetScheduleStatus(types.BillingScheduleCanceled).
		SetCancellationReason(reason).SetUpdatedAt(time.Now().UTC()).SetUpdatedBy(types.GetUserID(ctx)).Save(ctx)
	if err != nil {
		return false, billingPersistenceError(err, "cancel billing schedule item")
	}
	return count == 1, nil
}

func (r *billingScheduleItemRepository) Archive(ctx context.Context, owner domain.OwnerRef) error {
	tenantID, environmentID, err := billingScope(ctx)
	if err != nil {
		return err
	}
	if err := billingOwner(owner); err != nil {
		return err
	}
	_, err = r.client.Writer(ctx).BillingScheduleItem.Update().Where(
		itemdb.TenantID(tenantID), itemdb.EnvironmentID(environmentID),
		itemdb.OwnerType(owner.Type), itemdb.OwnerID(owner.ID), itemdb.Status(string(types.StatusPublished)),
	).SetStatus(string(types.StatusArchived)).SetUpdatedAt(time.Now().UTC()).SetUpdatedBy(types.GetUserID(ctx)).Save(ctx)
	if err != nil {
		return billingPersistenceError(err, "archive billing schedule items")
	}
	return nil
}

func billingCurrentSource(tenantID, environmentID string) predicate.BillingScheduleItem {
	return itemdb.Or(
		itemdb.And(itemdb.Kind(types.BillingScheduleSettlement), itemdb.BillingTermIDIsNil()),
		itemdb.HasBillingTermWith(ruledb.TenantID(tenantID), ruledb.EnvironmentID(environmentID), ruledb.Status(string(types.StatusPublished))),
	)
}

func billingVoidedInvoiceSource(tenantID, environmentID string) predicate.BillingScheduleItem {
	return func(s *sql.Selector) {
		line, invoice := sql.Table(linedb.Table), sql.Table(invoicedb.Table)
		source := sql.Select(line.C(linedb.FieldID)).From(line).
			Join(invoice).On(line.C(linedb.FieldInvoiceID), invoice.C(invoicedb.FieldID)).
			Where(sql.And(
				sql.EQ(line.C(linedb.FieldTenantID), tenantID), sql.EQ(line.C(linedb.FieldEnvironmentID), environmentID),
				sql.EQ(line.C(linedb.FieldStatus), string(types.StatusPublished)),
				sql.ColumnsEQ(line.C(linedb.FieldID), s.C(itemdb.FieldInvoiceLineItemID)),
				sql.ColumnsEQ(line.C(linedb.FieldInvoiceID), s.C(itemdb.FieldInvoiceID)),
				sql.ColumnsEQ(line.C(linedb.FieldBillingScheduleItemID), s.C(itemdb.FieldID)),
				sql.EQ(invoice.C(invoicedb.FieldTenantID), tenantID), sql.EQ(invoice.C(invoicedb.FieldEnvironmentID), environmentID),
				sql.EQ(invoice.C(invoicedb.FieldStatus), string(types.StatusPublished)),
				sql.EQ(invoice.C(invoicedb.FieldInvoiceStatus), types.InvoiceStatusVoided),
			))
		s.Where(sql.Exists(source))
	}
}

func billingScope(ctx context.Context) (string, string, error) {
	if nilBillingContext(ctx) || strings.TrimSpace(types.GetTenantID(ctx)) == "" || strings.TrimSpace(types.GetEnvironmentID(ctx)) == "" {
		return "", "", ierr.Validation("tenant and environment context are required")
	}
	return types.GetTenantID(ctx), types.GetEnvironmentID(ctx), nil
}

func nilBillingContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	value := reflect.ValueOf(ctx)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func billingModelScope(modelTenant, modelEnvironment, tenantID, environmentID string) error {
	if modelTenant != tenantID || modelEnvironment != environmentID {
		return ierr.Validation("billing source tenant/environment does not match context")
	}
	return nil
}

func billingIdentifier(label, value string) error {
	if strings.TrimSpace(value) == "" || len(value) > 50 {
		return ierr.Validationf("%s must contain between 1 and 50 bytes", label)
	}
	return nil
}

func billingIdentifiers(label string, values []string) error {
	if len(values) > billingMaximumPage {
		return ierr.Validationf("%s exceeds %d entries", label, billingMaximumPage)
	}
	for _, value := range values {
		if err := billingIdentifier(label, value); err != nil {
			return err
		}
	}
	return nil
}

func billingOwner(owner domain.OwnerRef) error {
	if err := owner.Type.Validate(); err != nil {
		return err
	}
	return billingIdentifier("owner id", owner.ID)
}

func billingCreateStatus(status types.Status) (types.Status, error) {
	if status == "" {
		return types.StatusPublished, nil
	}
	if status != types.StatusPublished && status != types.StatusArchived {
		return "", ierr.Validation("billing source status must be published or archived")
	}
	return status, nil
}

func billingListLimit(limit int, after *domain.Cursor) (int, error) {
	if limit == 0 {
		limit = billingDefaultPage
	}
	if limit < 1 || limit > billingMaximumPage {
		return 0, ierr.Validationf("billing list limit must be between 1 and %d", billingMaximumPage)
	}
	if after != nil {
		if after.CreatedAt.IsZero() {
			return 0, ierr.Validation("billing cursor requires creation time")
		}
		if err := billingIdentifier("billing cursor id", after.ID); err != nil {
			return 0, err
		}
	}
	return limit, nil
}

func billingPersistenceError(err error, action string) error {
	if ent.IsNotFound(err) {
		return ierr.WithError(err).WithHint("Failed to " + action).Mark(ierr.ErrNotFound)
	}
	var pqError *pq.Error
	if errors.As(err, &pqError) && pqError.Code == "23505" {
		return ierr.WithError(err).WithHint("Billing source already exists").Mark(ierr.ErrAlreadyExists)
	}
	return ierr.WithError(err).WithHint("Failed to " + action).Mark(ierr.ErrDatabase)
}

func billingRuleFromEnt(row *ent.BillingTerm) *domain.Rule {
	if row == nil {
		return nil
	}
	return domain.NewRuleBuilder(&domain.Rule{
		ID: row.ID, OwnerType: row.OwnerType, OwnerID: row.OwnerID,
		Kind: row.Kind, ValueType: row.ValueType, Value: row.Value,
		BillOn: row.BillOn, OnTrigger: row.OnTrigger,
		StartDate: row.StartDate, EndDate: row.EndDate, Interval: row.Interval,
		IntervalCount: row.IntervalCount, BillAt: row.BillAt, Split: row.Split,
		Description: row.Description, Metadata: row.Metadata, EnvironmentID: row.EnvironmentID,
		BaseModel: types.BaseModel{TenantID: row.TenantID, Status: types.Status(row.Status), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, CreatedBy: row.CreatedBy, UpdatedBy: row.UpdatedBy},
	}).Build()
}

func billingItemFromEnt(row *ent.BillingScheduleItem) *domain.ScheduleItem {
	if row == nil {
		return nil
	}
	return domain.NewScheduleItemBuilder(&domain.ScheduleItem{
		ID: row.ID, OwnerType: row.OwnerType, OwnerID: row.OwnerID,
		BillingTermID: row.BillingTermID, CustomerID: row.CustomerID,
		SubscriptionID: row.SubscriptionID, Currency: row.Currency, Kind: row.Kind,
		BillTrigger: row.BillTrigger, BillOn: row.BillOn, Amount: row.Amount,
		Description: row.Description, ServicePeriodStart: row.ServicePeriodStart, ServicePeriodEnd: row.ServicePeriodEnd,
		Delivery: row.Delivery, ScheduleStatus: row.ScheduleStatus, InvoiceID: row.InvoiceID,
		InvoiceLineItemID: row.InvoiceLineItemID, CancellationReason: row.CancellationReason,
		Metadata: row.Metadata, EnvironmentID: row.EnvironmentID,
		BaseModel: types.BaseModel{TenantID: row.TenantID, Status: types.Status(row.Status), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, CreatedBy: row.CreatedBy, UpdatedBy: row.UpdatedBy},
	}).Build()
}
