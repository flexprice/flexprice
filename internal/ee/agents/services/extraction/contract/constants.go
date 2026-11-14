package contract

const (
	maxConfidence = 100

	highLabelConfidence = 90
	lowLabelConfidence  = 50

	extractModelPrefix = "reducto/"
	defaultFileName    = "contract.pdf"
)

const (
	MediaTypePDF  = "application/pdf"
	MediaTypePNG  = "image/png"
	MediaTypeJPEG = "image/jpeg"
)

const (
	DocumentTypeMSA             = "msa"
	DocumentTypeOrderForm       = "order_form"
	DocumentTypePurchaseOrder   = "purchase_order"
	DocumentTypeAmendment       = "amendment"
	DocumentTypeStatementOfWork = "statement_of_work"
	DocumentTypeRenewal         = "renewal"
	DocumentTypeOther           = "other"
)

const (
	ContractTypeNew       = "new"
	ContractTypeAmendment = "amendment"
	ContractTypeRenewal   = "renewal"
)

const (
	ChargeKindRecurringFixed = "recurring_fixed"
	ChargeKindUsage          = "usage"
	ChargeKindOneTime        = "one_time"
)

const (
	BillingTimingAdvance = "advance"
	BillingTimingArrears = "arrears"
)

const (
	BillingPeriodMonthly    = "MONTHLY"
	BillingPeriodQuarterly  = "QUARTERLY"
	BillingPeriodHalfYearly = "HALF_YEARLY"
	BillingPeriodAnnual     = "ANNUAL"
	BillingPeriodWeekly     = "WEEKLY"
	BillingPeriodDaily      = "DAILY"
	BillingPeriodOneTime    = "ONETIME"
)

const (
	TierModeGraduated = "graduated"
	TierModeVolume    = "volume"
	TierModeUnknown   = "unknown"
)

var (
	documentTypes = []string{DocumentTypeMSA, DocumentTypeOrderForm, DocumentTypePurchaseOrder,
		DocumentTypeAmendment, DocumentTypeStatementOfWork, DocumentTypeRenewal, DocumentTypeOther}
	contractTypes  = []string{ContractTypeNew, ContractTypeAmendment, ContractTypeRenewal}
	chargeKinds    = []string{ChargeKindRecurringFixed, ChargeKindUsage, ChargeKindOneTime}
	billingTimings = []string{BillingTimingAdvance, BillingTimingArrears}
	tierModes      = []string{TierModeGraduated, TierModeVolume, TierModeUnknown}

	billingPeriods = []string{BillingPeriodMonthly, BillingPeriodQuarterly, BillingPeriodHalfYearly, BillingPeriodAnnual,
		BillingPeriodWeekly, BillingPeriodDaily, BillingPeriodOneTime}
)
