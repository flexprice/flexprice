package customer

import (
	"testing"

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
