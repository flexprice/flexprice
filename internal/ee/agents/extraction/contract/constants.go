package contract

const DefaultReviewThreshold = 90

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

const (
	CustomerMatchExisting = "existing"
	CustomerMatchNew      = "new"
)

const (
	MatchedOnEmail       = "billing_email"
	MatchedOnLegalName   = "legal_name"
	MatchedOnEmailDomain = "email_domain"
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

var periodMonths = map[string]int{"MONTHLY": 1, "QUARTERLY": 3, "HALF_YEARLY": 6, "ANNUAL": 12}

var genericEmailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true,
	"yahoo.com": true, "icloud.com": true, "proton.me": true, "protonmail.com": true, "aol.com": true,
}

var legalSuffixes = []string{"incorporated", "inc", "llc", "l.l.c", "ltd", "limited", "corp", "corporation",
	"co", "company", "plc", "gmbh", "pvt", "private", "llp", "lp", "sa", "ag", "bv", "pty"}
