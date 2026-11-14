package contract

const (
	schemaName = "contract_extraction"

	maxParseAttempts = 2
	maxConfidence    = 100
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

	billingPeriods = []string{"MONTHLY", "QUARTERLY", "HALF_YEARLY", "ANNUAL", "WEEKLY", "DAILY", "ONETIME"}
)
