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
		{name: "invoice email type", persons: []*ContactPerson{{FirstName: "AP", Email: "ap@example.test", EmailTypes: []types.EmailType{types.EmailTypeInvoice}}}},
		{name: "email type without email", persons: []*ContactPerson{{FirstName: "AP", EmailTypes: []types.EmailType{types.EmailTypeInvoice}}}, wantErr: true},
		{name: "unknown email type", persons: []*ContactPerson{{FirstName: "AP", Email: "ap@example.test", EmailTypes: []types.EmailType{"support"}}}, wantErr: true},
		{name: "empty email type", persons: []*ContactPerson{{FirstName: "AP", Email: "ap@example.test", EmailTypes: []types.EmailType{""}}}, wantErr: true},
		{name: "repeated email type", persons: []*ContactPerson{{FirstName: "AP", Email: "ap@example.test", EmailTypes: []types.EmailType{types.EmailTypeInvoice, types.EmailTypeInvoice}}}, wantErr: true},
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

func TestEmailRecipients(t *testing.T) {
	invoice := []types.EmailType{types.EmailTypeInvoice}
	tests := []struct {
		name     string
		customer *Customer
		want     []string
	}{
		{name: "no contact persons uses customer email", customer: &Customer{Email: "owner@example.test"}, want: []string{"owner@example.test"}},
		{name: "no email anywhere", customer: &Customer{}, want: nil},
		{
			name: "untagged contact persons use customer email",
			customer: &Customer{Email: "owner@example.test", ContactPersons: []*ContactPerson{
				{FirstName: "Ana", Email: "ana@example.test", IsPrimary: true},
			}},
			want: []string{"owner@example.test"},
		},
		{
			name: "invoice contacts replace customer email",
			customer: &Customer{Email: "owner@example.test", ContactPersons: []*ContactPerson{
				{FirstName: "AP", Email: "ap@example.test", EmailTypes: invoice},
				{FirstName: "Ana", Email: "ana@example.test"},
				{FirstName: "Fin", Email: "fin@example.test", EmailTypes: invoice},
			}},
			want: []string{"ap@example.test", "fin@example.test"},
		},
		{
			name: "repeated contact email is sent once",
			customer: &Customer{ContactPersons: []*ContactPerson{
				{FirstName: "AP", Email: "ap@example.test", EmailTypes: invoice},
				{FirstName: "Desk", Email: "AP@example.test", EmailTypes: invoice},
			}},
			want: []string{"ap@example.test"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.customer.EmailRecipients(types.EmailTypeInvoice))
		})
	}
}
