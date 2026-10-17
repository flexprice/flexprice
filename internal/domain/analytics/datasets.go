package analytics

import (
	"fmt"
	"slices"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type FieldKind string

const (
	FieldKindTime   FieldKind = "time"
	FieldKindString FieldKind = "string"
	FieldKindNumber FieldKind = "number"
	FieldKindBool   FieldKind = "bool"
)

// Report-builder operators absent from search filters.
const (
	FilterOpLTE     types.FilterOperatorType = "lte"
	FilterOpBetween types.FilterOperatorType = "between"
)

const (
	maxDatasetMeasures   = 5
	maxDatasetDimensions = 2
	maxDatasetFilters    = 10
)

const daysToPayExpr = "if(i.payment_status IN ('SUCCEEDED','OVERPAID') AND i.paid_at > toDateTime64(0, 6) AND i.issue_date > toDateTime64(0, 6), dateDiff('day', i.issue_date, i.paid_at, 'UTC'), NULL)"

var measureAggs = []string{"count", "count_distinct", "sum", "avg", "median", "min", "max"}

type DatasetField struct {
	Key       string    `json:"key"`
	Label     string    `json:"label"`
	Kind      FieldKind `json:"kind"`
	Expr      string    `json:"-"`
	LabelExpr string    `json:"-"`
	Money     bool      `json:"money,omitempty"`
	Group     bool      `json:"group"`
	Filter    bool      `json:"filter"`
	Measure   bool      `json:"measure"`
	Enum      []string  `json:"enum,omitempty"`
	Customer  bool      `json:"customer,omitempty"`
}

type DatasetJoin struct {
	Alias string
	Table string
	On    string
	Cols  []string
}

type Dataset struct {
	Key, Label, Description string
	Table, Alias            string
	Usage                   bool
	Joins                   []DatasetJoin
	Fields                  []DatasetField
	MaxRangeDays            int
}

type Measure struct {
	Agg   string `json:"agg"`
	Field string `json:"field,omitempty"`
}

func timeField(key, label, expr string) DatasetField {
	return DatasetField{Key: key, Label: label, Kind: FieldKindTime, Expr: expr}
}

func moneyField(key, label, expr string) DatasetField {
	return DatasetField{Key: key, Label: label, Kind: FieldKindNumber, Expr: expr, Money: true, Filter: true, Measure: true}
}

func numberField(key, label, expr string) DatasetField {
	return DatasetField{Key: key, Label: label, Kind: FieldKindNumber, Expr: expr, Filter: true, Measure: true}
}

func dimField(key, label, expr string, enum ...string) DatasetField {
	return DatasetField{Key: key, Label: label, Kind: FieldKindString, Expr: expr, Group: true, Filter: true, Enum: enum}
}

func labeledField(key, label, expr, labelExpr string) DatasetField {
	f := dimField(key, label, expr)
	f.LabelExpr = labelExpr
	return f
}

func customerField(expr string) DatasetField {
	f := labeledField("customer", "Customer", expr, "c.name")
	f.Customer = true
	return f
}

func countOnlyField(key, label, expr string) DatasetField {
	return DatasetField{Key: key, Label: label, Kind: FieldKindString, Expr: expr, Measure: true}
}

func customerJoin(on string, cols ...string) DatasetJoin {
	return DatasetJoin{Alias: "c", Table: "customers", On: on, Cols: append([]string{"id", "name"}, cols...)}
}

func strs[T ~string](vs ...T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

var datasets = []Dataset{
	{
		Key: "invoices", Label: "Invoices", Description: "One row per invoice",
		Table: "invoices", Alias: "i",
		Joins: []DatasetJoin{
			customerJoin("c.id = i.customer_id", "external_id", "address_country"),
			{Alias: "s", Table: "subscriptions", On: "s.id = i.subscription_id", Cols: []string{"id", "billing_period"}},
		},
		Fields: []DatasetField{
			timeField("billing_period", "Billing period", "coalesce(nullIf(i.period_start, toDateTime64(0, 6)), i.issue_date)"),
			timeField("issue_date", "Issue date", "i.issue_date"),
			timeField("finalized_at", "Finalized at", "i.finalized_at"),
			timeField("due_date", "Due date", "i.due_date"),
			timeField("paid_at", "Paid at", "i.paid_at"),
			moneyField("total", "Total", "i.total"),
			moneyField("amount_due", "Amount due", "i.amount_due"),
			moneyField("amount_paid", "Amount paid", "i.amount_paid"),
			moneyField("amount_remaining", "Amount remaining", "i.amount_remaining"),
			moneyField("total_tax", "Total tax", "i.total_tax"),
			moneyField("total_discount", "Total discount", "i.total_discount"),
			moneyField("total_credits_applied", "Credits applied", "i.total_credits_applied"),
			dimField("invoice_status", "Invoice status", "i.invoice_status", strs(
				types.InvoiceStatusDraft, types.InvoiceStatusFinalized, types.InvoiceStatusVoided, types.InvoiceStatusSkipped)...),
			dimField("payment_status", "Payment status", "i.payment_status", strs(
				types.PaymentStatusInitiated, types.PaymentStatusPending, types.PaymentStatusProcessing,
				types.PaymentStatusSucceeded, types.PaymentStatusOverpaid, types.PaymentStatusFailed,
				types.PaymentStatusRefunded, types.PaymentStatusPartiallyRefunded, types.PaymentStatusVoided)...),
			dimField("invoice_type", "Invoice type", "i.invoice_type", strs(
				types.InvoiceTypeSubscription, types.InvoiceTypeOneOff, types.InvoiceTypeCredit)...),
			dimField("billing_reason", "Billing reason", "i.billing_reason"),
			dimField("currency", "Currency", "i.currency"),
			customerField("i.customer_id"),
			dimField("customer_country", "Customer country", "c.address_country"),
			dimField("subscription_billing_period", "Subscription billing period", "s.billing_period"),
			numberField("days_to_pay", "Days to pay", daysToPayExpr),
			numberField("days_overdue", "Days overdue",
				"if(i.invoice_status = 'FINALIZED' AND i.amount_remaining > 0 AND i.due_date > toDateTime64(0, 6) AND i.due_date < now64(6, 'UTC'), dateDiff('day', i.due_date, now64(6, 'UTC'), 'UTC'), NULL)"),
			dimField("payment_speed", "Payment speed",
				"multiIf("+daysToPayExpr+" IS NULL, NULL, "+daysToPayExpr+" <= 7, '0-7', "+daysToPayExpr+" <= 30, '8-30', '30+')",
				"0-7", "8-30", "30+"),
			countOnlyField("invoice_id", "Invoice", "i.id"),
		},
	},
	{
		Key: "invoice_lines", Label: "Invoice line items", Description: "One row per invoice line item",
		Table: "invoice_line_items", Alias: "l",
		Joins: []DatasetJoin{
			{Alias: "i", Table: "invoices", On: "i.id = l.invoice_id", Cols: []string{"id", "invoice_status", "finalized_at"}},
			customerJoin("c.id = l.customer_id"),
			{Alias: "m", Table: "meters", On: "m.id = l.meter_id", Cols: []string{"id", "name"}},
		},
		Fields: []DatasetField{
			timeField("billing_period", "Billing period", "l.period_start"),
			timeField("finalized_at", "Invoice finalized at", "i.finalized_at"),
			moneyField("amount", "Amount", "l.amount"),
			numberField("quantity", "Quantity", "l.quantity"),
			moneyField("credits_applied", "Credits applied", "l.credits_applied"),
			moneyField("line_item_discount", "Discount", "l.line_item_discount"),
			dimField("price_type", "Price type", "l.price_type", strs(types.PRICE_TYPE_FIXED, types.PRICE_TYPE_USAGE)...),
			dimField("plan", "Plan", "l.plan_display_name"),
			labeledField("meter", "Meter", "l.meter_id", "m.name"),
			dimField("display_name", "Line item", "l.display_name"),
			dimField("entity_type", "Entity type", "l.entity_type"),
			dimField("currency", "Currency", "l.currency"),
			dimField("invoice_status", "Invoice status", "i.invoice_status", strs(
				types.InvoiceStatusDraft, types.InvoiceStatusFinalized, types.InvoiceStatusVoided, types.InvoiceStatusSkipped)...),
			customerField("l.customer_id"),
		},
	},
	{
		Key: "wallet_transactions", Label: "Wallet transactions", Description: "One row per wallet credit or debit",
		Table: "wallet_transactions", Alias: "w",
		Joins: []DatasetJoin{
			{Alias: "wl", Table: "wallets", On: "wl.id = w.wallet_id", Cols: []string{"id", "wallet_type"}},
			customerJoin("c.id = w.customer_id"),
		},
		Fields: []DatasetField{
			timeField("created_at", "Created at", "w.created_at"),
			numberField("credit_amount", "Credit amount", "w.credit_amount"),
			moneyField("amount", "Amount", "w.amount"),
			dimField("type", "Type", "w.type", strs(types.TransactionTypeCredit, types.TransactionTypeDebit)...),
			dimField("transaction_reason", "Reason", "w.transaction_reason"),
			dimField("transaction_status", "Status", "w.transaction_status"),
			dimField("wallet_type", "Wallet type", "wl.wallet_type"),
			dimField("currency", "Currency", "w.currency"),
			customerField("w.customer_id"),
		},
	},
	{
		Key: "credit_notes", Label: "Credit notes", Description: "One row per credit note",
		Table: "credit_notes", Alias: "n",
		Joins: []DatasetJoin{customerJoin("c.id = n.customer_id")},
		Fields: []DatasetField{
			timeField("finalized_at", "Finalized at", "n.finalized_at"),
			timeField("created_at", "Created at", "n.created_at"),
			moneyField("total_amount", "Total amount", "n.total_amount"),
			dimField("credit_note_status", "Status", "n.credit_note_status"),
			dimField("credit_note_type", "Type", "n.credit_note_type"),
			dimField("reason", "Reason", "n.reason"),
			dimField("refund_status", "Refund status", "n.refund_status"),
			dimField("currency", "Currency", "n.currency"),
			customerField("n.customer_id"),
		},
	},
	{
		Key: "subscriptions", Label: "Subscriptions", Description: "One row per subscription",
		Table: "subscriptions", Alias: "s",
		Joins: []DatasetJoin{customerJoin("c.id = s.customer_id")},
		Fields: []DatasetField{
			timeField("start_date", "Start date", "s.start_date"),
			timeField("created_at", "Created at", "s.created_at"),
			timeField("cancelled_at", "Cancelled at", "s.cancelled_at"),
			moneyField("commitment_amount", "Commitment amount", "s.commitment_amount"),
			dimField("subscription_status", "Status", "s.subscription_status"),
			dimField("billing_period", "Billing period", "s.billing_period"),
			dimField("billing_cadence", "Billing cadence", "s.billing_cadence"),
			dimField("collection_method", "Collection method", "s.collection_method"),
			dimField("plan_id", "Plan", "s.plan_id"),
			dimField("currency", "Currency", "s.currency"),
			customerField("s.customer_id"),
			countOnlyField("customer_id", "Customer", "s.customer_id"),
		},
	},
	{
		Key: "customers", Label: "Customers", Description: "One row per customer",
		Table: "customers", Alias: "c",
		Fields: []DatasetField{
			timeField("created_at", "Created at", "c.created_at"),
			dimField("address_country", "Country", "c.address_country"),
			dimField("billing_currency", "Billing currency", "c.billing_currency"),
			dimField("tax_treatment", "Tax treatment", "c.tax_treatment"),
		},
	},
	{
		Key: "usage", Label: "Usage", Description: "Metered usage, at most 7 days per query",
		Table: "meter_usage", Alias: "u", Usage: true, MaxRangeDays: 7,
		Joins: []DatasetJoin{
			{Alias: "m", Table: "meters", On: "m.id = u.meter_id", Cols: []string{"id", "name"}},
			// Customer picker yields internal ids.
			customerJoin("c.external_id = u.external_customer_id", "external_id"),
		},
		Fields: []DatasetField{
			timeField("timestamp", "Timestamp", "u.timestamp"),
			numberField("quantity", "Quantity", "u.qty_total"),
			labeledField("meter", "Meter", "u.meter_id", "m.name"),
			customerField("c.id"),
			dimField("source", "Source", "u.source"),
		},
	},
}

func Datasets() []Dataset { return datasets }

func LookupDataset(key string) (*Dataset, bool) {
	for i := range datasets {
		if datasets[i].Key == key {
			return &datasets[i], true
		}
	}
	return nil, false
}

func (d *Dataset) Field(key string) (*DatasetField, bool) {
	for i := range d.Fields {
		if d.Fields[i].Key == key {
			return &d.Fields[i], true
		}
	}
	return nil, false
}

func invalid(hint, format string, args ...any) error {
	return ierr.NewErrorf(format, args...).WithHint(hint).Mark(ierr.ErrValidation)
}

func DatasetShape(def ViewDefinition) types.Shape {
	switch {
	case def.Time.Grain != "":
		return types.ShapeTimeseries
	case len(def.Dimensions) > 0:
		return types.ShapeBreakdown
	}
	return types.ShapeScalar
}

func ValidateDataset(def ViewDefinition) error {
	ds, ok := LookupDataset(def.Dataset)
	if !ok {
		return invalid("pick a dataset from the catalog", "unknown dataset %q", def.Dataset)
	}
	if err := validateMeasures(ds, def.Measures); err != nil {
		return err
	}
	if err := validateDatasetDimensions(ds, def); err != nil {
		return err
	}
	if err := validateDatasetFilters(ds, def.Filters); err != nil {
		return err
	}
	if err := validateDatasetTime(ds, def.Time); err != nil {
		return err
	}
	switch def.Compare {
	case "":
	case "previous_period", "previous_year":
		if ds.Usage {
			return invalid("compare is not supported for usage", "compare not allowed on dataset %q", ds.Key)
		}
	default:
		return invalid("compare must be previous_period or previous_year", "compare %q not allowed", def.Compare)
	}
	switch def.Display {
	case "", "chart", "table":
	default:
		return invalid("display must be chart or table", "display %q not allowed", def.Display)
	}
	if def.Limit < 0 {
		return invalid("limit must be zero or positive", "invalid limit %d", def.Limit)
	}
	if shape := DatasetShape(def); shape != types.ShapeBreakdown && (len(def.Sort) > 0 || def.Limit > 0) {
		return ierr.NewErrorf("sort and limit are not supported for %s views", shape).
			WithHint("sort and limit apply to breakdown views: group by a dimension without a time grain").
			Mark(ierr.ErrValidation)
	}
	for _, s := range def.Sort {
		if s == nil {
			continue
		}
		if !slices.Contains(def.Dimensions, s.Field) && !isMeasureRef(s.Field, len(def.Measures)) {
			return invalid("sort by a chosen dimension or m0..mN", "invalid sort field %q", s.Field)
		}
		switch s.Dir {
		case "", types.SortDirectionAsc, types.SortDirectionDesc:
		default:
			return invalid("dir must be asc or desc", "invalid sort direction %q", s.Dir)
		}
	}
	return nil
}

func isMeasureRef(field string, n int) bool {
	for i := range n {
		if field == fmt.Sprintf("m%d", i) {
			return true
		}
	}
	return false
}

func validateMeasures(ds *Dataset, measures []*Measure) error {
	if len(measures) == 0 || len(measures) > maxDatasetMeasures {
		return invalid("choose 1 to 5 measures", "got %d measures", len(measures))
	}
	for _, m := range measures {
		if m == nil {
			return invalid("remove empty measures", "empty measure")
		}
		if !slices.Contains(measureAggs, m.Agg) {
			return invalid("agg must be count, count_distinct, sum, avg, median, min or max", "unknown aggregation %q", m.Agg)
		}
		if m.Agg == "count" {
			if m.Field != "" {
				return invalid("count takes no field", "count with field %q", m.Field)
			}
			continue
		}
		f, ok := ds.Field(m.Field)
		if !ok {
			return invalid("pick a field from the dataset", "unknown measure field %q", m.Field)
		}
		if m.Agg == "count_distinct" {
			if !f.Measure && !f.Group {
				return invalid("count_distinct needs a measure or dimension field", "field %q cannot be counted", m.Field)
			}
			continue
		}
		if !f.Measure || f.Kind != FieldKindNumber {
			return invalid("sum, avg, median, min and max need a numeric field", "%s not allowed on field %q", m.Agg, m.Field)
		}
	}
	return nil
}

func validateDatasetDimensions(ds *Dataset, def ViewDefinition) error {
	limit := maxDatasetDimensions
	if def.Time.Grain != "" {
		limit = 1
	}
	if len(def.Dimensions) > limit {
		return invalid("use at most 2 dimensions, or 1 with a time grain", "too many dimensions: %d", len(def.Dimensions))
	}
	for i, d := range def.Dimensions {
		f, ok := ds.Field(d)
		if !ok || !f.Group {
			return invalid("pick a groupable field from the dataset", "invalid dimension %q", d)
		}
		if slices.Contains(def.Dimensions[:i], d) {
			return invalid("remove the repeated dimension", "duplicate dimension %q", d)
		}
	}
	return nil
}

func filterOpsFor(kind FieldKind) []types.FilterOperatorType {
	switch kind {
	case FieldKindNumber:
		return []types.FilterOperatorType{types.EQUAL, types.IN, types.NOT_IN, types.GREATER_THAN, types.GREATER_THAN_EQUAL, types.LESS_THAN, FilterOpLTE, FilterOpBetween}
	case FieldKindBool:
		return []types.FilterOperatorType{types.EQUAL}
	case FieldKindString:
		return []types.FilterOperatorType{types.EQUAL, types.IN, types.NOT_IN}
	}
	return nil
}

func validateDatasetFilters(ds *Dataset, filters []*Filter) error {
	if len(filters) > maxDatasetFilters {
		return invalid("use at most 10 filters", "too many filters: %d", len(filters))
	}
	for _, flt := range filters {
		if flt == nil {
			continue
		}
		f, ok := ds.Field(flt.Field)
		if !ok || !f.Filter {
			return invalid("pick a filterable field from the dataset", "invalid filter field %q", flt.Field)
		}
		if !slices.Contains(filterOpsFor(f.Kind), flt.Op) {
			return invalid(fmt.Sprintf("allowed operators: %v", filterOpsFor(f.Kind)), "operator %q not allowed on field %q", flt.Op, flt.Field)
		}
		n := len(flt.Value)
		switch flt.Op {
		case types.IN, types.NOT_IN:
			ok = n >= 1
		case FilterOpBetween:
			ok = n == 2
		default:
			ok = n == 1
		}
		if !ok {
			return invalid("in/not_in take 1+ values, between 2, others 1", "wrong value count %d for %q", n, flt.Op)
		}
		if flt.Op == FilterOpBetween && f.Kind == FieldKindNumber {
			lo, lerr := decimal.NewFromString(flt.Value[0])
			hi, herr := decimal.NewFromString(flt.Value[1])
			if lerr == nil && herr == nil && lo.GreaterThan(hi) {
				return invalid("between needs low then high", "between range reversed on field %q", flt.Field)
			}
		}
		for _, v := range flt.Value {
			if f.Kind == FieldKindNumber {
				if _, err := decimal.NewFromString(v); err != nil {
					return invalid("numeric filters take decimal values", "invalid number %q for field %q", v, flt.Field)
				}
			}
			if f.Kind == FieldKindBool && v != "true" && v != "false" {
				return invalid("value must be true or false", "invalid bool %q for field %q", v, flt.Field)
			}
			if len(f.Enum) > 0 && !slices.Contains(f.Enum, v) {
				return invalid(fmt.Sprintf("allowed values: %v", f.Enum), "invalid value %q for field %q", v, flt.Field)
			}
		}
	}
	return nil
}

func validateDatasetTime(ds *Dataset, t TimeSpecRaw) error {
	f, ok := ds.Field(t.Field)
	if !ok || f.Kind != FieldKindTime {
		return invalid("time.field must be a time field of the dataset", "invalid time field %q", t.Field)
	}
	days, err := ReportRangeMaxDays(t.Range)
	if err != nil {
		return err
	}
	if ds.MaxRangeDays > 0 && days > ds.MaxRangeDays {
		return invalid(fmt.Sprintf("dataset %s allows at most %d days", ds.Key, ds.MaxRangeDays), "range %q too long", t.Range)
	}
	switch t.Grain {
	case "", types.GrainDay, types.GrainWeek, types.GrainMonth:
		return nil
	}
	return invalid("grain must be day, week or month", "grain %q not allowed", t.Grain)
}
