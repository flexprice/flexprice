package types

// CollectionsColumnKey identifies a column on the collections board.
type CollectionsColumnKey string

const (
	CollectionsColumnNotDue       CollectionsColumnKey = "not_due"
	CollectionsColumnOverdue      CollectionsColumnKey = "overdue"
	CollectionsColumnChasing      CollectionsColumnKey = "chasing"
	CollectionsColumnHeld         CollectionsColumnKey = "held"
	CollectionsColumnPaidThisWeek CollectionsColumnKey = "paid_this_week"
)

// ReceivableSourceType names the system a receivable comes from.
type ReceivableSourceType string

const (
	ReceivableSourceFlexprice ReceivableSourceType = "flexprice"
)

// ReceivableStatus is the normalized state of a receivable.
type ReceivableStatus string

const (
	ReceivableStatusOpen ReceivableStatus = "open"
	ReceivableStatusPaid ReceivableStatus = "paid"
	ReceivableStatusVoid ReceivableStatus = "void"
)

// ReminderStatus is the decision recorded on a reminder draft.
type ReminderStatus string

const (
	ReminderStatusPending  ReminderStatus = "pending"
	ReminderStatusApproved ReminderStatus = "approved"
	ReminderStatusHeld     ReminderStatus = "held"
	ReminderStatusSkipped  ReminderStatus = "skipped"
)

// ReminderTone is the wording level of a reminder, set by how overdue the oldest invoice is.
type ReminderTone string

const (
	ReminderToneFriendly ReminderTone = "friendly"
	ReminderToneFirm     ReminderTone = "firm"
	ReminderToneFinal    ReminderTone = "final"
)

func (s ReminderStatus) Valid() bool {
	switch s {
	case ReminderStatusPending, ReminderStatusApproved, ReminderStatusHeld, ReminderStatusSkipped:
		return true
	}
	return false
}

func (t ReminderTone) Valid() bool {
	switch t {
	case ReminderToneFriendly, ReminderToneFirm, ReminderToneFinal:
		return true
	}
	return false
}
