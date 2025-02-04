// Code generated by ent, DO NOT EDIT.

package migrate

import (
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/dialect/sql/schema"
	"entgo.io/ent/schema/field"
)

var (
	// BillingSequencesColumns holds the columns for the "billing_sequences" table.
	BillingSequencesColumns = []*schema.Column{
		{Name: "id", Type: field.TypeInt, Increment: true},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "subscription_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "last_sequence", Type: field.TypeInt, Default: 0, SchemaType: map[string]string{"postgres": "integer"}},
		{Name: "created_at", Type: field.TypeTime, SchemaType: map[string]string{"postgres": "timestamp"}},
		{Name: "updated_at", Type: field.TypeTime, SchemaType: map[string]string{"postgres": "timestamp"}},
	}
	// BillingSequencesTable holds the schema information for the "billing_sequences" table.
	BillingSequencesTable = &schema.Table{
		Name:       "billing_sequences",
		Columns:    BillingSequencesColumns,
		PrimaryKey: []*schema.Column{BillingSequencesColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "billingsequence_tenant_id_subscription_id",
				Unique:  true,
				Columns: []*schema.Column{BillingSequencesColumns[1], BillingSequencesColumns[2]},
			},
		},
	}
	// CustomersColumns holds the columns for the "customers" table.
	CustomersColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "external_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "name", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "email", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "address_line1", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "address_line2", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "address_city", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(100)"}},
		{Name: "address_state", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(100)"}},
		{Name: "address_postal_code", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "address_country", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(2)"}},
		{Name: "metadata", Type: field.TypeJSON, Nullable: true},
	}
	// CustomersTable holds the schema information for the "customers" table.
	CustomersTable = &schema.Table{
		Name:       "customers",
		Columns:    CustomersColumns,
		PrimaryKey: []*schema.Column{CustomersColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "customer_tenant_id_external_id",
				Unique:  true,
				Columns: []*schema.Column{CustomersColumns[1], CustomersColumns[7]},
				Annotation: &entsql.IndexAnnotation{
					Where: "status != 'deleted' AND external_id != ''",
				},
			},
			{
				Name:    "customer_tenant_id",
				Unique:  false,
				Columns: []*schema.Column{CustomersColumns[1]},
			},
		},
	}
	// FeaturesColumns holds the columns for the "features" table.
	FeaturesColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "lookup_key", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "name", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "description", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "type", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "meter_id", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "metadata", Type: field.TypeJSON, Nullable: true, SchemaType: map[string]string{"postgres": "jsonb"}},
		{Name: "unit_singular", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "unit_plural", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
	}
	// FeaturesTable holds the schema information for the "features" table.
	FeaturesTable = &schema.Table{
		Name:       "features",
		Columns:    FeaturesColumns,
		PrimaryKey: []*schema.Column{FeaturesColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "idx_tenant_lookup_key_unique",
				Unique:  true,
				Columns: []*schema.Column{FeaturesColumns[1], FeaturesColumns[7]},
				Annotation: &entsql.IndexAnnotation{
					Where: "lookup_key IS NOT NULL AND status = 'published'",
				},
			},
			{
				Name:    "idx_tenant_meter_id",
				Unique:  false,
				Columns: []*schema.Column{FeaturesColumns[1], FeaturesColumns[11]},
				Annotation: &entsql.IndexAnnotation{
					Where: "meter_id IS NOT NULL",
				},
			},
			{
				Name:    "idx_tenant_type",
				Unique:  false,
				Columns: []*schema.Column{FeaturesColumns[1], FeaturesColumns[10]},
			},
			{
				Name:    "idx_tenant_status",
				Unique:  false,
				Columns: []*schema.Column{FeaturesColumns[1], FeaturesColumns[2]},
			},
			{
				Name:    "idx_tenant_created_at",
				Unique:  false,
				Columns: []*schema.Column{FeaturesColumns[1], FeaturesColumns[3]},
			},
		},
	}
	// InvoicesColumns holds the columns for the "invoices" table.
	InvoicesColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "customer_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "subscription_id", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "invoice_type", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "invoice_status", Type: field.TypeString, Default: "DRAFT", SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "payment_status", Type: field.TypeString, Default: "PENDING", SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "currency", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(10)"}},
		{Name: "amount_due", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,8)"}},
		{Name: "amount_paid", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,8)"}},
		{Name: "amount_remaining", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,8)"}},
		{Name: "description", Type: field.TypeString, Nullable: true},
		{Name: "due_date", Type: field.TypeTime, Nullable: true},
		{Name: "paid_at", Type: field.TypeTime, Nullable: true},
		{Name: "voided_at", Type: field.TypeTime, Nullable: true},
		{Name: "finalized_at", Type: field.TypeTime, Nullable: true},
		{Name: "billing_period", Type: field.TypeString, Nullable: true},
		{Name: "period_start", Type: field.TypeTime, Nullable: true},
		{Name: "period_end", Type: field.TypeTime, Nullable: true},
		{Name: "invoice_pdf_url", Type: field.TypeString, Nullable: true},
		{Name: "billing_reason", Type: field.TypeString, Nullable: true},
		{Name: "metadata", Type: field.TypeJSON, Nullable: true, SchemaType: map[string]string{"postgres": "jsonb"}},
		{Name: "version", Type: field.TypeInt, Default: 1},
		{Name: "invoice_number", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "billing_sequence", Type: field.TypeInt, Nullable: true, SchemaType: map[string]string{"postgres": "integer"}},
		{Name: "idempotency_key", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(100)"}},
	}
	// InvoicesTable holds the schema information for the "invoices" table.
	InvoicesTable = &schema.Table{
		Name:       "invoices",
		Columns:    InvoicesColumns,
		PrimaryKey: []*schema.Column{InvoicesColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "idx_tenant_customer_status",
				Unique:  false,
				Columns: []*schema.Column{InvoicesColumns[1], InvoicesColumns[7], InvoicesColumns[10], InvoicesColumns[11], InvoicesColumns[2]},
			},
			{
				Name:    "idx_tenant_subscription_status",
				Unique:  false,
				Columns: []*schema.Column{InvoicesColumns[1], InvoicesColumns[8], InvoicesColumns[10], InvoicesColumns[11], InvoicesColumns[2]},
			},
			{
				Name:    "idx_tenant_type_status",
				Unique:  false,
				Columns: []*schema.Column{InvoicesColumns[1], InvoicesColumns[9], InvoicesColumns[10], InvoicesColumns[11], InvoicesColumns[2]},
			},
			{
				Name:    "idx_tenant_due_date_status",
				Unique:  false,
				Columns: []*schema.Column{InvoicesColumns[1], InvoicesColumns[17], InvoicesColumns[10], InvoicesColumns[11], InvoicesColumns[2]},
			},
			{
				Name:    "idx_tenant_invoice_number_unique",
				Unique:  true,
				Columns: []*schema.Column{InvoicesColumns[1], InvoicesColumns[28]},
			},
			{
				Name:    "idx_idempotency_key_unique",
				Unique:  true,
				Columns: []*schema.Column{InvoicesColumns[30]},
				Annotation: &entsql.IndexAnnotation{
					Where: "idempotency_key IS NOT NULL",
				},
			},
			{
				Name:    "idx_subscription_period_unique",
				Unique:  false,
				Columns: []*schema.Column{InvoicesColumns[8], InvoicesColumns[22], InvoicesColumns[23]},
				Annotation: &entsql.IndexAnnotation{
					Where: "invoice_status != 'VOIDED' AND subscription_id IS NOT NULL",
				},
			},
		},
	}
	// InvoiceLineItemsColumns holds the columns for the "invoice_line_items" table.
	InvoiceLineItemsColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "customer_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "subscription_id", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "plan_id", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "plan_display_name", Type: field.TypeString, Nullable: true},
		{Name: "price_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "price_type", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "meter_id", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "meter_display_name", Type: field.TypeString, Nullable: true},
		{Name: "display_name", Type: field.TypeString, Nullable: true},
		{Name: "amount", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,8)"}},
		{Name: "quantity", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,8)"}},
		{Name: "currency", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(10)"}},
		{Name: "period_start", Type: field.TypeTime, Nullable: true},
		{Name: "period_end", Type: field.TypeTime, Nullable: true},
		{Name: "metadata", Type: field.TypeJSON, Nullable: true, SchemaType: map[string]string{"postgres": "jsonb"}},
		{Name: "invoice_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
	}
	// InvoiceLineItemsTable holds the schema information for the "invoice_line_items" table.
	InvoiceLineItemsTable = &schema.Table{
		Name:       "invoice_line_items",
		Columns:    InvoiceLineItemsColumns,
		PrimaryKey: []*schema.Column{InvoiceLineItemsColumns[0]},
		ForeignKeys: []*schema.ForeignKey{
			{
				Symbol:     "invoice_line_items_invoices_line_items",
				Columns:    []*schema.Column{InvoiceLineItemsColumns[22]},
				RefColumns: []*schema.Column{InvoicesColumns[0]},
				OnDelete:   schema.NoAction,
			},
		},
		Indexes: []*schema.Index{
			{
				Name:    "invoicelineitem_tenant_id_invoice_id_status",
				Unique:  false,
				Columns: []*schema.Column{InvoiceLineItemsColumns[1], InvoiceLineItemsColumns[22], InvoiceLineItemsColumns[2]},
			},
			{
				Name:    "invoicelineitem_tenant_id_customer_id_status",
				Unique:  false,
				Columns: []*schema.Column{InvoiceLineItemsColumns[1], InvoiceLineItemsColumns[7], InvoiceLineItemsColumns[2]},
			},
			{
				Name:    "invoicelineitem_tenant_id_subscription_id_status",
				Unique:  false,
				Columns: []*schema.Column{InvoiceLineItemsColumns[1], InvoiceLineItemsColumns[8], InvoiceLineItemsColumns[2]},
			},
			{
				Name:    "invoicelineitem_tenant_id_price_id_status",
				Unique:  false,
				Columns: []*schema.Column{InvoiceLineItemsColumns[1], InvoiceLineItemsColumns[11], InvoiceLineItemsColumns[2]},
			},
			{
				Name:    "invoicelineitem_tenant_id_meter_id_status",
				Unique:  false,
				Columns: []*schema.Column{InvoiceLineItemsColumns[1], InvoiceLineItemsColumns[13], InvoiceLineItemsColumns[2]},
			},
			{
				Name:    "invoicelineitem_period_start_period_end",
				Unique:  false,
				Columns: []*schema.Column{InvoiceLineItemsColumns[19], InvoiceLineItemsColumns[20]},
			},
		},
	}
	// InvoiceSequencesColumns holds the columns for the "invoice_sequences" table.
	InvoiceSequencesColumns = []*schema.Column{
		{Name: "id", Type: field.TypeInt, Increment: true},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "year_month", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(6)"}},
		{Name: "last_value", Type: field.TypeInt64, Default: 0, SchemaType: map[string]string{"postgres": "bigint"}},
		{Name: "created_at", Type: field.TypeTime, SchemaType: map[string]string{"postgres": "timestamp"}},
		{Name: "updated_at", Type: field.TypeTime, SchemaType: map[string]string{"postgres": "timestamp"}},
	}
	// InvoiceSequencesTable holds the schema information for the "invoice_sequences" table.
	InvoiceSequencesTable = &schema.Table{
		Name:       "invoice_sequences",
		Columns:    InvoiceSequencesColumns,
		PrimaryKey: []*schema.Column{InvoiceSequencesColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "invoicesequence_tenant_id_year_month",
				Unique:  true,
				Columns: []*schema.Column{InvoiceSequencesColumns[1], InvoiceSequencesColumns[2]},
			},
		},
	}
	// MetersColumns holds the columns for the "meters" table.
	MetersColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "event_name", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "name", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "aggregation", Type: field.TypeJSON},
		{Name: "filters", Type: field.TypeJSON},
		{Name: "reset_usage", Type: field.TypeString, Default: "BILLING_PERIOD", SchemaType: map[string]string{"postgres": "varchar(20)"}},
	}
	// MetersTable holds the schema information for the "meters" table.
	MetersTable = &schema.Table{
		Name:       "meters",
		Columns:    MetersColumns,
		PrimaryKey: []*schema.Column{MetersColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "meter_tenant_id",
				Unique:  false,
				Columns: []*schema.Column{MetersColumns[1]},
			},
		},
	}
	// PlansColumns holds the columns for the "plans" table.
	PlansColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "lookup_key", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "name", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "description", Type: field.TypeString, Nullable: true, Size: 2147483647},
		{Name: "invoice_cadence", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "trial_period", Type: field.TypeInt, Default: 0},
	}
	// PlansTable holds the schema information for the "plans" table.
	PlansTable = &schema.Table{
		Name:       "plans",
		Columns:    PlansColumns,
		PrimaryKey: []*schema.Column{PlansColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "plan_tenant_id_lookup_key",
				Unique:  true,
				Columns: []*schema.Column{PlansColumns[1], PlansColumns[7]},
				Annotation: &entsql.IndexAnnotation{
					Where: "status != 'deleted' AND lookup_key IS NOT NULL AND lookup_key != ''",
				},
			},
			{
				Name:    "plan_tenant_id",
				Unique:  false,
				Columns: []*schema.Column{PlansColumns[1]},
			},
		},
	}
	// PricesColumns holds the columns for the "prices" table.
	PricesColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "amount", Type: field.TypeFloat64, SchemaType: map[string]string{"postgres": "numeric(25,15)"}},
		{Name: "currency", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(3)"}},
		{Name: "display_amount", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "plan_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "type", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "billing_period", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "billing_period_count", Type: field.TypeInt},
		{Name: "billing_model", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "billing_cadence", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "meter_id", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "filter_values", Type: field.TypeJSON, Nullable: true},
		{Name: "tier_mode", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "tiers", Type: field.TypeJSON, Nullable: true},
		{Name: "transform_quantity", Type: field.TypeJSON, Nullable: true},
		{Name: "lookup_key", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "description", Type: field.TypeString, Nullable: true, Size: 2147483647},
		{Name: "metadata", Type: field.TypeJSON, Nullable: true},
	}
	// PricesTable holds the schema information for the "prices" table.
	PricesTable = &schema.Table{
		Name:       "prices",
		Columns:    PricesColumns,
		PrimaryKey: []*schema.Column{PricesColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "price_tenant_id_lookup_key",
				Unique:  true,
				Columns: []*schema.Column{PricesColumns[1], PricesColumns[21]},
				Annotation: &entsql.IndexAnnotation{
					Where: "status != 'deleted' AND lookup_key IS NOT NULL AND lookup_key != ''",
				},
			},
			{
				Name:    "price_tenant_id_plan_id",
				Unique:  false,
				Columns: []*schema.Column{PricesColumns[1], PricesColumns[10]},
			},
			{
				Name:    "price_tenant_id",
				Unique:  false,
				Columns: []*schema.Column{PricesColumns[1]},
			},
		},
	}
	// SubscriptionsColumns holds the columns for the "subscriptions" table.
	SubscriptionsColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "lookup_key", Type: field.TypeString, Nullable: true},
		{Name: "customer_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "plan_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "subscription_status", Type: field.TypeString, Default: "active", SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "currency", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(10)"}},
		{Name: "billing_anchor", Type: field.TypeTime},
		{Name: "start_date", Type: field.TypeTime},
		{Name: "end_date", Type: field.TypeTime, Nullable: true},
		{Name: "current_period_start", Type: field.TypeTime},
		{Name: "current_period_end", Type: field.TypeTime},
		{Name: "cancelled_at", Type: field.TypeTime, Nullable: true},
		{Name: "cancel_at", Type: field.TypeTime, Nullable: true},
		{Name: "cancel_at_period_end", Type: field.TypeBool, Default: false},
		{Name: "trial_start", Type: field.TypeTime, Nullable: true},
		{Name: "trial_end", Type: field.TypeTime, Nullable: true},
		{Name: "invoice_cadence", Type: field.TypeString},
		{Name: "billing_cadence", Type: field.TypeString},
		{Name: "billing_period", Type: field.TypeString},
		{Name: "billing_period_count", Type: field.TypeInt, Default: 1},
		{Name: "version", Type: field.TypeInt, Default: 1},
		{Name: "metadata", Type: field.TypeJSON, Nullable: true, SchemaType: map[string]string{"postgres": "jsonb"}},
	}
	// SubscriptionsTable holds the schema information for the "subscriptions" table.
	SubscriptionsTable = &schema.Table{
		Name:       "subscriptions",
		Columns:    SubscriptionsColumns,
		PrimaryKey: []*schema.Column{SubscriptionsColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "subscription_tenant_id_customer_id_status",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionsColumns[1], SubscriptionsColumns[8], SubscriptionsColumns[2]},
			},
			{
				Name:    "subscription_tenant_id_plan_id_status",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionsColumns[1], SubscriptionsColumns[9], SubscriptionsColumns[2]},
			},
			{
				Name:    "subscription_tenant_id_subscription_status_status",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionsColumns[1], SubscriptionsColumns[10], SubscriptionsColumns[2]},
			},
			{
				Name:    "subscription_tenant_id_current_period_end_subscription_status_status",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionsColumns[1], SubscriptionsColumns[16], SubscriptionsColumns[10], SubscriptionsColumns[2]},
			},
		},
	}
	// SubscriptionLineItemsColumns holds the columns for the "subscription_line_items" table.
	SubscriptionLineItemsColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "customer_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "plan_id", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "plan_display_name", Type: field.TypeString, Nullable: true},
		{Name: "price_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "price_type", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "meter_id", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "meter_display_name", Type: field.TypeString, Nullable: true},
		{Name: "display_name", Type: field.TypeString, Nullable: true},
		{Name: "quantity", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,8)"}},
		{Name: "currency", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(10)"}},
		{Name: "billing_period", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "start_date", Type: field.TypeTime, Nullable: true},
		{Name: "end_date", Type: field.TypeTime, Nullable: true},
		{Name: "metadata", Type: field.TypeJSON, Nullable: true, SchemaType: map[string]string{"postgres": "jsonb"}},
		{Name: "subscription_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
	}
	// SubscriptionLineItemsTable holds the schema information for the "subscription_line_items" table.
	SubscriptionLineItemsTable = &schema.Table{
		Name:       "subscription_line_items",
		Columns:    SubscriptionLineItemsColumns,
		PrimaryKey: []*schema.Column{SubscriptionLineItemsColumns[0]},
		ForeignKeys: []*schema.ForeignKey{
			{
				Symbol:     "subscription_line_items_subscriptions_line_items",
				Columns:    []*schema.Column{SubscriptionLineItemsColumns[21]},
				RefColumns: []*schema.Column{SubscriptionsColumns[0]},
				OnDelete:   schema.NoAction,
			},
		},
		Indexes: []*schema.Index{
			{
				Name:    "subscriptionlineitem_tenant_id_subscription_id_status",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionLineItemsColumns[1], SubscriptionLineItemsColumns[21], SubscriptionLineItemsColumns[2]},
			},
			{
				Name:    "subscriptionlineitem_tenant_id_customer_id_status",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionLineItemsColumns[1], SubscriptionLineItemsColumns[7], SubscriptionLineItemsColumns[2]},
			},
			{
				Name:    "subscriptionlineitem_tenant_id_plan_id_status",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionLineItemsColumns[1], SubscriptionLineItemsColumns[8], SubscriptionLineItemsColumns[2]},
			},
			{
				Name:    "subscriptionlineitem_tenant_id_price_id_status",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionLineItemsColumns[1], SubscriptionLineItemsColumns[10], SubscriptionLineItemsColumns[2]},
			},
			{
				Name:    "subscriptionlineitem_tenant_id_meter_id_status",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionLineItemsColumns[1], SubscriptionLineItemsColumns[12], SubscriptionLineItemsColumns[2]},
			},
			{
				Name:    "subscriptionlineitem_start_date_end_date",
				Unique:  false,
				Columns: []*schema.Column{SubscriptionLineItemsColumns[18], SubscriptionLineItemsColumns[19]},
			},
		},
	}
	// WalletsColumns holds the columns for the "wallets" table.
	WalletsColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "name", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(255)"}},
		{Name: "customer_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "currency", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(10)"}},
		{Name: "description", Type: field.TypeString, Nullable: true},
		{Name: "metadata", Type: field.TypeJSON, Nullable: true, SchemaType: map[string]string{"postgres": "jsonb"}},
		{Name: "balance", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,9)"}},
		{Name: "wallet_status", Type: field.TypeString, Default: "active", SchemaType: map[string]string{"postgres": "varchar(50)"}},
	}
	// WalletsTable holds the schema information for the "wallets" table.
	WalletsTable = &schema.Table{
		Name:       "wallets",
		Columns:    WalletsColumns,
		PrimaryKey: []*schema.Column{WalletsColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "wallet_tenant_id_customer_id_status",
				Unique:  false,
				Columns: []*schema.Column{WalletsColumns[1], WalletsColumns[8], WalletsColumns[2]},
			},
			{
				Name:    "wallet_tenant_id_status_wallet_status",
				Unique:  false,
				Columns: []*schema.Column{WalletsColumns[1], WalletsColumns[2], WalletsColumns[13]},
			},
		},
	}
	// WalletTransactionsColumns holds the columns for the "wallet_transactions" table.
	WalletTransactionsColumns = []*schema.Column{
		{Name: "id", Type: field.TypeString, Unique: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "tenant_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "status", Type: field.TypeString, Default: "published", SchemaType: map[string]string{"postgres": "varchar(20)"}},
		{Name: "created_at", Type: field.TypeTime},
		{Name: "updated_at", Type: field.TypeTime},
		{Name: "created_by", Type: field.TypeString, Nullable: true},
		{Name: "updated_by", Type: field.TypeString, Nullable: true},
		{Name: "wallet_id", Type: field.TypeString, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "type", Type: field.TypeString, Default: "credit"},
		{Name: "amount", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,9)"}},
		{Name: "balance_before", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,9)"}},
		{Name: "balance_after", Type: field.TypeOther, SchemaType: map[string]string{"postgres": "numeric(20,9)"}},
		{Name: "reference_type", Type: field.TypeString, Nullable: true, SchemaType: map[string]string{"postgres": "varchar(50)"}},
		{Name: "reference_id", Type: field.TypeString, Nullable: true},
		{Name: "description", Type: field.TypeString, Nullable: true},
		{Name: "metadata", Type: field.TypeJSON, Nullable: true, SchemaType: map[string]string{"postgres": "jsonb"}},
		{Name: "transaction_status", Type: field.TypeString, Default: "pending", SchemaType: map[string]string{"postgres": "varchar(50)"}},
	}
	// WalletTransactionsTable holds the schema information for the "wallet_transactions" table.
	WalletTransactionsTable = &schema.Table{
		Name:       "wallet_transactions",
		Columns:    WalletTransactionsColumns,
		PrimaryKey: []*schema.Column{WalletTransactionsColumns[0]},
		Indexes: []*schema.Index{
			{
				Name:    "wallettransaction_tenant_id_wallet_id_status",
				Unique:  false,
				Columns: []*schema.Column{WalletTransactionsColumns[1], WalletTransactionsColumns[7], WalletTransactionsColumns[2]},
			},
			{
				Name:    "wallettransaction_tenant_id_reference_type_reference_id_status",
				Unique:  false,
				Columns: []*schema.Column{WalletTransactionsColumns[1], WalletTransactionsColumns[12], WalletTransactionsColumns[13], WalletTransactionsColumns[2]},
			},
			{
				Name:    "wallettransaction_created_at",
				Unique:  false,
				Columns: []*schema.Column{WalletTransactionsColumns[3]},
			},
		},
	}
	// Tables holds all the tables in the schema.
	Tables = []*schema.Table{
		BillingSequencesTable,
		CustomersTable,
		FeaturesTable,
		InvoicesTable,
		InvoiceLineItemsTable,
		InvoiceSequencesTable,
		MetersTable,
		PlansTable,
		PricesTable,
		SubscriptionsTable,
		SubscriptionLineItemsTable,
		WalletsTable,
		WalletTransactionsTable,
	}
)

func init() {
	InvoiceLineItemsTable.ForeignKeys[0].RefTable = InvoicesTable
	SubscriptionLineItemsTable.ForeignKeys[0].RefTable = SubscriptionsTable
}
