package types

type BillingTermOwnerType string

const (
	BillingTermOwnerCommitment BillingTermOwnerType = "commitment"
)

func (o BillingTermOwnerType) Validate() error {
	return validateEnum("owner_type", o, []BillingTermOwnerType{
		BillingTermOwnerCommitment,
	})
}

type BillingTermKind string

const (
	BillingTermKindOnce      BillingTermKind = "once"
	BillingTermKindRecurring BillingTermKind = "recurring"
)

func (k BillingTermKind) Validate() error {
	return validateEnum("kind", k, []BillingTermKind{
		BillingTermKindOnce,
		BillingTermKindRecurring,
	})
}

type BillingTermValueType string

const (
	BillingTermValueFixed BillingTermValueType = "fixed"
	// BillingTermValuePercentage is accepted on requests only and stored as a fixed amount.
	BillingTermValuePercentage BillingTermValueType = "percentage"
	BillingTermValueRemainder  BillingTermValueType = "remainder"
)

func (v BillingTermValueType) Validate() error {
	return validateEnum("value_type", v, []BillingTermValueType{
		BillingTermValueFixed,
		BillingTermValuePercentage,
		BillingTermValueRemainder,
	})
}

type BillingTermSplit string

const (
	BillingTermSplitEven         BillingTermSplit = "even"
	BillingTermSplitProportional BillingTermSplit = "proportional"
)

func (s BillingTermSplit) Validate() error {
	return validateEnum("split", s, []BillingTermSplit{
		BillingTermSplitEven,
		BillingTermSplitProportional,
	})
}

type InstalmentBillTrigger string

const (
	InstalmentBillTriggerDate   InstalmentBillTrigger = "date"
	InstalmentBillTriggerManual InstalmentBillTrigger = "manual"
)

func (t InstalmentBillTrigger) Validate() error {
	return validateEnum("bill_trigger", t, []InstalmentBillTrigger{
		InstalmentBillTriggerDate,
		InstalmentBillTriggerManual,
	})
}

type InstalmentKind string

const (
	InstalmentKindInstalment InstalmentKind = "instalment"
	InstalmentKindSettlement InstalmentKind = "settlement"
)

func (k InstalmentKind) Validate() error {
	return validateEnum("instalment_kind", k, []InstalmentKind{
		InstalmentKindInstalment,
		InstalmentKindSettlement,
	})
}

type InstalmentStatus string

const (
	InstalmentStatusScheduled InstalmentStatus = "scheduled"
	InstalmentStatusInvoiced  InstalmentStatus = "invoiced"
	InstalmentStatusCancelled InstalmentStatus = "cancelled"
)

func (s InstalmentStatus) Validate() error {
	return validateEnum("instalment_status", s, []InstalmentStatus{
		InstalmentStatusScheduled,
		InstalmentStatusInvoiced,
		InstalmentStatusCancelled,
	})
}

type InstalmentDelivery string

const (
	InstalmentDeliverySubscriptionInvoice InstalmentDelivery = "subscription_invoice"
	InstalmentDeliverySeparateInvoice     InstalmentDelivery = "separate_invoice"
)

func (d InstalmentDelivery) Validate() error {
	return validateEnum("instalment_delivery", d, []InstalmentDelivery{
		InstalmentDeliverySubscriptionInvoice,
		InstalmentDeliverySeparateInvoice,
	})
}

type CancellationFeeType string

const (
	CancellationFeeFixed      CancellationFeeType = "fixed"
	CancellationFeePercentage CancellationFeeType = "percentage"
)

func (f CancellationFeeType) Validate() error {
	return validateEnum("cancellation_fee_type", f, []CancellationFeeType{
		CancellationFeeFixed,
		CancellationFeePercentage,
	})
}
