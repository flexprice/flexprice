package customermatch

import (
	"time"

	"github.com/flexprice/flexprice/internal/ee/agents/providers/openrouter"
)

type MatchRequest struct {
	LegalName      string `json:"legal_name"`
	BillingEmail   string `json:"billing_email"`
	BillingAddress string `json:"billing_address"`
	PrimaryContact string `json:"primary_contact"`
}

type CustomerRecord struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Email      string    `json:"email,omitempty"`
	ExternalID string    `json:"external_id,omitempty"`
	Contact    string    `json:"contact,omitempty"`
	City       string    `json:"city,omitempty"`
	Country    string    `json:"country,omitempty"`
	Currency   string    `json:"currency,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Candidate struct {
	CustomerID string   `json:"customer_id"`
	Confidence int      `json:"confidence"`
	Evidence   []string `json:"evidence"`
	Reason     string   `json:"reason"`
}

type MatchResult struct {
	BestCustomerID string            `json:"best_customer_id,omitempty"`
	Candidates     []Candidate       `json:"candidates"`
	Model          string            `json:"model"`
	Steps          int               `json:"steps"`
	ToolCalls      int               `json:"tool_calls"`
	Usage          *openrouter.Usage `json:"usage,omitempty"`
}

type submission struct {
	BestCustomerID *string     `json:"best_customer_id"`
	Candidates     []Candidate `json:"candidates"`
}

type nameSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type emailSearchArgs struct {
	EmailOrDomain string `json:"email_or_domain"`
}

type getCustomerArgs struct {
	CustomerID string `json:"customer_id"`
}
