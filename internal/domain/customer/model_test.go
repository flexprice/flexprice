package customer

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePersons(t *testing.T) {
	tests := []struct {
		name    string
		persons []*ContactPerson
		wantErr bool
	}{
		{name: "empty"},
		{name: "one primary and one extra", persons: []*ContactPerson{{IsPrimary: true, FirstName: "Ravi"}, {FirstName: "Fin"}}},
		{name: "two primaries", persons: []*ContactPerson{{IsPrimary: true, FirstName: "A"}, {IsPrimary: true, FirstName: "B"}}, wantErr: true},
		{name: "duplicate ids", persons: []*ContactPerson{{ID: "cp_1", FirstName: "A"}, {ID: "cp_1", FirstName: "B"}}, wantErr: true},
		{name: "same person twice", persons: []*ContactPerson{{FirstName: "A", Email: "a@example.test"}, {FirstName: "a", Email: "A@example.test"}}, wantErr: true},
		{name: "null entry", persons: []*ContactPerson{nil}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePersons(tt.persons)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			for _, p := range tt.persons {
				assert.NotEmpty(t, p.ID)
			}
		})
	}
}

func TestValidateAdditionalEmails(t *testing.T) {
	invoice := []types.EmailType{types.EmailTypeInvoice}
	tests := []struct {
		name    string
		emails  []*types.AdditionalEmail
		wantErr bool
	}{
		{name: "empty"},
		{name: "valid", emails: []*types.AdditionalEmail{
			{Email: "ap@example.test", Types: invoice},
			{Email: "fin@example.test", Types: invoice},
		}},
		{name: "null entry", emails: []*types.AdditionalEmail{nil}, wantErr: true},
		{name: "no address", emails: []*types.AdditionalEmail{{Types: invoice}}, wantErr: true},
		{name: "repeated address", emails: []*types.AdditionalEmail{{Email: "ap@example.test", Types: invoice}, {Email: "AP@example.test", Types: invoice}}, wantErr: true},
		{name: "no types", emails: []*types.AdditionalEmail{{Email: "ap@example.test"}}, wantErr: true},
		{name: "unknown type", emails: []*types.AdditionalEmail{{Email: "ap@example.test", Types: []types.EmailType{"support"}}}, wantErr: true},
		{name: "empty type", emails: []*types.AdditionalEmail{{Email: "ap@example.test", Types: []types.EmailType{""}}}, wantErr: true},
		{name: "repeated type", emails: []*types.AdditionalEmail{{Email: "ap@example.test", Types: []types.EmailType{types.EmailTypeInvoice, types.EmailTypeInvoice}}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAdditionalEmails(tt.emails)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestInvoiceEmailRecipients(t *testing.T) {
	invoice := []types.EmailType{types.EmailTypeInvoice}
	tests := []struct {
		name     string
		customer *Customer
		want     []string
	}{
		{name: "primary only", customer: &Customer{Email: "owner@example.test"}, want: []string{"owner@example.test"}},
		{name: "no email anywhere", customer: &Customer{}, want: nil},
		{
			name: "primary plus invoice-tagged, others skipped",
			customer: &Customer{Email: "owner@example.test", AdditionalEmails: []*types.AdditionalEmail{
				{Email: "ap@example.test", Types: invoice},
				{Email: "other@example.test", Types: []types.EmailType{"other"}},
				{Email: "fin@example.test", Types: invoice},
			}},
			want: []string{"owner@example.test", "ap@example.test", "fin@example.test"},
		},
		{
			name: "additional repeating primary is deduped",
			customer: &Customer{Email: "owner@example.test", AdditionalEmails: []*types.AdditionalEmail{
				{Email: "OWNER@example.test", Types: invoice},
			}},
			want: []string{"owner@example.test"},
		},
		{
			name: "invoice-tagged without primary",
			customer: &Customer{AdditionalEmails: []*types.AdditionalEmail{
				{Email: "ap@example.test", Types: invoice},
			}},
			want: []string{"ap@example.test"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.customer.InvoiceEmailRecipients())
		})
	}
}
