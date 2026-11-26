package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestNewHostedInvoice_StripsCustomerContacts(t *testing.T) {
	cust := &customer.Customer{
		ID:               "cust_1",
		Email:            "billing@acme.com",
		Metadata:         map[string]string{"k": "v"},
		ContactPersons:   []*customer.ContactPerson{{FirstName: "Ana", Email: "ana@acme.com"}},
		AdditionalEmails: []*types.AdditionalEmail{{Email: "ap@acme.com", Types: []types.EmailType{types.EmailTypeInvoice}}},
	}
	inv := &InvoiceResponse{Customer: &CustomerResponse{Customer: cust}}

	got := NewHostedInvoice(inv).Customer.Customer

	require.Nil(t, got.Metadata)
	require.Nil(t, got.ContactPersons)
	require.Nil(t, got.AdditionalEmails)
	require.Equal(t, "cust_1", got.ID)
	require.NotNil(t, cust.AdditionalEmails, "the source invoice must not be modified")
}
