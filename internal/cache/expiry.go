package cache

import "time"

const (
	ExpiryDefaultInMemory  = 2 * time.Minute
	ExpiryDefaultRedis     = 30 * time.Minute
	ExpiryWalletBalance    = 30 * time.Minute
	ExpiryWalletAlertCheck = 1 * time.Minute
	ExpiryPriceSyncLock    = 6 * time.Hour
	ExpiryBillerTaxIDs     = 1 * time.Hour
)
