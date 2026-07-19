package cache

import "time"

const (
	ExpiryDefaultInMemory  = 2 * time.Minute
	ExpiryDefaultRedis     = 30 * time.Minute
	ExpiryWalletBalance    = 30 * time.Minute
	ExpiryWalletAlertCheck = 1 * time.Minute
	ExpiryUsageAlertGate   = 1 * time.Minute
	ExpiryPriceSyncLock    = 2 * time.Hour
)
