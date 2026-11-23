package collections

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/types"
)

func TestInvoiceReminder_OriginalAndEdited(t *testing.T) {
	r := NewInvoiceReminderBuilder(nil).
		WithOriginal(types.ReminderToneFirm, "subject", "body").
		WithText(types.ReminderToneFirm, "subject", "body").
		Build()
	require.Equal(t, Metadata{OriginalTone: types.ReminderToneFirm, OriginalSubject: "subject", OriginalBody: "body"}, r.Original())
	require.False(t, r.Edited())

	for name, edited := range map[string]*InvoiceReminder{
		"tone":    NewInvoiceReminderBuilder(r).WithText(types.ReminderToneFinal, "subject", "body").Build(),
		"subject": NewInvoiceReminderBuilder(r).WithText(types.ReminderToneFirm, "other", "body").Build(),
		"body":    NewInvoiceReminderBuilder(r).WithText(types.ReminderToneFirm, "subject", "other").Build(),
	} {
		require.True(t, edited.Edited(), name)
		require.Equal(t, r.Original(), edited.Original(), "editing the text leaves the original alone")
	}
	require.True(t, NewInvoiceReminderBuilder(nil).WithText(types.ReminderToneFirm, "s", "b").Build().Edited(), "no metadata means the text counts as edited")
}

func TestInvoiceReminderBuilder_CopiesTheSeed(t *testing.T) {
	seed := &InvoiceReminder{ID: "irem_1"}
	built := NewInvoiceReminderBuilder(seed).WithStatus(types.ReminderStatusHeld, "wait", nil).Build()
	require.Empty(t, seed.ReminderStatus)
	require.Equal(t, types.ReminderStatusHeld, built.ReminderStatus)
}
