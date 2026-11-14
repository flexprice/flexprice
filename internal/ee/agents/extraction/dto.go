package extraction

type ExtractRequest struct {
	FileName  string
	MediaType string
	Data      []byte
	Model     string
}

type Term struct {
	Value      string   `json:"value"`
	Confidence int      `json:"confidence"`
	Page       int      `json:"page"`
	Quote      string   `json:"quote"`
	Options    []string `json:"options"`
	Note       string   `json:"note"`
	Boxes      []Box    `json:"boxes,omitempty"`
	Derived    bool     `json:"derived,omitempty"`
}

type Box struct {
	Page   int     `json:"page"`
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (t Term) IsSet() bool { return t.Value != "" }

type Extraction struct {
	Result *Result `json:"result"`
	Model  string  `json:"model"`
	JobID  string  `json:"job_id,omitempty"`
	Usage  *Usage  `json:"usage"`
}

type Usage struct {
	Pages           int      `json:"pages"`
	Fields          int      `json:"fields"`
	Credits         *float64 `json:"credits,omitempty"`
	DurationSeconds float64  `json:"duration_seconds"`
}

type Result struct {
	Documents     []Document     `json:"documents"`
	ContractType  Term           `json:"contract_type"`
	Customer      Customer       `json:"customer"`
	Term          ContractTerm   `json:"term"`
	Billing       Billing        `json:"billing"`
	Charges       []Charge       `json:"charges"`
	Commitments   []Commitment   `json:"commitments"`
	Credits       []Credit       `json:"credits"`
	ServiceLevels []ServiceLevel `json:"service_levels"`
	Doubts        []string       `json:"doubts"`

	CustomerMatch *CustomerMatch `json:"customer_match,omitempty"`
	Derived       *Derived       `json:"derived,omitempty"`
	ToCheck       *ToCheck       `json:"to_check,omitempty"`
}

type Document struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	PageStart int    `json:"page_start"`
	PageEnd   int    `json:"page_end"`
}

type Customer struct {
	LegalName      Term `json:"legal_name"`
	BillingAddress Term `json:"billing_address"`
	PrimaryContact Term `json:"primary_contact"`
	BillingEmail   Term `json:"billing_email"`
}

type ContractTerm struct {
	SignedDate         Term `json:"signed_date"`
	EffectiveDate      Term `json:"effective_date"`
	TermMonths         Term `json:"term_months"`
	AutoRenew          Term `json:"auto_renew"`
	RenewalTermMonths  Term `json:"renewal_term_months"`
	NoticeDays         Term `json:"notice_days"`
	RenewalPriceChange Term `json:"renewal_price_change"`
	Cancellation       Term `json:"cancellation"`
}

type Billing struct {
	Structure    Term `json:"structure"`
	Currency     Term `json:"currency"`
	InvoiceDay   Term `json:"invoice_day"`
	PaymentTerms Term `json:"payment_terms"`
	LateFee      Term `json:"late_fee"`
	Tax          Term `json:"tax"`
}

type Charge struct {
	Kind               string `json:"kind"`
	Name               Term   `json:"name"`
	Amount             Term   `json:"amount"`
	AmountPeriod       Term   `json:"amount_period"`
	BillingPeriod      Term   `json:"billing_period"`
	BillingTiming      Term   `json:"billing_timing"`
	IncludedQuantity   Term   `json:"included_quantity"`
	Unit               Term   `json:"unit"`
	TierMode           Term   `json:"tier_mode"`
	Tiers              []Tier `json:"tiers"`
	Trigger            Term   `json:"trigger"`
	RevenueRecognition Term   `json:"revenue_recognition"`
}

type Tier struct {
	UpTo       string `json:"up_to"`
	UnitAmount string `json:"unit_amount"`
	FlatAmount string `json:"flat_amount"`
}

type Commitment struct {
	Amount    Term `json:"amount"`
	Period    Term `json:"period"`
	Covers    Term `json:"covers"`
	Shortfall Term `json:"shortfall"`
}

type Credit struct {
	Amount Term `json:"amount"`
	Kind   Term `json:"kind"`
	Expiry Term `json:"expiry"`
}

type ServiceLevel struct {
	Description Term `json:"description"`
}

type CustomerMatch struct {
	Status     string   `json:"status"`
	CustomerID string   `json:"customer_id,omitempty"`
	Name       string   `json:"name,omitempty"`
	MatchedOn  []string `json:"matched_on,omitempty"`
}

type Derived struct {
	EndDate          Term              `json:"end_date"`
	NoticeDeadline   Term              `json:"notice_deadline"`
	InvoiceSchedules []InvoiceSchedule `json:"invoice_schedules"`
}

type InvoiceSchedule struct {
	ChargeIndex      int      `json:"charge_index"`
	ChargeName       string   `json:"charge_name"`
	AmountPerInvoice string   `json:"amount_per_invoice"`
	Dates            []string `json:"dates"`
}

type ToCheck struct {
	Total    int `json:"total"`
	Customer int `json:"customer"`
	Charges  int `json:"charges"`
	Billing  int `json:"billing"`
}
