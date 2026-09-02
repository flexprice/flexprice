package types

import (
	"regexp"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// Metadata keys carrying Indian GST tax identity. These live in the existing
// metadata maps rather than dedicated columns; this file is the only place that
// knows the key names, so a later move to real columns is mechanical.
const (
	MetadataKeyGSTIN          = "gstin"
	MetadataKeyPAN            = "pan"
	MetadataKeyPlaceOfSupply  = "place_of_supply"
	MetadataKeyHSNSAC         = "hsn_sac"
	MetadataKeyShippingPrefix = "shipping_address_"
)

var (
	gstinRegex  = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z]{1}[A-Z0-9]{1}Z[A-Z0-9]{1}$`)
	panRegex    = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)
	hsnSACRegex = regexp.MustCompile(`^[0-9]{4}([0-9]{2})?([0-9]{2})?$`)
)

// Address is a read-only view of a postal address parsed out of a metadata map.
// It is deliberately not one of the codebase's storage address structs
// (ent/schema.TenantAddress, domain/tenant.TenantAddress) nor the render-side
// domain/pdf.AddressInfo: its fields are private and it is never serialised.
// Those types cannot be reused here — they all sit above internal/types in the
// dependency graph, so importing them would be circular. Converging the four
// shapes means making a canonical type here and having the others adopt it.
type Address struct {
	line1      string
	line2      string
	city       string
	state      string
	postalCode string
	country    string
}

func (a *Address) Line1() string {
	if a == nil {
		return ""
	}
	return a.line1
}

func (a *Address) Line2() string {
	if a == nil {
		return ""
	}
	return a.line2
}

func (a *Address) City() string {
	if a == nil {
		return ""
	}
	return a.city
}

func (a *Address) State() string {
	if a == nil {
		return ""
	}
	return a.state
}

func (a *Address) PostalCode() string {
	if a == nil {
		return ""
	}
	return a.postalCode
}

func (a *Address) Country() string {
	if a == nil {
		return ""
	}
	return a.country
}

// IsEmpty reports whether no address component was present.
func (a *Address) IsEmpty() bool {
	if a == nil {
		return true
	}
	return a.line1 == "" && a.line2 == "" && a.city == "" &&
		a.state == "" && a.postalCode == "" && a.country == ""
}

// TaxMetadata is a read-only view over the tax identity keys of a metadata map.
type TaxMetadata struct {
	gstin           string
	pan             string
	placeOfSupply   string
	hsnSAC          string
	shippingAddress *Address
}

// TaxMetadataFromMap parses tax identity out of a metadata map. Values are
// normalised (trimmed, upper-cased) so that comparison and validation do not
// depend on how the caller typed them. Returns nil only for a nil map.
func TaxMetadataFromMap(m map[string]string) *TaxMetadata {
	if m == nil {
		return nil
	}

	t := &TaxMetadata{
		gstin:         normaliseTaxID(m[MetadataKeyGSTIN]),
		pan:           normaliseTaxID(m[MetadataKeyPAN]),
		placeOfSupply: strings.TrimSpace(m[MetadataKeyPlaceOfSupply]),
		hsnSAC:        strings.TrimSpace(m[MetadataKeyHSNSAC]),
	}
	t.shippingAddress = shippingAddressFromMap(m)
	return t
}

func normaliseTaxID(v string) string {
	return strings.ToUpper(strings.TrimSpace(v))
}

func shippingAddressFromMap(m map[string]string) *Address {
	a := &Address{
		line1:      strings.TrimSpace(m[MetadataKeyShippingPrefix+"line1"]),
		line2:      strings.TrimSpace(m[MetadataKeyShippingPrefix+"line2"]),
		city:       strings.TrimSpace(m[MetadataKeyShippingPrefix+"city"]),
		state:      strings.TrimSpace(m[MetadataKeyShippingPrefix+"state"]),
		postalCode: strings.TrimSpace(m[MetadataKeyShippingPrefix+"postal_code"]),
		country:    strings.TrimSpace(m[MetadataKeyShippingPrefix+"country"]),
	}
	if a.IsEmpty() {
		return nil
	}
	return a
}

func (t *TaxMetadata) GSTIN() string {
	if t == nil {
		return ""
	}
	return t.gstin
}

// PAN returns the explicit PAN when set, else the PAN embedded in the GSTIN.
// A GSTIN is <2-digit state code><10-char PAN><3 chars>, so the PAN is derivable.
func (t *TaxMetadata) PAN() string {
	if t == nil {
		return ""
	}
	if t.pan != "" {
		return t.pan
	}
	return panFromGSTIN(t.gstin)
}

// PlaceOfSupply returns the explicit place of supply when set, else the 2-digit
// state code embedded in the GSTIN.
func (t *TaxMetadata) PlaceOfSupply() string {
	if t == nil {
		return ""
	}
	if t.placeOfSupply != "" {
		return t.placeOfSupply
	}
	return stateCodeFromGSTIN(t.gstin)
}

func (t *TaxMetadata) HSNSAC() string {
	if t == nil {
		return ""
	}
	return t.hsnSAC
}

func (t *TaxMetadata) ShippingAddress() *Address {
	if t == nil {
		return nil
	}
	return t.shippingAddress
}

func panFromGSTIN(gstin string) string {
	if !gstinRegex.MatchString(gstin) {
		return ""
	}
	return gstin[2:12]
}

func stateCodeFromGSTIN(gstin string) string {
	if !gstinRegex.MatchString(gstin) {
		return ""
	}
	return gstin[0:2]
}

// Validate checks the tax identity values that are present. Absent values are
// valid — these fields are optional for every customer outside India.
func (t *TaxMetadata) Validate() error {
	if t == nil {
		return nil
	}

	if t.gstin != "" && !gstinRegex.MatchString(t.gstin) {
		return ierr.NewError("invalid GSTIN").
			WithHint("GSTIN must be 15 characters, e.g. \"27AAMCM4148E1ZD\"").
			Mark(ierr.ErrValidation)
	}

	if t.pan != "" && !panRegex.MatchString(t.pan) {
		return ierr.NewError("invalid PAN").
			WithHint("PAN must be 10 characters, e.g. \"AAMCC5329F\"").
			Mark(ierr.ErrValidation)
	}

	// A GSTIN embeds the PAN. Storing a contradicting pair would leave the two
	// sync paths disagreeing about which is authoritative.
	if derived := panFromGSTIN(t.gstin); derived != "" && t.pan != "" && derived != t.pan {
		return ierr.NewError("PAN does not match the PAN embedded in the GSTIN").
			WithHint("Characters 3-12 of the GSTIN are the PAN; either correct the PAN or omit it").
			WithReportableDetails(map[string]interface{}{
				"pan_from_gstin": derived,
			}).
			Mark(ierr.ErrValidation)
	}

	if err := ValidateHSNSAC(t.hsnSAC); err != nil {
		return err
	}

	return nil
}

// ValidateTaxMetadata parses and validates the tax identity in a metadata map.
func ValidateTaxMetadata(m map[string]string) error {
	return TaxMetadataFromMap(m).Validate()
}

// ValidateHSNSAC checks a standalone HSN/SAC code, for callers that hold one
// outside a metadata map.
func ValidateHSNSAC(code string) error {
	if code == "" {
		return nil
	}
	if !hsnSACRegex.MatchString(code) {
		return ierr.NewError("invalid HSN/SAC code").
			WithHint("HSN/SAC must be 4, 6 or 8 digits, e.g. \"998415\"").
			Mark(ierr.ErrValidation)
	}
	return nil
}
