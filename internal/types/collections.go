package types

import "time"

// CollectionsColumnKey identifies a column on the collections board.
type CollectionsColumnKey string

const (
	CollectionsColumnNotDue       CollectionsColumnKey = "not_due"
	CollectionsColumnOverdue      CollectionsColumnKey = "overdue"
	CollectionsColumnChasing      CollectionsColumnKey = "chasing"
	CollectionsColumnHeld         CollectionsColumnKey = "held"
	CollectionsColumnPaidThisWeek CollectionsColumnKey = "paid_this_week"
)

// ReminderStatus is the state of an invoice reminder; an invoice nobody acted on is pending.
type ReminderStatus string

const (
	ReminderStatusPending  ReminderStatus = "pending"
	ReminderStatusApproved ReminderStatus = "approved"
	ReminderStatusHeld     ReminderStatus = "held"
	ReminderStatusSkipped  ReminderStatus = "skipped"
)

// ReminderTone is the wording level of a reminder, set by how overdue the invoice is.
type ReminderTone string

const (
	ReminderToneFriendly ReminderTone = "friendly"
	ReminderToneFirm     ReminderTone = "firm"
	ReminderToneFinal    ReminderTone = "final"
)

const CollectionsPaidWindow = 7 * 24 * time.Hour

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

func IsOverdue(dueDate *time.Time, now time.Time) bool {
	return dueDate != nil && dueDate.Before(now)
}

func ColumnStatuses(column CollectionsColumnKey) []ReminderStatus {
	switch column {
	case CollectionsColumnOverdue:
		return []ReminderStatus{ReminderStatusPending}
	case CollectionsColumnChasing:
		return []ReminderStatus{ReminderStatusApproved}
	case CollectionsColumnHeld:
		return []ReminderStatus{ReminderStatusHeld, ReminderStatusSkipped}
	}
	return nil
}
