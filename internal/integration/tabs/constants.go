package tabs

import "time"

// tabsDateLayout is the date format Tabs expects for service/billing dates (YYYY-MM-DD).
const tabsDateLayout = "2006-01-02"

// tabsInvoiceSyncLockTTL bounds how long a single invoice's sync holds its distributed lock.
const tabsInvoiceSyncLockTTL = 2 * time.Minute

// Tabs invoiceDateStrategy values, mapped from a flexprice invoice cadence by invoiceDateStrategy.
const (
	invoiceDateStrategyArrears       = "ARREARS"
	invoiceDateStrategyFirstOfPeriod = "FIRST_OF_PERIOD"
)

// chargeCategory is the coarse charge category a line item is billed under on Tabs. Every line item
// resolves to exactly one category, and each environment has exactly one Tabs product per category.
type chargeCategory string

const (
	categoryFixed chargeCategory = "fixed"
	categoryUsage chargeCategory = "usage"
)
