package customermatch

import "time"

const (
	defaultMaxSteps     = 8
	defaultMaxToolCalls = 12
	defaultTimeout      = 60 * time.Second

	defaultSearchLimit = 10
	maxSearchLimit     = 20
	minQueryLength     = 2
	maxQueryLength     = 100
	maxCandidates      = 5
	maxConfidence      = 100
)

const (
	toolSearchByName  = "search_customers_by_name"
	toolSearchByEmail = "search_customers_by_email"
	toolGetCustomer   = "get_customer"
	toolSubmitMatch   = "submit_match"
)

const (
	EvidenceExactName     = "exact_name"
	EvidenceNameVariant   = "name_variant"
	EvidenceEmailExact    = "email_exact"
	EvidenceEmailDomain   = "email_domain"
	EvidenceAddress       = "address"
	EvidenceContactPerson = "contact_person"
	EvidenceExternalID    = "external_id"
)

var evidenceKinds = []string{
	EvidenceExactName, EvidenceNameVariant, EvidenceEmailExact, EvidenceEmailDomain,
	EvidenceAddress, EvidenceContactPerson, EvidenceExternalID,
}
