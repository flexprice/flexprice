package types

// BillingEntityMode is a tenant's billing entities state; it only moves forward.
type BillingEntityMode string

const (
	BillingEntityModeOff      BillingEntityMode = "off"
	BillingEntityModeEnabling BillingEntityMode = "enabling"
	BillingEntityModeEnabled  BillingEntityMode = "enabled"
)

// Normalize reads an empty mode, as found in stale cached tenants, as off.
func (m BillingEntityMode) Normalize() BillingEntityMode {
	if m == "" {
		return BillingEntityModeOff
	}
	return m
}

func (m BillingEntityMode) Validate() error {
	return validateEnum("billing_entity_mode", m, []BillingEntityMode{
		BillingEntityModeOff,
		BillingEntityModeEnabling,
		BillingEntityModeEnabled,
	})
}

// SellerAddress is a billing entity's postal address, in the tenant billing details shape.
type SellerAddress struct {
	Line1      string `json:"address_line1,omitempty"`
	Line2      string `json:"address_line2,omitempty"`
	City       string `json:"address_city,omitempty"`
	State      string `json:"address_state,omitempty"`
	PostalCode string `json:"address_postal_code,omitempty"`
	Country    string `json:"address_country,omitempty"`
}

// SellerDetails are the contact details a billing entity prints as seller.
type SellerDetails struct {
	Email     string        `json:"email,omitempty"`
	HelpEmail string        `json:"help_email,omitempty"`
	Phone     string        `json:"phone,omitempty"`
	Address   SellerAddress `json:"address,omitempty"`
}

// SellerSnapshot freezes the seller of a finalized invoice.
type SellerSnapshot struct {
	BillingEntityID string          `json:"billing_entity_id"`
	LegalName       string          `json:"legal_name"`
	SellerDetails   SellerDetails   `json:"seller_details"`
	TaxIdentifiers  []TaxIdentifier `json:"tax_identifiers,omitempty"`
}

// BillingEntityFilter represents the filter options for billing entities
type BillingEntityFilter struct {
	*QueryFilter

	BillingEntityIDs []string `json:"billing_entity_ids,omitempty" form:"billing_entity_ids"`
	IsDefault        *bool    `json:"is_default,omitempty" form:"is_default"`
}

// NewBillingEntityFilter creates a new billing entity filter with default options
func NewBillingEntityFilter() *BillingEntityFilter {
	return &BillingEntityFilter{QueryFilter: NewDefaultQueryFilter()}
}

// NewNoLimitBillingEntityFilter creates a new billing entity filter without pagination
func NewNoLimitBillingEntityFilter() *BillingEntityFilter {
	return &BillingEntityFilter{QueryFilter: NewNoLimitQueryFilter()}
}

func (f *BillingEntityFilter) Validate() error {
	if f.QueryFilter != nil {
		return f.QueryFilter.Validate()
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *BillingEntityFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *BillingEntityFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *BillingEntityFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *BillingEntityFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *BillingEntityFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *BillingEntityFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *BillingEntityFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
