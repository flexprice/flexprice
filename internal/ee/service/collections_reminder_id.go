package service

import (
	"regexp"
	"strings"
)

const reminderIDPrefix = "rmd_"

var (
	reminderCurrencyPattern = regexp.MustCompile(`^[a-z0-9]{2,10}$`)
	reminderCustomerPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)
)

// formatReminderID names the reminder of a customer's overdue invoices in one currency: rmd_<currency>_<customer_id>.
func formatReminderID(currency, customerID string) string {
	return reminderIDPrefix + currency + "_" + customerID
}

// parseReminderID splits a reminder id and reports false for anything that format cannot produce.
func parseReminderID(id string) (currency, customerID string, ok bool) {
	rest, found := strings.CutPrefix(id, reminderIDPrefix)
	if !found {
		return "", "", false
	}
	currency, customerID, found = strings.Cut(rest, "_")
	if !found || !reminderCurrencyPattern.MatchString(currency) || !reminderCustomerPattern.MatchString(customerID) {
		return "", "", false
	}
	return currency, customerID, true
}
