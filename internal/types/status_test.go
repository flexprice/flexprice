package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Ent queries use the slice, doubles use the switch
func TestReadableCustomerStatusesMatchesPredicate(t *testing.T) {
	all := []Status{StatusPublished, StatusArchived, StatusDeleted, StatusSuppressed, ""}

	slice := ReadableCustomerStatuses()
	inSlice := make(map[string]bool, len(slice))
	for _, s := range slice {
		inSlice[s] = true
	}

	for _, s := range all {
		assert.Equal(t, IsCustomerStatusReadable(s), inSlice[CustomerStatusOrDefault(s)],
			"status %q disagrees between predicate and slice", s)
	}
}

func TestSuppressedCustomerNeverReadable(t *testing.T) {
	assert.False(t, IsCustomerStatusReadable(StatusSuppressed))
	assert.NotContains(t, ReadableCustomerStatuses(), string(StatusSuppressed))
}

func TestUnsetCustomerStatusIsReadable(t *testing.T) {
	assert.True(t, IsCustomerStatusReadable(""))
	assert.Contains(t, ReadableCustomerStatuses(), string(StatusPublished))
}
